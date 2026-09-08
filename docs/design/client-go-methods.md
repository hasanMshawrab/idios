# Kubernetes Monitoring -- client-go Surface Area

Companion to `data-storage.md`. That document says **what** is stored. This one says **which parts of `client-go` produce it**, table by table, so the shape of the ingest side is clear before any of it is written.

This is an orientation map, not a how-to. There is no complete code here; fragments exist only to show what a call looks like. Details of usage belong in the code and its comments.

---

## 1. The five things we actually use

Everything in the ingest layer reduces to five pieces of `client-go`. Almost every table below is filled by a combination of them.

| Piece | Package | Role |
|---|---|---|
| **Config loading** | `k8s.io/client-go/tools/clientcmd` | Turn a kubeconfig + context name into a `*rest.Config`. Also where we read the API server URL. |
| **Typed clientset** | `k8s.io/client-go/kubernetes` | `kubernetes.NewForConfig(cfg)` -> the object every API call hangs off (`clientset.CoreV1().Pods(ns)...`). |
| **Informers** | `k8s.io/client-go/informers` + `k8s.io/client-go/tools/cache` | The watch machinery. One `SharedInformerFactory` per (cluster, namespace); it does list-then-watch, caches, resyncs, and reconnects. **This is the main data source.** |
| **Direct reads** | `clientset.CoreV1().Pods(ns).GetLogs(...)`, `.Get(...)` | The few things informers can't give us: container logs, and the one-off `kube-system` Namespace lookup. |
| **Typed API structs** | `k8s.io/api/core/v1`, `k8s.io/api/batch/v1`, `k8s.io/api/apps/v1`, `k8s.io/apimachinery/pkg/apis/meta/v1` | Not client-go proper, but what every handler receives. `corev1.Pod`, `batchv1.Job`, `appsv1.ReplicaSet`, `corev1.Event`, `metav1.OwnerReference`. Column values come from walking these structs. |

Everything else in client-go (dynamic client, discovery, REST mapper, leader election, work queues, controller-runtime) is **not needed** for this design.

---

## 2. The informer pattern, once

Every watched resource uses the same shape. Written here once so the per-table sections can just say "Pod informer" and move on.

```go
factory := informers.NewSharedInformerFactoryWithOptions(
    clientset, resyncPeriod,
    informers.WithNamespace(ns),          // namespace-scoped: the RBAC constraint from Section 1 of the storage doc
)
podInformer := factory.Core().V1().Pods().Informer()
podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc:    func(obj interface{}) { /* whole *corev1.Pod */ },
    UpdateFunc: func(oldObj, newObj interface{}) { /* both whole objects */ },
    DeleteFunc: func(obj interface{}) { /* may be cache.DeletedFinalStateUnknown */ },
})
factory.Start(stopCh)
factory.WaitForCacheSync(stopCh)
```

What this gives us, mapped to the ingest contract in Section 4 of the storage doc:

| Ingest contract clause | Where it comes from |
|---|---|
| "Watches deliver whole objects, not diffs" | `UpdateFunc(old, new)` hands us two complete objects. We diff `old` vs `new` (or `new` vs our DB snapshot) ourselves. |
| "Reconciliation on startup" | After `WaitForCacheSync`, `podInformer.GetStore().List()` (or the `Lister()`) is the full current set. Anything in the DB with `deleted_at IS NULL` and absent from the store -> mark `deleted_at`, `deletion_source = 'reconcile'`. Startup only: a relist after a watch reconnect makes the informer emit `DeleteFunc` for anything that vanished in the gap. |
| "Informer resync is not used" | `resyncPeriod` is 0. Resync replays the local cache to handlers with `old == new`; it makes no API call and cannot discover deletions or changes. The diff-against-snapshot rule would reject every row it produced anyway. |
| "Missed transitions are reconstructed" | Informers reconnect on their own after a network gap. A short gap resumes from the last `resourceVersion` and the API server replays what was missed; a long gap (the version has been compacted, HTTP 410) forces a relist and the *intermediate* watch events are gone. In the second case we only see the final object; `restart_count` jumping by >1 is how we detect it. |
| DELETE event | `DeleteFunc`. Note the `cache.DeletedFinalStateUnknown` wrapper: after a watch gap the informer may only know the object *is* gone, not its last state. Unwrap before reading `.Obj`. |
| Namespace-only RBAC | `informers.WithNamespace(ns)`. One factory per namespace is the simplest way to stay inside a namespace-scoped Role. |

