# Phase 7: Status, Smoke, Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the single-run binary into `idios run` / `idios status` / `idios cluster add` / `idios ns add`, with in-process counters, JSON file logging, panic recovery at the handler boundary, config reload from the two configuration tables, a `make smoke` target against the OrbStack cluster, the deferred hardening items from Phases 5 and 6, and the release default for `data_dir`.

**Architecture:** `internal/status` holds the in-process counters (`Counters`) and the JSON `Snapshot` the daemon writes to `<data_dir>/status.json` every ten seconds; `idios status` runs as a second process, so it reads that file plus display queries in `store/status_sql.go` through `store.Reader`. `cmd/idios` becomes a dispatcher with one file per command; a `supervisor` re-reads `clusters` and `watched_namespaces` every ten seconds and restarts only the watchers whose rows changed, with `capture.Pool` growing start-on-add and `RemoveCluster`. The k8s handler boundary recovers panics and counts errors; `store.Writer` reports every transaction to an observer; the early cache reserves before it fetches; `Writer.Checkpoint` reports a busy checkpoint.

**Tech Stack:** Go 1.24, `log/slog`, `modernc.org/sqlite` v1.45.0, client-go v0.34.1, `github.com/BurntSushi/toml`, `github.com/google/go-cmp`. Smoke: `kubectl`, `sqlite3`, OrbStack Kubernetes.

**Spec:** `docs/design/process-architecture.md` Section 5 step 6 and the "Config changes" paragraph, Sections 10, 11, 12, 13, 14 (closing paragraph on `make smoke`); `docs/design/data-storage.md` Sections 5.1, 5.2, 11. Roadmap `docs/plans/m1-recorder/roadmap.md`: the Phase 7 section and every "hands Phase 7" paragraph. The plan may cite these; the code must not (`.ai/code-is-truth.md`).

## Global Constraints

- ASCII only in every file written, including this one; `make ascii` at every checkpoint (`.ai/ascii-only.md`).
- Tests trace to a spec statement or a bug named in this plan; table-driven; exact assertions on whole structs, row sets or output (`.ai/tests.md`).
- Comments say why, never what; every exported identifier has its one-line doc comment; no code comment names a document, section or plan (`.ai/comments.md`, `.ai/code-is-truth.md`).
- Build only what a task names (`.ai/scope.md`).
- One commit per task, `area: imperative subject`, no trailers, `git add` named paths, never commit red (`.ai/commits.md`).
- Checkpoint at the end of every task: `go build ./... && go vet ./... && go test ./... && make ascii`.
- Every stored timestamp goes through `clock.Format`; process time comes from `clock.Clock`. Durations measured for counters are the one place `time.Since` is used, because they are never stored.
- Every mutation goes through `store.Writer.Tx`; the display reads of `idios status` go through `store.Reader`.
- No migration after `0001_init.sql`. If a task seems to need one, stop and report; do not add it.
- Test cluster: OrbStack via `KUBECONFIG=./kube/config`, namespace `idios-smoke` only. Never read `~/.kube/config`; never touch other namespaces.
- Model choice per roadmap: implementer Sonnet, reviewer Opus, orchestrator Fable.

## Decisions made while reading the code

- **`idios status` is a second process, so the daemon publishes a file.** The process doc says the counters are "kept in a `status` struct read by `idios status` and a later `/status` endpoint", but no listener exists yet and a new table would be a migration. The daemon writes `<data_dir>/status.json` (mode 0600, temp file then rename) every `status.Interval` (10 s) and removes it on clean exit; `idios status` calls a snapshot older than `status.Stale` (30 s) stale. Everything the database already knows (`last_error`, incidents open by category, closed by reason, artifacts by `capture_gap`, latest `sweep_runs` per table, row counts) is read from the database, not duplicated in the file.
- **In-process counters are only what the database cannot say:** writer transactions, errors and p99 (a ring of the last 1024 durations); handler errors and recovered panics; last object time per cluster; capture queued / completed / dropped and completed rows by gap; closer last tick and counts; per-cluster ready and skew. The process doc's "ingest: pod events, transitions inserted, incidents opened, attached, reopened, closed by reason" are served from the tables (transitions = `container_state_history` rows, incidents by state and reason); "pod events" is the per-cluster last-event time plus the handler counters.
- **Per-cluster, not per-namespace, sync state.** `k8s.Watcher.Ready()` is one flag for the cluster; per-namespace informer state is not exposed and this phase does not add it. `idios status` shows `ready` per cluster.
- **Counters reach the watcher as a `*status.Counters` argument to `k8s.New`.** `status` imports only `clock`, so `k8s` importing it keeps every dependency rule. `capture.Pool` and `incident.Closer` keep their own counts (`Pool.Stats()`, `Closer.Last()`) and the daemon assembles the snapshot; that avoids threading one struct through every constructor.
- **`store.Writer.Observer`** is an exported field of interface type `store.TxObserver`, set once by `cmd/idios` after `Open`. `*status.Counters` implements it.
- **Config reload polls the two tables every 10 s** (`reloadInterval` in `cmd/idios`). The CLI writes from another process, so there is no in-process signal; polling is what "re-reads them when the user edits them" costs. A cluster whose `context_name` or namespace set changed is stopped and started again from step 1; others are untouched, as the process doc asks. A removed namespace is marked `unwatched` by the new watcher's reconcile pass, which already answers "not live" for rows outside its namespace list. A removed cluster row stops its watcher; its rows stay for the sweeper.
- **`capture.Pool` grows start-on-add and `RemoveCluster`**, rather than the daemon rebuilding the pool on reload: rebuilding would drop every queued request of every cluster for a one-row change.
- **The `400` on a waiting container's current instance stays `capture_gap = 'unknown'`.** A named value needs a new `CHECK` member, which is a migration. Recorded in the roadmap handoff as decided.
- **The two artifacts writers stay.** `store.InsertArtifactGap` keeps the first gap (`unobservable` rows the pool never touches); `store.UpsertArtifact` replaces a gap with a newer gap or a file. Folding them would change one of those behaviours. Recorded as decided.
- **Checkpoint busy column.** `Writer.Checkpoint` reads the `busy` column of `PRAGMA wal_checkpoint(TRUNCATE)` and returns `store.ErrCheckpointBusy` when it is 1, so the `wal_checkpoint` sweep row records that the WAL was not truncated instead of claiming success.
- **`data_dir` flips to the OS user data directory** (`~/Library/Application Support/idios` on darwin, `$XDG_DATA_HOME/idios` or `~/.local/share/idios` elsewhere, `%LocalAppData%\idios` on windows). The config file default follows: `<data_dir>/idios.toml`, where `data_dir` is the `-data-dir` flag when given. `kubeconfig` flips to clientcmd's default resolution (`KUBECONFIG`, then `~/.kube/config`) because a repo-relative path is not a release default; every run in this repository passes `-kubeconfig ./kube/config` (the Makefile does), so `~/.kube/config` is never read here.
- **Logging.** JSON to `<data_dir>/idios.log`, rotated once at 10 MiB to `idios.log.1` (one generation: the database is the record, the log is for the last stop). Text to stderr only when stderr is a character device. A small `teeHandler` fans out; no dependency added.
- **Subcommands.** `idios [-config f] [-data-dir d] [-kubeconfig f] <run|status|cluster add|ns add|version>`. A bare `idios` prints usage and fails. `make run` becomes `./bin/idios -data-dir .storage -kubeconfig ./kube/config run`.
- **Smoke fixtures** are four manifests under `hack/smoke/`: a crash-loop Deployment (exercises the ReplicaSet owner chain), a bad-image Pod, a missing-configmap Pod, and an OOM Pod with a 10Mi limit whose shell doubles a string until the kernel kills it (busybox needs no extra image). `make smoke` applies them into `idios-smoke`, runs the daemon for two minutes against `.storage/smoke`, prints `idios status` and the `incidents` and `artifacts` tables, and deletes the fixtures on exit.

## File structure

```
internal/store/config_sql.go          InsertCluster, FindClusterByName, AddWatchedNamespace
internal/store/config_sql_test.go
internal/store/status_sql.go          Querier, OpenIncidentsByCategory, ClosedIncidentsByReason,
                                      ArtifactsByOutcome, RowCounts, CountRows, LatestSweepRuns
internal/store/status_sql_test.go
internal/store/cluster_sql.go         ListClusters takes Querier
internal/store/writer.go              TxObserver, Writer.Observer, timing in Tx
internal/store/writer_test.go         observer sees commit and rollback
internal/store/sweep_sql.go           ErrCheckpointBusy, Checkpoint reads busy
internal/store/sweep_sql_test.go      busy checkpoint
internal/status/status.go             Counters, Writer, Handlers, Cluster, Capture, Closer, Snapshot,
                                      Interval, Stale, FileName, WriteFile, ReadFile
internal/status/status_test.go
internal/archtest/deps_test.go        status imports only clock
internal/incident/closer.go           Closer.Last
internal/incident/closer_test.go      Last after Tick
internal/capture/pool.go              Stats, start-on-add, RemoveCluster
internal/capture/worker.go            completed counts, reserve/release
internal/capture/early.go             reserve, release, pending
internal/capture/early_test.go        reserve rows
internal/capture/pool_test.go         running pool add/remove, Stats
internal/k8s/watcher.go               New takes *status.Counters, guard
internal/k8s/handler.go               guard with recover
internal/k8s/handler_test.go          errors counted, panics recovered
internal/k8s/client.go                empty path uses default loading rules
internal/k8s/client_test.go
internal/k8s/watcher_test.go          New signature
internal/config/config.go             DefaultDataDir, dataDir, Kubeconfig default ""
internal/config/config_test.go
cmd/idios/main.go                     dispatch, global flags, loadConfig, openStore, usage
cmd/idios/run.go                      runDaemon
cmd/idios/log.go                      rotatingFile, teeHandler, newLogger
cmd/idios/log_test.go
cmd/idios/config_cmd.go               runCluster, runNamespace
cmd/idios/config_cmd_test.go
cmd/idios/supervise.go                clusterSpec, specsFromRows, reloadPlan, supervisor
cmd/idios/supervise_test.go
cmd/idios/status.go                   writeSnapshot, statusLoop, report, collectReport, renderReport, runStatus
cmd/idios/status_test.go
hack/smoke/crash-loop.yaml            Deployment smoke-crash
hack/smoke/bad-image.yaml             Pod smoke-bad-image
hack/smoke/missing-configmap.yaml     Pod smoke-missing-config
hack/smoke/oom.yaml                   Pod smoke-oom
hack/smoke/run.sh                     the smoke script
Makefile                              run, smoke
docs/plans/m1-recorder/roadmap.md   Phase 7 status; roadmap complete
```

Every test below has a one-line trace to a spec statement or a bug. No trace, no test.

---

### Task 1: Configuration table writes in `store/config_sql.go`

**Files:**
- Create: `internal/store/config_sql.go`
- Create: `internal/store/config_sql_test.go`

**Interfaces:**
- Consumes: `store.Cluster`, `clusterColumns`, `scanCluster` (`cluster_sql.go`); test helpers `openMigratedStore`, `inTx`, `testEpoch`, `ptr`.
- Produces: `func InsertCluster(ctx context.Context, tx *sql.Tx, name, contextName, now string) (int64, error)`; `func FindClusterByName(ctx context.Context, tx *sql.Tx, name string) (*Cluster, error)` (nil, nil when absent); `func AddWatchedNamespace(ctx context.Context, tx *sql.Tx, clusterID int64, name, now string) (bool, error)` (false when the row already existed).

Tests trace to storage doc 5.2 (`watched_namespaces` is populated by the user, `UNIQUE (cluster_id, name)`, the two tables are the single source of truth) and 5.1 (a cluster row before its first connection has no identity and matches on `api_server_url`, so the CLI writes `''` and leaves the watcher to fill it).

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

func TestInsertClusterStartsUnconnectedAndIsFoundByName(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	now := clock.Format(clk.Now())
	var id int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		id, err = InsertCluster(ctx, tx, "orbstack", "orbstack-admin", now)
		return err
	})
	cases := []struct {
		name string
		want *Cluster
	}{
		{"orbstack", &Cluster{ID: id, Name: "orbstack", ContextName: "orbstack-admin", FirstSeenAt: now}},
		{"nowhere", nil},
	}
	for _, c := range cases {
		var got *Cluster
		inTx(t, s, func(tx *sql.Tx) (err error) {
			got, err = FindClusterByName(ctx, tx, c.name)
			return err
		})
		if d := cmp.Diff(c.want, got); d != "" {
			t.Errorf("FindClusterByName(%q): %s", c.name, d)
		}
	}
}

