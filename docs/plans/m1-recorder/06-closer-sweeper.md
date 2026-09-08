# Phase 6: Closer + Sweeper Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two timers in the running process: `incident.Closer` (every 30 s, one transaction: close stabilized pod incidents as `recovered`, then attach late events to incidents closed within the window) and `sweep.Sweeper` (at startup and hourly: the retention rules in order, file before row, one `sweep_runs` row per table, `wal_checkpoint(TRUNCATE)` last), plus the store SQL they need in `store/sweep_sql.go` and the wiring in `cmd/idios`.

**Architecture:** Both components hold a `*store.Writer`, a `clock.Clock` and a `*slog.Logger`, and have no memory between ticks; every decision is a SQL predicate over rows that already exist, so a restart mid-window changes nothing. The closer is two `UPDATE` statements in one `Writer.Tx`. The sweeper runs each step in its own short transaction (one per pod, one per incident, one per 1000 events) so the writer is never held for a whole pass, and records each step in `sweep_runs` even when it removed nothing. `sweep` imports only `store` and `clock` (archtest enforces it); `closer.go` imports `store` and `clock`.

**Tech Stack:** Go 1.24, `modernc.org/sqlite` v1.45.0 through `database/sql`, `log/slog`, `github.com/google/go-cmp/cmp` in tests.

**Spec:** `docs/design/process-architecture.md` Sections 7 (closer), 8 (sweeper order), 9 (process time from `clock.Clock`), 10 (file errors recorded in the row), 14.2 (sweep tests: injected clock, exact rows and files, `sweep_runs`); `docs/design/data-storage.md` Sections 5.10 (attach rule), 5.12 (`sweep_runs`), 6.3 (stable definition, terminal-phase pods stay open, closer is periodic and idempotent), 7 (file layout, write order), 8 (retention table, order, never-delete rules), 10 (WAL). Roadmap: `docs/plans/m1-recorder/roadmap.md` (Phase 6 section, "Phase 5 hands Phase 6", "Phase 3 hands Phase 5", "Phase 4 hands Phase 7"). The plan may cite these; the code must not (`.ai/code-is-truth.md`).

## Global Constraints

- ASCII only in every file, commit and reply (`.ai/ascii-only.md`); `make ascii` at every checkpoint.
- Tests trace to a spec statement named in this plan; variants are table rows; assertions compare whole structs or whole row sets with `cmp.Diff` (`.ai/tests.md`).
- Comments say why, never what; every exported identifier has a one-line doc comment; no document, section or plan is named in code (`.ai/comments.md`, `.ai/code-is-truth.md`).
- Build only what a task names (`.ai/scope.md`). No counters, no status struct, no config reload, no panic recovery: Phase 7.
- Commits: `area: imperative subject`, body only for the why, no trailers, `git add` named paths, never commit red (`.ai/commits.md`).
- Every DB timestamp goes through `clock.Format`; process time comes from the injected `clock.Clock`. Kubernetes timestamps are never adjusted.
- Every mutation goes through `store.Writer.Tx`; the one exception in this phase is `Writer.Checkpoint`, because `PRAGMA wal_checkpoint` is a no-op inside an open transaction.
- Store SQL for this phase lives in `internal/store/sweep_sql.go` only. No migration after `0001_init.sql`; if one seems needed, stop and raise it.
- `internal/sweep` may import from this module only `internal/store` and `internal/clock` (`internal/archtest/deps_test.go` already says so).
- Checkpoint at the end of every task: `go build ./... && go test ./... && make ascii` green, then one commit.
- Roadmap model choice for this phase: implementer Sonnet, reviewer Opus. Code blocks below are complete; the implementer transcribes them and reports honestly.

## Decisions made while reading the code

- **Concrete `*store.Writer`, no interface.** `capture.TxRunner` exists because `capture` tests substitute a checking writer; nothing here needs that, and `.ai/scope.md` rule 3 forbids a seam no test uses. The tests use a real temp-file SQLite like the processor tests do.
- **`Writer.Checkpoint` is a method on `store.Writer` defined in `sweep_sql.go`.** `Writer` only exposes `Tx`; `wal_checkpoint` inside a transaction returns busy without doing anything, so it needs the connection outside a transaction, under the same mutex.
- **Stable test is one SQL predicate.** `app`/`sidecar`: `state = 'running' AND ready = 1`; `init`: `state = 'terminated' AND exit_code = 0`; `scheduling` category: the pod row has `node_name`. Pod-level incidents of other categories (`node_pressure`, `rescheduled`) have `container_name = ''`, match no container row and never close here; their pods are in a terminal phase or about to be deleted and the delete path closes them. Job incidents (`subject_kind = 'job'`) are excluded. `ephemeral` containers match neither branch and never close; an incident on one would be `other` at most and is left to the delete path.
- **Late attach targets incidents closed at or after `now - stabilization_window`, whatever their `close_reason`.** A `pod_deleted` incident whose final `Killing` event arrives after the delete is exactly the case the doc describes.
- **Orphan-file grace applies to every file, not only `tmp/`.** The pool writes the file, renames it into place, then writes the row in a separate transaction. Between rename and row, the file is in its final location with no row; a walk at that instant would delete a good capture. The doc's ten-minute rule is applied to any file whose modification time is within ten minutes of the sweep's `now`. `tmp/` needs no special case then.
- **`incidents` step selects by `closed_at < cutoff` alone.** After the pods step, every remaining closed incident is on a live pod or on a job, so the doc's "on pods that still exist" holds by construction, and a closed job incident on a lingering Job object ages out on the same clock as a pod one would.
- **Directories are pruned only in the pods step.** After a pod's files are deleted, the sweeper removes the pod directory and its container directories if empty. Nothing removes namespace or cluster directories; they are one entry each and stable.
- **`duration_ms` comes from the injected clock.** Under `clock.Fake` it is 0; tests assert that value.
- **Retention is a `time.Duration`** in `sweep.Config`; `cmd/idios` converts `retention_days` (`config` is not on `sweep`'s allowed import list).
- **Startup sweep is a direct `Sweep` call in `cmd/idios` before the watchers start**; `Run` is the ticker-only loop. That keeps the doc's ordering promise in the wiring rather than in a goroutine race.

## File structure

```
internal/store/sweep_sql.go          closer SQL (CloseStableIncidents, AttachLateEvents),
                                     sweep SQL (list/delete helpers, InsertSweepRun, Writer.Checkpoint)
internal/store/sweep_sql_test.go     tables for the stable predicate, the attach rule, the event batch limit
internal/incident/closer.go          Closer: New, Tick, Run
internal/incident/closer_test.go     one tick against a seeded store
internal/sweep/sweep.go              Config, Sweeper, New, Sweep, Run, step runner, sweep_runs recording
internal/sweep/steps.go              the ordered steps: pods, incidents, history, jobs, rollouts, events
internal/sweep/orphans.go            orphan files, orphan rows, sweep_runs prune, checkpoint
internal/sweep/sweep_test.go         seeded DB + files, exact survivors and sweep_runs rows
cmd/idios/main.go                    start the closer, sweep once, start the sweeper loop
docs/plans/m1-recorder/roadmap.md  Phase 6 status and handoff
```

---

### Task 1: Closer SQL in `store/sweep_sql.go`

**Files:**
- Create: `internal/store/sweep_sql.go`
- Create: `internal/store/sweep_sql_test.go`

**Interfaces:**
- Consumes: `store.Incident`, `store.K8sEvent`, `rowScanner`, `incidentColumns`, `scanIncident` (`ingest_sql.go`), test helpers `openMigratedStore`, `insertCluster`, `insertPod`, `insertJob`, `insertPodIncident`, `insertJobIncident`, `mustExec`, `exec`, `inTx`, `ptr`, `testEpoch`.
- Produces: `func CloseStableIncidents(ctx context.Context, tx *sql.Tx, closedAt, staleBefore string) (int64, error)`; `func AttachLateEvents(ctx context.Context, tx *sql.Tx, closedSince string) (int64, error)`; unexported `execCount(ctx, tx, query string, args ...any) (int64, error)`; test helper `loadIncidents(t, s) []Incident`.

Tests trace to storage doc 6.3 (stable means `app`/`sidecar` running and ready, `init` terminated with exit 0; closer closes when `last_seen_at < now - window`; terminal-phase pods never satisfy it), process doc 7 (`scheduling`: pod has `node_name`; job incidents are not closed here), storage doc 5.10 attach rule (container from `field_path` or pod-level, category equal or `NULL`, latest `last_seen_at` wins) applied by the closer to incidents closed within the window.

- [ ] **Step 1: Write the failing tests**

```go
package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"idios/internal/clock"
)

func loadIncidents(t *testing.T, s *Store) []Incident {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), "SELECT "+incidentColumns+" FROM incidents ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []Incident
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func insertContainer(t *testing.T, s *Store, podUID, name, kind, state string, ready bool, exitCode *int64) {
	t.Helper()
	mustExec(t, s, `
INSERT INTO containers (pod_uid, name, kind, image, state, ready, exit_code, updated_at)
VALUES (?, ?, ?, 'img', ?, ?, ?, ?)`, podUID, name, kind, state, boolInt(ready), exitCode, clock.Format(testEpoch))
}

func TestCloseStableIncidentsClosesOnlyStableStaleContainers(t *testing.T) {
	const (
		stale  = "2026-08-27T11:40:00.000000Z"
		fresh  = "2026-08-27T11:55:00.000000Z"
		edge   = "2026-08-27T11:50:00.000000Z"
		nowS   = "2026-08-27T12:00:00.000000Z"
		noNode = ""
	)
	cases := []struct {
		name       string
		kind       string
		state      string
		ready      bool
		exitCode   *int64
		category   string
		container  string
		nodeName   string
		lastSeenAt string
		job        bool
		wantClosed bool
	}{
		{"app running and ready closes", ContainerKindApp, StateRunning, true, nil, CategoryCrash, "api", "n1", stale, false, true},
		{"sidecar running and ready closes", ContainerKindSidecar, StateRunning, true, nil, CategoryCrash, "proxy", "n1", stale, false, true},
		{"app running not ready stays", ContainerKindApp, StateRunning, false, nil, CategoryCrash, "api", "n1", stale, false, false},
		{"app waiting stays", ContainerKindApp, StateWaiting, false, nil, CategoryCrash, "api", "n1", stale, false, false},
		{"app terminated exit 0 stays", ContainerKindApp, StateTerminated, false, ptr(int64(0)), CategoryCrash, "api", "n1", stale, false, false},
		{"init terminated exit 0 closes", ContainerKindInit, StateTerminated, true, ptr(int64(0)), CategoryConfig, "init-db", "n1", stale, false, true},
		{"init terminated exit 1 stays", ContainerKindInit, StateTerminated, false, ptr(int64(1)), CategoryCrash, "init-db", "n1", stale, false, false},
		{"init running stays", ContainerKindInit, StateRunning, false, nil, CategoryCrash, "init-db", "n1", stale, false, false},
		{"stable but seen within window stays", ContainerKindApp, StateRunning, true, nil, CategoryCrash, "api", "n1", fresh, false, false},
		{"stable seen exactly at the edge stays", ContainerKindApp, StateRunning, true, nil, CategoryCrash, "api", "n1", edge, false, false},
		{"scheduling with node closes", ContainerKindApp, StateWaiting, false, nil, CategoryScheduling, "", "n1", stale, false, true},
		{"scheduling without node stays", ContainerKindApp, StateWaiting, false, nil, CategoryScheduling, "", noNode, stale, false, false},
		{"pod-level node_pressure never closes here", ContainerKindApp, StateRunning, true, nil, CategoryNodePressure, "", "n1", stale, false, false},
		{"job incident never closes here", ContainerKindApp, StateRunning, true, nil, CategoryJobFailed, "", "n1", stale, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			cid := insertCluster(t, s, "c")
			insertPod(t, s, cid, "p1")
			if tc.nodeName != noNode {
				mustExec(t, s, "UPDATE pods SET node_name = ? WHERE uid = 'p1'", tc.nodeName)
			}
			name := tc.container
			if name == "" {
				name = "api"
			}
			insertContainer(t, s, "p1", name, tc.kind, tc.state, tc.ready, tc.exitCode)
			var id int64
			var err error
			if tc.job {
				insertJob(t, s, cid, "j1")
				id, err = insertJobIncident(t, s, cid, "j1", tc.category)
			} else {
				id, err = insertPodIncident(t, s, cid, "p1", tc.container, tc.category)
			}
			if err != nil {
				t.Fatal(err)
			}
			mustExec(t, s, "UPDATE incidents SET last_seen_at = ? WHERE id = ?", tc.lastSeenAt, id)

			var n int64
			inTx(t, s, func(tx *sql.Tx) (err error) {
				n, err = CloseStableIncidents(context.Background(), tx, nowS, edge)
				return err
			})
			got := loadIncidents(t, s)[0]
			type outcome struct {
				N           int64
				ClosedAt    *string
				CloseReason *string
			}
			want := outcome{}
			if tc.wantClosed {
				want = outcome{1, ptr(nowS), ptr(CloseRecovered)}
			}
			if d := cmp.Diff(want, outcome{n, got.ClosedAt, got.CloseReason}); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func insertEvent(t *testing.T, s *Store, clusterID int64, uid, involvedUID, fieldPath string, category *string, incidentID *int64) int64 {
	t.Helper()
	ts := clock.Format(testEpoch)
	res := mustExec(t, s, `
INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason,
    first_ts, last_ts, category, incident_id, raw_json)
