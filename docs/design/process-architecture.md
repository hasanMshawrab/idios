# Kubernetes Monitoring -- Process Architecture

Third of four. `data-storage.md` says what is stored and why. `client-go-methods.md` says which client-go calls fill it. This document says how the pieces run together inside one Go process: goroutines, ownership of the database, the order things happen in, what is inline and what is queued, and how the whole thing is tested. `presentation.md` says how the record is shown.

Scope is ingestion: everything from a kubeconfig context to rows and files on disk. Presentation (API, CLI, application) is `presentation.md`; it runs inside the same process as one more component, reads through the read-only connection pool and the `query` package, and writes through the same single writer.

Fragments of Go appear only where a shape is easier to show than to describe. They are not the implementation.

---

## 1. Shape of the process

One binary, one long-lived process, no external services. Five roles, all in-process:

```
clusters + watched_namespaces (SQLite)
        |
        v
+-----------------------------------------------------------+
| k8s.Watcher, one per cluster                              |
|   one SharedInformerFactory per watched namespace         |
|   Pods / Jobs / ReplicaSets / Events -> handler funcs      |
+-----------------------------------------------------------+
        | handler: load snapshot rows, diff, write, in one tx
        v
+-------------------+      +-------------------------------+
| ingest.Processor  |----->| store.Writer                  |
| diff + classify   |      | one connection, mutex, Tx()   |
+-------------------+      +-------------------------------+
        | enqueue CaptureRequest after commit
        v
+-------------------+      +-------------------------------+
| capture.Pool      |----->| <artifacts_root>/... files    |
| bounded queue,    |      | then artifacts row via Writer |
| N workers         |      +-------------------------------+
+-------------------+
+-------------------+  every 30s: close stabilized incidents,
| incident.Closer   |  attach late events           -> Writer
+-------------------+
+-------------------+  at startup and every hour: retention,
| sweep.Sweeper     |  orphan files, orphan rows    -> Writer
+-------------------+
+-------------------+  read-only pool; presentation later
| query             |
+-------------------+
```

Everything that mutates state goes through `store.Writer`. Everything that reads for display goes through the read pool. There is no message bus, no worker per pod, no channel of closures.

The same binary has one other entry point: `idios mcp`, a stdio MCP server an AI agent spawns for one session. It runs as a separate short-lived process but is only an HTTP client of the daemon's API (`presentation.md` Section 2); it never opens the database or the files, so the single-writer picture above is untouched.

---

## 2. Package layout

```
cmd/idios/                   entry point: flags, config, wiring, signal handling
internal/config/             process settings: retention, windows, paths, intervals
internal/store/              open DB, pragmas, migrations (embedded), Writer, Reader, row types
internal/store/migrations/   0001_init.sql, 0002_..., applied in order
internal/k8s/                clientcmd loading, cluster identity, factories, Watcher lifecycle
internal/ingest/             informer handlers + pure diff functions (pods, jobs, replicasets, events)
internal/incident/           category mapping, open/attach/reopen, Closer
internal/capture/            log + pod_json capture pool, early-capture cache, file layout
internal/sweep/              retention rules, orphan passes, sweep_runs rows
internal/query/              read models (Section 12 of the storage doc, one function each)
internal/clock/              Clock interface and real implementation
```

Dependency rules, enforced by an import test:

- `ingest` and `incident` import `store` row types and the `k8s.io/api` structs. They do not import `client-go` proper or `capture`. Owner lookups (ReplicaSet -> Deployment, Job -> CronJob) reach the informer stores through an `OwnerResolver` interface that `ingest` defines and `k8s` implements with the listers.
- `k8s` imports `client-go` and calls into `ingest` through a small handler interface. It does not import `store`.
- `capture` imports `client-go` (for `GetLogs`) and `store`. It does not import `ingest`.
- `sweep`, `incident.Closer` import `store` only.
- `query` imports `store` only, and nothing imports `query` except presentation.

The point of the rules: the diff and incident logic, where all the correctness lives, is testable with a struct in and a struct out.

---

## 3. The single writer

`store.Writer` owns one `*sql.DB` with `SetMaxOpenConns(1)` and a `sync.Mutex`. Its one method:

```go
func (w *Writer) Tx(ctx context.Context, fn func(*sql.Tx) error) error
```

