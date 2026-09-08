# Phase 4: k8s Watcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Read `.ai/*.md` before writing any code; those rules override habits.

**Goal:** One package, `internal/k8s`, whose `Watcher` turns a cluster row plus its watched namespaces into running informers that feed the Phase 3 processor: kubeconfig loading, cluster identity, one `SharedInformerFactory` per namespace, two-stage start (ReplicaSets + Jobs synced before Pods + Events), a reconcile pass, backoff on failure, `DeletedFinalStateUnknown` unwrap, a skew round-tripper, a lister-backed `OwnerResolver`. Plus the few store and processor additions the watcher needs (cluster row updates, live-row loads, reconcile), and `cmd/idios` wiring so the binary watches the OrbStack cluster.

**Architecture:** `k8s` imports `client-go`, `ingest` (for `OwnerResolver`) and `clock`; it never imports `store` (the archtest forbids it). It talks to the processor through a `Handler` interface defined in `k8s` that `*processor.Processor` satisfies; `cmd/idios` asserts that at compile time. Everything that needs SQL (marking a cluster connected, recording `last_error`, listing live rows, the reconcile decision) is a `Processor` method backed by helpers in `store/cluster_sql.go`. The watcher supplies what only it knows: the identity lookup result, the API server URL, and a `live(uid)` predicate built from the informer stores. Handler errors are logged at the handler boundary and never returned to client-go.

**Tech Stack:** Go 1.24, `k8s.io/client-go` v0.34.1 (new dependency; matches the pinned `k8s.io/api` / `apimachinery` v0.34.1; v0.35 needs Go 1.25), `k8s.io/client-go/kubernetes/fake` for tests, `modernc.org/sqlite` v1.45.0, `github.com/google/go-cmp`.

**Spec:** `docs/design/process-architecture.md` Sections 2 (dependency rules), 4 (handlers inline, 4.2 owner resolution, 4.3 deletes), 5 (watcher lifecycle, failure handling), 9 (skew), 10 (error handling), 14.4 (informer integration test); `docs/design/client-go-methods.md` Sections 1, 2, 3.1 (identity), 3.3 (`workload_*` from listers), 5; `docs/design/data-storage.md` Sections 4 (reconciliation on startup), 5.1 (`last_error` cleared on next successful sync), 5.2. Roadmap: `docs/plans/m1-recorder/roadmap.md` (Phase 4 section and the "Phase 3 hands Phase 4" paragraph). The plan may cite these; the code must not (`.ai/code-is-truth.md`).

## Global Constraints

- `.ai/ascii-only.md`, `.ai/tests.md`, `.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`, `.ai/commits.md` apply to every line. Every exported identifier gets a one-line doc comment even where a block below omits it. Code blocks contain no comments beyond those shown.
- Every task ends with `go build ./... && go vet ./... && go test ./... && make ascii` green, then one commit per `.ai/commits.md` (`area: what`, no trailers, `git add` named paths). Work happens on branch `phase-4-k8s-watcher`; the orchestrator fast-forwards `main` onto it locally at the end and deletes the branch. No remote, no push.
- Module path `idios`. One new dependency: `k8s.io/client-go@v0.34.1` (Task 3). `k8s.io/api` and `k8s.io/apimachinery` stay at v0.34.1; if `go get` bumps them, pin them back.
- Dependency rules (archtest): `internal/k8s` never imports `internal/store`; `processor`, `ingest`, `incident` never import `client-go` or `k8s`. Test files are exempt (the archtest skips `_test.go`), so `k8s` tests may use `store` and `processor` directly.
- Every timestamp written goes through `clock.Format`; process time is `clock.Clock.Now()` injected. Kubernetes timestamps are never adjusted, including by the skew measurement.
- No schema change. `0001_init.sql` is the contract. If a task appears to need one, stop and report.
- Store SQL for this phase lives only in `internal/store/cluster_sql.go`. `ingest_sql.go` and `rows.go` are not edited.
- Fixtures: the existing JSON under `internal/ingest/testdata/`, read via `../ingest/testdata`. No new fixtures.
- Test cluster for manual verification only: `KUBECONFIG=./kube/config`, namespace `idios-smoke`. Never `~/.kube/config`, never another namespace. Automated tests use the fake clientset and need no cluster.
- Panic recovery at the handler boundary, counters, `idios status`, config reload and the cluster/namespace CLI are Phase 7. Not built here.

## Decisions made here (the spec or roadmap leaves them open)

- **`Handler` lives in `k8s`, satisfied by `*processor.Processor`.** The process doc says `k8s` calls into ingest "through a small handler interface" and does not import `store`. The interface is the seven Phase 3 methods plus three added in Task 2 (`ClusterConnected`, `ClusterError`, `Reconcile`). `k8s` does not import `processor`; `cmd/idios` holds the `var _ k8s.Handler = (*processor.Processor)(nil)` assertion.
- **The watcher starts from a `clusters` row, it does not create one.** Rows in `clusters` and `watched_namespaces` are the source of truth (storage 5.2); the CLI that writes them is Phase 7. `k8s.Config{ClusterID, ContextName, Namespaces}` is read from those rows by `cmd/idios`. On connect the watcher reports identity and URL; `MarkClusterConnected` writes them onto the row (`identity = COALESCE(new, old)` so an RBAC downgrade does not erase a known identity), sets `last_connected_at` and clears `last_error`. A UNIQUE violation on `identity` (two rows for one cluster) surfaces as a connect failure in `last_error`; merging rows is not attempted.
- **Reconcile is a processor method.** `Reconcile(ctx, clusterID, watched, live)` loads the cluster's live pod and job rows, and for each one `live(uid)` rejects calls `PodDeleted(uid, reconcile|unwatched)` or `JobDeleted(uid)` -- one transaction per row, as the process doc asks. `unwatched` when the row's namespace is not in `watched`. Errors are joined and returned; the watcher logs them and still becomes ready, because a row it cannot mark is not a reason to stop watching.
- **`deletion_source = 'watch'` is a `k8s`-local constant.** `k8s` cannot import `store.DeletionSourceWatch`; the string is the value the processor stores verbatim.
- **Two-stage start uses `factory.Start` twice.** `Start` only starts informers that were requested with `.Informer()` and not yet started. Stage 1 requests ReplicaSets and Jobs (the listers request them too), starts, waits; stage 2 requests Pods and Events, starts again, waits. Handlers are registered before the informer they belong to starts.
- **Sync wait tolerates a forbidden informer.** `SetWatchErrorHandlerWithContext` marks an informer forbidden on the first `IsForbidden` list error and writes `last_error = "forbidden: list <resource> in namespace <ns>"` once. The sync wait polls `HasSynced() || forbidden` per informer with a 30 s deadline, so a Role that lacks `events` or `replicasets` does not stall the pods informer. Any other list/watch error is logged at warn; client-go retries it on its own.
- **Backoff is 1 s doubling to 60 s** between failed `runOnce` attempts, reset to 1 s after an attempt that reached ready. The sleep is a `Watcher` field so tests assert the sequence without waiting.
- **`ClientFunc` is the seam for tests.** `New` takes `func() (kubernetes.Interface, string, error)`; production uses `KubeconfigClient(path, context, skew)` which re-reads the kubeconfig on each attempt and wraps the transport with the skew round-tripper. Tests pass a fake clientset.
- **Skew is measured per cluster in `k8s.Skew`**, reported by `Offset()` for Phase 7's status, logged at warn once per hour while it exceeds 5 minutes. Nothing stored is corrected.
- **Event deletes are ignored.** Kubernetes expires Events; the `k8s_events` row is history and stays for the sweeper.
- **The capture sink in `cmd/idios` is a logging stand-in** until Phase 5's pool exists: requests are logged at info and dropped.

## File structure

```
internal/store/cluster_sql.go            LiveObject, cluster row updates, live-row loads, list clusters/namespaces
internal/store/cluster_sql_test.go
internal/processor/cluster.go            ClusterConnected, ClusterError, Reconcile
internal/processor/cluster_test.go
internal/k8s/skew.go                     Skew, RoundTripper
internal/k8s/skew_test.go
internal/k8s/identity.go                 clusterIdentity
internal/k8s/identity_test.go
internal/k8s/client.go                   ClientFunc, KubeconfigClient
internal/k8s/handler.go                  Handler, deletionSourceWatch, handlerFuncs, deleted
internal/k8s/handler_test.go
internal/k8s/resolver.go                 listerResolver (ingest.OwnerResolver)
internal/k8s/resolver_test.go
internal/k8s/watcher.go                  Config, Watcher, New, Ready, Run, runOnce
internal/k8s/sync.go                     namedInformer, forbiddenSet, register, waitSync
internal/k8s/watcher_test.go             fake clientset + real processor + temp SQLite
cmd/idios/main.go                        wiring, logSink, Handler assertion
docs/plans/m1-recorder/roadmap.md      Phase 4 status and handoff
```

Every test below has a one-line trace to a spec statement. No trace, no test.

---

### Task 1: Store cluster helpers

**Files:**
- Create: `internal/store/cluster_sql.go`
- Test: `internal/store/cluster_sql_test.go`