VALUES (?, ?, 'idios-smoke', 'Warning', 'Pod', 'pod-p1', ?, ?, 'BackOff', ?, ?, ?, ?, '{}')`,
		clusterID, uid, involvedUID, fieldPath, ts, ts, category, incidentID)
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func eventIncidentIDs(t *testing.T, s *Store) map[string]*int64 {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), "SELECT event_uid, incident_id FROM k8s_events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]*int64{}
	for rows.Next() {
		var uid string
		var id *int64
		if err := rows.Scan(&uid, &id); err != nil {
			t.Fatal(err)
		}
		out[uid] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAttachLateEventsFollowsAttachRuleOnRecentlyClosedIncidents(t *testing.T) {
	const (
		recent = "2026-08-27T11:55:00.000000Z"
		old    = "2026-08-27T11:00:00.000000Z"
		edge   = "2026-08-27T11:50:00.000000Z"
	)
	type inc struct {
		container  string
		category   string
		closedAt   *string
		lastSeenAt string
	}
	type ev struct {
		uid        string
		involved   string
		fieldPath  string
		category   *string
		preAttach  bool
		wantAttach int // index into incidents, -1 for none
	}
	cases := []struct {
		name      string
		incidents []inc
		events    []ev
		wantN     int64
	}{
		{"container and category match", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", ptr(CategoryCrash), false, 0}}, 1},
		{"null category attaches", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, 0}}, 1},
		{"category mismatch does not attach", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", ptr(CategoryImagePull), false, -1}}, 0},
		{"other container does not attach", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{web}", nil, false, -1}}, 0},
		{"init container path matches", []inc{{"init-db", CategoryConfig, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.initContainers{init-db}", nil, false, 0}}, 1},
		{"pod-level incident takes any container", []inc{{"", CategoryNodePressure, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, 0}, {"e2", "p1", "", nil, false, 0}}, 2},
		{"other pod does not attach", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p2", "spec.containers{api}", nil, false, -1}}, 0},
		{"closed before the window does not attach", []inc{{"api", CategoryCrash, ptr(old), old}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, -1}}, 0},
		{"open incident is not the closer's job", []inc{{"api", CategoryCrash, nil, recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, -1}}, 0},
		{"already attached event is left alone", []inc{{"api", CategoryCrash, ptr(recent), recent}, {"api", CategoryOOM, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, true, 1}}, 0},
		{"latest last_seen_at wins", []inc{{"api", CategoryCrash, ptr(recent), old}, {"api", CategoryOOM, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, 1}}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			cid := insertCluster(t, s, "c")
			insertPod(t, s, cid, "p1")
			insertPod(t, s, cid, "p2")
			var ids []int64
			for _, in := range tc.incidents {
				id, err := insertPodIncident(t, s, cid, "p1", in.container, in.category)
				if err != nil {
					t.Fatal(err)
				}
				mustExec(t, s, "UPDATE incidents SET last_seen_at = ? WHERE id = ?", in.lastSeenAt, id)
				if in.closedAt != nil {
					mustExec(t, s, "UPDATE incidents SET closed_at = ?, close_reason = ? WHERE id = ?", *in.closedAt, CloseRecovered, id)
				}
				ids = append(ids, id)
			}
			want := map[string]*int64{}
			for _, e := range tc.events {
				var pre *int64
				if e.preAttach {
					pre = ptr(ids[e.wantAttach])
				}
				insertEvent(t, s, cid, e.uid, e.involved, e.fieldPath, e.category, pre)
				if e.wantAttach >= 0 {
					want[e.uid] = ptr(ids[e.wantAttach])
				} else {
					want[e.uid] = nil
				}
			}

			var n int64
			inTx(t, s, func(tx *sql.Tx) (err error) {
				n, err = AttachLateEvents(context.Background(), tx, edge)
				return err
			})
			if n != tc.wantN {
				t.Fatalf("attached %d, want %d", n, tc.wantN)
			}
			if d := cmp.Diff(want, eventIncidentIDs(t, s)); d != "" {
				t.Fatal(d)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestCloseStableIncidents|TestAttachLateEvents' 2>&1 | head -20`
Expected: build failure, `undefined: CloseStableIncidents` and `undefined: AttachLateEvents`.

- [ ] **Step 3: Write the implementation**

Create `internal/store/sweep_sql.go`:

```go
package store

import (
	"context"
	"database/sql"
)

func execCount(ctx context.Context, tx *sql.Tx, query string, args ...any) (int64, error) {
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CloseStableIncidents closes as recovered every open pod incident whose
// subject has been stable since before staleBefore. Stable is judged per
// container kind because the kubelet sets ready differently for each: an
// init container is ready only once it exited 0, an app or sidecar only
// while running. A scheduling incident is stable once the pod has a node.
// Pod-level incidents of other categories match no container row and are
// left for the delete path, as are job incidents.
func CloseStableIncidents(ctx context.Context, tx *sql.Tx, closedAt, staleBefore string) (int64, error) {
	return execCount(ctx, tx, `
UPDATE incidents SET closed_at = ?, close_reason = ?
WHERE closed_at IS NULL AND subject_kind = 'pod' AND last_seen_at < ?
  AND ((category = ? AND EXISTS (
          SELECT 1 FROM pods p WHERE p.uid = incidents.pod_uid AND p.node_name IS NOT NULL))
    OR (category <> ? AND EXISTS (
          SELECT 1 FROM containers c
          WHERE c.pod_uid = incidents.pod_uid AND c.name = incidents.container_name
            AND ((c.kind IN (?, ?) AND c.state = ? AND c.ready = 1)
              OR (c.kind = ? AND c.state = ? AND c.exit_code = 0)))))`,
		closedAt, CloseRecovered, staleBefore,
		CategoryScheduling, CategoryScheduling,
		ContainerKindApp, ContainerKindSidecar, StateRunning,
		ContainerKindInit, StateTerminated)
}

// AttachLateEvents links unattached events to an incident closed at or
// after closedSince on the same subject, matching the container from
// field_path (or any container for a pod-level incident) and an equal or
// absent category. Among several candidates the most recently seen wins.
func AttachLateEvents(ctx context.Context, tx *sql.Tx, closedSince string) (int64, error) {
	const candidate = `
SELECT i.id FROM incidents i
WHERE i.closed_at >= ?
  AND (i.pod_uid = k8s_events.involved_uid OR i.job_uid = k8s_events.involved_uid)
  AND (i.container_name = '' OR k8s_events.field_path GLOB 'spec.*{' || i.container_name || '}')
  AND (k8s_events.category IS NULL OR k8s_events.category = i.category)
ORDER BY i.last_seen_at DESC, i.id DESC LIMIT 1`
	return execCount(ctx, tx, `
UPDATE k8s_events SET incident_id = (`+candidate+`)
WHERE incident_id IS NULL AND EXISTS (`+candidate+`)`, closedSince, closedSince)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestCloseStableIncidents|TestAttachLateEvents' -v 2>&1 | tail -40`
Expected: every subtest PASS.

- [ ] **Step 5: Checkpoint and commit**

```bash
go build ./... && go test ./... && make ascii
git add internal/store/sweep_sql.go internal/store/sweep_sql_test.go
git commit -m "store: add closer SQL for stable closes and late attach"
```

---

### Task 2: `incident.Closer`

**Files:**
- Create: `internal/incident/closer.go`
- Create: `internal/incident/closer_test.go`