The four informers: `factory.Core().V1().Pods()`, `factory.Batch().V1().Jobs()`, `factory.Core().V1().Events()`, `factory.Apps().V1().ReplicaSets()`. Nothing else is watched.

---

## 3. Table by table

### 3.1 `clusters`

Not fed by an informer. Filled once per connection.

| Column | Source |
|---|---|
| `context_name` | Our own config -- the context name the user picked. |
| `api_server_url` | `clientcmd` -> `rest.Config.Host`. |
| `identity` | One direct read: `clientset.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})` -> `.ObjectMeta.UID`. If this returns a 403 (`k8s.io/apimachinery/pkg/api/errors.IsForbidden`), leave null and fall back to `api_server_url`, as the storage doc specifies. |
| `first_seen_at`, `last_connected_at` | Ours. |

`clientcmd` also provides `clientcmd.NewNonInteractiveDeferredLoadingClientConfig` / `RawConfig()` for listing the contexts in a kubeconfig -- that's how the "pick a cluster" UI gets its list.

### 3.2 `watched_namespaces`

Configuration; the user types names in. The only client-go involvement is optional: `clientset.CoreV1().Namespaces().List(...)` to offer a picker **when RBAC allows**. Expect this to fail under namespace-scoped RBAC and treat that as normal.

### 3.3 `pods`

**Pod informer.** Handler receives `*corev1.Pod`.

| Column | Struct path |
|---|---|
| `uid`, `namespace`, `name`, `created_at` | `pod.ObjectMeta.UID` / `.Namespace` / `.Name` / `.CreationTimestamp` |
| `node_name` | `pod.Spec.NodeName` (empty until scheduled) |
| `phase` | `pod.Status.Phase` |
| `status_reason`, `status_message` | `pod.Status.Reason` / `.Message` (empty unless the kubelet or scheduler set them: `Evicted`, `Preempting`, `NodeLost`, `Shutdown`, `DeadlineExceeded`) |
| `deletion_requested_at` | `pod.ObjectMeta.DeletionTimestamp` (`*metav1.Time`, nil until a delete was requested) |
| `qos_class` | `pod.Status.QOSClass` |
| `started_at` | `pod.Status.StartTime` (pointer, nil until started) |
| `controller_kind/name/uid` | `metav1.GetControllerOf(pod)` from `k8s.io/apimachinery/pkg/apis/meta/v1` -- returns the single `*OwnerReference` with `Controller: true`. Gives `.Kind`, `.Name`, `.UID`. |
| `workload_kind/name` | Resolved locally, not via the API: if controller is a ReplicaSet, look it up in the **ReplicaSet informer's store** (`rsInformer.Lister().ReplicaSets(ns).Get(name)`) and call `metav1.GetControllerOf(rs)` again to reach the Deployment. If controller is a Job, look it up in the **Job informer's store** and repeat to reach the CronJob. The listers are handed to the ingest layer behind a small `OwnerResolver` interface so `ingest` does not import `k8s.io/client-go/listers`. A miss in the informer store falls back to the store's own rows, which outlive the controller: `jobs.cronjob_name` for a Job, `rollout_history.deployment_name` for a ReplicaSet (the processor reads them in the same transaction and hands the answer to `ingest`). Only when neither knows the owner does `workload_* = controller_*` get written, and a pod whose controller is unchanged keeps the owner it already had, so a Job pruned by its CronJob cannot downgrade its pods on their last events. When a later update resolves a chain that was unknown, the pod row and the `workload_*` of its open incidents are corrected together. |
| `deleted_at`, `deletion_source` | `DeleteFunc` -> `'watch'`; store-vs-DB diff after sync -> `'reconcile'`; namespace removed from `watched_namespaces` -> `'unwatched'`. |
| `deletion_reason` | Computed in `DeleteFunc` from local data only (storage doc 5.3): `controller_kind`, sibling pods in the DB by `controller_uid` and `created_at`, ReplicaSets in `rollout_history` with the same `deployment_uid` and a higher `revision`. No `get cronjobs` / `get deployments`. |

