# Kubernetes Monitoring -- Data Storage Design

## 1. Purpose and context

This application runs on a **developer's machine**, not inside a cluster. It watches a handful of namespaces in one or more Kubernetes clusters and keeps a short, local record of what happened to Pods and Jobs so the developer can answer "why did this break?" **after** the Kubernetes objects, logs, and events are gone.

It is explicitly *not* a fleet-wide observability system for DevOps. That shapes every decision below:

- Data volume is small (roughly 2 clusters x ~5 namespaces x tens to a few hundred pods).
- Data is short-lived. Everything is deleted after a configurable retention period (default 3 days), whether or not a human looked at it.
- One user, one process, one machine. SQLite is the right database; there is no reason to look further.
- The developer may have **namespace-scoped RBAC only**. The design must not depend on cluster-wide permissions.

Ingest is written in Go using `client-go` informers. The sweeper is a separate loop in the same binary. Both are out of scope here except where they constrain the schema (Section 4).

This document describes **what is stored and why**. How the data is presented is `presentation.md` and is not constrained by anything here.

---

## 2. Storage split

| Store | Holds | Why |
|---|---|---|
| **SQLite** (single file, WAL mode) | All structured state: clusters, pods, containers, transition history, incidents, jobs, rollouts, events | Filterable, joinable, aggregatable. Small enough that even full event bodies fit comfortably. |
| **Files on disk** | Container logs and Pod manifest snapshots | Human-browsable with `cat`/`less`, opened on demand, never queried by content. |

Kubernetes Events live **entirely in SQLite**, including the full message and the raw JSON. An Event is 1-2 KB; a file per event would add filesystem churn and an indirection for no benefit. Logs are also small (a tail of ~50 lines) but stay on disk because browsing them as plain files is a stated goal.

The `artifacts` table links every file to the pod (and incident) it belongs to. No file exists without a row, and the sweeper deletes files *through* that table.

---

## 3. Design principles

1. **Identity is `metadata.uid`, never the name.** Names repeat over time (a Job pod recreated tomorrow gets the same generated name). This applies to Pods, Jobs, ReplicaSets, CronJob owners, and the objects an Event refers to. Names are stored for display only.

2. **Every table carries `cluster_id`.** Two clusters, same namespace, same Deployment name is the normal case, not an edge case.

3. **A cluster's identity is not its kubeconfig context.** Two contexts can point at the same cluster. A cluster is identified by the `uid` of its `kube-system` Namespace (stable for the life of the cluster). If RBAC prevents reading that Namespace, the API server URL is used instead (Section 5.1).

4. **Three separate concepts, three separate places:**
   - **Is the object still there?** -> `pods.deleted_at IS NULL` (likewise `jobs.deleted_at`). Set from the watch DELETE event, or by reconciliation on startup.
   - **Is something wrong right now?** -> an **open incident** in the `incidents` table. Incidents are opened and closed **by the system** based on observed state.
   - **Has a human dealt with it?** -> `incidents.acknowledged_at` / `incidents.dismissed_at`. Informational only. Has no effect on retention.

5. **The incident is the unit of investigation; the pod is the unit of retention.** Logs, manifest snapshots, and transition history attach to a pod and, where relevant, to an incident. When a pod is swept, everything under it goes with it via `ON DELETE CASCADE`.

6. **History is append-only and immutable while it lives.** `container_state_history` and `pod_condition_history` get a new row on every *actual* transition and are never updated. They are still deleted by the sweeper like everything else (Section 8). "Append-only" describes how rows are written, not how long they live. Anything mutable (`closed_at`, flags) lives on `incidents` or the snapshot tables.

7. **Store the raw Kubernetes reason and message alongside the derived classification.** Classification rules (`category`) will change as we learn. Because the raw fields are always kept, old rows can be re-classified at any time.

8. **Store Kubernetes' own timestamps, not only ours.** `observed_at` says when *we* saw something. `lastState.terminated.finishedAt` says when it *happened*. If the laptop was asleep for an hour, only the second one is true.

9. **Retention is time-based and applies to everything that is not alive.** There is no "unresolved stays forever". A developer's laptop is not an audit log. The user can delete sooner; nothing survives longer than the retention window except objects that still exist in the cluster.

---

## 4. Ingest contract (what the schema assumes about the watcher)

These are the behaviours the schema is designed around. If the ingest side does something different, the schema fills with garbage, so they are written down here even though ingest is out of scope.

**Watched resources per (cluster, namespace):** Pods, Jobs, Events, ReplicaSets. All are namespace-scoped, so namespace-only RBAC is sufficient. CronJobs and Deployments are *not* watched: the Job -> CronJob and ReplicaSet -> Deployment relationships come from `ownerReferences`, which include the owner's `uid` and `name`.

**Watches deliver whole objects, not diffs.** A Pod watch event fires when *anything* on the Pod changes (labels, annotations, conditions, a status field). The ingester must diff the incoming object against the `containers` / `pods` snapshot and only insert a history row when the observed state actually changed. Without this, history tables bloat with identical rows and every flapping query breaks.

**Transition key for containers:** a history row is inserted when any of `(state, reason, exit_code, restart_count, image_id, container_id)` differs from the snapshot.

**Transition key for the pod itself:** `(phase, status_reason)`. A change opens or attaches pod-level incidents (`Evicted`) but has no history table of its own; conditions are the pod-level history and have their own key (Section 5.4).

**First sight is decided by the snapshot.** When an object arrives (initial list, relist after a reconnect, or a normal update) the ingester compares it against the `pods` / `containers` rows for that uid:

