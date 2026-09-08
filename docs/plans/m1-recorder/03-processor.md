# Phase 3: Processor (glue) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Read `.ai/*.md` before writing any code; those rules override habits.

**Goal:** One package, `internal/processor`, whose `Processor` turns an informer callback (pod, job, replicaset, event, or a delete of one) into exactly one `store.Writer.Tx` that loads the snapshot rows, calls the Phase 2 pure functions, executes their result as SQL, and after commit hands `CaptureRequest`s to a `CaptureSink`. Plus the SQL helpers in `store/ingest_sql.go` it needs, and `deletion_reason` inference.

**Architecture:** `processor` imports `ingest`, `incident`, `store`, `clock`. It cannot live in `ingest` as the roadmap named it, because `incident` already imports `ingest` for `PodChanges`; a `Processor` in `ingest` that calls `incident.Apply` would be an import cycle. Nothing else changes: `k8s` (Phase 4) calls `processor`, `capture` (Phase 5) implements `processor.CaptureSink` or `cmd/idios` adapts the pool to it. All SQL is in `store/ingest_sql.go` as functions taking a `*sql.Tx`; the processor holds no SQL strings. Diff and lifecycle decisions stay in Phase 2 code; the processor only sequences and executes.

**Tech Stack:** Go 1.24, `modernc.org/sqlite` v1.45.0 (supports `ON CONFLICT ... WHERE` against a partial unique index and `RETURNING`), `k8s.io/api` v0.34.1, `github.com/google/go-cmp`.

**Spec:** `docs/design/process-architecture.md` Sections 4.1-4.4, 6.1 (the `CaptureRequest` shape), 14.1; `docs/design/data-storage.md` Sections 4, 5.3 (`deletion_reason`, `workload_*` correction), 5.4, 5.7 (open path, partial indexes), 5.10 (event upsert, attach rule), 5.11 (`unobservable`), 6.2, 6.3 (`pod_deleted`, `job_finished`, reopen); `docs/design/client-go-methods.md` 3.7, 3.10, 3.11. Roadmap: `docs/plans/m1-recorder/roadmap.md` (Phase 3 section and its handoff paragraph). The plan may cite these; the code must not (`.ai/code-is-truth.md`).

## Global Constraints

- `.ai/ascii-only.md`, `.ai/tests.md`, `.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`, `.ai/commits.md` apply to every line. Every exported identifier gets a one-line doc comment even where a block below omits it. Code blocks contain no comments beyond those shown.
- Every task ends with `go build ./... && go vet ./... && go test ./... && make ascii` green, then one commit per `.ai/commits.md` (`area: what`, no trailers, `git add` named paths). Work happens on branch `phase-3-processor`; the orchestrator fast-forwards `main` onto it locally at the end. No remote, no push.
- Module path `idios`. No new dependencies. Never `client-go` in `processor`, `ingest`, `incident` (the archtest forbids it; Task 5 adds `processor` to the rule).
- Every timestamp written goes through `clock.Format`; process time is `clock.Clock.Now()` injected into `Processor`. Kubernetes timestamps arrive already formatted by `ingest`.
- No schema change. `0001_init.sql` is the contract. If a task appears to need one, stop and report.
- Store SQL for this phase lives only in `internal/store/ingest_sql.go` (scanners too). No other store file is edited.
- Store row types are used as they exist in `internal/store/rows.go`; no fields are added.
- Fixtures: JSON under `internal/ingest/testdata/<scenario>/`, namespace `idios-smoke`, cluster id `1`, `testNow = 2026-08-27T12:00:00Z`. The processor tests read them via `../ingest/testdata`. This phase adds three fixtures (Task 7 `replicaset/deploy-rev8.json`, Task 9 `event-unhealthy/event.json` and `event-killing/event.json`).
- Booleans are written as `INTEGER` 0/1 through an explicit `boolInt` conversion; nothing relies on the driver's bool binding.

## Decisions made here (the spec or roadmap leaves them open)