**Interfaces:**
- Consumes: `store.CloseStableIncidents`, `store.AttachLateEvents` (Task 1); `store.Writer.Tx`; `clock.Clock`, `clock.Format`; `store.Open`, `(*store.Store).Migrate`.
- Produces: `type Closer struct`; `func NewCloser(w *store.Writer, clk clock.Clock, window, interval time.Duration, log *slog.Logger) *Closer`; `type TickResult struct{ Closed, Attached int64 }`; `func (c *Closer) Tick(ctx context.Context) (TickResult, error)`; `func (c *Closer) Run(ctx context.Context) error`. `cmd/idios` (Task 6) calls `NewCloser` and `Run`.

The name is `NewCloser`, not `New`, because the package already exports `Apply` and friends and `incident.New` would say nothing.

The test traces to storage doc 6.3: "a closer runs every `stabilization_check_interval` and closes, in one statement, every open incident whose subject container is stable and whose `last_seen_at < now - stabilization_window` ... The same tick attaches late events to incidents closed within the last `stabilization_window`." The close-then-attach order inside the tick is what the test pins: the event attaches to the incident this very tick closed. The predicate variants are Task 1's tables; this test does not repeat them.

- [ ] **Step 1: Write the failing test**

```go
package incident

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"idios/internal/clock"
	"idios/internal/store"
)

func TestTickClosesStableIncidentsThenAttachesTheirLateEvents(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now)
	s, err := store.Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(ctx, clk); err != nil {
		t.Fatal(err)
	}
	nowS := clock.Format(now)
	stale := clock.Format(now.Add(-11 * time.Minute))
	fresh := clock.Format(now.Add(-1 * time.Minute))
	var recoveredID, stillOpenID int64
	err = s.Writer.Tx(ctx, func(tx *sql.Tx) error {
		stmts := []struct {
			q    string
			args []any
		}{
			{"INSERT INTO clusters (id, identity, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'ctx', 'u', ?)", []any{nowS}},
			{`INSERT INTO pods (uid, cluster_id, namespace, name, node_name, phase, created_at, first_seen_at, last_seen_at)
VALUES ('p1', 1, 'idios-smoke', 'pod', 'n1', 'Running', ?, ?, ?)`, []any{stale, stale, nowS}},
			{`INSERT INTO containers (pod_uid, name, kind, image, state, ready, updated_at) VALUES ('p1', 'api', 'app', 'img', 'running', 1, ?)`, []any{nowS}},
			{`INSERT INTO containers (pod_uid, name, kind, image, state, ready, updated_at) VALUES ('p1', 'web', 'app', 'img', 'waiting', 0, ?)`, []any{nowS}},
			{`INSERT INTO incidents (id, cluster_id, namespace, subject_kind, pod_uid, container_name, category, first_reason, last_reason, opened_at, last_seen_at)
VALUES (10, 1, 'idios-smoke', 'pod', 'p1', 'api', 'crash', 'Error', 'CrashLoopBackOff', ?, ?)`, []any{stale, stale}},
			{`INSERT INTO incidents (id, cluster_id, namespace, subject_kind, pod_uid, container_name, category, first_reason, last_reason, opened_at, last_seen_at)
VALUES (11, 1, 'idios-smoke', 'pod', 'p1', 'web', 'image_pull', 'ErrImagePull', 'ImagePullBackOff', ?, ?)`, []any{stale, fresh}},
			{`INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, first_ts, last_ts, raw_json)
VALUES (1, 'e1', 'idios-smoke', 'Normal', 'Pod', 'pod', 'p1', 'spec.containers{api}', 'Killing', ?, ?, '{}')`, []any{nowS, nowS}},
			{`INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, first_ts, last_ts, raw_json)
VALUES (1, 'e2', 'idios-smoke', 'Warning', 'Pod', 'pod', 'p1', 'spec.containers{web}', 'BackOff', ?, ?, '{}')`, []any{nowS, nowS}},
		}
		for _, st := range stmts {
			if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	recoveredID, stillOpenID = 10, 11

	c := NewCloser(s.Writer, clk, 10*time.Minute, 30*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	got, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(TickResult{Closed: 1, Attached: 1}, got); d != "" {
		t.Fatal(d)
	}

	type incidentState struct {
		ID          int64
		ClosedAt    *string
		CloseReason *string
	}
	type eventState struct {
		UID        string
		IncidentID *int64
	}
	var incs []incidentState
	var evs []eventState
	rows, err := s.Reader.DB().QueryContext(ctx, "SELECT id, closed_at, close_reason FROM incidents ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var i incidentState
		if err := rows.Scan(&i.ID, &i.ClosedAt, &i.CloseReason); err != nil {
			t.Fatal(err)
		}
		incs = append(incs, i)
	}
	_ = rows.Close()
	rows, err = s.Reader.DB().QueryContext(ctx, "SELECT event_uid, incident_id FROM k8s_events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var e eventState
		if err := rows.Scan(&e.UID, &e.IncidentID); err != nil {
			t.Fatal(err)
		}
		evs = append(evs, e)
	}
	_ = rows.Close()
	recovered := store.CloseRecovered
	wantIncs := []incidentState{{recoveredID, &nowS, &recovered}, {stillOpenID, nil, nil}}
	wantEvs := []eventState{{"e1", &recoveredID}, {"e2", nil}}
	if d := cmp.Diff(wantIncs, incs); d != "" {
		t.Fatal(d)
	}
	if d := cmp.Diff(wantEvs, evs); d != "" {
		t.Fatal(d)
	}

	again, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(TickResult{}, again); d != "" {
		t.Fatalf("second tick is not idempotent: %s", d)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/incident/ -run TestTickCloses 2>&1 | head`
Expected: `undefined: NewCloser`, `undefined: TickResult`.

- [ ] **Step 3: Write the implementation**

Create `internal/incident/closer.go`:

```go
package incident

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"idios/internal/clock"
	"idios/internal/store"
)

// Closer periodically closes stabilized pod incidents and attaches events
// that arrived after their incident closed. It keeps no state between
// ticks, so a restart mid-window changes nothing.
type Closer struct {
	w        *store.Writer
	clk      clock.Clock
	window   time.Duration
	interval time.Duration
	log      *slog.Logger
}

// TickResult counts what one tick changed.
type TickResult struct {
	Closed   int64
	Attached int64
}

// NewCloser returns a Closer that ticks every interval and treats an
// incident as stable once last_seen_at is older than window.
func NewCloser(w *store.Writer, clk clock.Clock, window, interval time.Duration, log *slog.Logger) *Closer {
	return &Closer{w: w, clk: clk, window: window, interval: interval, log: log}
}

// Tick runs one close-then-attach pass in a single transaction. Closing
// first lets an event attach to the incident closed in the same tick.
func (c *Closer) Tick(ctx context.Context) (TickResult, error) {
	now := c.clk.Now()
	nowS := clock.Format(now)
	edge := clock.Format(now.Add(-c.window))
	var r TickResult
	err := c.w.Tx(ctx, func(tx *sql.Tx) (err error) {
		if r.Closed, err = store.CloseStableIncidents(ctx, tx, nowS, edge); err != nil {
			return err
		}
		r.Attached, err = store.AttachLateEvents(ctx, tx, edge)
		return err
	})
	if err != nil {
		return TickResult{}, err
	}
	return r, nil
}

// Run ticks until ctx is done and returns ctx.Err(). A failed tick is
// logged and the next one runs; nothing is retried early.
func (c *Closer) Run(ctx context.Context) error {
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			r, err := c.Tick(ctx)
			if err != nil {
				c.log.Error("closer tick", "err", err)
				continue
			}
			if r.Closed > 0 || r.Attached > 0 {
				c.log.Info("closer tick", "closed", r.Closed, "attached", r.Attached)
			}
		}
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/incident/ -run TestTickCloses -v 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Checkpoint and commit**

```bash
go build ./... && go test ./... && make ascii
git add internal/incident/closer.go internal/incident/closer_test.go
git commit -m "incident: add the periodic closer"
```

---

### Task 3: Sweep SQL in `store/sweep_sql.go`

**Files:**
- Modify: `internal/store/sweep_sql.go` (append)
- Modify: `internal/store/sweep_sql_test.go` (append)

**Interfaces:**
- Consumes: `execCount` (Task 1), `SweepRun`, `Writer.db`, `Writer.mu`; test helpers from Task 1 and earlier phases.
- Produces: `type ArtifactFile struct{ ID int64; FilePath string }`; `ListExpiredPods(ctx, tx, cutoff string) ([]string, error)`; `ListExpiredIncidents(ctx, tx, cutoff string) ([]int64, error)`; `ListPodArtifactFiles(ctx, tx, podUID string) ([]ArtifactFile, error)`; `ListIncidentArtifactFiles(ctx, tx, incidentID int64) ([]ArtifactFile, error)`; `ListArtifactFiles(ctx, tx) ([]ArtifactFile, error)`; `DeletePod(ctx, tx, uid string) error`; `DeleteIncident(ctx, tx, id int64) error`; `DeleteExpiredContainerHistory(ctx, tx, cutoff string) (int64, error)`; `DeleteExpiredConditionHistory(ctx, tx, cutoff string) (int64, error)`; `DeleteExpiredJobs(ctx, tx, cutoff string) (int64, error)`; `DeleteExpiredRollouts(ctx, tx, cutoff string) (int64, error)`; `DeleteExpiredEventsBatch(ctx, tx, cutoff string, limit int) (int64, error)`; `DeleteArtifactRows(ctx, tx, ids []int64) (int64, error)`; `DeleteExpiredSweepRuns(ctx, tx, cutoff string) (int64, error)`; `InsertSweepRun(ctx, tx, r SweepRun) error`; `func (w *Writer) Checkpoint(ctx context.Context) error`. Tasks 4 and 5 call all of these.

Tests trace to storage doc 8: events are kept while their subject has an open incident and deleted in batches (process doc 8 step 4, "1000 rows per transaction"); history rows go only when `incident_id` is null or points to a closed incident; jobs go when `deleted_at < cutoff` or when gone with `finished_at < cutoff`, never while the object exists; process doc 8 step 8: the checkpoint truncates the WAL. The pod, incident, rollout, artifact and sweep_runs deletes are plain statements exercised by the sweep test in Tasks 4 and 5 and get no separate test.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/sweep_sql_test.go`:

