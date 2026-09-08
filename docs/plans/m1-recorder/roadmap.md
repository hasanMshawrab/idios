# idios Implementation Roadmap: milestone 1, the recorder

Status: complete 2026-08-27, all seven phases merged to `main`. This
milestone is frozen; nothing in this directory is edited again.

This is the ordering document. It says which phase comes first, what each
phase delivers, what can run in parallel, and the handful of cross-cutting
decisions every phase must respect. Each phase gets its own detailed plan
file in this directory when it starts; only Phase 1 is written up front,
because every other phase compiles against the types and schema it creates.

Design docs (the spec; the plans argue from them):

- `docs/design/data-storage.md` -- what is stored
- `docs/design/client-go-methods.md` -- how it is filled
- `docs/design/process-architecture.md` -- how the process runs

## Cross-cutting decisions

These apply to every phase. They are repeated in each phase plan's
Global Constraints section.

1. **ASCII only** in everything written (code, comments, docs, commits,
   file names). See `.ai/ascii-only.md`. Check with `hack/ascii-check`
   (`make ascii`); macOS grep has no `-P`.
2. **Storage path.** The process doc (Section 13) defaults `data_dir` to
   the OS user data directory. For development the default is
   `./.storage` relative to the working directory, gitignored. Layout:
   `.storage/idios.db` (SQLite, plus `-wal` / `-shm`),
   `.storage/artifacts/` (log and pod_json files),
   `.storage/idios.toml` (optional config). The default flips to the OS
   data dir before any release; nothing but one constant in
   `internal/config` changes.
3. **One timestamp layout.** `clock.Layout = "2006-01-02T15:04:05.000000Z"`.
   Every timestamp written to SQLite goes through `clock.Format`. No
   package calls `time.Format` with any other layout for a DB value.
4. **Process time comes from `clock.Clock`**, injected. Kubernetes
   timestamps are stored verbatim (UTC) and never adjusted.
5. **Single writer.** Every mutation goes through `store.Writer.Tx`.
   Reads for display go through `store.Reader`.
6. **Categories key on `reason` only.** Never on `phase`, `exit_code` or
   `message` inside mapping functions. A test enforces it (Phase 2).
7. **Driver:** `modernc.org/sqlite` (pure Go). **Config:** TOML via
   `github.com/BurntSushi/toml`. **Logging:** `log/slog`.
8. **Go 1.24**, module path `idios` (single binary, not published as a
   library; a short module path keeps imports readable).
9. **TDD per task.** Failing test, run it, implement, run it. The
   checkpoint at the end of each task is `go build ./... && go test ./...
   && make ascii` green, then one commit per `.ai/commits.md`. Branch
   `main`, no feature branches until more than one person works on it.
10. **Test cluster.** OrbStack's built-in Kubernetes (server 1.35). Its
    kubeconfig is copied to `kube/config` in this repo (context
    `orbstack`, `https://127.0.0.1:26443`); the user's `~/.kube/config` is
    never read. All manual and smoke testing uses
    `KUBECONFIG=./kube/config` and the namespace `idios-smoke`, created
    for this project. Never touch the other namespaces on that cluster
    (`default`, `plinth-*-test`). `kube/config` holds credentials and is
    gitignored. If the API refuses connections, run `orb start`.

## Phases

```
Phase 1  Skeleton + store            sequential, first
Phase 2  ingest + incident (pure)    |
Phase 5  capture pool                | parallel after Phase 1
Phase 6  closer + sweeper            |
Phase 3  processor (glue)            after Phase 2
Phase 4  k8s watcher                 after Phase 3; needs a kind cluster
Phase 7  status, smoke, hardening    after 4, 5, 6
```

Numbering follows the dependency order from the first overview; 5 and 6
are numbered after 3 and 4 but do not wait for them.

### Phase 1: Skeleton + store (plan: `01-skeleton-and-store.md`)

Status: done, merged to `main` 2026-08-27 (`295626b`).