**Interfaces:**
- Consumes: `Cluster`, `rowScanner` from `rows.go`; test helpers `openMigratedStore`, `insertCluster`, `inTx`, `ptr`, `mustExec`, `testEpoch`.
- Produces: `type LiveObject struct{ UID, Namespace string }`; `func ListClusters(ctx, tx *sql.Tx) ([]Cluster, error)` (ordered by `id`); `func ListWatchedNamespaces(ctx, tx, clusterID int64) ([]string, error)` (ordered by `name`); `func MarkClusterConnected(ctx, tx, id int64, identity *string, apiServerURL, at string) error`; `func SetClusterError(ctx, tx, id int64, msg, at string) error`; `func LoadLivePods(ctx, tx, clusterID int64) ([]LiveObject, error)`; `func LoadLiveJobs(ctx, tx, clusterID int64) ([]LiveObject, error)` (both ordered by `uid`, `deleted_at IS NULL` only).

Tests and their trace:
- `TestClusterConnectedClearsErrorKeepsKnownIdentity`: storage 5.1 `last_error` "Cleared on the next successful sync"; client-go 3.1 identity null under RBAC (a nil identity must not erase one learned earlier).
- `TestWatchedNamespacesPerCluster`: storage 5.2 "which namespaces the app watches in each cluster" -- another cluster's rows are not returned.

`LoadLivePods` / `LoadLiveJobs` are asserted through the Task 2 reconcile test, which checks the rows they lead to.

- [ ] **Step 1: Write the failing tests**

`internal/store/cluster_sql_test.go`:

```go
package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"idios/internal/clock"
)

func TestClusterConnectedClearsErrorKeepsKnownIdentity(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "ks-uid")
	ctx := context.Background()
	inTx(t, s, func(tx *sql.Tx) error {
		return SetClusterError(ctx, tx, cid, "connect: dial tcp 127.0.0.1:26443: connection refused", "2026-08-27T11:59:00.000000Z")
	})
	inTx(t, s, func(tx *sql.Tx) error {
		return MarkClusterConnected(ctx, tx, cid, nil, "https://127.0.0.1:26443", "2026-08-27T12:00:00.000000Z")
	})
	var got []Cluster
	inTx(t, s, func(tx *sql.Tx) (err error) { got, err = ListClusters(ctx, tx); return err })
	want := []Cluster{{ID: cid, Identity: ptr("ks-uid"), Name: "c", ContextName: "ctx", APIServerURL: "https://127.0.0.1:26443",
		FirstSeenAt: clock.Format(testEpoch), LastConnectedAt: ptr("2026-08-27T12:00:00.000000Z")}}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}

func TestWatchedNamespacesPerCluster(t *testing.T) {
	s, _ := openMigratedStore(t)
	a := insertCluster(t, s, "a")
	b := insertCluster(t, s, "b")
	ts := clock.Format(testEpoch)
	mustExec(t, s, "INSERT INTO watched_namespaces (cluster_id, name, added_at) VALUES (?, 'payments', ?), (?, 'idios-smoke', ?), (?, 'other', ?)", a, ts, a, ts, b, ts)
	var got []string
	inTx(t, s, func(tx *sql.Tx) (err error) { got, err = ListWatchedNamespaces(context.Background(), tx, a); return err })
	if d := cmp.Diff([]string{"idios-smoke", "payments"}, got); d != "" {
		t.Fatal(d)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestClusterConnected|TestWatchedNamespaces' -v`
Expected: compile error, `undefined: SetClusterError` and friends.

- [ ] **Step 3: Write the helpers**

`internal/store/cluster_sql.go`:

```go
package store

import (
	"context"
	"database/sql"
)

// LiveObject is a pod or job row not yet marked deleted.
type LiveObject struct {
	UID       string
	Namespace string
}

const clusterColumns = `id, identity, name, context_name, api_server_url, first_seen_at, last_connected_at, last_error, last_error_at`

func scanCluster(r rowScanner) (Cluster, error) {
	var c Cluster
	err := r.Scan(&c.ID, &c.Identity, &c.Name, &c.ContextName, &c.APIServerURL, &c.FirstSeenAt, &c.LastConnectedAt, &c.LastError, &c.LastErrorAt)
	return c, err
}

// ListClusters returns every cluster row in id order.
func ListClusters(ctx context.Context, tx *sql.Tx) ([]Cluster, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+clusterColumns+" FROM clusters ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Cluster
	for rows.Next() {
		c, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListWatchedNamespaces returns the namespace names watched in one cluster.
func ListWatchedNamespaces(ctx context.Context, tx *sql.Tx, clusterID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name FROM watched_namespaces WHERE cluster_id = ? ORDER BY name", clusterID)
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

// MarkClusterConnected records a successful connection and clears the last
// error. A nil identity keeps the one an earlier connection learned.
func MarkClusterConnected(ctx context.Context, tx *sql.Tx, id int64, identity *string, apiServerURL, at string) error {
	_, err := tx.ExecContext(ctx, `
UPDATE clusters SET identity = COALESCE(?, identity), api_server_url = ?, last_connected_at = ?, last_error = NULL, last_error_at = NULL
WHERE id = ?`, identity, apiServerURL, at, id)
	return err
}

// SetClusterError records the most recent failure in words a user can act on.
func SetClusterError(ctx context.Context, tx *sql.Tx, id int64, msg, at string) error {
	_, err := tx.ExecContext(ctx, "UPDATE clusters SET last_error = ?, last_error_at = ? WHERE id = ?", msg, at, id)
	return err
}

// LoadLivePods returns the cluster's pod rows not yet marked deleted.
func LoadLivePods(ctx context.Context, tx *sql.Tx, clusterID int64) ([]LiveObject, error) {
	return loadLive(ctx, tx, "pods", clusterID)
}

// LoadLiveJobs returns the cluster's job rows not yet marked deleted.
func LoadLiveJobs(ctx context.Context, tx *sql.Tx, clusterID int64) ([]LiveObject, error) {
	return loadLive(ctx, tx, "jobs", clusterID)
}

func loadLive(ctx context.Context, tx *sql.Tx, table string, clusterID int64) ([]LiveObject, error) {
	rows, err := tx.QueryContext(ctx, "SELECT uid, namespace FROM "+table+" WHERE cluster_id = ? AND deleted_at IS NULL ORDER BY uid", clusterID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []LiveObject
	for rows.Next() {
		var o LiveObject
		if err := rows.Scan(&o.UID, &o.Namespace); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestClusterConnected|TestWatchedNamespaces' -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/store/cluster_sql.go internal/store/cluster_sql_test.go && git commit -m "store: add cluster row and live-row helpers"`

---

### Task 2: Processor cluster methods and reconcile

**Files:**
- Create: `internal/processor/cluster.go`
- Test: `internal/processor/cluster_test.go`

**Interfaces:**
- Consumes: `store.MarkClusterConnected`, `store.SetClusterError`, `store.LoadLivePods`, `store.LoadLiveJobs`, `store.LiveObject`, `store.DeletionSourceReconcile`, `store.DeletionSourceUnwatched`; `Processor.PodDeleted`, `Processor.JobDeleted`; test harness `newHarness`, `harness.exec`, `harness.pod`, `harness.incidents`, `harness.seedIncident`, `harness.tx`, `diff`, `testNow`.
- Produces: `func (p *Processor) ClusterConnected(ctx, clusterID int64, identity *string, apiServerURL string) error`; `func (p *Processor) ClusterError(ctx, clusterID int64, msg string) error`; `func (p *Processor) Reconcile(ctx, clusterID int64, watched []string, live func(uid string) bool) error`.

Tests and their trace:
- `TestReconcileMarksRowsTheStoresNoLongerHold`: storage 4 "Reconciliation on startup" (absent -> `reconcile`; namespace removed from `watched_namespaces` -> `unwatched`; present rows untouched; incidents closed `pod_deleted` / `job_finished`); process 5 step 5 (only this cluster's rows); Phase 3 decision that a reconcile delete enqueues no capture request.

`ClusterConnected` and `ClusterError` are one `Tx` each around a Task 1 helper whose rule is tested there; the Task 5 integration test asserts the row they produce.

- [ ] **Step 1: Write the failing test**

`internal/processor/cluster_test.go`:

```go
package processor

import (
	"context"
	"database/sql"
	"testing"

	"idios/internal/clock"
	"idios/internal/store"
)

type deletion struct {
	UID    string
	At     *string
	Source *string
}

func (h *harness) podDeletions(t *testing.T) []deletion {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), "SELECT uid, deleted_at, deletion_source FROM pods ORDER BY uid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []deletion
	for rows.Next() {
		var d deletion
		if err := rows.Scan(&d.UID, &d.At, &d.Source); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func (h *harness) jobDeletions(t *testing.T) []deletion {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), "SELECT uid, deleted_at FROM jobs ORDER BY uid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []deletion
	for rows.Next() {
		var d deletion
		if err := rows.Scan(&d.UID, &d.At); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func TestReconcileMarksRowsTheStoresNoLongerHold(t *testing.T) {
	h := newHarness(t)
	now := clock.Format(testNow)
	earlier := "2026-08-27T11:00:00.000000Z"
	h.exec(t, `INSERT INTO clusters (id, identity, name, context_name, api_server_url, first_seen_at) VALUES (2, 'c2', 'c2', 'other', 'https://10.0.0.2', ?)`, now)
	insertPod := func(uid string, cluster int64, ns string, deletedAt, source *string) {
		h.exec(t, `INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at, deleted_at, deletion_source)