- **Package `internal/processor`, type `Processor`.** See Architecture. The roadmap's `ingest.Processor` is corrected in Task 10.
- **`CaptureRequest` and `CaptureSink` live in `processor`.** `capture` may not import `ingest` (archtest) and Phase 5 owns `internal/capture`, so the type the processor emits cannot live there without Phase 5 existing first. `CaptureRequest` is the process doc 6.1 struct plus `IncidentID *int64`: the processor knows the incident at commit time and the pool must write `artifacts.incident_id`, which the sweeper uses to delete an incident's files. `RestartCount` is `int64` to match `store.Artifact.RestartCount` and `ingest.DeadInstance.Index`.
- **Processor is cluster-agnostic.** `clusterID` and the `ingest.OwnerResolver` are parameters of `Pod`; one `Processor` serves every watcher over the one `Writer`.
- **Attach bumps `occurrences` by one per `Attach` op.** `incident.Apply` coalesces two same-key problems in one event into one `Attach` (it does for `Open` by `Occurrences++`); the processor does not re-derive the count. A coalesced attach therefore counts once. Stated so nobody "fixes" it silently.
- **`Open` executes as `INSERT ... ON CONFLICT ... DO UPDATE ... RETURNING id`** against the matching partial unique index (pod or job). `Apply` already resolved against the loaded rows, so the conflict branch fires only under replay or a race, exactly as storage doc 5.7 intends.
- **Newly opened incidents attach earlier events in SQL** (storage 6.2 step 4): unattached `k8s_events` with the same `involved_uid`, `last_ts >= opened_at - stabilization_window`, category equal or `NULL`, and for a container incident `field_path GLOB 'spec.*{<name>}'`. Container names are DNS labels (no GLOB metacharacters), so the pattern needs no escaping.
- **`scheduling` `last_message` is refreshed from the condition directly** (the roadmap's open question). Storage 5.4 says the latest wording lives on the incident; a `0/3 nodes` -> `0/4 nodes` change is the fact a developer wants. Implemented as one `UPDATE` on the pod's open `scheduling` incident when the pod carries a condition that `incident.ConditionCategory` maps to `scheduling`. `last_seen_at` is not touched, so the closer's stability window is unaffected.
- **Capture dedupe is a DB check, not memory.** A restart seen as two events (`terminated` then `running`) reports the same `DeadInstance` twice, once per event. Before enqueueing `log_previous`, the processor skips the request when an `artifacts` row with a file already exists for `(pod, container, log_previous, index)`. A request in flight is not visible yet; that residual duplicate is absorbed by the pool's upsert on the same unique key (process doc 6.1). Within one event `DiffPod` cannot report an index twice, so no in-memory set exists.
- **`Unobservable` dead instances become `artifacts` rows in the transaction** (`capture_gap = 'unobservable'`, `file_path NULL`, `incident_id` = the incident touched for that container in this event, else `NULL`), `INSERT OR IGNORE` on the unique key. No request is enqueued for them.
- **`PodDeleted(ctx, uid, source)`** takes the `deletion_source` so Phase 4's reconcile pass reuses it with `reconcile` / `unwatched`. Delete-trigger capture requests (`Trigger = "delete"`, one `log_current` per container) are enqueued only for `source = watch` and only when the pod is worth keeping (an incident open at delete time or closed within `stabilization_window`, or any container whose `exit_code` or `last_terminated_exit_code` is non-zero). Reconcile deletes happen at startup when the early-capture cache is empty; enqueueing hundreds of doomed `GetLogs` calls there buys nothing. A pod not worth keeping gets no signal; the pool's own expiry and LRU drop its cache entry.
- **`deletion_reason` decision is a pure function** `ingest.DeletionReason` fed by two store loads; the roadmap put the whole thing in Phase 3 because of the SQL, and the pure part is tested as a table.
- **Event path enqueues.** A `probe` open enqueues `log_current` (no `pod_json`: the event path holds no `*corev1.Pod`). A reason accepted by `ingest.EarlyCaptureReason` on a `Pod` event enqueues `log_current` with `Trigger = "early:<reason>"` for the container from `field_path` (`""` = all) when no open incident exists on that container (storage 6.2 rule 6). What the pool does with `early:` requests (cache, debounce) is Phase 5.
- **Event `incident_id` on upsert keeps an existing attach**: `incident_id = COALESCE(excluded.incident_id, k8s_events.incident_id)`, so a count bump whose attach lookup finds nothing (incident since closed) does not detach the row.
- **Deleted pods are idempotent.** `PodDeleted` for an unknown uid or an already-deleted row does nothing and returns nil; `DeletedFinalStateUnknown` and reconcile can both name pods the store never saw or already closed.

## File structure

```
internal/store/ingest_sql.go                  scanners + every SQL helper this phase adds
internal/store/ingest_sql_test.go             helper tests that trace to a spec rule
internal/ingest/deletion.go                   DeletionReason (pure)
internal/ingest/deletion_test.go
internal/ingest/testdata/replicaset/deploy-rev8.json
internal/ingest/testdata/event-unhealthy/event.json
internal/ingest/testdata/event-killing/event.json
internal/processor/processor.go               Processor, New, CaptureRequest, CaptureSink, Trigger consts
internal/processor/pod.go                     Pod
internal/processor/capture.go                 request derivation after commit
internal/processor/delete.go                  PodDeleted
internal/processor/job.go                     Job, JobDeleted
internal/processor/replicaset.go              ReplicaSet, ReplicaSetDeleted
internal/processor/event.go                   Event
internal/processor/ops.go                     execOps (shared by pod, job, event)
internal/processor/processor_test.go          fixtures, fake sink, row readers
internal/processor/pod_test.go
internal/processor/delete_test.go
internal/processor/job_test.go
internal/processor/replicaset_test.go
internal/processor/event_test.go
internal/archtest/deps_test.go                processor rule
docs/plans/m1-recorder/roadmap.md           Phase 3 status
```

Every test below has a one-line trace to a spec statement. No trace, no test.

---

### Task 1: Store scanners and snapshot load/upsert helpers

**Files:**
- Create: `internal/store/ingest_sql.go`
- Test: `internal/store/ingest_sql_test.go`

**Interfaces:**
- Consumes: `Pod`, `Container`, `PodCondition`, `podColumns`, `scanPod`, `rowScanner` from `rows.go`; `clock.Format`.
- Produces: `func boolInt(b bool) int64`; `func LoadPod(ctx, tx *sql.Tx, uid string) (*Pod, error)` (nil, nil when absent); `func LoadContainers(ctx, tx, podUID string) ([]Container, error)` ordered by `id`; `func LoadContainer(ctx, tx, podUID, name string) (*Container, error)`; `func LoadLatestConditions(ctx, tx, podUID string) ([]PodCondition, error)` (newest row per type, ordered by `id`); `func UpsertPod(ctx, tx, p Pod) error`; `func UpsertContainers(ctx, tx, cs []Container) error`; `func InsertConditions(ctx, tx, cs []PodCondition) error`; `scanContainer`, `containerColumns`, `scanCondition`, `conditionColumns`.

Tests and their trace:
- `TestLatestConditionPerType`: storage 5.4 "insert only when (type, status, reason) changes for that pod" -- the diff needs the newest row per type, not all rows.
- `TestUpsertOverwritesStatusColumns`: storage 5.3 "identity columns are written once; status columns are overwritten in place", 5.5 "one row per container per pod... overwritten in place" on `UNIQUE (pod_uid, name)`.

- [ ] **Step 1: Write the failing tests**

`internal/store/ingest_sql_test.go`:

```go
package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"idios/internal/clock"
)

func inTx(t *testing.T, s *Store, fn func(tx *sql.Tx) error) {
	t.Helper()
	if err := s.Writer.Tx(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestLatestConditionPerType(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	ts := clock.Format(testEpoch)
	rows := []PodCondition{
		{PodUID: "p1", Type: "PodScheduled", Status: "False", Reason: "Unschedulable", Message: ptr("0/3 nodes"), ObservedAt: ts},
		{PodUID: "p1", Type: "Ready", Status: "False", Reason: "", ObservedAt: ts},
		{PodUID: "p1", Type: "PodScheduled", Status: "True", Reason: "", ObservedAt: ts},
	}
	ctx := context.Background()
	inTx(t, s, func(tx *sql.Tx) error { return InsertConditions(ctx, tx, rows) })
	var got []PodCondition
	inTx(t, s, func(tx *sql.Tx) (err error) { got, err = LoadLatestConditions(ctx, tx, "p1"); return err })
	want := []PodCondition{
		{ID: 2, PodUID: "p1", Type: "Ready", Status: "False", ObservedAt: ts},
		{ID: 3, PodUID: "p1", Type: "PodScheduled", Status: "True", ObservedAt: ts},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}

func TestUpsertOverwritesStatusColumns(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ctx := context.Background()
	first := Pod{UID: "p1", ClusterID: cid, Namespace: "idios-smoke", Name: "web-1", Phase: "Pending", ControllerKind: "none", WorkloadKind: "none",
		CreatedAt: "2026-08-27T11:45:00.000000Z", FirstSeenAt: "2026-08-27T11:50:00.000000Z", LastSeenAt: "2026-08-27T11:50:00.000000Z"}
	second := first
	second.Phase, second.NodeName, second.LastSeenAt = "Running", ptr("node-a"), "2026-08-27T11:51:00.000000Z"
	c1 := Container{PodUID: "p1", Name: "api", Kind: ContainerKindApp, Image: "web:1", State: StateWaiting, UpdatedAt: first.LastSeenAt}
	c2 := c1
	c2.State, c2.Ready, c2.RestartCount, c2.ExitCode, c2.UpdatedAt = StateRunning, true, 2, ptr[int64](1), second.LastSeenAt
	inTx(t, s, func(tx *sql.Tx) error {
		if err := UpsertPod(ctx, tx, first); err != nil {
			return err
		}
		return UpsertContainers(ctx, tx, []Container{c1})
	})
	inTx(t, s, func(tx *sql.Tx) error {
		if err := UpsertPod(ctx, tx, second); err != nil {
			return err
		}
		return UpsertContainers(ctx, tx, []Container{c2})
	})
	var gotPod *Pod
	var gotContainers []Container
	inTx(t, s, func(tx *sql.Tx) (err error) {
		if gotPod, err = LoadPod(ctx, tx, "p1"); err != nil {
			return err
		}
		gotContainers, err = LoadContainers(ctx, tx, "p1")
		return err
	})
	c2.ID = 1
	if d := cmp.Diff(&second, gotPod); d != "" {
		t.Error(d)
	}
	if d := cmp.Diff([]Container{c2}, gotContainers); d != "" {
		t.Error(d)
	}
	if n := countRows(t, s, "containers"); n != 1 {
		t.Errorf("containers rows = %d, want 1", n)
	}
	var missing *Pod
	inTx(t, s, func(tx *sql.Tx) (err error) { missing, err = LoadPod(ctx, tx, "nope"); return err })
	if missing != nil {
		t.Errorf("LoadPod(unknown) = %+v, want nil", missing)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestLatestConditionPerType|TestUpsertOverwritesStatusColumns' -v`
Expected: compile error, `undefined: InsertConditions` and friends.

- [ ] **Step 3: Write the helpers**

`internal/store/ingest_sql.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
)

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// LoadPod returns the pods row for uid, or nil when there is none.
func LoadPod(ctx context.Context, tx *sql.Tx, uid string) (*Pod, error) {
	p, err := scanPod(tx.QueryRowContext(ctx, "SELECT "+podColumns+" FROM pods WHERE uid = ?", uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertPod writes every column; the caller has already carried first_seen_at
// and the deletion columns over from the snapshot.
func UpsertPod(ctx context.Context, tx *sql.Tx, p Pod) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO pods (`+podColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (uid) DO UPDATE SET
    cluster_id = excluded.cluster_id, namespace = excluded.namespace, name = excluded.name,
    node_name = excluded.node_name, phase = excluded.phase, status_reason = excluded.status_reason,
    status_message = excluded.status_message, deletion_requested_at = excluded.deletion_requested_at,
    qos_class = excluded.qos_class, controller_kind = excluded.controller_kind,
    controller_name = excluded.controller_name, controller_uid = excluded.controller_uid,
    workload_kind = excluded.workload_kind, workload_name = excluded.workload_name,
    created_at = excluded.created_at, started_at = excluded.started_at,
    first_seen_at = excluded.first_seen_at, last_seen_at = excluded.last_seen_at,
    deleted_at = excluded.deleted_at, deletion_source = excluded.deletion_source,
    deletion_reason = excluded.deletion_reason`,
		p.UID, p.ClusterID, p.Namespace, p.Name, p.NodeName, p.Phase, p.StatusReason, p.StatusMessage,
		p.DeletionRequestedAt, p.QOSClass, p.ControllerKind, p.ControllerName, p.ControllerUID,
		p.WorkloadKind, p.WorkloadName, p.CreatedAt, p.StartedAt, p.FirstSeenAt, p.LastSeenAt,
		p.DeletedAt, p.DeletionSource, p.DeletionReason)
	return err
}

const containerColumns = `id, pod_uid, name, kind, image, image_tag, image_id, container_id,
cpu_request, cpu_limit, mem_request, mem_limit, cpu_request_millis, cpu_limit_millis,
mem_request_bytes, mem_limit_bytes, state, reason, exit_code, signal, ready, restart_count,
running_since, last_terminated_reason, last_terminated_exit_code, last_terminated_signal,
last_terminated_at, updated_at`

func scanContainer(r rowScanner) (Container, error) {
	var c Container
	var ready int64
	err := r.Scan(&c.ID, &c.PodUID, &c.Name, &c.Kind, &c.Image, &c.ImageTag, &c.ImageID, &c.ContainerID,
		&c.CPURequest, &c.CPULimit, &c.MemRequest, &c.MemLimit, &c.CPURequestMillis, &c.CPULimitMillis,
		&c.MemRequestBytes, &c.MemLimitBytes, &c.State, &c.Reason, &c.ExitCode, &c.Signal, &ready, &c.RestartCount,
		&c.RunningSince, &c.LastTerminatedReason, &c.LastTerminatedExitCode, &c.LastTerminatedSignal,
		&c.LastTerminatedAt, &c.UpdatedAt)
	c.Ready = ready == 1
	return c, err
}

// LoadContainers returns the pod's container rows in insertion order.
func LoadContainers(ctx context.Context, tx *sql.Tx, podUID string) ([]Container, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+containerColumns+" FROM containers WHERE pod_uid = ? ORDER BY id", podUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Container
	for rows.Next() {
		c, err := scanContainer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LoadContainer returns one container row, or nil when there is none.
func LoadContainer(ctx context.Context, tx *sql.Tx, podUID, name string) (*Container, error) {
	c, err := scanContainer(tx.QueryRowContext(ctx, "SELECT "+containerColumns+" FROM containers WHERE pod_uid = ? AND name = ?", podUID, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertContainers writes every column of each row on (pod_uid, name).
func UpsertContainers(ctx context.Context, tx *sql.Tx, cs []Container) error {
	for _, c := range cs {
		_, err := tx.ExecContext(ctx, `
INSERT INTO containers (pod_uid, name, kind, image, image_tag, image_id, container_id,
    cpu_request, cpu_limit, mem_request, mem_limit, cpu_request_millis, cpu_limit_millis,
    mem_request_bytes, mem_limit_bytes, state, reason, exit_code, signal, ready, restart_count,
    running_since, last_terminated_reason, last_terminated_exit_code, last_terminated_signal,
    last_terminated_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (pod_uid, name) DO UPDATE SET
    kind = excluded.kind, image = excluded.image, image_tag = excluded.image_tag,
    image_id = excluded.image_id, container_id = excluded.container_id,
    cpu_request = excluded.cpu_request, cpu_limit = excluded.cpu_limit,
    mem_request = excluded.mem_request, mem_limit = excluded.mem_limit,
    cpu_request_millis = excluded.cpu_request_millis, cpu_limit_millis = excluded.cpu_limit_millis,
    mem_request_bytes = excluded.mem_request_bytes, mem_limit_bytes = excluded.mem_limit_bytes,
    state = excluded.state, reason = excluded.reason, exit_code = excluded.exit_code,
    signal = excluded.signal, ready = excluded.ready, restart_count = excluded.restart_count,
    running_since = excluded.running_since, last_terminated_reason = excluded.last_terminated_reason,
    last_terminated_exit_code = excluded.last_terminated_exit_code,
    last_terminated_signal = excluded.last_terminated_signal,
    last_terminated_at = excluded.last_terminated_at, updated_at = excluded.updated_at`,
			c.PodUID, c.Name, c.Kind, c.Image, c.ImageTag, c.ImageID, c.ContainerID,
			c.CPURequest, c.CPULimit, c.MemRequest, c.MemLimit, c.CPURequestMillis, c.CPULimitMillis,
			c.MemRequestBytes, c.MemLimitBytes, c.State, c.Reason, c.ExitCode, c.Signal, boolInt(c.Ready), c.RestartCount,
			c.RunningSince, c.LastTerminatedReason, c.LastTerminatedExitCode, c.LastTerminatedSignal,
			c.LastTerminatedAt, c.UpdatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

const conditionColumns = `id, pod_uid, type, status, reason, message, k8s_transition_at, observed_at`

func scanCondition(r rowScanner) (PodCondition, error) {
	var c PodCondition
	err := r.Scan(&c.ID, &c.PodUID, &c.Type, &c.Status, &c.Reason, &c.Message, &c.K8sTransitionAt, &c.ObservedAt)
	return c, err
}

// LoadLatestConditions returns the newest history row per condition type.
func LoadLatestConditions(ctx context.Context, tx *sql.Tx, podUID string) ([]PodCondition, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT `+conditionColumns+` FROM pod_condition_history h
WHERE pod_uid = ? AND id = (SELECT MAX(id) FROM pod_condition_history WHERE pod_uid = h.pod_uid AND type = h.type)
ORDER BY id`, podUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PodCondition
	for rows.Next() {
		c, err := scanCondition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertConditions appends condition history rows.
func InsertConditions(ctx context.Context, tx *sql.Tx, cs []PodCondition) error {
	for _, c := range cs {
		_, err := tx.ExecContext(ctx, `
INSERT INTO pod_condition_history (pod_uid, type, status, reason, message, k8s_transition_at, observed_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, c.PodUID, c.Type, c.Status, c.Reason, c.Message, c.K8sTransitionAt, c.ObservedAt)
		if err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestLatestConditionPerType|TestUpsertOverwritesStatusColumns' -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/store/ingest_sql.go internal/store/ingest_sql_test.go && git commit -m "store: add pod snapshot load and upsert helpers"`

---

### Task 2: Store incident, history, event and artifact helpers

**Files:**
- Modify: `internal/store/ingest_sql.go`
- Test: `internal/store/ingest_sql_test.go`

**Interfaces:**
- Consumes: `Incident`, `ContainerStateHistory`, `K8sEvent`, `Artifact`, `SubjectPod`, `SubjectJob`, `boolInt`, `rowScanner`.
- Produces: `func LoadIncidentsForSubject(ctx, tx, uid string) ([]Incident, error)` (pod_uid or job_uid equal to uid, open and closed, ordered by `id`); `func OpenIncident(ctx, tx, inc Incident) (int64, error)`; `func AttachIncident(ctx, tx, id int64, reopen bool, lastReason string, lastMessage *string, lastSeenAt string) error`; `func CloseIncident(ctx, tx, id int64, reason, closedAt string) error`; `func CloseOpenIncidents(ctx, tx, subjectUID, reason, closedAt string) (int64, error)`; `func SetIncidentWorkload(ctx, tx, podUID, kind, name string) error`; `func SetOpenIncidentMessage(ctx, tx, podUID, category string, message *string) error`; `func InsertHistory(ctx, tx, h ContainerStateHistory) error`; `func AttachEvents(ctx, tx, incidentID int64, involvedUID, container string, category string, sinceTS string) (int64, error)`; `func UpsertEvent(ctx, tx, ev K8sEvent) error`; `func InsertArtifactGap(ctx, tx, a Artifact) error`; `func HasArtifactFile(ctx, tx, podUID, container, kind string, restartCount int64) (bool, error)`; `incidentColumns`, `scanIncident`.

Tests and their trace:
- `TestOpenIncidentIsIdempotentUnderReplay`: storage 5.7 "`INSERT ... ON CONFLICT DO UPDATE` against the partial index, which makes the insert itself idempotent under replay" -- for both subject kinds, and a closed row with the same key does not conflict.
- `TestReopenClearsDismissedKeepsAcknowledged`: storage 6.3 "clear `closed_at`, `close_reason` and `dismissed_at`, keep the row and its `acknowledged_at`".
- `TestEventUpsertKeepsAttachAndBumpsCount`: storage 5.10 "`event_uid` ... upsert key, so `count`/`last_ts` updates land on the same row"; the `COALESCE` decision above.
- `TestAttachEventsRule`: storage 5.10 attach rule (same `involved_uid`; container from `field_path` or pod-level; category equal or `NULL`; `last_ts >= opened_at - stabilization_window`; already attached rows untouched).

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/ingest_sql_test.go`:

```go
func podIncident(podUID, container, category string) Incident {
	ts := clock.Format(testEpoch)
	return Incident{ClusterID: 1, Namespace: "idios-smoke", SubjectKind: SubjectPod, PodUID: ptr(podUID), ContainerName: container,
		WorkloadKind: "Deployment", WorkloadName: "web", Category: category, FirstReason: "r", LastReason: "r",
		Occurrences: 1, OpenedAt: ts, LastSeenAt: ts}
}

func loadIncidents(t *testing.T, s *Store, uid string) []Incident {
	t.Helper()
	var out []Incident
	inTx(t, s, func(tx *sql.Tx) (err error) { out, err = LoadIncidentsForSubject(context.Background(), tx, uid); return err })
	return out
}

func TestOpenIncidentIsIdempotentUnderReplay(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	insertJob(t, s, cid, "j1")
	ctx := context.Background()
	pod := podIncident("p1", "api", CategoryCrash)
	pod.ClusterID = cid
	job := Incident{ClusterID: cid, Namespace: "idios-smoke", SubjectKind: SubjectJob, JobUID: ptr("j1"), WorkloadKind: "Job", WorkloadName: "j",
		Category: CategoryJobFailed, FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", Occurrences: 1, OpenedAt: pod.OpenedAt, LastSeenAt: pod.LastSeenAt}
	cases := []struct {
		name string
		inc  Incident
		uid  string
	}{
		{"pod key", pod, "p1"},
		{"job key", job, "j1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var first, second int64
			replay := c.inc
			replay.LastReason, replay.LastSeenAt = "again", "2026-08-27T12:01:00.000000Z"
			inTx(t, s, func(tx *sql.Tx) (err error) {
				if first, err = OpenIncident(ctx, tx, c.inc); err != nil {
					return err
				}
				second, err = OpenIncident(ctx, tx, replay)
				return err
			})
			if first != second {
				t.Fatalf("replay opened a second incident: %d then %d", first, second)
			}
			got := loadIncidents(t, s, c.uid)
			want := c.inc
			want.ID, want.Occurrences, want.LastReason, want.LastSeenAt = first, 2, "again", replay.LastSeenAt
			if d := cmp.Diff([]Incident{want}, got); d != "" {
				t.Fatal(d)
			}
			inTx(t, s, func(tx *sql.Tx) error { return CloseIncident(ctx, tx, first, CloseRecovered, "2026-08-27T12:02:00.000000Z") })
			var third int64
			inTx(t, s, func(tx *sql.Tx) (err error) { third, err = OpenIncident(ctx, tx, c.inc); return err })
			if third == first {
				t.Fatal("open after close reused the closed row instead of inserting")
			}
		})
	}
}

func TestReopenClearsDismissedKeepsAcknowledged(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	ctx := context.Background()
	inc := podIncident("p1", "api", CategoryCrash)
	inc.ClusterID = cid
	var id int64
	inTx(t, s, func(tx *sql.Tx) (err error) { id, err = OpenIncident(ctx, tx, inc); return err })
	mustExec(t, s, "UPDATE incidents SET closed_at = ?, close_reason = 'recovered', acknowledged_at = ?, dismissed_at = ? WHERE id = ?",
		"2026-08-27T12:05:00.000000Z", "2026-08-27T12:03:00.000000Z", "2026-08-27T12:04:00.000000Z", id)
	inTx(t, s, func(tx *sql.Tx) error {
		return AttachIncident(ctx, tx, id, true, "CrashLoopBackOff", ptr("back-off"), "2026-08-27T12:10:00.000000Z")
	})
	want := inc
	want.ID, want.Occurrences, want.LastReason, want.LastMessage, want.LastSeenAt = id, 2, "CrashLoopBackOff", ptr("back-off"), "2026-08-27T12:10:00.000000Z"
	want.AcknowledgedAt = ptr("2026-08-27T12:03:00.000000Z")
	if d := cmp.Diff([]Incident{want}, loadIncidents(t, s, "p1")); d != "" {
		t.Fatal(d)
	}
}

func testEvent(uid, fieldPath, reason string, cat *string, lastTS string) K8sEvent {
	return K8sEvent{ClusterID: 1, EventUID: "ev-" + reason + "-" + fieldPath + lastTS, Namespace: "idios-smoke", Type: "Warning", InvolvedKind: "Pod",
		InvolvedName: "web", InvolvedUID: uid, FieldPath: fieldPath, Reason: reason, Message: "m", SourceComponent: "kubelet",
		Count: 1, FirstTS: lastTS, LastTS: lastTS, Category: cat, RawJSON: "{}"}
}

func loadEvents(t *testing.T, s *Store) []K8sEvent {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), `
SELECT id, cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, message,
       source_component, count, first_ts, last_ts, category, incident_id, raw_json FROM k8s_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []K8sEvent
	for rows.Next() {
		var e K8sEvent
		if err := rows.Scan(&e.ID, &e.ClusterID, &e.EventUID, &e.Namespace, &e.Type, &e.InvolvedKind, &e.InvolvedName, &e.InvolvedUID,
			&e.FieldPath, &e.Reason, &e.Message, &e.SourceComponent, &e.Count, &e.FirstTS, &e.LastTS, &e.Category, &e.IncidentID, &e.RawJSON); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func TestEventUpsertKeepsAttachAndBumpsCount(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ctx := context.Background()
	ev := testEvent("p1", "spec.containers{api}", "BackOff", nil, "2026-08-27T11:55:00.000000Z")
	ev.ClusterID = cid
	insertPod(t, s, cid, "p1")
	var incID int64
	inTx(t, s, func(tx *sql.Tx) (err error) { incID, err = OpenIncident(ctx, tx, podIncident("p1", "api", CategoryCrash)); return err })
	ev.IncidentID = &incID
	inTx(t, s, func(tx *sql.Tx) error { return UpsertEvent(ctx, tx, ev) })
	bump := ev
	bump.Count, bump.LastTS, bump.Message, bump.IncidentID = 5, "2026-08-27T11:59:30.000000Z", "m5", nil
	inTx(t, s, func(tx *sql.Tx) error { return UpsertEvent(ctx, tx, bump) })
	want := bump
	want.ID, want.IncidentID = 1, &incID
	if d := cmp.Diff([]K8sEvent{want}, loadEvents(t, s)); d != "" {
		t.Fatal(d)
	}
}

func TestAttachEventsRule(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	insertPod(t, s, cid, "p2")
	ctx := context.Background()
	since := "2026-08-27T11:50:00.000000Z"
	events := []K8sEvent{
		testEvent("p1", "spec.containers{api}", "Failed", ptr(CategoryImagePull), "2026-08-27T11:55:00.000000Z"),
		testEvent("p1", "spec.containers{api}", "BackOff", nil, "2026-08-27T11:56:00.000000Z"),
		testEvent("p1", "spec.containers{api}", "Unhealthy", ptr(CategoryProbe), "2026-08-27T11:57:00.000000Z"),
		testEvent("p1", "spec.containers{worker}", "BackOff", nil, "2026-08-27T11:58:00.000000Z"),
		testEvent("p1", "", "Killing", nil, "2026-08-27T11:59:00.000000Z"),
		testEvent("p1", "spec.containers{api}", "BackOff", nil, "2026-08-27T11:49:59.000000Z"),
		testEvent("p2", "spec.containers{api}", "BackOff", nil, "2026-08-27T11:56:30.000000Z"),
		testEvent("p1", "spec.containers{api}", "Pulled", nil, "2026-08-27T11:56:45.000000Z"),
	}
	inTx(t, s, func(tx *sql.Tx) error {
		for i := range events {
			events[i].ClusterID = cid
			if err := UpsertEvent(ctx, tx, events[i]); err != nil {
				return err
			}
		}
		return nil
	})
	var earlier int64
	inTx(t, s, func(tx *sql.Tx) (err error) { earlier, err = OpenIncident(ctx, tx, podIncident("p1", "api", CategoryCrash)); return err })
	mustExec(t, s, "UPDATE k8s_events SET incident_id = ? WHERE event_uid = ?", earlier, events[7].EventUID)

	cases := []struct {
		name      string
		container string
		category  string
		wantIdx   []int
	}{
		{"container incident", "api", CategoryImagePull, []int{0, 1}},
		{"pod-level incident", "", CategoryNodePressure, []int{1, 3, 4}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustExec(t, s, "UPDATE k8s_events SET incident_id = NULL WHERE incident_id <> ?", earlier)
			inc := podIncident("p1", c.container, c.category)
			var id, n int64
			inTx(t, s, func(tx *sql.Tx) (err error) {
				if id, err = OpenIncident(ctx, tx, inc); err != nil {
					return err
				}
				n, err = AttachEvents(ctx, tx, id, "p1", c.container, c.category, since)
				return err
			})
			var got []int
			for i, e := range loadEvents(t, s) {
				if e.IncidentID != nil && *e.IncidentID == id {
					got = append(got, i)
				}
			}
			if d := cmp.Diff(c.wantIdx, got); d != "" {
				t.Error(d)
			}
			if n != int64(len(c.wantIdx)) {
				t.Errorf("AttachEvents reported %d rows, want %d", n, len(c.wantIdx))
			}
			inTx(t, s, func(tx *sql.Tx) error { return CloseIncident(ctx, tx, id, CloseManual, since) })
		})
	}
}
```

The `Pulled` event (index 7) is pre-attached to another incident and must stay there; the `BackOff` at 11:49:59 is before `since`; `p2` is another pod; `Unhealthy` carries `probe` which matches neither incident. The pod-level incident takes container events (`api`, `worker`) and the pod event, per the rule's "or the incident is pod-level".

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'Incident|Event' -v`
Expected: compile error, `undefined: OpenIncident`.

- [ ] **Step 3: Write the helpers**

Append to `internal/store/ingest_sql.go`:

```go
const incidentColumns = `id, cluster_id, namespace, subject_kind, pod_uid, job_uid, container_name, workload_kind, workload_name,
category, first_reason, last_reason, last_message, image, image_tag, image_id, occurrences, opened_at, last_seen_at,
closed_at, close_reason, acknowledged_at, dismissed_at, note`

func scanIncident(r rowScanner) (Incident, error) {
	var i Incident
	err := r.Scan(&i.ID, &i.ClusterID, &i.Namespace, &i.SubjectKind, &i.PodUID, &i.JobUID, &i.ContainerName, &i.WorkloadKind, &i.WorkloadName,
		&i.Category, &i.FirstReason, &i.LastReason, &i.LastMessage, &i.Image, &i.ImageTag, &i.ImageID, &i.Occurrences, &i.OpenedAt, &i.LastSeenAt,
		&i.ClosedAt, &i.CloseReason, &i.AcknowledgedAt, &i.DismissedAt, &i.Note)
	return i, err
}

// LoadIncidentsForSubject returns every incident, open or closed, whose pod
// or job is uid. Closed rows are needed for reopen.
func LoadIncidentsForSubject(ctx context.Context, tx *sql.Tx, uid string) ([]Incident, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+incidentColumns+" FROM incidents WHERE pod_uid = ? OR job_uid = ? ORDER BY id", uid, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// OpenIncident inserts inc and returns its id. A replay that hits the open
// row's partial unique key bumps that row instead of failing.
func OpenIncident(ctx context.Context, tx *sql.Tx, inc Incident) (int64, error) {
	target := "(pod_uid, container_name, category) WHERE closed_at IS NULL AND subject_kind = 'pod'"
	if inc.SubjectKind == SubjectJob {
		target = "(job_uid, category) WHERE closed_at IS NULL AND subject_kind = 'job'"
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
INSERT INTO incidents (cluster_id, namespace, subject_kind, pod_uid, job_uid, container_name, workload_kind, workload_name,
    category, first_reason, last_reason, last_message, image, image_tag, image_id, occurrences, opened_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT `+target+` DO UPDATE SET
    occurrences = incidents.occurrences + 1, last_reason = excluded.last_reason,
    last_message = excluded.last_message, last_seen_at = excluded.last_seen_at
RETURNING id`,
		inc.ClusterID, inc.Namespace, inc.SubjectKind, inc.PodUID, inc.JobUID, inc.ContainerName, inc.WorkloadKind, inc.WorkloadName,
		inc.Category, inc.FirstReason, inc.LastReason, inc.LastMessage, inc.Image, inc.ImageTag, inc.ImageID, inc.Occurrences, inc.OpenedAt, inc.LastSeenAt,
	).Scan(&id)
	return id, err
}

// AttachIncident bumps an incident; with reopen it also clears the close and
// the dismissal, keeping acknowledged_at.
func AttachIncident(ctx context.Context, tx *sql.Tx, id int64, reopen bool, lastReason string, lastMessage *string, lastSeenAt string) error {
	set := ""
	if reopen {
		set = ", closed_at = NULL, close_reason = NULL, dismissed_at = NULL"
	}
	_, err := tx.ExecContext(ctx, `
UPDATE incidents SET occurrences = occurrences + 1, last_reason = ?, last_message = ?, last_seen_at = ?`+set+` WHERE id = ?`,
		lastReason, lastMessage, lastSeenAt, id)
	return err
}

// CloseIncident closes one open incident.
func CloseIncident(ctx context.Context, tx *sql.Tx, id int64, reason, closedAt string) error {
	_, err := tx.ExecContext(ctx, "UPDATE incidents SET closed_at = ?, close_reason = ? WHERE id = ? AND closed_at IS NULL", closedAt, reason, id)
	return err
}

// CloseOpenIncidents closes every open incident on a pod or job and returns
// how many it closed.
func CloseOpenIncidents(ctx context.Context, tx *sql.Tx, subjectUID, reason, closedAt string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
UPDATE incidents SET closed_at = ?, close_reason = ? WHERE closed_at IS NULL AND (pod_uid = ? OR job_uid = ?)`, closedAt, reason, subjectUID, subjectUID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetIncidentWorkload corrects workload_* on a pod's open incidents.
func SetIncidentWorkload(ctx context.Context, tx *sql.Tx, podUID, kind, name string) error {
	_, err := tx.ExecContext(ctx, "UPDATE incidents SET workload_kind = ?, workload_name = ? WHERE pod_uid = ? AND closed_at IS NULL", kind, name, podUID)
	return err
}

// SetOpenIncidentMessage refreshes last_message on a pod's open incident of
// one category without touching last_seen_at.
func SetOpenIncidentMessage(ctx context.Context, tx *sql.Tx, podUID, category string, message *string) error {
	_, err := tx.ExecContext(ctx, "UPDATE incidents SET last_message = ? WHERE pod_uid = ? AND category = ? AND closed_at IS NULL", message, podUID, category)
	return err
}

// InsertHistory appends one container transition row.
func InsertHistory(ctx context.Context, tx *sql.Tx, h ContainerStateHistory) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO container_state_history (pod_uid, container_name, incident_id, image, image_id, container_id, state, reason, exit_code, signal,
    restart_count, category, k8s_started_at, k8s_finished_at, observed_at, gap_reconstructed)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.PodUID, h.ContainerName, h.IncidentID, h.Image, h.ImageID, h.ContainerID, h.State, h.Reason, h.ExitCode, h.Signal,
		h.RestartCount, h.Category, h.K8sStartedAt, h.K8sFinishedAt, h.ObservedAt, boolInt(h.GapReconstructed))
	return err
}

// AttachEvents links unattached events of the subject to incidentID when the
// container from field_path matches (or the incident is pod-level), the
// category is equal or absent, and last_ts is not before sinceTS.
func AttachEvents(ctx context.Context, tx *sql.Tx, incidentID int64, involvedUID, container, category, sinceTS string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
UPDATE k8s_events SET incident_id = ?
WHERE incident_id IS NULL AND involved_uid = ? AND last_ts >= ?
  AND (category IS NULL OR category = ?)
  AND (? = '' OR field_path GLOB ?)`,
		incidentID, involvedUID, sinceTS, category, container, "spec.*{"+container+"}")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpsertEvent writes the event on (cluster_id, event_uid). An existing
// incident_id survives a bump whose own lookup found nothing.
func UpsertEvent(ctx context.Context, tx *sql.Tx, ev K8sEvent) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, message,
    source_component, count, first_ts, last_ts, category, incident_id, raw_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (cluster_id, event_uid) DO UPDATE SET
    type = excluded.type, message = excluded.message, source_component = excluded.source_component,
    count = excluded.count, first_ts = excluded.first_ts, last_ts = excluded.last_ts, category = excluded.category,
    incident_id = COALESCE(excluded.incident_id, k8s_events.incident_id), raw_json = excluded.raw_json`,
		ev.ClusterID, ev.EventUID, ev.Namespace, ev.Type, ev.InvolvedKind, ev.InvolvedName, ev.InvolvedUID, ev.FieldPath, ev.Reason, ev.Message,
		ev.SourceComponent, ev.Count, ev.FirstTS, ev.LastTS, ev.Category, ev.IncidentID, ev.RawJSON)
	return err
}

// InsertArtifactGap records an instance no capture can reach. A second
// report of the same instance is ignored.
func InsertArtifactGap(ctx context.Context, tx *sql.Tx, a Artifact) error {
	_, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO artifacts (pod_uid, incident_id, container_name, kind, restart_count, capture_gap, captured_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, a.PodUID, a.IncidentID, a.ContainerName, a.Kind, a.RestartCount, a.CaptureGap, a.CapturedAt)
	return err
}

// HasArtifactFile reports whether a file was already captured for the key.
func HasArtifactFile(ctx context.Context, tx *sql.Tx, podUID, container, kind string, restartCount int64) (bool, error) {
	var n int64
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM artifacts WHERE pod_uid = ? AND container_name = ? AND kind = ? AND restart_count = ? AND file_path IS NOT NULL`,
		podUID, container, kind, restartCount).Scan(&n)
	return n > 0, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'Incident|Event' -v`
Expected: PASS. If `OpenIncident` fails with `ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint`, the conflict target's `WHERE` must be spelled exactly as in `0001_init.sql` (`closed_at IS NULL AND subject_kind = 'pod'`); SQLite matches partial indexes textually.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/store/ingest_sql.go internal/store/ingest_sql_test.go && git commit -m "store: add incident, history, event and artifact helpers"`

---

### Task 3: Store job, rollout and deletion helpers

**Files:**
- Modify: `internal/store/ingest_sql.go`
- Test: `internal/store/ingest_sql_test.go`

**Interfaces:**
- Consumes: `Job`, `RolloutHistory`, `rowScanner`.
- Produces: `func LoadJob(ctx, tx, uid string) (*Job, error)`; `func UpsertJob(ctx, tx, j Job) error`; `func MarkJobDeleted(ctx, tx, uid, deletedAt string) (int64, error)`; `func UpsertRolloutHistory(ctx, tx, r RolloutHistory) error`; `func MarkReplicaSetDeleted(ctx, tx, replicasetUID, deletedAt string) error`; `func MarkPodDeleted(ctx, tx, uid, deletedAt, source, reason string) error`; `func LoadRolloutRevisions(ctx, tx, replicasetUID string) (own, newest *int64, err error)` (`own` is the pod's ReplicaSet revision, `newest` the highest revision among ReplicaSets with the same non-empty `deployment_uid`; both nil when the ReplicaSet is unknown); `func LoadLiveSiblingCreatedAt(ctx, tx, controllerUID, exceptUID string) ([]string, error)`; `func HasRecentIncidentOrFailure(ctx, tx, podUID, closedSince string) (bool, error)`; `jobColumns`, `scanJob`.

Tests and their trace:
- `TestRolloutUpsertKeepsFirstSeenUpdatesRevision`: storage 5.9 `revision` "updated in place: a rollback re-uses the old RS and bumps its revision"; `first_seen_at` is identity.
- `TestRolloutRevisionsForDeployment`: storage 5.3 "a ReplicaSet in `rollout_history` with the same `deployment_uid` and a higher `revision`"; a bare ReplicaSet (empty `deployment_uid`) has no siblings.

Everything else in this task is exercised by the Task 7 and 8 processor tests, which assert the rows.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/ingest_sql_test.go`:

```go
func rollout(rsUID, rsName, container, depUID string, rev *int64, ts string) RolloutHistory {
	return RolloutHistory{ClusterID: 1, Namespace: "idios-smoke", DeploymentName: "web", DeploymentUID: depUID, ReplicaSetUID: rsUID,
		ReplicaSetName: rsName, ContainerName: container, Image: "web:1", ImageTag: ptr("1"), Revision: rev, FirstSeenAt: ts, LastSeenAt: ts}
}

func loadRollouts(t *testing.T, s *Store) []RolloutHistory {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), `
SELECT id, cluster_id, namespace, deployment_name, deployment_uid, replicaset_uid, replicaset_name, container_name, image, image_tag,
       revision, first_seen_at, last_seen_at, deleted_at FROM rollout_history ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []RolloutHistory
	for rows.Next() {
		var r RolloutHistory
		if err := rows.Scan(&r.ID, &r.ClusterID, &r.Namespace, &r.DeploymentName, &r.DeploymentUID, &r.ReplicaSetUID, &r.ReplicaSetName,
			&r.ContainerName, &r.Image, &r.ImageTag, &r.Revision, &r.FirstSeenAt, &r.LastSeenAt, &r.DeletedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestRolloutUpsertKeepsFirstSeenUpdatesRevision(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ctx := context.Background()
	first := rollout("rs-1", "web-a", "api", "dep-web", ptr[int64](7), "2026-08-27T11:50:00.000000Z")
	first.ClusterID = cid
	second := first
	second.Revision, second.FirstSeenAt, second.LastSeenAt = ptr[int64](9), "2026-08-27T11:55:00.000000Z", "2026-08-27T11:55:00.000000Z"
	inTx(t, s, func(tx *sql.Tx) error { return UpsertRolloutHistory(ctx, tx, first) })
	inTx(t, s, func(tx *sql.Tx) error { return UpsertRolloutHistory(ctx, tx, second) })
	want := second
	want.ID, want.FirstSeenAt = 1, first.FirstSeenAt
	if d := cmp.Diff([]RolloutHistory{want}, loadRollouts(t, s)); d != "" {
		t.Fatal(d)
	}
}

func TestRolloutRevisionsForDeployment(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ctx := context.Background()
	ts := clock.Format(testEpoch)
	rows := []RolloutHistory{
		rollout("rs-1", "web-a", "api", "dep-web", ptr[int64](7), ts),
		rollout("rs-1", "web-a", "worker", "dep-web", ptr[int64](7), ts),
		rollout("rs-2", "web-b", "api", "dep-web", ptr[int64](8), ts),
		rollout("rs-3", "other-a", "api", "dep-other", ptr[int64](12), ts),
		rollout("rs-bare", "standalone", "api", "", nil, ts),
	}
	inTx(t, s, func(tx *sql.Tx) error {
		for _, r := range rows {
			r.ClusterID = cid
			if err := UpsertRolloutHistory(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
	cases := []struct {
		rs         string
		own, newest *int64
	}{
		{"rs-1", ptr[int64](7), ptr[int64](8)},
		{"rs-2", ptr[int64](8), ptr[int64](8)},
		{"rs-bare", nil, nil},
		{"rs-unknown", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.rs, func(t *testing.T) {
			var own, newest *int64
			inTx(t, s, func(tx *sql.Tx) (err error) { own, newest, err = LoadRolloutRevisions(ctx, tx, c.rs); return err })
			if d := cmp.Diff([]*int64{c.own, c.newest}, []*int64{own, newest}); d != "" {
				t.Fatal(d)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run Rollout -v`
Expected: compile error, `undefined: UpsertRolloutHistory`.

- [ ] **Step 3: Write the helpers**

Append to `internal/store/ingest_sql.go`:

```go
const jobColumns = `uid, cluster_id, namespace, name, cronjob_uid, cronjob_name, active, succeeded, failed, backoff_limit, completions,
parallelism, restart_policy, condition_type, condition_reason, condition_message, created_at, started_at, finished_at,
first_seen_at, last_seen_at, deleted_at`

func scanJob(r rowScanner) (Job, error) {
	var j Job
	err := r.Scan(&j.UID, &j.ClusterID, &j.Namespace, &j.Name, &j.CronJobUID, &j.CronJobName, &j.Active, &j.Succeeded, &j.Failed,
		&j.BackoffLimit, &j.Completions, &j.Parallelism, &j.RestartPolicy, &j.ConditionType, &j.ConditionReason, &j.ConditionMessage,
		&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.FirstSeenAt, &j.LastSeenAt, &j.DeletedAt)
	return j, err
}

// LoadJob returns the jobs row for uid, or nil when there is none.
func LoadJob(ctx context.Context, tx *sql.Tx, uid string) (*Job, error) {
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE uid = ?", uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// UpsertJob writes every column of the job row.
func UpsertJob(ctx context.Context, tx *sql.Tx, j Job) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO jobs (`+jobColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (uid) DO UPDATE SET
    cluster_id = excluded.cluster_id, namespace = excluded.namespace, name = excluded.name,
    cronjob_uid = excluded.cronjob_uid, cronjob_name = excluded.cronjob_name, active = excluded.active,
    succeeded = excluded.succeeded, failed = excluded.failed, backoff_limit = excluded.backoff_limit,
    completions = excluded.completions, parallelism = excluded.parallelism, restart_policy = excluded.restart_policy,
    condition_type = excluded.condition_type, condition_reason = excluded.condition_reason,
    condition_message = excluded.condition_message, created_at = excluded.created_at, started_at = excluded.started_at,
    finished_at = excluded.finished_at, first_seen_at = excluded.first_seen_at, last_seen_at = excluded.last_seen_at,
    deleted_at = excluded.deleted_at`,
		j.UID, j.ClusterID, j.Namespace, j.Name, j.CronJobUID, j.CronJobName, j.Active, j.Succeeded, j.Failed, j.BackoffLimit, j.Completions,
		j.Parallelism, j.RestartPolicy, j.ConditionType, j.ConditionReason, j.ConditionMessage, j.CreatedAt, j.StartedAt, j.FinishedAt,
		j.FirstSeenAt, j.LastSeenAt, j.DeletedAt)
	return err
}

// MarkJobDeleted sets deleted_at once and returns how many rows changed.
func MarkJobDeleted(ctx context.Context, tx *sql.Tx, uid, deletedAt string) (int64, error) {
	res, err := tx.ExecContext(ctx, "UPDATE jobs SET deleted_at = ? WHERE uid = ? AND deleted_at IS NULL", deletedAt, uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpsertRolloutHistory writes one (replicaset, container) row; first_seen_at
// is kept and a returning ReplicaSet is undeleted.
func UpsertRolloutHistory(ctx context.Context, tx *sql.Tx, r RolloutHistory) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO rollout_history (cluster_id, namespace, deployment_name, deployment_uid, replicaset_uid, replicaset_name, container_name,
    image, image_tag, revision, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (replicaset_uid, container_name) DO UPDATE SET
    deployment_name = excluded.deployment_name, deployment_uid = excluded.deployment_uid, replicaset_name = excluded.replicaset_name,
    image = excluded.image, image_tag = excluded.image_tag, revision = excluded.revision,
    last_seen_at = excluded.last_seen_at, deleted_at = NULL`,
		r.ClusterID, r.Namespace, r.DeploymentName, r.DeploymentUID, r.ReplicaSetUID, r.ReplicaSetName, r.ContainerName,
		r.Image, r.ImageTag, r.Revision, r.FirstSeenAt, r.LastSeenAt)
	return err
}

// MarkReplicaSetDeleted sets deleted_at on every row of the ReplicaSet.
func MarkReplicaSetDeleted(ctx context.Context, tx *sql.Tx, replicasetUID, deletedAt string) error {
	_, err := tx.ExecContext(ctx, "UPDATE rollout_history SET deleted_at = ? WHERE replicaset_uid = ? AND deleted_at IS NULL", deletedAt, replicasetUID)
	return err
}

// MarkPodDeleted records the deletion on the pod row.
func MarkPodDeleted(ctx context.Context, tx *sql.Tx, uid, deletedAt, source, reason string) error {
	_, err := tx.ExecContext(ctx, "UPDATE pods SET deleted_at = ?, deletion_source = ?, deletion_reason = ? WHERE uid = ?", deletedAt, source, reason, uid)
	return err
}

// LoadRolloutRevisions returns the ReplicaSet's own revision and the highest
// revision among ReplicaSets of the same Deployment. Both are nil when the
// ReplicaSet is unknown or owned by no Deployment.
func LoadRolloutRevisions(ctx context.Context, tx *sql.Tx, replicasetUID string) (own, newest *int64, err error) {
	var depUID string
	err = tx.QueryRowContext(ctx, "SELECT deployment_uid, revision FROM rollout_history WHERE replicaset_uid = ? LIMIT 1", replicasetUID).Scan(&depUID, &own)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil || depUID == "" {
		return nil, nil, err
	}
	err = tx.QueryRowContext(ctx, "SELECT MAX(revision) FROM rollout_history WHERE deployment_uid = ?", depUID).Scan(&newest)
	return own, newest, err
}

// LoadLiveSiblingCreatedAt returns created_at of the controller's other live
// pods.
func LoadLiveSiblingCreatedAt(ctx context.Context, tx *sql.Tx, controllerUID, exceptUID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT created_at FROM pods WHERE controller_uid = ? AND uid <> ? AND deleted_at IS NULL", controllerUID, exceptUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// HasRecentIncidentOrFailure says whether a pod's logs are worth keeping: an
// incident open or closed at or after closedSince, or a container whose
// current or last exit code is non-zero.
func HasRecentIncidentOrFailure(ctx context.Context, tx *sql.Tx, podUID, closedSince string) (bool, error) {
	var n int64
	err := tx.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM incidents WHERE pod_uid = ? AND (closed_at IS NULL OR closed_at >= ?))
     + (SELECT COUNT(*) FROM containers WHERE pod_uid = ? AND (exit_code <> 0 OR last_terminated_exit_code <> 0))`,
		podUID, closedSince, podUID).Scan(&n)
	return n > 0, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run Rollout -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/store/ingest_sql.go internal/store/ingest_sql_test.go && git commit -m "store: add job, rollout and pod deletion helpers"`

---

### Task 4: Deletion reason inference (pure)

**Files:**
- Create: `internal/ingest/deletion.go`
- Test: `internal/ingest/deletion_test.go`

**Interfaces:**
- Consumes: `store.DeletionReason*` constants.
- Produces: `func DeletionReason(controllerKind, createdAt string, ownRevision, newestRevision *int64, siblingCreatedAt []string) string`.

Test and its trace:
- `TestDeletionReasonInference`: storage 5.3 `deletion_reason` paragraph, rule by rule in its stated order, and "sibling count is not an input" (three replicas, one newer -> `replaced`; all older -> `scaled_down`; none -> `unknown`).

- [ ] **Step 1: Write the failing test**

`internal/ingest/deletion_test.go`:

```go
package ingest

import (
	"testing"

	"idios/internal/store"
)

func TestDeletionReasonInference(t *testing.T) {
	created := "2026-08-27T11:45:00.000000Z"
	older := "2026-08-27T11:00:00.000000Z"
	newer := "2026-08-27T11:50:00.000000Z"
	seven, eight := int64(7), int64(8)
	cases := []struct {
		name       string
		kind       string
		own, newest *int64
		siblings   []string
		want       string
	}{
		{"job pod", "Job", nil, nil, []string{newer}, store.DeletionReasonJobPruned},
		{"newer revision of the same deployment", "ReplicaSet", &seven, &eight, []string{newer}, store.DeletionReasonRollout},
		{"same revision, newer sibling", "ReplicaSet", &eight, &eight, []string{older, newer}, store.DeletionReasonReplaced},
		{"same revision, siblings all older", "ReplicaSet", &eight, &eight, []string{older, older}, store.DeletionReasonScaledDown},
		{"sibling created in the same second is not newer", "ReplicaSet", &eight, &eight, []string{created}, store.DeletionReasonScaledDown},
		{"no revision known, newer sibling", "ReplicaSet", nil, nil, []string{newer}, store.DeletionReasonReplaced},
		{"no siblings", "ReplicaSet", &eight, &eight, nil, store.DeletionReasonUnknown},
		{"bare pod", "none", nil, nil, nil, store.DeletionReasonUnknown},
		{"statefulset with newer sibling", "StatefulSet", nil, nil, []string{newer}, store.DeletionReasonReplaced},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeletionReason(c.kind, created, c.own, c.newest, c.siblings); got != c.want {
				t.Fatalf("DeletionReason = %q, want %q", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ingest/ -run TestDeletionReasonInference -v`
Expected: compile error, `undefined: DeletionReason`.

- [ ] **Step 3: Write the function**

`internal/ingest/deletion.go`:

```go
package ingest

import "idios/internal/store"

// DeletionReason infers why a pod went away from rows that are local at
// delete time. Revision, not first_seen_at, decides a rollout: a rollback
// reuses an old ReplicaSet and bumps its revision. Sibling count is not an
// input; on a 3-replica Deployment every deletion leaves two siblings.
func DeletionReason(controllerKind, createdAt string, ownRevision, newestRevision *int64, siblingCreatedAt []string) string {
	if controllerKind == "Job" {
		return store.DeletionReasonJobPruned
	}
	if ownRevision != nil && newestRevision != nil && *newestRevision > *ownRevision {
		return store.DeletionReasonRollout
	}
	if len(siblingCreatedAt) == 0 {
		return store.DeletionReasonUnknown
	}
	for _, s := range siblingCreatedAt {
		if s > createdAt {
			return store.DeletionReasonReplaced
		}
	}
	return store.DeletionReasonScaledDown
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ingest/ -run TestDeletionReasonInference -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/ingest/deletion.go internal/ingest/deletion_test.go && git commit -m "ingest: add DeletionReason"`

---

### Task 5: Processor package and the pod path (rows)

**Files:**
- Create: `internal/processor/processor.go`, `internal/processor/ops.go`, `internal/processor/pod.go`
- Modify: `internal/archtest/deps_test.go:13-19` (`forbidden` map)
- Test: `internal/processor/processor_test.go`, `internal/processor/pod_test.go`

**Interfaces:**
- Consumes: `ingest.DiffPod`, `ingest.PodSnapshot`, `ingest.PodChanges`, `ingest.OwnerResolver`, `incident.Apply`, `incident.Ops`, `incident.ConditionCategory`, every Task 1-2 store helper, `clock.Clock`, `clock.Parse`, `clock.Format`, `store.NoRestartIndex`, `store.GapUnobservable`.
- Produces: `type CaptureRequest struct{ClusterID int64; Namespace, PodUID, PodName, Container, Kind string; RestartCount int64; Previous bool; Trigger string; IncidentID *int64; Pod *corev1.Pod}`; `const TriggerRestart = "restart"`, `TriggerIncidentOpen = "incident_open"`, `TriggerDelete = "delete"`, `TriggerEarlyPrefix = "early:"`; `type CaptureSink interface{ Enqueue(CaptureRequest) }`; `type Processor struct`; `func New(w *store.Writer, clk clock.Clock, sink CaptureSink, stabilizationWindow time.Duration) *Processor`; `func (p *Processor) Pod(ctx context.Context, clusterID int64, pod *corev1.Pod, r ingest.OwnerResolver) error`; unexported `type opsResult struct{openIDs []int64; history map[int]int64}`, `func (p *Processor) execOps(ctx, tx, ops incident.Ops, involvedUID string) (opsResult, error)`, `func touchedByContainer(incidents []store.Incident, ops incident.Ops, res opsResult) (byContainer map[string]*int64, names map[int64]string)`, `func loadSnapshot(ctx, tx, uid string) (*ingest.PodSnapshot, error)`, `func schedulingMessage(pod *corev1.Pod) (*string, bool)`, `func ptr[T any](v T) *T`. In this task `Pod` enqueues nothing; Task 6 adds `podRequests`.

Tests and their trace (one table, `TestPodScenariosWriteRows`):
- crash loop: process doc 4.1 (one event end to end: pod and container upsert, one history row with `incident_id`, one incident).
- image pull s1 -> s2 -> s3: storage 4 first-sight rule (incident with `occurrences = 1`, `opened_at` from `creationTimestamp`, no history row), 6.2 step 2 (attach bumps `occurrences`, updates `last_reason`), and the roadmap's Phase 3 reading (1): a `running` row is never classified.
- restart_count jump: storage 4 "missed transitions are reconstructed" and 5.11 `unobservable` row for the middle instance.
- first sight healthy: storage 4 "no row exists: record the snapshot. No history row" and no incident.
- unschedulable s1 -> s2 -> s3: storage 5.4 "latest wording is kept on the incident's last_message" (the message-only change), 5.4 condition key (no second condition row for s2), one `PodScheduled=True` row for s3.
- evicted: storage 6.1 (category from `status.reason`; the fixture's `phase = Failed` plays no part) with the container `crash` next to it.
- reopen after recovered / never after pod_deleted: storage 6.3 reopen paragraph.
- workload correction: storage 5.3 "the pod row is corrected and so are the `workload_*` columns of every open incident on that pod, in the same transaction".
- error path: process doc 4.1 step 3 "after commit" -- a failed transaction writes nothing (asserted here with an unknown cluster id violating the FK; Task 6 asserts the sink stays empty).

- [ ] **Step 1: Write the package**

`internal/processor/processor.go`:

```go
// Package processor turns one informer callback into one write transaction:
// load the snapshot rows, diff, decide incidents, execute, and after commit
// hand capture requests to the sink. Diffing and lifecycle decisions live in
// ingest and incident; this package only sequences them.
package processor

import (
	"time"

	corev1 "k8s.io/api/core/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

// Capture triggers name why a request was made. Early triggers carry the
// event reason after the prefix.
const (
	TriggerRestart      = "restart"
	TriggerIncidentOpen = "incident_open"
	TriggerDelete       = "delete"
	TriggerEarlyPrefix  = "early:"
)

// CaptureRequest asks the capture pool for one artifact. RestartCount is the
// dead-instance index for log_previous and store.NoRestartIndex otherwise;
// Previous says the dead instance is in lastState. Pod is set for pod_json
// only.
type CaptureRequest struct {
	ClusterID    int64
	Namespace    string
	PodUID       string
	PodName      string
	Container    string
	Kind         string
	RestartCount int64
	Previous     bool
	Trigger      string
	IncidentID   *int64
	Pod          *corev1.Pod
}

// CaptureSink receives requests after the transaction that produced them
// committed. The capture pool implements it.
type CaptureSink interface {
	Enqueue(CaptureRequest)
}

// Processor handles every watched object kind through one Writer.
type Processor struct {
	w      *store.Writer
	clk    clock.Clock
	sink   CaptureSink
	window time.Duration
}

// New returns a Processor. stabilizationWindow bounds how far back a newly
// opened incident adopts earlier events.
func New(w *store.Writer, clk clock.Clock, sink CaptureSink, stabilizationWindow time.Duration) *Processor {
	return &Processor{w: w, clk: clk, sink: sink, window: stabilizationWindow}
}

func ptr[T any](v T) *T { return &v }
```

`internal/processor/ops.go`:

```go
package processor

import (
	"context"
	"database/sql"
	"fmt"

	"idios/internal/clock"
	"idios/internal/incident"
	"idios/internal/store"
)

// opsResult maps what execOps wrote: the id of each Open in order, and the
// incident id each history index belongs to.
type opsResult struct {
	openIDs []int64
	history map[int]int64
}

// execOps runs attaches first so a reopen never collides with an open of the
// same key, then opens (adopting earlier events of the subject), then closes.
func (p *Processor) execOps(ctx context.Context, tx *sql.Tx, ops incident.Ops, involvedUID string) (opsResult, error) {
	res := opsResult{history: map[int]int64{}}
	for _, a := range ops.Attach {
		if err := store.AttachIncident(ctx, tx, a.IncidentID, a.Reopen, a.LastReason, a.LastMessage, a.LastSeenAt); err != nil {
			return res, err
		}
		for _, i := range a.HistoryIndexes {
			res.history[i] = a.IncidentID
		}
	}
	for _, o := range ops.Open {
		id, err := store.OpenIncident(ctx, tx, o.Incident)
		if err != nil {
			return res, err
		}
		res.openIDs = append(res.openIDs, id)
		for _, i := range o.HistoryIndexes {
			res.history[i] = id
		}
		openedAt, err := clock.Parse(o.Incident.OpenedAt)
		if err != nil {
			return res, fmt.Errorf("incident %d opened_at %q: %w", id, o.Incident.OpenedAt, err)
		}
		since := clock.Format(openedAt.Add(-p.window))
		if _, err := store.AttachEvents(ctx, tx, id, involvedUID, o.Incident.ContainerName, o.Incident.Category, since); err != nil {
			return res, err
		}
	}
	for _, c := range ops.Close {
		if err := store.CloseIncident(ctx, tx, c.IncidentID, c.Reason, c.ClosedAt); err != nil {
			return res, err
		}
	}
	return res, nil
}

// touchedByContainer returns, per container name, the incident this event
// opened or attached to, and the container name of every incident id known.
func touchedByContainer(incidents []store.Incident, ops incident.Ops, res opsResult) (byContainer map[string]*int64, names map[int64]string) {
	names = map[int64]string{}
	for _, inc := range incidents {
		names[inc.ID] = inc.ContainerName
	}
	byContainer = map[string]*int64{}
	for i, o := range ops.Open {
		id := res.openIDs[i]
		names[id] = o.Incident.ContainerName
		byContainer[o.Incident.ContainerName] = &id
	}
	for _, a := range ops.Attach {
		id := a.IncidentID
		byContainer[names[id]] = &id
	}
	return byContainer, names
}
```

`internal/processor/pod.go`:

```go
package processor

import (
	"context"
	"database/sql"

	corev1 "k8s.io/api/core/v1"

	"idios/internal/clock"
	"idios/internal/incident"
	"idios/internal/ingest"
	"idios/internal/store"
)

// Pod handles an add or update. The store snapshot, not the informer's old
// object, is the baseline, so cold start, relist and steady state share one
// path.
func (p *Processor) Pod(ctx context.Context, clusterID int64, pod *corev1.Pod, r ingest.OwnerResolver) error {
	now := p.clk.Now()
	nowS := clock.Format(now)
	uid := string(pod.UID)
	return p.w.Tx(ctx, func(tx *sql.Tx) error {
		snap, err := loadSnapshot(ctx, tx, uid)
		if err != nil {
			return err
		}
		incidents, err := store.LoadIncidentsForSubject(ctx, tx, uid)
		if err != nil {
			return err
		}
		changes := ingest.DiffPod(snap, pod, clusterID, r, now)
		ops := incident.Apply(incidents, changes, now)

		if err := store.UpsertPod(ctx, tx, changes.Pod); err != nil {
			return err
		}
		if err := store.UpsertContainers(ctx, tx, changes.Containers); err != nil {
			return err
		}
		if err := store.InsertConditions(ctx, tx, changes.Conditions); err != nil {
			return err
		}
		res, err := p.execOps(ctx, tx, ops, uid)
		if err != nil {
			return err
		}
		for i, h := range changes.History {
			h.Category = ops.HistoryCategories[i]
			if id, ok := res.history[i]; ok {
				h.IncidentID = &id
			}
			if err := store.InsertHistory(ctx, tx, h); err != nil {
				return err
			}
		}
		if changes.WorkloadChanged {
			if err := store.SetIncidentWorkload(ctx, tx, uid, changes.Pod.WorkloadKind, changes.Pod.WorkloadName); err != nil {
				return err
			}
		}
		if msg, ok := schedulingMessage(pod); ok {
			if err := store.SetOpenIncidentMessage(ctx, tx, uid, store.CategoryScheduling, msg); err != nil {
				return err
			}
		}
		byContainer, _ := touchedByContainer(incidents, ops, res)
		for _, d := range changes.DeadInstances {
			if !d.Unobservable {
				continue
			}
			err := store.InsertArtifactGap(ctx, tx, store.Artifact{
				PodUID: uid, IncidentID: byContainer[d.Container], ContainerName: d.Container, Kind: store.ArtifactLogPrevious,
				RestartCount: d.Index, CaptureGap: ptr(store.GapUnobservable), CapturedAt: nowS,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func loadSnapshot(ctx context.Context, tx *sql.Tx, uid string) (*ingest.PodSnapshot, error) {
	pod, err := store.LoadPod(ctx, tx, uid)
	if err != nil || pod == nil {
		return nil, err
	}
	containers, err := store.LoadContainers(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	conditions, err := store.LoadLatestConditions(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	return &ingest.PodSnapshot{Pod: *pod, Containers: containers, Conditions: conditions}, nil
}

// schedulingMessage returns the current Unschedulable wording. The condition
// key excludes the message, so a node-count change is no transition; the
// incident still shows the latest text.
func schedulingMessage(pod *corev1.Pod) (*string, bool) {
	for _, c := range pod.Status.Conditions {
		if incident.ConditionCategory(string(c.Type), string(c.Status), c.Reason) != store.CategoryScheduling {
			continue
		}
		if c.Message == "" {
			return nil, true
		}
		return ptr(c.Message), true
	}
	return nil, false
}
```

Add to `internal/archtest/deps_test.go` `forbidden` map, after the `internal/incident` line:

```go
	"internal/processor": {"k8s.io/client-go", module + "/internal/capture", module + "/internal/k8s"},
```

- [ ] **Step 2: Write the test helpers and the scenario table**

`internal/processor/processor_test.go`:

```go
package processor

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

const window = 10 * time.Minute

type fakeSink struct{ reqs []CaptureRequest }

func (f *fakeSink) Enqueue(r CaptureRequest) { f.reqs = append(f.reqs, r) }

type noOwners struct{}

func (noOwners) ReplicaSetOwner(string, string) *metav1.OwnerReference { return nil }
func (noOwners) JobOwner(string, string) *metav1.OwnerReference        { return nil }

type deployOwners struct{}

func (deployOwners) ReplicaSetOwner(string, string) *metav1.OwnerReference {
	return &metav1.OwnerReference{Kind: "Deployment", Name: "web", UID: "dep-web"}
}
func (deployOwners) JobOwner(string, string) *metav1.OwnerReference { return nil }

type harness struct {
	p    *Processor
	s    *store.Store
	sink *fakeSink
	clk  *clock.Fake
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clk := clock.NewFake(testNow)
	if err := s.Migrate(context.Background(), clk); err != nil {
		t.Fatal(err)
	}
	h := &harness{s: s, sink: &fakeSink{}, clk: clk}
	h.exec(t, `INSERT INTO clusters (id, identity, name, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'c', 'orbstack', 'https://127.0.0.1:26443', ?)`, clock.Format(testNow))
	h.p = New(s.Writer, clk, h.sink, window)
	return h
}

func (h *harness) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	err := h.s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), query, args...)
		return err
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, query)
	}
}

func (h *harness) tx(t *testing.T, fn func(tx *sql.Tx) error) {
	t.Helper()
	if err := h.s.Writer.Tx(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "ingest", "testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func loadPod(t *testing.T, path string) *corev1.Pod {
	t.Helper()
	var pod corev1.Pod
	fixture(t, path, &pod)
	return &pod
}

func (h *harness) incidents(t *testing.T, uid string) []store.Incident {
	t.Helper()
	var out []store.Incident
	h.tx(t, func(tx *sql.Tx) (err error) { out, err = store.LoadIncidentsForSubject(context.Background(), tx, uid); return err })
	return out
}

func (h *harness) pod(t *testing.T, uid string) *store.Pod {
	t.Helper()
	var out *store.Pod
	h.tx(t, func(tx *sql.Tx) (err error) { out, err = store.LoadPod(context.Background(), tx, uid); return err })
	return out
}

func (h *harness) history(t *testing.T) []store.ContainerStateHistory {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT id, pod_uid, container_name, incident_id, image, image_id, container_id, state, reason, exit_code, signal, restart_count,
       category, k8s_started_at, k8s_finished_at, observed_at, gap_reconstructed FROM container_state_history ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []store.ContainerStateHistory
	for rows.Next() {
		var r store.ContainerStateHistory
		var gap int64
		if err := rows.Scan(&r.ID, &r.PodUID, &r.ContainerName, &r.IncidentID, &r.Image, &r.ImageID, &r.ContainerID, &r.State, &r.Reason,
			&r.ExitCode, &r.Signal, &r.RestartCount, &r.Category, &r.K8sStartedAt, &r.K8sFinishedAt, &r.ObservedAt, &gap); err != nil {
			t.Fatal(err)
		}
		r.GapReconstructed = gap == 1
		out = append(out, r)
	}
	return out
}

func (h *harness) artifacts(t *testing.T) []store.Artifact {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT id, pod_uid, incident_id, container_name, kind, restart_count, file_path, size_bytes, truncated, captured_early, capture_gap, capture_note, captured_at
FROM artifacts ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []store.Artifact
	for rows.Next() {
		var a store.Artifact
		var trunc, early int64
		if err := rows.Scan(&a.ID, &a.PodUID, &a.IncidentID, &a.ContainerName, &a.Kind, &a.RestartCount, &a.FilePath, &a.SizeBytes, &trunc, &early,
			&a.CaptureGap, &a.CaptureNote, &a.CapturedAt); err != nil {
			t.Fatal(err)
		}
		a.Truncated, a.CapturedEarly = trunc == 1, early == 1
		out = append(out, a)
	}
	return out
}

func (h *harness) conditions(t *testing.T, uid string) []store.PodCondition {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(),
		"SELECT id, pod_uid, type, status, reason, message, k8s_transition_at, observed_at FROM pod_condition_history WHERE pod_uid = ? ORDER BY id", uid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []store.PodCondition
	for rows.Next() {
		var c store.PodCondition
		if err := rows.Scan(&c.ID, &c.PodUID, &c.Type, &c.Status, &c.Reason, &c.Message, &c.K8sTransitionAt, &c.ObservedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// seedIncident opens an incident with the given close state so reopen rules
// can be exercised without a fixture sequence long enough to produce one.
func (h *harness) seedIncident(t *testing.T, podUID, container, category string, closeReason *string) int64 {
	t.Helper()
	var id int64
	h.tx(t, func(tx *sql.Tx) (err error) {
		id, err = store.OpenIncident(context.Background(), tx, store.Incident{
			ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr(podUID), ContainerName: container,
			WorkloadKind: "ReplicaSet", WorkloadName: "web-7d9f8c6b5", Category: category, FirstReason: "Error", LastReason: "Error",
			Occurrences: 3, OpenedAt: "2026-08-27T10:00:00.000000Z", LastSeenAt: "2026-08-27T10:30:00.000000Z",
		})
		if err != nil || closeReason == nil {
			return err
		}
		return store.CloseIncident(context.Background(), tx, id, *closeReason, "2026-08-27T10:45:00.000000Z")
	})
	return id
}

func diff(t *testing.T, want, got any) {
	t.Helper()
	if d := cmp.Diff(want, got, cmpopts.EquateEmpty()); d != "" {
		t.Error(d)
	}
}
```

`internal/processor/pod_test.go`:

```go
package processor

import (
	"context"
	"testing"

	"idios/internal/clock"
	"idios/internal/ingest"
	"idios/internal/store"
)

type step struct {
	file string
	r    ingest.OwnerResolver
}

func steps(files ...string) []step {
	var out []step
	for _, f := range files {
		out = append(out, step{f, noOwners{}})
	}
	return out
}

func (h *harness) feed(t *testing.T, s []step) {
	t.Helper()
	for _, st := range s {
		if err := h.p.Pod(context.Background(), 1, loadPod(t, st.file), st.r); err != nil {
			t.Fatalf("%s: %v", st.file, err)
		}
	}
}

const (
	web    = "registry.example.com/web:1.4.2"
	webID  = "registry.example.com/web@sha256:1111"
	webTag = "1.4.2"
)

func podIncident(id int64, podUID, workloadKind, workloadName, container, category, first, last, openedAt string) store.Incident {
	return store.Incident{
		ID: id, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr(podUID), ContainerName: container,
		WorkloadKind: workloadKind, WorkloadName: workloadName, Category: category, FirstReason: first, LastReason: last,
		Occurrences: 1, OpenedAt: openedAt, LastSeenAt: clock.Format(testNow),
	}
}

func withImage(i store.Incident, image string, tag, id *string) store.Incident {
	i.Image, i.ImageTag, i.ImageID = ptr(image), tag, id
	return i
}

func TestPodScenariosWriteRows(t *testing.T) {
	now := clock.Format(testNow)
	crash := withImage(podIncident(1, "pod-crash", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "CrashLoopBackOff", "CrashLoopBackOff", "2026-08-27T11:55:00.000000Z"), web, ptr(webTag), ptr(webID))
	crashRow := store.ContainerStateHistory{ID: 1, PodUID: "pod-crash", ContainerName: "api", IncidentID: ptr[int64](1), Image: web, ImageID: ptr(webID),
		ContainerID: ptr("containerd://aaa"), State: store.StateWaiting, Reason: ptr("CrashLoopBackOff"), RestartCount: 1, Category: ptr(store.CategoryCrash), ObservedAt: now}
	pull := withImage(podIncident(1, "pod-pull", "ReplicaSet", "web-66c9d", "api", store.CategoryImagePull, "ErrImagePull", "ImagePullBackOff", "2026-08-27T11:45:00.000000Z"), "registry.example.com/web:does-not-exist", ptr("does-not-exist"), nil)
	pull.Occurrences = 2
	jump := withImage(podIncident(1, "pod-jump", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "CrashLoopBackOff", "Error", "2026-08-27T11:40:00.000000Z"), web, ptr(webTag), ptr(webID))
	jump.Occurrences = 2
	unsched := podIncident(1, "pod-unsched", "ReplicaSet", "web-2b3c4", "", store.CategoryScheduling, "Unschedulable", "Unschedulable", "2026-08-27T11:45:01.000000Z")
	unsched.LastMessage = ptr("0/4 nodes are available: 4 Insufficient cpu. preemption: 0/4 nodes are available: 4 No preemption victims found for incoming pod.")
	evictCrash := withImage(podIncident(1, "pod-evict", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "Error", "Error", "2026-08-27T11:57:30.000000Z"), web, ptr(webTag), ptr(webID))
	evictPod := podIncident(2, "pod-evict", "ReplicaSet", "web-7d9f8c6b5", "", store.CategoryNodePressure, "Evicted", "Evicted", now)
	evictPod.LastMessage = ptr("The node was low on resource: memory. Threshold quantity: 100Mi, available: 52Mi. Container api was using 900Mi, request is 0, has larger consumption of memory.")
	reopened := store.Incident{
		ID: 1, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api",
		WorkloadKind: "ReplicaSet", WorkloadName: "web-7d9f8c6b5", Category: store.CategoryCrash, FirstReason: "Error", LastReason: "CrashLoopBackOff",
		Occurrences: 4, OpenedAt: "2026-08-27T10:00:00.000000Z", LastSeenAt: now,
	}
	deletedStays := reopened
	deletedStays.Occurrences, deletedStays.LastReason, deletedStays.LastSeenAt = 3, "Error", "2026-08-27T10:30:00.000000Z"
	deletedStays.ClosedAt, deletedStays.CloseReason = ptr("2026-08-27T10:45:00.000000Z"), ptr(store.ClosePodDeleted)
	crashAfterDeleted := crash
	crashAfterDeleted.ID = 2
	corrected := pull
	corrected.WorkloadKind, corrected.WorkloadName = "Deployment", "web"

	cases := []struct {
		name          string
		seedClose     *string
		steps         []step
		uid           string
		wantIncidents []store.Incident
		wantHistory   []store.ContainerStateHistory
		wantArtifacts []store.Artifact
		wantWorkload  [2]string
	}{
		{"crash loop opens one incident and one history row", nil, steps("crash-loop/before.json", "crash-loop/after.json"), "pod-crash",
			[]store.Incident{crash}, []store.ContainerStateHistory{crashRow}, nil, [2]string{"ReplicaSet", "web-7d9f8c6b5"}},
		{"image pull: first sight opens, back-off attaches, running is not classified", nil, steps("image-pull/s1.json", "image-pull/s2.json", "image-pull/s3.json"), "pod-pull",
			[]store.Incident{pull},
			[]store.ContainerStateHistory{
				{ID: 1, PodUID: "pod-pull", ContainerName: "api", IncidentID: ptr[int64](1), Image: "registry.example.com/web:does-not-exist", State: store.StateWaiting,
					Reason: ptr("ImagePullBackOff"), Category: ptr(store.CategoryImagePull), ObservedAt: now},
				{ID: 2, PodUID: "pod-pull", ContainerName: "api", Image: "registry.example.com/web:does-not-exist", ImageID: ptr("registry.example.com/web@sha256:4444"),
					ContainerID: ptr("containerd://ppp"), State: store.StateRunning, K8sStartedAt: ptr("2026-08-27T11:59:00.000000Z"), ObservedAt: now},
			}, nil, [2]string{"ReplicaSet", "web-66c9d"}},
		{"restart jump attaches the reconstructed row and marks the middle instance unobservable", nil, steps("restart-jump/before.json", "restart-jump/after.json"), "pod-jump",
			[]store.Incident{jump},
			[]store.ContainerStateHistory{
				{ID: 1, PodUID: "pod-jump", ContainerName: "api", IncidentID: ptr[int64](1), Image: web, ImageID: ptr(webID), ContainerID: ptr("containerd://j3"),
					State: store.StateTerminated, Reason: ptr("Error"), ExitCode: ptr[int64](1), Signal: ptr[int64](0), RestartCount: 3, Category: ptr(store.CategoryCrash),
					K8sStartedAt: ptr("2026-08-27T11:57:00.000000Z"), K8sFinishedAt: ptr("2026-08-27T11:58:00.000000Z"), ObservedAt: now, GapReconstructed: true},
				{ID: 2, PodUID: "pod-jump", ContainerName: "api", Image: web, ImageID: ptr(webID), ContainerID: ptr("containerd://j4"),
					State: store.StateRunning, RestartCount: 4, K8sStartedAt: ptr("2026-08-27T11:59:00.000000Z"), ObservedAt: now},
			},
			[]store.Artifact{{ID: 1, PodUID: "pod-jump", IncidentID: ptr[int64](1), ContainerName: "api", Kind: store.ArtifactLogPrevious, RestartCount: 2,
				CaptureGap: ptr(store.GapUnobservable), CapturedAt: now}},
			[2]string{"ReplicaSet", "web-7d9f8c6b5"}},
		{"first sight healthy records the snapshot only", nil, steps("first-sight-healthy/pod.json"), "pod-fresh", nil, nil, nil, [2]string{"ReplicaSet", "web-7d9f8c6b5"}},
		{"unschedulable keeps the latest message without a new row", nil, steps("unschedulable/s1.json", "unschedulable/s2.json", "unschedulable/s3.json"), "pod-unsched",
			[]store.Incident{unsched},
			[]store.ContainerStateHistory{{ID: 1, PodUID: "pod-unsched", ContainerName: "api", Image: web, ImageID: ptr(webID), ContainerID: ptr("containerd://uuu"),
				State: store.StateRunning, K8sStartedAt: ptr("2026-08-27T11:58:05.000000Z"), ObservedAt: now}},
			nil, [2]string{"ReplicaSet", "web-2b3c4"}},
		{"evicted opens node_pressure from status.reason next to the crash", nil, steps("evicted/before.json", "evicted/after.json"), "pod-evict",
			[]store.Incident{evictCrash, evictPod},
			[]store.ContainerStateHistory{{ID: 1, PodUID: "pod-evict", ContainerName: "api", IncidentID: ptr[int64](1), Image: web, ImageID: ptr(webID), ContainerID: ptr("containerd://eee"),
				State: store.StateTerminated, Reason: ptr("Error"), ExitCode: ptr[int64](137), Signal: ptr[int64](9), Category: ptr(store.CategoryCrash),
				K8sStartedAt: ptr("2026-08-27T11:45:08.000000Z"), K8sFinishedAt: ptr("2026-08-27T11:57:30.000000Z"), ObservedAt: now}},
			nil, [2]string{"ReplicaSet", "web-7d9f8c6b5"}},
		{"recovered incident is reopened", ptr(store.CloseRecovered), steps("crash-loop/before.json", "crash-loop/after.json"), "pod-crash",
			[]store.Incident{reopened}, []store.ContainerStateHistory{crashRow}, nil, [2]string{"ReplicaSet", "web-7d9f8c6b5"}},
		{"pod_deleted incident is never reopened", ptr(store.ClosePodDeleted), steps("crash-loop/before.json", "crash-loop/after.json"), "pod-crash",
			[]store.Incident{deletedStays, crashAfterDeleted},
			[]store.ContainerStateHistory{func() store.ContainerStateHistory { r := crashRow; r.IncidentID = ptr[int64](2); return r }()}, nil, [2]string{"ReplicaSet", "web-7d9f8c6b5"}},
		{"resolved owner corrects the pod and its open incident", nil, []step{{"image-pull/s1.json", noOwners{}}, {"image-pull/s2.json", deployOwners{}}}, "pod-pull",
			[]store.Incident{corrected},
			[]store.ContainerStateHistory{{ID: 1, PodUID: "pod-pull", ContainerName: "api", IncidentID: ptr[int64](1), Image: "registry.example.com/web:does-not-exist", State: store.StateWaiting,
				Reason: ptr("ImagePullBackOff"), Category: ptr(store.CategoryImagePull), ObservedAt: now}},
			nil, [2]string{"Deployment", "web"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			if c.seedClose != nil {
				h.feed(t, c.steps[:1])
				h.seedIncident(t, c.uid, "api", store.CategoryCrash, c.seedClose)
				h.feed(t, c.steps[1:])
			} else {
				h.feed(t, c.steps)
			}
			diff(t, c.wantIncidents, h.incidents(t, c.uid))
			diff(t, c.wantHistory, h.history(t))
			diff(t, c.wantArtifacts, h.artifacts(t))
			pod := h.pod(t, c.uid)
			if pod == nil {
				t.Fatal("pod row missing")
			}
			diff(t, c.wantWorkload, [2]string{pod.WorkloadKind, pod.WorkloadName})
		})
	}
}

func TestUnschedulableConditionRows(t *testing.T) {
	h := newHarness(t)
	h.feed(t, steps("unschedulable/s1.json", "unschedulable/s2.json", "unschedulable/s3.json"))
	now := clock.Format(testNow)
	want := []store.PodCondition{
		{ID: 1, PodUID: "pod-unsched", Type: "PodScheduled", Status: "False", Reason: "Unschedulable",
			Message: ptr("0/3 nodes are available: 3 Insufficient cpu. preemption: 0/3 nodes are available: 3 No preemption victims found for incoming pod."),
			K8sTransitionAt: ptr("2026-08-27T11:45:01.000000Z"), ObservedAt: now},
		{ID: 2, PodUID: "pod-unsched", Type: "PodScheduled", Status: "True", K8sTransitionAt: ptr("2026-08-27T11:58:00.000000Z"), ObservedAt: now},
	}
	diff(t, want, h.conditions(t, "pod-unsched"))
}

func TestFailedTransactionWritesNothing(t *testing.T) {
	h := newHarness(t)
	if err := h.p.Pod(context.Background(), 99, loadPod(t, "crash-loop/before.json"), noOwners{}); err == nil {
		t.Fatal("pod for an unknown cluster id was accepted")
	}
	if pod := h.pod(t, "pod-crash"); pod != nil {
		t.Fatalf("pod row written despite failed transaction: %+v", pod)
	}
}
```

In the reopen rows the seed is inserted after the first fixture (which creates the pod row the incident's FK needs) and before the transition. The seeded incident has `FirstReason = "Error"`, `Occurrences = 3`; the reopen bumps to 4 and sets `LastReason` from the transition.

- [ ] **Step 3: Run tests to verify they fail, then pass**

Run: `go test ./internal/processor/ -v`
Expected before the code exists: compile errors. After Step 1: PASS. If `pod_deleted incident is never reopened` reports a UNIQUE violation, `OpenIncident` inserted while the seeded row was still open: check that the seed's `CloseIncident` ran (the test closes it at `10:45`).

Run: `go test ./internal/archtest/`
Expected: PASS with the new rule.

- [ ] **Step 4: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/processor internal/archtest/deps_test.go && git commit -m "processor: add Pod with snapshot diff and incident execution"`

---

### Task 6: Capture requests from the pod path

**Files:**
- Create: `internal/processor/capture.go`
- Modify: `internal/processor/pod.go`
- Test: `internal/processor/pod_test.go`

**Interfaces:**
- Consumes: `ingest.DeadInstance`, `store.HasArtifactFile`, `touchedByContainer`, `opsResult`, Task 5 types.
- Produces: `func (p *Processor) podRequests(ctx, tx, clusterID int64, pod *corev1.Pod, changes ingest.PodChanges, ops incident.Ops, res opsResult, incidents []store.Incident) ([]CaptureRequest, error)`; `Pod` enqueues the result after commit.

Tests and their trace (`TestPodScenariosEnqueueCaptures`):
- crash loop: process doc 4.1 step 3 (`log_previous` with the dead-instance index and `previous` flag, `log_current` and `pod_json` for the opened incident) and storage 4 dead-instance rules (`waiting` with `restart_count = 1` -> index 1, `previous = true`).
- `restartPolicy: Never` Job pod: process doc 14.1 (`log_previous` with `Previous = false`, index 0).
- dead-instance index when running: process doc 14.1 "`running` with `restart_count = 2` -> index 1"; the second sighting of index 1 is skipped when its file exists (the dedupe decision above).
- restart jump: storage 4 "one log is captured and the middle instance gets an `unobservable` row and no API call".
- first sight config error: storage 6.2 step 4 (`log_current` + `pod_json` on open) and the roadmap note that no `log_previous` exists for a container that never ran.
- unschedulable: storage 6.2 step 4 "(or all containers, for pod-level incidents)".
- first sight healthy: nothing enqueued (process doc 4.1 lists the three triggers; none fires).
- failed transaction: process doc 4.1 step 3 "after commit" -- the sink stays empty.

- [ ] **Step 1: Write the failing tests**

Append to `internal/processor/pod_test.go`:

```go
func request(uid, name, container, kind string, restart int64, previous bool, trigger string, incident *int64) CaptureRequest {
	return CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: uid, PodName: name, Container: container, Kind: kind,
		RestartCount: restart, Previous: previous, Trigger: trigger, IncidentID: incident}
}

func TestPodScenariosEnqueueCaptures(t *testing.T) {
	one := ptr[int64](1)
	cases := []struct {
		name  string
		steps []step
		seed  func(t *testing.T, h *harness)
		want  []CaptureRequest
	}{
		{"crash loop: previous log, current log, manifest", steps("crash-loop/before.json", "crash-loop/after.json"), nil, []CaptureRequest{
			request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogPrevious, 1, true, TriggerRestart, one),
			request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, one),
			request("pod-crash", "web-7d9f8c6b5-abcde", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerRestart, nil),
		}},
		{"job pod: the dead instance is the current state", steps("job-never-error/before.json", "job-never-error/after.json"), nil, []CaptureRequest{
			request("pod-jobfail", "import-28812346-b7c3d", "worker", store.ArtifactLogPrevious, 0, false, TriggerRestart, one),
			request("pod-jobfail", "import-28812346-b7c3d", "worker", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, one),
			request("pod-jobfail", "import-28812346-b7c3d", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerRestart, nil),
		}},
		{"running after restart names the previous index once", steps("dead-index-running/before.json", "dead-index-running/after.json"),
			func(t *testing.T, h *harness) {
				h.exec(t, `INSERT INTO artifacts (pod_uid, container_name, kind, restart_count, file_path, captured_at) VALUES ('pod-idx', 'api', 'log_previous', 1, '1/idios-smoke/pod-idx/api/restart_001.log', ?)`, clock.Format(testNow))
			},
			[]CaptureRequest{
				request("pod-idx", "web-7d9f8c6b5-idx01", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, one),
				request("pod-idx", "web-7d9f8c6b5-idx01", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerIncidentOpen, nil),
				request("pod-idx", "web-7d9f8c6b5-idx01", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerRestart, nil),
			}},
		{"restart jump asks only for the reachable instance", steps("restart-jump/before.json", "restart-jump/after.json"), nil, []CaptureRequest{
			request("pod-jump", "web-7d9f8c6b5-jump1", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, one),
			request("pod-jump", "web-7d9f8c6b5-jump1", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerIncidentOpen, nil),
			request("pod-jump", "web-7d9f8c6b5-jump1", "api", store.ArtifactLogPrevious, 3, true, TriggerRestart, one),
			request("pod-jump", "web-7d9f8c6b5-jump1", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerRestart, nil),
		}},
		{"first sight config error has no dead instance", steps("first-sight-config-error/pod.json"), nil, []CaptureRequest{
			request("pod-cfg-first", "web-5b8c7-cfg02", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, one),
			request("pod-cfg-first", "web-5b8c7-cfg02", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerIncidentOpen, nil),
		}},
		{"pod-level incident captures every container", steps("unschedulable/s1.json"), nil, []CaptureRequest{
			request("pod-unsched", "web-2b3c4-pend1", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, one),
			request("pod-unsched", "web-2b3c4-pend1", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerIncidentOpen, nil),
		}},
		{"healthy first sight enqueues nothing", steps("first-sight-healthy/pod.json"), nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			if c.seed != nil {
				h.feed(t, c.steps[:1])
				c.seed(t, h)
				h.feed(t, c.steps[1:])
			} else {
				h.feed(t, c.steps)
			}
			for i, r := range h.sink.reqs {
				if (r.Pod != nil) != (r.Kind == store.ArtifactPodJSON) {
					t.Errorf("request %d (%s): Pod set = %v", i, r.Kind, r.Pod != nil)
				}
			}
			if d := cmp.Diff(c.want, h.sink.reqs, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(CaptureRequest{}, "Pod")); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestFailedTransactionEnqueuesNothing(t *testing.T) {
	h := newHarness(t)
	_ = h.p.Pod(context.Background(), 99, loadPod(t, "crash-loop/after.json"), noOwners{})
	if len(h.sink.reqs) != 0 {
		t.Fatalf("sink received %d requests from a failed transaction", len(h.sink.reqs))
	}
}
```

Add `"github.com/google/go-cmp/cmp"` and `"github.com/google/go-cmp/cmp/cmpopts"` to the imports of `pod_test.go`.

The sink accumulates across every step of a case. In `dead-index-running` and `restart-jump`, `before.json` is a first sight in `waiting(CrashLoopBackOff)`, which opens a `crash` incident and enqueues its `log_current` and manifest; the seed in `dead-index-running` then runs (the pod row exists) and marks index 1 as already captured, so `after.json` (`running`, `restart_count = 2`, index 1 again) adds only the manifest refresh for its transition. In `restart-jump` the reconstructed row attaches to the first-sight incident (no reopen, so no second `log_current`) and index 3 is the one reachable instance.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/processor/ -run 'Captures|EnqueuesNothing' -v`
Expected: FAIL, the sink is empty in every case that wants requests.

- [ ] **Step 3: Write the derivation and enqueue after commit**

`internal/processor/capture.go`:

```go
package processor

import (
	"context"
	"database/sql"

	corev1 "k8s.io/api/core/v1"

	"idios/internal/incident"
	"idios/internal/ingest"
	"idios/internal/store"
)

// podRequests derives what to capture: the newest dead instance per
// container unless its file already exists (a restart seen as two events
// reports it twice), the live log of every container an incident opened or
// reopened on, and one manifest refresh when anything moved.
func (p *Processor) podRequests(ctx context.Context, tx *sql.Tx, clusterID int64, pod *corev1.Pod, changes ingest.PodChanges, ops incident.Ops, res opsResult, incidents []store.Incident) ([]CaptureRequest, error) {
	base := CaptureRequest{ClusterID: clusterID, Namespace: pod.Namespace, PodUID: string(pod.UID), PodName: pod.Name, RestartCount: store.NoRestartIndex}
	byContainer, names := touchedByContainer(incidents, ops, res)
	var reqs []CaptureRequest
	for _, d := range changes.DeadInstances {
		if d.Unobservable {
			continue
		}
		done, err := store.HasArtifactFile(ctx, tx, base.PodUID, d.Container, store.ArtifactLogPrevious, d.Index)
		if err != nil {
			return nil, err
		}
		if done {
			continue
		}
		r := base
		r.Container, r.Kind, r.RestartCount, r.Previous, r.Trigger, r.IncidentID = d.Container, store.ArtifactLogPrevious, d.Index, d.Previous, TriggerRestart, byContainer[d.Container]
		reqs = append(reqs, r)
	}

	opened := map[int64]bool{}
	for _, id := range res.openIDs {
		opened[id] = true
	}
	for _, a := range ops.Attach {
		if a.Reopen {
			opened[a.IncidentID] = true
		}
	}
	seen := map[string]bool{}
	for _, inc := range incidentsInOrder(res.openIDs, ops.Attach) {
		if !opened[inc] {
			continue
		}
		containers := []string{names[inc]}
		if names[inc] == "" {
			containers = containers[:0]
			for _, c := range changes.Containers {
				containers = append(containers, c.Name)
			}
		}
		for _, name := range containers {
			if seen[name] {
				continue
			}
			seen[name] = true
			r := base
			r.Container, r.Kind, r.Trigger, r.IncidentID = name, store.ArtifactLogCurrent, TriggerIncidentOpen, ptr(inc)
			reqs = append(reqs, r)
		}
	}

	switch {
	case len(changes.History) > 0:
		r := base
		r.Kind, r.Trigger, r.Pod = store.ArtifactPodJSON, TriggerRestart, pod
		reqs = append(reqs, r)
	case len(opened) > 0:
		r := base
		r.Kind, r.Trigger, r.Pod = store.ArtifactPodJSON, TriggerIncidentOpen, pod
		reqs = append(reqs, r)
	}
	return reqs, nil
}

// incidentsInOrder lists opened ids then attached ids, so requests come out
// in a stable order for the pool and the tests.
func incidentsInOrder(openIDs []int64, attaches []incident.Attach) []int64 {
	out := append([]int64(nil), openIDs...)
	for _, a := range attaches {
		out = append(out, a.IncidentID)
	}
	return out
}
```

Modify `Pod` in `internal/processor/pod.go`: declare `var reqs []CaptureRequest` before `p.w.Tx`, replace the final `return nil` inside the closure with:

```go
		reqs, err = p.podRequests(ctx, tx, clusterID, pod, changes, ops, res, incidents)
		return err
```

and after the transaction:

```go
	err := p.w.Tx(ctx, func(tx *sql.Tx) error { ... })
	if err != nil {
		return err
	}
	for _, r := range reqs {
		p.sink.Enqueue(r)
	}
	return nil
```

(`Pod` now assigns the `Tx` result to `err` instead of returning it directly.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/processor/ -v`
Expected: PASS, including Task 5's tests.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/processor && git commit -m "processor: enqueue capture requests after commit"`

---

### Task 7: Job, JobDeleted, ReplicaSet, ReplicaSetDeleted

**Files:**
- Create: `internal/processor/job.go`, `internal/processor/replicaset.go`, `internal/ingest/testdata/replicaset/deploy-rev8.json`
- Test: `internal/processor/job_test.go`, `internal/processor/replicaset_test.go`

**Interfaces:**
- Consumes: `ingest.DiffJob`, `incident.ApplyJob`, `ingest.MapReplicaSet`, Task 3 store helpers, `execOps`.
- Produces: `func (p *Processor) Job(ctx, clusterID int64, job *batchv1.Job) error`; `func (p *Processor) JobDeleted(ctx, uid string) error`; `func (p *Processor) ReplicaSet(ctx, clusterID int64, rs *appsv1.ReplicaSet) error`; `func (p *Processor) ReplicaSetDeleted(ctx, uid string) error`. None enqueues anything.

Tests and their trace:
- `TestJobFailedOpensAndDeleteCloses`: storage 6.1 `job_failed` "Job condition Failed (any reason)", 5.8 `finished_at` from the Failed condition, 6.3 "Job reaches Complete or is deleted -> `job_finished`"; a completing job with no incident writes only its row.
- `TestReplicaSetRowsPerContainerAndDelete`: storage 5.9 "one row per (ReplicaSet, container)", `revision` per ReplicaSet, `deleted_at` from `DeleteFunc`.

- [ ] **Step 1: Add the fixture**

`internal/ingest/testdata/replicaset/deploy-rev8.json` (the next revision of the same Deployment as `deploy.json`, one container changed):

```json
{"metadata":{"name":"web-8e0a9d7c6","namespace":"idios-smoke","uid":"rs-web-2","creationTimestamp":"2026-08-27T11:58:00Z","annotations":{"deployment.kubernetes.io/revision":"8"},"ownerReferences":[{"apiVersion":"apps/v1","kind":"Deployment","name":"web","uid":"dep-web","controller":true}]},
 "spec":{"replicas":3,"template":{"spec":{"containers":[{"name":"api","image":"registry.example.com/web:1.4.3"},{"name":"worker","image":"registry.example.com/worker@sha256:7777"}]}}}}
```

- [ ] **Step 2: Write the failing tests**

`internal/processor/job_test.go`:

```go
package processor

import (
	"context"
	"database/sql"
	"testing"

	batchv1 "k8s.io/api/batch/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

func loadJob(t *testing.T, path string) *batchv1.Job {
	t.Helper()
	var job batchv1.Job
	fixture(t, path, &job)
	return &job
}

func (h *harness) feedJobs(t *testing.T, files ...string) {
	t.Helper()
	for _, f := range files {
		if err := h.p.Job(context.Background(), 1, loadJob(t, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
}

func (h *harness) job(t *testing.T, uid string) *store.Job {
	t.Helper()
	var out *store.Job
	h.tx(t, func(tx *sql.Tx) (err error) { out, err = store.LoadJob(context.Background(), tx, uid); return err })
	return out
}

func TestJobFailedOpensAndDeleteCloses(t *testing.T) {
	now := clock.Format(testNow)
	failed := store.Incident{
		ID: 1, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectJob, JobUID: ptr("job-report-1"),
		WorkloadKind: "CronJob", WorkloadName: "report", Category: store.CategoryJobFailed,
		FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", LastMessage: ptr("Job has reached the specified backoff limit"),
		Occurrences: 1, OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: now,
	}
	closed := failed
	closed.ClosedAt, closed.CloseReason = ptr(now), ptr(store.CloseJobFinished)
	cases := []struct {
		name          string
		files         []string
		uid           string
		wantCondition *string
		wantIncidents []store.Incident
		wantAfterDel  []store.Incident
	}{
		{"failed job", []string{"job-failed/before.json", "job-failed/after.json"}, "job-report-1", ptr("Failed"), []store.Incident{failed}, []store.Incident{closed}},
		{"completed job", []string{"job-complete/before.json", "job-complete/after.json"}, "job-report-2", ptr("Complete"), nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.feedJobs(t, c.files...)
			job := h.job(t, c.uid)
			if job == nil {
				t.Fatal("job row missing")
			}
			diff(t, c.wantCondition, job.ConditionType)
			diff(t, c.wantIncidents, h.incidents(t, c.uid))
			if err := h.p.JobDeleted(context.Background(), c.uid); err != nil {
				t.Fatal(err)
			}
			if err := h.p.JobDeleted(context.Background(), c.uid); err != nil {
				t.Fatal(err)
			}
			diff(t, ptr(now), h.job(t, c.uid).DeletedAt)
			diff(t, c.wantAfterDel, h.incidents(t, c.uid))
			if len(h.sink.reqs) != 0 {
				t.Errorf("job path enqueued %d requests", len(h.sink.reqs))
			}
		})
	}
}
```

`internal/processor/replicaset_test.go`:

```go
package processor

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

func loadReplicaSet(t *testing.T, path string) *appsv1.ReplicaSet {
	t.Helper()
	var rs appsv1.ReplicaSet
	fixture(t, path, &rs)
	return &rs
}

func (h *harness) feedReplicaSets(t *testing.T, files ...string) {
	t.Helper()
	for _, f := range files {
		if err := h.p.ReplicaSet(context.Background(), 1, loadReplicaSet(t, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
}

func (h *harness) rollouts(t *testing.T) []store.RolloutHistory {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT id, cluster_id, namespace, deployment_name, deployment_uid, replicaset_uid, replicaset_name, container_name, image, image_tag,
       revision, first_seen_at, last_seen_at, deleted_at FROM rollout_history ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []store.RolloutHistory
	for rows.Next() {
		var r store.RolloutHistory
		if err := rows.Scan(&r.ID, &r.ClusterID, &r.Namespace, &r.DeploymentName, &r.DeploymentUID, &r.ReplicaSetUID, &r.ReplicaSetName,
			&r.ContainerName, &r.Image, &r.ImageTag, &r.Revision, &r.FirstSeenAt, &r.LastSeenAt, &r.DeletedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestReplicaSetRowsPerContainerAndDelete(t *testing.T) {
	h := newHarness(t)
	h.feedReplicaSets(t, "replicaset/deploy.json", "replicaset/deploy-rev8.json")
	if err := h.p.ReplicaSetDeleted(context.Background(), "rs-web-1"); err != nil {
		t.Fatal(err)
	}
	now := clock.Format(testNow)
	row := func(id int64, rsUID, rsName, container, image string, tag *string, rev int64, deleted *string) store.RolloutHistory {
		return store.RolloutHistory{ID: id, ClusterID: 1, Namespace: "idios-smoke", DeploymentName: "web", DeploymentUID: "dep-web",
			ReplicaSetUID: rsUID, ReplicaSetName: rsName, ContainerName: container, Image: image, ImageTag: tag, Revision: ptr(rev),
			FirstSeenAt: now, LastSeenAt: now, DeletedAt: deleted}
	}
	want := []store.RolloutHistory{
		row(1, "rs-web-1", "web-7d9f8c6b5", "api", "registry.example.com/web:1.4.2", ptr("1.4.2"), 7, ptr(now)),
		row(2, "rs-web-1", "web-7d9f8c6b5", "worker", "registry.example.com/worker@sha256:7777", nil, 7, ptr(now)),
		row(3, "rs-web-2", "web-8e0a9d7c6", "api", "registry.example.com/web:1.4.3", ptr("1.4.3"), 8, nil),
		row(4, "rs-web-2", "web-8e0a9d7c6", "worker", "registry.example.com/worker@sha256:7777", nil, 8, nil),
	}
	diff(t, want, h.rollouts(t))
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/processor/ -run 'Job|ReplicaSet' -v`
Expected: compile error, `h.p.Job undefined`.

- [ ] **Step 4: Write the methods**

`internal/processor/job.go`:

```go
package processor

import (
	"context"
	"database/sql"

	batchv1 "k8s.io/api/batch/v1"

	"idios/internal/clock"
	"idios/internal/incident"
	"idios/internal/ingest"
	"idios/internal/store"
)

// Job handles an add or update of a Job.
func (p *Processor) Job(ctx context.Context, clusterID int64, job *batchv1.Job) error {
	now := p.clk.Now()
	uid := string(job.UID)
	return p.w.Tx(ctx, func(tx *sql.Tx) error {
		snap, err := store.LoadJob(ctx, tx, uid)
		if err != nil {
			return err
		}
		incidents, err := store.LoadIncidentsForSubject(ctx, tx, uid)
		if err != nil {
			return err
		}
		changes := ingest.DiffJob(snap, job, clusterID, now)
		ops := incident.ApplyJob(incidents, changes, now)
		if err := store.UpsertJob(ctx, tx, changes.Job); err != nil {
			return err
		}
		_, err = p.execOps(ctx, tx, ops, uid)
		return err
	})
}

// JobDeleted marks the job gone and closes its incidents. A repeat is a
// no-op.
func (p *Processor) JobDeleted(ctx context.Context, uid string) error {
	nowS := clock.Format(p.clk.Now())
	return p.w.Tx(ctx, func(tx *sql.Tx) error {
		n, err := store.MarkJobDeleted(ctx, tx, uid, nowS)
		if err != nil || n == 0 {
			return err
		}
		_, err = store.CloseOpenIncidents(ctx, tx, uid, store.CloseJobFinished, nowS)
		return err
	})
}
```

`internal/processor/replicaset.go`:

```go
package processor

import (
	"context"
	"database/sql"

	appsv1 "k8s.io/api/apps/v1"

	"idios/internal/clock"
	"idios/internal/ingest"
	"idios/internal/store"
)

// ReplicaSet upserts one rollout_history row per template container.
func (p *Processor) ReplicaSet(ctx context.Context, clusterID int64, rs *appsv1.ReplicaSet) error {
	rows := ingest.MapReplicaSet(rs, clusterID, p.clk.Now())
	return p.w.Tx(ctx, func(tx *sql.Tx) error {
		for _, r := range rows {
			if err := store.UpsertRolloutHistory(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplicaSetDeleted marks the ReplicaSet's rows gone.
func (p *Processor) ReplicaSetDeleted(ctx context.Context, uid string) error {
	nowS := clock.Format(p.clk.Now())
	return p.w.Tx(ctx, func(tx *sql.Tx) error {
		return store.MarkReplicaSetDeleted(ctx, tx, uid, nowS)
	})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/processor/ -run 'Job|ReplicaSet' -v`
Expected: PASS.

- [ ] **Step 6: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/processor internal/ingest/testdata/replicaset/deploy-rev8.json && git commit -m "processor: add Job, ReplicaSet and their delete paths"`

---

### Task 8: PodDeleted

**Files:**
- Create: `internal/processor/delete.go`
- Test: `internal/processor/delete_test.go`

**Interfaces:**
- Consumes: `ingest.DeletionReason`, `store.LoadRolloutRevisions`, `store.LoadLiveSiblingCreatedAt`, `store.MarkPodDeleted`, `store.HasRecentIncidentOrFailure`, `store.CloseOpenIncidents`, `store.LoadContainers`, `store.DeletionSource*`, `TriggerDelete`.
- Produces: `func (p *Processor) PodDeleted(ctx context.Context, uid, source string) error`.

Tests and their trace (`TestPodDeletedScenarios`):
- watch delete of a pod with an open incident: process doc 4.3 (`deleted_at`, `deletion_source = 'watch'`, `deletion_reason`, close every open incident with `pod_deleted`, then a delete-trigger request per container because the pod had an open incident).
- healthy pod under a newer ReplicaSet revision: storage 5.3 `rollout`; healthy pod dropped (no request), process doc 4.3 "otherwise drop".
- healthy pod with a newer live sibling: storage 5.3 `replaced`.
- Job pod: storage 5.3 `job_pruned`; its container exited non-zero so the request is enqueued even though the incident already... no: the incident is open (crash), and both conditions hold; the row asserts the request carries that incident.
- reconcile source: storage 4 "marked deleted with `deleted_at = now` and `deletion_source = 'reconcile'`" and the decision that reconcile deletes enqueue nothing.
- unknown uid, repeated delete: the idempotency decision (`DeletedFinalStateUnknown` for a pod never stored).

- [ ] **Step 1: Write the failing test**

`internal/processor/delete_test.go`:

```go
package processor

import (
	"context"
	"testing"

	"idios/internal/clock"
	"idios/internal/store"
)

func TestPodDeletedScenarios(t *testing.T) {
	now := clock.Format(testNow)
	cases := []struct {
		name        string
		prepare     func(t *testing.T, h *harness)
		uid, source string
		wantPod     *[3]string
		wantClosed  int
		wantReqs    []CaptureRequest
	}{
		{"watch delete closes incidents and keeps the logs", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
		}, "pod-crash", store.DeletionSourceWatch, &[3]string{now, store.DeletionSourceWatch, store.DeletionReasonUnknown}, 1,
			[]CaptureRequest{request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerDelete, ptr[int64](1))}},
		{"healthy pod under a newer revision is a rollout and is dropped", func(t *testing.T, h *harness) {
			h.feedReplicaSets(t, "replicaset/deploy.json", "replicaset/deploy-rev8.json")
			h.feed(t, steps("crash-loop/before.json"))
		}, "pod-crash", store.DeletionSourceWatch, &[3]string{now, store.DeletionSourceWatch, store.DeletionReasonRollout}, 0, nil},
		{"healthy pod with a newer live sibling was replaced", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json"))
			h.exec(t, `INSERT INTO pods (uid, cluster_id, namespace, name, phase, controller_kind, controller_uid, created_at, first_seen_at, last_seen_at)
VALUES ('pod-sib', 1, 'idios-smoke', 'web-7d9f8c6b5-zzzzz', 'Running', 'ReplicaSet', 'rs-web-1', '2026-08-27T11:50:00.000000Z', ?, ?)`, now, now)
		}, "pod-crash", store.DeletionSourceWatch, &[3]string{now, store.DeletionSourceWatch, store.DeletionReasonReplaced}, 0, nil},
		{"job pod is pruned and its failed log is kept", func(t *testing.T, h *harness) {
			h.feed(t, steps("job-never-error/before.json", "job-never-error/after.json"))
		}, "pod-jobfail", store.DeletionSourceWatch, &[3]string{now, store.DeletionSourceWatch, store.DeletionReasonJobPruned}, 1,
			[]CaptureRequest{request("pod-jobfail", "import-28812346-b7c3d", "worker", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerDelete, ptr[int64](1))}},
		{"reconcile closes but enqueues nothing", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
		}, "pod-crash", store.DeletionSourceReconcile, &[3]string{now, store.DeletionSourceReconcile, store.DeletionReasonUnknown}, 1, nil},
		{"unknown pod is ignored", func(*testing.T, *harness) {}, "pod-never-seen", store.DeletionSourceWatch, nil, 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			c.prepare(t, h)
			h.sink.reqs = nil
			if err := h.p.PodDeleted(context.Background(), c.uid, c.source); err != nil {
				t.Fatal(err)
			}
			if err := h.p.PodDeleted(context.Background(), c.uid, c.source); err != nil {
				t.Fatal(err)
			}
			pod := h.pod(t, c.uid)
			if c.wantPod == nil {
				if pod != nil {
					t.Fatalf("pod row appeared: %+v", pod)
				}
			} else {
				if pod == nil || pod.DeletedAt == nil || pod.DeletionSource == nil || pod.DeletionReason == nil {
					t.Fatalf("deletion columns not set: %+v", pod)
				}
				diff(t, *c.wantPod, [3]string{*pod.DeletedAt, *pod.DeletionSource, *pod.DeletionReason})
			}
			closed := 0
			for _, inc := range h.incidents(t, c.uid) {
				if inc.ClosedAt == nil {
					t.Errorf("incident %d still open", inc.ID)
				}
				if inc.CloseReason != nil && *inc.CloseReason == store.ClosePodDeleted && *inc.ClosedAt == now {
					closed++
				}
			}
			if closed != c.wantClosed {
				t.Errorf("closed with pod_deleted: %d, want %d", closed, c.wantClosed)
			}
			if d := cmp.Diff(c.wantReqs, h.sink.reqs, cmpopts.EquateEmpty()); d != "" {
				t.Error(d)
			}
		})
	}
}
```

Add `"github.com/google/go-cmp/cmp"` and `"github.com/google/go-cmp/cmp/cmpopts"` to the imports. The second `PodDeleted` call in every case asserts idempotency: nothing changes and nothing more is enqueued.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/processor/ -run TestPodDeletedScenarios -v`
Expected: compile error, `h.p.PodDeleted undefined`.

- [ ] **Step 3: Write the method**

`internal/processor/delete.go`:

```go
package processor

import (
	"context"
	"database/sql"

	"idios/internal/clock"
	"idios/internal/ingest"
	"idios/internal/store"
)

// PodDeleted records the removal, infers why, closes the pod's incidents
// and, for a watch delete of a pod worth keeping, asks for each container's
// last log so the pool can fall back to its early-capture cache.
func (p *Processor) PodDeleted(ctx context.Context, uid, source string) error {
	now := p.clk.Now()
	nowS := clock.Format(now)
	var reqs []CaptureRequest
	err := p.w.Tx(ctx, func(tx *sql.Tx) error {
		pod, err := store.LoadPod(ctx, tx, uid)
		if err != nil || pod == nil || pod.DeletedAt != nil {
			return err
		}
		var own, newest *int64
		var siblings []string
		if pod.ControllerUID != "" {
			if own, newest, err = store.LoadRolloutRevisions(ctx, tx, pod.ControllerUID); err != nil {
				return err
			}
			if siblings, err = store.LoadLiveSiblingCreatedAt(ctx, tx, pod.ControllerUID, uid); err != nil {
				return err
			}
		}
		reason := ingest.DeletionReason(pod.ControllerKind, pod.CreatedAt, own, newest, siblings)
		if err := store.MarkPodDeleted(ctx, tx, uid, nowS, source, reason); err != nil {
			return err
		}
		keep, err := store.HasRecentIncidentOrFailure(ctx, tx, uid, clock.Format(now.Add(-p.window)))
		if err != nil {
			return err
		}
		incidents, err := store.LoadIncidentsForSubject(ctx, tx, uid)
		if err != nil {
			return err
		}
		if _, err := store.CloseOpenIncidents(ctx, tx, uid, store.ClosePodDeleted, nowS); err != nil {
			return err
		}
		// Reconcile runs at startup with an empty early-capture cache; the
		// requests would only produce pod_deleted gaps.
		if !keep || source != store.DeletionSourceWatch {
			return nil
		}
		open := map[string]*int64{}
		for _, inc := range incidents {
			if inc.ClosedAt == nil {
				open[inc.ContainerName] = ptr(inc.ID)
			}
		}
		containers, err := store.LoadContainers(ctx, tx, uid)
		if err != nil {
			return err
		}
		for _, c := range containers {
			id := open[c.Name]
			if id == nil {
				id = open[""]
			}
			reqs = append(reqs, CaptureRequest{
				ClusterID: pod.ClusterID, Namespace: pod.Namespace, PodUID: uid, PodName: pod.Name, Container: c.Name,
				Kind: store.ArtifactLogCurrent, RestartCount: store.NoRestartIndex, Trigger: TriggerDelete, IncidentID: id,
			})
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, r := range reqs {
		p.sink.Enqueue(r)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/processor/ -run TestPodDeletedScenarios -v`
Expected: PASS. If the `rollout` case reports `unknown`, `LoadRolloutRevisions` was called with the pod's `ControllerUID` (`rs-web-1`); check the fixture's ReplicaSet uid matches.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/processor && git commit -m "processor: add PodDeleted with deletion reason and delete captures"`

---

### Task 9: Event

**Files:**
- Create: `internal/processor/event.go`, `internal/ingest/testdata/event-unhealthy/event.json`, `internal/ingest/testdata/event-killing/event.json`
- Test: `internal/processor/event_test.go`

**Interfaces:**
- Consumes: `ingest.MapEvent`, `ingest.EarlyCaptureReason`, `incident.EventCategory`, `incident.FieldPathContainer`, `incident.ApplyEvent`, `incident.EventRef`, `store.LoadContainer`, `store.UpsertEvent`, `execOps`, `TriggerEarlyPrefix`.
- Produces: `func (p *Processor) Event(ctx context.Context, clusterID int64, ev *corev1.Event) error`; unexported `func hasOpenOn(incidents []store.Incident, container string) bool`.

Tests and their trace (`TestEventScenarios`):
- BackOff attaches to the open crash incident: storage 5.10 attach rule with `category = NULL`, and process doc 4.4 (upsert, classify with `BackOff` staying `NULL`).
- Failed pull event before the pod: storage 5.10 "the `Failed` pull event routinely lands before the status update that opens the incident" -- stored unattached, adopted when `image_pull` opens.
- Unhealthy on a ready container: storage 6.1 `probe` gate (noise) and 6.2 rule 6 (early capture for `Unhealthy`).
- Unhealthy on a not-ready container: storage 6.1 `probe` "this is the only category opened by an Event", 6.2 step 4 (`log_current` on open); no early capture because an incident now exists.
- Killing with no incident: process doc 14.1 "`Killing` event with `type = Normal` triggers early capture".
- Killing on a container with an open incident: 5.10 attach (`Killing` has no category) and 6.2 rule 6 "for a pod with no open incident on that container" -- so no early capture.
- Event for a pod never stored: process doc 4.4 / storage 5.10 "can arrive before the Pod itself" -- stored, unattached, nothing enqueued.

- [ ] **Step 1: Add the fixtures**

`internal/ingest/testdata/event-unhealthy/event.json`:

```json
{"metadata":{"name":"web-7d9f8c6b5-abcde.18a2b3c4d5e6f7c0","namespace":"idios-smoke","uid":"ev-unhealthy-1","creationTimestamp":"2026-08-27T11:58:00Z"},
 "involvedObject":{"kind":"Pod","namespace":"idios-smoke","name":"web-7d9f8c6b5-abcde","uid":"pod-crash","fieldPath":"spec.containers{api}"},
 "reason":"Unhealthy","message":"Readiness probe failed: HTTP probe failed with statuscode: 503","type":"Warning",
 "firstTimestamp":"2026-08-27T11:58:00Z","lastTimestamp":"2026-08-27T11:59:00Z","count":3,
 "source":{"component":"kubelet","host":"node-a"}}
```

`internal/ingest/testdata/event-killing/event.json`:

```json
{"metadata":{"name":"web-7d9f8c6b5-abcde.18a2b3c4d5e6f7d1","namespace":"idios-smoke","uid":"ev-killing-1","creationTimestamp":"2026-08-27T11:59:50Z"},
 "involvedObject":{"kind":"Pod","namespace":"idios-smoke","name":"web-7d9f8c6b5-abcde","uid":"pod-crash","fieldPath":"spec.containers{api}"},
 "reason":"Killing","message":"Stopping container api","type":"Normal",
 "eventTime":"2026-08-27T11:59:50.500000Z",
 "reportingComponent":"kubelet","reportingInstance":"node-a","source":{}}
```

- [ ] **Step 2: Write the failing test**

`internal/processor/event_test.go`:

```go
package processor

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"

	"idios/internal/clock"
	"idios/internal/incident"
	"idios/internal/ingest"
	"idios/internal/store"
)

func loadEvent(t *testing.T, path string) *corev1.Event {
	t.Helper()
	var ev corev1.Event
	fixture(t, path, &ev)
	return &ev
}

func (h *harness) feedEvent(t *testing.T, file string) {
	t.Helper()
	if err := h.p.Event(context.Background(), 1, loadEvent(t, file)); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
}

func (h *harness) events(t *testing.T) []store.K8sEvent {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT id, cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, message,
       source_component, count, first_ts, last_ts, category, incident_id, raw_json FROM k8s_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []store.K8sEvent
	for rows.Next() {
		var e store.K8sEvent
		if err := rows.Scan(&e.ID, &e.ClusterID, &e.EventUID, &e.Namespace, &e.Type, &e.InvolvedKind, &e.InvolvedName, &e.InvolvedUID,
			&e.FieldPath, &e.Reason, &e.Message, &e.SourceComponent, &e.Count, &e.FirstTS, &e.LastTS, &e.Category, &e.IncidentID, &e.RawJSON); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// storedEvent is the row MapEvent produces for a fixture, classified and
// attached as the case expects; the processor adds nothing else.
func storedEvent(t *testing.T, id int64, file string, incidentID *int64) store.K8sEvent {
	t.Helper()
	row, err := ingest.MapEvent(loadEvent(t, file), 1)
	if err != nil {
		t.Fatal(err)
	}
	row.ID, row.Category, row.IncidentID = id, incident.EventCategory(row.Reason), incidentID
	return row
}

func TestEventScenarios(t *testing.T) {
	now := clock.Format(testNow)
	probe := withImage(podIncident(2, "pod-crash", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryProbe, "Unhealthy", "Unhealthy", "2026-08-27T11:59:00.000000Z"), web, ptr(webTag), ptr(webID))
	probe.LastMessage = ptr("Readiness probe failed: HTTP probe failed with statuscode: 503")
	crash := withImage(podIncident(1, "pod-crash", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "CrashLoopBackOff", "CrashLoopBackOff", "2026-08-27T11:55:00.000000Z"), web, ptr(webTag), ptr(webID))
	pull := withImage(podIncident(1, "pod-pull", "ReplicaSet", "web-66c9d", "api", store.CategoryImagePull, "ErrImagePull", "ErrImagePull", "2026-08-27T11:45:00.000000Z"), "registry.example.com/web:does-not-exist", ptr("does-not-exist"), nil)
	early := func(reason string) CaptureRequest {
		return request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerEarlyPrefix+reason, nil)
	}
	cases := []struct {
		name          string
		run           func(t *testing.T, h *harness)
		uid           string
		wantEvents    []store.K8sEvent
		wantIncidents []store.Incident
		wantReqs      []CaptureRequest
	}{
		{"backoff attaches to the open crash incident", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-series/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-series/event.json", ptr[int64](1))}, []store.Incident{crash}, nil},
		{"failed pull event before the pod is adopted when the incident opens", func(t *testing.T, h *harness) {
			h.feedEvent(t, "event-legacy/event.json")
			h.feed(t, steps("image-pull/s1.json"))
			h.sink.reqs = nil
		}, "pod-pull", []store.K8sEvent{storedEvent(t, 1, "event-legacy/event.json", ptr[int64](1))}, []store.Incident{pull}, nil},
		{"unhealthy on a ready container is noise but early-captures", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-unhealthy/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-unhealthy/event.json", nil)}, nil, []CaptureRequest{early("Unhealthy")}},
		{"unhealthy on a not-ready container opens probe", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-unhealthy/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-unhealthy/event.json", ptr[int64](2))}, []store.Incident{crash, probe},
			[]CaptureRequest{request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, ptr[int64](2))}},
		{"killing with no incident early-captures", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-killing/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-killing/event.json", nil)}, nil, []CaptureRequest{early("Killing")}},
		{"killing on a container with an open incident attaches and skips early capture", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-killing/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-killing/event.json", ptr[int64](1))}, []store.Incident{crash}, nil},
		{"event for an unknown pod is stored unattached", func(t *testing.T, h *harness) {
			h.feedEvent(t, "event-killing/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-killing/event.json", nil)}, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			c.run(t, h)
			diff(t, c.wantEvents, h.events(t))
			diff(t, c.wantIncidents, h.incidents(t, c.uid))
			if d := cmp.Diff(c.wantReqs, h.sink.reqs, cmpopts.EquateEmpty()); d != "" {
				t.Error(d)
			}
		})
	}
	_ = now
}
```

Remove the `now` variable and the `_ = now` line if nothing in the table ends up using it; the `crash` and `probe` literals take `LastSeenAt` from `podIncident`.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/processor/ -run TestEventScenarios -v`
Expected: compile error, `h.p.Event undefined`.

- [ ] **Step 4: Write the method**

`internal/processor/event.go`:

```go
package processor

import (
	"context"
	"database/sql"

	corev1 "k8s.io/api/core/v1"

	"idios/internal/incident"
	"idios/internal/ingest"
	"idios/internal/store"
)

// Event stores the event, classifies it by reason, lets a probe event open
// or bump an incident, attaches it, and asks for an early capture when the
// reason announces the pod's removal and nothing is watching that container
// yet.
func (p *Processor) Event(ctx context.Context, clusterID int64, ev *corev1.Event) error {
	now := p.clk.Now()
	row, err := ingest.MapEvent(ev, clusterID)
	if err != nil {
		return err
	}
	row.Category = incident.EventCategory(row.Reason)
	container := incident.FieldPathContainer(row.FieldPath)
	var reqs []CaptureRequest
	err = p.w.Tx(ctx, func(tx *sql.Tx) error {
		pod, err := store.LoadPod(ctx, tx, row.InvolvedUID)
		if err != nil {
			return err
		}
		var cont *store.Container
		if pod != nil && container != "" {
			if cont, err = store.LoadContainer(ctx, tx, pod.UID, container); err != nil {
				return err
			}
		}
		incidents, err := store.LoadIncidentsForSubject(ctx, tx, row.InvolvedUID)
		if err != nil {
			return err
		}
		ops, ref := incident.ApplyEvent(incidents, row, pod, cont, now)
		res, err := p.execOps(ctx, tx, ops, row.InvolvedUID)
		if err != nil {
			return err
		}
		switch {
		case ref.OpenIndex >= 0:
			row.IncidentID = ptr(res.openIDs[ref.OpenIndex])
		case ref.IncidentID != nil:
			row.IncidentID = ref.IncidentID
		}
		if err := store.UpsertEvent(ctx, tx, row); err != nil {
			return err
		}
		if pod == nil || row.InvolvedKind != "Pod" {
			return nil
		}
		base := CaptureRequest{ClusterID: clusterID, Namespace: pod.Namespace, PodUID: pod.UID, PodName: pod.Name, Kind: store.ArtifactLogCurrent, RestartCount: store.NoRestartIndex}
		for i, o := range ops.Open {
			r := base
			r.Container, r.Trigger, r.IncidentID = o.Incident.ContainerName, TriggerIncidentOpen, ptr(res.openIDs[i])
			reqs = append(reqs, r)
		}
		if ingest.EarlyCaptureReason(row.Reason) && len(ops.Open) == 0 && !hasOpenOn(incidents, container) {
			r := base
			r.Container, r.Trigger = container, TriggerEarlyPrefix+row.Reason
			reqs = append(reqs, r)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, r := range reqs {
		p.sink.Enqueue(r)
	}
	return nil
}

// hasOpenOn says whether an open incident already covers the container: its
// own, or a pod-level one. An empty container means any open incident.
func hasOpenOn(incidents []store.Incident, container string) bool {
	for _, inc := range incidents {
		if inc.ClosedAt == nil && (container == "" || inc.ContainerName == container || inc.ContainerName == "") {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/processor/ -v`
Expected: PASS. If `unhealthy on a not-ready container` shows the probe incident with `ID: 2` but the event attached to `1`, `execOps` returned `openIDs` in a different order than `ops.Open`; they must be parallel.

- [ ] **Step 6: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/processor internal/ingest/testdata/event-unhealthy internal/ingest/testdata/event-killing && git commit -m "processor: add Event with probe gate, attach and early capture"`

---

### Task 10: Roadmap status

**Files:**
- Modify: `docs/plans/m1-recorder/roadmap.md`

- [ ] **Step 1: Record the phase**

Under `### Phase 3: processor (glue)` add, as the first paragraph after the heading:

```
Status: done, merged to `main` <date> (`<merge commit>`). Plan:
`03-processor.md`. Built as `internal/processor` (not
`ingest.Processor`: `incident` imports `ingest`, so a processor inside
`ingest` could not call `incident.Apply`).
```

Replace the "Delivers:" sentence's `ingest.Processor` with `processor.Processor`. After the existing "Phase 2 hands Phase 3" paragraph add:

```
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
```

In "Parallel execution", change "Track A owns `internal/ingest`, `internal/incident` (except `closer.go`) and `internal/k8s`" to include `internal/processor`.

In "Facts a new session needs" add:

```
- SQLite accepts `ON CONFLICT (cols) WHERE <partial index predicate>` only
  when the predicate is spelled exactly as in the index; `store.OpenIncident`
  carries both spellings from `0001_init.sql`.
- Processor tests build expected event rows with `ingest.MapEvent` and set
  only `Category` and `IncidentID`; the processor adds nothing else to an
  event row.
```

In the "Plan files" list add `docs/plans/m1-recorder/03-processor.md (Phase 3)`.

- [ ] **Step 2: Checkpoint**

Run: `make ascii`

Commit: `git add docs/plans/m1-recorder/roadmap.md && git commit -m "docs: record phase 3 completion and handoff in roadmap"`

The `<date>` and `<merge commit>` placeholders are filled by the orchestrator at merge time, in this same commit, since the merge is a fast-forward of the branch onto `main`.

---

## Self-review

**Spec coverage.** Process doc 4.1 steps a-d and 3 (Tasks 5, 6); 4.2 owner correction in the same transaction (Task 5 `SetIncidentWorkload`); 4.3 deletes (Task 8: `deleted_at`, `deletion_source`, `deletion_reason`, `pod_deleted` closes, keep-or-drop decision); 4.4 events (Task 9: upsert, `BackOff` `NULL`, attach rule, early capture on the five reasons regardless of type, `probe` only when the container row has `ready = 0`); 6.1 `CaptureRequest` shape (Task 5, plus `IncidentID`); 14.1 bullets touched by this phase: crash loop, image pull, unschedulable then scheduled, evicted, restart jump with one `unobservable` row, `restartPolicy: Never` Job pod with `Previous = false` index 0, dead-instance index (Task 6 `dead-index-running`), first sight x3, reopen x2, `Killing` `Normal` early capture, ReplicaSet chain (Task 7 rows), event timestamp pairs (Task 9 via `storedEvent`). Storage 4 first-sight rule (Task 5), reconstruction (Task 5), reconcile source (Task 8); 5.3 `deletion_reason` (Tasks 4, 8) and `workload_*` correction (Task 5); 5.4 latest-per-type and `last_message` (Tasks 1, 5); 5.7 open path and partial-index idempotency (Task 2); 5.9 revision in place (Task 3, 7); 5.10 upsert key, precedence, classification, attach rule at arrival and at open (Tasks 2, 9); 5.11 `unobservable` (Task 5); 6.2 steps 1-6 (Tasks 2, 5, 6, 9); 6.3 `pod_deleted`, `job_finished`, reopen semantics (Tasks 2, 7, 8). client-go 3.7 (DB snapshot as baseline, reopen needs closed rows: `LoadIncidentsForSubject` loads both), 3.10 (Task 9), 3.11 index and `previous` rules (Task 6). Not in this phase by design: `recovered` closes and late attach by the closer (Phase 6), the reconcile pass itself and handler-boundary logging (Phase 4), files and the early-capture cache (Phase 5).

**Roadmap "Phase 3 must also" items:** `k8s_events.category` set before `ApplyEvent` (Task 9, first lines of `Event`); only the involved subject's incidents passed to `Apply`/`ApplyEvent` (`LoadIncidentsForSubject(uid)`); capture dedupe (Task 6, `HasArtifactFile`); first-seen terminated container's `opened_at` from `created_at` is Phase 2 behaviour, exercised by `first sight config error` (11:45:00). `last_message` refresh decided yes (Task 5). Reading (1) is asserted by the image-pull `s3` row (running, `category` nil); reading (2) by `dead-index-running` and the crash-loop index 1.

**Placeholder scan:** no TBD/TODO; every code step has its code; the one "if nothing uses it, delete" note in Task 9 names the exact variable.

**Type consistency:** `CaptureRequest.RestartCount int64` = `ingest.DeadInstance.Index` = `store.Artifact.RestartCount` = `store.NoRestartIndex` (untyped const, fine). `opsResult{openIDs []int64; history map[int]int64}` used identically in Tasks 5, 6, 9. `touchedByContainer(incidents, ops, res) (map[string]*int64, map[int64]string)` called in Tasks 5 and 6 with the same argument order. `store.AttachEvents(ctx, tx, incidentID, involvedUID, container, category, sinceTS)` (Task 2 signature, Task 5 call). `PodDeleted(ctx, uid, source)`, `JobDeleted(ctx, uid)`, `ReplicaSetDeleted(ctx, uid)` (Tasks 7, 8, 10). Test helpers `request(uid, name, container, kind, restart, previous, trigger, incident)` (Task 6) reused in Tasks 8, 9; `podIncident(id, podUID, workloadKind, workloadName, container, category, first, last, openedAt)` and `withImage` (Task 5) reused in Task 9; `ptr` defined once in `processor.go` and once in `ingest_sql_test.go` (separate packages).

**`.ai` rules:** ascii-only (every block ASCII; `make ascii` at each checkpoint); tests (each test traced above; variants as table rows; whole-struct `cmp.Diff`; edge cases: unknown pod on delete, repeated delete, message-only change, pre-attached event untouched, event before pod, ready-container gate, bare ReplicaSet revisions); comments (only why-comments and doc comments; SQL helpers explain the one non-obvious clause each); code-is-truth (no block names a document); scope (no closer, no files, no reconcile loop, no logging inside the processor; `hasOpenOn` and `incidentsInOrder` exist because a test needs them); commits (one per task, named subjects, no trailers).