The `workload_*` resolution is why ReplicaSets are watched at all: the lookup has to be local because a pod event can arrive faster than a round-trip to the API server, and because we don't want Deployment-level RBAC.

### 3.4 `pod_condition_history`

**Pod informer**, same handler as above. Walk `pod.Status.Conditions` (`[]corev1.PodCondition`).

| Column | Struct path |
|---|---|
| `type`, `status`, `reason`, `message` | `cond.Type`, `cond.Status`, `cond.Reason`, `cond.Message` |
| `k8s_transition_at` | `cond.LastTransitionTime` |

Insert only when `(type, status, reason)` differs from what we last stored for that pod. The `Unschedulable` case lives here: `Type: PodScheduled, Status: False, Reason: Unschedulable`, with the "0/3 nodes are available..." text in `Message`.

The `node_pressure` / `rescheduled` category split (storage doc Section 6.1) reads `Type: DisruptionTarget`'s `.Reason` from this same table: `PreemptionByScheduler` / `DeletionByTaintManager` / `DeletionByPodGC` -> `rescheduled`; `TerminationByKubelet` -> `node_pressure`; `EvictionByEvictionAPI` -> no incident, the condition row alone. These are the five reasons the API defines for the condition. No extra client-go call -- it's the condition already captured here. The condition exists on clusters 1.26+ (GA in 1.31); on older clusters `node_pressure` still comes from `pod.Status.Reason == "Evicted"`.

### 3.5 `containers`

**Pod informer.** Two parallel slices have to be zipped by container name:

- **Spec side** (`pod.Spec.InitContainers`, `pod.Spec.Containers`, `pod.Spec.EphemeralContainers`) -> `name`, `kind`, `image`, resources.
- **Status side** (`pod.Status.InitContainerStatuses`, `pod.Status.ContainerStatuses`, `pod.Status.EphemeralContainerStatuses`) -> everything runtime.

| Column | Struct path |
|---|---|
| `image` | `container.Image` (spec) |
| `image_tag` | parsed by us from `container.Image` |
| `image_id`, `container_id` | `status.ImageID`, `status.ContainerID` |
| `cpu_request` etc. (text) | `container.Resources.Requests[corev1.ResourceCPU].String()` and friends |
| `*_millis`, `*_bytes` (parsed) | `resource.Quantity.MilliValue()` / `.Value()` from `k8s.io/apimachinery/pkg/api/resource` -- no hand parsing of `500m` or `512Mi` |
| `state`, `reason`, `exit_code`, `signal`, `running_since` | `status.State` is a `corev1.ContainerState` with exactly one of `.Waiting`, `.Running`, `.Terminated` non-nil. Whichever is set gives the state; `.Waiting.Reason`, `.Terminated.Reason` / `.ExitCode` / `.Signal`, `.Running.StartedAt`. |
| `last_terminated_*` (incl. `last_terminated_signal`) | `status.LastTerminationState.Terminated` -- same struct, previous instance |
| `ready`, `restart_count` | `status.Ready`, `status.RestartCount` |
| `kind` | `init` from `Spec.InitContainers`, except entries with `RestartPolicy != nil && *RestartPolicy == corev1.ContainerRestartPolicyAlways` which are `sidecar`; `app` from `Spec.Containers`; `ephemeral` from `Spec.EphemeralContainers`. |

A status entry may not exist yet for a spec container (pod still Pending). Left-join spec to status; missing status = `waiting` with no reason.