```go
func countRows(t *testing.T, s *Store, table string) int64 {
	t.Helper()
	var n int64
	if err := s.Reader.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestExpiredEventsGoInBatchesExceptOnSubjectsWithOpenIncidents(t *testing.T) {
	const (
		cutoff = "2026-08-24T12:00:00.000000Z"
		old    = "2026-08-20T12:00:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "open")
	insertPod(t, s, cid, "closed")
	insertJob(t, s, cid, "jopen")
	if _, err := insertPodIncident(t, s, cid, "open", "api", CategoryCrash); err != nil {
		t.Fatal(err)
	}
	if _, err := insertJobIncident(t, s, cid, "jopen", CategoryJobFailed); err != nil {
		t.Fatal(err)
	}
	id, err := insertPodIncident(t, s, cid, "closed", "api", CategoryCrash)
	if err != nil {
		t.Fatal(err)
	}
	closeIncident(t, s, id, CloseRecovered)
	for i, involved := range []string{"open", "jopen", "closed", "closed", "closed", "unknown"} {
		insertEvent(t, s, cid, "e"+string(rune('0'+i)), involved, "", nil, nil)
	}
	mustExec(t, s, "UPDATE k8s_events SET last_ts = ? WHERE event_uid <> 'e5'", old)

	var batches []int64
	for {
		var n int64
		inTx(t, s, func(tx *sql.Tx) (err error) {
			n, err = DeleteExpiredEventsBatch(context.Background(), tx, cutoff, 2)
			return err
		})
		batches = append(batches, n)
		if n == 0 {
			break
		}
	}
	if d := cmp.Diff([]int64{2, 1, 0}, batches); d != "" {
		t.Fatal(d)
	}
	want := map[string]*int64{"e0": nil, "e1": nil, "e5": nil}
	if d := cmp.Diff(want, eventIncidentIDs(t, s)); d != "" {
		t.Fatal(d)
	}
}

func TestExpiredHistoryStaysOnlyWhileItsIncidentIsOpen(t *testing.T) {
	const (
		cutoff = "2026-08-24T12:00:00.000000Z"
		old    = "2026-08-20T12:00:00.000000Z"
		recent = "2026-08-26T12:00:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	openID, err := insertPodIncident(t, s, cid, "p1", "api", CategoryCrash)
	if err != nil {
		t.Fatal(err)
	}
	closedID, err := insertPodIncident(t, s, cid, "p1", "api", CategoryOOM)
	if err != nil {
		t.Fatal(err)
	}
	closeIncident(t, s, closedID, CloseRecovered)
	rows := []struct {
		name       string
		incidentID *int64
		observedAt string
		survives   bool
	}{
		{"old-open", &openID, old, true},
		{"old-closed", &closedID, old, false},
		{"old-unattached", nil, old, false},
		{"recent-unattached", nil, recent, true},
	}
	for _, r := range rows {
		mustExec(t, s, `
INSERT INTO container_state_history (pod_uid, container_name, incident_id, image, state, restart_count, observed_at)
VALUES ('p1', ?, ?, 'img', 'waiting', 0, ?)`, r.name, r.incidentID, r.observedAt)
		mustExec(t, s, `
INSERT INTO pod_condition_history (pod_uid, type, status, reason, observed_at) VALUES ('p1', ?, 'False', 'r', ?)`, r.name, r.observedAt)
	}

	var gotC, gotP int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		if gotC, err = DeleteExpiredContainerHistory(context.Background(), tx, cutoff); err != nil {
			return err
		}
		gotP, err = DeleteExpiredConditionHistory(context.Background(), tx, cutoff)
		return err
	})
	if gotC != 2 || gotP != 3 {
		t.Fatalf("removed %d container rows and %d condition rows, want 2 and 3", gotC, gotP)
	}
	var survivors []string
	rs, err := s.Reader.DB().QueryContext(context.Background(), "SELECT container_name FROM container_state_history ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rs.Close() }()
	for rs.Next() {
		var n string
		if err := rs.Scan(&n); err != nil {
			t.Fatal(err)
		}
		survivors = append(survivors, n)
	}
	if d := cmp.Diff([]string{"old-open", "recent-unattached"}, survivors); d != "" {
		t.Fatal(d)
	}
	var conditionSurvivor string
	if err := s.Reader.DB().QueryRowContext(context.Background(), "SELECT type FROM pod_condition_history").Scan(&conditionSurvivor); err != nil {
		t.Fatal(err)
	}
	if conditionSurvivor != "recent-unattached" {
		t.Fatalf("condition survivor %q, want recent-unattached", conditionSurvivor)
	}
}

func TestExpiredJobsGoOnlyWhenTheObjectIsGone(t *testing.T) {
	const (
		cutoff = "2026-08-24T12:00:00.000000Z"
		old    = "2026-08-20T12:00:00.000000Z"
		recent = "2026-08-26T12:00:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	jobs := []struct {
		uid        string
		deletedAt  *string
		finishedAt *string
	}{
		{"deleted-old", ptr(old), nil},
		{"deleted-recent-finished-old", ptr(recent), ptr(old)},
		{"deleted-recent", ptr(recent), ptr(recent)},
		{"live-finished-old", nil, ptr(old)},
		{"live", nil, nil},
	}
	for _, j := range jobs {
		insertJob(t, s, cid, j.uid)
		mustExec(t, s, "UPDATE jobs SET deleted_at = ?, finished_at = ? WHERE uid = ?", j.deletedAt, j.finishedAt, j.uid)
	}
	var n int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		n, err = DeleteExpiredJobs(context.Background(), tx, cutoff)
		return err
	})
	if n != 2 {
		t.Fatalf("removed %d, want 2", n)
	}
	var survivors []string
	rs, err := s.Reader.DB().QueryContext(context.Background(), "SELECT uid FROM jobs ORDER BY uid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rs.Close() }()
	for rs.Next() {
		var u string
		if err := rs.Scan(&u); err != nil {
			t.Fatal(err)
		}
		survivors = append(survivors, u)
	}
	if d := cmp.Diff([]string{"deleted-recent", "live", "live-finished-old"}, survivors); d != "" {
		t.Fatal(d)
	}
}

func TestCheckpointTruncatesTheWAL(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	for i := 0; i < 50; i++ {
		insertPod(t, s, cid, "p"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	wal := s.path + "-wal"
	before, err := os.Stat(wal)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() == 0 {
		t.Fatal("WAL is empty before the checkpoint; the test proves nothing")
	}
	if err := s.Writer.Checkpoint(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(wal)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != 0 {
		t.Fatalf("WAL is %d bytes after TRUNCATE checkpoint, want 0", after.Size())
	}
}
```

Add `"os"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestExpired|TestCheckpoint' 2>&1 | head`
Expected: `undefined: DeleteExpiredEventsBatch` and friends.

- [ ] **Step 3: Write the implementation**

Append to `internal/store/sweep_sql.go`:

```go
// ArtifactFile is an artifacts row that owns a file on disk.
type ArtifactFile struct {
	ID       int64
	FilePath string
}

func listStrings(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

// ListExpiredPods returns the uids of pods deleted before cutoff.
func ListExpiredPods(ctx context.Context, tx *sql.Tx, cutoff string) ([]string, error) {
	return listStrings(ctx, tx, "SELECT uid FROM pods WHERE deleted_at < ? ORDER BY uid", cutoff)
}

// ListExpiredIncidents returns the ids of incidents closed before cutoff.
func ListExpiredIncidents(ctx context.Context, tx *sql.Tx, cutoff string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM incidents WHERE closed_at < ? ORDER BY id", cutoff)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func listArtifactFiles(ctx context.Context, tx *sql.Tx, where string, args ...any) ([]ArtifactFile, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, file_path FROM artifacts WHERE file_path IS NOT NULL"+where+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ArtifactFile
	for rows.Next() {
		var f ArtifactFile
		if err := rows.Scan(&f.ID, &f.FilePath); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListPodArtifactFiles returns the file-owning artifact rows of one pod.
func ListPodArtifactFiles(ctx context.Context, tx *sql.Tx, podUID string) ([]ArtifactFile, error) {
	return listArtifactFiles(ctx, tx, " AND pod_uid = ?", podUID)
}

// ListIncidentArtifactFiles returns the file-owning artifact rows of one
// incident.
func ListIncidentArtifactFiles(ctx context.Context, tx *sql.Tx, incidentID int64) ([]ArtifactFile, error) {
	return listArtifactFiles(ctx, tx, " AND incident_id = ?", incidentID)
}

// ListArtifactFiles returns every file-owning artifact row.
func ListArtifactFiles(ctx context.Context, tx *sql.Tx) ([]ArtifactFile, error) {
	return listArtifactFiles(ctx, tx, "")
}

// DeletePod removes the pod row; the cascades remove everything under it.
func DeletePod(ctx context.Context, tx *sql.Tx, uid string) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM pods WHERE uid = ?", uid)
	return err
}

// DeleteIncident removes the incident row. Its artifact, history and event
// rows are detached, not removed; callers delete the artifact rows they
// have taken the files of.
func DeleteIncident(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM incidents WHERE id = ?", id)
	return err
}

// DeleteExpiredContainerHistory removes transitions observed before cutoff
// unless they belong to an incident that is still open.
func DeleteExpiredContainerHistory(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, `
DELETE FROM container_state_history
WHERE observed_at < ? AND (incident_id IS NULL OR incident_id IN (SELECT id FROM incidents WHERE closed_at IS NOT NULL))`, cutoff)
}

// DeleteExpiredConditionHistory removes condition rows observed before
// cutoff. Conditions carry no incident_id, so age alone decides.
func DeleteExpiredConditionHistory(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, "DELETE FROM pod_condition_history WHERE observed_at < ?", cutoff)
}

// DeleteExpiredJobs removes jobs whose object is gone and that were deleted,
// or had finished, before cutoff. A job that still exists is never removed.
func DeleteExpiredJobs(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, "DELETE FROM jobs WHERE deleted_at < ? OR (deleted_at IS NOT NULL AND finished_at < ?)", cutoff, cutoff)
}

// DeleteExpiredRollouts removes rollout rows whose ReplicaSet was deleted,
// or last seen, before cutoff.
func DeleteExpiredRollouts(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, "DELETE FROM rollout_history WHERE deleted_at < ? OR last_seen_at < ?", cutoff, cutoff)
}

// DeleteExpiredEventsBatch removes up to limit events last seen before
// cutoff whose subject has no open incident. Open incidents exist only on
// live objects, so that one check covers "still exists with an open
// incident".
func DeleteExpiredEventsBatch(ctx context.Context, tx *sql.Tx, cutoff string, limit int) (int64, error) {
	return execCount(ctx, tx, `