func TestAddWatchedNamespaceIsIdempotentPerCluster(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	now := clock.Format(clk.Now())
	var c1, c2 int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		if c1, err = InsertCluster(ctx, tx, "a", "a", now); err != nil {
			return err
		}
		c2, err = InsertCluster(ctx, tx, "b", "b", now)
		return err
	})
	steps := []struct {
		name    string
		cluster int64
		ns      string
		want    bool
	}{
		{"first add", c1, "idios-smoke", true},
		{"same namespace again", c1, "idios-smoke", false},
		{"same name in another cluster", c2, "idios-smoke", true},
		{"second namespace", c1, "payments", true},
	}
	for _, st := range steps {
		var got bool
		inTx(t, s, func(tx *sql.Tx) (err error) {
			got, err = AddWatchedNamespace(ctx, tx, st.cluster, st.ns, now)
			return err
		})
		if got != st.want {
			t.Errorf("%s: added = %v, want %v", st.name, got, st.want)
		}
	}
	var got []string
	inTx(t, s, func(tx *sql.Tx) (err error) {
		got, err = ListWatchedNamespaces(ctx, tx, c1)
		return err
	})
	if d := cmp.Diff([]string{"idios-smoke", "payments"}, got); d != "" {
		t.Fatal(d)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/store/ -run 'TestInsertCluster|TestAddWatchedNamespace'`
Expected: compile error, `undefined: InsertCluster`.

- [ ] **Step 3: Implement**

```go
package store

import (
	"context"
	"database/sql"
	"errors"
)

// InsertCluster adds a cluster row the watcher has not reached yet: no
// identity and an empty api_server_url, both filled on the first connection.
func InsertCluster(ctx context.Context, tx *sql.Tx, name, contextName, now string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
INSERT INTO clusters (name, context_name, api_server_url, first_seen_at) VALUES (?, ?, '', ?)`, name, contextName, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindClusterByName returns the cluster row with that name, nil when there
// is none. The schema does not make names unique; the CLI refuses a second
// row of the same name, so the lowest id is the one it created.
func FindClusterByName(ctx context.Context, tx *sql.Tx, name string) (*Cluster, error) {
	c, err := scanCluster(tx.QueryRowContext(ctx, "SELECT "+clusterColumns+" FROM clusters WHERE name = ? ORDER BY id LIMIT 1", name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AddWatchedNamespace records that name is watched in the cluster and
// reports whether the row is new.
func AddWatchedNamespace(ctx context.Context, tx *sql.Tx, clusterID int64, name, now string) (bool, error) {
	n, err := execCount(ctx, tx, `
INSERT INTO watched_namespaces (cluster_id, name, added_at) VALUES (?, ?, ?)
ON CONFLICT (cluster_id, name) DO NOTHING`, clusterID, name, now)
	return n == 1, err
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/store/ -run 'TestInsertCluster|TestAddWatchedNamespace'`
Expected: PASS.

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/store/config_sql.go internal/store/config_sql_test.go
git commit -m "store: add cluster and watched namespace inserts"
```

---

### Task 2: Display reads in `store/status_sql.go`

**Files:**
- Create: `internal/store/status_sql.go`
- Create: `internal/store/status_sql_test.go`
- Modify: `internal/store/cluster_sql.go` (`ListClusters` parameter type)

**Interfaces:**
- Consumes: `SweepRun`, `InsertSweepRun`, test helpers `openMigratedStore`, `insertCluster`, `insertPod`, `insertPodIncident`, `closeIncident`, `insertArtifact`, `mustExec`, `inTx`, `ptr`.
- Produces: `type Querier interface { QueryContext(...) (*sql.Rows, error); QueryRowContext(...) *sql.Row }` (satisfied by `*sql.DB` and `*sql.Tx`); `func ListClusters(ctx context.Context, db Querier) ([]Cluster, error)`; `func OpenIncidentsByCategory(ctx context.Context, db Querier) (map[string]int64, error)`; `func ClosedIncidentsByReason(ctx context.Context, db Querier) (map[string]int64, error)`; `func ArtifactsByOutcome(ctx context.Context, db Querier) (map[string]int64, error)` (key `file` for rows with a file, else the gap); `type RowCounts struct { Pods, LivePods, Transitions, Events int64 }`; `func CountRows(ctx context.Context, db Querier) (RowCounts, error)`; `func LatestSweepRuns(ctx context.Context, db Querier) ([]SweepRun, error)` (the newest row per `table_name`, in id order).

Tests trace to process doc 11 (counters: incidents closed by reason, capture by `capture_gap`, sweep last run and per-table rows removed, transitions inserted) and storage doc 5.12 (`sweep_runs` written by the sweeper, one row per table per pass; status reads the latest).

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

func TestStatusReadsGroupIncidentsArtifactsAndRows(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	now := clock.Format(clk.Now())
	c := insertCluster(t, s, "c")
	insertPod(t, s, c, "p1")
	insertPod(t, s, c, "p2")
	mustExec(t, s, "UPDATE pods SET deleted_at = ? WHERE uid = 'p2'", now)
	open1, _ := insertPodIncident(t, s, c, "p1", "api", CategoryCrash)
	if _, err := insertPodIncident(t, s, c, "p1", "web", CategoryCrash); err != nil {
		t.Fatal(err)
	}
	if _, err := insertPodIncident(t, s, c, "p1", "db", CategoryImagePull); err != nil {
		t.Fatal(err)
	}
	closed1, _ := insertPodIncident(t, s, c, "p2", "api", CategoryOOM)
	closeIncident(t, s, closed1, CloseRecovered)
	closed2, _ := insertPodIncident(t, s, c, "p2", "web", CategoryOOM)
	closeIncident(t, s, closed2, ClosePodDeleted)
	if err := insertArtifact(t, s, "p1", "api", ArtifactLogPrevious, 0, &open1); err != nil {
		t.Fatal(err)
	}
	mustExec(t, s, `INSERT INTO artifacts (pod_uid, container_name, kind, restart_count, capture_gap, captured_at) VALUES ('p1', 'api', 'log_current', -1, 'no_output', ?)`, now)
	mustExec(t, s, `INSERT INTO artifacts (pod_uid, container_name, kind, restart_count, capture_gap, captured_at) VALUES ('p1', 'web', 'log_current', -1, 'no_output', ?)`, now)
	mustExec(t, s, `INSERT INTO container_state_history (pod_uid, container_name, image, state, restart_count, observed_at) VALUES ('p1', 'api', 'img', 'waiting', 1, ?)`, now)
	mustExec(t, s, `INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, reason, first_ts, last_ts, raw_json) VALUES (?, 'e1', 'idios-smoke', 'Warning', 'Pod', 'p', 'p1', 'BackOff', ?, ?, '{}')`, c, now, now)

	db := s.Reader.DB()
	open, err := OpenIncidentsByCategory(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(map[string]int64{CategoryCrash: 2, CategoryImagePull: 1}, open); d != "" {
		t.Error("open:", d)
	}
	closed, err := ClosedIncidentsByReason(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(map[string]int64{CloseRecovered: 1, ClosePodDeleted: 1}, closed); d != "" {
		t.Error("closed:", d)
	}
	arts, err := ArtifactsByOutcome(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(map[string]int64{"file": 1, GapNoOutput: 2}, arts); d != "" {
		t.Error("artifacts:", d)
	}
	counts, err := CountRows(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(RowCounts{Pods: 2, LivePods: 1, Transitions: 1, Events: 1}, counts); d != "" {
		t.Error("counts:", d)
	}
}

func TestLatestSweepRunsKeepsTheNewestRowPerTable(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	t0 := clock.Format(clk.Now())
	t1 := clock.Format(clk.Now().Add(time.Hour))
	rows := []SweepRun{
		{RanAt: t0, Cutoff: t0, TableName: "pods", RowsRemoved: 3},
		{RanAt: t0, Cutoff: t0, TableName: "incidents", RowsRemoved: 1},
		{RanAt: t1, Cutoff: t1, TableName: "pods", RowsRemoved: 0, Error: ptr("pod p9: permission denied")},
	}
	inTx(t, s, func(tx *sql.Tx) error {
		for _, r := range rows {
			if err := InsertSweepRun(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
	got, err := LatestSweepRuns(ctx, s.Reader.DB())
	if err != nil {
		t.Fatal(err)
	}
	want := []SweepRun{
		{ID: 2, RanAt: t0, Cutoff: t0, TableName: "incidents", RowsRemoved: 1},
		{ID: 3, RanAt: t1, Cutoff: t1, TableName: "pods", Error: ptr("pod p9: permission denied")},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}
```

Add `"time"` to the test imports.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/store/ -run 'TestStatusReads|TestLatestSweepRuns'`
Expected: compile error, `undefined: OpenIncidentsByCategory`.

- [ ] **Step 3: Implement**

`internal/store/status_sql.go`:

```go
package store

import (
	"context"
	"database/sql"
)

// Querier is what the display reads need; *sql.DB (the Reader) and *sql.Tx
// both provide it.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var (
	_ Querier = (*sql.DB)(nil)
	_ Querier = (*sql.Tx)(nil)
)