Delivers: a git repo, `go.mod`, `cmd/idios` that opens `.storage/idios.db`,
applies the full schema from storage doc Section 5, and exits cleanly on
SIGINT. Packages `internal/clock`, `internal/config`, `internal/store`
(open, pragmas, embedded migrations, `Writer.Tx`, `Reader`, row types),
`internal/archtest` (dependency rule test). Store tests against a real
temp-file SQLite: migrations from empty, cascades, both partial unique
indexes, STRICT, timestamp layout.

Why first: `0001_init.sql` and the row types are the contract for every
other package. Freeze them here; changing a column later ripples into diff
functions, fixtures and SQL in three other phases.

### Phase 2: ingest + incident, pure logic

Status: done, merged to `main` 2026-08-27 (`f8488c5`). Plan:
`02-ingest-incident.md`.

Delivers: `internal/ingest` (`DiffPod`, `DiffJob`, `MapReplicaSet`,
`MapEvent`, `OwnerResolver` interface, `PodChanges` and friends) and
`internal/incident` (`Category` mapping, `Apply(incidents, changes, now)
-> IncidentOps`). Inputs are `k8s.io/api` structs plus store row types;
outputs are structs. No SQL, no network, clock passed in.

Tests: the scenario list in process doc Section 14.1 as table-driven tests
with JSON fixtures under `internal/ingest/testdata/<scenario>/`. Each
asserts the exact history rows, incident operations and capture requests.
Plus the prohibition test (no `.Phase`, `.ExitCode`, `.Message` inside
mapping functions).

Depends on: Phase 1 row types. Imports `k8s.io/api` and
`k8s.io/apimachinery` only (no client-go).

Fixtures can be authored in parallel with any other phase; they are
derived directly from the Section 14.1 list.

### Phase 3: processor (glue)

Status: done, merged to `main` 2026-08-27 (`b97bd69`). Plan:
`03-processor.md`. Built as `internal/processor` (not
`ingest.Processor`: `incident` imports `ingest`, so a processor inside
`ingest` could not call `incident.Apply`).

Delivers: `processor.Processor` with `Pod`, `PodDeleted`, `Job`, `JobDeleted`,
`ReplicaSet`, `ReplicaSetDeleted`, `Event` methods. Each: load snapshot
rows -> `Diff` -> `incident.Apply` -> execute SQL, in one `Writer.Tx`;
after commit, derive `CaptureRequest`s and hand them to a `CaptureSink`
interface (the pool implements it in Phase 5; tests use a recording fake).
Adds the SQL helpers in `store` that the processor needs (load pod
snapshot, upsert pod/containers, insert history, incident open with
`ON CONFLICT` on the partial index, attach events, delete-path closes).
Also the deletion_reason inference (storage doc 5.3) since it needs SQL.

Phase 2 hands Phase 3: `ingest.DeadInstance` (log_previous requests, one
`unobservable` artifact row per `Unobservable` instance), `incident.Ops`
(`HistoryCategories` copied into history rows, `Open`/`Attach`/`Close`
executed, `Open.HistoryIndexes` and `EventRef` resolved after insert),
`changes.WorkloadChanged` (correct `workload_*` on the pod's open
incidents), and `incident.EventCategory` (set `k8s_events.category` before
`ApplyEvent`). A message-only change of an `Unschedulable` condition produces
no operation, so `last_message` on a `scheduling` incident only updates on a
real transition; Phase 3 decides whether to refresh it from the condition
directly. Two Phase 2 readings of the design go one step past its text and
are for Phase 3 and the Phase 7 smoke run to confirm against the real
kubelet: (1) a `running` history row is never classified, so an OOM or Error
seen only in `lastState` on a `running -> running` restart opens no incident
until a `CrashLoopBackOff` is observed (the history row and the log capture
still happen); (2) the dead-instance index rule (`restart_count` when
waiting/terminated, `restart_count - 1` when running). Phase 3 must also:
set `k8s_events.category` with `incident.EventCategory` before `ApplyEvent`;
pass `Apply`/`ApplyEvent` only the involved pod's incidents; dedupe capture
requests on (pod, container, index) because a restart observed as two events
reports the same `DeadInstance` twice; and note that a first-seen pod whose
container is already `terminated` gets `opened_at` from `created_at` because
the container row holds no `finished_at` for the current state.