- No row exists: record the snapshot. No history row. A pod that is already in `CrashLoopBackOff` when the app first sees it is a *state*, not a transition, so nothing is appended to the history tables. But a state can still be a problem: if the snapshot itself matches a category in Section 6.1 (a waiting reason in the table, `PodScheduled=False` with `Unschedulable`, `status.reason = Evicted`, a Job with condition `Failed`), an incident is opened for it with `occurrences = 1` and `opened_at` taken from the Kubernetes timestamp (`lastState.terminated.finishedAt`, the condition's `lastTransitionTime`, or `metadata.creationTimestamp` as a last resort). "Is something wrong right now?" must be answerable the moment the tool starts, not only after the next flap. A pod stuck in `CreateContainerConfigError` never flaps.
- A row exists and the transition key differs: this is a real transition, including when it is the first event after the app restarted. It gets a history row and opens or attaches an incident. This is how the tool sees what broke while the laptop was asleep.

**Missed transitions are reconstructed, not ignored.** If `restart_count` jumped by more than 1 since the last snapshot (watch was down, laptop slept), the ingester inserts one history row for the observed `lastState.terminated` with `gap_reconstructed = 1`; it does not pretend the intermediate restarts didn't happen.

**Reconciliation on startup:** any pod/job in the DB with `deleted_at IS NULL` that is absent from the informer store after the initial list completes is marked deleted with `deleted_at = now` and `deletion_source = 'reconcile'`. Without this, pods that died while the app was closed stay "alive" forever and are never swept. The real deletion time is unknown, so such a row is kept for `retention_days` counted from the reconcile, not from the moment the object actually vanished; a pod that died on day one of a five-day absence therefore lives longer than the window. This is accepted and is the one exception to the retention promise in Section 8. A pod whose namespace was removed from `watched_namespaces` is also absent from every store; it is marked with `deletion_source = 'unwatched'`, because "we stopped looking" is not "it is gone". This is a startup-only pass: after a watch reconnect the informer relists on its own and emits DELETE events for objects that vanished. Informer resync is not used (period 0); it replays the local cache and discovers nothing.

**Log capture happens while the pod still exists.** Logs of a dead container instance are only retrievable while the Pod object exists. Capture is triggered per container when `restart_count` increased, `lastState.terminated.finishedAt` changed, or `state` became `terminated` since the snapshot. The trigger is the counter and the `lastState` timestamp, not `state = terminated` alone: in a crash loop the kubelet usually reports `running -> waiting(CrashLoopBackOff)` directly and the `terminated` state never appears in any watch event.

Two facts about the kubelet decide how the dead instance is addressed:

- **Where the dead instance lives decides the `previous` flag.** When the container restarted (`restartPolicy: Always` / `OnFailure`), the dead instance is `lastState.terminated` and its log is read with `previous = true`. When the container will not restart (`restartPolicy: Never`, the Job case), the dead instance *is* `state.terminated`, there is no previous instance, and its log is read with `previous = false`. Both are stored as `kind = log_previous`: the kind describes "the log of a dead instance", not the API flag used to fetch it.
- **`restart_count` is the label of the most recently created container.** In `waiting(CrashLoopBackOff)` it has not yet been bumped for the next attempt; once the next instance is `running` it has. So the **dead-instance index** used for `artifacts.restart_count` and the `restart_NNN.log` file name is `restart_count` when the current state is `waiting` or `terminated`, and `restart_count - 1` when it is `running`. The first instance to die is index 0 either way.

`previous = true` only returns the most recent dead instance, so when two restarts happened between watch events one log is captured and the middle instance gets an `artifacts` row with `capture_gap = 'unobservable'` and no API call is made for it. Log size limits (line tail, default 50, and a byte cap) are **enforced by the ingester**; storage only records the resulting `size_bytes` and whether the capture was `truncated`.

**Events are incomplete by construction.** Every component that emits Events (kubelet, scheduler, controllers) does so through client-go's `EventCorrelator`, which rate-limits per object with a token bucket (burst 25, refill one per 5 minutes) keyed on source and involved object, not on reason. A container flapping its readiness probe emits `Unhealthy` 25 times, drains the bucket, and the `BackOff` or `Failed` event that follows on the same pod is dropped silently. The same correlator also aggregates: after 10 similar events in 10 minutes the message is rewritten to `(combined from similar events)`. Events get the same only-on-change discipline as the object snapshots: a delivery whose `count` and `last_ts` are not past the stored row is a relist replaying an unchanged Event object, and it does nothing -- no upsert, no incident, no capture. Without this, every watch reconnect re-counts the same occurrences. `k8s_events` is evidence that explains. It opens an incident in exactly one case, `probe` (Section 6.1), because probe failures leave no trace on the Pod object other than `ready = false`. No rule may take the form "no event, therefore nothing happened"; container status on the watched Pod object is the authority.

**Pod manifests are captured as JSON.** `client-go` has no `describe`; `kubectl describe` is client-side formatting of the Pod object plus its Events. Storing the Pod JSON (artifact kind `pod_json`) alongside the Events in `k8s_events` preserves the same information.

---

## 5. Schema

Conventions used throughout:

- **Timestamps** are stored as UTC RFC 3339 text in **one fixed-width layout with six fractional digits**, Go layout `2006-01-02T15:04:05.000000Z`, e.g. `2026-08-26T14:03:11.482913Z` or `2026-08-26T14:03:11.000000Z`. Every timestamp column, whether the source had microseconds (`MicroTime`), whole seconds (`Time`), or is our own clock, is written in this layout. Fixed width is what makes lexicographic order equal chronological order; a column mixing `...11Z` and `...11.482913Z` sorts the later value first because `.` < `Z`. The layout is readable in any SQLite browser and Go's `time.Time` round-trips it without ambiguity. Rows written in the same microsecond are ordered by `(timestamp, id)`.
- **Booleans** are `INTEGER` 0/1.
- **Foreign keys** are enforced (`PRAGMA foreign_keys = ON`). Every child of `clusters`, `pods`, `jobs`, and `incidents` declares `ON DELETE CASCADE` (or `ON DELETE SET NULL` where noted), so deleting a parent row removes or detaches everything beneath it in one statement. Removing a cluster is one `DELETE FROM clusters` plus deleting its artifact directory.
- **Tables are `STRICT`** (SQLite 3.37+). A Go bug that writes a string into an INTEGER column fails loudly at insert time instead of silently storing text.
- `id INTEGER PRIMARY KEY` is SQLite's rowid alias; used where there is no natural key.
- **NULL and uniqueness:** SQLite treats every NULL as distinct in a UNIQUE index. Any column that takes part in a uniqueness rule and can be "absent" stores `''`, never NULL, and is declared `NOT NULL DEFAULT ''`. This applies to `incidents.container_name` and `artifacts.container_name`.

### 5.1 `clusters`

One row per distinct cluster ever connected.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| identity | TEXT UNIQUE, nullable | `uid` of the `kube-system` Namespace. Null if RBAC prevented reading it. |
| name | TEXT | Friendly name, user-editable |
| context_name | TEXT | Last kubeconfig context used to reach it |
| api_server_url | TEXT | |
| first_seen_at | TEXT | |
| last_connected_at | TEXT | |
| last_error | TEXT | nullable. Most recent connection or RBAC failure for this cluster, in words a user can act on (`forbidden: list events in namespace payments`). Cleared on the next successful sync. |
| last_error_at | TEXT | nullable |
| grafana_url | TEXT NOT NULL DEFAULT '' | Base URL of a Grafana instance with Explore access to this cluster's logs. Empty means unconfigured. |
| loki_datasource_uid | TEXT NOT NULL DEFAULT '' | The Loki datasource's uid in that Grafana instance. |
| log_selector | TEXT NOT NULL DEFAULT '' | A LogQL selector template. Placeholders `$namespace`, `$pod`, `$container`, `$workload`, `$node` and `$cluster` are substituted from the row a link is built for (`$cluster` is this row's `name`). |

`last_error` exists so a cluster that cannot be reached is visibly broken rather than silently stale.

A cluster is configured for Grafana when `grafana_url` is non-empty; clearing it clears the feature. The three columns travel together: the store setter and the write endpoint always set or clear all three at once.

**Matching a connection to a row:** if the `kube-system` uid is readable, match on `identity`. Otherwise match on `api_server_url`. Consequence of the fallback: the same cluster reached through two different URLs (VPN vs. public endpoint) appears as two cluster rows. This is accepted; it is visible and harmless, and the user can rename either.

### 5.2 `watched_namespaces`

Configuration, not observation. Which namespaces the app watches in each cluster. Populated by the user (typed in, or picked from a list pulled from the cluster when RBAC allows), editable later.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| cluster_id | INTEGER FK -> clusters (cascade) | |
| name | TEXT | |
| added_at | TEXT | |

`UNIQUE (cluster_id, name)`.

`clusters` plus `watched_namespaces` is the single source of truth for what the process watches. There is no cluster list in a config file; the process reads these tables at startup and re-reads them when the user edits them. The config file holds only process-level settings (Section 11).

There is no separate namespaces dimension table. Observation tables store `namespace` as plain text; that is the right denormalisation for a name that is also the natural query key.

### 5.3 `pods`

One row per Pod object ever observed. Identity columns are written once; status columns are overwritten in place.

| Column | Type | Notes |
|---|---|---|
| uid | TEXT PK | `metadata.uid` |
| cluster_id | INTEGER FK -> clusters | |
| namespace | TEXT | |
| name | TEXT | display only |
| node_name | TEXT | nullable until scheduled |
| phase | TEXT | Pending / Running / Succeeded / Failed / Unknown |
| status_reason | TEXT | nullable, `status.reason`: `Evicted` / `Preempting` / `NodeLost` / `Shutdown` / `DeadlineExceeded` ... The raw source for the pod-level `node_pressure` category. |
| status_message | TEXT | nullable, `status.message` (e.g. `The node was low on resource: memory.`) |
| deletion_requested_at | TEXT | nullable, the **first** `metadata.deletionTimestamp` observed. Set while the pod is Terminating; the moment termination began, and the only way to explain a pod stuck in Terminating for twenty minutes. The API server rewrites the field when a later delete arrives with a shorter grace period, and the kubelet's final delete does exactly that, so the last object seen carries the moment of removal; `ingest.DiffPod` keeps the value the snapshot already holds. |
| qos_class | TEXT | Guaranteed / Burstable / BestEffort -- context for OOM and eviction |
| controller_kind | TEXT | direct owner: ReplicaSet / Job / StatefulSet / DaemonSet / none |
| controller_name | TEXT | |
| controller_uid | TEXT | join key to `jobs.uid` or `rollout_history.replicaset_uid` |
| workload_kind | TEXT | top-level owner: Deployment / StatefulSet / DaemonSet / CronJob / Job / none |
| workload_name | TEXT | **the stable identity for "which service fails most"** |
| created_at | TEXT | `metadata.creationTimestamp` |
| started_at | TEXT | `status.startTime`, nullable |
| first_seen_at | TEXT | when we first observed it |
| last_seen_at | TEXT | updated on every watch event |
| deleted_at | TEXT | null while the object exists |
| deletion_source | TEXT | `watch` (DELETE event), `reconcile` (absent from the store at startup), `unwatched` (its namespace was removed from `watched_namespaces`); nullable |
| deletion_reason | TEXT | nullable, **inferred** at delete time: `evicted` / `rollout` / `replaced` / `scaled_down` / `job_pruned` / `unknown`. See below. |

Indexes: `(cluster_id, deleted_at)`, `(cluster_id, namespace, name)`, `(cluster_id, workload_kind, workload_name)`, `(deleted_at)` for the sweeper.

There is no separate `is_alive` flag: it is exactly `deleted_at IS NULL`, and two columns that must agree is one invariant too many.

`deletion_reason` is computed once, when the delete is observed, from data that is local at that moment and swept on the same clock as the pod itself. It is never derived at read time. Inputs, in order: a `DisruptionTarget=True` condition on the pod -> `evicted`, checked first and alone, because a pod told to leave by the eviction API or the kubelet was not replaced by a rollout even when one also holds; `controller_kind = Job` -> `job_pruned`; a ReplicaSet in `rollout_history` with the same `deployment_uid` and a higher `revision` than the pod's own ReplicaSet -> `rollout` (revision, not `first_seen_at`: a rollback reuses an old ReplicaSet row and bumps its revision); a live sibling (same `controller_uid`, `deleted_at IS NULL`) with a newer `created_at` -> `replaced`; live siblings all older -> `scaled_down`; otherwise `unknown`. Sibling *count* is not an input: on a 3-replica Deployment every deletion leaves two siblings, so count alone cannot tell a replacement from a scale-down. The value is labelled as inferred wherever it is shown.

`workload_*` exists because neither pod name (rotates) nor image (changes every deploy) nor ReplicaSet name (changes every deploy) is a stable identity for a service. Resolving it: Pod -> owner ReplicaSet (from `ownerReferences`) -> the ReplicaSet's own `ownerReferences` gives the Deployment. ReplicaSets are watched, so this lookup is local. For Job pods: Pod -> Job -> CronJob (from the Job's `ownerReferences`). If the informer store cannot resolve the chain, the `jobs` and `rollout_history` rows are asked next; they keep `cronjob_name` and `deployment_name` after the Job or ReplicaSet has been pruned, which is exactly when a pod's last events arrive. Only when neither knows the owner (custom controllers, an owner never seen) is `workload_*` = `controller_*` written, and never over an owner already recorded for the same controller. When a later update resolves the chain, the pod row is corrected and so are the `workload_*` columns of every open incident on that pod, in the same transaction; an incident must never keep a ReplicaSet name where a Deployment name belongs, or "which service fails most" splits one service into one row per rollout.

### 5.4 `pod_condition_history`

Append-only. Covers the Pending / Unschedulable case and Ready flapping.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| pod_uid | TEXT FK -> pods (cascade) | |
| type | TEXT | PodScheduled / Initialized / ContainersReady / Ready / DisruptionTarget |
| status | TEXT | True / False / Unknown |
| reason | TEXT | e.g. `Unschedulable` |
| message | TEXT | e.g. `0/3 nodes are available: insufficient cpu` |
| k8s_transition_at | TEXT | `lastTransitionTime` from the condition |
| observed_at | TEXT | |

Insert only when `(type, status, reason)` changes for that pod. `message` is deliberately not in the key: the `Unschedulable` message changes every time the node count changes (`0/3 nodes` -> `0/4 nodes`) and would flood the table; the latest wording is kept on the incident's `last_message`. Index `(pod_uid, observed_at)`, `(observed_at)` for the sweeper.

### 5.5 `containers`

Current snapshot. One row per container per pod, including init and ephemeral containers. Overwritten in place.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| pod_uid | TEXT FK -> pods (cascade) | |
| name | TEXT | |
| kind | TEXT | `init` / `sidecar` / `app` / `ephemeral`. `sidecar` is an init container with `restartPolicy: Always` (Kubernetes 1.28+); it runs for the life of the pod and behaves like an app container. |
| image | TEXT | as written in spec |
| image_tag | TEXT | parsed from `image`; null for digest-pinned images |
| image_id | TEXT | resolved digest from `status.imageID` -- catches "same tag, different image" |
| container_id | TEXT | runtime id from `status.containerID`; changes on every restart |
| cpu_request / cpu_limit | TEXT | as written (`500m`) |
| mem_request / mem_limit | TEXT | as written (`512Mi`) |
| cpu_request_millis / cpu_limit_millis | INTEGER | parsed, nullable -- so SQL can compare |
| mem_request_bytes / mem_limit_bytes | INTEGER | parsed, nullable |
| state | TEXT | waiting / running / terminated |
| reason | TEXT | from the current state (`CrashLoopBackOff`, `OOMKilled`, ...) |
| message | TEXT | nullable, `state.terminated.message` -- the kubelet's reading of the container's termination log. Capped at 4096 bytes at ingest, the same cap the kubelet applies per container, because it rides `incidents.last_message`. Only a terminated state has one. |
| exit_code | INTEGER | nullable |
| signal | INTEGER | nullable, `state.terminated.signal` -- not reliably derivable from `exit_code` alone |
| ready | INTEGER | |
| restart_count | INTEGER | |
| running_since | TEXT | `state.running.startedAt`, nullable |
| last_terminated_reason | TEXT | from `lastState.terminated` |
| last_terminated_exit_code | INTEGER | |
| last_terminated_signal | INTEGER | nullable, `lastState.terminated.signal` |
| last_terminated_at | TEXT | `lastState.terminated.finishedAt` |
| updated_at | TEXT | |

`UNIQUE (pod_uid, name)` -- container names are unique within a pod across init/app/ephemeral, so this is the natural upsert key.

**What `ready` means depends on `kind`.** The kubelet sets it differently per container class (`pkg/kubelet/prober/prober_manager.go`, `UpdatePodStatus`):

- `app` and `sidecar`: `ready` follows the readiness probe while the container is running; false whenever it is not running. With no probe it is true once started.
- `init`: `ready` is never probe-based. It is true exactly when the container terminated with exit code 0, false otherwise, including while it is running. For an init container `ready = 1` means "finished successfully", not "serving".
- `ephemeral`: no readiness; stored as observed.

Every rule that reads `ready` (the `recovered` close in Section 6.3, any "is this pod healthy" query) must read `kind` next to it. `ready = 1 AND state = 'terminated'` is normal for `init` and impossible for `app`.

### 5.6 `container_state_history`

**Append-only.** The pattern-detection table: flapping, repeated OOM, restart storms.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| pod_uid | TEXT FK -> pods (cascade) | |
| container_name | TEXT | |
| incident_id | INTEGER FK -> incidents (set null on delete), nullable | which incident this transition belongs to, if any |
| image | TEXT | at time of transition |
| image_id | TEXT | |
| container_id | TEXT | |
| state | TEXT | |
| reason | TEXT | |
| message | TEXT | nullable, `terminated.message`, capped as in Section 5.5; a reconstructed row carries the message of the instance it reconstructs |
| exit_code | INTEGER | nullable |
| signal | INTEGER | nullable, `terminated.signal` |
| restart_count | INTEGER | |
| category | TEXT | derived -- see Section 6 |
| k8s_started_at | TEXT | `terminated.startedAt` / `running.startedAt`, nullable |
| k8s_finished_at | TEXT | `terminated.finishedAt`, nullable |
| observed_at | TEXT | |
| gap_reconstructed | INTEGER | 1 if this row was inferred from a `restart_count` jump rather than directly observed |

Indexes: `(pod_uid, observed_at)`, `(incident_id)`, `(observed_at)` for the sweeper.

Nothing on this table is ever updated. Resolution state belongs to `incidents`: a crash loop is one incident spanning many transitions, and a per-row flag would make the retention rules unimplementable.

### 5.7 `incidents`

One row per *problem*, spanning many transitions and events. This is the row a person acknowledges, dismisses, annotates, or deletes, and the row logs attach to.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| cluster_id | INTEGER FK -> clusters | |
| namespace | TEXT | |
| subject_kind | TEXT | `pod` or `job` |
| pod_uid | TEXT FK -> pods (cascade), nullable | set when `subject_kind = pod` |
| job_uid | TEXT, nullable | always set when `subject_kind = job`; also set on a pod incident whose pod has `controller_kind = 'Job'`, copied from the pod's `controller_uid` at open time, so a Job's own incident and the incidents of its pods can be listed together after the pods are pruned. No reference to `jobs`: the two informers deliver in no order, so the `jobs` row may arrive after the incident opens and may be swept while the incident lives on. A pod incident's job uid is a link, never its subject: what loads, closes and matches events for a subject is `subject_kind` together with that kind's uid column. |
| container_name | TEXT | `NOT NULL DEFAULT ''` -- pod-level incidents (Unschedulable, Evicted) store `''` so the uniqueness rule below holds |
| workload_kind / workload_name | TEXT | copied from the pod (or job) at open time so incidents group by service without a join; kept in sync when the pod's own `workload_*` is corrected from a fallback value (Section 5.3) |
| category | TEXT | see Section 6 |
| first_reason | TEXT | raw Kubernetes reason that opened it |
| last_reason | TEXT | most recent raw reason |
| last_message | TEXT | most recent raw message (from a condition, an event, or a terminated container's `message`) |
| image / image_tag / image_id | TEXT | at open time -- "which version broke" |
| node_name | TEXT, nullable | the pod's node at open time -- "which machine broke", answered without a join, so one node's storm of failures is one query. Filled from the pod row when the incident opens and never back-filled: a scheduling incident opens before placement and a job incident has no pod, so both stay NULL and the filter finds placed pods only. |
| occurrences | INTEGER | number of transitions/events attached |
| opened_at | TEXT | Kubernetes time of the first transition, when known; else observed |
| last_seen_at | TEXT | |
| closed_at | TEXT | null while open |
| close_reason | TEXT | `recovered` / `pod_deleted` / `job_finished` / `manual` |
| acknowledged_at | TEXT | human: "I've seen this". Informational. |
| dismissed_at | TEXT | human: soft delete. Informational; swept on the normal schedule. |
| note | TEXT | free text the user can leave ("fixed in PR 123") |

Constraints: `CHECK ((subject_kind='pod') = (pod_uid IS NOT NULL))`, and one way only for job: `CHECK (subject_kind <> 'job' OR job_uid IS NOT NULL)`.

Two partial unique indexes, one per subject kind, are the dedup mechanism:

- `UNIQUE (pod_uid, container_name, category) WHERE closed_at IS NULL AND subject_kind = 'pod'` -- at most one *open* incident per (pod, container, category). `container_name` is `''` for pod-level incidents, so they are covered too.
- `UNIQUE (job_uid, category) WHERE closed_at IS NULL AND subject_kind = 'job'`.

New matching transitions attach to the open incident and bump `occurrences`; they do not open a second one. The indexes only cover *open* rows, so a plain insert never conflicts with a closed incident and would silently create a second story. The open path is therefore three steps in one transaction (Section 6.2): look for an open incident with the key and attach; else look for the most recent *closed* incident with the key whose `close_reason` is not `pod_deleted` and reopen it; else `INSERT ... ON CONFLICT DO UPDATE` against the partial index, which makes the insert itself idempotent under replay.

Indexes: `(cluster_id, closed_at)`, `(workload_kind, workload_name, opened_at)`, `(closed_at)` for the sweeper, `(pod_uid, closed_at)` for the reopen lookup and so the `pods` cascade does not scan, and `(job_uid, closed_at)` for the reopen lookup and the related-incidents query.

### 5.8 `jobs`

Current snapshot of each Job.

| Column | Type | Notes |
|---|---|---|
| uid | TEXT PK | |
| cluster_id | INTEGER FK -> clusters | |
| namespace | TEXT | |
| name | TEXT | |
| cronjob_uid | TEXT | from `ownerReferences`, nullable |
| cronjob_name | TEXT | nullable |
| active | INTEGER | `status.active` |
| succeeded | INTEGER | `status.succeeded` |
| failed | INTEGER | `status.failed` (counts failed *pods*; with `restartPolicy: OnFailure` retries are container restarts and do not increment this) |
| backoff_limit | INTEGER | |
| completions | INTEGER | nullable |
| parallelism | INTEGER | nullable |
| active_deadline_seconds | INTEGER | nullable, `spec.activeDeadlineSeconds` -- a `DeadlineExceeded` failure is unreadable without the deadline it names |
| restart_policy | TEXT | `Never` / `OnFailure` -- needed to interpret `failed` correctly |
| condition_type | TEXT | `Complete` / `Failed` / `Suspended` / null while running |
| condition_reason | TEXT | e.g. `BackoffLimitExceeded`, `DeadlineExceeded` |
| condition_message | TEXT | |
| created_at | TEXT | `metadata.creationTimestamp` |
| started_at | TEXT | `status.startTime` |
| finished_at | TEXT | `status.completionTime`, or the `Failed` condition's transition time |
| first_seen_at / last_seen_at | TEXT | |
| deleted_at | TEXT | nullable |

Indexes: `(cluster_id, namespace, cronjob_uid, started_at)`, `(deleted_at, finished_at)` for the sweeper.

Job success/failure is read from `condition_type` / `condition_reason`, **not** from counter arithmetic: `failed` vs `backoff_limit` misreads terminal failures (`failed` can exceed the limit) and says nothing for `OnFailure` jobs.

### 5.9 `rollout_history`

Which image each Deployment revision ran. One row per (ReplicaSet, container) so multi-container pods are handled.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| cluster_id | INTEGER FK -> clusters | |
| namespace | TEXT | |
| deployment_name | TEXT | from the ReplicaSet's `ownerReferences` |
| deployment_uid | TEXT | |
| replicaset_uid | TEXT | |
| replicaset_name | TEXT | |
| container_name | TEXT | |
| image / image_tag | TEXT | from the RS pod template |
| revision | INTEGER | `deployment.kubernetes.io/revision` annotation -- **updated in place**: a rollback re-uses the old RS and bumps its revision |
| created_at | TEXT NOT NULL | the ReplicaSet's own `creationTimestamp` -- when this revision shipped, not when the daemon connected |
| replicas | INTEGER | nullable, `spec.replicas` is optional on the object |
| ready_replicas | INTEGER | nullable, absent on a ReplicaSet its controller has never observed |
| available_replicas | INTEGER | nullable, same as `ready_replicas` |
| first_seen_at | TEXT | |
| last_seen_at | TEXT | |
| deleted_at | TEXT | nullable |

`UNIQUE (replicaset_uid, container_name)`. Index `(cluster_id, namespace, deployment_name, first_seen_at)`, `(deleted_at, last_seen_at)` for the sweeper. `created_at` and the three replica counts are written on insert and updated on every event, because a scaling ReplicaSet's counts change while its creation time does not; the three counts are the only mutable facts, the rest of a row is set once.

### 5.10 `k8s_events`

Every Event in the watched namespaces, stored whole. Events expire in the cluster after one hour by default, so this table is often the only record.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| cluster_id | INTEGER FK -> clusters | |
| event_uid | TEXT | `metadata.uid` of the Event object -- upsert key, so `count`/`last_ts` updates land on the same row |
| namespace | TEXT | |
| type | TEXT | `Normal` / `Warning` |
| involved_kind | TEXT | Pod / Job / ReplicaSet / ... |
| involved_name | TEXT | display |
| involved_uid | TEXT | **the join key** |
| field_path | TEXT | e.g. `spec.containers{api}` -- container-level detail |
| reason | TEXT | `BackOff`, `Unhealthy`, `FailedScheduling`, `Killing`, ... |
| message | TEXT | full, untrimmed |
| source_component | TEXT | kubelet / default-scheduler / ... |
| count | INTEGER | Kubernetes' own dedup counter |
| first_ts | TEXT | |
| last_ts | TEXT | |
| category | TEXT | derived, nullable -- most Normal events get none |
| incident_id | INTEGER FK -> incidents (set null on delete), nullable | attached when the event clearly belongs to an open incident (same pod, matching category/container) |
| raw_json | TEXT | full Event object, **always stored**. Insurance against a mapping bug while the field mapping is young; a few MB at this scale and swept with everything else. |

`UNIQUE (cluster_id, event_uid)`. Indexes `(involved_uid, last_ts)`, `(cluster_id, namespace, last_ts)`, `(last_ts)` for the sweeper.

The `core/v1` Events API is watched (`involvedObject`, `message`, `count`). If the app later moves to `events.k8s.io/v1`, the mapping is `regarding` -> involved, `note` -> message, `series.count`/`deprecatedCount` -> count. Decide once, map in one place.

**Timestamp precedence for `last_ts` and `first_ts`.** An Event carries up to four timestamps of two precisions. `eventTime` and `series.lastObservedTime` are `MicroTime` (microseconds); `firstTimestamp` and `lastTimestamp` are `Time` (whole seconds). Newer components fill the first pair and leave the second empty or zero. A pod's whole story (scheduled, pulling, failed, backing off) often happens inside one minute, so second precision collapses it into identical timestamps. Order used, first non-zero wins:

- `last_ts`: `series.lastObservedTime` -> `eventTime` -> `lastTimestamp` -> `firstTimestamp` -> `metadata.creationTimestamp`
- `first_ts`: `eventTime` -> `firstTimestamp` -> `metadata.creationTimestamp`
- `count`: `series.count` -> `count` -> 1

Stored in the fixed-width layout from Section 5 (`2026-08-26T14:03:11.482913Z`, or `.000000Z` when the source had whole seconds), so a column mixing both sources still sorts chronologically.

**Event classification.** `category` is derived from `reason` alone (never from `message`):

| Event reason | category |
|---|---|
| `FailedScheduling` | `scheduling` |
| `Failed`, `ErrImagePull`, `ImagePullBackOff`, `InvalidImageName` (kubelet, `spec.containers{...}` field path) | `image_pull` |
| `CreateContainerConfigError`, `CreateContainerError` | `config` |
| `Unhealthy` | `probe` |
| `OOMKilling` | `oom` |
| `Evicted` | `node_pressure` |
| `BackOff` | **ambiguous**: `Back-off pulling image` and `Back-off restarting failed container` share the reason and differ only in message. Stored with `category = NULL`. |
| `Killing`, `Preempted`, `Preempting`, `Pulling`, `Pulled`, `Scheduled`, `Started`, `Created`, others | `NULL` |

**Attach rule.** An event is attached (`incident_id` set) to an incident when: same `involved_uid` as the incident's pod or job; the container parsed from `field_path` (`spec.containers{api}` / `spec.initContainers{init-db}`) equals `incidents.container_name`, or the incident is pod-level (`''`); and the event's `category` equals the incident's, or the event's `category` is `NULL`. If several open incidents qualify, the one with the latest `last_seen_at` wins. This is applied when the event arrives, when an incident opens (to unattached events with `last_ts >= opened_at - stabilization_window`, because the `Failed` pull event routinely lands before the status update that opens the incident), and by the closer for incidents closed within the last `stabilization_window`.

Events reference pods by `involved_uid` rather than a foreign key, because Events also arrive for objects we do not store (ReplicaSets, Nodes, custom resources) and can arrive before the Pod itself. They are swept by age (Section 8), not by cascade.

### 5.11 `artifacts`

Every file on disk, and what it belongs to.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| pod_uid | TEXT FK -> pods (cascade) | all artifacts are pod-scoped |
| incident_id | INTEGER FK -> incidents (set null on delete), nullable | |
| container_name | TEXT | `NOT NULL DEFAULT ''`; `''` for `pod_json` |
| kind | TEXT | `log_previous` / `log_current` / `pod_json` |
| restart_count | INTEGER | `NOT NULL`. For `log_previous`: the dead-instance index (Section 4). `-1` for `pod_json` and `log_current`. |
| file_path | TEXT | relative to the artifacts root, so the root can move |
| size_bytes | INTEGER | |
| truncated | INTEGER | 1 if the ingester's line/byte limit cut the capture. Known exactly: the ingester asks for `log_tail_lines + 1` lines and drops the extra one when it arrives. |
| captured_early | INTEGER | 1 if captured on a precursor event (`Killing`/`Preempted`/`Preempting`/`Evicted`/`Unhealthy`, matched on reason regardless of event type -- `Killing` and `Preempting` are `Normal` events) ahead of a `terminated` transition or deletion -- this may be the only copy that ever exists |
| capture_gap | TEXT | nullable -- set when there is no log: `pod_deleted` / `no_previous_run` / `forbidden` / `no_output` / `kubelet_error` / `unknown` mean "we tried and got nothing"; `unobservable` means "no call could ever have worked" (the middle instance of a multi-restart gap, Section 4). |
| capture_note | TEXT | nullable -- the API's own words when `capture_gap` is set (the error message, or the one-line body described below). Shown as a labelled quotation, never as log content. |
| captured_at | TEXT | |

`UNIQUE (pod_uid, container_name, kind, restart_count)`; `restart_count` is `-1` for `pod_json` and `log_current` so the index has no NULL. Index `(incident_id)` so the `SET NULL` cascade from `incidents` does not scan.

**The log endpoint can fail with HTTP 200.** When the kubelet cannot serve a log (container never started, node gone), the API server sometimes returns status 200 with a one-line body that is an error message, such as `unable to retrieve container logs for containerd://...` or `failed to try resolving symlinks in path ...`. Stored blindly, that line becomes the container's "last output". The capture worker treats a body that is a single line starting with a known kubelet error prefix as a failed capture: no file, `capture_gap = 'kubelet_error'`, `capture_note` = the line.

Every artifact has a real pod FK. Job artifacts are their pods' artifacts; events have no files. So a polymorphic reference is unnecessary and the cascade is enforceable.

### 5.12 `sweep_runs`

One row per table per sweep pass. Written by the sweeper, read by nothing yet.

| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| ran_at | TEXT | |
| cutoff | TEXT | the `now - retention_days` used |
| table_name | TEXT | `pods` / `incidents` / `k8s_events` / ... / `orphan_files` / `orphan_rows` |
| rows_removed | INTEGER | |
| files_removed | INTEGER | |
| bytes_removed | INTEGER | file bytes |
| duration_ms | INTEGER | |
| error | TEXT | nullable -- why the pass stopped early, if it did |

Why store it: a history that silently shortens is worse than one that says it was shortened. An empty stretch before 03:00 reads as a quiet night unless something records that 400 rows from that stretch were removed at 03:00. Rows here age out with `ran_at < cutoff` like everything else.

### 5.13 `schema_migrations`

| Column | Type |
|---|---|
| version | INTEGER PK |
| applied_at | TEXT |

One row per migration applied. Trivial, and the reason this document can change without anyone hand-editing a database.

---

## 6. Incident lifecycle

This is the logic that turns raw transitions into something a person wants to look at. It is deliberately small.

### 6.1 Categories

Derived from the raw Kubernetes **reason**, for an event the `source_component` that reported it, and whether the pod is terminating (`pods.deletion_requested_at` set): never from `phase` (`CrashLoopBackOff` reports `Running`), never from `exit_code` (137 is OOM or a grace-period kill), never from `message` (free prose). A test in `internal/incident` enforces this. The raw reason is always stored, so this mapping can change at any time without data loss.

| category | Opened by |
|---|---|
| `oom` | container terminated (`state` or `lastState`) with reason `OOMKilled` |
| `crash` | container terminated with reason `Error` (the kubelet's reason for any non-zero exit that is not OOM) on a pod that is not terminating, or waiting with `CrashLoopBackOff` |
| `unclean_exit` | container terminated with reason `Error` while the pod's `deletion_requested_at` is set: the app that dies on SIGTERM instead of draining (exit 1) and the process that outlives its grace period and is killed (exit 137) alike, one row per container, without reading the exit code; the history row and the row's own sentence carry the code and the signal, so the two shapes stay distinguishable. It opens only when the pod has no open incident at all, of any category on any container: a pod-level `rescheduled` from a preemption or a taint means the pod died because of the node, and a container that was already failing owns its own ending. When the same container has an open incident the termination attaches to it as an occurrence and the history row takes that incident's category, so a history row and the incident it names never disagree; when another part of the pod has the open incident the history row says `unclean_exit` with no incident. Known limitation: a crash-looping container that also mishandles SIGTERM never has the SIGTERM defect named; its death is the end of the crash. `OOMKilled` on a terminating pod is still `oom`: the limit is still the limit. |
| `image_pull` | waiting with `ErrImagePull` / `ImagePullBackOff` / `InvalidImageName` |
| `config` | waiting with `CreateContainerConfigError` / `CreateContainerError` (missing Secret/ConfigMap), or terminated with `ContainerCannotRun` / `StartError` (bad command or entrypoint) |
| `probe` | Event reason `Unhealthy` on the container, **and** the container's snapshot row has `ready = 0` at that moment, **and** the pod is not deleted, **and** the pod is not terminating (`deletion_requested_at` unset): readiness failing on a container that is being killed says nothing. The ready check is the gate: `Unhealthy` fires for readiness, liveness and startup probes alike, including the routine readiness misses during every rollout, and a single event on a container that is ready again is noise. The deleted check exists because a deleted pod's containers stay `ready = 0` forever and its `pod_deleted` close is final, so an `Unhealthy` delivery trailing the delete would open a fresh incident no close path can reach; it only attaches instead. This is the only category opened by an Event. A liveness kill is not a separate rule; it restarts the container and surfaces as `crash`. |
| `scheduling` | PodScheduled condition `False`, reason `Unschedulable` (not `SchedulingGated`, which is intentional) |
| `node_pressure` | `pods.status_reason = Evicted` (written only by the kubelet), `DisruptionTarget` condition with reason `TerminationByKubelet`, or an Event with reason `Evicted` whose `source_component` is `kubelet` or empty -- a real failure. An empty component is an older event stream, where kubelet pressure eviction is the fallback meaning. `TerminationByKubelet` also covers graceful node shutdown; the two differ only in message, so they share the category. |
| `rescheduled` | `DisruptionTarget` condition with reason `PreemptionByScheduler` (capacity was taken), `DeletionByTaintManager` (the node went NotReady) or `DeletionByPodGC` (an orphan was collected), or an Event with reason `Evicted` from any other named `source_component`. The condition with reason `EvictionByEvictionAPI` opens nothing: it is the autoscaler, a drain, or anything that goes through the eviction API and respects a PodDisruptionBudget, and the API cannot say whether the operator drained for consolidation or because the node was sick. The eviction itself was never the actionable fact: a pod that cannot be placed afterwards opens `scheduling`, one that comes up broken opens `crash`, one that dies badly on the way out opens `unclean_exit`; the condition row, the events and the pod's inferred `deletion_reason` (`evicted`) still record the eviction. An eviction-API `Evicted` event keeps this category: it attaches to a `rescheduled` row when one of the other reasons opened it and otherwise stays unattached with its category intact. `Evicted` is the one event reason whose meaning depends on who reported it, and the component is the only thing that separates the two: the message would say the same thing in prose. |
| `job_failed` | Job condition `Failed` (any reason) |
| `stuck` | a live pod that is not terminating, whose containers have all been `waiting` since before `stuck_after` (default 10 minutes), with no incident already open on it |
| `other` | anything else we decided is a problem but could not classify |

`stuck` is the one category keyed on elapsed time rather than on a reason: a pod waiting on a missing PersistentVolumeClaim or a mistyped ConfigMap sits in `Pending` or `ContainerCreating` forever with no container state ever naming why, so the reason-only rule above has nothing to read. A periodic pass, not a watch event, is what notices it (Section 6.2). A terminated reason of `Completed` (exit 0) opens nothing. Both `node_pressure` rules can fire for the same eviction (the kubelet sets the condition and then `status.reason`); the uniqueness rule on `(pod_uid, '', node_pressure)` makes that one incident.

There is no separate severity or actionable flag. Neither has a definition beyond "derived from category", and `category` already supports filtering. A mute would choose by category or by identity, and neither would have answered the noise a node autoscaler produces: its evictions were categorised `crash` and `probe` on pods whose one true fact was routine, so the fix was the category rule (the terminating gate above), not a flag on the rows it misfiled.

### 6.2 Opening and attaching

On each qualifying transition, first-sight problem state, or `probe` event:

1. Look for an open incident with the same `(pod_uid, container_name, category)`.
2. If found: attach (`incident_id` on the history/event row), bump `occurrences`, update `last_reason`, `last_message`, `last_seen_at` (process time, the same clock as `observed_at`).
3. If not, look for the most recent **closed** incident with the same key whose `close_reason` is not `pod_deleted` nor `manual`. If found: reopen it (Section 6.3), then attach as in step 2.
4. If neither: open one. `opened_at` uses the Kubernetes timestamp of the transition when available. Attach any unattached `k8s_events` rows for the pod that match the attach rule (Section 5.10) with `last_ts >= opened_at - stabilization_window`. **Capture `log_current`** for the container (or all containers, for pod-level incidents) and refresh `pod_json` -- for a stuck or probe-failing container, the live log at the moment of detection is often the only useful view.
5. When a dead instance appears (`restart_count` rose, `lastState.terminated.finishedAt` changed, or `state` became `terminated`): capture `log_previous` for that dead-instance index with the `previous` flag chosen as in Section 4, and refresh `pod_json` -- unless the instance exited 0 with `restart_count` still 0 and no incident names its container, in which case nothing is captured. A container that ran once and exited 0 is a success, and its log is not evidence of anything; on a two-minute CronJob this is the one log read per container per run that a pod nobody will ever ask about would otherwise accumulate forever.
6. On a precursor event with reason `Killing`, `Preempted`, `Preempting`, `Evicted` or `Unhealthy` (matched on reason; `Killing` and `Preempting` are `Normal` events, the others `Warning`) for a pod with no open incident on that container: capture `log_current` immediately into an in-memory early-capture cache. Kubernetes announces intent to remove a pod before it disappears; if a `terminated` transition is never observed (pod deleted outright, or the watch drops it), this is the only chance to capture anything at all. The cached bytes become an `artifacts` row with `captured_early = 1` only when the normal capture path fails **and** the pod is worth keeping: it has an open or recently closed incident, or the dead instance's exit code was non-zero. `Killing` fires on every routine rollout; the last 50 lines of every healthy pod that was rolled are not evidence and are dropped.

A crash loop therefore produces **one** incident with `occurrences` climbing, one history row per restart, and one log file per restart -- not 47 incidents.

**A grace window belongs to a category, and only `scheduling` has one.** The first `Unschedulable` condition does not open an incident on the spot: it opens `scheduling_grace` (default 60 seconds) later, on a pod update that arrives after the window, because on an autoscaled cluster a `FailedScheduling` is normally followed within a minute by a node claim and the pod starts. A pod pruned inside the window never opens anything at all. Every other category is still taken the moment its condition or state is first seen, with no grace.

**A persisting condition can open an incident; it can never attach to one.** With the watch informers run at resync 0, nothing re-delivers a pod on a timer; a pod update arrives only when something on the object changes, which for an unschedulable pod is normally the scheduler rewriting the condition's message on every retry. Past the grace window, a condition that changed opens or attaches exactly as any other transition does; a condition that did not change is considered only when no incident of its category is already open on the pod, and if it opens one it does so directly, never through the reopen step (step 3): reopening a story that had already ended because the same stale condition is still sitting there would turn "nothing new happened" into "it broke again". `PodScheduled` becoming `True` closes the open `scheduling` incident immediately (Section 6.3), which is the other half of why the grace window can afford to wait: recovery no longer depends on the closer's polling pass.

`stuck` is the exception to all of the above: it is decided by a periodic pass over the pods table, not by an event. Every tick, the pass lists every live pod that is not terminating whose containers have all been `waiting` since before `now - stuck_after` with no incident already open on it, and opens one row per pod, naming the waiting container so it can close through the same path as any other category (Section 6.3). No condition or transition ever opens it, which is what "keyed on time, not on a reason" means in practice.

### 6.3 Closing (system)

| Trigger | close_reason |
|---|---|
| Container is stable for `stabilization_window` (**10 minutes**, configurable) with no new transition. Stable means: `app` / `sidecar` container `running` and `ready`; `init` container `terminated` with exit code 0. | `recovered` |
| Pod deleted (watch or reconcile) | `pod_deleted` |
| `scheduling`: pod becomes scheduled | `recovered` |
| Job reaches `Complete` or is deleted | `job_finished` |
| A later Job of the same CronJob completes | `job_finished` |
| User marks resolved | `manual` |

Why a stabilization window: `CrashLoopBackOff` alternates `waiting` -> `running` -> `terminated` on every attempt. Closing the moment the container is `running` would close and reopen the incident every 30 seconds. Ten minutes is the default because the kubelet's maximum crash back-off is 300 seconds; any window longer than that cannot close an incident between two attempts of the same loop. It only affects *when* an incident counts as closed, never whether data is kept.

A CronJob run's `Failed` condition, first seen already older than `stabilization_window`, opens its incident already closed with `close_reason = job_finished` and `closed_at` the Job's own finish time: the daemon looked after the run had already ended, so it is recorded as history rather than entering the open list. A later Job of the same CronJob reaching `Complete` closes every other open incident of that CronJob's runs the same way, because a run that worked is the answer to an earlier run that did not; no new close reason exists for this, since `job_finished` already means "the Job is done with" on any row it appears on.

**Incidents on pods in a terminal phase stay open.** An evicted pod sits in `Failed` until PodGC or a human deletes it; a `restartPolicy: Never` Job pod sits until the Job is pruned. Their containers are never "stable", so `recovered` never applies and the incident closes with `pod_deleted` when the object finally goes. This is intended: the open list mirrors what is still sitting broken in the cluster. Acknowledge is the tool for "seen it, cannot fix it yet".

**The `recovered` close is evaluated by a periodic closer, not by watch events.** Nothing arrives from the cluster while a container is quietly healthy, so "stable for 10 minutes" cannot be observed as an event. A closer runs every `stabilization_check_interval` (**30 seconds**) and closes, in one statement, every open incident whose subject container is stable and whose `last_seen_at < now - stabilization_window`. All state it needs is in the DB, so it is restart-safe and idempotent; there are no in-memory timers. The same tick attaches late events (Section 6.2 rule on `k8s_events.incident_id`) to incidents closed within the last `stabilization_window`.

**The closer also closes, before the stable pass, every open incident whose pod is already marked deleted**, with `close_reason = pod_deleted` and `closed_at` the pod's own `deleted_at` (or `opened_at` when that is later, so a close never precedes its open). The delete path closes what is open at the instant the DELETE is processed; anything that slips past it would stay open forever, because a deleted pod's containers never stabilize -- except an init container that exited 0, which looks stable, and that is why this pass runs first: its close must say `pod_deleted`, not `recovered`.

A closed incident is **reopened** if a matching transition arrives while it still exists: clear `closed_at`, `close_reason` and `dismissed_at`, keep the row and its `acknowledged_at`. A dismissed incident that recurs is no longer dismissed; it is happening again. Incidents closed with `pod_deleted` are never reopened because their pod is gone, and incidents closed `manual` are never reopened by a recurrence either: a person said this one was done, so it happening again is a new story, not the old one quietly coming back. `DELETE /v1/incidents/{id}/resolve` is the explicit inverse of a manual close -- it clears `closed_at` and `close_reason` when they are `manual` and does nothing otherwise. This is what makes "it recovered and then broke again" one story instead of two.

`close_reason = pod_deleted` is stored together with the pod's `deletion_reason` (Section 5.3), so the incident can say "pod deleted (rollout)" rather than only "pod deleted".

### 6.4 Human actions

| Action | Effect on data |
|---|---|
| Acknowledge | sets `acknowledged_at`. Nothing else. |
| Mark resolved | `closed_at = now`, `close_reason = manual`. Final: a recurrence opens a new incident instead of reopening this one. Its inverse, unresolve, clears both fields again when they are still `manual`. |
| Dismiss (soft delete) | sets `dismissed_at`. Swept on the normal schedule. Cleared if the incident reopens. |
| Delete (hard) | deletes the incident row immediately, along with its artifact files. Attached history and event rows are detached (`incident_id` set null) and remain until their own sweep. The pod row stays -- it may still exist, and other incidents may reference it. |
| Note | free text on the incident. |

None of these affect retention. That is intentional: on a developer machine, "I haven't looked at it yet" must not mean "keep it forever".

---

## 7. File storage layout

```
<artifacts_root>/
  <cluster_id>/
    <namespace>/
      <pod_uid>/
        pod.json                       # latest manifest snapshot, overwritten on each capture
        <container_name>/
          restart_000.log              # log_previous captured after the 1st termination
          restart_001.log
          current.log                  # log_current, overwritten on each capture
```

- `cluster_id` (integer) not cluster name: names are user-editable.
- `pod_uid` not pod name: names repeat.
- One directory per container, one file per restart. A single "previous log" per pod would be overwritten on every restart and could not hold multi-container pods.
- `file_path` in the DB is relative to `artifacts_root`.

Write order: file first, then the `artifacts` row. A crash between the two leaves an orphan file, which the sweeper's orphan pass removes. The reverse order would leave a row pointing at nothing.

---

## 8. Retention and sweep

One configuration value: `retention_days` (default **3**). One rule: **anything that no longer exists in the cluster is deleted once it is older than the retention window; anything that still exists is kept.** Everything below is that rule applied to each table. The one known slack is Section 4's reconcile: an object that vanished while the process was not running is aged from the moment the absence was noticed, because nothing else is known.

Let `cutoff = now - retention_days`.

| What | Deleted when | Removed with it |
|---|---|---|
| **Pods** | `deleted_at < cutoff` | by cascade: `containers`, `pod_condition_history`, `container_state_history`, `incidents`, `artifacts` rows (files deleted first -- see below) |
| **Incidents on pods that still exist** | `closed_at < cutoff` | its `artifacts` (files first, then the rows; the schema only detaches them from the incident); attached history and event rows are detached (`incident_id` -> null), not deleted |
| **`container_state_history` / `pod_condition_history` on pods that still exist** | `observed_at < cutoff` and `incident_id` is null or points to a closed incident | -- |
| **Jobs** | `deleted_at < cutoff`, or `finished_at < cutoff` when the Job object is gone | by cascade: job-scoped `incidents` |
| **Events** | `last_ts < cutoff` and `involved_uid` is not a pod/job that still exists with an open incident | -- |
| **Rollout history** | `deleted_at < cutoff` (ReplicaSet gone) | -- |
| **Orphan files** | file on disk with no `artifacts` row | -- |
| **Orphan rows** | `artifacts` row whose file is missing | row deleted |
| **`sweep_runs`** | `ran_at < cutoff` | -- |

Every pass writes one `sweep_runs` row per table it touched, including zero-row passes.

A rollout row is never aged by `last_seen_at` alone: with the informers run at resync 0, a stable ReplicaSet's row legitimately goes quiet for as long as it keeps running, which is not staleness. Only `deleted_at` (the ReplicaSet itself gone from the cluster) starts its clock.

Because `k8s_events` and the history tables are swept by their own age, a closed incident on a long-lived pod can outlive some of its evidence: the incident row (closed within the window) stays, while events and transitions older than the cutoff are gone. This is accepted; the incident still carries `first_reason`, `last_reason`, `last_message`, `occurrences` and its artifacts.

A pod that succeeded cleanly and was deleted has a row, no incidents, and no artifacts; three days later the pod row is deleted and the cascade removes its containers and history. Nothing special-cases "boring" pods; they simply have little under them.

Things the sweeper **never** does:

- Delete a pod or job row whose object still exists in the cluster, regardless of age. A pod running for six months has a six-month-old row. Its *history* older than the window is trimmed; the pod itself is not.
- Delete an open incident. Open incidents only exist on live objects (deletion closes them), so this follows from the rule above; it is stated so nobody "optimises" it away.

Deletion order for a pod: read its artifact paths -> delete the files -> delete the pod row (the FK cascade removes the rest in one statement). If file deletion fails midway, stop and retry next run; a few stale files are harmless, a DB row pointing at a deleted file is a lie.

The sweeper runs on its own timer (`sweep_interval`, default hourly) and once at startup, in the same process, on the same SQLite file. Each pod (or incident, or batch of event rows) is its own short transaction; a single long transaction would hold the writer for the whole pass. With WAL mode readers are never blocked.

Sizing, so nobody worries: 2 clusters x 5 namespaces x ~100 pods, a rollout or two a day, a modest incident rate, 3-day window -- on the order of a few thousand rows per table, a few MB of event JSON, and tens of MB of logs. SQLite will not notice.

---

## 9. What is deliberately not here

| Not included | Why |
|---|---|
| CronJob missed-schedule detection | Correct detection depends on `spec.timeZone`, `suspend`, `concurrencyPolicy: Forbid`, `startingDeadlineSeconds`, and CronJob controller behaviour. Without watching CronJobs and handling all of these, a "missed" flag is a false-positive generator. A monitoring tool that lies is worse than one that stays silent. Revisit only with a design that is right by construction. |
| A namespaces dimension table | Nothing would reference it. `watched_namespaces` holds configuration; observation tables carry the name. |
| Severity / actionable flags | No definition beyond "derived from category". A mute would choose by category or by identity; the noise seen so far was a category rule filing routine terminations as failures, and was fixed there (Section 6.1). |
| Event files on disk | Events live whole in SQLite. |
| `describe` output | Does not exist in `client-go`; `pod_json` + events is the same information. |
| Per-cluster retention, long-term aggregates, exporting to another store | Not this tool's job today. The schema does not prevent it later. |

---

## 10. SQLite configuration

Set once on open:

```sql
PRAGMA journal_mode = WAL;        -- ingest and sweep don't block each other
PRAGMA synchronous = NORMAL;      -- safe with WAL; fsync on checkpoint, not every commit
PRAGMA foreign_keys = ON;         -- cascades depend on this
PRAGMA busy_timeout = 5000;       -- wait instead of failing on a brief writer overlap
```

One write connection, many read connections is the pattern that keeps SQLite happy in Go: two `*sql.DB` handles on the same file, the writer with `SetMaxOpenConns(1)`, the reader pool with `PRAGMA query_only = ON`. Ingest, capture, closer and sweeper all write through the single writer; `busy_timeout` is a backstop that should never fire.

Driver: `modernc.org/sqlite` (pure Go, no cgo). Performance at this scale is not a factor; a cross-compilable binary is.

---

## 11. Configuration values referenced by this design

| Setting | Default | Used in |
|---|---|---|
| `retention_days` | 3 | Section 8 |
| `sweep_interval` | 1 hour | Section 8 |
| `stabilization_window` | 10 minutes | Section 6.3 |
| `stabilization_check_interval` | 30 seconds | Section 6.3 |
| `attention_window` | 24 hours | The list's default view: open, or closed within the window and never acknowledged or dismissed (presentation doc Section 4.2) |
| Log tail lines / byte cap | 50 lines (requested as 51 to detect truncation), byte cap set by ingest | Enforced by the ingester; storage records `size_bytes` and `truncated` |
| `artifacts_root` | app data directory | Section 7 |

---

## 12. Common questions and how the schema answers them

| Question | Tables | How |
|---|---|---|
| What is running right now, and who owns it? | `pods` | `deleted_at IS NULL`; `workload_kind/name` |
| What is broken right now? | `incidents` | `closed_at IS NULL` |
| Containers of a pod, init or not | `containers` | `pod_uid = ?`, `kind` |
| A container crashed earlier but is fine now, pod still alive | `incidents` + `pods` | incident with `close_reason = recovered`, pod `deleted_at IS NULL` |
| Everything that happened to a pod that no longer exists | `pods` + `container_state_history` + `pod_condition_history` + `k8s_events` + `artifacts` | all by `pod_uid` / `involved_uid`; logs per restart |
| Which service fails most often? | `incidents` | `GROUP BY workload_kind, workload_name` -- not by pod name (rotates) and not by image (changes every deploy) |
| Which version is the failing one? | `incidents` | `image_tag` / `image_id` at open time |
| Is this pod flapping? | `incidents` / `container_state_history` | one incident with high `occurrences`; history rows alternating `running`/`terminated` within a short window, ordered by `k8s_finished_at` |
| OOM vs app crash vs image vs config breakdown | `incidents` | `GROUP BY category` |
| Did it OOM because the limit was low? | `incidents` + `containers` | `category = oom`, read `mem_limit_bytes` |
| Did failures start after a deploy? | `incidents` + `rollout_history` | incidents for the workload with `opened_at` just after a new `replicaset_uid` appeared |
| Did the image change without the tag changing? | `container_state_history` | `image_tag` same, `image_id` differs |
| Did a CronJob run succeed? | `jobs` | `cronjob_uid = ?` ordered by `started_at`; `condition_type` |
| Why did a Job fail terminally? | `jobs` | `condition_reason` (`BackoffLimitExceeded`, `DeadlineExceeded`) |
| How long did this Job take vs. its siblings? | `jobs` | `finished_at - started_at` grouped by `cronjob_uid` |
| Why is this pod stuck Pending? | `pod_condition_history` / `incidents` | `category = scheduling`, read `message` |
| A container's own event trail | `k8s_events` | `involved_uid = pod` and `field_path LIKE 'spec.containers{name}%'` |
| Which cluster/namespace did image X fail in? | `incidents` + `clusters` | `cluster_id` on every row |
| What time of day do failures cluster? | `incidents` | `GROUP BY` hour of `opened_at` -- within the retention window only |
| Was the app asleep and did it miss anything? | `container_state_history` | `gap_reconstructed = 1` |
| Why did this pod go away? | `pods` | `deletion_reason` (inferred), `status_reason`, `status_message` |
| Why is this pod stuck Terminating? | `pods` | `deletion_requested_at` set, `deleted_at` still null |
| What did the sweeper remove last night? | `sweep_runs` | `ran_at`, `table_name`, `rows_removed` |