`status.Ready` is stored as observed and interpreted by `kind` (storage doc 5.5): for `init` it is set by the kubelet only when the container terminated with exit 0; for `app` / `sidecar` it follows the readiness probe. Nothing in ingest derives readiness itself; it stores the flag and the kind side by side.

### 3.6 `container_state_history`

Same source as `containers`; no additional client-go calls. The row is inserted when the transition key `(state, reason, exit_code, restart_count, image_id, container_id)` computed from the new `ContainerStatus` differs from the `containers` snapshot row.

| Column | Struct path |
|---|---|
| `k8s_started_at` | `state.Terminated.StartedAt` or `state.Running.StartedAt` |
| `k8s_finished_at` | `state.Terminated.FinishedAt` |
| `signal` | `state.Terminated.Signal` |
| `gap_reconstructed` | ours: set when `status.RestartCount - snapshot.restart_count > 1`; the row then describes `status.LastTerminationState.Terminated` |

### 3.7 `incidents`

Derived table. No client-go source of its own -- opened, attached, and closed by our own logic from the transitions above and the Events below. The `image / image_tag / image_id` copied at open time come from the `containers` row at that moment.

Three rules for the code that derives it:

- **The DB snapshot decides what is a transition, not the watch event type.** `AddFunc` and `UpdateFunc` are handled identically: load the `pods` / `containers` rows for the uid, diff. No row -> record the snapshot, no history row; if the snapshot is itself a problem state (a waiting reason in the category table, `Unschedulable`, `Evicted`), open an incident for it (storage doc Section 4). Row exists and the transition key differs -> a real transition, history row, incident open/attach/reopen, even when it is the first event after a process restart (that is the laptop-was-asleep case the tool exists for). The informer's `old` object in `UpdateFunc` is not used as the baseline; the DB row is, so one code path covers cold start, reconnect relist and steady state.
- **Classify on `reason`, never on `message`, `phase` or `exit_code`.** `Reason` tokens (`OOMKilled`, `Error`, `ErrImagePull`, `Evicted`...) are stable enum-like strings; `Message` is free prose that changes between kubelet versions and is reused across unrelated situations (`TaintManagerEviction` also fires for "Cancelling deletion of Pod..."). `BackOff` is the one Event reason that is ambiguous (image pull vs. container restart); it is stored with `category = NULL` and attached by pod and container only. If another reason turns out to be ambiguous, do the same rather than substring-matching the message. Raw `reason`/`message` are stored so rows can be reclassified later.
- **Keep attaching events until close.** `k8s_events.incident_id` is set for events on the incident's `involved_uid` from `opened_at - stabilization_window` through `closed_at` plus the stabilization window, not just those present at open time. This is what makes the `Failed` pull event that arrived a second before the status update, the `Pulled` that resolved an `image_pull` incident, or the `Killing` that preceded a `pod_deleted` close, show up in the incident's own evidence. The attach rule itself (pod, container from `FieldPath`, category or `NULL`) is in storage doc 5.10.

**Why the pod was deleted** is computed here, in code, at `DeleteFunc` time and stored in `pods.deletion_reason` -- never at read time, because the inputs are sibling pods that are swept on the same retention clock. Inputs are all local: `controller_kind == Job` -> `job_pruned`; a ReplicaSet in `rollout_history` with the same `deployment_uid` and a higher `revision` -> `rollout`; live siblings (same `controller_uid`, `deleted_at IS NULL`) with a newer `created_at` -> `replaced`; siblings all older -> `scaled_down`; otherwise `unknown`. No extra RBAC (`get cronjobs`, `get deployments`) is used for this, so the result is labelled as inferred wherever it is shown.

**Closing on `recovered`** is not done in any handler. A 30-second ticker (storage doc 6.3) closes open incidents whose container has been stable for the window; handlers only open, attach and reopen. Reopen needs the most recent *closed* incident for the key, so the handler loads open and closed incidents for the pod, not open ones only.

### 3.8 `jobs`

**Job informer.** Handler receives `*batchv1.Job`.