Begin, run `fn`, commit or roll back. Callers never hold a `*sql.Tx` outside `fn`. Ingest handlers, the capture pool, the closer and the sweeper all call `Tx`. Contention is the mutex; each transaction is sub-millisecond at this scale, so a handler waiting on the sweeper waits for one small transaction, not a whole pass.

`store.Reader` is a second `*sql.DB` on the same file with a normal pool and `PRAGMA query_only = ON`. Presentation and `query` use it. WAL mode means readers never block the writer and the writer never blocks readers.

Pragmas are set on every new connection via the driver's connection hook, not once, because the pool opens connections lazily.

Migrations run before anything else, each file in its own transaction, recorded in `schema_migrations`. There are no down migrations; the file is a local cache and can be deleted.

---

## 4. Informer handlers write inline

The client-go doc says struct walking and SQLite writes are fast enough inline. That is the design: `AddFunc`, `UpdateFunc` and `DeleteFunc` call the processor directly and the processor calls `Writer.Tx`. A handler blocks only its own informer's listener goroutine, for one small transaction.

Why not a queue between informers and the writer:

- A queue keyed by uid (the `workqueue` pattern) coalesces: an intermediate `waiting -> running -> terminated` sequence collapses into the latest object. The history tables exist to record exactly those steps.
- A plain FIFO channel of whole objects preserves them but adds a buffer with a size to choose, a drop policy and a second goroutine, and buys nothing at tens of events per second.
- Ordering per informer is already guaranteed by client-go's listener. Inline writes keep it.

The one thing that is never inline is a network call. Log capture goes to the pool in Section 6.

### 4.1 One pod event, end to end

1. `UpdateFunc(old, new)` -> `processor.Pod(clusterID, new)`. `old` is ignored; the DB snapshot is the baseline (client-go doc 3.7).
2. `Writer.Tx`:
   a. Load the `pods` row and `containers` rows for `new.UID`. Load the pod's incidents, open and closed (closed ones are needed for reopen).
   b. `ingest.DiffPod(snapshot, new, now) -> PodChanges`: pod upsert values (including a resolved `workload_*` replacing a fallback), per-container upsert values, `container_state_history` rows to insert (transition key differs; `gap_reconstructed` when `restart_count` jumped by more than 1), `pod_condition_history` rows to insert, deletion fields, and, when there was no snapshot row, the problem states present at first sight.
   c. `incident.Apply(incidents, changes, now) -> IncidentOps`: attach to an open incident; else reopen the latest closed one for the key (not `pod_deleted`); else open (with `ON CONFLICT` on the partial unique index); which history rows get `incident_id`; `workload_*` corrections for open incidents when the pod's changed; unattached `k8s_events` to attach to a newly opened incident.
   d. Execute the upserts and inserts. Commit.
3. After commit, derive `CaptureRequest`s from `changes` and enqueue: `log_previous` for each dead instance that appeared (`restart_count` rose, `lastState.terminated.finishedAt` changed, or `state` became `terminated`), carrying the dead-instance index and the `previous` flag from the storage doc's rule; `log_current` and `pod_json` for each incident opened or reopened; `pod_json` refresh on any container transition.

Steps 2b and 2c are pure functions. They take structs and return structs. They do not touch SQL, the network or the clock (the clock is passed in).

### 4.2 Owner resolution

`workload_kind` / `workload_name` come from the ReplicaSet and Job informer stores, via `Lister().Get` behind the `OwnerResolver` interface. Those are in-memory maps; no API call. The cold-start race (pod arrives before its ReplicaSet) is avoided by start order: the ReplicaSet and Job informers are started and synced before the Pod and Event informers are started (Section 5), so the owner stores are full before the first pod handler runs. In steady state a miss falls back to `workload_* = controller_*`; the next update corrects the pod row and, in the same transaction, the `workload_*` of its open incidents, which are copied at open time and would otherwise keep a ReplicaSet name.

### 4.3 Deletes

`DeleteFunc` may receive `cache.DeletedFinalStateUnknown`; unwrap it. Then, in one transaction: set `deleted_at`, `deletion_source = 'watch'`, compute `deletion_reason` from local rows (storage doc 5.3), close every open incident on the pod with `close_reason = pod_deleted`. After commit: if the early-capture cache holds bytes for this pod **and** the pod had an open or recently closed incident or a container whose last exit code was non-zero, enqueue a request that writes them as `captured_early = 1` artifacts. Otherwise drop the cache entry: `Killing` fires on every rollout and the last lines of a healthy pod are not evidence.