Phase 3 hands Phase 4: `processor.New(w, clk, sink, stabilizationWindow)`;
`Pod(ctx, clusterID, pod, resolver)`, `PodDeleted(ctx, uid, source)` (pass
`store.DeletionSourceReconcile` / `Unwatched` from the reconcile pass),
`Job`, `JobDeleted(ctx, uid)`, `ReplicaSet`, `ReplicaSetDeleted(ctx, uid)`,
`Event(ctx, clusterID, ev)`. Every method returns the transaction error;
the handler boundary in `k8s` logs it and never propagates it to client-go.
Phase 3 hands Phase 5: `processor.CaptureSink` (`Enqueue(CaptureRequest)`)
and `processor.CaptureRequest`, the process doc struct plus `IncidentID`
(write it to `artifacts.incident_id`) with `RestartCount int64`. Triggers:
`restart`, `incident_open`, `delete`, `early:<reason>`. Whether the pool
imports `processor` or `cmd/idios` adapts it is Phase 5's call; `capture`
must still not import `ingest` directly. A `delete` request arrives only for
a watch delete of a pod worth keeping; a pod not worth keeping gets no signal
and its cache entry ages out. The same dead instance may be requested twice
when its first capture is still in flight; the artifacts unique key makes
the second an upsert.

Depends on: Phase 2.

### Phase 4: k8s watcher

Status: done, merged to `main` 2026-08-27 (fast-forward of
`phase-4-k8s-watcher`, last code commit `3d859eb`). Plan:
`04-k8s-watcher.md`.

Delivers: `internal/k8s`: clientcmd loading, cluster identity
(`kube-system` uid with `IsForbidden` fallback), one
`SharedInformerFactory` per watched namespace, two-stage start
(ReplicaSets + Jobs, sync, then Pods + Events), reconcile pass, backoff on
failure, `DeletedFinalStateUnknown` unwrap, skew round-tripper, lister-backed
`OwnerResolver`. Wires informers to the Phase 3 processor.