func countBy(ctx context.Context, db Querier, query string) (map[string]int64, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// OpenIncidentsByCategory counts open incidents per category.
func OpenIncidentsByCategory(ctx context.Context, db Querier) (map[string]int64, error) {
	return countBy(ctx, db, "SELECT category, COUNT(*) FROM incidents WHERE closed_at IS NULL GROUP BY category")
}

// ClosedIncidentsByReason counts closed incidents per close_reason.
func ClosedIncidentsByReason(ctx context.Context, db Querier) (map[string]int64, error) {
	return countBy(ctx, db, "SELECT close_reason, COUNT(*) FROM incidents WHERE closed_at IS NOT NULL GROUP BY close_reason")
}

// ArtifactsByOutcome counts artifact rows per capture_gap, under "file" for
// rows that captured one.
func ArtifactsByOutcome(ctx context.Context, db Querier) (map[string]int64, error) {
	return countBy(ctx, db, "SELECT COALESCE(capture_gap, 'file'), COUNT(*) FROM artifacts GROUP BY 1")
}

// RowCounts sizes the observation tables.
type RowCounts struct {
	Pods, LivePods, Transitions, Events int64
}

// CountRows returns the pod, live pod, container transition and event counts.
func CountRows(ctx context.Context, db Querier) (RowCounts, error) {
	var c RowCounts
	err := db.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM pods), (SELECT COUNT(*) FROM pods WHERE deleted_at IS NULL),
       (SELECT COUNT(*) FROM container_state_history), (SELECT COUNT(*) FROM k8s_events)`).
		Scan(&c.Pods, &c.LivePods, &c.Transitions, &c.Events)
	return c, err
}

// LatestSweepRuns returns the newest sweep_runs row of every table, in id
// order, so the last pass can be read without the rows before it.
func LatestSweepRuns(ctx context.Context, db Querier) ([]SweepRun, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, ran_at, cutoff, table_name, rows_removed, files_removed, bytes_removed, duration_ms, error
FROM sweep_runs WHERE id IN (SELECT MAX(id) FROM sweep_runs GROUP BY table_name) ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SweepRun
	for rows.Next() {
		var r SweepRun
		if err := rows.Scan(&r.ID, &r.RanAt, &r.Cutoff, &r.TableName, &r.RowsRemoved, &r.FilesRemoved, &r.BytesRemoved, &r.DurationMs, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

In `internal/store/cluster_sql.go` change the signature of `ListClusters` to `func ListClusters(ctx context.Context, db Querier) ([]Cluster, error)` and its body to call `db.QueryContext`. Every existing caller passes a `*sql.Tx` and still compiles.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/store/status_sql.go internal/store/status_sql_test.go internal/store/cluster_sql.go
git commit -m "store: add the display reads behind idios status"
```

---

### Task 3: `internal/status`: counters and snapshot file

**Files:**
- Create: `internal/status/status.go`
- Create: `internal/status/status_test.go`
- Modify: `internal/archtest/deps_test.go` (`onlyInternal` gains `internal/status`)

**Interfaces:**
- Consumes: `clock.Clock`, `clock.Format`.
- Produces:
  - `const Interval = 10 * time.Second`, `const Stale = 3 * Interval`, `const FileName = "status.json"`.
  - `type Counters struct`; `func New(clk clock.Clock) *Counters`; methods `ObserveTx(d time.Duration, err error)`, `HandlerError()`, `HandlerPanic()`, `ObjectSeen(clusterID int64)`, `LastSeen(clusterID int64) (time.Time, bool)`, `Writer() Writer`, `Handlers() Handlers`.
  - `type Writer struct { Transactions, Errors uint64; P99Ms float64 }`, `type Handlers struct { Errors, Panics uint64 }`, `type Cluster struct { ID int64; Ready bool; LastEventAt string; SkewSeconds float64 }`, `type Capture struct { Queued, Completed, Dropped uint64; Gaps map[string]uint64 }`, `type Closer struct { LastTickAt string; Closed, Attached int64 }`, `type Snapshot struct { WrittenAt string; PID int; Version string; Clusters []Cluster; Writer Writer; Handlers Handlers; Capture Capture; Closer Closer }` with snake_case JSON tags.
  - `func WriteFile(path string, s Snapshot) error` (temp file in the same directory, 0600, rename); `func ReadFile(path string) (Snapshot, error)`.

Tests trace to process doc 11 (writer: transactions, errors, p99 duration; per cluster last event time) and process doc 12 (files under the data directory are 0600; the snapshot is written like the artifacts, temp then rename, so a reader never sees a half file).

- [ ] **Step 1: Write the failing tests**

```go
package status

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"idios/internal/clock"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func TestWriterCountersReportP99OverTheRing(t *testing.T) {
	cases := []struct {
		name      string
		durations []time.Duration
		errAt     []int
		want      Writer
	}{
		{"no transactions", nil, nil, Writer{}},
		{"one transaction is its own p99", []time.Duration{3 * time.Millisecond}, nil, Writer{Transactions: 1, P99Ms: 3}},
		{"errors count and time", []time.Duration{time.Millisecond, 5 * time.Millisecond}, []int{1}, Writer{Transactions: 2, Errors: 1, P99Ms: 5}},
		{"p99 of a hundred is the second largest", ramp(100), nil, Writer{Transactions: 100, P99Ms: 99}},
		{"the ring forgets the oldest", ramp(2 * ringSize), nil, Writer{Transactions: 2 * ringSize, P99Ms: float64(2*ringSize - 10)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cs := New(clock.NewFake(testNow))
			for i, d := range c.durations {
				var err error
				for _, at := range c.errAt {
					if at == i {
						err = os.ErrInvalid
					}
				}
				cs.ObserveTx(d, err)
			}
			if d := cmp.Diff(c.want, cs.Writer()); d != "" {
				t.Fatal(d)
			}
		})
	}
}

// ramp returns 1ms, 2ms, ... n ms.
func ramp(n int) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = time.Duration(i+1) * time.Millisecond
	}
	return out
}

func TestLastSeenIsPerClusterFromTheClock(t *testing.T) {
	clk := clock.NewFake(testNow)
	cs := New(clk)
	cs.ObjectSeen(1)
	clk.Advance(time.Minute)
	cs.ObjectSeen(2)
	cs.HandlerError()
	cs.HandlerPanic()
	cs.HandlerPanic()
	if got, ok := cs.LastSeen(1); !ok || !got.Equal(testNow) {
		t.Errorf("cluster 1 = %v, %v", got, ok)
	}
	if got, ok := cs.LastSeen(2); !ok || !got.Equal(testNow.Add(time.Minute)) {
		t.Errorf("cluster 2 = %v, %v", got, ok)
	}
	if _, ok := cs.LastSeen(3); ok {
		t.Error("cluster 3 was never seen")
	}
	if d := cmp.Diff(Handlers{Errors: 1, Panics: 2}, cs.Handlers()); d != "" {
		t.Fatal(d)
	}
}

func TestSnapshotFileRoundTripsAndIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	want := Snapshot{
		WrittenAt: clock.Format(testNow), PID: 42, Version: "0.0.1-dev",
		Clusters: []Cluster{{ID: 1, Ready: true, LastEventAt: clock.Format(testNow), SkewSeconds: 1.5}},
		Writer:   Writer{Transactions: 10, Errors: 1, P99Ms: 2.5},
		Handlers: Handlers{Errors: 1},
		Capture:  Capture{Queued: 3, Completed: 2, Dropped: 1, Gaps: map[string]uint64{"file": 1, "no_output": 1}},
		Closer:   Closer{LastTickAt: clock.Format(testNow), Closed: 1, Attached: 2},
	}
	if err := WriteFile(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", fi.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the snapshot", len(entries))
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/status/`
Expected: compile error, `undefined: New`.

- [ ] **Step 3: Implement**

```go
// Package status keeps the counters the running process can answer for and
// the snapshot it publishes for idios status, which runs as another process.
package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"idios/internal/clock"
)

const (
	// Interval is how often the daemon writes its snapshot.
	Interval = 10 * time.Second
	// Stale is the snapshot age past which the daemon is presumed gone.
	Stale = 3 * Interval
	// FileName is the snapshot's name under the data directory.
	FileName = "status.json"

	ringSize = 1024
)

// Counters is what the process counts about itself. Every method is safe
// for concurrent use.
type Counters struct {
	clk clock.Clock

	mu            sync.Mutex
	txCount       uint64
	txErrors      uint64
	durations     []time.Duration
	next          int
	handlerErrors uint64
	handlerPanics uint64
	lastSeen      map[int64]time.Time
}

// New returns empty counters that stamp events with clk.
func New(clk clock.Clock) *Counters {
	return &Counters{clk: clk, lastSeen: map[int64]time.Time{}}
}

// ObserveTx records one transaction. It satisfies store.TxObserver.
func (c *Counters) ObserveTx(d time.Duration, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.txCount++
	if err != nil {
		c.txErrors++
	}
	// A ring of recent durations: p99 over the whole run would hide a slow
	// hour behind a fast day.
	if len(c.durations) < ringSize {
		c.durations = append(c.durations, d)
		return
	}
	c.durations[c.next] = d
	c.next = (c.next + 1) % ringSize
}

// HandlerError counts an informer handler that returned an error.
func (c *Counters) HandlerError() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlerErrors++
}

// HandlerPanic counts a panic recovered at the handler boundary.
func (c *Counters) HandlerPanic() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlerPanics++
}

// ObjectSeen stamps the cluster with the current time.
func (c *Counters) ObjectSeen(clusterID int64) {
	now := c.clk.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSeen[clusterID] = now
}

// LastSeen returns when the cluster last delivered an object.
func (c *Counters) LastSeen(clusterID int64) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.lastSeen[clusterID]
	return t, ok
}

// Writer is the transaction counters at one instant.
type Writer struct {
	Transactions uint64  `json:"transactions"`
	Errors       uint64  `json:"errors"`
	P99Ms        float64 `json:"p99_ms"`
}

// Writer reports the transaction counters and the p99 of recent durations.
func (c *Counters) Writer() Writer {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := Writer{Transactions: c.txCount, Errors: c.txErrors}
	if n := len(c.durations); n > 0 {
		sorted := make([]time.Duration, n)
		copy(sorted, c.durations)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		idx := (n*99 + 99) / 100
		if idx > 0 {
			idx--
		}
		w.P99Ms = float64(sorted[idx]) / float64(time.Millisecond)
	}
	return w
}

// Handlers is the handler boundary counters at one instant.
type Handlers struct {
	Errors uint64 `json:"errors"`
	Panics uint64 `json:"panics"`
}

// Handlers reports the handler boundary counters.
func (c *Counters) Handlers() Handlers {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Handlers{Errors: c.handlerErrors, Panics: c.handlerPanics}
}

// Cluster is one watcher's runtime state.
type Cluster struct {
	ID          int64   `json:"id"`
	Ready       bool    `json:"ready"`
	LastEventAt string  `json:"last_event_at,omitempty"`
	SkewSeconds float64 `json:"skew_seconds"`
}

// Capture is the pool's counters at one instant.
type Capture struct {
	Queued    uint64            `json:"queued"`
	Completed uint64            `json:"completed"`
	Dropped   uint64            `json:"dropped"`
	Gaps      map[string]uint64 `json:"gaps,omitempty"`
}

// Closer is the last successful closer tick.
type Closer struct {
	LastTickAt string `json:"last_tick_at,omitempty"`
	Closed     int64  `json:"closed"`
	Attached   int64  `json:"attached"`
}

// Snapshot is what the daemon publishes; everything the database already
// holds is left out of it.
type Snapshot struct {
	WrittenAt string    `json:"written_at"`
	PID       int       `json:"pid"`
	Version   string    `json:"version"`
	Clusters  []Cluster `json:"clusters,omitempty"`
	Writer    Writer    `json:"writer"`
	Handlers  Handlers  `json:"handlers"`
	Capture   Capture   `json:"capture"`
	Closer    Closer    `json:"closer"`
}

// WriteFile publishes s at path through a temp file in the same directory,
// so a reader sees the old snapshot or the new one, never a partial one.
func WriteFile(path string, s Snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".status-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// ReadFile loads the snapshot at path.
func ReadFile(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}
```

In `internal/archtest/deps_test.go` add to `onlyInternal`:

```go
	"internal/status": {module + "/internal/clock"},
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/status/ ./internal/archtest/`
Expected: PASS. Check the ring row by hand: after 1024 durations of 1..1024 ms and one more of 1 ms, slot 0 (1 ms) is overwritten by 1 ms; sorted, the p99 index is `(1024*99+99)/100 - 1 = 1013`, whose value is 1014 ms (the sorted ring is 1..1024 ms, so index k holds k+1 ms).

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/status/status.go internal/status/status_test.go internal/archtest/deps_test.go
git commit -m "status: add process counters and the snapshot file"
```

---

### Task 4: `store.Writer` reports every transaction

**Files:**
- Modify: `internal/store/writer.go`
- Modify: `internal/store/writer_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `type TxObserver interface { ObserveTx(d time.Duration, err error) }`; field `Writer.Observer TxObserver` (nil means no reporting).

Test traces to process doc 11 ("writer: transactions, errors, p99 duration").

- [ ] **Step 1: Write the failing test**

Append to `internal/store/writer_test.go`:

```go
type recordingObserver struct {
	calls []string
}

func (r *recordingObserver) ObserveTx(d time.Duration, err error) {
	outcome := "ok"
	if err != nil {
		outcome = "err"
	}
	if d < 0 {
		outcome += " negative"
	}
	r.calls = append(r.calls, outcome)
}

func TestObserverSeesEveryTransactionOutcome(t *testing.T) {
	s, _ := openMigratedStore(t)
	obs := &recordingObserver{}
	s.Writer.Observer = obs
	ctx := context.Background()
	_ = s.Writer.Tx(ctx, func(*sql.Tx) error { return nil })
	_ = s.Writer.Tx(ctx, func(*sql.Tx) error { return errors.New("boom") })
	_ = s.Writer.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO clusters (context_name) VALUES ('missing not null columns')")
		return err
	})
	if d := cmp.Diff([]string{"ok", "err", "err"}, obs.calls); d != "" {
		t.Fatal(d)
	}
}
```

Add `"time"`, `"errors"` and `"github.com/google/go-cmp/cmp"` to the imports if absent.

- [ ] **Step 2: Run the test to see it fail**

Run: `go test ./internal/store/ -run TestObserverSeesEveryTransactionOutcome`
Expected: compile error, `s.Writer.Observer undefined`.

- [ ] **Step 3: Implement**

Replace `internal/store/writer.go` with:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

// TxObserver learns the duration and outcome of every transaction.
type TxObserver interface {
	ObserveTx(d time.Duration, err error)
}

// Writer owns the single write connection; every mutation goes through it.
// Observer, when set, is told about each transaction.
type Writer struct {
	db       *sql.DB
	mu       sync.Mutex
	Observer TxObserver
}

// Tx runs fn in one transaction. Commit on nil, roll back on error. A panic
// in fn rolls back and is re-raised so the handler boundary can log it.
func (w *Writer) Tx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// time.Since, not the clock: a duration is never stored, and the
	// monotonic reading is what a percentile needs.
	start := time.Now()
	defer func() {
		if w.Observer != nil {
			w.Observer.ObserveTx(time.Since(start), err)
		}
	}()

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/store/writer.go internal/store/writer_test.go
git commit -m "store: report every transaction to an observer"
```

---

### Task 5: `Writer.Checkpoint` reports a busy checkpoint

**Files:**
- Modify: `internal/store/sweep_sql.go`
- Modify: `internal/store/sweep_sql_test.go`

**Interfaces:**
- Produces: `var ErrCheckpointBusy error`.

Test traces to storage doc 5.12 (`sweep_runs.error` records why a step did not do its work) and process doc 8 step 8 (the checkpoint truncates the WAL); a `busy` result means it did not, and the row must say so.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/sweep_sql_test.go`:

```go
func TestCheckpointReportsBusyWhileAReaderHoldsTheWAL(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out busy_timeout")
	}
	s, _ := openMigratedStore(t)
	ctx := context.Background()
	insertCluster(t, s, "c")
	rtx, err := s.Reader.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := rtx.QueryRowContext(ctx, "SELECT COUNT(*) FROM clusters").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := s.Writer.Checkpoint(ctx); !errors.Is(err, ErrCheckpointBusy) {
		t.Fatalf("Checkpoint with an open reader = %v, want ErrCheckpointBusy", err)
	}
	if err := rtx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := s.Writer.Checkpoint(ctx); err != nil {
		t.Fatalf("Checkpoint after the reader left = %v", err)
	}
}
```

Add `"errors"` to the imports if absent.

- [ ] **Step 2: Run the test to see it fail**

Run: `go test ./internal/store/ -run TestCheckpointReportsBusy`
Expected: compile error, `undefined: ErrCheckpointBusy`.

- [ ] **Step 3: Implement**

In `internal/store/sweep_sql.go` add `"errors"` to the imports and replace `Checkpoint`:

```go
// ErrCheckpointBusy is returned when a reader kept the WAL from being
// truncated; the frames are still there for the next pass.
var ErrCheckpointBusy = errors.New("wal checkpoint: busy, WAL not truncated")

// Checkpoint moves the WAL into the main file and truncates it. It runs
// outside Tx because a checkpoint inside an open transaction does nothing,
// and it reads the busy column because the pragma reports a blocked
// truncation as a row, not as an error.
func (w *Writer) Checkpoint(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var busy, logFrames, checkpointed int64
	if err := w.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return ErrCheckpointBusy
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/store/ -run 'Checkpoint'`
Expected: PASS (the busy test takes about five seconds, the connection's `busy_timeout`).

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/store/sweep_sql.go internal/store/sweep_sql_test.go
git commit -m "store: report a busy wal checkpoint as an error"
```

---

### Task 6: `incident.Closer.Last`

**Files:**
- Modify: `internal/incident/closer.go`
- Modify: `internal/incident/closer_test.go`

**Interfaces:**
- Produces: `func (c *Closer) Last() (time.Time, TickResult)` (zero time before the first successful tick).

Test traces to process doc 11 ("closer: last tick, closed count").

- [ ] **Step 1: Write the failing assertion**

In `TestTickClosesStableIncidentsThenAttachesTheirLateEvents`, after the existing assertion on the `TickResult` returned by `Tick`, add:

```go
	if at, last := c.Last(); !at.Equal(now) || last != got {
		t.Fatalf("Last() = %v, %+v; want %v, %+v", at, last, now, got)
	}
```

where `c` is the `*Closer` and `got` the `TickResult` the test already holds (rename local variables if the test uses other names, and add before the tick: `if at, _ := c.Last(); !at.IsZero() { t.Fatal("Last() set before any tick") }`).

- [ ] **Step 2: Run the test to see it fail**

Run: `go test ./internal/incident/ -run TestTickCloses`
Expected: compile error, `c.Last undefined`.

- [ ] **Step 3: Implement**

In `closer.go` add `"sync"` to the imports, the fields

```go
	mu     sync.Mutex
	lastAt time.Time
	last   TickResult
```

to `Closer`, and in `Tick` after the transaction succeeds, before `return r, nil`:

```go
	c.mu.Lock()
	c.lastAt, c.last = now, r
	c.mu.Unlock()
```

and the method:

```go
// Last reports when the most recent successful tick ran and what it
// changed; a zero time means none has run yet.
func (c *Closer) Last() (time.Time, TickResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastAt, c.last
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/incident/`
Expected: PASS.

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/incident/closer.go internal/incident/closer_test.go
git commit -m "incident: remember the closer's last tick"
```

---

### Task 7: `capture.Pool`: stats, start-on-add, `RemoveCluster`

**Files:**
- Modify: `internal/capture/pool.go`
- Modify: `internal/capture/worker.go` (`writeRow` counts completions)
- Modify: `internal/capture/pool_test.go`

**Interfaces:**
- Consumes: `processor.CaptureRequest`, `store.Artifact`.
- Produces: `type Stats struct { Queued, Completed, Dropped uint64; Gaps map[string]uint64 }`; `func (p *Pool) Stats() Stats`; `func (p *Pool) RemoveCluster(clusterID int64)`; `AddCluster` on a running pool starts workers. `Dropped()` is removed (its callers move to `Stats().Dropped`).

Tests trace to process doc 11 ("capture: queued, completed, dropped (queue full), by capture_gap") and process doc 5 "Config changes" (a watcher is cancelled and started again; its capture workers must follow), which the Phase 5 handoff names as the reload requirement.

- [ ] **Step 1: Write the failing tests**

Replace `TestFullQueueDropsAndCounts` and `TestWorkersDrainTheQueue` in `pool_test.go` with:

```go
func TestFullQueueDropsAndCounts(t *testing.T) {
	h := newHarness(t)
	h.p.cfg.QueueSize = 2
	h.p.AddCluster(1, h.src)
	for i := 0; i < 3; i++ {
		h.p.Enqueue(podJSONRequest(1, "p1"))
	}
	h.p.Enqueue(podJSONRequest(9, "p1"))
	want := Stats{Queued: 2, Dropped: 2, Gaps: map[string]uint64{}}
	if d := cmp.Diff(want, h.p.Stats()); d != "" {
		t.Fatal(d)
	}
	if got := len(h.p.clusters[1].ch); got != 2 {
		t.Fatalf("queued = %d, want 2", got)
	}
}

func waitForArtifacts(t *testing.T, h *harness, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(h.artifacts(t)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("fewer than %d artifact rows within 5s", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestClusterAddedToARunningPoolGetsWorkersAndARemovedOneDrops(t *testing.T) {
	h := newHarness(t)
	h.seedPod(t, "p1", 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.p.Run(ctx) }()

	h.p.AddCluster(1, h.src)
	h.p.Enqueue(podJSONRequest(1, "p1"))
	waitForArtifacts(t, h, 1)

	h.p.RemoveCluster(1)
	h.p.Enqueue(podJSONRequest(1, "p1"))

	want := Stats{Queued: 1, Completed: 1, Dropped: 1, Gaps: map[string]uint64{"file": 1}}
	if d := cmp.Diff(want, h.p.Stats()); d != "" {
		t.Fatal(d)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
}
```

Add `"github.com/google/go-cmp/cmp"` to the imports.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/capture/ -run 'TestFullQueue|TestClusterAdded'`
Expected: compile error, `undefined: Stats`.

- [ ] **Step 3: Implement**

Replace `internal/capture/pool.go` from `type clusterQueue` to the end with:

```go
type clusterQueue struct {
	src    LogSource
	ch     chan processor.CaptureRequest
	cancel context.CancelFunc
}

// Pool captures logs and manifests for every registered cluster and writes
// the artifacts rows. It implements processor.CaptureSink.
type Pool struct {
	cfg   Config
	w     TxRunner
	clk   clock.Clock
	log   *slog.Logger
	cache *earlyCache

	mu       sync.Mutex
	clusters map[int64]*clusterQueue
	runCtx   context.Context
	wg       sync.WaitGroup

	queued, completed, dropped atomic.Uint64
	gapsMu                     sync.Mutex
	gaps                       map[string]uint64
}

// Stats counts requests through the pool. Gaps counts completed rows by
// capture_gap, under "file" for rows that captured one.
type Stats struct {
	Queued, Completed, Dropped uint64
	Gaps                       map[string]uint64
}

// New returns a Pool with no clusters; AddCluster registers them and Run
// starts their workers.
func New(cfg Config, w TxRunner, clk clock.Clock, log *slog.Logger) *Pool {
	return &Pool{cfg: cfg, w: w, clk: clk, log: log, cache: newEarlyCache(cfg.EarlyDebounce), clusters: map[int64]*clusterQueue{}, gaps: map[string]uint64{}}
}

func ptr[T any](v T) *T { return &v }

var _ processor.CaptureSink = (*Pool)(nil)

// AddCluster registers the log source for one cluster row. While Run is
// active the cluster's workers start at once; before it, Run starts them.
// Registering an id again replaces its queue and stops the old workers.
func (p *Pool) AddCluster(clusterID int64, src LogSource) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if old := p.clusters[clusterID]; old != nil && old.cancel != nil {
		old.cancel()
	}
	q := &clusterQueue{src: src, ch: make(chan processor.CaptureRequest, p.cfg.QueueSize)}
	p.clusters[clusterID] = q
	if p.runCtx != nil {
		p.startWorkers(q)
	}
}

// RemoveCluster stops the cluster's workers. Requests still queued are
// dropped without a row, and later requests for the id count as dropped.
func (p *Pool) RemoveCluster(clusterID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if q := p.clusters[clusterID]; q != nil && q.cancel != nil {
		q.cancel()
	}
	delete(p.clusters, clusterID)
}

// startWorkers is called with p.mu held and p.runCtx set.
func (p *Pool) startWorkers(q *clusterQueue) {
	ctx, cancel := context.WithCancel(p.runCtx)
	q.cancel = cancel
	for i := 0; i < p.cfg.Workers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for {
				// select picks a ready case at random, so a worker can take
				// a queued request after cancel; the calls it needs would
				// fail, so the queue is checked second.
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case r := <-q.ch:
					p.process(ctx, q.src, r)
				}
			}
		}()
	}
}