DELETE FROM k8s_events WHERE id IN (
  SELECT id FROM k8s_events
  WHERE last_ts < ?
    AND involved_uid NOT IN (SELECT pod_uid FROM incidents WHERE closed_at IS NULL AND pod_uid IS NOT NULL)
    AND involved_uid NOT IN (SELECT job_uid FROM incidents WHERE closed_at IS NULL AND job_uid IS NOT NULL)
  ORDER BY id LIMIT ?)`, cutoff, limit)
}

// DeleteArtifactRows removes the given artifact rows.
func DeleteArtifactRows(ctx context.Context, tx *sql.Tx, ids []int64) (int64, error) {
	var n int64
	for _, id := range ids {
		k, err := execCount(ctx, tx, "DELETE FROM artifacts WHERE id = ?", id)
		if err != nil {
			return n, err
		}
		n += k
	}
	return n, nil
}

// DeleteExpiredSweepRuns removes sweep records older than cutoff.
func DeleteExpiredSweepRuns(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, "DELETE FROM sweep_runs WHERE ran_at < ?", cutoff)
}

// InsertSweepRun records one sweep step.
func InsertSweepRun(ctx context.Context, tx *sql.Tx, r SweepRun) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO sweep_runs (ran_at, cutoff, table_name, rows_removed, files_removed, bytes_removed, duration_ms, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, r.RanAt, r.Cutoff, r.TableName, r.RowsRemoved, r.FilesRemoved, r.BytesRemoved, r.DurationMs, r.Error)
	return err
}

// Checkpoint moves the WAL into the main file and truncates it. It runs
// outside Tx because a checkpoint inside an open transaction does nothing.
func (w *Writer) Checkpoint(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestExpired|TestCheckpoint' -v 2>&1 | tail -12`
Expected: four PASS lines.

If `TestCheckpointTruncatesTheWAL` finds the WAL at 0 bytes before the checkpoint, the driver auto-checkpointed at commit; raise the pod count in the loop until it does not, or bump the insert to a larger row. Do not skip the assertion.

- [ ] **Step 5: Checkpoint and commit**

```bash
go build ./... && go test ./... && make ascii
git add internal/store/sweep_sql.go internal/store/sweep_sql_test.go
git commit -m "store: add sweep SQL and Writer.Checkpoint"
```

---

### Task 4: `sweep.Sweeper` with the row steps

**Files:**
- Create: `internal/sweep/sweep.go`
- Create: `internal/sweep/steps.go`
- Create: `internal/sweep/sweep_test.go`

**Interfaces:**
- Consumes: Task 3 helpers `ListExpiredPods`, `ListPodArtifactFiles`, `DeletePod`, `ListExpiredIncidents`, `ListIncidentArtifactFiles`, `DeleteIncident`, `DeleteExpiredContainerHistory`, `DeleteExpiredConditionHistory`, `DeleteExpiredJobs`, `DeleteExpiredRollouts`, `DeleteExpiredEventsBatch`, `InsertSweepRun`, `store.ArtifactFile`, `store.SweepRun`; `store.Writer.Tx`; `clock.Clock`, `clock.Format`.
- Produces: `type Config struct{ Root string; Retention, Interval time.Duration }`; `type Sweeper struct`; `func New(cfg Config, w *store.Writer, clk clock.Clock, log *slog.Logger) *Sweeper`; `func (s *Sweeper) Sweep(ctx context.Context) error`; `func (s *Sweeper) Run(ctx context.Context) error`; unexported `type result struct{ rows, files, bytes int64; errs []error }`, `type step struct{ table string; run func(context.Context, string) result }`, `func (s *Sweeper) steps() []step`, `func (s *Sweeper) removeFile(rel string) (int64, bool, error)`, `func (s *Sweeper) removeFiles(files []store.ArtifactFile) (int64, int64, error)`, `func (s *Sweeper) pruneDirs(files []store.ArtifactFile)`, `func (s *Sweeper) countStep(fn func(context.Context, *sql.Tx, string) (int64, error)) func(context.Context, string) result`, `const eventBatch = 1000`. Task 5 appends steps to `steps()` and extends the test; Task 6 calls `New`, `Sweep`, `Run`.

The test traces to storage doc 8's retention table row by row (pods by `deleted_at`, incidents by `closed_at`, history by `observed_at` unless its incident is open, jobs by `deleted_at` or `finished_at` when gone, rollout history by `deleted_at` or `last_seen_at`, events by `last_ts` unless the subject has an open incident), the two "never" rules (a live pod's row and an open incident survive whatever their age), the cascade of a job incident with its job, and storage doc 5.12 / process doc 8 ("one `sweep_runs` row per step, always, including zero-row steps") with the step order of process doc 8. Files are not seeded here; Task 5 adds them and the orphan passes.

- [ ] **Step 1: Write the failing test**

```go
package sweep

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"idios/internal/clock"
	"idios/internal/store"
)

const (
	oldTS    = "2026-08-20T12:00:00.000000Z"
	recentTS = "2026-08-26T12:00:00.000000Z"
	nowTS    = "2026-08-27T12:00:00.000000Z"
	cutoffTS = "2026-08-24T12:00:00.000000Z"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

type harness struct {
	s    *store.Store
	clk  *clock.Fake
	root string
	sw   *Sweeper
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clk := clock.NewFake(testNow)
	if err := s.Migrate(context.Background(), clk); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "artifacts")
	sw := New(Config{Root: root, Retention: 3 * 24 * time.Hour, Interval: time.Hour}, s.Writer, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &harness{s: s, clk: clk, root: root, sw: sw}
}

func (h *harness) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	err := h.s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
}

func (h *harness) strings(t *testing.T, q string) []string {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (h *harness) sweepRuns(t *testing.T) []store.SweepRun {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT ran_at, cutoff, table_name, rows_removed, files_removed, bytes_removed, duration_ms, error FROM sweep_runs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.SweepRun
	for rows.Next() {
		var r store.SweepRun
		if err := rows.Scan(&r.RanAt, &r.Cutoff, &r.TableName, &r.RowsRemoved, &r.FilesRemoved, &r.BytesRemoved, &r.DurationMs, &r.Error); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// seedRows builds one database in which every retention rule has a row that
// goes and a row that stays.
func (h *harness) seedRows(t *testing.T) {
	t.Helper()
	x := func(q string, args ...any) { h.exec(t, q, args...) }
	x("INSERT INTO clusters (id, identity, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'ctx', 'u', ?)", oldTS)
	pod := `INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at, deleted_at, deletion_source)
VALUES (?, 1, 'idios-smoke', ?, 'Running', ?, ?, ?, ?, ?)`
	x(pod, "gone-old", "gone-old", oldTS, oldTS, oldTS, oldTS, "watch")
	x(pod, "gone-recent", "gone-recent", oldTS, oldTS, recentTS, recentTS, "watch")
	x(pod, "live", "live", oldTS, oldTS, nowTS, nil, nil)
	ctr := `INSERT INTO containers (pod_uid, name, kind, image, state, ready, updated_at) VALUES (?, 'api', 'app', 'img', 'running', 1, ?)`
	x(ctr, "gone-old", oldTS)
	x(ctr, "live", nowTS)
	inc := `INSERT INTO incidents (id, cluster_id, namespace, subject_kind, pod_uid, job_uid, container_name, category, first_reason, last_reason,
    opened_at, last_seen_at, closed_at, close_reason) VALUES (?, 1, 'idios-smoke', ?, ?, ?, 'api', 'crash', 'r', 'r', ?, ?, ?, ?)`
	x(inc, 10, "pod", "gone-old", nil, oldTS, oldTS, oldTS, "pod_deleted")
	x(inc, 20, "pod", "live", nil, oldTS, oldTS, oldTS, "recovered")
	x(inc, 21, "pod", "live", nil, oldTS, nowTS, nil, nil)
	x(inc, 22, "pod", "live", nil, oldTS, recentTS, recentTS, "recovered")
	hist := `INSERT INTO container_state_history (id, pod_uid, container_name, incident_id, image, state, restart_count, observed_at)
VALUES (?, ?, 'api', ?, 'img', 'waiting', 0, ?)`
	x(hist, 1, "gone-old", 10, oldTS)
	x(hist, 2, "live", 20, oldTS)
	x(hist, 3, "live", 21, oldTS)
	x(hist, 4, "live", 22, oldTS)
	x(hist, 5, "live", nil, oldTS)
	x(hist, 6, "live", nil, recentTS)
	cond := `INSERT INTO pod_condition_history (id, pod_uid, type, status, observed_at) VALUES (?, 'live', 'Ready', 'False', ?)`
	x(cond, 1, oldTS)
	x(cond, 2, recentTS)
	job := `INSERT INTO jobs (uid, cluster_id, namespace, name, created_at, first_seen_at, last_seen_at, deleted_at, finished_at)
VALUES (?, 1, 'idios-smoke', ?, ?, ?, ?, ?, ?)`
	x(job, "job-gone-old", "j", oldTS, oldTS, oldTS, oldTS, oldTS)
	x(job, "job-finished-gone", "j", oldTS, oldTS, recentTS, recentTS, oldTS)
	x(job, "job-live-old", "j", oldTS, oldTS, nowTS, nil, oldTS)
	x(`INSERT INTO incidents (id, cluster_id, namespace, subject_kind, job_uid, category, first_reason, last_reason, opened_at, last_seen_at, closed_at, close_reason)
VALUES (30, 1, 'idios-smoke', 'job', 'job-gone-old', 'job_failed', 'r', 'r', ?, ?, ?, 'job_finished')`, oldTS, recentTS, recentTS)
	rs := `INSERT INTO rollout_history (cluster_id, namespace, replicaset_uid, replicaset_name, container_name, image, first_seen_at, last_seen_at, deleted_at)
VALUES (1, 'idios-smoke', ?, ?, 'api', 'img', ?, ?, ?)`
	x(rs, "rs-old", "rs-old", oldTS, oldTS, oldTS)
	x(rs, "rs-stale", "rs-stale", oldTS, oldTS, nil)
	x(rs, "rs-live", "rs-live", oldTS, recentTS, nil)
	ev := `INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, reason, first_ts, last_ts, raw_json)
VALUES (1, ?, 'idios-smoke', 'Warning', 'Pod', 'p', ?, 'BackOff', ?, ?, '{}')`
	x(ev, "ev-live-old", "live", oldTS, oldTS)
	x(ev, "ev-gone-recent-old", "gone-recent", oldTS, oldTS)
	x(ev, "ev-recent", "live", recentTS, recentTS)
	art := `INSERT INTO artifacts (id, pod_uid, incident_id, container_name, kind, restart_count, file_path, captured_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	x(art, 1, "gone-old", 10, "api", "log_previous", 0, "1/idios-smoke/gone-old/api/restart_000.log", oldTS)
	x(art, 2, "live", 20, "api", "log_current", -1, "1/idios-smoke/live/api/current.log", oldTS)
	x(art, 3, "live", nil, "", "pod_json", -1, "1/idios-smoke/live/pod.json", recentTS)
	x(art, 4, "live", nil, "api", "log_previous", 1, "1/idios-smoke/live/api/restart_001.log", recentTS)
	run := `INSERT INTO sweep_runs (ran_at, cutoff, table_name) VALUES (?, ?, 'pods')`
	x(run, oldTS, oldTS)
	x(run, recentTS, recentTS)
}