### 4.4 Events

Event handler: map to a `k8s_events` row (timestamp precedence from storage doc 5.10), upsert on `(cluster_id, event_uid)`, classify by reason (`BackOff` stays `NULL`), attach per the storage doc 5.10 rule (same pod, container from `field_path`, category equal or `NULL`). For events with reason in `Killing`, `Preempted`, `Preempting`, `Evicted`, `Unhealthy`, regardless of `type` (`Killing` and `Preempting` are `Normal`), enqueue an early capture (Section 6.2). No event ever opens an incident on its own except `probe` (storage doc 6.1), and that only when the involved pod exists in `pods` and its container row has `ready = 0` at that moment.

---

## 5. Watcher lifecycle

One `k8s.Watcher` per cluster row, each with its own `context.Context` derived from the process context.

Startup order per cluster:

1. Load kubeconfig, select context, build `*rest.Config` and clientset. Wrap the transport with the skew round-tripper (Section 9).
2. Identity: `Namespaces().Get("kube-system")`. `IsForbidden` -> identity null, match on `api_server_url`. Upsert `clusters`, set `last_connected_at`, clear `last_error`.
3. For each `watched_namespaces` row: one `SharedInformerFactory` with `WithNamespace(ns)`, resync 0. Register handlers on all four informers, then start them in two stages: ReplicaSets and Jobs first, `WaitForCacheSync` on those (timeout 30 seconds), then Pods and Events. The owner stores are therefore full before the first pod handler runs.
4. `WaitForCacheSync` on the Pod and Event informers of all namespaces of this cluster, same timeout. Handlers already run during the initial list; the "no snapshot row -> no history row" rule means they cannot fabricate transitions, and the first-sight rule opens incidents only for states that are problems on their own.
5. Reconcile: for every `pods` / `jobs` row of this cluster with `deleted_at IS NULL` and not in the informer stores, mark `deleted_at`, `deletion_source = 'reconcile'` (or `'unwatched'` when the row's namespace is no longer in `watched_namespaces`), close its incidents with `pod_deleted` / `job_finished`. One transaction per row.
6. Mark the watcher ready. `idios status` reports per-namespace sync state.

Failure handling:

- A transport error at step 1-2 or a `WaitForCacheSync` timeout: write `clusters.last_error`, back off (1s doubling to 60s), retry. The other clusters are unaffected.
- `IsForbidden` on a specific informer's list (the namespace Role lacks `events` or `replicasets`): record it in `last_error` naming the resource and namespace, keep the other informers running. A watcher with only `pods` still produces most of the value.
- client-go handles watch reconnects and relists itself. Nothing in this process restarts an informer because a watch dropped.
- Laptop sleep: the watch times out, client-go relists on wake, the informer emits deletes for what vanished and updates for what changed; the snapshot diff turns those into history rows and incidents with `gap_reconstructed` where the counter says so.

Config changes: when `clusters` or `watched_namespaces` rows change, the affected watcher's context is cancelled and it is started again from step 1. Step 5 then marks the pods and jobs of a removed namespace with `deletion_source = 'unwatched'`. Nothing else restarts.

---

## 6. Capture pool

Log capture is a network call and the only thing that must never run on an informer goroutine.

### 6.1 Queue and workers

`capture.Pool` has a bounded channel (1024) of `CaptureRequest` and `N` worker goroutines per cluster (4). A full channel drops the request and increments a counter that `idios status` shows. Dropping is correct here: the API server is slow or down, and blocking the informers would make the pod table stale as well.

```go
type CaptureRequest struct {
    ClusterID     int64
    Namespace     string
    PodUID        string
    PodName       string
    Container     string   // "" for pod_json
    Kind          string   // log_previous | log_current | pod_json
    RestartCount  int      // dead-instance index the log belongs to; -1 for log_current / pod_json
    Previous      bool     // true when the dead instance is lastState.terminated
    Trigger       string   // restart | incident_open | early:<reason> | delete
    Pod           *corev1.Pod // for pod_json only; nil otherwise
}
```

Worker steps for a log: `GetLogs` with `TailLines = log_tail_lines + 1`, `LimitBytes = log_max_bytes + 1`, and `Previous` as carried on the request (true when the dead instance is in `lastState`, false when it is the current `state.terminated` or a `log_current`); stream to a temp file under `<artifacts_root>/tmp/`; apply the 200-with-error-body check (storage doc 5.11); drop the extra line or byte and set `truncated` if it was present; rename into place; then `Writer.Tx` to upsert the `artifacts` row. File first, row second, always. Errors map to `capture_gap` per client-go doc 3.11, and a row is written for every attempt so "we tried and got nothing" is recorded.

Dedup: `UNIQUE (pod_uid, container_name, kind, restart_count)` means a second request for the same instance is an upsert of the same row; the file is overwritten. `log_current` and `pod_json` are meant to be overwritten.

### 6.2 Early capture

Held in memory by the pool, keyed by `pod_uid`, one entry per container, with the bytes and the reason that triggered it.

- `Unhealthy`: debounced to one capture per pod per 60 seconds. Probes flap.
- `Killing`, `Evicted`, `Preempted`, `Preempting`: bypass the debounce, once per pod. These are the last chance. Matched on reason; `Killing` and `Preempting` are `Normal` events.
- The entry is kept after `DeleteFunc`; that is when it becomes the only copy.
- The `terminated` / delete capture path consults the cache only after its own `GetLogs` failed or returned empty, then writes the held bytes as an artifact with `captured_early = 1` -- but only for pods worth keeping (an open or recently closed incident, or a non-zero last exit code). Healthy pods rolled by a Deployment produce a `Killing` each and are dropped here.
- Entries older than 30 minutes with no delete observed are dropped. A pod that survived its `Unhealthy` does not need a snapshot kept forever.

Memory bound: at 50 lines per container this is kilobytes per pod; a cap of 500 pods with LRU eviction is more than enough and keeps a runaway namespace from growing the process.

---

## 7. Incident closer

A ticker at `stabilization_check_interval` (30 seconds). Each tick is one `Writer.Tx`:

1. Close deleted: every `incidents` row with `closed_at IS NULL`, `subject_kind = 'pod'`, whose pod has `deleted_at` set. Set `closed_at` to the pod's `deleted_at` (or the incident's `opened_at` when that is later), `close_reason = 'pod_deleted'`. The delete path closes what is open at the instant the DELETE is processed; an incident that slipped past it would stay open forever. This runs before the stable close because an init container that exited 0 on a deleted pod passes the stable test, and that close must say `pod_deleted`, not `recovered`.
2. Close stable: every open pod incident whose container row is stable (`app`/`sidecar`: `state = 'running' AND ready = 1`; `init`: `state = 'terminated' AND exit_code = 0`; `scheduling` category: pod has `node_name`) and whose `last_seen_at < now - stabilization_window`. Set `closed_at = now`, `close_reason = 'recovered'`.
3. Late attach: `k8s_events` rows with `incident_id IS NULL` whose `involved_uid` matches an incident closed within the last `stabilization_window`, applying the storage doc 5.10 attach rule (container from `field_path`, category equal or `NULL`). Set `incident_id`.