Tests: `k8s.io/client-go/kubernetes/fake` driving real informers against the
real processor and a temp SQLite (process doc Section 14.4): pod lifecycle
with owner resolution and reconcile, a forbidden informer (replicasets, and
pods leaving the namespace's rows alone), backoff to 60 s, sync timeout; plus
unit tests for skew, identity, `DeletedFinalStateUnknown` unwrap and the
lister resolver.

Prerequisite for manual verification: the OrbStack cluster via
`KUBECONFIG=./kube/config`, namespace `idios-smoke`. Not needed for the
automated tests.

Depends on: Phase 3.

Phase 4 hands Phase 5: `processor.CaptureRequest`s reach `cmd/idios`'s
`logSink` today; the pool replaces it by implementing
`processor.CaptureSink` (`Enqueue(CaptureRequest)`) and being passed to
`processor.New`. The pool needs a `kubernetes.Interface` per cluster for
`GetLogs`; `k8s.KubeconfigClient(path, context, skew)` returns a
`k8s.ClientFunc` that builds one, and the watcher already calls it, so the
pool either receives the clientset from `cmd/idios` (build it once with the
same `ClientFunc`) or `k8s.Watcher` grows an accessor. `k8s` does not import
`store`, so a pool that writes `artifacts` rows lives in `capture` as
planned. Phase 4 hands Phase 7: `k8s.Watcher.Ready()` and `k8s.Skew.Offset()`
are the per-cluster status facts; `clusters.last_error` is written on
connect failure, sync timeout and per-informer `IsForbidden`; handler
errors are logged at the boundary with `cluster`, `namespace`, `kind`,
`uid`, `name` but not counted, and panics are not yet recovered there.
Config reload re-runs `cmd/idios`'s cluster loop: cancel a watcher's
context and build a new one from the rows. Manual runs need a `clusters`
row and a `watched_namespaces` row inserted by hand until the CLI exists.
`processor.Reconcile` takes `live func(namespace, uid string) bool`; the
watcher answers true for every row of a namespace whose pods or jobs
informer the Role refused, so those rows are neither confirmed nor
declared gone.

### Phase 5: capture pool

Status: done, merged to `main` 2026-08-27 (fast-forward of
`phase-5-capture-pool`, last code commit `ba3b9f8`). Plan:
`05-capture-pool.md`.

Delivers: `internal/capture`: `Pool` (one queue of `capture_queue_size` and
`capture_workers_per_cluster` workers per registered cluster, `Dropped()`
counter), `LogSource` seam with `ClientLogs` over a `kubernetes.Interface`
getter, worker logic (`GetLogs` with `TailLines+1` / `LimitBytes+1`, body
in memory, kubelet 200-error check, line then byte truncation, temp file
then rename, `capture_gap` mapping), early-capture cache (per-container
debounce and once-per-container final copies, 30 min expiry, 500-pod LRU),
file layout from storage doc Section 7, `pod_json` with `TypeMeta` set.
Writes `artifacts` rows through `store.UpsertArtifact`, file first.
`capture` imports `processor` for `CaptureRequest` and implements
`processor.CaptureSink`; `k8s.Watcher.Client()` supplies the clientset.

Tests: fake `LogSource` per process doc Section 14.5
(`TestLogCaptureWritesFileThenRow` table: content, tail+1 lines, max+1
bytes, empty, kubelet error line, NotFound, BadRequest on previous and on
current, Forbidden; the options the fake received; file exists when the
row is written), `pod_json` TypeMeta and overwrite, early copy used only
for pods worth keeping, early requests fill the cache without a row,
cache debounce/once/expiry/LRU, queue full drops and counts, workers
drain; `store.UpsertArtifact` keeps a file over a later gap.

Depends on: Phase 1 only (store + client-go; does not import ingest). Can
run in parallel with Phase 2.

Phase 5 hands Phase 6: files live at
`<artifacts_root>/<cluster_id>/<namespace>/<pod_uid>/pod.json` and
`<pod_uid>/<container>/{restart_NNN.log,current.log}`; `artifacts.file_path`
is that path relative to the root with forward slashes. Temp files are
`<artifacts_root>/tmp/capture-*`; a crash between write and rename leaves
one there, and a crash between rename and the row leaves a file with no
row, so the orphan pass must cover both `tmp/` and files without rows.
`store.UpsertArtifact` never replaces a file row with a gap, so a row with
`file_path` set is the only kind that owns a file. Phase 5 hands Phase 7:
`capture.Pool.Dropped()` is the drop counter for `idios status`; per-gap
and completed counts are not kept. Clusters are registered with
`pool.AddCluster(id, src)` before `pool.Run(ctx)`; a cluster added to a
running pool gets no workers, so config reload must build a new pool (or
`Pool` grows start-on-add) when it arrives. A request that reaches the
pool before its watcher connected is recorded as `capture_gap = 'unknown'`
with note `cluster not connected`. In the smoke run against OrbStack a
`crash` incident's `log_current` capture succeeded (the container was
reachable at detection time); a `400` on the current instance of a
waiting container still lands as `unknown`, and whether that case is
common enough to deserve a named gap value is a Phase 7 decision after
the full smoke target runs. `artifacts` has two writers:
`store.InsertArtifactGap` (processor, `INSERT OR IGNORE`, keeps the first
gap) and `store.UpsertArtifact` (pool, replaces a gap with a newer gap or
a file); a later pass may fold the first into the second. The early
cache's `wants`/`put` pair is not atomic across workers, so two
`Unhealthy` events for one container handled concurrently can both fetch
inside one debounce window; a reserve-before-fetch call is a Phase 7
hardening item.

### Phase 6: closer + sweeper

Status: done, merged to `main` 2026-08-27 (fast-forward of
`phase-6-closer-sweeper`). Plan: `06-closer-sweeper.md`.

Delivers: `internal/incident/closer.go` (`incident.Closer`: 30 s ticker, one
transaction per tick, `store.CloseStableIncidents` then
`store.AttachLateEvents`) and `internal/sweep` (`sweep.Sweeper`: `Sweep` runs
the ordered steps pods, incidents, container_state_history,
pod_condition_history, jobs, rollout_history, k8s_events (batches of 1000),
orphan_files, orphan_rows, sweep_runs, wal_checkpoint; one `sweep_runs` row
per step; `Run` repeats it every `sweep_interval`). Store SQL in
`store/sweep_sql.go`, including `Writer.Checkpoint`. `cmd/idios` sweeps once
before starting the watchers, then runs both loops.