VALUES (?, ?, ?, ?, 'Running', ?, ?, ?, ?, ?)`, uid, cluster, ns, "pod-"+uid, earlier, earlier, earlier, deletedAt, source)
	}
	insertPod("p-done", 1, "idios-smoke", ptr(earlier), ptr(store.DeletionSourceWatch))
	insertPod("p-gone", 1, "idios-smoke", nil, nil)
	insertPod("p-live", 1, "idios-smoke", nil, nil)
	insertPod("p-other", 2, "idios-smoke", nil, nil)
	insertPod("p-unwatched", 1, "legacy", nil, nil)
	for _, uid := range []string{"j-gone", "j-live"} {
		h.exec(t, `INSERT INTO jobs (uid, cluster_id, namespace, name, restart_policy, created_at, first_seen_at, last_seen_at) VALUES (?, 1, 'idios-smoke', ?, 'Never', ?, ?, ?)`,
			uid, "job-"+uid, earlier, earlier, earlier)
	}
	podInc := h.seedIncident(t, "p-gone", "api", store.CategoryCrash, nil)
	var jobInc int64
	h.tx(t, func(tx *sql.Tx) (err error) {
		jobInc, err = store.OpenIncident(context.Background(), tx, store.Incident{
			ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectJob, JobUID: ptr("j-gone"), WorkloadKind: "Job", WorkloadName: "job-j-gone",
			Category: store.CategoryJobFailed, FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", Occurrences: 1, OpenedAt: earlier, LastSeenAt: earlier,
		})
		return err
	})

	live := map[string]bool{"p-live": true, "j-live": true}
	if err := h.p.Reconcile(context.Background(), 1, []string{"idios-smoke"}, func(uid string) bool { return live[uid] }); err != nil {
		t.Fatal(err)
	}

	diff(t, []deletion{
		{"p-done", ptr(earlier), ptr(store.DeletionSourceWatch)},
		{"p-gone", ptr(now), ptr(store.DeletionSourceReconcile)},
		{"p-live", nil, nil},
		{"p-other", nil, nil},
		{"p-unwatched", ptr(now), ptr(store.DeletionSourceUnwatched)},
	}, h.podDeletions(t))
	diff(t, []deletion{{"j-gone", ptr(now), nil}, {"j-live", nil, nil}}, h.jobDeletions(t))
	closes := map[int64][2]*string{}
	for _, uid := range []string{"p-gone", "j-gone"} {
		for _, inc := range h.incidents(t, uid) {
			closes[inc.ID] = [2]*string{inc.ClosedAt, inc.CloseReason}
		}
	}
	diff(t, map[int64][2]*string{
		podInc: {ptr(now), ptr(store.ClosePodDeleted)},
		jobInc: {ptr(now), ptr(store.CloseJobFinished)},
	}, closes)
	if len(h.sink.reqs) != 0 {
		t.Errorf("reconcile enqueued %d capture requests, want none: %+v", len(h.sink.reqs), h.sink.reqs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/processor/ -run TestReconcile -v`
Expected: compile error, `h.p.Reconcile undefined`.

- [ ] **Step 3: Write the methods**

`internal/processor/cluster.go`:

```go
package processor

import (
	"context"
	"database/sql"
	"errors"

	"idios/internal/clock"
	"idios/internal/store"
)

// ClusterConnected records a successful connection to the cluster row.
func (p *Processor) ClusterConnected(ctx context.Context, clusterID int64, identity *string, apiServerURL string) error {
	nowS := clock.Format(p.clk.Now())
	return p.w.Tx(ctx, func(tx *sql.Tx) error {
		return store.MarkClusterConnected(ctx, tx, clusterID, identity, apiServerURL, nowS)
	})
}

// ClusterError records the cluster's most recent failure.
func (p *Processor) ClusterError(ctx context.Context, clusterID int64, msg string) error {
	nowS := clock.Format(p.clk.Now())
	return p.w.Tx(ctx, func(tx *sql.Tx) error {
		return store.SetClusterError(ctx, tx, clusterID, msg, nowS)
	})
}

// Reconcile marks the cluster's live pod and job rows that live rejects. A
// row whose namespace is no longer watched is unwatched, not gone; the rest
// vanished while nothing was watching. Every row is its own transaction and
// a failing row does not stop the others.
func (p *Processor) Reconcile(ctx context.Context, clusterID int64, watched []string, live func(uid string) bool) error {
	isWatched := map[string]bool{}
	for _, ns := range watched {
		isWatched[ns] = true
	}
	var pods, jobs []store.LiveObject
	err := p.w.Tx(ctx, func(tx *sql.Tx) (err error) {
		if pods, err = store.LoadLivePods(ctx, tx, clusterID); err != nil {
			return err
		}
		jobs, err = store.LoadLiveJobs(ctx, tx, clusterID)
		return err
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, o := range pods {
		if live(o.UID) {
			continue
		}
		source := store.DeletionSourceReconcile
		if !isWatched[o.Namespace] {
			source = store.DeletionSourceUnwatched
		}
		if err := p.PodDeleted(ctx, o.UID, source); err != nil {
			errs = append(errs, err)
		}
	}
	for _, o := range jobs {
		if live(o.UID) {
			continue
		}
		if err := p.JobDeleted(ctx, o.UID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/processor/ -run TestReconcile -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/processor/cluster.go internal/processor/cluster_test.go && git commit -m "processor: add cluster connection records and reconcile pass"`

---

### Task 3: client-go dependency, skew round-tripper, identity, kubeconfig client

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/k8s/skew.go`, `internal/k8s/identity.go`, `internal/k8s/client.go`
- Test: `internal/k8s/skew_test.go`, `internal/k8s/identity_test.go`

**Interfaces:**
- Consumes: `clock.Clock`, `clock.Fake`.
- Produces: `type Skew struct`; `func NewSkew(clk clock.Clock, log *slog.Logger) *Skew`; `func (s *Skew) Offset() time.Duration`; `func (s *Skew) RoundTripper(rt http.RoundTripper) http.RoundTripper`; `func clusterIdentity(ctx, client kubernetes.Interface) (*string, error)`; `type ClientFunc func() (kubernetes.Interface, string, error)`; `func KubeconfigClient(path, contextName string, skew *Skew) ClientFunc`.

Tests and their trace:
- `TestSkewKeepsLatestOffsetAndWarnsHourly`: process 9 "reads the Date header of every API response and keeps the most recent difference to local time. When it exceeds 5 minutes ... logs it once per hour"; a response without a parseable Date leaves the value unchanged.
- `TestIdentityIsNullWhenForbidden`: client-go 3.1 "If this returns a 403 ... leave null"; any other error is a connection failure, not an identity.

`KubeconfigClient` is clientcmd wiring exercised only against a real kubeconfig; the manual verification in Task 6 covers it.

- [ ] **Step 1: Add the dependency**

Run: `go get k8s.io/client-go@v0.34.1 && go mod tidy`

Then check: `grep -E 'k8s.io/(api|apimachinery|client-go) ' go.mod` must show all three at `v0.34.1`. If `go mod tidy` moved `api` or `apimachinery`, run `go get k8s.io/api@v0.34.1 k8s.io/apimachinery@v0.34.1 && go mod tidy` and check again. The `go 1.24.0` / `toolchain` lines stay as they are.

The package needs at least one non-test file for `go mod tidy` to keep client-go; write Step 3 before tidying if tidy drops it, then tidy again.

- [ ] **Step 2: Write the failing tests**

`internal/k8s/skew_test.go`:

```go
package k8s

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"idios/internal/clock"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSkewKeepsLatestOffsetAndWarnsHourly(t *testing.T) {
	var logs bytes.Buffer
	clk := clock.NewFake(testNow)
	s := NewSkew(clk, slog.New(slog.NewTextHandler(&logs, nil)))
	var header string
	rt := s.RoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		h := http.Header{}
		if header != "" {
			h.Set("Date", header)
		}
		return &http.Response{StatusCode: 200, Header: h}, nil
	}))
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	steps := []struct {
		name       string
		advance    time.Duration
		date       string
		wantOffset time.Duration
		wantWarns  int
	}{
		{"no date header", 0, "", 0, 0},
		{"ten minutes ahead warns", 0, testNow.Add(10 * time.Minute).UTC().Format(http.TimeFormat), 10 * time.Minute, 1},
		{"still ahead a minute later stays quiet", time.Minute, testNow.Add(11 * time.Minute).UTC().Format(http.TimeFormat), 10 * time.Minute, 1},
		{"garbage date leaves the value", 0, "not a date", 10 * time.Minute, 1},
		{"an hour later warns again", time.Hour, testNow.Add(time.Hour + time.Minute - 6*time.Minute).UTC().Format(http.TimeFormat), -6 * time.Minute, 2},
		{"back in tolerance", time.Hour, testNow.Add(2*time.Hour + time.Minute + 30*time.Second).UTC().Format(http.TimeFormat), 30 * time.Second, 2},
	}
	for _, st := range steps {
		clk.Advance(st.advance)
		header = st.date
		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		_ = resp
		if got := s.Offset(); got != st.wantOffset {
			t.Errorf("%s: offset = %v, want %v", st.name, got, st.wantOffset)
		}
		if got := strings.Count(logs.String(), "clock skew"); got != st.wantWarns {
			t.Errorf("%s: warnings = %d, want %d\n%s", st.name, got, st.wantWarns, logs.String())
		}
	}
}
```

`internal/k8s/identity_test.go`:

```go
package k8s

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestIdentityIsNullWhenForbidden(t *testing.T) {
	kubeSystem := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "ks-uid"}}
	nsResource := schema.GroupResource{Resource: "namespaces"}
	cases := []struct {
		name    string
		react   error
		want    *string
		wantErr bool
	}{
		{"readable", nil, ptr("ks-uid"), false},
		{"forbidden", apierrors.NewForbidden(nsResource, "kube-system", errors.New("no")), nil, false},
		{"unreachable", apierrors.NewInternalError(errors.New("boom")), nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fake.NewClientset(kubeSystem)
			if c.react != nil {
				client.PrependReactor("get", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, c.react })
			}
			got, err := clusterIdentity(context.Background(), client)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Fatalf("identity = %v, want %v", deref(got), deref(c.want))
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
```

- [ ] **Step 3: Write the code**

`internal/k8s/skew.go`:

```go
// Package k8s runs the informers of one cluster and hands every object to a
// Handler. It owns kubeconfig loading, cluster identity, start order,
// reconcile after sync and backoff; what the objects mean is decided
// elsewhere.
package k8s

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"idios/internal/clock"
)