Job incidents close in the Job handler (`Complete` condition or delete), not here. Incidents on pods in a terminal phase (`Failed` / `Succeeded`) never satisfy the stable test and are closed by the delete path when the object goes, or by the deleted pass above when the delete path did not see them; that is intended (storage doc 6.3).

The closer has no memory between ticks. Restarting the process mid-window changes nothing.

---

## 8. Sweeper

Runs once at startup (before the watchers start, so the retention promise is true from the first second) and then every `sweep_interval`. Rules are storage doc Section 8, applied in this order so cascades do the work:

1. Pods with `deleted_at < cutoff`: for each, read artifact paths, delete files, then `DELETE FROM pods WHERE uid = ?` in one transaction. If a file delete fails, skip that pod, record the error in `sweep_runs.error`, continue with the next.
2. Incidents on live pods with `closed_at < cutoff`: same file-then-row order.
3. History rows on live pods older than the cutoff with no open incident.
4. Jobs, rollout history, events by their own rules. Events in batches of 1000 rows per transaction.
5. Orphan files: walk `<artifacts_root>`, delete any file whose relative path has no `artifacts` row. Skip `tmp/` entries younger than 10 minutes (a capture in flight).
6. Orphan rows: `artifacts` rows whose file is missing.
7. `sweep_runs` rows older than the cutoff.
8. `PRAGMA wal_checkpoint(TRUNCATE)` at the end, so the WAL file does not grow between passes.

One `sweep_runs` row per step, always, including zero-row steps.