| Column | Struct path |
|---|---|
| `uid`, `namespace`, `name`, `created_at` | `job.ObjectMeta` |
| `cronjob_uid/name` | `metav1.GetControllerOf(job)` where `.Kind == "CronJob"` |
| `active`, `succeeded`, `failed` | `job.Status.Active` / `.Succeeded` / `.Failed` |
| `backoff_limit`, `completions`, `parallelism` | `job.Spec.BackoffLimit` / `.Completions` / `.Parallelism` (all `*int32`, nil = default) |
| `restart_policy` | `job.Spec.Template.Spec.RestartPolicy` |
| `condition_type/reason/message` | `job.Status.Conditions` (`[]batchv1.JobCondition`): pick the one with `Status: True` among `Complete`, `Failed`, `Suspended` |
| `started_at`, `finished_at` | `job.Status.StartTime`, `job.Status.CompletionTime`; for failures use the `Failed` condition's `LastTransitionTime` |

### 3.9 `rollout_history`

**ReplicaSet informer.** Handler receives `*appsv1.ReplicaSet`.

| Column | Struct path |
|---|---|
| `replicaset_uid/name`, `namespace` | `rs.ObjectMeta` |
| `deployment_uid/name` | `metav1.GetControllerOf(rs)` where `.Kind == "Deployment"` |
| `container_name`, `image`, `image_tag` | one row per entry in `rs.Spec.Template.Spec.Containers` |
| `revision` | `rs.ObjectMeta.Annotations["deployment.kubernetes.io/revision"]`, parsed to int. Updated in place on every `UpdateFunc` because rollbacks bump it. |
| `deleted_at` | `DeleteFunc` |

ReplicaSets with `Spec.Replicas == 0` are old revisions kept by the Deployment's `revisionHistoryLimit`. They still arrive on the initial list -- that's useful, it back-fills recent rollout history for free.

### 3.10 `k8s_events`

**Event informer** on `core/v1` (`factory.Core().V1().Events()`). Handler receives `*corev1.Event`.

| Column | Struct path |
|---|---|
| `event_uid`, `namespace` | `ev.ObjectMeta.UID`, `.Namespace` |
| `type`, `reason`, `message` | `ev.Type`, `ev.Reason`, `ev.Message` |
| `involved_kind/name/uid`, `field_path` | `ev.InvolvedObject` (`corev1.ObjectReference`): `.Kind`, `.Name`, `.UID`, `.FieldPath` |
| `source_component` | `ev.Source.Component` (or `ev.ReportingController` on newer clusters -- take whichever is non-empty) |
| `count`, `first_ts`, `last_ts` | Precedence from storage doc 5.10, first non-zero wins. `last_ts`: `ev.Series.LastObservedTime` -> `ev.EventTime` -> `ev.LastTimestamp` -> `ev.FirstTimestamp` -> `ev.CreationTimestamp`. `first_ts`: `ev.EventTime` -> `ev.FirstTimestamp` -> `ev.CreationTimestamp`. `count`: `ev.Series.Count` -> `ev.Count` -> 1. `EventTime` and `Series.LastObservedTime` are `metav1.MicroTime`; the rest are whole-second `metav1.Time`. One function, one place. |
| `raw_json` | `encoding/json.Marshal(ev)` -- the typed struct serialises to the same JSON the API server sent |

Events are updated in place by Kubernetes as `count` increments, so `UpdateFunc` fires repeatedly for the same `event_uid`. That is why the table upserts on `(cluster_id, event_uid)`. `UpdateFunc` also fires for every Event object on a relist after a reconnect; a delivery whose `count` and `last_ts` do not advance past the stored row is such a replay and is dropped before it can bump an incident or trigger a capture (storage doc Section 4).

### 3.11 `artifacts`

The only table fed by **direct reads**, not informers.

**Logs:**

```go
req := clientset.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
    Container:  containerName,
    Previous:   previous,     // see below: where the dead instance lives decides this
    TailLines:  &tailPlusOne, // log_tail_lines + 1, so truncation is known exactly
    LimitBytes: &byteCap,
})
stream, err := req.Stream(ctx)   // io.ReadCloser -> write to restart_NNN.log
```