type survivors struct {
	Pods      []string
	Incidents []string
	History   []string
	Condition []string
	Jobs      []string
	Rollouts  []string
	Events    []string
	Artifacts []string
}

func (h *harness) survivors(t *testing.T) survivors {
	t.Helper()
	return survivors{
		Pods:      h.strings(t, "SELECT uid FROM pods ORDER BY uid"),
		Incidents: h.strings(t, "SELECT CAST(id AS TEXT) FROM incidents ORDER BY id"),
		History:   h.strings(t, "SELECT CAST(id AS TEXT) FROM container_state_history ORDER BY id"),
		Condition: h.strings(t, "SELECT CAST(id AS TEXT) FROM pod_condition_history ORDER BY id"),
		Jobs:      h.strings(t, "SELECT uid FROM jobs ORDER BY uid"),
		Rollouts:  h.strings(t, "SELECT replicaset_uid FROM rollout_history ORDER BY replicaset_uid"),
		Events:    h.strings(t, "SELECT event_uid FROM k8s_events ORDER BY event_uid"),
		Artifacts: h.strings(t, "SELECT CAST(id AS TEXT) FROM artifacts ORDER BY id"),
	}
}

func runRow(table string, rows, files, bytes int64) store.SweepRun {
	return store.SweepRun{RanAt: nowTS, Cutoff: cutoffTS, TableName: table, RowsRemoved: rows, FilesRemoved: files, BytesRemoved: bytes}
}

func TestSweepRemovesExpiredRowsInOrderAndRecordsEveryStep(t *testing.T) {
	h := newHarness(t)
	h.seedRows(t)

	if err := h.sw.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := survivors{
		Pods:      []string{"gone-recent", "live"},
		Incidents: []string{"21", "22"},
		History:   []string{"3", "6"},
		Condition: []string{"2"},
		Jobs:      []string{"job-live-old"},
		Rollouts:  []string{"rs-live"},
		Events:    []string{"ev-live-old", "ev-recent"},
		Artifacts: []string{"3", "4"},
	}
	if d := cmp.Diff(want, h.survivors(t)); d != "" {
		t.Fatal(d)
	}
	wantRuns := []store.SweepRun{
		{RanAt: oldTS, Cutoff: oldTS, TableName: "pods"},
		{RanAt: recentTS, Cutoff: recentTS, TableName: "pods"},
		runRow("pods", 1, 0, 0),
		runRow("incidents", 1, 0, 0),
		runRow("container_state_history", 3, 0, 0),
		runRow("pod_condition_history", 1, 0, 0),
		runRow("jobs", 2, 0, 0),
		runRow("rollout_history", 2, 0, 0),
		runRow("k8s_events", 1, 0, 0),
	}
	if d := cmp.Diff(wantRuns, h.sweepRuns(t)); d != "" {
		t.Fatal(d)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sweep/ 2>&1 | head`
Expected: `undefined: New`, `undefined: Config`, `undefined: Sweeper`.

- [ ] **Step 3: Write the implementation**

Create `internal/sweep/sweep.go`:

```go
// Package sweep applies the retention rules: anything that no longer exists
// in the cluster goes once it is older than the window, anything that still
// exists stays, and every step leaves a sweep_runs row saying what it did.
package sweep

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"idios/internal/clock"
	"idios/internal/store"
)

// Config sizes the sweeper. Root is the artifacts root every file_path is
// relative to; Retention is the window; Interval is the tick of Run.
type Config struct {
	Root      string
	Retention time.Duration
	Interval  time.Duration
}

// Sweeper runs the retention pass. It keeps no state between passes.
type Sweeper struct {
	cfg Config
	w   *store.Writer
	clk clock.Clock
	log *slog.Logger
}

// New returns a Sweeper; Sweep runs one pass, Run repeats it every Interval.
func New(cfg Config, w *store.Writer, clk clock.Clock, log *slog.Logger) *Sweeper {
	return &Sweeper{cfg: cfg, w: w, clk: clk, log: log}
}

type result struct {
	rows, files, bytes int64
	errs               []error
}

type step struct {
	table string
	run   func(ctx context.Context, cutoff string) result
}

// Sweep runs every step once against one cutoff and records each in
// sweep_runs. A step's failures are recorded on its row and the next step
// still runs; only a failure to record stops the pass.
func (s *Sweeper) Sweep(ctx context.Context) error {
	cutoff := clock.Format(s.clk.Now().Add(-s.cfg.Retention))
	for _, st := range s.steps() {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := s.clk.Now()
		r := st.run(ctx, cutoff)
		row := store.SweepRun{
			RanAt: clock.Format(start), Cutoff: cutoff, TableName: st.table,
			RowsRemoved: r.rows, FilesRemoved: r.files, BytesRemoved: r.bytes,
			DurationMs: s.clk.Now().Sub(start).Milliseconds(),
		}
		if err := errors.Join(r.errs...); err != nil {
			msg := err.Error()
			row.Error = &msg
			s.log.Warn("sweep step", "table", st.table, "err", err)
		}
		if err := s.w.Tx(ctx, func(tx *sql.Tx) error { return store.InsertSweepRun(ctx, tx, row) }); err != nil {
			return fmt.Errorf("record sweep of %s: %w", st.table, err)
		}
	}
	return nil
}

// Run sweeps every Interval until ctx is done and returns ctx.Err(). The
// startup pass is the caller's, so it can run before anything else starts.
func (s *Sweeper) Run(ctx context.Context) error {
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := s.Sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("sweep", "err", err)
			}
		}
	}
}

// removeFile deletes one artifact file and reports its size. A file that is
// already gone counts as nothing removed; its row is the orphan-rows step's.
func (s *Sweeper) removeFile(rel string) (int64, bool, error) {
	p := filepath.Join(s.cfg.Root, filepath.FromSlash(rel))
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if err := os.Remove(p); err != nil {
		return 0, false, err
	}
	return fi.Size(), true, nil
}

// removeFiles deletes the files of the given rows and stops at the first
// failure, so the caller can leave the rows in place and retry next pass.
func (s *Sweeper) removeFiles(files []store.ArtifactFile) (count, bytes int64, err error) {
	for _, f := range files {
		n, ok, err := s.removeFile(f.FilePath)
		if err != nil {
			return count, bytes, err
		}
		if ok {
			count++
			bytes += n
		}
	}
	return count, bytes, nil
}

// pruneDirs removes the container and pod directories of the given files
// once they are empty. It stops at the namespace directory, which is shared
// by every pod of that namespace and is not worth racing a capture for.
func (s *Sweeper) pruneDirs(files []store.ArtifactFile) {
	for _, f := range files {
		dir := path.Dir(f.FilePath)
		for strings.Count(dir, "/") >= 2 {
			if err := os.Remove(filepath.Join(s.cfg.Root, filepath.FromSlash(dir))); err != nil {
				break
			}
			dir = path.Dir(dir)
		}
	}
}
```

Create `internal/sweep/steps.go`:

```go
package sweep

import (
	"context"
	"database/sql"
	"fmt"

	"idios/internal/store"
)

const eventBatch = 1000

// steps lists the pass in dependency order: pods first so their cascades do
// the work, then what is left on live pods, then the independent tables.
func (s *Sweeper) steps() []step {
	return []step{
		{"pods", s.sweepPods},
		{"incidents", s.sweepIncidents},
		{"container_state_history", s.countStep(store.DeleteExpiredContainerHistory)},
		{"pod_condition_history", s.countStep(store.DeleteExpiredConditionHistory)},
		{"jobs", s.countStep(store.DeleteExpiredJobs)},
		{"rollout_history", s.countStep(store.DeleteExpiredRollouts)},
		{"k8s_events", s.sweepEvents},
	}
}

func (s *Sweeper) countStep(fn func(context.Context, *sql.Tx, string) (int64, error)) func(context.Context, string) result {
	return func(ctx context.Context, cutoff string) result {
		var r result
		err := s.w.Tx(ctx, func(tx *sql.Tx) (err error) {
			r.rows, err = fn(ctx, tx, cutoff)
			return err
		})
		if err != nil {
			r.errs = append(r.errs, err)
		}
		return r
	}
}

// sweepPods removes each expired pod in its own transaction: files first,
// then the row, whose cascades take everything under it. A pod whose file
// cannot be deleted is skipped whole and tried again next pass.
func (s *Sweeper) sweepPods(ctx context.Context, cutoff string) result {
	var r result
	var uids []string
	err := s.w.Tx(ctx, func(tx *sql.Tx) (err error) {
		uids, err = store.ListExpiredPods(ctx, tx, cutoff)
		return err
	})
	if err != nil {
		r.errs = append(r.errs, err)
		return r
	}
	for _, uid := range uids {
		var files, bytes int64
		err := s.w.Tx(ctx, func(tx *sql.Tx) error {
			arts, err := store.ListPodArtifactFiles(ctx, tx, uid)
			if err != nil {
				return err
			}
			if files, bytes, err = s.removeFiles(arts); err != nil {
				return fmt.Errorf("pod %s: %w", uid, err)
			}
			s.pruneDirs(arts)
			return store.DeletePod(ctx, tx, uid)
		})
		if err != nil {
			r.errs = append(r.errs, err)
			continue
		}
		r.rows++
		r.files += files
		r.bytes += bytes
	}
	return r
}

// sweepIncidents removes each expired incident in its own transaction:
// files, then their rows, then the incident. The schema only detaches
// artifact rows from a deleted incident, so a row whose file is gone
// would otherwise linger until the orphan pass. History and event rows
// are detached and age out on their own.
func (s *Sweeper) sweepIncidents(ctx context.Context, cutoff string) result {
	var r result
	var ids []int64
	err := s.w.Tx(ctx, func(tx *sql.Tx) (err error) {
		ids, err = store.ListExpiredIncidents(ctx, tx, cutoff)
		return err
	})
	if err != nil {
		r.errs = append(r.errs, err)
		return r
	}
	for _, id := range ids {
		var files, bytes int64
		err := s.w.Tx(ctx, func(tx *sql.Tx) error {
			arts, err := store.ListIncidentArtifactFiles(ctx, tx, id)
			if err != nil {
				return err
			}
			if files, bytes, err = s.removeFiles(arts); err != nil {
				return fmt.Errorf("incident %d: %w", id, err)
			}
			ids := make([]int64, 0, len(arts))
			for _, a := range arts {
				ids = append(ids, a.ID)
			}
			if _, err := store.DeleteArtifactRows(ctx, tx, ids); err != nil {
				return err
			}
			return store.DeleteIncident(ctx, tx, id)
		})
		if err != nil {
			r.errs = append(r.errs, err)
			continue
		}
		r.rows++
		r.files += files
		r.bytes += bytes
	}
	return r
}