---

## 9. Time

Two clocks are involved and the design keeps them apart.

- Kubernetes timestamps (`creationTimestamp`, `finishedAt`, `lastTransitionTime`, event times) are stored verbatim, UTC. They are never adjusted.
- Process time (`observed_at`, `first_seen_at`, `captured_at`, `closed_at`, the sweep cutoff, the stabilization test) comes from `clock.Clock`, injected everywhere so tests can move it.

Skew detection: a `http.RoundTripper` on each cluster's transport reads the `Date` header of every API response and keeps the most recent difference to local time. When it exceeds 5 minutes, `idios status` reports it for that cluster and the process logs it once per hour. Nothing else changes: the rules that use process time against cluster-observed state (the stabilization window on `last_seen_at`, the sweep cutoff against `deleted_at`) are documented as process-time rules and a visible skew warning is the honest response. Gap detection is counter-based (`restart_count`) and does not compare clocks at all.

All timestamps, from either clock, are written in the one fixed-width layout of storage doc Section 5 (`2006-01-02T15:04:05.000000Z`), so `metav1.Time` (seconds) and `metav1.MicroTime` (microseconds) land in the same column in sortable form. Correcting stored values by an estimated offset would be a second source of error.

---

## 10. Error handling

- A handler error never propagates to client-go. Log at error level with `cluster`, `namespace`, `kind`, `uid`, `name`; increment a counter; return. The next event for the object is a fresh diff against whatever was committed.
- `Writer.Tx` errors from a CHECK or UNIQUE violation are a bug in the mapping, not in the cluster. They are logged with the offending object's uid, the object is skipped, and the counter is visible in `idios status`. They are not retried in a loop.
- `errors.IsForbidden` is a configuration problem and stops retrying that call. Transport errors are transient and client-go retries them.
- Any file operation error in capture or sweep is recorded in the row that describes it (`capture_gap`, `sweep_runs.error`) so the DB tells the truth about the disk.
- Panics in a handler are recovered at the handler boundary, logged with the stack, and counted. A panic in one informer must not take the process down; the other clusters are still recording.

---

## 11. Observability

`log/slog`. JSON to a rotating file under the data directory, text to stderr when attached to a terminal. Every log line about an object carries `cluster`, `namespace`, `kind`, `uid`, `name`.

Counters, kept in a `status` struct read by `idios status` and a later `/status` endpoint:

- per cluster and namespace: informer synced, last event time, `last_error`
- writer: transactions, errors, p99 duration
- ingest: pod events, transitions inserted, incidents opened, attached, reopened, closed by reason
- capture: queued, completed, dropped (queue full), by `capture_gap`
- closer: last tick, closed count
- sweep: last run, per-table rows removed (also in `sweep_runs`)
- DB file size, WAL size, artifacts directory size
- clock skew per cluster

That is the whole metrics surface. No Prometheus, no tracing.

---

## 12. Security

- Container logs contain secrets. Data directory `0700`, files `0600`, temp files created with the same mode before rename.
- Kubeconfig contents are never stored; only the context name. Credentials stay where kubectl keeps them.
- Any later listener binds `127.0.0.1` only. Binding elsewhere requires an explicit flag whose name says it is insecure.
- `pod_json` is stored as received. It leaves for an AI agent only through `internal/sanitize`, which replaces every environment value and the `last-applied-configuration` annotation with a marker; the serving rule is `presentation.md` Section 10.

---

## 13. Configuration

Process settings in one file, `<config_dir>/idios.toml` (or flags overriding it):

| Setting | Default |
|---|---|
| `data_dir` | OS user data directory |
| `artifacts_root` | `<data_dir>/artifacts` |
| `retention_days` | 3 |
| `sweep_interval` | 1h |
| `stabilization_window` | 10m |
| `stabilization_check_interval` | 30s |
| `log_tail_lines` | 50 |
| `log_max_bytes` | 262144 |
| `capture_workers_per_cluster` | 4 |
| `capture_queue_size` | 1024 |
| `early_capture_debounce` | 60s |
| `kubeconfig` | default kubeconfig resolution |

Clusters and namespaces are rows in `clusters` and `watched_namespaces`, not settings. A CLI writes them; that CLI is part of the presentation document.

---

## 14. Testing

Ordered by value.