Tests: store tables for the stable predicate per container kind, the attach
rule on recently closed incidents, event batching, history/jobs retention
predicates, WAL truncation; one closer tick against a seeded store; a sweep
over a seeded database and artifact tree asserting exact survivors, exact
remaining files and the exact `sweep_runs` rows; a pod whose file cannot be
deleted is skipped and recorded.

Depends on: Phase 1 only (store).

Phase 6 hands Phase 7: `incident.Closer.Tick` returns
`TickResult{Closed, Attached}` and `sweep.Sweeper.Sweep` writes `sweep_runs`;
the "closer: last tick, closed count" and "sweep: last run, per-table rows
removed" counters read those. The stable predicate treats `ephemeral`
containers and pod-level `node_pressure` / `rescheduled` incidents as never
stable; they close on delete. Late attach considers incidents of any
`close_reason` closed within the window, including `pod_deleted`, and only
events last seen within that window. The orphan pass leaves any file
modified within the last ten minutes alone (not only `tmp/`), because a
renamed capture may not have its row yet. A pod whose
artifact file cannot be deleted keeps its row and is retried next pass;
`sweep_runs.error` names it. The sweeper prunes empty pod and container
directories only; namespace and cluster directories stay. Nothing in this
phase recovers panics or counts errors beyond the `sweep_runs` rows.

### Phase 7: status, smoke, hardening

Status: done, merged to `main` 2026-08-27 (fast-forward of
`phase-7-status-smoke-hardening`). Plan:
`07-status-smoke-hardening.md`.

Delivers: `internal/status` (`Counters` for writer transactions, errors and
p99, handler errors and recovered panics, last object time per cluster;
`Snapshot` written to `<data_dir>/status.json` every 10 s and removed on
exit), `idios status` (snapshot plus display reads in
`store/status_sql.go`: incidents open by category and closed by reason,
artifacts by `capture_gap`, row counts, latest `sweep_runs` per table,
database, WAL and artifacts sizes), `idios cluster add` and `idios ns add`
(`store/config_sql.go`), `idios run`, slog to a rotating JSON file plus
text on a terminal, panic recovery and error counting at the k8s handler
boundary, config reload (the supervisor in `cmd/idios` re-reads the two
tables every 10 s and restarts only changed watchers; `capture.Pool` starts
workers for clusters added while running and has `RemoveCluster`),
`Writer.Observer`, `Writer.Checkpoint` reporting a busy checkpoint, the
early cache reserving a slot before it fetches, `data_dir` defaulting to
the OS user data directory and `kubeconfig` to clientcmd's resolution, and
`make smoke` (`hack/smoke/`).

Decisions: the `400` on a waiting container's current instance stays
`capture_gap = 'unknown'` (a named value is a migration); the two artifacts
writers stay (`InsertArtifactGap` keeps the first gap, `UpsertArtifact`
replaces a gap); sync state is reported per cluster, not per namespace;
`idios status` is a second process, so the daemon publishes a file rather
than a table or a listener.

Smoke run 2026-08-27: `make smoke` against the OrbStack cluster opened all four
incidents within two minutes: `oom` (`OOMKilled`, 8 occurrences), `crash` for
`Deployment smoke-crash` through the ReplicaSet owner chain (`Error`, 8),
`image_pull` (`ErrImagePull` then `ImagePullBackOff`), and `config`
(`CreateContainerConfigError`); 51 of 51 captures completed, no handler errors
or panics, eleven `sweep_runs` rows. Both Phase 2 kubelet readings were
confirmed: the kubelet surfaces a `terminated`/`OOMKilled` status between every
pair of `running` states, and the `oom` incident opened on the first such
termination, 21 s before the first `CrashLoopBackOff`; `log_previous` rows
carry index `restart_count - 1` while the new instance is running and
`restart_count` once it is waiting or terminated. Two smoke fixes landed on the
branch: the script's INT/TERM trap exits after one cleanup, and the OOM pod
sleeps five seconds before allocating so it is seen running first. No code bug
was found.