// Enqueue hands a request to its cluster's queue without waiting. When the
// queue is full the API server is slow or down; blocking here would stall
// the informer and make the pod table stale as well, so the request is
// dropped and counted.
func (p *Pool) Enqueue(r processor.CaptureRequest) {
	p.mu.Lock()
	q := p.clusters[r.ClusterID]
	p.mu.Unlock()
	if q == nil {
		p.dropped.Add(1)
		p.log.Warn("capture request for unknown cluster", "cluster", r.ClusterID, "pod", r.PodName, "kind", r.Kind)
		return
	}
	select {
	case q.ch <- r:
		p.queued.Add(1)
	default:
		p.dropped.Add(1)
		p.log.Warn("capture queue full", "cluster", r.ClusterID, "pod", r.PodName, "container", r.Container, "kind", r.Kind, "trigger", r.Trigger)
	}
}

// Stats reports the counters at this instant.
func (p *Pool) Stats() Stats {
	p.gapsMu.Lock()
	gaps := make(map[string]uint64, len(p.gaps))
	for k, v := range p.gaps {
		gaps[k] = v
	}
	p.gapsMu.Unlock()
	return Stats{Queued: p.queued.Load(), Completed: p.completed.Load(), Dropped: p.dropped.Load(), Gaps: gaps}
}

func (p *Pool) countCompleted(a store.Artifact) {
	key := "file"
	if a.CaptureGap != nil {
		key = *a.CaptureGap
	}
	p.completed.Add(1)
	p.gapsMu.Lock()
	p.gaps[key]++
	p.gapsMu.Unlock()
}