- **Which instance a call reads.** The kubelet resolves `Previous: true` to `status.LastTerminationState.Terminated.ContainerID` and `Previous: false` to the container in `status.State` (running or terminated; a `waiting` container has none and the call fails with 400). So for a `log_previous` capture: dead instance in `LastTerminationState` (the container restarted) -> `Previous: true`; dead instance in `State.Terminated` (`restartPolicy: Never`, no restart coming) -> `Previous: false`. Getting this wrong on a Job pod returns `400 previous terminated container not found` and loses the only log there is.
- **Dead-instance index** (storage doc Section 4): `status.RestartCount` is read from the label of the most recently created container, so in `waiting(CrashLoopBackOff)` it still names the dead one and once the next instance is `Running` it names the live one. Index = `RestartCount` when `State.Waiting` or `State.Terminated`, `RestartCount - 1` when `State.Running`.
- Both are only valid while the Pod exists and only for the most recent dead instance -- hence the storage doc's rule that capture is triggered the moment `RestartCount` increases, `LastTerminationState.Terminated.FinishedAt` changes, or `State.Terminated` appears. The `terminated` state alone is not the trigger; in a crash loop it is usually never present in any watch event.
- A 200 response whose body is a single line beginning with a kubelet error prefix (`unable to retrieve container logs for`, `failed to try resolving symlinks`) is a failed capture, not a log. The capture worker checks this before writing a file (storage doc 5.11, `capture_gap = 'kubelet_error'`).
- `TailLines` / `LimitBytes` are server-side. The ingester asks for `log_tail_lines + 1` lines; if that many come back, the oldest is dropped and `truncated = 1`. Comparing a count against the limit cannot otherwise tell "exactly 50 lines existed" from "cut at 50". `LimitBytes` truncation is detected the same way with `log_max_bytes + 1`.
- **`captured_early` trigger:** the Event informer handler also calls `GetLogs(Previous: false)` on `ev.Reason` in `Killing` / `Preempted` / `Preempting` / `Evicted` / `Unhealthy`, keyed by `ev.InvolvedObject.UID` -> `pod_uid`, before any `terminated` transition is seen. Match on reason only: `Killing` ("Stopping container ...") and `Preempting` (kubelet admission preemption) are recorded with `EventTypeNormal`; `Preempted` (the scheduler's event on the victim), `Evicted` and `Unhealthy` are `Warning`. A type filter would silently disable the imminent-death path. This is the only capture path that can still succeed once the pod is deleted outright. Two classes of trigger, treated differently:
  - `Unhealthy` is *speculative* -- probes flap, so debounce it (order of a minute per pod) or a readiness flap becomes a `GetLogs` storm.
  - `Killing` / `Evicted` / `Preempted` / `Preempting` mean *imminent death* -- they bypass the debounce, once per pod. The debounced copy from a probe flap a minute ago is a picture of the process working; the copy taken on `Killing` is the last one anyone will get.
  - The early capture is held in memory keyed by `pod_uid` and is **not** discarded on `DeleteFunc` -- that is exactly the moment it becomes the only copy. The `terminated`/delete capture path consults it only after its own `GetLogs` has failed or returned empty, and writes it as an artifact with `captured_early = 1` only when the pod has an open or recently closed incident or the dead instance's exit code was non-zero (storage doc 6.2). `Killing` fires on every rollout; healthy rolled pods are dropped from the cache.
- Expect `errors.IsBadRequest` / `IsNotFound` when the container hasn't started, has no previous instance, or the pod vanished between the watch event and the log call. Map these to `capture_gap`: `IsNotFound` after a DELETE -> `pod_deleted`; `IsBadRequest` on `Previous: true` with no `LastTerminationState` -> `no_previous_run`; `IsForbidden` -> `forbidden`; a successful call with an empty stream -> `no_output`; anything else -> `unknown`. The middle instance of a `RestartCount` jump gets `unobservable` without a call. These are normal and just mean the artifact row is written with `capture_gap` set and no file.

**Pod manifest (`pod_json`):** no API call. The informer already handed us the whole `*corev1.Pod`; set `TypeMeta` (`Kind: "Pod"`, `APIVersion: "v1"`, which informers leave empty) and `json.Marshal` it to `pod.json`. This is the client-go answer to "there is no `describe`".

### 3.12 `schema_migrations`

Ours entirely.

---

## 4. Summary matrix

| Table | Pod informer | Job informer | RS informer | Event informer | Direct API call | Derived / ours |
|---|:-:|:-:|:-:|:-:|:-:|:-:|
| `clusters` | | | | | `Namespaces().Get("kube-system")`, `rest.Config.Host` | ✓ |
| `watched_namespaces` | | | | | optional `Namespaces().List()` | ✓ |
| `pods` | ✓ | lookup | lookup | | | reconcile |
| `pod_condition_history` | ✓ | | | | | diff |
| `containers` | ✓ | | | | | quantity parsing |
| `container_state_history` | ✓ | | | | | diff, gap detection |
| `incidents` | | | | | | ✓ entirely |
| `jobs` | | ✓ | | | | |
| `rollout_history` | | | ✓ | | | |
| `k8s_events` | | | | ✓ | | classification |
| `artifacts` | (trigger) | | | (trigger) | `Pods().GetLogs()` | `json.Marshal(pod)` |
| `schema_migrations` | | | | | | ✓ |

"lookup" = read from that informer's local store to resolve owners, no API call.

---

## 5. Things to know before writing any of it

- **`UpdateFunc` fires for changes we do not care about** (labels, annotations, `resourceVersion`, `managedFields`) and again for every object on a relist after reconnect. The diff-against-snapshot rule handles this; a naive "insert on every update" does not. This is the single biggest source of garbage rows if forgotten. Resync is off (period 0) so no synthetic updates are added on top.
- **Events are rate-limited by client-go's recorder per object, not per reason** (storage doc Section 4), in every emitting component. An event that "should" be there may have been dropped after a noisy `Unhealthy` burst. No handler waits for or requires an event; events only attach to what pod status already established, with the single exception of `probe`.
- **Informer handlers must not block.** Log capture is a network call; do it on a goroutine/queue, not inside `UpdateFunc`. Everything else (struct walking, SQLite writes through the single writer) is fast enough inline at this scale.
- **Owner resolution can race.** A Pod's `AddFunc` can fire before its ReplicaSet is in the RS informer store. On cold start this is avoided by ordering: start the ReplicaSet and Job informers first, `WaitForCacheSync` them, then start the Pod and Event informers, so no pod handler ever runs against an empty owner store. In steady state a rollout can still deliver the pod a few milliseconds before its ReplicaSet; the lookup misses and the store's `rollout_history` or `jobs` row answers instead when it already exists, otherwise `workload_* = controller_*` is written and the next pod update corrects both the pod row and its open incidents (storage doc 5.3). The reverse case matters more: a Job pruned by its CronJob leaves the informer store while its pods still emit deletion events, so a miss must never overwrite an owner already recorded.
- **`GetLogs` is the only call that needs the Pod to still exist.** Everything else survives the object being gone because informers hand us the final state on delete.
- **Timestamps are `metav1.Time`**, a wrapper around `time.Time`. `.Time.UTC().Format(time.RFC3339)` gives the storage format. Pointers (`*metav1.Time`) are common and nil until the thing happened.
- **The retention sweeper runs on a ticker, not only at startup.** This is a long-lived process; a sweep that runs once at boot lets the store grow past `retention_days` for as long as the process stays up, and the promise in the UI footer is only true for a moment after restart.
- **RBAC failures are `*errors.StatusError`.** `errors.IsForbidden(err)` distinguishes "not allowed" from "network is down", which matters for the `clusters.identity` fallback and for surfacing a useful message when a namespace Role is missing `events` or `replicasets`.

Minimum Role rules per watched namespace: `get/list/watch` on `pods`, `pods/log`, `events`, `jobs` (`batch`), `replicasets` (`apps`). Nothing cluster-scoped.