**1. Pure diff and incident logic (most of the tests).** Table-driven tests for `ingest.DiffPod`, `ingest.DiffJob`, `ingest.DiffReplicaSet`, `ingest.MapEvent`, `incident.Apply`. Inputs are JSON fixtures of real objects under `testdata/`, one directory per scenario:

- crash loop: `running -> waiting(CrashLoopBackOff)` with `restart_count` +1 and no `terminated` state in between
- OOM: `lastState.terminated.reason = OOMKilled`, exit 137, signal 9
- OOM with exit code 0 (seen in the wild; the reason wins)
- image pull: `ErrImagePull` then `ImagePullBackOff` then `Pulled` and running
- config: `CreateContainerConfigError`
- pending unschedulable: `PodScheduled=False, Unschedulable`, then scheduled
- evicted: `status.reason = Evicted` (the fixture also has `phase = Failed`; the assertion is that the category came from the reason)
- disruption: `DisruptionTarget` with `EvictionByEvictionAPI` (the condition row alone, no incident), with `DeletionByTaintManager` (rescheduled) and with `TerminationByKubelet` (node_pressure)
- init container failure, then success with `ready = true` on a terminated init container
- sidecar init container running alongside app containers
- multi-container pod where only one container fails
- `restart_count` jump of 3 between two snapshots (gap reconstruction, one `unobservable` artifact row)
- `restartPolicy: Never` Job pod whose container ends in `state.terminated` with reason `Error`: a `crash` incident, a `log_previous` request with `Previous = false` and dead-instance index 0
- dead-instance index: `waiting(CrashLoopBackOff)` with `restart_count = 1` -> index 1; `running` with `restart_count = 2` -> index 1
- first sight with no snapshot row and a healthy state (no incident); first sight with no snapshot row and `CreateContainerConfigError` (incident, no history row); first sight with a differing snapshot row (incident and history row)
- reopen: a `recovered` incident followed by a matching transition reopens the same row; a `pod_deleted` one does not
- `Killing` event with `type = Normal` triggers early capture; delete of a healthy pod drops the cached copy, delete of a pod with an incident writes it
- Job -> CronJob owner chain, and a Job pod with no CronJob owner
- ReplicaSet -> Deployment owner chain, and a bare ReplicaSet
- Event with only `eventTime` / `series`, and an Event with only `firstTimestamp` / `lastTimestamp`

Each scenario asserts the exact history rows, incident operations and capture requests produced.

**2. Store tests against a real temp-file SQLite.** Migrations apply cleanly from empty; every cascade removes what the storage doc says; both partial unique indexes reject a duplicate open incident and accept one after close, including the `container_name = ''` and job cases; `STRICT` rejects a wrong type; sweep rules with an injected clock remove exactly the expected rows and files and write `sweep_runs`.

**3. Prohibition tests.** A test greps `internal/incident` and `internal/ingest` source for `.Phase`, `.ExitCode` and `.Message` used inside category mapping functions and fails if found. Categories key on `reason` only (storage doc 6.1): `phase` lies (`CrashLoopBackOff` reports `Running`), exit codes are ambiguous (137 is OOM or a grace-period kill; the kubelet already folds "non-zero" into reason `Error`), and messages are prose. `ExitCode` is legitimately read elsewhere (the closer's init-container stable test, the early-capture keep decision) and those sites are outside the mapping functions. A second test asserts the package dependency rules of Section 2. A third asserts that every timestamp written by `store` matches the fixed-width layout.

**4. Informer integration.** `k8s.io/client-go/kubernetes/fake` clientset driving real informers against the real processor and a temp SQLite file. Scripted sequences (create pod, update status three times, delete) assert end state of the tables. A `DeletedFinalStateUnknown` case is included. No kind or envtest in CI; a manual smoke target against a local kind cluster is documented in the Makefile.

**5. Capture pool.** Fake `GetLogs` returning: content; exactly `log_tail_lines + 1` lines (asserts `truncated = 1` and the stored line count); empty; 200 with a kubelet error line; `IsNotFound`; `IsBadRequest` on `Previous = true`; `IsForbidden`. Assert the file, the `artifacts` row, `capture_gap`, `capture_note`, the `Previous` flag the fake received, and that the file exists before the row does.

Verification is running the binary against a cluster and reading the tables, not only the tests. A `make smoke` target starts the process against kind with a namespace of deliberately broken pods (crash loop, bad image, missing configmap, OOM at 10Mi) and prints the `incidents` table after two minutes.