Depends on: 4, 5, 6.

The roadmap is complete. Presentation (`internal/query`, CLI/API/UI) is a
later design document and is out of scope for this roadmap.

## Parallel execution

After Phase 1 is complete and `go test ./...` is green, three independent
tracks can start at once. The tracks are separated by package ownership
so agents or sessions working concurrently never edit the same file; if
they run in parallel sessions, each takes a git worktree and merges to
`main` when its phase is green:

- Track A: Phase 2 then Phase 3 then Phase 4
- Track B: Phase 5
- Track C: Phase 6

Tracks B and C touch `internal/capture` and `internal/sweep` +
`internal/incident/closer.go` respectively; Track A owns `internal/ingest`,
`internal/incident` (except `closer.go`), `internal/processor` and
`internal/k8s`. The only
shared package any track may need to edit is `internal/store` (new SQL
helpers). To avoid two tracks editing one file, each adds its helpers in
its own file: `store/ingest_sql.go`, `store/capture_sql.go`,
`store/sweep_sql.go`. Migrations after `0001_init.sql` are not expected;
if a track needs a schema change it stops and raises it, because the other
tracks compile against the same schema.

Phase 7 is sequential after the three tracks are done.

## Model choice per phase

Execution uses `superpowers:subagent-driven-development`: an orchestrator
holds the plan and context, a fresh implementer subagent runs each task,
and a reviewer subagent checks each task's diff against the plan and the
`.ai` rules before the next task starts. The orchestrator pastes the
`.ai/*.md` paths into every subagent prompt; a fresh subagent sees only
its prompt.

| Phase | Orchestrator | Implementer | Reviewer | Why |
|---|---|---|---|---|
| 1 | Fable | Sonnet | Opus | Code is in the plan verbatim; the work is transcription and honest reporting. Judgement is in review. |
| 2 | Fable | Opus | Fable | Pure diff and incident logic with real-object fixtures; edge cases need reasoning, not transcription. |
| 3 | Fable | Opus | Opus | Glue plus SQL; moderate; correctness depends on reading Phase 2 types exactly. |
| 4 | Fable | Opus | Fable | client-go lifecycle, informer ordering, fake-clientset integration; easy to get subtly wrong. |
| 5 | Fable | Opus | Opus | Network, files, cache with debounce and LRU; many failure branches. |
| 6 | Fable | Sonnet | Opus | Closer and sweeper are SQL over a fixed schema with a fixed rule list. |
| 7 | Fable | Sonnet | Opus | Wiring, CLI, logging, smoke target. |

Plan writing for every phase: Fable, inline (no subagents), because it
reads the design docs and the real code together and the output is one
document.

## Kubeconfig and cluster access

Not needed for Phases 1, 2, 3, 5, 6. Phase 4's automated tests use the
fake clientset. `kube/config` (OrbStack, namespace `idios-smoke`) is used
only for manual verification in Phase 4 and for `make smoke` in Phase 7.

## Plan files

- `docs/plans/m1-recorder/roadmap.md` (this file)
- `docs/plans/m1-recorder/01-skeleton-and-store.md` (Phase 1)
- `docs/plans/m1-recorder/02-ingest-incident.md` (Phase 2)
- `docs/plans/m1-recorder/03-processor.md` (Phase 3)
- `docs/plans/m1-recorder/04-k8s-watcher.md` (Phase 4)
- `docs/plans/m1-recorder/05-capture-pool.md` (Phase 5)
- `docs/plans/m1-recorder/06-closer-sweeper.md` (Phase 6)
- `docs/plans/m1-recorder/07-status-smoke-hardening.md` (Phase 7)

## Writing the next phase plan

A phase plan is written when its phase starts, never earlier, by reading
the code that exists at that moment (not by guessing from earlier plans).
Use the `superpowers:writing-plans` skill. Before writing, read in this
order: `CLAUDE.md`, every `.ai/*.md`, this roadmap, the phase's section
above, the design doc sections it names, the previous phase's plan (for
the names and signatures it promised), and the actual code under
`internal/` (for what was really built).