// sweepEvents deletes in batches, one transaction each, so a large backlog
// never holds the writer for the whole table.
func (s *Sweeper) sweepEvents(ctx context.Context, cutoff string) result {
	var r result
	for {
		var n int64
		err := s.w.Tx(ctx, func(tx *sql.Tx) (err error) {
			n, err = store.DeleteExpiredEventsBatch(ctx, tx, cutoff, eventBatch)
			return err
		})
		if err != nil {
			r.errs = append(r.errs, err)
			return r
		}
		r.rows += n
		if n < eventBatch {
			return r
		}
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/sweep/ -v 2>&1 | tail -5`
Expected: PASS. `go vet ./internal/sweep/` clean (an unused import in `sweep.go` fails the build; every import listed is used by the code above).

- [ ] **Step 5: Checkpoint and commit**

```bash
go build ./... && go test ./... && make ascii
git add internal/sweep/sweep.go internal/sweep/steps.go internal/sweep/sweep_test.go
git commit -m "sweep: add the sweeper with the row retention steps"
```

---

### Task 5: Orphan passes, `sweep_runs` prune, checkpoint

**Files:**
- Create: `internal/sweep/orphans.go`
- Modify: `internal/sweep/steps.go` (append four steps to `steps()`)
- Modify: `internal/sweep/sweep_test.go` (seed files, extend the expectations, add the skip-on-failure test)

**Interfaces:**
- Consumes: Task 3 `ListArtifactFiles`, `DeleteArtifactRows`, `DeleteExpiredSweepRuns`, `Writer.Checkpoint`; Task 4 `step`, `result`, `countStep`, harness helpers.
- Produces: `const orphanGrace = 10 * time.Minute`; `func (s *Sweeper) sweepOrphanFiles(ctx context.Context, _ string) result`; `func (s *Sweeper) sweepOrphanRows(ctx context.Context, _ string) result`; `func (s *Sweeper) checkpoint(ctx context.Context, _ string) result`. Nothing later depends on these names.

Tests trace to process doc 8 steps 5-8 (orphan files with the ten-minute grace, orphan rows, `sweep_runs` by age, `wal_checkpoint(TRUNCATE)` as the last recorded step), storage doc 7 write order ("a crash between the two leaves an orphan file, which the sweeper's orphan pass removes"), storage doc 8 deletion order ("read its artifact paths -> delete the files -> delete the pod row ... If file deletion fails midway, stop and retry next run") and process doc 8 step 1 ("skip that pod, record the error in `sweep_runs.error`, continue with the next") and 10 ("any file operation error ... is recorded in the row that describes it"). The roadmap's "Phase 5 hands Phase 6" paragraph names both orphan shapes: a leftover under `tmp/` and a file in place with no row.

- [ ] **Step 1: Extend the test**

Add to `internal/sweep/sweep_test.go` (imports gain `"os"`, `"io/fs"`, `"strings"`):

```go
// seedFiles puts one file behind every artifacts row that should own one,
// two orphans (a stale temp file and a log with no row), and one temp file
// young enough to be a capture in flight. Row 4 gets no file on purpose.
func (h *harness) seedFiles(t *testing.T) {
	t.Helper()
	files := []struct {
		rel     string
		content string
		mtime   time.Time
	}{
		{"1/idios-smoke/gone-old/api/restart_000.log", "12345", testNow.Add(-7 * 24 * time.Hour)},
		{"1/idios-smoke/live/api/current.log", "1234567", testNow.Add(-7 * 24 * time.Hour)},
		{"1/idios-smoke/live/pod.json", "{}", testNow.Add(-time.Hour)},
		{"1/idios-smoke/live/api/restart_009.log", "abc", testNow.Add(-time.Hour)},
		{"tmp/capture-fresh", "xx", testNow},
		{"tmp/capture-stale", "yyyy", testNow.Add(-20 * time.Minute)},
	}
	for _, f := range files {
		p := filepath.Join(h.root, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, f.mtime, f.mtime); err != nil {
			t.Fatal(err)
		}
	}
}

func (h *harness) files(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(h.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(h.root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
```

Replace `TestSweepRemovesExpiredRowsInOrderAndRecordsEveryStep` with:

```go
func TestSweepRemovesExpiredRowsAndFilesInOrderAndRecordsEveryStep(t *testing.T) {
	h := newHarness(t)
	h.seedRows(t)
	h.seedFiles(t)

	if err := h.sw.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := survivors{
		Pods:      []string{"gone-recent", "live"},
		Incidents: []string{"21", "22"},
		History:   []string{"3", "6"},
		Condition: []string{"2"},
		Jobs:      []string{"job-live-old"},
		Rollouts:  []string{"rs-live"},
		Events:    []string{"ev-live-old", "ev-recent"},
		Artifacts: []string{"3"},
	}
	if d := cmp.Diff(want, h.survivors(t)); d != "" {
		t.Fatal(d)
	}
	if d := cmp.Diff([]string{"1/idios-smoke/live/pod.json", "tmp/capture-fresh"}, h.files(t)); d != "" {
		t.Fatal(d)
	}
	if _, err := os.Stat(filepath.Join(h.root, "1", "idios-smoke", "gone-old")); !os.IsNotExist(err) {
		t.Fatalf("pod directory of a swept pod still exists (err=%v)", err)
	}
	wantRuns := []store.SweepRun{
		{RanAt: recentTS, Cutoff: recentTS, TableName: "pods"},
		runRow("pods", 1, 1, 5),
		runRow("incidents", 1, 1, 7),
		runRow("container_state_history", 3, 0, 0),
		runRow("pod_condition_history", 1, 0, 0),
		runRow("jobs", 2, 0, 0),
		runRow("rollout_history", 2, 0, 0),
		runRow("k8s_events", 1, 0, 0),
		runRow("orphan_files", 0, 2, 7),
		runRow("orphan_rows", 1, 0, 0),
		runRow("sweep_runs", 1, 0, 0),
		runRow("wal_checkpoint", 0, 0, 0),
	}
	if d := cmp.Diff(wantRuns, h.sweepRuns(t)); d != "" {
		t.Fatal(d)
	}
}

func TestPodWhoseFileCannotBeDeletedIsSkippedAndRecorded(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	h := newHarness(t)
	h.seedRows(t)
	h.seedFiles(t)
	locked := filepath.Join(h.root, "1", "idios-smoke", "gone-old", "api")
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	if err := h.sw.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	if d := cmp.Diff([]string{"gone-old", "gone-recent", "live"}, h.survivors(t).Pods); d != "" {
		t.Fatal(d)
	}
	if _, err := os.Stat(filepath.Join(locked, "restart_000.log")); err != nil {
		t.Fatalf("file of the skipped pod: %v", err)
	}
	runs := h.sweepRuns(t)
	var pods store.SweepRun
	for _, r := range runs {
		if r.TableName == "pods" && r.RanAt == nowTS {
			pods = r
		}
	}
	if pods.Error == nil || !strings.Contains(*pods.Error, "gone-old") {
		t.Fatalf("pods row error = %v, want it to name the pod", pods.Error)
	}
	pods.Error = nil
	if d := cmp.Diff(runRow("pods", 0, 0, 0), pods); d != "" {
		t.Fatal(d)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sweep/ 2>&1 | head -30`
Expected: the first test fails on `Artifacts` (row 4 still there) and on the `sweepRuns` diff (seven rows, not eleven, and the old row still present); the second fails on the pods row (rows_removed 1: without the orphan steps the pods step still runs, but the locked directory makes the file delete fail, so check the error text it reports and move on to Step 3 either way).

- [ ] **Step 3: Write the implementation**

Create `internal/sweep/orphans.go`:

```go
package sweep

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"idios/internal/store"
)

// orphanGrace is how young a file with no row may be before it counts as an
// orphan. The pool writes the file first and the row in a later
// transaction, so a file this fresh may simply not have its row yet.
const orphanGrace = 10 * time.Minute

// sweepOrphanFiles deletes every file under the root that no artifacts row
// owns, temp files included, except those inside the grace period.
func (s *Sweeper) sweepOrphanFiles(ctx context.Context, _ string) result {
	var r result
	owned := map[string]bool{}
	err := s.w.Tx(ctx, func(tx *sql.Tx) error {
		files, err := store.ListArtifactFiles(ctx, tx)
		for _, f := range files {
			owned[f.FilePath] = true
		}
		return err
	})
	if err != nil {
		r.errs = append(r.errs, err)
		return r
	}
	edge := s.clk.Now().Add(-orphanGrace)
	err = filepath.WalkDir(s.cfg.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.cfg.Root, p)
		if err != nil {
			return err
		}
		if owned[filepath.ToSlash(rel)] {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.ModTime().After(edge) {
			return nil
		}
		if err := os.Remove(p); err != nil {
			r.errs = append(r.errs, err)
			return nil
		}
		r.files++
		r.bytes += fi.Size()
		return nil
	})
	// No root yet means nothing was ever captured, not a failure.
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		r.errs = append(r.errs, err)
	}
	return r
}

// sweepOrphanRows deletes artifact rows whose file is gone, so the table
// never claims a file the disk does not have.
func (s *Sweeper) sweepOrphanRows(ctx context.Context, _ string) result {
	var r result
	err := s.w.Tx(ctx, func(tx *sql.Tx) error {
		files, err := store.ListArtifactFiles(ctx, tx)
		if err != nil {
			return err
		}
		var gone []int64
		for _, f := range files {
			_, err := os.Stat(filepath.Join(s.cfg.Root, filepath.FromSlash(f.FilePath)))
			if errors.Is(err, fs.ErrNotExist) {
				gone = append(gone, f.ID)
				continue
			}
			if err != nil {
				return err
			}
		}
		r.rows, err = store.DeleteArtifactRows(ctx, tx, gone)
		return err
	})
	if err != nil {
		r.errs = append(r.errs, err)
	}
	return r
}

func (s *Sweeper) checkpoint(ctx context.Context, _ string) result {
	var r result
	if err := s.w.Checkpoint(ctx); err != nil {
		r.errs = append(r.errs, err)
	}
	return r
}
```

In `internal/sweep/steps.go`, extend `steps()` so the slice ends:

```go
		{"k8s_events", s.sweepEvents},
		{"orphan_files", s.sweepOrphanFiles},
		{"orphan_rows", s.sweepOrphanRows},
		{"sweep_runs", s.countStep(store.DeleteExpiredSweepRuns)},
		{"wal_checkpoint", s.checkpoint},
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sweep/ -v 2>&1 | tail -8`
Expected: both PASS.

- [ ] **Step 5: Checkpoint and commit**

```bash
go build ./... && go test ./... && make ascii
git add internal/sweep/orphans.go internal/sweep/steps.go internal/sweep/sweep_test.go
git commit -m "sweep: add orphan passes, sweep_runs prune and WAL checkpoint"
```

---

### Task 6: Wire the closer and sweeper into `cmd/idios`; record the handoff

**Files:**
- Modify: `cmd/idios/main.go`
- Modify: `docs/plans/m1-recorder/roadmap.md` (Phase 6 section)

**Interfaces:**
- Consumes: `incident.NewCloser(w, clk, window, interval, log)`, `(*incident.Closer).Run(ctx)` (Task 2); `sweep.New(cfg, w, clk, log)`, `(*sweep.Sweeper).Sweep(ctx)`, `(*sweep.Sweeper).Run(ctx)` (Tasks 4, 5); `config.Config.RetentionDays`, `SweepInterval`, `StabilizationWindow`, `StabilizationCheckInterval`, `ArtifactsRoot`.
- Produces: nothing new; the binary runs both loops.

No new test: the wiring is exercised by running the binary (Step 3), and process doc 14 lists no automated test for it. `cmd/idios` has no test today and this task does not add one.

- [ ] **Step 1: Edit `cmd/idios/main.go`**

Add to the import block, keeping it sorted:

```go
	"idios/internal/incident"
	"idios/internal/sweep"
```

Immediately after `proc := processor.New(st.Writer, clock.Real{}, pool, cfg.StabilizationWindow)` insert:

```go
	sweeper := sweep.New(sweep.Config{
		Root: cfg.ArtifactsRoot, Retention: time.Duration(cfg.RetentionDays) * 24 * time.Hour, Interval: cfg.SweepInterval,
	}, st.Writer, clock.Real{}, logger)
	// The first pass runs before any watcher so the retention promise holds
	// from the first second of the process, not from the first tick.
	if err := sweeper.Sweep(ctx); err != nil {
		return fmt.Errorf("startup sweep: %w", err)
	}
	closer := incident.NewCloser(st.Writer, clock.Real{}, cfg.StabilizationWindow, cfg.StabilizationCheckInterval, logger)
```

Add `"time"` to the standard-library imports.

Immediately after the block that starts `pool.Run` in its goroutine (before `<-ctx.Done()`) insert:

```go
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := sweeper.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("sweeper stopped", "err", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := closer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("closer stopped", "err", err)
		}
	}()
```

- [ ] **Step 2: Build**

Run: `go build ./... && go vet ./cmd/idios/`
Expected: clean.

- [ ] **Step 3: Run the binary against an empty data dir**

```bash
mkdir -p /tmp/idios-plan/phase6 && go run ./cmd/idios -data-dir /tmp/idios-plan/phase6 -config /tmp/idios-plan/phase6/idios.toml & sleep 4; kill -INT %1; wait
sqlite3 /tmp/idios-plan/phase6/idios.db "SELECT table_name, rows_removed, files_removed, error FROM sweep_runs ORDER BY id"
```

Expected: the process logs `idios started`, warns `no clusters configured`, and exits on SIGINT with `idios stopping`; the query prints eleven rows in the order `pods, incidents, container_state_history, pod_condition_history, jobs, rollout_history, k8s_events, orphan_files, orphan_rows, sweep_runs, wal_checkpoint`, all zero, all with `error` empty. If `sqlite3` is not on the machine, assert the same with a five-line Go test file under the job tmp directory instead; do not add it to the repo. Do not touch `./.storage` or the cluster.

- [ ] **Step 4: Update the roadmap**

In `docs/plans/m1-recorder/roadmap.md`, under `### Phase 6: closer + sweeper`, replace the "Delivers:" paragraph and add status, tests and handoff, so the section reads:

```markdown
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
`close_reason` closed within the window, including `pod_deleted`. The orphan
pass leaves any file modified within the last ten minutes alone (not only
`tmp/`), because a renamed capture may not have its row yet. A pod whose
artifact file cannot be deleted keeps its row and is retried next pass;
`sweep_runs.error` names it. The sweeper prunes empty pod and container
directories only; namespace and cluster directories stay. Nothing in this
phase recovers panics or counts errors beyond the `sweep_runs` rows.
```

Also change the `Phase 6  closer + sweeper` line in the phases block if it carries a status note (it does not today; leave the block as is).

- [ ] **Step 5: Checkpoint and commit**

```bash
go build ./... && go test ./... && make ascii
git add cmd/idios/main.go
git commit -m "cmd: run the startup sweep, the sweeper and the closer"
git add docs/plans/m1-recorder/roadmap.md
git commit -m "docs: record phase 6 completion and handoff in roadmap"
```

---

## Self-review

**Spec coverage.** Process doc 7: ticker at `stabilization_check_interval` (Task 2 `Run`), one `Writer.Tx` with two statements (Task 2 `Tick`), stable per container kind and `scheduling` by `node_name` (Task 1 `CloseStableIncidents` and its table), late attach with the 5.10 rule for incidents closed within the window (Task 1 `AttachLateEvents` and its table), job incidents not closed here (Task 1 row), terminal-phase pods never stable (Task 1 rows "terminated exit 0 stays", "waiting stays"), no memory between ticks (Task 2 idempotency assertion). Process doc 8: startup pass before watchers and hourly after (Task 6), steps 1-8 in order (Tasks 4, 5 `steps()`), file-then-row and skip-on-failure (Task 4 `sweepPods`, Task 5 second test), events in batches of 1000 (Task 4 `sweepEvents`, Task 3 batch test), orphan files with grace and `tmp/` (Task 5), orphan rows (Task 5), `sweep_runs` prune (Task 5), checkpoint (Task 3 `Checkpoint`, Task 5 step), one row per step including zero-row steps (Task 4/5 `wantRuns` with `wal_checkpoint` at 0). Process doc 9: every timestamp through `clock.Format`, process time from `clock.Clock` (Tasks 2, 4). Process doc 10: file errors recorded in `sweep_runs.error` (Task 4 `Sweep`, Task 5 test). Process doc 14.2: injected clock, exact rows and files, `sweep_runs` asserted (Tasks 4, 5). Storage 5.12 columns all written (Task 3 `InsertSweepRun`). Storage 6.3 table: `recovered` by the closer only (Task 1), `scheduling` recovered when scheduled (Task 1 row). Storage 7 write order and orphan (Task 5). Storage 8 table row by row (Task 3 tests, Task 4 test), the two "never" rules (Task 4 `live` pod and incident 21 survive), reconcile slack is by construction (nothing to do). Storage 10: WAL checkpoint (Task 3). Roadmap Phase 6 section: `internal/incident/closer.go`, `internal/sweep`, `store/sweep_sql.go` (all tasks); "Phase 5 hands Phase 6": both orphan shapes covered (Task 5 seeds `tmp/capture-stale` and `restart_009.log`), `file_path` set is the only kind that owns a file (Task 3 `listArtifactFiles` filters `file_path IS NOT NULL`).

**Placeholder scan.** Every step has its code or its exact command and expected output. No "TBD", no "similar to", no "add error handling". Task 6 Step 3 gives the fallback if `sqlite3` is missing.

**Type consistency.** `CloseStableIncidents(ctx, tx, closedAt, staleBefore string) (int64, error)` and `AttachLateEvents(ctx, tx, closedSince string) (int64, error)` (Task 1) are what `Closer.Tick` calls (Task 2). `NewCloser(w *store.Writer, clk clock.Clock, window, interval time.Duration, log *slog.Logger)` (Task 2) is what Task 6 calls with `cfg.StabilizationWindow, cfg.StabilizationCheckInterval`. `TickResult{Closed, Attached int64}` is compared with `cmp.Diff` in Task 2. Task 3 names (`ListExpiredPods`, `ListExpiredIncidents`, `ListPodArtifactFiles`, `ListIncidentArtifactFiles`, `ListArtifactFiles`, `DeletePod`, `DeleteIncident`, `DeleteExpiredContainerHistory`, `DeleteExpiredConditionHistory`, `DeleteExpiredJobs`, `DeleteExpiredRollouts`, `DeleteExpiredEventsBatch(ctx, tx, cutoff string, limit int)`, `DeleteArtifactRows(ctx, tx, ids []int64)`, `DeleteExpiredSweepRuns`, `InsertSweepRun`, `(*Writer).Checkpoint`) match every call in Tasks 4 and 5; `countStep` takes `func(context.Context, *sql.Tx, string) (int64, error)`, the shape of every `DeleteExpired*` helper. `store.ArtifactFile{ID, FilePath}` is what `removeFiles` and `pruneDirs` read. `sweep.Config{Root, Retention, Interval}` and `New(cfg, w, clk, log)` (Task 4) are what the harness and Task 6 build. `store.SweepRun` field names in `sweepRuns` (test) match `rows.go`. The `step.run` signature `func(context.Context, string) result` is shared by `sweepPods`, `sweepIncidents`, `sweepEvents`, `sweepOrphanFiles`, `sweepOrphanRows`, `checkpoint` and the closures `countStep` returns.

**`.ai` rules.** ascii-only: every block is ASCII; `make ascii` at each checkpoint. tests: each test traced above; variants as table rows (stable predicate, attach rule, jobs); whole row sets via `cmp.Diff` on `survivors`, `[]store.SweepRun`, `map[string]*int64`; edge cases first (edge-of-window timestamp, pod-level incidents, job incidents, open incident, pre-attached event, missing file, locked directory, empty root). comments: doc comments on every exported identifier, why-comments only (kubelet `ready` semantics, checkpoint-in-transaction, grace period, cascade order); no document named in code. code-is-truth: no plan or section cited in code; the roadmap update states what is. scope: no counters, no config reload, no panic recovery, no `TxRunner` interface, no directory pruning beyond the pod tree. commits: one per task (two in Task 6: code and docs), `area: imperative subject`, named paths, no trailers.