// Run starts the workers of every registered cluster, starts those of any
// cluster added later, and blocks until ctx ends. Requests still queued at
// that point are dropped without a row: a canceled context could not make
// the calls they need.
func (p *Pool) Run(ctx context.Context) error {
	p.mu.Lock()
	p.runCtx = ctx
	for _, q := range p.clusters {
		p.startWorkers(q)
	}
	p.mu.Unlock()
	<-ctx.Done()
	// Clearing runCtx under the lock orders every AddCluster before the
	// wait, so no worker joins the group while it is being waited on.
	p.mu.Lock()
	p.runCtx = nil
	p.mu.Unlock()
	p.wg.Wait()
	return ctx.Err()
}
```

Add `"idios/internal/store"` to the imports of `pool.go`. In `worker.go` change `writeRow` to:

```go
func (p *Pool) writeRow(ctx context.Context, a store.Artifact) {
	err := p.w.Tx(ctx, func(tx *sql.Tx) error { return store.UpsertArtifact(ctx, tx, a) })
	if err != nil {
		p.log.Error("artifact row", "uid", a.PodUID, "container", a.ContainerName, "kind", a.Kind, "restart", a.RestartCount, "err", err)
		return
	}
	p.countCompleted(a)
}
```

`cmd/idios/main.go` does not call `Dropped()`; nothing else does after the test change.

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/capture/`
Expected: PASS.

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/capture/pool.go internal/capture/worker.go internal/capture/pool_test.go
git commit -m "capture: count outcomes and start workers for clusters added late"
```

---

### Task 8: Early cache reserves before it fetches

**Files:**
- Modify: `internal/capture/early.go`
- Modify: `internal/capture/worker.go` (`captureEarly`)
- Modify: `internal/capture/early_test.go`

**Interfaces:**
- Produces: `func (c *earlyCache) reserve(podUID, container, reason string, now time.Time) bool` replacing `wants`; `func (c *earlyCache) release(podUID, container string)`; `held.pending bool`.

Test traces to the bug named in the Phase 5 handoff: `wants` and `put` were not atomic across workers, so two `Unhealthy` events for one container handled concurrently could both fetch inside one debounce window.

- [ ] **Step 1: Write the failing test**

In `early_test.go` rename every `c.wants(` to `c.reserve(` and the message `wants = %v` to `reserve = %v`. Then add:

```go
func TestReserveAdmitsOneFetchAtATime(t *testing.T) {
	c := newEarlyCache(60 * time.Second)
	steps := []struct {
		name string
		do   func() bool
		want bool
	}{
		{"first request reserves", func() bool { return c.reserve("p1", "api", "Unhealthy", testNow) }, true},
		{"sibling request while in flight is refused", func() bool { return c.reserve("p1", "api", "Unhealthy", testNow) }, false},
		{"a final reason while in flight is refused too", func() bool { return c.reserve("p1", "api", "Killing", testNow) }, false},
		{"release after a failed fetch reopens", func() bool { c.release("p1", "api"); return c.reserve("p1", "api", "Unhealthy", testNow) }, true},
		{"put ends the reservation and starts the window", func() bool {
			c.put("p1", "api", "Unhealthy", []byte("one"), false, testNow)
			return c.reserve("p1", "api", "Unhealthy", testNow.Add(59*time.Second))
		}, false},
		{"at the window a new fetch reserves", func() bool { return c.reserve("p1", "api", "Unhealthy", testNow.Add(60*time.Second)) }, true},
		{"release keeps the earlier copy", func() bool { c.release("p1", "api"); h, ok := c.take("p1", "api"); return ok && string(h.body) == "one" }, true},
		{"a placeholder with no copy is not taken", func() bool { c.reserve("p1", "api", "Unhealthy", testNow); _, ok := c.take("p1", "api"); return ok }, false},
	}
	for _, st := range steps {
		if got := st.do(); got != st.want {
			t.Errorf("%s: got %v, want %v", st.name, got, st.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/capture/ -run 'TestEarlyCache|TestReserve'`
Expected: compile error, `c.reserve undefined`.

- [ ] **Step 3: Implement**

In `early.go` replace `held` and `wants` with:

```go
// held is one early copy of a container's live log and the time it was
// taken. final marks a copy taken on a death announcement; nothing later
// replaces it because nothing later exists. pending marks a fetch in
// flight, so a concurrent request for the same container waits for its
// result instead of fetching too.
type held struct {
	body      []byte
	truncated bool
	at        time.Time
	final     bool
	pending   bool
}

// reserve says whether an early request for the container should fetch now
// and, when it should, marks the fetch in flight. The caller ends it with
// put on success or release on failure.
func (c *earlyCache) reserve(podUID, container, reason string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		e = &podEntry{containers: map[string]*held{}, touched: now}
		c.pods[podUID] = e
	}
	h := e.containers[container]
	switch {
	case h == nil:
		e.containers[container] = &held{pending: true}
	case h.pending, h.final:
		return false
	case reason == reasonUnhealthy && now.Sub(h.at) < c.debounce:
		return false
	default:
		h.pending = true
	}
	return true
}

// release ends a reservation whose fetch produced nothing, leaving the
// earlier copy, if there was one, as it was.
func (c *earlyCache) release(podUID, container string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		return
	}
	h := e.containers[container]
	if h == nil {
		return
	}
	if h.body == nil {
		delete(e.containers, container)
		if len(e.containers) == 0 {
			delete(c.pods, podUID)
		}
		return
	}
	h.pending = false
}
```

In `take`, after `h := e.containers[container]`, treat a placeholder as absent:

```go
	if h == nil || h.body == nil {
		return held{}, false
	}
```

(replace the existing `if h == nil` check). `put` is unchanged: it stores a fresh `held` with `pending` false.

In `worker.go` change `captureEarly`:

```go
func (p *Pool) captureEarly(ctx context.Context, src LogSource, r processor.CaptureRequest) {
	reason := strings.TrimPrefix(r.Trigger, processor.TriggerEarlyPrefix)
	now := p.clk.Now()
	if !p.cache.reserve(r.PodUID, r.Container, reason, now) {
		return
	}
	r.Previous = false
	f := p.fetch(ctx, src, r)
	if f.gap != nil {
		p.cache.release(r.PodUID, r.Container)
		return
	}
	p.cache.put(r.PodUID, r.Container, reason, f.body, f.truncated, now)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/capture/`
Expected: PASS, including `TestEarlyRequestFillsCacheWithoutARow` (a failed early fetch leaves no entry: `release` removes the placeholder).

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/capture/early.go internal/capture/worker.go internal/capture/early_test.go
git commit -m "capture: reserve the early cache slot before fetching"
```

---

### Task 9: Handler boundary: recover panics, count, stamp; default kubeconfig resolution

**Files:**
- Modify: `internal/k8s/watcher.go` (`New` takes `*status.Counters`; `counters` field)
- Modify: `internal/k8s/handler.go` (`guard`)
- Modify: `internal/k8s/handler_test.go`
- Modify: `internal/k8s/watcher_test.go` (`New` call)
- Modify: `internal/k8s/client.go`
- Create: `internal/k8s/client_test.go`

**Interfaces:**
- Consumes: `status.New`, `(*status.Counters).HandlerError/HandlerPanic/ObjectSeen/Handlers`.
- Produces: `func New(cfg Config, client ClientFunc, h Handler, log *slog.Logger, counters *status.Counters) *Watcher`; `KubeconfigClient("", contextName, skew)` uses clientcmd's default loading rules.

Tests trace to process doc 10 ("A handler error never propagates to client-go. Log ... increment a counter; return"; "Panics in a handler are recovered at the handler boundary, logged with the stack, and counted. A panic in one informer must not take the process down") and process doc 13 (`kubeconfig` default is "default kubeconfig resolution").

- [ ] **Step 1: Write the failing tests**

In `handler_test.go` change `quietWatcher` and add a test:

```go
func quietWatcher() *Watcher {
	return &Watcher{log: slog.New(slog.NewTextHandler(io.Discard, nil)), counters: status.New(clock.NewFake(time.Time{}))}
}

func TestHandlerBoundaryCountsErrorsAndRecoversPanics(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "idios-smoke", UID: "pod-1"}}
	cases := []struct {
		name string
		fn   func(*corev1.Pod) error
		want status.Handlers
	}{
		{"success counts nothing", func(*corev1.Pod) error { return nil }, status.Handlers{}},
		{"error is counted", func(*corev1.Pod) error { return errors.New("tx failed") }, status.Handlers{Errors: 1}},
		{"panic is recovered and counted", func(*corev1.Pod) error { panic("nil map") }, status.Handlers{Panics: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := quietWatcher()
			calls := 0
			h := handlerFuncs(w, "Pod", "idios-smoke", func(p *corev1.Pod) error { calls++; return c.fn(p) }, nil)
			h.OnAdd(pod, false)
			h.OnAdd(pod, false)
			if calls != 2 {
				t.Fatalf("handler ran %d times, want 2: the informer must keep delivering", calls)
			}
			want := status.Handlers{Errors: c.want.Errors * 2, Panics: c.want.Panics * 2}
			if d := cmp.Diff(want, w.counters.Handlers()); d != "" {
				t.Fatal(d)
			}
			if _, ok := w.counters.LastSeen(0); !ok {
				t.Fatal("delivered objects were not stamped on the cluster")
			}
		})
	}
}
```

`want` in each row is the count for one delivery; the test delivers twice to show the informer keeps being served. Add `"time"`, `"idios/internal/clock"` and `"idios/internal/status"` to the imports.

In `watcher_test.go` `newRun`, change the `New(...)` call to pass `status.New(clk)` as the last argument and add the `"idios/internal/status"` import.

Create `client_test.go`:

```go
package k8s

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"idios/internal/clock"
)

const kubeconfigFor = `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: %s
    insecure-skip-tls-verify: true
contexts:
- name: orbstack
  context:
    cluster: c
    user: u
users:
- name: u
  user:
    token: t
`

func writeKubeconfig(t *testing.T, server string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte(fmt.Sprintf(kubeconfigFor, server)), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEmptyKubeconfigPathUsesTheDefaultResolution(t *testing.T) {
	fromEnv := writeKubeconfig(t, "https://env.invalid:6443")
	explicit := writeKubeconfig(t, "https://explicit.invalid:6443")
	t.Setenv("KUBECONFIG", fromEnv)
	cases := []struct {
		name string
		path string
		want string
	}{
		{"empty path follows KUBECONFIG", "", "https://env.invalid:6443"},
		{"explicit path wins over KUBECONFIG", explicit, "https://explicit.invalid:6443"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, host, err := KubeconfigClient(c.path, "orbstack", NewSkew(clock.NewFake(testNow), slog.New(slog.NewTextHandler(io.Discard, nil))))()
			if err != nil {
				t.Fatal(err)
			}
			if host != c.want {
				t.Fatalf("host = %q, want %q", host, c.want)
			}
		})
	}
}
```

Add `"fmt"` to the imports. `testNow` already exists in the package's tests (used by `newRun`); if it is named differently there, use that name.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/k8s/`
Expected: compile errors (`w.counters undefined`, wrong argument count to `New`), then `TestEmptyKubeconfigPathUsesTheDefaultResolution` fails on the empty path because an empty `ExplicitPath` loads nothing.

- [ ] **Step 3: Implement**

`watcher.go`: add `"idios/internal/status"` to the imports, the field `counters *status.Counters` to `Watcher`, and change `New`:

```go
// New returns a Watcher for cfg that counts handler outcomes in counters.
// Run starts it.
func New(cfg Config, client ClientFunc, h Handler, log *slog.Logger, counters *status.Counters) *Watcher {
	return &Watcher{cfg: cfg, client: client, h: h, log: log, counters: counters, syncTimeout: 30 * time.Second, wait: sleep}
}
```

`handler.go`: add `"runtime/debug"` to the imports and replace `handlerFuncs`:

```go
// handlerFuncs adapts upsert and del to informer callbacks. Add and update
// are the same call: the store snapshot, not the informer, decides what
// changed.
func handlerFuncs[T metav1.Object](w *Watcher, kind, ns string, upsert, del func(T) error) cache.ResourceEventHandlerFuncs {
	on := func(obj any, fn func(T) error) {
		o, ok := deleted[T](obj)
		if !ok {
			w.log.Error("unexpected object", "cluster", w.cfg.ClusterID, "namespace", ns, "kind", kind, "type", fmt.Sprintf("%T", obj))
			return
		}
		w.counters.ObjectSeen(w.cfg.ClusterID)
		w.guard(kind, ns, o, func() error { return fn(o) })
	}
	f := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { on(obj, upsert) },
		UpdateFunc: func(_, obj any) { on(obj, upsert) },
	}
	if del != nil {
		f.DeleteFunc = func(obj any) { on(obj, del) }
	}
	return f
}

// guard runs one handler call so that neither an error nor a panic reaches
// client-go: an error there would be dropped silently, and a panic would
// end the informer's goroutine and with it every other object it serves.
func (w *Watcher) guard(kind, ns string, o metav1.Object, fn func() error) {
	defer func() {
		if r := recover(); r != nil {
			w.counters.HandlerPanic()
			w.log.Error("handler panicked", "cluster", w.cfg.ClusterID, "namespace", ns, "kind", kind, "uid", string(o.GetUID()), "name", o.GetName(), "panic", r, "stack", string(debug.Stack()))
		}
	}()
	if err := fn(); err != nil {
		w.counters.HandlerError()
		w.log.Error("handler failed", "cluster", w.cfg.ClusterID, "namespace", ns, "kind", kind, "uid", string(o.GetUID()), "name", o.GetName(), "err", err)
	}
}
```

`client.go`:

```go
// KubeconfigClient loads contextName from the kubeconfig at path, or from
// the usual places (KUBECONFIG, then the home directory) when path is
// empty, and routes every response through skew.
func KubeconfigClient(path, contextName string, skew *Skew) ClientFunc {
	return func() (kubernetes.Interface, string, error) {
		var rules clientcmd.ClientConfigLoader = clientcmd.NewDefaultClientConfigLoadingRules()
		if path != "" {
			rules = &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
		}
		overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
		cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
		if err != nil {
			return nil, "", err
		}
		cfg.Wrap(skew.RoundTripper)
		client, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		return client, cfg.Host, nil
	}
}
```

`cmd/idios/main.go`: add `status.New(clock.Real{})` as the last argument of the `k8s.New` call (a temporary wiring; Task 12 rewrites the file) and import `"idios/internal/status"`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/k8s/ && go build ./...`
Expected: PASS.

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/k8s/watcher.go internal/k8s/handler.go internal/k8s/handler_test.go internal/k8s/watcher_test.go internal/k8s/client.go internal/k8s/client_test.go cmd/idios/main.go
git commit -m "k8s: recover and count handler failures; resolve kubeconfig by default"
```

---

### Task 10: Release defaults in `config`

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `func DefaultDataDir() string`; unexported `func dataDir(goos string, getenv func(string) string) string`; `Default().DataDir == DefaultDataDir()`, `Default().Kubeconfig == ""`. The constants `DefaultDataDir`, `DefaultKubeconfig`, `DefaultConfigFile` are removed.

Tests trace to process doc 13 (`data_dir` default "OS user data directory", `kubeconfig` default "default kubeconfig resolution") and roadmap cross-cutting decision 2 (the default flips before release).

- [ ] **Step 1: Write the failing tests**

In `config_test.go` change `TestDefaultsMatchDesign`'s `want` to use `DataDir: DefaultDataDir()`, `ArtifactsRoot: filepath.Join(DefaultDataDir(), "artifacts")`, `Kubeconfig: ""`, and the `DBPath` assertion to `filepath.Join(DefaultDataDir(), "idios.db")`. Add:

```go
func TestDataDirFollowsTheOS(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name string
		goos string
		env  map[string]string
		want string
	}{
		{"darwin", "darwin", map[string]string{"HOME": "/Users/h"}, "/Users/h/Library/Application Support/idios"},
		{"linux without xdg", "linux", map[string]string{"HOME": "/home/h"}, "/home/h/.local/share/idios"},
		{"linux with xdg", "linux", map[string]string{"HOME": "/home/h", "XDG_DATA_HOME": "/data"}, "/data/idios"},
		{"windows", "windows", map[string]string{"LocalAppData": `C:\Users\h\AppData\Local`}, filepath.Join(`C:\Users\h\AppData\Local`, "idios")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dataDir(c.goos, env(c.env)); got != filepath.FromSlash(c.want) {
				t.Fatalf("dataDir = %q, want %q", got, filepath.FromSlash(c.want))
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/config/`
Expected: compile error, `undefined: DefaultDataDir`.

- [ ] **Step 3: Implement**

Replace the constants block and `Default` in `config.go`:

```go
// DefaultDataDir is the per-user data directory of this OS, where the
// database, artifacts, log and config file live unless configured.
func DefaultDataDir() string {
	return dataDir(runtime.GOOS, os.Getenv)
}

func dataDir(goos string, getenv func(string) string) string {
	switch goos {
	case "darwin":
		return filepath.Join(getenv("HOME"), "Library", "Application Support", "idios")
	case "windows":
		return filepath.Join(getenv("LocalAppData"), "idios")
	default:
		if x := getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "idios")
		}
		return filepath.Join(getenv("HOME"), ".local", "share", "idios")
	}
}

// Default returns the release configuration. Kubeconfig empty means the
// usual resolution: KUBECONFIG, then the home directory.
func Default() Config {
	c := Config{
		DataDir:                    DefaultDataDir(),
		RetentionDays:              3,
		SweepInterval:              time.Hour,
		StabilizationWindow:        10 * time.Minute,
		StabilizationCheckInterval: 30 * time.Second,
		LogTailLines:               50,
		LogMaxBytes:                262144,
		CaptureWorkersPerCluster:   4,
		CaptureQueueSize:           1024,
		EarlyCaptureDebounce:       60 * time.Second,
	}
	c.deriveArtifactsRoot()
	return c
}
```

Add `"runtime"` to the imports and delete the "Development defaults" comment. In `cmd/idios/main.go` replace `config.DefaultConfigFile` with `filepath.Join(config.DefaultDataDir(), "idios.toml")` so the build stays green (Task 12 rewrites the file). Check `TestLoad` in `config_test.go`: its `overlay` case sets `DataDir` and `Kubeconfig` explicitly, so it still passes; if a case relies on the old `kube/config` default, change its expectation to `""`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/config/ && go build ./...`
Expected: PASS.

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add internal/config/config.go internal/config/config_test.go cmd/idios/main.go
git commit -m "config: default data_dir to the OS data directory"
```

---

### Task 11: Logging: rotating JSON file plus terminal text

**Files:**
- Create: `cmd/idios/log.go`
- Create: `cmd/idios/log_test.go`

**Interfaces:**
- Produces: `func newLogger(dataDir string, stderr *os.File) (*slog.Logger, io.Closer, error)`; unexported `rotatingFile` (`openRotatingFile(path string, max int64)`), `teeHandler`, `const logFileName = "idios.log"`, `const logRotateBytes = 10 << 20`.

Tests trace to process doc 11 ("JSON to a rotating file under the data directory, text to stderr when attached to a terminal") and process doc 12 (files under the data directory are 0600).

- [ ] **Step 1: Write the failing tests**

```go
package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLogFileRotatesOnceAtTheCapAndIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	f, err := openRotatingFile(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"first line\n", "second line\n", "third line\n"} {
		if _, err := f.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "third line\n" {
		t.Errorf("current = %q", got)
	}
	if got := readFile(t, path+".1"); got != "second line\n" {
		t.Errorf("previous = %q; the first generation must be gone", got)
	}
	if _, err := os.Stat(path + ".2"); err == nil {
		t.Error("a second generation was kept")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", fi.Mode().Perm())
	}
}

func TestTeeHandlerDeliversAttrsToEveryHandler(t *testing.T) {
	var a, b bytes.Buffer
	h := teeHandler{
		slog.NewJSONHandler(&a, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelWarn}),
	}
	log := slog.New(h).With("cluster", 1)
	log.Info("started")
	log.Warn("skew", "offset", "6m")
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled must be true when any handler wants the level")
	}
	if got := strings.Count(a.String(), "\n"); got != 2 {
		t.Errorf("json lines = %d, want 2", got)
	}
	if got := strings.Count(b.String(), "\n"); got != 1 {
		t.Errorf("text lines = %d, want 1 (info is below its level)", got)
	}
	for name, out := range map[string]string{"json": a.String(), "text": b.String()} {
		if !strings.Contains(out, "cluster") || !strings.Contains(out, "offset=6m") && !strings.Contains(out, `"offset":"6m"`) {
			t.Errorf("%s output lost attrs: %s", name, out)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./cmd/idios/`
Expected: compile error, `undefined: openRotatingFile`.

- [ ] **Step 3: Implement**

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	logFileName    = "idios.log"
	logRotateBytes = 10 << 20
)

// rotatingFile appends to path and, when a write would take it past max,
// renames it to path+".1" and starts over. One generation is kept: the log
// exists for a look at the last stop, the database is the record.
type rotatingFile struct {
	path string
	max  int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

func openRotatingFile(path string, max int64) (*rotatingFile, error) {
	r := &rotatingFile{path: path, max: max}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, fi.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		if err := r.f.Close(); err != nil {
			return 0, err
		}
		if err := os.Rename(r.path, r.path+".1"); err != nil {
			return 0, err
		}
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// teeHandler gives every record to each handler that wants its level.
type teeHandler []slog.Handler

func (t teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range t {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range t {
		if h.Enabled(ctx, r.Level) {
			errs = append(errs, h.Handle(ctx, r.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (t teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(teeHandler, len(t))
	for i, h := range t {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (t teeHandler) WithGroup(name string) slog.Handler {
	out := make(teeHandler, len(t))
	for i, h := range t {
		out[i] = h.WithGroup(name)
	}
	return out
}

// newLogger writes JSON to the log file under dataDir and, when stderr is
// a terminal, text to stderr as well. The returned Closer closes the file.
func newLogger(dataDir string, stderr *os.File) (*slog.Logger, io.Closer, error) {
	f, err := openRotatingFile(filepath.Join(dataDir, logFileName), logRotateBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	hs := teeHandler{slog.NewJSONHandler(f, nil)}
	if fi, err := stderr.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		hs = append(hs, slog.NewTextHandler(stderr, nil))
	}
	return slog.New(hs), f, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./cmd/idios/`
Expected: PASS. Trace the rotation by hand: "first line" (11 bytes) is written to an empty file; "second line" (12) would make 23 > 20, so the file rotates and holds 12; "third line" (11) would make 23 > 20, so it rotates again: `.1` holds the second line, the current file the third.

- [ ] **Step 5: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add cmd/idios/log.go cmd/idios/log_test.go
git commit -m "cmd: log json to a rotating file and text to a terminal"
```

---

### Task 12: Subcommands: `run`, `cluster add`, `ns add`, `version`

**Files:**
- Modify: `cmd/idios/main.go` (rewrite)
- Create: `cmd/idios/run.go`
- Create: `cmd/idios/config_cmd.go`
- Create: `cmd/idios/config_cmd_test.go`
- Modify: `Makefile` (`run` target)

**Interfaces:**
- Consumes: `config.Load`, `config.DefaultDataDir`, `store.Open`, `(*store.Store).Migrate`, `store.InsertCluster`, `store.FindClusterByName`, `store.AddWatchedNamespace`, `store.ListClusters`, `store.ListWatchedNamespaces`, `newLogger`, `status.New`, `k8s.New` (Task 9 signature), `capture.New`, `processor.New`, `sweep.New`, `incident.NewCloser`.
- Produces: `func run(args []string, stdout, stderr io.Writer) error`; `func loadConfig(configPath, dataDir, kubeconfig string) (config.Config, error)`; `func openStore(ctx context.Context, cfg config.Config) (*store.Store, error)`; `func runDaemon(ctx context.Context, cfg config.Config) error` (keeps the static cluster loop; Task 13 replaces it with the supervisor); `func runCluster(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error`; `func runNamespace(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error`; `const usage string`. `runStatus` is a stub returning an error until Task 14.

Test traces to storage doc 5.2 (`clusters` plus `watched_namespaces` is the single source of truth, populated by the user; `UNIQUE (cluster_id, name)`) and process doc 13 ("Clusters and namespaces are rows ... A CLI writes them").

- [ ] **Step 1: Write the failing test**

`cmd/idios/config_cmd_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"idios/internal/store"
)

func TestClusterAndNamespaceCommandsWriteRows(t *testing.T) {
	dir := t.TempDir()
	steps := []struct {
		args    []string
		wantOut string
		wantErr string
	}{
		{[]string{"cluster", "add", "orbstack"}, "added cluster orbstack (id 1, context orbstack)\n", ""},
		{[]string{"cluster", "add", "orbstack"}, "", `cluster "orbstack" exists (id 1)`},
		{[]string{"cluster", "add", "prod", "-context", "prod-admin"}, "added cluster prod (id 2, context prod-admin)\n", ""},
		{[]string{"cluster", "add"}, "", "usage: idios cluster add <name> [-context name]"},
		{[]string{"ns", "add", "orbstack", "idios-smoke"}, "watching idios-smoke in orbstack\n", ""},
		{[]string{"ns", "add", "orbstack", "idios-smoke"}, "idios-smoke already watched in orbstack\n", ""},
		{[]string{"ns", "add", "nowhere", "x"}, "", `no cluster named "nowhere"; add it with idios cluster add`},
		{[]string{"bogus"}, "", `unknown command "bogus"`},
	}
	for _, st := range steps {
		var out bytes.Buffer
		err := run(append([]string{"-data-dir", dir}, st.args...), &out, io.Discard)
		gotErr := ""
		if err != nil {
			gotErr = err.Error()
		}
		if gotErr != st.wantErr || out.String() != st.wantOut {
			t.Errorf("%v: out %q err %q; want out %q err %q", st.args, out.String(), gotErr, st.wantOut, st.wantErr)
		}
	}
	cfg, err := loadConfig("", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	var clusters []store.Cluster
	var namespaces []string
	err = s.Writer.Tx(ctx, func(tx *sql.Tx) (err error) {
		if clusters, err = store.ListClusters(ctx, tx); err != nil {
			return err
		}
		namespaces, err = store.ListWatchedNamespaces(ctx, tx, 1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	wantClusters := []store.Cluster{
		{ID: 1, Name: "orbstack", ContextName: "orbstack"},
		{ID: 2, Name: "prod", ContextName: "prod-admin"},
	}
	if d := cmp.Diff(wantClusters, clusters, cmpopts.IgnoreFields(store.Cluster{}, "FirstSeenAt")); d != "" {
		t.Error(d)
	}
	if d := cmp.Diff([]string{"idios-smoke"}, namespaces); d != "" {
		t.Error(d)
	}
}
```

- [ ] **Step 2: Run the test to see it fail**

Run: `go test ./cmd/idios/ -run TestClusterAndNamespaceCommandsWriteRows`
Expected: compile error, `run` has the wrong signature / `loadConfig` undefined.

- [ ] **Step 3: Implement**

Replace `cmd/idios/main.go`:

```go
// Command idios watches Kubernetes clusters for incidents and records them
// in a local SQLite database.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"idios/internal/clock"
	"idios/internal/config"
	"idios/internal/store"
)

const version = "0.0.1-dev"

const usage = `usage: idios [-config file] [-data-dir dir] [-kubeconfig file] <command>

commands:
  run                                  watch the configured clusters until SIGINT
  status                               show the data directory and the running process
  cluster add <name> [-context name]   add a cluster row; the context defaults to the name
  ns add <cluster> <namespace>         watch a namespace in a cluster
  version                              print the version
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "idios:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("idios", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	configPath := fs.String("config", "", "path to idios.toml (default <data-dir>/idios.toml)")
	dataDir := fs.String("data-dir", "", "override data_dir")
	kubeconfig := fs.String("kubeconfig", "", "override kubeconfig")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprint(stderr, usage)
		return errors.New("a command is required")
	}
	if rest[0] == "version" {
		fmt.Fprintln(stdout, "idios", version)
		return nil
	}
	cfg, err := loadConfig(*configPath, *dataDir, *kubeconfig)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch rest[0] {
	case "run":
		return runDaemon(ctx, cfg)
	case "status":
		return runStatus(ctx, cfg, stdout)
	case "cluster":
		return runCluster(ctx, cfg, rest[1:], stdout)
	case "ns":
		return runNamespace(ctx, cfg, rest[1:], stdout)
	}
	fmt.Fprint(stderr, usage)
	return fmt.Errorf("unknown command %q", rest[0])
}

// loadConfig reads the TOML file and applies the flag overrides. The file
// defaults to idios.toml inside the data directory the flags select, so
// -data-dir alone points at a self-contained directory.
func loadConfig(configPath, dataDir, kubeconfig string) (config.Config, error) {
	if configPath == "" {
		base := config.DefaultDataDir()
		if dataDir != "" {
			base = dataDir
		}
		configPath = filepath.Join(base, "idios.toml")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return config.Config{}, err
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
		cfg.ArtifactsRoot = filepath.Join(dataDir, "artifacts")
	}
	if kubeconfig != "" {
		cfg.Kubeconfig = kubeconfig
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

// openStore creates the directories, opens the database and migrates it.
func openStore(ctx context.Context, cfg config.Config) (*store.Store, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data_dir: %w", err)
	}
	if err := os.MkdirAll(cfg.ArtifactsRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create artifacts_root: %w", err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(ctx, clock.Real{}); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return st, nil
}
```

Create `cmd/idios/run.go` with the daemon body (moved from the old `run`, with the logger, counters and observer added):

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"idios/internal/capture"
	"idios/internal/clock"
	"idios/internal/config"
	"idios/internal/incident"
	"idios/internal/k8s"
	"idios/internal/processor"
	"idios/internal/status"
	"idios/internal/store"
	"idios/internal/sweep"
)

var _ k8s.Handler = (*processor.Processor)(nil)

// runDaemon watches the configured clusters until ctx ends.
func runDaemon(ctx context.Context, cfg config.Config) error {
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	logger, closeLog, err := newLogger(cfg.DataDir, os.Stderr)
	if err != nil {
		return err
	}
	defer func() { _ = closeLog.Close() }()
	slog.SetDefault(logger)
	ver, err := st.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	logger.Info("idios started",
		"version", version,
		"data_dir", cfg.DataDir,
		"db", cfg.DBPath(),
		"artifacts_root", cfg.ArtifactsRoot,
		"schema_version", ver,
		"kubeconfig", cfg.Kubeconfig,
	)

	counters := status.New(clock.Real{})
	st.Writer.Observer = counters
	pool := capture.New(capture.Config{
		Root: cfg.ArtifactsRoot, TailLines: cfg.LogTailLines, MaxBytes: cfg.LogMaxBytes,
		Workers: cfg.CaptureWorkersPerCluster, QueueSize: cfg.CaptureQueueSize,
		EarlyDebounce: cfg.EarlyCaptureDebounce, StabilizationWindow: cfg.StabilizationWindow,
	}, st.Writer, clock.Real{}, logger)
	proc := processor.New(st.Writer, clock.Real{}, pool, cfg.StabilizationWindow)
	sweeper := sweep.New(sweep.Config{
		Root: cfg.ArtifactsRoot, Retention: time.Duration(cfg.RetentionDays) * 24 * time.Hour, Interval: cfg.SweepInterval,
	}, st.Writer, clock.Real{}, logger)
	// The first pass runs before any watcher so the retention promise holds
	// from the first second of the process, not from the first tick.
	if err := sweeper.Sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("startup sweep: %w", err)
	}
	closer := incident.NewCloser(st.Writer, clock.Real{}, cfg.StabilizationWindow, cfg.StabilizationCheckInterval, logger)

	var wg sync.WaitGroup
	start := func(name string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error(name+" stopped", "err", err)
			}
		}()
	}
	sup := newSupervisor(cfg, st, proc, pool, counters, logger)
	start("watchers", sup.run)
	start("capture pool", pool.Run)
	start("sweeper", sweeper.Run)
	start("closer", closer.Run)

	<-ctx.Done()
	wg.Wait()
	if !errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	logger.Info("idios stopping")
	return nil
}
```

Because the supervisor arrives in Task 13, this task ships a placeholder `cmd/idios/supervise.go` holding only what `runDaemon` needs to compile and behave as before: a `supervisor` whose `run` loads the rows once and starts one watcher per cluster row. Write it as:

```go
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"idios/internal/capture"
	"idios/internal/clock"
	"idios/internal/config"
	"idios/internal/k8s"
	"idios/internal/processor"
	"idios/internal/status"
	"idios/internal/store"
)

type supervisor struct {
	cfg      config.Config
	st       *store.Store
	proc     *processor.Processor
	pool     *capture.Pool
	counters *status.Counters
	log      *slog.Logger
}

func newSupervisor(cfg config.Config, st *store.Store, proc *processor.Processor, pool *capture.Pool, counters *status.Counters, log *slog.Logger) *supervisor {
	return &supervisor{cfg: cfg, st: st, proc: proc, pool: pool, counters: counters, log: log}
}

func (s *supervisor) run(ctx context.Context) error {
	var clusters []store.Cluster
	namespaces := map[int64][]string{}
	err := s.st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if clusters, err = store.ListClusters(ctx, tx); err != nil {
			return err
		}
		for _, c := range clusters {
			if namespaces[c.ID], err = store.ListWatchedNamespaces(ctx, tx, c.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("load clusters: %w", err)
	}
	if len(clusters) == 0 {
		s.log.Warn("no clusters configured; nothing to watch")
	}
	var wg sync.WaitGroup
	for _, c := range clusters {
		skew := k8s.NewSkew(clock.Real{}, s.log.With("cluster", c.ID))
		w := k8s.New(k8s.Config{ClusterID: c.ID, ContextName: c.ContextName, Namespaces: namespaces[c.ID]},
			k8s.KubeconfigClient(s.cfg.Kubeconfig, c.ContextName, skew), s.proc, s.log, s.counters)
		s.pool.AddCluster(c.ID, capture.ClientLogs(w.Client))
		s.log.Info("watching", "cluster", c.ID, "context", c.ContextName, "namespaces", namespaces[c.ID])
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("watcher stopped", "cluster", c.ID, "err", err)
			}
		}()
	}
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}
```

Create `cmd/idios/config_cmd.go`:

```go
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"idios/internal/clock"
	"idios/internal/config"
	"idios/internal/store"
)

var errClusterUsage = errors.New("usage: idios cluster add <name> [-context name]")

// runCluster handles `idios cluster add <name> [-context name]`. The name
// comes before the flags so the common case reads as a sentence.
func runCluster(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) < 2 || args[0] != "add" || strings.HasPrefix(args[1], "-") {
		return errClusterUsage
	}
	name := args[1]
	fs := flag.NewFlagSet("cluster add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	contextName := fs.String("context", "", "kubeconfig context (default: the name)")
	if err := fs.Parse(args[2:]); err != nil {
		return errClusterUsage
	}
	if *contextName == "" {
		*contextName = name
	}
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	now := clock.Format(clock.Real{}.Now())
	var id int64
	err = st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		existing, err := store.FindClusterByName(ctx, tx, name)
		if err != nil {
			return err
		}
		if existing != nil {
			return fmt.Errorf("cluster %q exists (id %d)", name, existing.ID)
		}
		id, err = store.InsertCluster(ctx, tx, name, *contextName, now)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "added cluster %s (id %d, context %s)\n", name, id, *contextName)
	return nil
}

// runNamespace handles `idios ns add <cluster> <namespace>`. Adding a
// namespace that is already watched succeeds without a change, so scripts
// can run it again.
func runNamespace(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 3 || args[0] != "add" {
		return errors.New("usage: idios ns add <cluster> <namespace>")
	}
	clusterName, ns := args[1], args[2]
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	now := clock.Format(clock.Real{}.Now())
	var added bool
	err = st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		c, err := store.FindClusterByName(ctx, tx, clusterName)
		if err != nil {
			return err
		}
		if c == nil {
			return fmt.Errorf("no cluster named %q; add it with idios cluster add", clusterName)
		}
		added, err = store.AddWatchedNamespace(ctx, tx, c.ID, ns, now)
		return err
	})
	if err != nil {
		return err
	}
	if !added {
		fmt.Fprintf(stdout, "%s already watched in %s\n", ns, clusterName)
		return nil
	}
	fmt.Fprintf(stdout, "watching %s in %s\n", ns, clusterName)
	return nil
}
```

Add to `cmd/idios/status.go` (created here, filled in Task 14):

```go
package main

import (
	"context"
	"errors"
	"io"

	"idios/internal/config"
)

func runStatus(context.Context, config.Config, io.Writer) error {
	return errors.New("status: not implemented")
}
```

In the `Makefile` change the `run` target to:

```make
run: build
	./bin/idios -data-dir .storage -kubeconfig ./kube/config run
```

- [ ] **Step 4: Run the tests and build**

Run: `go test ./cmd/idios/ && go build ./...`
Expected: PASS.

- [ ] **Step 5: Run the binary once against an empty data dir**

```bash
go build -o bin/idios ./cmd/idios
D=/tmp/idios-plan/phase7-run; rm -rf "$D"; mkdir -p "$D"
./bin/idios -data-dir "$D" -kubeconfig ./kube/config run & sleep 4; kill -INT %1; wait
head -c 400 "$D/idios.log"; echo
./bin/idios version
```

Expected: `idios.log` holds JSON lines starting with `idios started` and a `no clusters configured` warning, ends with `idios stopping`; `idios version` prints `idios 0.0.1-dev`. The process exits 0 on SIGINT.

- [ ] **Step 6: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add cmd/idios/main.go cmd/idios/run.go cmd/idios/supervise.go cmd/idios/config_cmd.go cmd/idios/config_cmd_test.go cmd/idios/status.go Makefile
git commit -m "cmd: split idios into run, status, cluster add and ns add"
```

---

### Task 13: Config reload: the supervisor

**Files:**
- Modify: `cmd/idios/supervise.go` (replace the placeholder)
- Create: `cmd/idios/supervise_test.go`

**Interfaces:**
- Consumes: `capture.Pool.AddCluster/RemoveCluster`, `k8s.New`, `k8s.Watcher.Ready`, `k8s.Skew.Offset`.
- Produces: `const reloadInterval = 10 * time.Second`; `type clusterSpec struct { contextName string; namespaces []string }`; `func specsFromRows(clusters []store.Cluster, namespaces map[int64][]string) map[int64]clusterSpec`; `func reloadPlan(running, want map[int64]clusterSpec) (start, stop []int64)`; `type runningCluster struct { id int64; spec clusterSpec; w *k8s.Watcher; skew *k8s.Skew; cancel context.CancelFunc; done chan struct{} }`; `(*supervisor).run(ctx) error`, `(*supervisor).reload(ctx) error`, `(*supervisor).list() []*runningCluster`.

Test traces to process doc 5 "Config changes" ("when `clusters` or `watched_namespaces` rows change, the affected watcher's context is cancelled and it is started again from step 1 ... Nothing else restarts").

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"idios/internal/store"
)

func TestReloadPlanRestartsOnlyChangedClusters(t *testing.T) {
	running := map[int64]clusterSpec{
		1: {contextName: "orbstack", namespaces: []string{"a", "b"}},
		2: {contextName: "prod", namespaces: []string{"payments"}},
		3: {contextName: "old", namespaces: []string{"x"}},
	}
	cases := []struct {
		name       string
		clusters   []store.Cluster
		namespaces map[int64][]string
		wantStart  []int64
		wantStop   []int64
	}{
		{"nothing changed, namespaces in another order", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod"}, {ID: 3, ContextName: "old"}},
			map[int64][]string{1: {"b", "a"}, 2: {"payments"}, 3: {"x"}}, nil, nil},
		{"namespace added restarts that cluster", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod"}, {ID: 3, ContextName: "old"}},
			map[int64][]string{1: {"a", "b", "c"}, 2: {"payments"}, 3: {"x"}}, []int64{1}, []int64{1}},
		{"context changed restarts that cluster", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod-admin"}, {ID: 3, ContextName: "old"}},
			map[int64][]string{1: {"a", "b"}, 2: {"payments"}, 3: {"x"}}, []int64{2}, []int64{2}},
		{"new cluster starts, removed cluster stops", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod"}, {ID: 4, ContextName: "new"}},
			map[int64][]string{1: {"a", "b"}, 2: {"payments"}, 4: {"y"}}, []int64{4}, []int64{3}},
		{"cluster with no namespaces still runs", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod"}, {ID: 3, ContextName: "old"}, {ID: 5, ContextName: "bare"}},
			map[int64][]string{1: {"a", "b"}, 2: {"payments"}, 3: {"x"}}, []int64{5}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, stop := reloadPlan(running, specsFromRows(c.clusters, c.namespaces))
			if d := cmp.Diff(c.wantStart, start); d != "" {
				t.Error("start:", d)
			}
			if d := cmp.Diff(c.wantStop, stop); d != "" {
				t.Error("stop:", d)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to see it fail**

Run: `go test ./cmd/idios/ -run TestReloadPlan`
Expected: compile error, `undefined: clusterSpec`.

- [ ] **Step 3: Implement**

Replace `cmd/idios/supervise.go`:

```go
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"idios/internal/capture"
	"idios/internal/clock"
	"idios/internal/config"
	"idios/internal/k8s"
	"idios/internal/processor"
	"idios/internal/status"
	"idios/internal/store"
)

// reloadInterval is how often the clusters and watched_namespaces tables
// are read again. The CLI writes them from another process, so polling is
// the only signal.
const reloadInterval = 10 * time.Second

// clusterSpec is what a watcher was built from; a row change that leaves it
// equal needs no restart.
type clusterSpec struct {
	contextName string
	namespaces  []string
}

func (a clusterSpec) equal(b clusterSpec) bool {
	return a.contextName == b.contextName && slices.Equal(a.namespaces, b.namespaces)
}

func specsFromRows(clusters []store.Cluster, namespaces map[int64][]string) map[int64]clusterSpec {
	out := map[int64]clusterSpec{}
	for _, c := range clusters {
		ns := slices.Clone(namespaces[c.ID])
		slices.Sort(ns)
		out[c.ID] = clusterSpec{contextName: c.ContextName, namespaces: ns}
	}
	return out
}

// reloadPlan says which watchers to stop and which to start so that running
// matches want. A changed cluster is in both lists: stopped, then started
// again from its new rows.
func reloadPlan(running, want map[int64]clusterSpec) (start, stop []int64) {
	for id, r := range running {
		if w, ok := want[id]; !ok || !w.equal(r) {
			stop = append(stop, id)
		}
	}
	for id, w := range want {
		if r, ok := running[id]; !ok || !r.equal(w) {
			start = append(start, id)
		}
	}
	slices.Sort(start)
	slices.Sort(stop)
	return start, stop
}

type runningCluster struct {
	id     int64
	spec   clusterSpec
	w      *k8s.Watcher
	skew   *k8s.Skew
	cancel context.CancelFunc
	done   chan struct{}
}

// supervisor keeps one watcher per cluster row and follows the tables.
type supervisor struct {
	cfg      config.Config
	st       *store.Store
	proc     *processor.Processor
	pool     *capture.Pool
	counters *status.Counters
	log      *slog.Logger

	mu      sync.Mutex
	running map[int64]*runningCluster
}

func newSupervisor(cfg config.Config, st *store.Store, proc *processor.Processor, pool *capture.Pool, counters *status.Counters, log *slog.Logger) *supervisor {
	return &supervisor{cfg: cfg, st: st, proc: proc, pool: pool, counters: counters, log: log, running: map[int64]*runningCluster{}}
}

func (s *supervisor) loadSpecs(ctx context.Context) (map[int64]clusterSpec, error) {
	var clusters []store.Cluster
	namespaces := map[int64][]string{}
	err := s.st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if clusters, err = store.ListClusters(ctx, tx); err != nil {
			return err
		}
		for _, c := range clusters {
			if namespaces[c.ID], err = store.ListWatchedNamespaces(ctx, tx, c.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return specsFromRows(clusters, namespaces), nil
}

// reload reads the tables and restarts exactly the watchers whose rows
// changed. Stops happen before starts so a changed cluster's old workers
// are gone before its new queue is registered.
func (s *supervisor) reload(ctx context.Context) error {
	want, err := s.loadSpecs(ctx)
	if err != nil {
		return fmt.Errorf("load clusters: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := map[int64]clusterSpec{}
	for id, rc := range s.running {
		current[id] = rc.spec
	}
	start, stop := reloadPlan(current, want)
	for _, id := range stop {
		s.stopLocked(id)
	}
	for _, id := range start {
		s.running[id] = s.start(ctx, id, want[id])
	}
	return nil
}

func (s *supervisor) stopLocked(id int64) {
	rc := s.running[id]
	rc.cancel()
	<-rc.done
	s.pool.RemoveCluster(id)
	delete(s.running, id)
	s.log.Info("watcher stopped", "cluster", id)
}

func (s *supervisor) start(ctx context.Context, id int64, spec clusterSpec) *runningCluster {
	skew := k8s.NewSkew(clock.Real{}, s.log.With("cluster", id))
	w := k8s.New(k8s.Config{ClusterID: id, ContextName: spec.contextName, Namespaces: spec.namespaces},
		k8s.KubeconfigClient(s.cfg.Kubeconfig, spec.contextName, skew), s.proc, s.log, s.counters)
	s.pool.AddCluster(id, capture.ClientLogs(w.Client))
	cctx, cancel := context.WithCancel(ctx)
	rc := &runningCluster{id: id, spec: spec, w: w, skew: skew, cancel: cancel, done: make(chan struct{})}
	s.log.Info("watching", "cluster", id, "context", spec.contextName, "namespaces", spec.namespaces)
	go func() {
		defer close(rc.done)
		if err := w.Run(cctx); err != nil && !errors.Is(err, context.Canceled) {
			s.log.Error("watcher stopped", "cluster", id, "err", err)
		}
	}()
	return rc
}

// run loads the tables now and every reloadInterval until ctx ends, then
// stops every watcher. The first load must succeed; later failures are
// logged and the running set is kept.
func (s *supervisor) run(ctx context.Context) error {
	if err := s.reload(ctx); err != nil {
		return err
	}
	if len(s.list()) == 0 {
		s.log.Warn("no clusters configured; nothing to watch")
	}
	t := time.NewTicker(reloadInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for id := range s.running {
				s.stopLocked(id)
			}
			s.mu.Unlock()
			return ctx.Err()
		case <-t.C:
			if err := s.reload(ctx); err != nil {
				s.log.Error("reload clusters", "err", err)
			}
		}
	}
}

// list returns the running clusters in id order.
func (s *supervisor) list() []*runningCluster {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*runningCluster, 0, len(s.running))
	for _, rc := range s.running {
		out = append(out, rc)
	}
	slices.SortFunc(out, func(a, b *runningCluster) int { return int(a.id - b.id) })
	return out
}
```

- [ ] **Step 4: Run the tests and build**

Run: `go test ./cmd/idios/ && go build ./...`
Expected: PASS.

- [ ] **Step 5: Verify the reload by hand against OrbStack (orchestrator; skip only if the API is unreachable after `orb start`)**

```bash
go build -o bin/idios ./cmd/idios
D=/tmp/idios-plan/phase7-reload; rm -rf "$D"; mkdir -p "$D"
./bin/idios -data-dir "$D" -kubeconfig ./kube/config run & sleep 3
./bin/idios -data-dir "$D" cluster add orbstack
./bin/idios -data-dir "$D" ns add orbstack idios-smoke
sleep 15
kill -INT %1; wait
grep -E '"msg":"(watching|no clusters|watcher stopped)"' "$D/idios.log"
sqlite3 -header "$D/idios.db" "SELECT id, identity IS NOT NULL AS has_identity, api_server_url, last_error FROM clusters"
```

Expected: the log shows `no clusters configured`, then `watching` for cluster 1 within about ten seconds of the `ns add`, then `watcher stopped` on SIGINT; the cluster row has an identity and `api_server_url` `https://127.0.0.1:26443` with `last_error` NULL. Nothing in `idios-smoke` is changed by this step.

- [ ] **Step 6: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add cmd/idios/supervise.go cmd/idios/supervise_test.go
git commit -m "cmd: restart watchers when their cluster rows change"
```

---

### Task 14: Status snapshot loop and `idios status`

**Files:**
- Modify: `cmd/idios/status.go` (replace the stub)
- Create: `cmd/idios/status_test.go`
- Modify: `cmd/idios/run.go` (start the status loop)

**Interfaces:**
- Consumes: `status.*`, `store.ListClusters`, `store.OpenIncidentsByCategory`, `store.ClosedIncidentsByReason`, `store.ArtifactsByOutcome`, `store.CountRows`, `store.LatestSweepRuns`, `(*capture.Pool).Stats`, `(*incident.Closer).Last`, `(*supervisor).list`.
- Produces: `func writeSnapshot(path string, sup *supervisor, pool *capture.Pool, closer *incident.Closer, counters *status.Counters, now time.Time) error`; `func statusLoop(ctx, cfg, sup, pool, closer, counters, log)`; `type report struct`; `func collectReport(ctx context.Context, cfg config.Config, now time.Time) (report, error)`; `func renderReport(w io.Writer, r report)`; `func runStatus(ctx context.Context, cfg config.Config, stdout io.Writer) error`; `func bytesString(n int64) string`.

Tests trace to process doc 5 step 6 ("`idios status` reports per-namespace sync state", served per cluster here), process doc 9 ("`idios status` reports [skew] for that cluster"), process doc 11 (the counter list, DB file size, WAL size, artifacts directory size) and storage doc 5.1 (`last_error` makes a broken cluster visible).

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"testing"
	"time"

	"idios/internal/status"
	"idios/internal/store"
)

func TestRenderReport(t *testing.T) {
	ts := "2026-08-27T12:00:00.000000Z"
	errAt := "2026-08-27T11:59:00.000000Z"
	full := report{
		DataDir: "/data", DBBytes: 12288, WALBytes: 0, ArtifactFiles: 2, ArtifactBytes: 1536,
		Snap: &status.Snapshot{
			PID: 42, Version: "0.0.1-dev",
			Clusters: []status.Cluster{{ID: 1, Ready: true, LastEventAt: ts, SkewSeconds: 0}, {ID: 2}},
			Writer:   status.Writer{Transactions: 1234, Errors: 0, P99Ms: 1.2},
			Handlers: status.Handlers{Errors: 0, Panics: 0},
			Capture:  status.Capture{Queued: 20, Completed: 20, Dropped: 0, Gaps: map[string]uint64{"file": 17, "no_output": 3}},
			Closer:   status.Closer{LastTickAt: ts, Closed: 1, Attached: 0},
		},
		SnapAge: 3 * time.Second,
		Clusters: []store.Cluster{
			{ID: 1, Name: "orbstack", ContextName: "orbstack"},
			{ID: 2, Name: "prod", ContextName: "prod-admin", LastError: ptr("connect: dial tcp: connection refused"), LastErrorAt: &errAt},
		},
		OpenByCategory: map[string]int64{"image_pull": 1, "crash": 2},
		ClosedByReason: map[string]int64{"recovered": 3},
		Counts:         store.RowCounts{Pods: 12, LivePods: 10, Transitions: 45, Events: 120},
		Gaps:           map[string]int64{"file": 17, "no_output": 3},
		Sweeps: []store.SweepRun{
			{TableName: "pods", RanAt: ts, RowsRemoved: 2, FilesRemoved: 3, BytesRemoved: 4096},
			{TableName: "wal_checkpoint", RanAt: ts, Error: ptr("wal checkpoint: busy, WAL not truncated")},
		},
	}
	wantFull := `data_dir   /data
database   12.0 KiB, wal 0 B
artifacts  2 files, 1.5 KiB
daemon     running, pid 42, snapshot 3s old

clusters
  id  name      context     ready  last_event                   skew  last_error
  1   orbstack  orbstack    yes    2026-08-27T12:00:00.000000Z  0s    -
  2   prod      prod-admin  no     -                            0s    connect: dial tcp: connection refused (2026-08-27T11:59:00.000000Z)

incidents
  open    crash 2, image_pull 1
  closed  recovered 3
rows
  pods 12 (10 live), transitions 45, events 120
  artifacts: file 17, no_output 3

writer    tx 1234, errors 0, p99 1.2ms
handlers  errors 0, panics 0
capture   queued 20, completed 20, dropped 0
closer    last tick 2026-08-27T12:00:00.000000Z, closed 1, attached 0
sweep
  table           ran_at                       rows  files  bytes  error
  pods            2026-08-27T12:00:00.000000Z  2     3      4096   -
  wal_checkpoint  2026-08-27T12:00:00.000000Z  0     0      0      wal checkpoint: busy, WAL not truncated
`
	empty := report{DataDir: "/data"}
	wantEmpty := `data_dir   /data
database   0 B, wal 0 B
artifacts  0 files, 0 B
daemon     not running (no status file)

clusters
  none

incidents
  open    none
  closed  none
rows
  pods 0 (0 live), transitions 0, events 0
  artifacts: none
`
	stale := full
	stale.SnapAge = 5 * time.Minute
	cases := []struct {
		name string
		in   report
		want string
	}{
		{"full", full, wantFull},
		{"empty data dir", empty, wantEmpty},
		{"stale snapshot", stale, replaceLine(wantFull, "daemon     running, pid 42, snapshot 3s old", "daemon     stale, pid 42, snapshot 5m0s old")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			renderReport(&out, c.in)
			if out.String() != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", out.String(), c.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func replaceLine(s, old, new string) string {
	return bytes.NewBuffer(bytes.Replace([]byte(s), []byte(old), []byte(new), 1)).String()
}
```

- [ ] **Step 2: Run the test to see it fail**

Run: `go test ./cmd/idios/ -run TestRenderReport`
Expected: compile error, `undefined: report`.

- [ ] **Step 3: Implement**

Replace `cmd/idios/status.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"idios/internal/capture"
	"idios/internal/clock"
	"idios/internal/config"
	"idios/internal/incident"
	"idios/internal/status"
	"idios/internal/store"
)

// writeSnapshot assembles what only the running process knows and
// publishes it for idios status.
func writeSnapshot(path string, sup *supervisor, pool *capture.Pool, closer *incident.Closer, counters *status.Counters, now time.Time) error {
	snap := status.Snapshot{WrittenAt: clock.Format(now), PID: os.Getpid(), Version: version,
		Writer: counters.Writer(), Handlers: counters.Handlers()}
	for _, rc := range sup.list() {
		c := status.Cluster{ID: rc.id, Ready: rc.w.Ready(), SkewSeconds: rc.skew.Offset().Seconds()}
		if t, ok := counters.LastSeen(rc.id); ok {
			c.LastEventAt = clock.Format(t)
		}
		snap.Clusters = append(snap.Clusters, c)
	}
	ps := pool.Stats()
	snap.Capture = status.Capture{Queued: ps.Queued, Completed: ps.Completed, Dropped: ps.Dropped, Gaps: ps.Gaps}
	if at, r := closer.Last(); !at.IsZero() {
		snap.Closer = status.Closer{LastTickAt: clock.Format(at), Closed: r.Closed, Attached: r.Attached}
	}
	return status.WriteFile(path, snap)
}

// statusLoop writes the snapshot now and every status.Interval, and removes
// it when the process stops so a stale file is not mistaken for a live one.
func statusLoop(ctx context.Context, cfg config.Config, sup *supervisor, pool *capture.Pool, closer *incident.Closer, counters *status.Counters, log *slog.Logger) error {
	path := filepath.Join(cfg.DataDir, status.FileName)
	t := time.NewTicker(status.Interval)
	defer t.Stop()
	for {
		if err := writeSnapshot(path, sup, pool, closer, counters, clock.Real{}.Now()); err != nil {
			log.Error("write status", "err", err)
		}
		select {
		case <-ctx.Done():
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Error("remove status", "err", err)
			}
			return ctx.Err()
		case <-t.C:
		}
	}
}

// report is everything idios status prints, gathered from the data
// directory, the snapshot and the database.
type report struct {
	DataDir        string
	DBBytes        int64
	WALBytes       int64
	ArtifactFiles  int64
	ArtifactBytes  int64
	Snap           *status.Snapshot
	SnapAge        time.Duration
	Clusters       []store.Cluster
	OpenByCategory map[string]int64
	ClosedByReason map[string]int64
	Counts         store.RowCounts
	Gaps           map[string]int64
	Sweeps         []store.SweepRun
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

func dirSize(root string) (files, bytes int64) {
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			files++
			bytes += fi.Size()
		}
		return nil
	})
	return files, bytes
}

func collectReport(ctx context.Context, cfg config.Config, now time.Time) (report, error) {
	r := report{DataDir: cfg.DataDir}
	r.DBBytes = fileSize(cfg.DBPath())
	r.WALBytes = fileSize(cfg.DBPath() + "-wal")
	r.ArtifactFiles, r.ArtifactBytes = dirSize(cfg.ArtifactsRoot)
	snap, err := status.ReadFile(filepath.Join(cfg.DataDir, status.FileName))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return report{}, fmt.Errorf("read status file: %w", err)
	default:
		r.Snap = &snap
		if t, err := clock.Parse(snap.WrittenAt); err == nil {
			r.SnapAge = now.Sub(t)
		}
	}
	if _, err := os.Stat(cfg.DBPath()); errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return report{}, err
	}
	defer func() { _ = st.Close() }()
	db := st.Reader.DB()
	if r.Clusters, err = store.ListClusters(ctx, db); err != nil {
		return report{}, err
	}
	if r.OpenByCategory, err = store.OpenIncidentsByCategory(ctx, db); err != nil {
		return report{}, err
	}
	if r.ClosedByReason, err = store.ClosedIncidentsByReason(ctx, db); err != nil {
		return report{}, err
	}
	if r.Counts, err = store.CountRows(ctx, db); err != nil {
		return report{}, err
	}
	if r.Gaps, err = store.ArtifactsByOutcome(ctx, db); err != nil {
		return report{}, err
	}
	if r.Sweeps, err = store.LatestSweepRuns(ctx, db); err != nil {
		return report{}, err
	}
	return r, nil
}

func bytesString(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

func countList[V int64 | uint64](m map[string]V) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(parts, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func renderReport(w io.Writer, r report) {
	fmt.Fprintf(w, "data_dir   %s\n", r.DataDir)
	fmt.Fprintf(w, "database   %s, wal %s\n", bytesString(r.DBBytes), bytesString(r.WALBytes))
	fmt.Fprintf(w, "artifacts  %d files, %s\n", r.ArtifactFiles, bytesString(r.ArtifactBytes))
	switch {
	case r.Snap == nil:
		fmt.Fprintln(w, "daemon     not running (no status file)")
	case r.SnapAge > status.Stale:
		fmt.Fprintf(w, "daemon     stale, pid %d, snapshot %s old\n", r.Snap.PID, r.SnapAge)
	default:
		fmt.Fprintf(w, "daemon     running, pid %d, snapshot %s old\n", r.Snap.PID, r.SnapAge)
	}

	fmt.Fprintln(w, "\nclusters")
	if len(r.Clusters) == 0 {
		fmt.Fprintln(w, "  none")
	} else {
		live := map[int64]status.Cluster{}
		if r.Snap != nil {
			for _, c := range r.Snap.Clusters {
				live[c.ID] = c
			}
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  id\tname\tcontext\tready\tlast_event\tskew\tlast_error")
		for _, c := range r.Clusters {
			ready, lastEvent, skew := "-", "-", "-"
			if s, ok := live[c.ID]; ok {
				ready = "no"
				if s.Ready {
					ready = "yes"
				}
				lastEvent = orDash(s.LastEventAt)
				skew = (time.Duration(s.SkewSeconds * float64(time.Second))).Round(time.Second).String()
			}
			lastErr := "-"
			if c.LastError != nil {
				lastErr = *c.LastError
				if c.LastErrorAt != nil {
					lastErr += " (" + *c.LastErrorAt + ")"
				}
			}
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%s\t%s\n", c.ID, c.Name, c.ContextName, ready, lastEvent, skew, lastErr)
		}
		_ = tw.Flush()
	}

	fmt.Fprintln(w, "\nincidents")
	fmt.Fprintf(w, "  open    %s\n", countList(r.OpenByCategory))
	fmt.Fprintf(w, "  closed  %s\n", countList(r.ClosedByReason))
	fmt.Fprintln(w, "rows")
	fmt.Fprintf(w, "  pods %d (%d live), transitions %d, events %d\n", r.Counts.Pods, r.Counts.LivePods, r.Counts.Transitions, r.Counts.Events)
	fmt.Fprintf(w, "  artifacts: %s\n", countList(r.Gaps))

	if r.Snap == nil {
		return
	}
	s := r.Snap
	fmt.Fprintf(w, "\nwriter    tx %d, errors %d, p99 %.1fms\n", s.Writer.Transactions, s.Writer.Errors, s.Writer.P99Ms)
	fmt.Fprintf(w, "handlers  errors %d, panics %d\n", s.Handlers.Errors, s.Handlers.Panics)
	fmt.Fprintf(w, "capture   queued %d, completed %d, dropped %d\n", s.Capture.Queued, s.Capture.Completed, s.Capture.Dropped)
	fmt.Fprintf(w, "closer    last tick %s, closed %d, attached %d\n", orDash(s.Closer.LastTickAt), s.Closer.Closed, s.Closer.Attached)
	fmt.Fprintln(w, "sweep")
	if len(r.Sweeps) == 0 {
		fmt.Fprintln(w, "  none")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  table\tran_at\trows\tfiles\tbytes\terror")
	for _, sr := range r.Sweeps {
		e := "-"
		if sr.Error != nil {
			e = *sr.Error
		}
		fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t%d\t%s\n", sr.TableName, sr.RanAt, sr.RowsRemoved, sr.FilesRemoved, sr.BytesRemoved, e)
	}
	_ = tw.Flush()
}

// runStatus prints the report for the configured data directory.
func runStatus(ctx context.Context, cfg config.Config, stdout io.Writer) error {
	r, err := collectReport(ctx, cfg, clock.Real{}.Now())
	if err != nil {
		return err
	}
	renderReport(stdout, r)
	return nil
}
```

In `run.go`, after `start("closer", closer.Run)` add:

```go
	start("status", func(ctx context.Context) error {
		return statusLoop(ctx, cfg, sup, pool, closer, counters, logger)
	})
```

The sweep table in the wanted output shows the `wal_checkpoint` row with a zero `rows`; the tabwriter aligns on the longest cell of each column, which is why the golden text has the widths it has. If the first run of the test shows only spacing differences, fix the golden text to the tabwriter's output after confirming by eye that the columns are aligned; anything other than spacing is a bug in the renderer.

- [ ] **Step 4: Run the tests**

Run: `go test ./cmd/idios/`
Expected: PASS.

- [ ] **Step 5: Run `idios status` against the reload data dir from Task 13**

```bash
go build -o bin/idios ./cmd/idios
D=/tmp/idios-plan/phase7-reload
./bin/idios -data-dir "$D" status
./bin/idios -data-dir "$D" -kubeconfig ./kube/config run & sleep 12; ./bin/idios -data-dir "$D" status; kill -INT %1; wait
ls "$D"
```

Expected: the first call reports `daemon     not running (no status file)` with the cluster row and `last_error` `-`; the second, while the daemon runs, reports `running`, cluster 1 `ready yes` with a `last_event` timestamp and `skew 0s`, the writer and capture lines, and the eleven sweep rows; after SIGINT `status.json` is gone from the directory.

- [ ] **Step 6: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add cmd/idios/status.go cmd/idios/status_test.go cmd/idios/run.go
git commit -m "cmd: publish a status snapshot and print it with idios status"
```

---

### Task 15: `make smoke`

**Files:**
- Create: `hack/smoke/crash-loop.yaml`, `hack/smoke/bad-image.yaml`, `hack/smoke/missing-configmap.yaml`, `hack/smoke/oom.yaml`, `hack/smoke/run.sh` (mode 0755)
- Modify: `Makefile` (`smoke` target, `.PHONY`)

**Interfaces:**
- Consumes: `idios cluster add`, `idios ns add`, `idios run`, `idios status`.
- Produces: `make smoke`.

No unit test: the smoke run is the verification (process doc 14 closing paragraph: "A `make smoke` target starts the process against [the cluster] with a namespace of deliberately broken pods (crash loop, bad image, missing configmap, OOM at 10Mi) and prints the `incidents` table after two minutes"). The orchestrator runs it in Step 3.

- [ ] **Step 1: Write the fixtures and the script**

`hack/smoke/crash-loop.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: smoke-crash
  labels:
    idios-smoke: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: smoke-crash
  template:
    metadata:
      labels:
        app: smoke-crash
    spec:
      containers:
      - name: app
        image: busybox:1.36
        command: ["sh", "-c", "echo hello from attempt; echo line two; exit 1"]
```

`hack/smoke/bad-image.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: smoke-bad-image
  labels:
    idios-smoke: "true"
spec:
  restartPolicy: Always
  containers:
  - name: app
    image: busybox:this-tag-does-not-exist
    command: ["sleep", "3600"]
```

`hack/smoke/missing-configmap.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: smoke-missing-config
  labels:
    idios-smoke: "true"
spec:
  restartPolicy: Always
  containers:
  - name: app
    image: busybox:1.36
    command: ["sleep", "3600"]
    envFrom:
    - configMapRef:
        name: smoke-config-that-does-not-exist
```

`hack/smoke/oom.yaml` (the shell doubles a string until the 10Mi limit kills it; no image beyond busybox is needed):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: smoke-oom
  labels:
    idios-smoke: "true"
spec:
  restartPolicy: Always
  containers:
  - name: app
    image: busybox:1.36
    command: ["sh", "-c", "a=x; while :; do a=\"$a$a\"; done"]
    resources:
      limits:
        memory: 10Mi
      requests:
        memory: 10Mi
```

`hack/smoke/run.sh`:

```sh
#!/bin/sh
# Runs idios for two minutes against deliberately broken pods in the
# idios-smoke namespace of the OrbStack cluster and prints what it recorded.
# Only kube/config and the idios-smoke namespace are touched.
set -eu

export KUBECONFIG=./kube/config
NS=idios-smoke
DIR=${SMOKE_DIR:-.storage/smoke}
DURATION=${SMOKE_SECONDS:-120}
IDIOS="./bin/idios -data-dir $DIR -kubeconfig $KUBECONFIG"

kubectl get ns "$NS" >/dev/null
rm -rf "$DIR"
mkdir -p "$DIR"

$IDIOS cluster add orbstack --context orbstack
$IDIOS ns add orbstack "$NS"

cleanup() {
    if [ -n "${pid:-}" ]; then
        kill -INT "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi
    kubectl -n "$NS" delete -f hack/smoke/ --ignore-not-found --wait=false
}
trap cleanup EXIT INT TERM

$IDIOS run &
pid=$!
sleep 5
kubectl -n "$NS" apply -f hack/smoke/
echo "watching $NS for ${DURATION}s"
sleep "$DURATION"

echo
$IDIOS status
echo
echo "incidents"
sqlite3 -header -column "$DIR/idios.db" "
SELECT id, workload_kind AS wkind, workload_name AS workload, container_name AS container, category,
       first_reason, last_reason, occurrences AS n, opened_at, close_reason
FROM incidents ORDER BY id"
echo
echo "artifacts"
sqlite3 -header -column "$DIR/idios.db" "
SELECT p.name AS pod, a.container_name AS container, a.kind, a.restart_count AS idx,
       a.file_path IS NOT NULL AS has_file, a.size_bytes AS bytes, a.captured_early AS early, a.capture_gap
FROM artifacts a JOIN pods p ON p.uid = a.pod_uid ORDER BY a.id"
```

`chmod 0755 hack/smoke/run.sh`.

The daemon is started before the fixtures are applied so the first-sight rule is exercised the same way it is in real use (a pod appears healthy, then breaks). `cluster add` runs with the daemon not yet started; the daemon's first reload picks the rows up.

In the `Makefile` add `smoke` to `.PHONY` and the target:

```make
smoke: build
	hack/smoke/run.sh
```

- [ ] **Step 2: Check the script parses and the fixtures are valid**

Run: `sh -n hack/smoke/run.sh && KUBECONFIG=./kube/config kubectl -n idios-smoke apply --dry-run=client -f hack/smoke/ && make ascii`
Expected: four `... (dry run)` lines, no other output.

- [ ] **Step 3: Run the smoke target (orchestrator; if the API refuses connections run `orb start` first, and if it is still unreachable say so in the report and skip this step)**

Run: `make smoke 2>&1 | tee /tmp/idios-plan/smoke.log`

Expected in the `incidents` table after two minutes:
- `smoke-crash` (workload `Deployment smoke-crash`, container `app`): a `crash` incident with `first_reason` `Error` or `CrashLoopBackOff`, `occurrences` > 1, open.
- `smoke-bad-image`: an `image_pull` incident (`ErrImagePull` then `ImagePullBackOff`), open.
- `smoke-missing-config`: a `config` incident (`CreateContainerConfigError`), open.
- `smoke-oom`: an `oom` incident (`OOMKilled`), open; possibly a `crash` incident as well once `CrashLoopBackOff` is reported, which is the reason-keyed design working as written.
- The `artifacts` table: `log_previous` rows for `smoke-crash` with `has_file 1` and a few tens of bytes; `pod_json` rows per incident; `log_current` rows with `capture_gap` `unknown` or `no_output` for waiting containers.
- `idios status` shows the daemon running, cluster 1 ready, counters above zero, no handler errors or panics, and eleven sweep rows.

Note in the report, for the roadmap handoff, the two Phase 2 readings the smoke run was to confirm against the real kubelet: (1) whether a `running -> running` restart with `OOMKilled` only in `lastState` opened no incident until `CrashLoopBackOff` was observed (read the `smoke-oom` incident's `first_reason` and `opened_at` against its first `container_state_history` row); (2) the dead-instance index (`restart_count` when waiting/terminated, `restart_count - 1` when running), read off the `log_previous` `idx` values against `restart_count` in `containers`. If either reading is wrong, that is a bug: fix it on the branch with its own test and commit before merging, and record it.

After the run, confirm the fixtures are gone: `KUBECONFIG=./kube/config kubectl -n idios-smoke get pods,deploy -l idios-smoke=true` shows nothing or `Terminating`. Nothing outside `idios-smoke` is touched. `rm -rf .storage/smoke bin` afterwards.

- [ ] **Step 4: Checkpoint and commit**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

```bash
git add hack/smoke/crash-loop.yaml hack/smoke/bad-image.yaml hack/smoke/missing-configmap.yaml hack/smoke/oom.yaml hack/smoke/run.sh Makefile
git commit -m "build: add make smoke against the orbstack cluster"
```

---

### Task 16: Roadmap status; the roadmap is complete

**Files:**
- Modify: `docs/plans/m1-recorder/roadmap.md` (Phase 7 section; plan files list)

- [ ] **Step 1: Record the status**

Replace the `### Phase 7: status, smoke, hardening` section with:

```markdown
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

Smoke run 2026-08-27: <one paragraph from Task 15 Step 3: which incidents
opened, the two Phase 2 readings confirmed or corrected, anything fixed>.

Depends on: 4, 5, 6.

The roadmap is complete. Presentation (`internal/query`, CLI/API/UI) is a
later design document and is out of scope for this roadmap.
```

In the `## Plan files` list add `docs/plans/m1-recorder/07-status-smoke-hardening.md (Phase 7)` in place of the "and so on" line. In "Facts a new session needs" add: `data_dir` now defaults to the OS user data directory; every command in this repository passes `-data-dir .storage` (or `.storage/smoke`) and `-kubeconfig ./kube/config`; `make run` and `make smoke` do so.

- [ ] **Step 2: Checkpoint and commit**

Run: `make ascii`

```bash
git add docs/plans/m1-recorder/roadmap.md
git commit -m "docs: record phase 7 completion; roadmap complete"
```

---

## Self-review

**Spec coverage.**
- Process 5 step 6 (`idios status` reports sync state): Task 14 per cluster (`Ready`), decision recorded. "Config changes" paragraph: Task 13 (supervisor, stop then start from step 1; reconcile marks `unwatched`), Task 7 (pool follows).
- Process 10: handler errors counted and never propagated, panics recovered with stack and counted (Task 9); `Writer.Tx` errors counted (Task 4, surfaced in `idios status`); file errors in rows (Phases 5, 6; `Checkpoint` busy now in `sweep_runs.error`, Task 5).
- Process 11: slog JSON file plus terminal text (Task 11); counters: per cluster ready, last event, `last_error`, skew (Tasks 3, 9, 14); writer tx/errors/p99 (Tasks 3, 4); ingest served from tables (Task 2); capture queued/completed/dropped/by gap (Task 7); closer last tick and counts (Task 6); sweep last run per table (Task 2); DB, WAL and artifacts sizes (Task 14).
- Process 12: status file and log file 0600 (Tasks 3, 11); no listener added.
- Process 13: defaults flipped (Task 10), config file follows the data dir (Task 12), CLI writes the two tables (Tasks 1, 12).
- Process 14 closing paragraph: `make smoke` (Task 15).
- Storage 5.1, 5.2: `InsertCluster` leaves identity and URL for the watcher; `AddWatchedNamespace` honours the unique key (Task 1). Storage 11: no config value changed.
- Roadmap Phase 7 list: status struct and subcommand, slog setup, panic recovery, `make smoke`, config reload, the CLI, `data_dir` flip. Every "hands Phase 7" item: `Ready()`/`Offset()`/`last_error` (Task 14), reload (Tasks 7, 13), `Dropped()` (folded into `Stats`, Task 7), artifacts writers and `400` gap (decisions), reserve-before-fetch (Task 8), `TickResult` and `sweep_runs` counters (Tasks 6, 2, 14), Checkpoint busy (Task 5), handler errors counted and panics recovered (Task 9).

**Placeholder scan.** Every step carries its code or exact command. The one deliberately open text is the smoke paragraph in Task 16, filled from the Task 15 run.

**Type consistency.** `k8s.New(cfg, client, h, log, counters *status.Counters)` in Tasks 9, 12, 13. `status.Counters` methods `ObserveTx`, `HandlerError`, `HandlerPanic`, `ObjectSeen`, `LastSeen`, `Writer()`, `Handlers()` in Tasks 3, 4, 9, 14. `store.TxObserver.ObserveTx(time.Duration, error)` matches `(*status.Counters).ObserveTx`. `capture.Stats{Queued, Completed, Dropped, Gaps}` in Tasks 7 and 14. `(*incident.Closer).Last() (time.Time, TickResult)` in Tasks 6 and 14. `store.Querier` accepted by `ListClusters` and the Task 2 reads; `*sql.DB` from `Reader.DB()` and `*sql.Tx` both satisfy it (Tasks 2, 12, 13, 14). `config.DefaultDataDir()` in Tasks 10 and 12. `clusterSpec`, `specsFromRows`, `reloadPlan`, `(*supervisor).list()` in Tasks 13 and 14.

**`.ai` rules by name.** ascii-only: plan and every code block are ASCII. tests: each test has its trace in the task prose; variants are table rows; assertions are whole structs, maps, row sets or exact output. comments: doc comments on every exported identifier; body comments carry reasons only. code-is-truth: no code comment names a document or plan. scope: `Dropped()` deleted when `Stats` replaced it; no seam without a caller in this plan. commits: one per task, named paths, no trailers.