const (
	skewWarnAbove = 5 * time.Minute
	skewWarnEvery = time.Hour
)

// Skew keeps the latest difference between the API server's Date header and
// local time. It is reported, never applied: stored timestamps keep the
// clock they came from.
type Skew struct {
	clk clock.Clock
	log *slog.Logger

	mu       sync.Mutex
	offset   time.Duration
	warnedAt time.Time
}

// NewSkew returns a Skew that logs through log when the offset is large.
func NewSkew(clk clock.Clock, log *slog.Logger) *Skew {
	return &Skew{clk: clk, log: log}
}

// Offset returns server time minus local time as of the last response.
func (s *Skew) Offset() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offset
}

type roundTripper struct {
	next http.RoundTripper
	skew *Skew
}

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err == nil {
		r.skew.observe(resp.Header.Get("Date"))
	}
	return resp, err
}

// RoundTripper wraps rt so every response's Date header is observed.
func (s *Skew) RoundTripper(rt http.RoundTripper) http.RoundTripper {
	return roundTripper{next: rt, skew: s}
}

func (s *Skew) observe(date string) {
	if date == "" {
		return
	}
	server, err := http.ParseTime(date)
	if err != nil {
		return
	}
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offset = server.Sub(now)
	// The Date header has whole-second resolution; the warning is only
	// worth a line when the difference could change what a human reads.
	if s.offset.Abs() <= skewWarnAbove {
		return
	}
	if !s.warnedAt.IsZero() && now.Sub(s.warnedAt) < skewWarnEvery {
		return
	}
	s.warnedAt = now
	s.log.Warn("clock skew against api server", "offset", s.offset)
}
```

`internal/k8s/identity.go`:

```go
package k8s

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// clusterIdentity returns the kube-system namespace uid, or nil when RBAC
// hides it; the cluster row is then matched on its URL instead.
func clusterIdentity(ctx context.Context, client kubernetes.Interface) (*string, error) {
	ns, err := client.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if apierrors.IsForbidden(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	uid := string(ns.UID)
	return &uid, nil
}
```

`internal/k8s/client.go`:

```go
package k8s

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ClientFunc builds a clientset and reports the API server URL. It runs on
// every connection attempt so an edited kubeconfig is picked up on retry.
type ClientFunc func() (kubernetes.Interface, string, error)

// KubeconfigClient loads contextName from the kubeconfig at path and routes
// every response through skew.
func KubeconfigClient(path, contextName string, skew *Skew) ClientFunc {
	return func() (kubernetes.Interface, string, error) {
		rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/k8s/ -run 'TestSkew|TestIdentity' -v`
Expected: PASS. If `s.offset.Abs()` does not compile, the Go version is below 1.19; it is 1.24 here, so it does.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

The archtest must still pass: `internal/k8s` imports no `idios/internal/store`.

Commit: `git add go.mod go.sum internal/k8s/skew.go internal/k8s/skew_test.go internal/k8s/identity.go internal/k8s/identity_test.go internal/k8s/client.go && git commit -m "k8s: add skew round-tripper, cluster identity and kubeconfig client"`

---

### Task 4: Handler interface, event handler funcs, lister resolver, Watcher skeleton

**Files:**
- Create: `internal/k8s/handler.go`, `internal/k8s/resolver.go`, `internal/k8s/watcher.go`
- Test: `internal/k8s/handler_test.go`, `internal/k8s/resolver_test.go`

**Interfaces:**
- Consumes: `ingest.OwnerResolver`; `ClientFunc` (Task 3).
- Produces: `type Handler interface`; `const deletionSourceWatch = "watch"`; `func deleted[T any](obj any) (T, bool)`; `func handlerFuncs[T metav1.Object](w *Watcher, kind, ns string, upsert, del func(T) error) cache.ResourceEventHandlerFuncs`; `type listerResolver struct`; `func newListerResolver() *listerResolver`; `func (r *listerResolver) add(ns string, rs appslisters.ReplicaSetNamespaceLister, jobs batchlisters.JobNamespaceLister)`; `type Config struct{ ClusterID int64; ContextName string; Namespaces []string }`; `type Watcher struct` with fields `cfg Config; client ClientFunc; h Handler; log *slog.Logger; syncTimeout time.Duration; wait func(ctx, d time.Duration) error; ready atomic.Bool`; `func New(cfg Config, client ClientFunc, h Handler, log *slog.Logger) *Watcher`; `func (w *Watcher) Ready() bool`; `func sleep(ctx, d time.Duration) error`. `Run` arrives in Task 5.

Tests and their trace:
- `TestDeleteUnwrapsFinalStateUnknown`: client-go 2 "DELETE event ... `cache.DeletedFinalStateUnknown` wrapper ... Unwrap before reading `.Obj`"; process 14.4 "A `DeletedFinalStateUnknown` case is included"; an object of the wrong type or a nil inner object is dropped, not passed on.
- `TestResolverReadsControllerFromListers`: client-go 3.3 `workload_kind/name` "look it up in the ReplicaSet informer's store ... A miss falls back" -- a Deployment-owned ReplicaSet yields its controller, a bare one yields nil, an unknown name or an unwatched namespace yields nil.

- [ ] **Step 1: Write the failing tests**

`internal/k8s/handler_test.go`:

```go
package k8s

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func quietWatcher() *Watcher {
	return &Watcher{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestDeleteUnwrapsFinalStateUnknown(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "idios-smoke", UID: "pod-1"}}
	cases := []struct {
		name string
		obj  any
		want []string
	}{
		{"plain object", pod, []string{"pod-1"}},
		{"final state unknown", cache.DeletedFinalStateUnknown{Key: "idios-smoke/web-1", Obj: pod}, []string{"pod-1"}},
		{"final state unknown without object", cache.DeletedFinalStateUnknown{Key: "idios-smoke/web-1"}, nil},
		{"wrong type", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{UID: "job-1"}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			h := handlerFuncs(quietWatcher(), "Pod", "idios-smoke",
				func(*corev1.Pod) error { t.Fatal("upsert called on delete"); return nil },
				func(p *corev1.Pod) error { got = append(got, string(p.UID)); return errors.New("logged, not returned") })
			h.OnDelete(c.obj)
			if d := cmp.Diff(c.want, got); d != "" {
				t.Fatal(d)
			}
		})
	}
}
```

`internal/k8s/resolver_test.go`:

```go
package k8s

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
)

func TestResolverReadsControllerFromListers(t *testing.T) {
	yes := true
	owned := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "web-7d9f8c6b5", Namespace: "idios-smoke",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "web", UID: "dep-web", Controller: &yes}}}}
	bare := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "idios-smoke"}}
	cronOwned := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "import-28812346", Namespace: "idios-smoke",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: "import", UID: "cj-import", Controller: &yes}}}}
	rsIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	jobIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, o := range []any{owned, bare} {
		if err := rsIndexer.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	if err := jobIndexer.Add(cronOwned); err != nil {
		t.Fatal(err)
	}
	r := newListerResolver()
	r.add("idios-smoke", appslisters.NewReplicaSetLister(rsIndexer).ReplicaSets("idios-smoke"), batchlisters.NewJobLister(jobIndexer).Jobs("idios-smoke"))

	cases := []struct {
		name string
		got  *metav1.OwnerReference
		want *metav1.OwnerReference
	}{
		{"deployment-owned replicaset", r.ReplicaSetOwner("idios-smoke", "web-7d9f8c6b5"), &owned.OwnerReferences[0]},
		{"bare replicaset", r.ReplicaSetOwner("idios-smoke", "standalone"), nil},
		{"unknown replicaset", r.ReplicaSetOwner("idios-smoke", "nope"), nil},
		{"unwatched namespace", r.ReplicaSetOwner("payments", "web-7d9f8c6b5"), nil},
		{"cronjob-owned job", r.JobOwner("idios-smoke", "import-28812346"), &cronOwned.OwnerReferences[0]},
		{"unknown job", r.JobOwner("idios-smoke", "nope"), nil},
	}
	for _, c := range cases {
		if d := cmp.Diff(c.want, c.got); d != "" {
			t.Errorf("%s: %s", c.name, d)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/k8s/ -run 'TestDeleteUnwraps|TestResolver' -v`
Expected: compile error, `undefined: handlerFuncs`, `undefined: newListerResolver`.

- [ ] **Step 3: Write the code**

`internal/k8s/handler.go`:

```go
package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"idios/internal/ingest"
)

// Handler receives every object the informers deliver, plus the connection
// and reconcile facts only the watcher knows. The processor implements it.
type Handler interface {
	ClusterConnected(ctx context.Context, clusterID int64, identity *string, apiServerURL string) error
	ClusterError(ctx context.Context, clusterID int64, msg string) error
	Pod(ctx context.Context, clusterID int64, pod *corev1.Pod, r ingest.OwnerResolver) error
	PodDeleted(ctx context.Context, uid, source string) error
	Job(ctx context.Context, clusterID int64, job *batchv1.Job) error
	JobDeleted(ctx context.Context, uid string) error
	ReplicaSet(ctx context.Context, clusterID int64, rs *appsv1.ReplicaSet) error
	ReplicaSetDeleted(ctx context.Context, uid string) error
	Event(ctx context.Context, clusterID int64, ev *corev1.Event) error
	Reconcile(ctx context.Context, clusterID int64, watched []string, live func(uid string) bool) error
}

// deletionSourceWatch is the pods.deletion_source value for a delete the
// informer itself delivered; the processor stores it verbatim.
const deletionSourceWatch = "watch"

// deleted returns the typed object of a notification, looking inside
// DeletedFinalStateUnknown, which the informer sends when the delete itself
// was missed during a watch gap.
func deleted[T any](obj any) (T, bool) {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = d.Obj
	}
	v, ok := obj.(T)
	return v, ok
}

// handlerFuncs adapts upsert and del to informer callbacks. Add and update
// are the same call: the store snapshot, not the informer, decides what
// changed. Errors are logged and dropped; client-go never sees them.
func handlerFuncs[T metav1.Object](w *Watcher, kind, ns string, upsert, del func(T) error) cache.ResourceEventHandlerFuncs {
	on := func(obj any, fn func(T) error) {
		o, ok := deleted[T](obj)
		if !ok {
			w.log.Error("unexpected object", "cluster", w.cfg.ClusterID, "namespace", ns, "kind", kind, "type", fmt.Sprintf("%T", obj))
			return
		}
		if err := fn(o); err != nil {
			w.log.Error("handler failed", "cluster", w.cfg.ClusterID, "namespace", ns, "kind", kind, "uid", string(o.GetUID()), "name", o.GetName(), "err", err)
		}
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
```

`internal/k8s/resolver.go`:

```go
package k8s

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
)

// listerResolver answers owner lookups from the informer stores of each
// watched namespace. It is filled before any informer starts and read-only
// afterwards, so it needs no lock.
type listerResolver struct {
	rs   map[string]appslisters.ReplicaSetNamespaceLister
	jobs map[string]batchlisters.JobNamespaceLister
}

func newListerResolver() *listerResolver {
	return &listerResolver{rs: map[string]appslisters.ReplicaSetNamespaceLister{}, jobs: map[string]batchlisters.JobNamespaceLister{}}
}

func (r *listerResolver) add(ns string, rs appslisters.ReplicaSetNamespaceLister, jobs batchlisters.JobNamespaceLister) {
	r.rs[ns] = rs
	r.jobs[ns] = jobs
}

// ReplicaSetOwner returns the ReplicaSet's controller, or nil when the store
// does not hold it or it has none.
func (r *listerResolver) ReplicaSetOwner(namespace, name string) *metav1.OwnerReference {
	l, ok := r.rs[namespace]
	if !ok {
		return nil
	}
	rs, err := l.Get(name)
	if err != nil {
		return nil
	}
	return metav1.GetControllerOf(rs)
}

// JobOwner returns the Job's controller, or nil when the store does not hold
// it or it has none.
func (r *listerResolver) JobOwner(namespace, name string) *metav1.OwnerReference {
	l, ok := r.jobs[namespace]
	if !ok {
		return nil
	}
	job, err := l.Get(name)
	if err != nil {
		return nil
	}
	return metav1.GetControllerOf(job)
}
```

`internal/k8s/watcher.go`:

```go
package k8s

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Config names the cluster row a Watcher serves and the namespaces it
// watches there.
type Config struct {
	ClusterID   int64
	ContextName string
	Namespaces  []string
}

// Watcher runs the informers of one cluster and feeds a Handler until its
// context ends.
type Watcher struct {
	cfg    Config
	client ClientFunc
	h      Handler
	log    *slog.Logger

	syncTimeout time.Duration
	wait        func(ctx context.Context, d time.Duration) error
	ready       atomic.Bool
}

// New returns a Watcher for cfg. Run starts it.
func New(cfg Config, client ClientFunc, h Handler, log *slog.Logger) *Watcher {
	return &Watcher{cfg: cfg, client: client, h: h, log: log, syncTimeout: 30 * time.Second, wait: sleep}
}

// Ready reports whether every informer is synced and the reconcile pass ran.
func (w *Watcher) Ready() bool {
	return w.ready.Load()
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/k8s/ -run 'TestDeleteUnwraps|TestResolver' -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

`go vet` may report `unusedresult` nothing; `Watcher.wait`, `syncTimeout` and `ready` are used by `New` and `Ready`, so no unused warnings. Note for the reviewer: `Watcher` has no `Run` yet; Task 5 adds it.

Commit: `git add internal/k8s/handler.go internal/k8s/handler_test.go internal/k8s/resolver.go internal/k8s/resolver_test.go internal/k8s/watcher.go && git commit -m "k8s: add handler interface, informer callbacks and lister resolver"`

---

### Task 5: Watcher lifecycle: connect, two-stage start, forbidden tolerance, reconcile, backoff

**Files:**
- Modify: `internal/k8s/watcher.go`
- Create: `internal/k8s/sync.go`
- Test: `internal/k8s/watcher_test.go`

**Interfaces:**
- Consumes: `clusterIdentity`, `handlerFuncs`, `newListerResolver`, `deletionSourceWatch`, `Handler`, `Watcher` fields from Tasks 3-4; test-side: `processor.New`, `store.Open`, `Store.Migrate`, `store.ListClusters`, `store.LoadPod`, `store.LoadIncidentsForSubject`, `clock.NewFake`, fixtures `crash-loop/before.json`, `crash-loop/after.json`, `replicaset/deploy.json`.
- Produces: `func (w *Watcher) Run(ctx) error`; `func (w *Watcher) runOnce(ctx) error`; `type namedInformer struct{ namespace, resource string; inf cache.SharedIndexInformer; h cache.ResourceEventHandler }`; `type forbiddenSet struct`; `func (w *Watcher) register(ctx, ni namedInformer, forbidden *forbiddenSet) error`; `func (w *Watcher) waitSync(ctx, infs []namedInformer, forbidden *forbiddenSet) error`; `const maxBackoff = time.Minute`.

Tests and their trace:
- `TestWatcherRecordsPodLifecycle`: process 14.4 "fake clientset driving real informers against the real processor and a temp SQLite ... Scripted sequences (create pod, update status ..., delete) assert end state of the tables"; process 5 steps 2-5 (identity written, ReplicaSet store synced before the first pod handler so `workload_*` is the Deployment, reconcile marks the row the store lacks); process 4.3 (watch delete closes with `pod_deleted`, `deletion_source = watch`).
- `TestForbiddenInformerRecordsErrorAndKeepsOthers`: process 5 "`IsForbidden` on a specific informer's list ...: record it in `last_error` naming the resource and namespace, keep the other informers running."
- `TestRunBacksOffDoublingToOneMinute`: process 5 "A transport error at step 1-2 ...: write `clusters.last_error`, back off (1s doubling to 60s), retry."
- `TestSyncTimeoutIsAFailure`: process 5 "or a `WaitForCacheSync` timeout: write `clusters.last_error`, back off".

- [ ] **Step 1: Write the failing tests**

`internal/k8s/watcher_test.go`:

```go
package k8s

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"idios/internal/clock"
	"idios/internal/processor"
	"idios/internal/store"
)

const (
	ns     = "idios-smoke"
	web    = "registry.example.com/web:1.4.2"
	webID  = "registry.example.com/web@sha256:1111"
	webTag = "1.4.2"
)

type dropSink struct{}

func (dropSink) Enqueue(processor.CaptureRequest) {}

type run struct {
	t      *testing.T
	s      *store.Store
	client *fake.Clientset
	w      *Watcher
	cancel context.CancelFunc
	done   chan error

	mu     sync.Mutex
	delays []time.Duration
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

func kubeSystem() *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "ks-uid"}}
}

// newRun opens a temp store with cluster row 1, builds the real processor
// and a Watcher over a fake clientset. Backoff sleeps are recorded, not
// slept; a lifecycle test that hits one has failed.
func newRun(t *testing.T, client ClientFunc, objs ...runtime.Object) *run {
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
	r := &run{t: t, s: s, done: make(chan error, 1)}
	r.exec(`INSERT INTO clusters (id, name, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'orbstack', '', ?)`, clock.Format(testNow))
	r.client = fake.NewClientset(objs...)
	if client == nil {
		client = func() (kubernetes.Interface, string, error) { return r.client, "https://fake.invalid", nil }
	}
	proc := processor.New(s.Writer, clk, dropSink{}, 10*time.Minute)
	r.w = New(Config{ClusterID: 1, ContextName: "orbstack", Namespaces: []string{ns}}, client, proc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.w.syncTimeout = 5 * time.Second
	r.w.wait = func(ctx context.Context, d time.Duration) error {
		r.mu.Lock()
		r.delays = append(r.delays, d)
		r.mu.Unlock()
		return ctx.Err()
	}
	return r
}

func (r *run) start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() { r.done <- r.w.Run(ctx) }()
	r.t.Cleanup(func() {
		cancel()
		<-r.done
	})
}

func (r *run) exec(query string, args ...any) {
	r.t.Helper()
	err := r.s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), query, args...)
		return err
	})
	if err != nil {
		r.t.Fatalf("%v\n%s", err, query)
	}
}

func (r *run) cluster() store.Cluster {
	r.t.Helper()
	var out []store.Cluster
	err := r.s.Writer.Tx(context.Background(), func(tx *sql.Tx) (err error) { out, err = store.ListClusters(context.Background(), tx); return err })
	if err != nil || len(out) != 1 {
		r.t.Fatalf("clusters: %v %+v", err, out)
	}
	return out[0]
}

func (r *run) pod(uid string) *store.Pod {
	r.t.Helper()
	var out *store.Pod
	err := r.s.Writer.Tx(context.Background(), func(tx *sql.Tx) (err error) { out, err = store.LoadPod(context.Background(), tx, uid); return err })
	if err != nil {
		r.t.Fatal(err)
	}
	return out
}

func (r *run) incidents(uid string) []store.Incident {
	r.t.Helper()
	var out []store.Incident
	err := r.s.Writer.Tx(context.Background(), func(tx *sql.Tx) (err error) {
		out, err = store.LoadIncidentsForSubject(context.Background(), tx, uid)
		return err
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return out
}

func (r *run) historyCount() int {
	r.t.Helper()
	var n int
	if err := r.s.Reader.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM container_state_history").Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// eventually polls because informer handlers run on their own goroutines.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func diff(t *testing.T, want, got any) {
	t.Helper()
	if d := cmp.Diff(want, got, cmpopts.EquateEmpty()); d != "" {
		t.Error(d)
	}
}

func TestWatcherRecordsPodLifecycle(t *testing.T) {
	var rs appsv1.ReplicaSet
	var before, after corev1.Pod
	fixture(t, "replicaset/deploy.json", &rs)
	fixture(t, "crash-loop/before.json", &before)
	fixture(t, "crash-loop/after.json", &after)
	r := newRun(t, nil, kubeSystem(), &rs, &before)
	stale := "2026-08-27T11:00:00.000000Z"
	r.exec(`INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at) VALUES ('pod-stale', 1, ?, 'gone', 'Running', ?, ?, ?)`, ns, stale, stale, stale)
	now := clock.Format(testNow)
	r.start()
	eventually(t, "ready", r.w.Ready)

	diff(t, store.Cluster{ID: 1, Identity: ptr("ks-uid"), Name: "c", ContextName: "orbstack", APIServerURL: "https://fake.invalid", FirstSeenAt: now, LastConnectedAt: ptr(now)}, r.cluster())
	diff(t, &store.Pod{UID: "pod-crash", ClusterID: 1, Namespace: ns, Name: "web-7d9f8c6b5-abcde", NodeName: ptr("node-a"), Phase: "Running", QOSClass: ptr("BestEffort"),
		ControllerKind: "ReplicaSet", ControllerName: "web-7d9f8c6b5", ControllerUID: "rs-web-1", WorkloadKind: "Deployment", WorkloadName: "web",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: ptr("2026-08-27T11:45:05.000000Z"), FirstSeenAt: now, LastSeenAt: now}, r.pod("pod-crash"))
	if n := r.historyCount(); n != 0 {
		t.Errorf("first sight wrote %d history rows, want 0", n)
	}
	gone := r.pod("pod-stale")
	diff(t, [2]*string{ptr(now), ptr(store.DeletionSourceReconcile)}, [2]*string{gone.DeletedAt, gone.DeletionSource})

	if _, err := r.client.CoreV1().Pods(ns).Update(context.Background(), &after, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "crash incident", func() bool { return len(r.incidents("pod-crash")) == 1 })
	crash := store.Incident{ID: 1, ClusterID: 1, Namespace: ns, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api",
		WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash, FirstReason: "CrashLoopBackOff", LastReason: "CrashLoopBackOff",
		Image: ptr(web), ImageTag: ptr(webTag), ImageID: ptr(webID), Occurrences: 1, OpenedAt: "2026-08-27T11:55:00.000000Z", LastSeenAt: now}
	diff(t, []store.Incident{crash}, r.incidents("pod-crash"))
	if n := r.historyCount(); n != 1 {
		t.Errorf("transition wrote %d history rows, want 1", n)
	}

	if err := r.client.CoreV1().Pods(ns).Delete(context.Background(), after.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "pod deleted", func() bool { return r.pod("pod-crash").DeletedAt != nil })
	deleted := r.pod("pod-crash")
	diff(t, [3]*string{ptr(now), ptr(store.DeletionSourceWatch), ptr(store.DeletionReasonUnknown)}, [3]*string{deleted.DeletedAt, deleted.DeletionSource, deleted.DeletionReason})
	crash.ClosedAt, crash.CloseReason = ptr(now), ptr(store.ClosePodDeleted)
	diff(t, []store.Incident{crash}, r.incidents("pod-crash"))

	r.cancel()
	if err := <-r.done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run returned %v, want context.Canceled", err)
	}
	r.done <- nil
	if len(r.delays) != 0 {
		t.Errorf("backoff slept %v during a healthy run", r.delays)
	}
}

func TestForbiddenInformerRecordsErrorAndKeepsOthers(t *testing.T) {
	var before corev1.Pod
	fixture(t, "crash-loop/before.json", &before)
	r := newRun(t, nil, kubeSystem(), &before)
	r.client.PrependReactor("list", "replicasets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "replicasets"}, "", errors.New("no list"))
	})
	r.start()
	eventually(t, "ready despite forbidden replicasets", r.w.Ready)
	eventually(t, "last_error", func() bool { return r.cluster().LastError != nil })
	c := r.cluster()
	diff(t, [2]*string{ptr("forbidden: list replicasets in namespace idios-smoke"), ptr(clock.Format(testNow))}, [2]*string{c.LastError, c.LastErrorAt})
	pod := r.pod("pod-crash")
	if pod == nil {
		t.Fatal("pods informer did not run")
	}
	diff(t, [2]string{"ReplicaSet", "web-7d9f8c6b5"}, [2]string{pod.WorkloadKind, pod.WorkloadName})
}

func TestRunBacksOffDoublingToOneMinute(t *testing.T) {
	var attempts int
	r := newRun(t, func() (kubernetes.Interface, string, error) {
		attempts++
		return nil, "", errors.New("dial tcp 127.0.0.1:26443: connection refused")
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.w.wait = func(_ context.Context, d time.Duration) error {
		r.delays = append(r.delays, d)
		if len(r.delays) == 8 {
			cancel()
			return context.Canceled
		}
		return nil
	}
	if err := r.w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
	sec := func(n int) time.Duration { return time.Duration(n) * time.Second }
	diff(t, []time.Duration{sec(1), sec(2), sec(4), sec(8), sec(16), sec(32), sec(60), sec(60)}, r.delays)
	if attempts != 8 {
		t.Errorf("client built %d times, want 8", attempts)
	}
	c := r.cluster()
	diff(t, [2]*string{ptr("connect: dial tcp 127.0.0.1:26443: connection refused"), ptr(clock.Format(testNow))}, [2]*string{c.LastError, c.LastErrorAt})
	if r.w.Ready() {
		t.Error("watcher reported ready without a connection")
	}
}

func TestSyncTimeoutIsAFailure(t *testing.T) {
	r := newRun(t, nil, kubeSystem())
	r.client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewInternalError(errors.New("etcd unavailable"))
	})
	r.w.syncTimeout = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	r.w.wait = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}
	if err := r.w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
	c := r.cluster()
	if c.LastError == nil || !strings.HasPrefix(*c.LastError, "sync pods: cache sync timed out after 200ms") {
		t.Fatalf("last_error = %v", c.LastError)
	}
}
```

`ptr` comes from `identity_test.go` (Task 3). The stale pod row and the deployment resolution both ride on the lifecycle test so one informer run covers steps 2-5 of the process doc.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/k8s/ -run 'TestWatcher|TestForbidden|TestRunBacks|TestSyncTimeout' -v`
Expected: compile error, `r.w.Run undefined`.

- [ ] **Step 3: Write the lifecycle**

`internal/k8s/sync.go`:

```go
package k8s

import (
	"context"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

type namedInformer struct {
	namespace string
	resource  string
	inf       cache.SharedIndexInformer
	h         cache.ResourceEventHandler
}

func (ni namedInformer) key() string {
	return ni.namespace + "/" + ni.resource
}

// forbiddenSet remembers informers whose list the Role refuses, so the sync
// wait stops expecting them and the error is written once.
type forbiddenSet struct {
	mu   sync.Mutex
	keys map[string]bool
}

func newForbiddenSet() *forbiddenSet {
	return &forbiddenSet{keys: map[string]bool{}}
}

func (f *forbiddenSet) add(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keys[key] {
		return false
	}
	f.keys[key] = true
	return true
}

func (f *forbiddenSet) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys[key]
}

// register attaches the handler and a list/watch error handler; both must
// precede the informer's start.
func (w *Watcher) register(ctx context.Context, ni namedInformer, forbidden *forbiddenSet) error {
	if _, err := ni.inf.AddEventHandler(ni.h); err != nil {
		return err
	}
	return ni.inf.SetWatchErrorHandlerWithContext(func(_ context.Context, _ *cache.Reflector, err error) {
		if !apierrors.IsForbidden(err) {
			w.log.Warn("list/watch failed", "cluster", w.cfg.ClusterID, "namespace", ni.namespace, "resource", ni.resource, "err", err)
			return
		}
		if !forbidden.add(ni.key()) {
			return
		}
		msg := fmt.Sprintf("forbidden: list %s in namespace %s", ni.resource, ni.namespace)
		if err := w.h.ClusterError(ctx, w.cfg.ClusterID, msg); err != nil {
			w.log.Error("record cluster error", "cluster", w.cfg.ClusterID, "err", err)
		}
	})
}

// waitSync returns once every informer has synced or has been refused by
// the Role; a namespace missing one resource must not stall the rest.
func (w *Watcher) waitSync(ctx context.Context, infs []namedInformer, forbidden *forbiddenSet) error {
	deadline := time.NewTimer(w.syncTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		pending := 0
		for _, ni := range infs {
			if !ni.inf.HasSynced() && !forbidden.has(ni.key()) {
				pending++
			}
		}
		if pending == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("cache sync timed out after %s", w.syncTimeout)
		case <-tick.C:
		}
	}
}
```

Append to `internal/k8s/watcher.go` (add the imports `fmt`, `appsv1 "k8s.io/api/apps/v1"`, `batchv1 "k8s.io/api/batch/v1"`, `corev1 "k8s.io/api/core/v1"`, `"k8s.io/apimachinery/pkg/api/meta"`, `"k8s.io/client-go/informers"`):

```go
const maxBackoff = time.Minute

// Run connects, watches and reconciles until ctx ends. A failed attempt is
// written to the cluster row and retried after 1s, doubling to a minute;
// an attempt that reached ready resets the delay.
func (w *Watcher) Run(ctx context.Context) error {
	delay := time.Second
	for {
		err := w.runOnce(ctx)
		if w.ready.Swap(false) {
			delay = time.Second
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.log.Error("watcher failed", "cluster", w.cfg.ClusterID, "context", w.cfg.ContextName, "err", err)
		if rerr := w.h.ClusterError(ctx, w.cfg.ClusterID, err.Error()); rerr != nil {
			w.log.Error("record cluster error", "cluster", w.cfg.ClusterID, "err", rerr)
		}
		if err := w.wait(ctx, delay); err != nil {
			return err
		}
		delay = min(delay*2, maxBackoff)
	}
}

func (w *Watcher) runOnce(ctx context.Context) error {
	client, host, err := w.client()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	identity, err := clusterIdentity(ctx, client)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	if err := w.h.ClusterConnected(ctx, w.cfg.ClusterID, identity, host); err != nil {
		return fmt.Errorf("record connection: %w", err)
	}

	stop := make(chan struct{})
	var factories []informers.SharedInformerFactory
	defer func() {
		close(stop)
		for _, f := range factories {
			f.Shutdown()
		}
	}()
	id := w.cfg.ClusterID
	forbidden := newForbiddenSet()
	resolver := newListerResolver()
	var owners, workloads []namedInformer
	for _, ns := range w.cfg.Namespaces {
		f := informers.NewSharedInformerFactoryWithOptions(client, 0, informers.WithNamespace(ns))
		factories = append(factories, f)
		rs, jobs := f.Apps().V1().ReplicaSets(), f.Batch().V1().Jobs()
		resolver.add(ns, rs.Lister().ReplicaSets(ns), jobs.Lister().Jobs(ns))
		stage := []namedInformer{
			{ns, "replicasets", rs.Informer(), handlerFuncs(w, "ReplicaSet", ns,
				func(o *appsv1.ReplicaSet) error { return w.h.ReplicaSet(ctx, id, o) },
				func(o *appsv1.ReplicaSet) error { return w.h.ReplicaSetDeleted(ctx, string(o.UID)) })},
			{ns, "jobs", jobs.Informer(), handlerFuncs(w, "Job", ns,
				func(o *batchv1.Job) error { return w.h.Job(ctx, id, o) },
				func(o *batchv1.Job) error { return w.h.JobDeleted(ctx, string(o.UID)) })},
		}
		for _, ni := range stage {
			if err := w.register(ctx, ni, forbidden); err != nil {
				return err
			}
		}
		owners = append(owners, stage...)
	}
	// Owner stores fill first; a pod handler that ran before its ReplicaSet
	// was listed would write the fallback workload for every pod on the
	// initial list.
	for _, f := range factories {
		f.Start(stop)
	}
	if err := w.waitSync(ctx, owners, forbidden); err != nil {
		return fmt.Errorf("sync owners: %w", err)
	}
	for i, ns := range w.cfg.Namespaces {
		f := factories[i]
		pods, events := f.Core().V1().Pods(), f.Core().V1().Events()
		stage := []namedInformer{
			{ns, "pods", pods.Informer(), handlerFuncs(w, "Pod", ns,
				func(o *corev1.Pod) error { return w.h.Pod(ctx, id, o, resolver) },
				func(o *corev1.Pod) error { return w.h.PodDeleted(ctx, string(o.UID), deletionSourceWatch) })},
			{ns, "events", events.Informer(), handlerFuncs[*corev1.Event](w, "Event", ns,
				func(o *corev1.Event) error { return w.h.Event(ctx, id, o) }, nil)},
		}
		for _, ni := range stage {
			if err := w.register(ctx, ni, forbidden); err != nil {
				return err
			}
		}
		workloads = append(workloads, stage...)
	}
	for _, f := range factories {
		f.Start(stop)
	}
	if err := w.waitSync(ctx, workloads, forbidden); err != nil {
		return fmt.Errorf("sync pods: %w", err)
	}

	live := map[string]bool{}
	for _, ni := range append(append([]namedInformer{}, owners...), workloads...) {
		if ni.resource != "pods" && ni.resource != "jobs" {
			continue
		}
		for _, obj := range ni.inf.GetStore().List() {
			if m, err := meta.Accessor(obj); err == nil {
				live[string(m.GetUID())] = true
			}
		}
	}
	if err := w.h.Reconcile(ctx, id, w.cfg.Namespaces, func(uid string) bool { return live[uid] }); err != nil {
		// The informers are healthy; a row that cannot be marked is not a
		// reason to reconnect.
		w.log.Error("reconcile", "cluster", id, "err", err)
	}
	w.ready.Store(true)
	<-ctx.Done()
	return ctx.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/k8s/ -race -v`
Expected: PASS. Known places to look if not:
- `handlerFuncs` for `*corev1.Event` needs the explicit type argument because `del` is `nil`.
- `rs.Lister().ReplicaSets(ns)` returns `appslisters.ReplicaSetNamespaceLister`; if the compiler wants the non-namespaced lister, `resolver.add` was declared with the wrong type -- the resolver (Task 4) takes the namespaced one.
- If `TestForbiddenInformerRecordsErrorAndKeepsOthers` times out waiting for ready, check that the reactor's error reaches `SetWatchErrorHandlerWithContext` (it does: the reflector calls the handler on every `ListAndWatch` error) and that `forbidden.add` uses the same key `waitSync` checks.
- If `TestWatcherRecordsPodLifecycle` sees `WorkloadKind = "ReplicaSet"`, the pod informer started before the ReplicaSet store synced: `pods.Informer()` must not be called before the first `Start`.
- If the fake clientset's watch misses the `Update`, ensure the update happens only after `Ready()`; the informer's watch is established by then.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test -race ./... && make ascii`

Commit: `git add internal/k8s/watcher.go internal/k8s/sync.go internal/k8s/watcher_test.go && git commit -m "k8s: add watcher lifecycle with two-stage start, reconcile and backoff"`

---

### Task 6: Wire the watcher into cmd/idios and verify against the OrbStack cluster

**Files:**
- Modify: `cmd/idios/main.go`

**Interfaces:**
- Consumes: `k8s.New`, `k8s.Config`, `k8s.KubeconfigClient`, `k8s.NewSkew`, `k8s.Handler`, `processor.New`, `processor.CaptureRequest`, `store.ListClusters`, `store.ListWatchedNamespaces`, `config.Config.StabilizationWindow`, `config.Config.Kubeconfig`.
- Produces: a binary that watches every `clusters` row over its `watched_namespaces` until SIGINT; `type logSink struct` in `main`.

No automated test: `cmd/idios` is wiring of tested parts, verified by running it. The manual verification below is the check.

- [ ] **Step 1: Replace the body after migration in `cmd/idios/main.go`**

Replace everything from the `logger.Info("idios started", ...)` call through the end of `run` with:

```go
	logger.Info("idios started",
		"version", version,
		"data_dir", cfg.DataDir,
		"db", cfg.DBPath(),
		"artifacts_root", cfg.ArtifactsRoot,
		"schema_version", ver,
		"kubeconfig", cfg.Kubeconfig,
	)

	proc := processor.New(st.Writer, clock.Real{}, logSink{log: logger}, cfg.StabilizationWindow)
	var clusters []store.Cluster
	namespaces := map[int64][]string{}
	err = st.Writer.Tx(ctx, func(tx *sql.Tx) error {
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
		logger.Warn("no clusters configured; nothing to watch")
	}
	var wg sync.WaitGroup
	for _, c := range clusters {
		skew := k8s.NewSkew(clock.Real{}, logger.With("cluster", c.ID))
		w := k8s.New(k8s.Config{ClusterID: c.ID, ContextName: c.ContextName, Namespaces: namespaces[c.ID]},
			k8s.KubeconfigClient(cfg.Kubeconfig, c.ContextName, skew), proc, logger)
		logger.Info("watching", "cluster", c.ID, "context", c.ContextName, "namespaces", namespaces[c.ID])
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("watcher stopped", "cluster", c.ID, "err", err)
			}
		}()
	}

	<-ctx.Done()
	wg.Wait()
	if !errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	logger.Info("idios stopping")
	return nil
}

var _ k8s.Handler = (*processor.Processor)(nil)

// logSink stands in for the capture pool until it exists: requests are
// logged and dropped.
type logSink struct{ log *slog.Logger }

func (s logSink) Enqueue(r processor.CaptureRequest) {
	s.log.Info("capture request", "pod", r.PodName, "container", r.Container, "kind", r.Kind, "trigger", r.Trigger)
}
```

Add to the import block: `"database/sql"`, `"sync"`, `"idios/internal/k8s"`, `"idios/internal/processor"`. Keep the existing imports.

- [ ] **Step 2: Build and run the test suite**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`
Expected: green. The archtest still passes: `cmd` is allowed to import everything.

- [ ] **Step 3: Manual verification against OrbStack (orchestrator; skip only if the API is unreachable after `orb start`)**

```bash
KUBECONFIG=./kube/config kubectl get ns idios-smoke
rm -rf .storage && mkdir -p .storage
go build -o bin/idios ./cmd/idios
./bin/idios & sleep 3; kill -INT %1; wait
sqlite3 .storage/idios.db "INSERT INTO clusters (name, context_name, api_server_url, first_seen_at) VALUES ('orbstack', 'orbstack', '', strftime('%Y-%m-%dT%H:%M:%f000Z','now'));
INSERT INTO watched_namespaces (cluster_id, name, added_at) VALUES (1, 'idios-smoke', strftime('%Y-%m-%dT%H:%M:%f000Z','now'));"
./bin/idios & sleep 20; kill -INT %1; wait
sqlite3 -header .storage/idios.db "SELECT id, identity, api_server_url, last_connected_at, last_error FROM clusters; SELECT COUNT(*) AS pods FROM pods; SELECT namespace, name, workload_kind, workload_name, deleted_at FROM pods LIMIT 10;"
```

Expected: `identity` is the `kube-system` uid (`kubectl get ns kube-system -o jsonpath='{.metadata.uid}'` matches), `api_server_url` is `https://127.0.0.1:26443`, `last_error` is NULL, and every pod currently in `idios-smoke` has a row. Note what was seen in the commit body only if something had to change; otherwise no body.

If `idios-smoke` has no pods, `kubectl -n idios-smoke run smoke-sleep --image=busybox:1.36 --restart=Never -- sleep 300` before the second run and delete it after; touch nothing outside `idios-smoke`.

- [ ] **Step 4: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add cmd/idios/main.go && git commit -m "cmd: start one watcher per cluster row"`

---

### Task 7: Roadmap status and handoff

**Files:**
- Modify: `docs/plans/m1-recorder/roadmap.md` (Phase 4 section)

- [ ] **Step 1: Record the status**

Below the `### Phase 4: k8s watcher` heading, before "Delivers:", insert:

```markdown
Status: done, merged to `main` 2026-08-27 (`<merge commit>`). Plan:
`04-k8s-watcher.md`.
```

The orchestrator fills `<merge commit>` after the fast-forward (edit and commit the roadmap on `main` after the merge, or use the branch head hash that becomes `main`).

- [ ] **Step 2: Record what Phase 4 hands on**

Append to the Phase 4 section, after "Depends on: Phase 3.":

```markdown
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
```

Also fix the roadmap's Phase 4 "Tests:" line to name the actual tests: informer integration (`TestWatcherRecordsPodLifecycle`, forbidden informer, backoff, sync timeout) plus unit tests for skew, identity, `DeletedFinalStateUnknown` unwrap and the lister resolver.

- [ ] **Step 3: Checkpoint**

Run: `make ascii`

Commit: `git add docs/plans/m1-recorder/roadmap.md && git commit -m "docs: record phase 4 completion and handoff in roadmap"`

---

## Self-review

**Spec coverage.**
- Process 2 dependency rules: `k8s` imports `client-go`, `ingest`, `clock`; not `store` (archtest unchanged, Task 5 build proves it). `Handler` is the "small handler interface" (Task 4).
- Process 4 handlers inline, 4.1 step 1 (`old` ignored): `handlerFuncs` `UpdateFunc` drops `old` (Task 4). 4.2 owner resolution via listers and start order: `listerResolver` (Task 4), two-stage start (Task 5), asserted by `WorkloadKind = Deployment` in the lifecycle test. 4.3 `DeletedFinalStateUnknown` unwrap (Task 4 test). 4.4 events to `Handler.Event` (Task 5).
- Process 5 steps 1-6: `KubeconfigClient` + skew (Task 3), identity with `IsForbidden` fallback (Task 3), factories per namespace with resync 0 (Task 5), two-stage start and sync with 30 s timeout (Task 5), reconcile with `reconcile` / `unwatched` (Task 2, Task 5), ready flag (Task 4/5). Failure handling: `last_error` + backoff 1 s to 60 s (Task 5 tests), per-informer forbidden (Task 5 test), client-go's own reconnects untouched. Config changes: Phase 7, noted in handoff.
- Process 9 skew: `Skew` (Task 3), warn once per hour above 5 min, nothing corrected.
- Process 10 error handling: logged with `cluster`, `namespace`, `kind`, `uid`, `name`, never propagated (Task 4). Counters and panic recovery: Phase 7, in handoff.
- Process 14.4: fake clientset + real informers + real processor + temp SQLite, scripted create/update/delete, `DeletedFinalStateUnknown` case (Tasks 4, 5).
- Client-go 3.1 identity, 3.3 listers, 5 (resync off, `UpdateFunc` noise handled by the snapshot diff in Phase 3): covered.
- Storage 4 reconciliation: Task 2 test. 5.1 `last_error` cleared: Task 1 test. 5.2 namespaces per cluster: Task 1 test.

**Placeholder scan.** Every code step has full code. `<merge commit>` in Task 7 is filled by the orchestrator at merge time and is the only value not known while planning.

**Type consistency.** `Handler` method signatures (Task 4) match `processor.Processor` methods as they exist (`Pod(ctx, clusterID, pod, resolver)`, `PodDeleted(ctx, uid, source)`, `Job`, `JobDeleted(ctx, uid)`, `ReplicaSet`, `ReplicaSetDeleted(ctx, uid)`, `Event(ctx, clusterID, ev)`) plus Task 2's `ClusterConnected(ctx, clusterID, identity *string, apiServerURL string)`, `ClusterError(ctx, clusterID, msg)`, `Reconcile(ctx, clusterID, watched []string, live func(string) bool)`. `resolver.add` takes namespaced listers in Task 4 and receives `rs.Lister().ReplicaSets(ns)` / `jobs.Lister().Jobs(ns)` in Task 5. `ClientFunc` returns `(kubernetes.Interface, string, error)` in Task 3, consumed by `New` (Task 4) and `runOnce` (Task 5) and produced by `KubeconfigClient` (Task 3) and the test closures (Task 5). `store.LiveObject{UID, Namespace}` (Task 1) is what `Reconcile` iterates (Task 2). `syncTimeout`, `wait`, `ready` fields (Task 4) are what Task 5's tests set and `Run` reads.

**`.ai` rules by name.** ascii-only: plan and code are ASCII. tests: every test has a trace above; variants are table rows; assertions compare whole rows or exact sequences. comments: only reasons and doc comments. code-is-truth: no code comment names a document. scope: `Skew.Offset` and `Watcher.Ready` are named seams for Phase 7 in the handoff; nothing else beyond the tasks. commits: one per task, `area: subject`, no trailers, named paths.