Conventions every phase plan follows (Phase 1's plan is the example):

- Header: Goal, Architecture, Tech Stack, Spec (the doc sections the phase
  implements), Global Constraints (the `.ai` rules plus the cross-cutting
  decisions above that the phase touches).
- A file structure block first, then tasks. One task = one independently
  testable deliverable. Steps: failing test, run it, implement, run it,
  checkpoint (`go build ./... && go test ./... && make ascii`), commit
  per `.ai/commits.md`.
- Every test in the plan has a one-line trace, in the plan prose, to a
  design doc statement ("Section 14.1 crash loop scenario") or a bug. No
  trace, no test. Variants are table rows. Assertions compare whole
  structs or row sets.
- Code blocks contain no comments that restate the code and no reference
  to any document, section or plan. Reasons are written inline in one
  sentence.
- Interfaces block per task: Consumes (exact names from earlier tasks and
  phases) and Produces (exact names later tasks rely on).
- Named seams (a struct without its method yet, an interface with one
  fake) are in scope only when the plan names them and says which task or
  phase fills them.
- Fixtures for Phase 2 live under `internal/ingest/testdata/<scenario>/`
  as JSON of real Kubernetes objects; the scenario list is process doc
  Section 14.1, one directory per bullet.
- End with a self-review: spec coverage per section, the `.ai` rules
  checked by name, type consistency across tasks.

Facts a new session needs that are not in the design docs:

- Storage is `./.storage`; kubeconfig is `./kube/config`; the test
  namespace is `idios-smoke` on the OrbStack cluster (`orb start` if the
  API refuses connections). Nothing else on that cluster is touched.
- `data_dir` defaults to the OS user data directory; every command in this
  repository passes `-data-dir .storage` (or `.storage/smoke`) and
  `-kubeconfig ./kube/config`; `make run` and `make smoke` do so.
- Test cluster server version is 1.35. `kubectl` on the machine is 1.32,
  which is fine for applying smoke fixtures.
- `kind` is not installed and is not needed.
- Migrations after `0001_init.sql` are not expected. A phase that needs a
  schema change stops and raises it instead of adding a migration, because
  parallel tracks compile against the same schema.
- Store SQL helpers are added per phase in their own file
  (`store/ingest_sql.go`, `store/capture_sql.go`, `store/sweep_sql.go`)
  so concurrent tracks never edit one file.
- `modernc.org/sqlite` is pinned to v1.45.0; newer releases require a Go
  version above 1.24. `go mod tidy` writes the directive as `go 1.24.0`
  with a `toolchain` line; that is the accepted form.
- Every exported identifier carries a one-line doc comment
  (`.ai/comments.md` rule 2 wins over any plan code block that omits it).
  `Test*` functions are exempt.
- SQLite accepts `ON CONFLICT (cols) WHERE <partial index predicate>` only
  when the predicate is spelled exactly as in the index; `store.OpenIncident`
  carries both spellings from `0001_init.sql`.
- Processor tests build expected event rows with `ingest.MapEvent` and set
  only `Category` and `IncidentID`; the processor adds nothing else to an
  event row.
- `.golangci.yml` excludes `unparam` for `_test.go` files: test helpers
  legitimately take parameters that have one caller value today.
- `k8s.io/api` and `k8s.io/apimachinery` are pinned to v0.34.1; v0.35.0
  requires Go 1.25.
- Fixture pods for tests are real-shaped JSON under
  `internal/ingest/testdata/<scenario>/`; `internal/incident` tests read them
  through `../ingest/testdata` and drive `ingest.DiffPod` first.
- Known Phase 1 leftovers for later phases, parked deliberately:
  `cmd/idios -data-dir` re-derives `artifacts_root` even when the TOML
  set it explicitly; `store.dsn` does not URL-escape the path; the writer
  uses `BEGIN DEFERRED` (a second process on the same file could hit
  `SQLITE_BUSY_SNAPSHOT`); `Store.SchemaVersion` reads through the
  writer handle without the mutex. None is hit by Phase 1 callers.
