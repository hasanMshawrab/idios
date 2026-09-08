# Phase 5: Capture Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Read `.ai/*.md` before writing any code; those rules override habits.

**Goal:** One package, `internal/capture`, whose `Pool` takes the `processor.CaptureRequest`s the processor emits after commit and turns each into a file under `artifacts_root` plus one `artifacts` row: `GetLogs` off the informer goroutine through a bounded per-cluster queue with a drop counter, `TailLines + 1` / `LimitBytes + 1` truncation detection, the kubelet 200-with-error-body check, `capture_gap` mapping, temp file then rename, file before row; an in-memory early-capture cache (Unhealthy debounced, Killing/Evicted/Preempted/Preempting once per container, 30 minute expiry, 500-pod LRU) consulted by the terminated and delete paths when their own call got nothing; `pod_json` from the pod the request carries. Plus the one store helper it needs, a clientset accessor on `k8s.Watcher`, and `cmd/idios` wiring that replaces `logSink`.

**Architecture:** `capture` imports `processor` (for `CaptureRequest`, `CaptureSink` and the trigger constants), `store`, `clock` and `client-go` (for `corev1.PodLogOptions`, `apierrors` and the `kubernetes.Interface` adapter). It does not import `ingest` (archtest) or `k8s`. The pool never calls a clientset directly: it reads logs through a `LogSource` interface with one production adapter (`ClientLogs`, built on `kubernetes.Interface`) and a recording fake in tests, and writes rows through a `TxRunner` interface satisfied by `*store.Writer` so a test can check the file exists at the moment the row is written. One `Pool` per process holds one queue and one worker set per registered cluster (requests route on `ClusterID`) and one shared early-capture cache keyed by `pod_uid`. `k8s.Watcher` grows `Client()` returning the clientset of the current connection, so the pool reads through whatever the watcher last connected with and needs no kubeconfig of its own.

**Tech Stack:** Go 1.24, `k8s.io/client-go` v0.34.1 (already a dependency), `k8s.io/api` / `apimachinery` v0.34.1, `modernc.org/sqlite` v1.45.0, `github.com/google/go-cmp`.

**Spec:** `docs/design/process-architecture.md` Sections 2 (dependency rules), 6.1 (queue and workers, worker steps, dedup), 6.2 (early capture), 10 (file errors recorded in the row), 12 (file modes), 13 (`log_tail_lines`, `log_max_bytes`, `capture_workers_per_cluster`, `capture_queue_size`, `early_capture_debounce`), 14.5 (capture pool tests); `docs/design/data-storage.md` Sections 4 (previous flag, dead-instance index, size limits enforced by the ingester), 5.11 (`artifacts`, the 200-error rule), 6.2 rules 4-6, 7 (file layout, write order); `docs/design/client-go-methods.md` Section 3.11. Roadmap: `docs/plans/m1-recorder/roadmap.md` (Phase 5 section, "Phase 3 hands Phase 5" in the Phase 3 section, "Phase 4 hands Phase 5" in the Phase 4 section). The plan may cite these; the code must not (`.ai/code-is-truth.md`).

## Global Constraints

- `.ai/ascii-only.md`, `.ai/tests.md`, `.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`, `.ai/commits.md` apply to every line. Every exported identifier gets a one-line doc comment even where a block below omits it. Code blocks contain no comments beyond those shown.
- Every task ends with `go build ./... && go vet ./... && go test ./... && make ascii` green, then one commit per `.ai/commits.md` (`area: subject`, no trailers, `git add` named paths). Work happens on branch `phase-5-capture-pool`; the orchestrator fast-forwards `main` onto it locally at the end and deletes the branch. No remote, no push.
- Module path `idios`. No new dependencies. `k8s.io/*` stay at v0.34.1.
- Dependency rules (archtest): `capture` never imports `internal/ingest`; `processor`, `ingest`, `incident` never import `capture` or `client-go`; `k8s` never imports `store`. Test files are exempt.
- Every timestamp written goes through `clock.Format`; process time is `clock.Clock.Now()` injected. `captured_at` is process time.
- No schema change. `0001_init.sql` is the contract; its `artifacts` CHECK `(file_path IS NULL) = (capture_gap IS NOT NULL)` is what every row written here must satisfy. If a task appears to need a migration, stop and report.
- Store SQL for this phase lives only in `internal/store/capture_sql.go`. `ingest_sql.go`, `cluster_sql.go` and `rows.go` are not edited. `store.Artifact`, `store.Artifact*` kinds, `store.Gap*` and `store.NoRestartIndex` already exist and are used as they are.
- Files `0600`, directories `0700`, temp files created under `<artifacts_root>/tmp/` with mode `0600` before rename.
- Counters beyond the drop counter, `idios status`, panic recovery, config reload (adding a cluster to a running pool) and the sweeper's orphan pass are Phase 6/7. Not built here.
- No cluster is needed. Automated tests use a fake `LogSource`. The manual check in Task 7 uses `KUBECONFIG=./kube/config`, namespace `idios-smoke` only.

## Decisions made here (the spec or roadmap leaves them open)

- **`capture` imports `processor`.** The roadmap left the direction open. `processor.CaptureRequest` already exists with the exact fields the pool needs (`IncidentID`, `RestartCount int64`, `Previous`, `Trigger`, `Pod`); a second struct in `capture` would be a copy kept in step by hand. `*capture.Pool` satisfies `processor.CaptureSink` directly and `cmd/idios` passes it to `processor.New`. `processor` still does not import `capture`, so there is no cycle, and `capture` still does not import `ingest` directly.
- **One `Pool`, one queue and worker set per cluster.** Workers need a per-cluster clientset, so the queue is per cluster too (`capture_queue_size` each, `capture_workers_per_cluster` workers each). `Enqueue` routes on `ClusterID`; an unknown cluster or a full queue drops the request and bumps one counter, `Dropped()`, which Phase 7's status reads. Clusters are registered with `AddCluster` before `Run`; a cluster added later is Phase 7's reload work and is listed in the handoff.
- **`LogSource` is the client seam; `k8s.Watcher.Client()` is where the clientset comes from.** `capture.ClientLogs(get func() kubernetes.Interface)` calls `get()` per request; `cmd/idios` passes the watcher's `Client` method. Before the first connection `Client()` is nil and the adapter returns an error, which the worker records as `capture_gap = 'unknown'` with the note `cluster not connected`; that is the truthful row for a request that arrived before the cluster was reachable. Building the clientset once in `cmd/idios` was rejected: a kubeconfig unreachable at startup would leave the pool without a client forever while the watcher keeps retrying.
- **The body is read into memory, not streamed to disk.** The server caps it at `log_max_bytes + 1` (256 KiB + 1 by default) and the early-capture cache must hold bytes anyway. In memory, the single-line kubelet error check, the byte drop and the line drop are three slice operations before one file write.
- **Truncation.** `LimitBytes` stops the server-side stream, so a body of `log_max_bytes + 1` bytes has its last byte dropped. `TailLines` selects the newest lines, so a body with `log_tail_lines + 1` lines has its oldest (first) line dropped. Bytes are applied first, then lines, matching the order the server applied them. A line is a `\n`-terminated run; a final run without `\n` counts as a line.
- **Kubelet error prefixes** are `unable to retrieve container logs for` and `failed to try resolving symlinks`, matched only when the body is a single line.
- **`capture_gap` mapping** for the `GetLogs` error: `IsNotFound` -> `pod_deleted`; `IsBadRequest` with `Previous = true` -> `no_previous_run`; `IsForbidden` -> `forbidden`; any other error, including `IsBadRequest` with `Previous = false` (a waiting container has no current instance) -> `unknown`. An empty body -> `no_output`. `capture_note` is the error's `Error()` text, or the kubelet line. A file write error is a gap `unknown` with the OS error as the note, so the row says what the disk did.
- **The early cache holds one entry per (pod, container).** The processor emits one early request per container of the pod on a pod-level event, so a per-pod debounce would let the first container through and block its siblings. Debounce and "once" are per container; expiry and the LRU bound are per pod. `Unhealthy` is debounced by `early_capture_debounce` (60 s default) from the last capture for that container. `Killing`, `Evicted`, `Preempted`, `Preempting` bypass the debounce, once per container: the entry is marked final and later early requests for it are ignored, including `Unhealthy` ones. An early `GetLogs` that fails or returns nothing leaves the cache as it was.
- **Consulting the cache.** Only `log_previous` requests (trigger `restart`) and `log_current` requests with trigger `delete` consult it, and only when their own fetch produced a gap. The entry is taken (removed) either way. The held bytes are written as the artifact with `captured_early = 1` only when `store.HasRecentIncidentOrFailure(podUID, now - stabilization_window)` is true; the processor already applies that gate before emitting `delete` requests, and applying it here as well covers the `restart` path with one rule in one place. When the gate says no, the normal gap row is written and the copy is dropped.
- **The `delete` request's own fetch is still attempted.** A watch delete may arrive while the pod object still exists (graceful termination), in which case `GetLogs` works and is the better copy.
- **Test seam for write order.** `TxRunner` (`Tx(ctx, func(*sql.Tx) error) error`) is satisfied by `*store.Writer`; a test wraps the real writer and checks the file at the moment `Tx` is called. `.ai/scope.md` rule 1: the interface is named here and has its production implementation today.
- **Store helper: one upsert.** `UpsertArtifact` replaces the row on the unique key with one exception: a row that has a file is not replaced by a later gap for the same key. A retry that fails must not turn a captured log into a gap row pointing at an orphan. `log_current` and `pod_json` are overwritten by every new file, as the process doc says.

## File structure

```
internal/store/capture_sql.go            UpsertArtifact
internal/store/capture_sql_test.go
internal/capture/source.go               LogSource, ClientLogs, TxRunner
internal/capture/early.go                earlyCache, held
internal/capture/early_test.go
internal/capture/worker.go               process, fetch, trim, kubeletError, relPath, writeFile, captureLog, capturePodJSON, captureEarly
internal/capture/worker_test.go          harness, fakeSource, checkingWriter, 14.5 table, pod_json, early copy on delete
internal/capture/pool.go                 Config, Pool, New, AddCluster, Enqueue, Run, Dropped
internal/capture/pool_test.go            queue full drops, workers drain
internal/k8s/watcher.go                  Client accessor
internal/k8s/watcher_test.go             one assertion in the backoff test
cmd/idios/main.go                        pool wiring, logSink removed
docs/plans/m1-recorder/roadmap.md      Phase 5 status and handoff
```

Every test below has a one-line trace to a spec statement. No trace, no test.

---

### Task 1: Store artifact upsert

**Files:**
- Create: `internal/store/capture_sql.go`
- Test: `internal/store/capture_sql_test.go`

**Interfaces:**
- Consumes: `Artifact` from `rows.go`; test helpers `openMigratedStore`, `insertCluster`, `insertPod`, `inTx`, `ptr`, `testEpoch`.
- Produces: `func UpsertArtifact(ctx context.Context, tx *sql.Tx, a Artifact) error`.

Tests and their trace:
- `TestArtifactUpsertReplacesFileRowsAndKeepsFileOverGap`: process 6.1 "a second request for the same instance is an upsert of the same row; the file is overwritten"; storage 5.11 CHECK `(file_path IS NULL) = (capture_gap IS NOT NULL)` (a gap after a file must not produce a half-row, and a file after a gap must clear the gap columns).

- [ ] **Step 1: Write the failing test**

`internal/store/capture_sql_test.go`:

```go
package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"idios/internal/clock"
)

func loadArtifacts(t *testing.T, s *Store) []Artifact {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), `
SELECT id, pod_uid, incident_id, container_name, kind, restart_count, file_path, size_bytes, truncated, captured_early, capture_gap, capture_note, captured_at
FROM artifacts ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.PodUID, &a.IncidentID, &a.ContainerName, &a.Kind, &a.RestartCount, &a.FilePath, &a.SizeBytes, &a.Truncated, &a.CapturedEarly, &a.CaptureGap, &a.CaptureNote, &a.CapturedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestArtifactUpsertReplacesFileRowsAndKeepsFileOverGap(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	ts := clock.Format(testEpoch)
	key := Artifact{PodUID: "p1", ContainerName: "api", Kind: ArtifactLogPrevious, RestartCount: 1}
	gap := key
	gap.CaptureGap, gap.CaptureNote, gap.CapturedAt = ptr(GapNoOutput), nil, ts
	file := key
	file.FilePath, file.SizeBytes, file.Truncated, file.CapturedAt = ptr("1/idios-smoke/p1/api/restart_001.log"), 120, true, "2026-08-27T12:01:00.000000Z"
	bigger := file
	bigger.SizeBytes, bigger.Truncated, bigger.CapturedEarly, bigger.CapturedAt = 300, false, true, "2026-08-27T12:02:00.000000Z"
	laterGap := gap
	laterGap.CaptureGap, laterGap.CaptureNote, laterGap.CapturedAt = ptr(GapPodDeleted), ptr(`pods "pod-p1" not found`), "2026-08-27T12:03:00.000000Z"

	steps := []struct {
		name string
		in   Artifact
		want Artifact
	}{
		{"gap row is written", gap, gap},
		{"file replaces gap", file, file},
		{"newer file replaces file", bigger, bigger},
		{"later gap leaves the file row", laterGap, bigger},
	}
	for _, st := range steps {
		inTx(t, s, func(tx *sql.Tx) error { return UpsertArtifact(context.Background(), tx, st.in) })
		got := loadArtifacts(t, s)
		if len(got) != 1 {
			t.Fatalf("%s: %d rows, want 1", st.name, len(got))
		}
		got[0].ID = 0
		if d := cmp.Diff(st.want, got[0]); d != "" {
			t.Fatalf("%s: %s", st.name, d)
		}
	}
}
```

No incident row is seeded, so every `IncidentID` stays nil; the FK would otherwise fail.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestArtifactUpsert -v`
Expected: compile error, `undefined: UpsertArtifact`.

- [ ] **Step 3: Write the helper**

`internal/store/capture_sql.go`:

```go
package store

import (
	"context"
	"database/sql"
)

// UpsertArtifact writes the row for one capture key. A later capture with a
// file replaces whatever is there; a later gap keeps an existing file row,
// because a failed retry must not turn a captured log into a gap pointing
// at an orphan file.
func UpsertArtifact(ctx context.Context, tx *sql.Tx, a Artifact) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO artifacts (pod_uid, incident_id, container_name, kind, restart_count, file_path, size_bytes, truncated, captured_early, capture_gap, capture_note, captured_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (pod_uid, container_name, kind, restart_count) DO UPDATE SET
    incident_id = COALESCE(excluded.incident_id, artifacts.incident_id),
    file_path = excluded.file_path, size_bytes = excluded.size_bytes, truncated = excluded.truncated,
    captured_early = excluded.captured_early, capture_gap = excluded.capture_gap, capture_note = excluded.capture_note,
    captured_at = excluded.captured_at
WHERE excluded.file_path IS NOT NULL OR artifacts.file_path IS NULL`,
		a.PodUID, a.IncidentID, a.ContainerName, a.Kind, a.RestartCount, a.FilePath, a.SizeBytes, boolInt(a.Truncated), boolInt(a.CapturedEarly), a.CaptureGap, a.CaptureNote, a.CapturedAt)
	return err
}
```

`boolInt` exists in `ingest_sql.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestArtifactUpsert -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/store/capture_sql.go internal/store/capture_sql_test.go && git commit -m "store: add artifact upsert that keeps a file over a later gap"`

---

### Task 2: Log source, tx runner, early-capture cache

**Files:**
- Create: `internal/capture/source.go`, `internal/capture/early.go`
- Test: `internal/capture/early_test.go`

**Interfaces:**
- Consumes: `kubernetes.Interface`, `corev1.PodLogOptions`.
- Produces: `type LogSource interface{ Logs(ctx context.Context, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error) }`; `func ClientLogs(get func() kubernetes.Interface) LogSource`; `var errNotConnected`; `type TxRunner interface{ Tx(ctx context.Context, fn func(*sql.Tx) error) error }`; `const earlyExpiry = 30 * time.Minute`, `earlyMaxPods = 500`, `reasonUnhealthy = "Unhealthy"`; `type held struct{ body []byte; truncated bool; at time.Time; final bool }`; `type earlyCache struct`; `func newEarlyCache(debounce time.Duration) *earlyCache`; `func (c *earlyCache) wants(podUID, container, reason string, now time.Time) bool`; `func (c *earlyCache) put(podUID, container, reason string, body []byte, truncated bool, now time.Time)`; `func (c *earlyCache) take(podUID, container string) (held, bool)`; `func (c *earlyCache) len() int`.

Tests and their trace:
- `TestEarlyCacheDebouncesUnhealthyAndTakesFinalReasonsOnce`: process 6.2 "`Unhealthy`: debounced to one capture per pod per 60 seconds" and "`Killing`, `Evicted`, `Preempted`, `Preempting`: bypass the debounce, once per pod"; client-go 3.11 "The debounced copy from a probe flap a minute ago is a picture of the process working; the copy taken on `Killing` is the last one anyone will get" (a later `Unhealthy` does not overwrite a final copy); the per-container decision from this plan (a sibling container is not blocked).
- `TestEarlyCacheExpiresAndBoundsPods`: process 6.2 "Entries older than 30 minutes with no delete observed are dropped" and "a cap of 500 pods with LRU eviction".

`ClientLogs` is a two-line adapter over client-go exercised by the manual check in Task 7.

- [ ] **Step 1: Write the failing tests**

`internal/capture/early_test.go`:

```go
package capture

import (
	"fmt"
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func TestEarlyCacheDebouncesUnhealthyAndTakesFinalReasonsOnce(t *testing.T) {
	c := newEarlyCache(60 * time.Second)
	steps := []struct {
		name      string
		at        time.Duration
		container string
		reason    string
		want      bool
	}{
		{"first unhealthy captures", 0, "api", "Unhealthy", true},
		{"unhealthy inside the window waits", 30 * time.Second, "api", "Unhealthy", false},
		{"sibling container is not blocked", 30 * time.Second, "sidecar", "Unhealthy", true},
		{"unhealthy at the window captures", 60 * time.Second, "api", "Unhealthy", true},
		{"killing bypasses the window", 61 * time.Second, "api", "Killing", true},
		{"second killing is ignored", 62 * time.Second, "api", "Killing", false},
		{"evicted after killing is ignored", 63 * time.Second, "api", "Evicted", false},
		{"unhealthy after killing is ignored", 200 * time.Second, "api", "Unhealthy", false},
	}
	for _, st := range steps {
		now := testNow.Add(st.at)
		got := c.wants("p1", st.container, st.reason, now)
		if got != st.want {
			t.Errorf("%s: wants = %v, want %v", st.name, got, st.want)
		}
		if got {
			c.put("p1", st.container, st.reason, []byte(st.name), false, now)
		}
	}
	h, ok := c.take("p1", "api")
	if !ok || string(h.body) != "killing bypasses the window" || !h.final {
		t.Fatalf("api copy = %+v, %v; want the Killing copy, final", h, ok)
	}
	if _, ok := c.take("p1", "api"); ok {
		t.Fatal("take returned the api copy twice")
	}
	if !c.wants("p1", "api", "Unhealthy", testNow.Add(300*time.Second)) {
		t.Fatal("a taken container does not accept a new capture")
	}
}

func TestEarlyCacheExpiresAndBoundsPods(t *testing.T) {
	c := newEarlyCache(time.Minute)
	for i := 0; i < earlyMaxPods; i++ {
		c.put(fmt.Sprintf("p%03d", i), "api", "Killing", []byte("x"), false, testNow.Add(time.Duration(i)*time.Second))
	}
	c.put("newest", "api", "Killing", []byte("x"), false, testNow.Add(earlyMaxPods*time.Second))
	if got := c.len(); got != earlyMaxPods {
		t.Fatalf("pods held = %d, want %d", got, earlyMaxPods)
	}
	if _, ok := c.take("p000", "api"); ok {
		t.Fatal("the least recently written pod survived the bound")
	}
	if _, ok := c.take("p001", "api"); !ok {
		t.Fatal("the second oldest pod was evicted")
	}

	c.put("late", "api", "Killing", []byte("x"), false, testNow.Add(earlyExpiry+earlyMaxPods*time.Second))
	if got := c.len(); got != 2 {
		t.Fatalf("pods held after expiry = %d, want 2 (newest, late)", got)
	}
	if _, ok := c.take("newest", "api"); !ok {
		t.Fatal("a pod written inside the expiry window was dropped")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capture/ -run TestEarlyCache -v`
Expected: compile error, `undefined: newEarlyCache`.

- [ ] **Step 3: Write the code**

`internal/capture/source.go`:

```go
// Package capture turns capture requests into files under the artifacts root
// and artifacts rows, off the informer goroutines. It owns the queue, the
// GetLogs call and its truncation and error mapping, the file layout, and
// the early-capture cache; what to capture is decided upstream.
package capture

import (
	"context"
	"database/sql"
	"errors"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// LogSource reads one container log stream. The pool never touches a
// clientset directly so tests can script every response.
type LogSource interface {
	Logs(ctx context.Context, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error)
}

// TxRunner runs one write transaction; *store.Writer is the implementation.
type TxRunner interface {
	Tx(ctx context.Context, fn func(*sql.Tx) error) error
}

var errNotConnected = errors.New("cluster not connected")

type clientLogs struct {
	get func() kubernetes.Interface
}

// ClientLogs reads logs through the clientset get returns at call time, so a
// cluster that reconnects with a new clientset is picked up without a
// restart. A nil clientset means no connection yet.
func ClientLogs(get func() kubernetes.Interface) LogSource {
	return clientLogs{get: get}
}

func (c clientLogs) Logs(ctx context.Context, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	client := c.get()
	if client == nil {
		return nil, errNotConnected
	}
	return client.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(ctx)
}
```

`internal/capture/early.go`:

```go
package capture

import (
	"sync"
	"time"
)

const (
	earlyExpiry     = 30 * time.Minute
	earlyMaxPods    = 500
	reasonUnhealthy = "Unhealthy"
)

// held is one early copy of a container's live log and the time it was
// taken. final marks a copy taken on a death announcement; nothing later
// replaces it because nothing later exists.
type held struct {
	body      []byte
	truncated bool
	at        time.Time
	final     bool
}

type podEntry struct {
	containers map[string]*held
	touched    time.Time
}

// earlyCache holds early copies keyed by pod uid then container. Debounce
// and "once" are per container because a pod-level event produces one
// request per container at the same instant; expiry and the size bound are
// per pod. Entries survive the pod's deletion: that is when they matter.
type earlyCache struct {
	debounce time.Duration

	mu   sync.Mutex
	pods map[string]*podEntry
}

func newEarlyCache(debounce time.Duration) *earlyCache {
	return &earlyCache{debounce: debounce, pods: map[string]*podEntry{}}
}

// wants says whether an early request for the container should fetch now.
func (c *earlyCache) wants(podUID, container, reason string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		return true
	}
	h := e.containers[container]
	if h == nil {
		return true
	}
	if h.final {
		return false
	}
	if reason != reasonUnhealthy {
		return true
	}
	return now.Sub(h.at) >= c.debounce
}

// put stores a copy, then drops pods untouched for earlyExpiry and the
// least recently touched pods above earlyMaxPods.
func (c *earlyCache) put(podUID, container, reason string, body []byte, truncated bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		e = &podEntry{containers: map[string]*held{}}
		c.pods[podUID] = e
	}
	e.containers[container] = &held{body: body, truncated: truncated, at: now, final: reason != reasonUnhealthy}
	e.touched = now
	for uid, p := range c.pods {
		if now.Sub(p.touched) >= earlyExpiry {
			delete(c.pods, uid)
		}
	}
	// The bound is small enough that a scan per insert costs less than
	// keeping an ordered list in step with every put and take.
	for len(c.pods) > earlyMaxPods {
		var oldest string
		for uid, p := range c.pods {
			if oldest == "" || p.touched.Before(c.pods[oldest].touched) {
				oldest = uid
			}
		}
		delete(c.pods, oldest)
	}
}

// take removes and returns the container's copy.
func (c *earlyCache) take(podUID, container string) (held, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		return held{}, false
	}
	h := e.containers[container]
	if h == nil {
		return held{}, false
	}
	delete(e.containers, container)
	if len(e.containers) == 0 {
		delete(c.pods, podUID)
	}
	return *h, true
}

func (c *earlyCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pods)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capture/ -run TestEarlyCache -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

The archtest must pass: `internal/capture` imports no `idios/internal/ingest`. `go vet` may report `errNotConnected` and `TxRunner` as unused until Task 3; if `golangci-lint` is run it will too, but the checkpoint command does not run it. If `go build` complains about an unused import, none exists in the code above; recheck the copy.

Commit: `git add internal/capture/source.go internal/capture/early.go internal/capture/early_test.go && git commit -m "capture: add log source seam and early-capture cache"`

---

### Task 3: Worker: fetch, trim, files, rows, early path

**Files:**
- Create: `internal/capture/worker.go`, `internal/capture/pool.go` (the `Config` and `Pool` struct only; `AddCluster`, `Enqueue`, `Run`, `Dropped` arrive in Task 4)
- Test: `internal/capture/worker_test.go`

**Interfaces:**
- Consumes: `LogSource`, `TxRunner`, `earlyCache` (Task 2); `store.UpsertArtifact` (Task 1); `store.HasRecentIncidentOrFailure`, `store.Artifact`, `store.ArtifactLogPrevious`, `store.ArtifactLogCurrent`, `store.ArtifactPodJSON`, `store.Gap*`, `store.NoRestartIndex`; `processor.CaptureRequest`, `processor.TriggerRestart`, `processor.TriggerDelete`, `processor.TriggerEarlyPrefix`; `clock.Clock`, `clock.Format`.
- Produces: `type Config struct{ Root string; TailLines int; MaxBytes int64; Workers int; QueueSize int; EarlyDebounce time.Duration; StabilizationWindow time.Duration }`; `type Pool struct` with fields `cfg Config; w TxRunner; clk clock.Clock; log *slog.Logger; cache *earlyCache; mu sync.Mutex; clusters map[int64]*clusterQueue; dropped atomic.Uint64`; `type clusterQueue struct{ src LogSource; ch chan processor.CaptureRequest }`; `func New(cfg Config, w TxRunner, clk clock.Clock, log *slog.Logger) *Pool`; `const tmpDir = "tmp"`; `var kubeletErrorPrefixes`; `type fetched struct{ body []byte; truncated bool; gap *string; note *string }`; `func (p *Pool) process(ctx context.Context, src LogSource, r processor.CaptureRequest)`; `func (p *Pool) fetch(ctx, src, r) fetched`; `func gapFor(err error, previous bool) fetched`; `func trim(body []byte, tail int, maxBytes int64) fetched`; `func lines(b []byte) int`; `func kubeletError(body []byte) (string, bool)`; `func relPath(r processor.CaptureRequest) string`; `func (p *Pool) writeFile(rel string, data []byte) error`; `func (p *Pool) captureLog(ctx, src, r)`; `func (p *Pool) capturePodJSON(ctx, r)`; `func (p *Pool) captureEarly(ctx, src, r)`; `func (p *Pool) writeRow(ctx, a store.Artifact)`; `func (p *Pool) worthKeeping(ctx, podUID string) (bool, error)`; `func ptr[T any](v T) *T`.

Tests and their trace:
- `TestLogCaptureWritesFileThenRow`: process 14.5, one row per bullet: content; exactly `log_tail_lines + 1` lines (`truncated = 1`, stored line count); empty; 200 with a kubelet error line; `IsNotFound`; `IsBadRequest` on `Previous = true`; `IsForbidden`; plus `log_max_bytes + 1` bytes (client-go 3.11 "`LimitBytes` truncation is detected the same way") and `IsBadRequest` on `Previous = false` (this plan's mapping to `unknown`). Asserts the file, the row, `capture_gap`, `capture_note`, the `PodLogOptions` the fake received (`Previous`, `TailLines`, `LimitBytes`, `Container`), the storage 7 path, and that the file exists when the row is written (storage 7 "file first, then the `artifacts` row").
- `TestPodJSONCarriesTypeMetaAndIsOverwritten`: client-go 3.11 "set `TypeMeta` (`Kind: "Pod"`, `APIVersion: "v1"`, which informers leave empty) and `json.Marshal` it to `pod.json`"; process 6.1 "`log_current` and `pod_json` are meant to be overwritten"; a request without a pod writes nothing.
- `TestDeleteAndRestartUseEarlyCopyOnlyForPodsWorthKeeping`: process 14.1 "delete of a healthy pod drops the cached copy, delete of a pod with an incident writes it"; storage 6.2 rule 6 "only when the normal capture path fails and the pod is worth keeping"; process 6.2 "consults the cache only after its own `GetLogs` failed or returned empty" (a successful fetch leaves the copy unused); an `incident_open` request never consults it.
- `TestEarlyRequestFillsCacheWithoutARow`: storage 6.2 rule 6 "capture `log_current` immediately into an in-memory early-capture cache" (no `artifacts` row, `Previous = false`, a failed fetch leaves the cache empty).

- [ ] **Step 1: Write the failing tests**

`internal/capture/worker_test.go`:

```go
package capture

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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"idios/internal/clock"
	"idios/internal/processor"
	"idios/internal/store"
)

const (
	testTail  = 2
	testBytes = int64(8)
)

type call struct {
	Namespace string
	Pod       string
	Opts      corev1.PodLogOptions
}

type fakeSource struct {
	mu      sync.Mutex
	calls   []call
	respond func(c call) (string, error)
}

func (f *fakeSource) Logs(_ context.Context, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	c := call{Namespace: namespace, Pod: pod, Opts: *opts}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	body, err := f.respond(c)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func (f *fakeSource) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// checkingWriter records whether the artifact file existed when the row
// was written.
type checkingWriter struct {
	w    *store.Writer
	root string

	mu           sync.Mutex
	fileAtRow    []bool
	pendingCheck string
}

func (c *checkingWriter) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	c.mu.Lock()
	if c.pendingCheck != "" {
		_, err := os.Stat(filepath.Join(c.root, c.pendingCheck))
		c.fileAtRow = append(c.fileAtRow, err == nil)
	}
	c.mu.Unlock()
	return c.w.Tx(ctx, fn)
}

type harness struct {
	p    *Pool
	s    *store.Store
	src  *fakeSource
	cw   *checkingWriter
	clk  *clock.Fake
	root string
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
	h := &harness{s: s, clk: clk, root: root}
	h.src = &fakeSource{respond: func(call) (string, error) { return "", nil }}
	h.cw = &checkingWriter{w: s.Writer, root: root}
	cfg := Config{Root: root, TailLines: testTail, MaxBytes: testBytes, Workers: 1, QueueSize: 4, EarlyDebounce: time.Minute, StabilizationWindow: 10 * time.Minute}
	h.p = New(cfg, h.cw, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.exec(t, `INSERT INTO clusters (id, identity, name, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'c', 'orbstack', 'https://127.0.0.1:26443', ?)`, clock.Format(testNow))
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

func (h *harness) seedPod(t *testing.T, uid string, exitCode int64) {
	t.Helper()
	ts := clock.Format(testNow)
	h.exec(t, `INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at) VALUES (?, 1, 'idios-smoke', ?, 'Running', ?, ?, ?)`, uid, "pod-"+uid, ts, ts, ts)
	h.exec(t, `INSERT INTO containers (pod_uid, name, kind, image, state, exit_code, restart_count, updated_at) VALUES (?, 'api', 'app', 'img', 'terminated', ?, 1, ?)`, uid, exitCode, ts)
}

func (h *harness) seedIncident(t *testing.T, uid string) int64 {
	t.Helper()
	var id int64
	err := h.s.Writer.Tx(context.Background(), func(tx *sql.Tx) (err error) {
		id, err = store.OpenIncident(context.Background(), tx, store.Incident{
			ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: &uid, ContainerName: "api",
			WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash, FirstReason: "Error", LastReason: "Error",
			Occurrences: 1, OpenedAt: clock.Format(testNow), LastSeenAt: clock.Format(testNow),
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (h *harness) artifacts(t *testing.T) []store.Artifact {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT pod_uid, incident_id, container_name, kind, restart_count, file_path, size_bytes, truncated, captured_early, capture_gap, capture_note, captured_at
FROM artifacts ORDER BY pod_uid, container_name, kind, restart_count`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.Artifact
	for rows.Next() {
		var a store.Artifact
		if err := rows.Scan(&a.PodUID, &a.IncidentID, &a.ContainerName, &a.Kind, &a.RestartCount, &a.FilePath, &a.SizeBytes, &a.Truncated, &a.CapturedEarly, &a.CaptureGap, &a.CaptureNote, &a.CapturedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (h *harness) file(t *testing.T, rel string) *string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.root, rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	return &s
}

func previousRequest(uid string, previous bool) processor.CaptureRequest {
	return processor.CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: uid, PodName: "pod-" + uid, Container: "api",
		Kind: store.ArtifactLogPrevious, RestartCount: 1, Previous: previous, Trigger: processor.TriggerRestart}
}

func diff(t *testing.T, want, got any) {
	t.Helper()
	if d := cmp.Diff(want, got, cmpopts.EquateEmpty()); d != "" {
		t.Fatal(d)
	}
}

func TestLogCaptureWritesFileThenRow(t *testing.T) {
	pods := schema.GroupResource{Resource: "pods"}
	notFound := apierrors.NewNotFound(pods, "pod-p1")
	badReq := apierrors.NewBadRequest("previous terminated container \"api\" in pod \"pod-p1\" not found")
	forbidden := apierrors.NewForbidden(pods, "pod-p1", errors.New("no"))
	kubelet := "unable to retrieve container logs for containerd://abc\n"
	rel := "1/idios-smoke/p1/api/restart_001.log"
	cases := []struct {
		name      string
		previous  bool
		body      string
		err       error
		wantFile  *string
		wantRow   store.Artifact
		fileAtRow bool
	}{
		{"content", true, "a\nb\n", nil, ptr("a\nb\n"), store.Artifact{FilePath: &rel, SizeBytes: 4}, true},
		{"tail plus one line drops the oldest", true, "l1\nl2\nl3\n", nil, ptr("l2\nl3\n"), store.Artifact{FilePath: &rel, SizeBytes: 6, Truncated: true}, true},
		{"max plus one byte drops the last", true, "123456789", nil, ptr("12345678"), store.Artifact{FilePath: &rel, SizeBytes: 8, Truncated: true}, true},
		{"no trailing newline counts as a line", true, "l1\nl2\nl3", nil, ptr("l2\nl3"), store.Artifact{FilePath: &rel, SizeBytes: 5, Truncated: true}, true},
		{"empty body", true, "", nil, nil, store.Artifact{CaptureGap: ptr(store.GapNoOutput)}, false},
		{"kubelet error with status 200", true, kubelet, nil, nil, store.Artifact{CaptureGap: ptr(store.GapKubeletError), CaptureNote: ptr(strings.TrimSuffix(kubelet, "\n"))}, false},
		{"not found", true, "", notFound, nil, store.Artifact{CaptureGap: ptr(store.GapPodDeleted), CaptureNote: ptr(notFound.Error())}, false},
		{"bad request on previous", true, "", badReq, nil, store.Artifact{CaptureGap: ptr(store.GapNoPreviousRun), CaptureNote: ptr(badReq.Error())}, false},
		{"bad request on current", false, "", badReq, nil, store.Artifact{CaptureGap: ptr(store.GapUnknown), CaptureNote: ptr(badReq.Error())}, false},
		{"forbidden", true, "", forbidden, nil, store.Artifact{CaptureGap: ptr(store.GapForbidden), CaptureNote: ptr(forbidden.Error())}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.seedPod(t, "p1", 1)
			h.src.respond = func(call) (string, error) { return c.body, c.err }
			h.cw.pendingCheck = rel
			r := previousRequest("p1", c.previous)

			h.p.process(context.Background(), h.src, r)

			diff(t, []call{{Namespace: "idios-smoke", Pod: "pod-p1", Opts: corev1.PodLogOptions{Container: "api", Previous: c.previous, TailLines: ptr(int64(testTail + 1)), LimitBytes: ptr(testBytes + 1)}}}, h.src.calls)
			diff(t, c.wantFile, h.file(t, rel))
			want := c.wantRow
			want.PodUID, want.ContainerName, want.Kind, want.RestartCount, want.CapturedAt = "p1", "api", store.ArtifactLogPrevious, 1, clock.Format(testNow)
			diff(t, []store.Artifact{want}, h.artifacts(t))
			diff(t, []bool{c.fileAtRow}, h.cw.fileAtRow)
		})
	}
}

func TestPodJSONCarriesTypeMetaAndIsOverwritten(t *testing.T) {
	h := newHarness(t)
	h.seedPod(t, "p1", 0)
	rel := "1/idios-smoke/p1/pod.json"
	req := func(pod *corev1.Pod) processor.CaptureRequest {
		return processor.CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: "p1", PodName: "pod-p1",
			Kind: store.ArtifactPodJSON, RestartCount: store.NoRestartIndex, Trigger: processor.TriggerRestart, Pod: pod}
	}
	first := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-p1", Namespace: "idios-smoke", UID: "p1"}}
	second := first.DeepCopy()
	second.Labels = map[string]string{"app": "web"}

	h.p.process(context.Background(), h.src, req(first))
	h.p.process(context.Background(), h.src, req(second))
	h.p.process(context.Background(), h.src, req(nil))

	var got corev1.Pod
	data := h.file(t, rel)
	if data == nil {
		t.Fatal("pod.json missing")
	}
	if err := json.Unmarshal([]byte(*data), &got); err != nil {
		t.Fatal(err)
	}
	want := second.DeepCopy()
	want.TypeMeta = metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}
	diff(t, want, &got)
	diff(t, []store.Artifact{{PodUID: "p1", Kind: store.ArtifactPodJSON, RestartCount: store.NoRestartIndex, FilePath: &rel, SizeBytes: int64(len(*data)), CapturedAt: clock.Format(testNow)}}, h.artifacts(t))
	if h.src.count() != 0 {
		t.Errorf("pod_json made %d log calls", h.src.count())
	}
}

func TestDeleteAndRestartUseEarlyCopyOnlyForPodsWorthKeeping(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "pod-p1")
	cases := []struct {
		name     string
		exitCode int64
		incident bool
		trigger  string
		kind     string
		fetch    string
		fetchErr error
		wantFile *string
		wantRow  store.Artifact
		wantHeld bool
	}{
		{"delete of a pod with an incident writes the copy", 0, true, processor.TriggerDelete, store.ArtifactLogCurrent, "", notFound,
			ptr("early\n"), store.Artifact{CapturedEarly: true, SizeBytes: 6}, false},
		{"delete of a healthy pod drops the copy", 0, false, processor.TriggerDelete, store.ArtifactLogCurrent, "", notFound,
			nil, store.Artifact{CaptureGap: ptr(store.GapPodDeleted), CaptureNote: ptr(notFound.Error())}, false},
		{"restart with a failed exit writes the copy", 1, false, processor.TriggerRestart, store.ArtifactLogPrevious, "", nil,
			ptr("early\n"), store.Artifact{CapturedEarly: true, SizeBytes: 6}, false},
		{"restart whose own fetch worked leaves the copy", 1, false, processor.TriggerRestart, store.ArtifactLogPrevious, "live\n", nil,
			ptr("live\n"), store.Artifact{SizeBytes: 5}, true},
		{"incident open never consults the cache", 1, true, processor.TriggerIncidentOpen, store.ArtifactLogCurrent, "", notFound,
			nil, store.Artifact{CaptureGap: ptr(store.GapPodDeleted), CaptureNote: ptr(notFound.Error())}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.seedPod(t, "p1", c.exitCode)
			var incidentID *int64
			if c.incident {
				incidentID = ptr(h.seedIncident(t, "p1"))
			}
			h.p.cache.put("p1", "api", "Killing", []byte("early\n"), false, testNow)
			h.src.respond = func(call) (string, error) { return c.fetch, c.fetchErr }
			r := processor.CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: "p1", PodName: "pod-p1", Container: "api",
				Kind: c.kind, RestartCount: store.NoRestartIndex, Trigger: c.trigger, IncidentID: incidentID}
			rel := "1/idios-smoke/p1/api/current.log"
			if c.kind == store.ArtifactLogPrevious {
				r.RestartCount, r.Previous = 1, true
				rel = "1/idios-smoke/p1/api/restart_001.log"
			}

			h.p.process(context.Background(), h.src, r)

			diff(t, c.wantFile, h.file(t, rel))
			want := c.wantRow
			want.PodUID, want.ContainerName, want.Kind, want.RestartCount, want.IncidentID, want.CapturedAt = "p1", "api", c.kind, r.RestartCount, incidentID, clock.Format(testNow)
			if want.CaptureGap == nil {
				want.FilePath = &rel
			}
			diff(t, []store.Artifact{want}, h.artifacts(t))
			if _, held := h.p.cache.take("p1", "api"); held != c.wantHeld {
				t.Errorf("copy still held = %v, want %v", held, c.wantHeld)
			}
		})
	}
}

func TestEarlyRequestFillsCacheWithoutARow(t *testing.T) {
	h := newHarness(t)
	h.seedPod(t, "p1", 0)
	early := processor.CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: "p1", PodName: "pod-p1", Container: "api",
		Kind: store.ArtifactLogCurrent, RestartCount: store.NoRestartIndex, Trigger: processor.TriggerEarlyPrefix + "Killing"}

	h.src.respond = func(call) (string, error) { return "", apierrors.NewBadRequest("container api is waiting to start") }
	h.p.process(context.Background(), h.src, early)
	if _, ok := h.p.cache.take("p1", "api"); ok {
		t.Fatal("a failed early fetch left a copy")
	}

	h.src.respond = func(call) (string, error) { return "l1\nl2\nl3\n", nil }
	h.p.process(context.Background(), h.src, early)

	diff(t, []call{
		{Namespace: "idios-smoke", Pod: "pod-p1", Opts: corev1.PodLogOptions{Container: "api", TailLines: ptr(int64(testTail + 1)), LimitBytes: ptr(testBytes + 1)}},
		{Namespace: "idios-smoke", Pod: "pod-p1", Opts: corev1.PodLogOptions{Container: "api", TailLines: ptr(int64(testTail + 1)), LimitBytes: ptr(testBytes + 1)}},
	}, h.src.calls)
	got, ok := h.p.cache.take("p1", "api")
	if !ok {
		t.Fatal("early copy missing")
	}
	if d := cmp.Diff(held{body: []byte("l2\nl3\n"), truncated: true, at: testNow, final: true}, got, cmp.AllowUnexported(held{})); d != "" {
		t.Fatal(d)
	}
	diff(t, []store.Artifact(nil), h.artifacts(t))
	if h.file(t, "1/idios-smoke/p1/api/current.log") != nil {
		t.Fatal("an early capture wrote a file")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capture/ -run 'TestLogCapture|TestPodJSON|TestDeleteAndRestart|TestEarlyRequest' -v`
Expected: compile error, `undefined: New`, `undefined: Config`.

- [ ] **Step 3: Write the pool skeleton and the worker**

`internal/capture/pool.go` (this task's part; Task 4 appends to it):

```go
package capture

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"idios/internal/clock"
	"idios/internal/processor"
)

// Config sizes the pool and the captures it makes. Root is the artifacts
// root every file path is relative to.
type Config struct {
	Root                string
	TailLines           int
	MaxBytes            int64
	Workers             int
	QueueSize           int
	EarlyDebounce       time.Duration
	StabilizationWindow time.Duration
}

type clusterQueue struct {
	src LogSource
	ch  chan processor.CaptureRequest
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
	dropped  atomic.Uint64
}

// New returns a Pool with no clusters; AddCluster registers them and Run
// starts their workers.
func New(cfg Config, w TxRunner, clk clock.Clock, log *slog.Logger) *Pool {
	return &Pool{cfg: cfg, w: w, clk: clk, log: log, cache: newEarlyCache(cfg.EarlyDebounce), clusters: map[int64]*clusterQueue{}}
}

func ptr[T any](v T) *T { return &v }
```

`internal/capture/worker.go`:

```go
package capture

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/clock"
	"idios/internal/processor"
	"idios/internal/store"
)

const tmpDir = "tmp"

// The API server answers 200 with one of these lines as the whole body when
// the kubelet cannot serve the log; stored as is it would read as output.
var kubeletErrorPrefixes = []string{
	"unable to retrieve container logs for",
	"failed to try resolving symlinks",
}

// fetched is the outcome of one log read: a body, or the gap and note that
// explain its absence.
type fetched struct {
	body      []byte
	truncated bool
	gap       *string
	note      *string
}

func (p *Pool) process(ctx context.Context, src LogSource, r processor.CaptureRequest) {
	switch {
	case r.Kind == store.ArtifactPodJSON:
		p.capturePodJSON(ctx, r)
	case strings.HasPrefix(r.Trigger, processor.TriggerEarlyPrefix):
		p.captureEarly(ctx, src, r)
	default:
		p.captureLog(ctx, src, r)
	}
}

// fetch asks for one line and one byte more than the caps so that hitting a
// cap is known exactly rather than inferred from a count that equals it.
func (p *Pool) fetch(ctx context.Context, src LogSource, r processor.CaptureRequest) fetched {
	opts := &corev1.PodLogOptions{Container: r.Container, Previous: r.Previous, TailLines: ptr(int64(p.cfg.TailLines) + 1), LimitBytes: ptr(p.cfg.MaxBytes + 1)}
	rc, err := src.Logs(ctx, r.Namespace, r.PodName, opts)
	if err != nil {
		return gapFor(err, r.Previous)
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(io.LimitReader(rc, p.cfg.MaxBytes+1))
	if err != nil {
		return fetched{gap: ptr(store.GapUnknown), note: ptr(err.Error())}
	}
	return trim(body, p.cfg.TailLines, p.cfg.MaxBytes)
}

// gapFor maps a GetLogs error. A 400 on previous=true means no dead
// instance exists; a 400 on the current instance (a waiting container has
// none) has no name of its own.
func gapFor(err error, previous bool) fetched {
	gap := store.GapUnknown
	switch {
	case apierrors.IsNotFound(err):
		gap = store.GapPodDeleted
	case apierrors.IsBadRequest(err) && previous:
		gap = store.GapNoPreviousRun
	case apierrors.IsForbidden(err):
		gap = store.GapForbidden
	}
	return fetched{gap: ptr(gap), note: ptr(err.Error())}
}

// trim applies the byte cap before the line cap, the order the server
// applied them: LimitBytes stops the stream, so the extra byte is the last
// one; TailLines keeps the newest lines, so the extra line is the first.
func trim(body []byte, tail int, maxBytes int64) fetched {
	if len(body) == 0 {
		return fetched{gap: ptr(store.GapNoOutput)}
	}
	if line, ok := kubeletError(body); ok {
		return fetched{gap: ptr(store.GapKubeletError), note: ptr(line)}
	}
	var truncated bool
	if int64(len(body)) > maxBytes {
		body, truncated = body[:maxBytes], true
	}
	if lines(body) > tail {
		body, truncated = body[bytes.IndexByte(body, '\n')+1:], true
	}
	return fetched{body: body, truncated: truncated}
}

func lines(b []byte) int {
	n := bytes.Count(b, []byte{'\n'})
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n
}

func kubeletError(body []byte) (string, bool) {
	line := strings.TrimSuffix(string(body), "\n")
	if strings.Contains(line, "\n") {
		return "", false
	}
	for _, prefix := range kubeletErrorPrefixes {
		if strings.HasPrefix(line, prefix) {
			return line, true
		}
	}
	return "", false
}

// relPath is the file's path under the root, with forward slashes so the
// stored value reads the same on every platform.
func relPath(r processor.CaptureRequest) string {
	base := path.Join(strconv.FormatInt(r.ClusterID, 10), r.Namespace, r.PodUID)
	switch r.Kind {
	case store.ArtifactPodJSON:
		return path.Join(base, "pod.json")
	case store.ArtifactLogCurrent:
		return path.Join(base, r.Container, "current.log")
	default:
		return path.Join(base, r.Container, fmt.Sprintf("restart_%03d.log", r.RestartCount))
	}
}

// writeFile lands data at rel through a temp file in the same tree, so a
// reader never sees a partial file and a crash leaves only an orphan.
func (p *Pool) writeFile(rel string, data []byte) error {
	dst := filepath.Join(p.cfg.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(p.cfg.Root, tmpDir)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(tmp, "capture-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// captureLog fetches the log, falls back to the early copy when the fetch
// got nothing and the pod is worth keeping, writes the file and then the
// row. A row is written for every attempt so that a miss is recorded.
func (p *Pool) captureLog(ctx context.Context, src LogSource, r processor.CaptureRequest) {
	f := p.fetch(ctx, src, r)
	early := false
	if f.gap != nil && (r.Trigger == processor.TriggerDelete || r.Kind == store.ArtifactLogPrevious) {
		if h, ok := p.cache.take(r.PodUID, r.Container); ok {
			keep, err := p.worthKeeping(ctx, r.PodUID)
			if err != nil {
				p.log.Error("early copy: keep check", "pod", r.PodName, "uid", r.PodUID, "err", err)
			}
			if keep {
				f, early = fetched{body: h.body, truncated: h.truncated}, true
			}
		}
	}
	a := store.Artifact{PodUID: r.PodUID, IncidentID: r.IncidentID, ContainerName: r.Container, Kind: r.Kind, RestartCount: r.RestartCount,
		CapturedEarly: early, CapturedAt: clock.Format(p.clk.Now())}
	if f.gap != nil {
		a.CaptureGap, a.CaptureNote = f.gap, f.note
	} else {
		rel := relPath(r)
		if err := p.writeFile(rel, f.body); err != nil {
			a.CaptureGap, a.CaptureNote = ptr(store.GapUnknown), ptr(err.Error())
		} else {
			a.FilePath, a.SizeBytes, a.Truncated = &rel, int64(len(f.body)), f.truncated
		}
	}
	p.writeRow(ctx, a)
}

// capturePodJSON writes the pod the request carries. Informers hand out
// objects with an empty TypeMeta, so it is set before marshalling.
func (p *Pool) capturePodJSON(ctx context.Context, r processor.CaptureRequest) {
	if r.Pod == nil {
		p.log.Error("pod_json request without a pod", "pod", r.PodName, "uid", r.PodUID)
		return
	}
	pod := r.Pod.DeepCopy()
	pod.TypeMeta = metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}
	a := store.Artifact{PodUID: r.PodUID, IncidentID: r.IncidentID, Kind: store.ArtifactPodJSON, RestartCount: store.NoRestartIndex, CapturedAt: clock.Format(p.clk.Now())}
	data, err := json.Marshal(pod)
	if err == nil {
		rel := relPath(r)
		if err = p.writeFile(rel, data); err == nil {
			a.FilePath, a.SizeBytes = &rel, int64(len(data))
		}
	}
	if err != nil {
		a.CaptureGap, a.CaptureNote = ptr(store.GapUnknown), ptr(err.Error())
	}
	p.writeRow(ctx, a)
}

// captureEarly takes a live copy into the cache when the debounce and
// once-per-container rules allow it. Nothing is written to disk or to the
// database: the copy only becomes an artifact if a later capture needs it.
func (p *Pool) captureEarly(ctx context.Context, src LogSource, r processor.CaptureRequest) {
	reason := strings.TrimPrefix(r.Trigger, processor.TriggerEarlyPrefix)
	now := p.clk.Now()
	if !p.cache.wants(r.PodUID, r.Container, reason, now) {
		return
	}
	r.Previous = false
	f := p.fetch(ctx, src, r)
	if f.gap != nil {
		return
	}
	p.cache.put(r.PodUID, r.Container, reason, f.body, f.truncated, now)
}

func (p *Pool) writeRow(ctx context.Context, a store.Artifact) {
	err := p.w.Tx(ctx, func(tx *sql.Tx) error { return store.UpsertArtifact(ctx, tx, a) })
	if err != nil {
		p.log.Error("artifact row", "uid", a.PodUID, "container", a.ContainerName, "kind", a.Kind, "restart", a.RestartCount, "err", err)
	}
}

// worthKeeping is the gate on early copies: an incident open or closed
// within the stabilization window, or a container that exited non-zero.
func (p *Pool) worthKeeping(ctx context.Context, podUID string) (bool, error) {
	var keep bool
	since := clock.Format(p.clk.Now().Add(-p.cfg.StabilizationWindow))
	err := p.w.Tx(ctx, func(tx *sql.Tx) (err error) {
		keep, err = store.HasRecentIncidentOrFailure(ctx, tx, podUID, since)
		return err
	})
	return keep, err
}
```

`store.HasRecentIncidentOrFailure` counts containers whose `exit_code <> 0 OR last_terminated_exit_code <> 0`; a NULL exit code compares as unknown and does not count, which is why the tests seed `exit_code` explicitly.

Note on `checkingWriter`: `worthKeeping` also runs a `Tx`, so in the "writes the copy" cases `fileAtRow` records two entries. That test does not assert `fileAtRow`; only `TestLogCaptureWritesFileThenRow` does, and there the cache is empty so `worthKeeping` is never called.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capture/ -v`
Expected: PASS for all six tests. If `TestLogCaptureWritesFileThenRow/kubelet error with status 200` fails on the note, check that the fake body ends in `\n` and the expectation strips it; the worker stores the line without its newline.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/capture/pool.go internal/capture/worker.go internal/capture/worker_test.go && git commit -m "capture: add worker with truncation, gap mapping, files and early fallback"`

---

### Task 4: Queue, workers, drop counter

**Files:**
- Modify: `internal/capture/pool.go`
- Test: `internal/capture/pool_test.go`

**Interfaces:**
- Consumes: `Pool`, `clusterQueue`, `process` (Task 3); test harness `newHarness`, `harness.seedPod`, `harness.artifacts`, `diff` (Task 3).
- Produces: `func (p *Pool) AddCluster(clusterID int64, src LogSource)`; `func (p *Pool) Enqueue(r processor.CaptureRequest)`; `func (p *Pool) Run(ctx context.Context) error`; `func (p *Pool) Dropped() uint64`; compile-time `var _ processor.CaptureSink = (*Pool)(nil)`.

Tests and their trace:
- `TestFullQueueDropsAndCounts`: process 6.1 "A full channel drops the request and increments a counter that `idios status` shows" (plus a request for a cluster the pool does not know, which has no queue to be full).
- `TestWorkersDrainTheQueue`: process 6.1 "`N` worker goroutines per cluster" and 6.1 "the only thing that must never run on an informer goroutine": `Enqueue` returns before the row exists and the row appears without the caller doing anything else.

- [ ] **Step 1: Write the failing tests**

`internal/capture/pool_test.go`:

```go
package capture

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/processor"
	"idios/internal/store"
)

func podJSONRequest(cluster int64, uid string) processor.CaptureRequest {
	return processor.CaptureRequest{ClusterID: cluster, Namespace: "idios-smoke", PodUID: uid, PodName: "pod-" + uid,
		Kind: store.ArtifactPodJSON, RestartCount: store.NoRestartIndex, Trigger: processor.TriggerRestart,
		Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-" + uid, Namespace: "idios-smoke", UID: "p1"}}}
}

func TestFullQueueDropsAndCounts(t *testing.T) {
	h := newHarness(t)
	h.p.cfg.QueueSize = 2
	h.p.AddCluster(1, h.src)
	for i := 0; i < 3; i++ {
		h.p.Enqueue(podJSONRequest(1, "p1"))
	}
	h.p.Enqueue(podJSONRequest(9, "p1"))
	if got := h.p.Dropped(); got != 2 {
		t.Fatalf("dropped = %d, want 2 (one over capacity, one unknown cluster)", got)
	}
	if got := len(h.p.clusters[1].ch); got != 2 {
		t.Fatalf("queued = %d, want 2", got)
	}
}

func TestWorkersDrainTheQueue(t *testing.T) {
	h := newHarness(t)
	h.seedPod(t, "p1", 0)
	h.p.AddCluster(1, h.src)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.p.Run(ctx) }()

	h.p.Enqueue(podJSONRequest(1, "p1"))
	deadline := time.Now().Add(5 * time.Second)
	for len(h.artifacts(t)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no artifact row within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
	if got := h.p.Dropped(); got != 0 {
		t.Fatalf("dropped = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capture/ -run 'TestFullQueue|TestWorkersDrain' -v`
Expected: compile error, `h.p.AddCluster undefined`.

- [ ] **Step 3: Append to `internal/capture/pool.go`**

Add `"context"` to the import block, then append:

```go
var _ processor.CaptureSink = (*Pool)(nil)

// AddCluster registers the log source for one cluster row. Call it before
// Run; a cluster added afterwards gets no workers.
func (p *Pool) AddCluster(clusterID int64, src LogSource) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clusters[clusterID] = &clusterQueue{src: src, ch: make(chan processor.CaptureRequest, p.cfg.QueueSize)}
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
	default:
		p.dropped.Add(1)
		p.log.Warn("capture queue full", "cluster", r.ClusterID, "pod", r.PodName, "container", r.Container, "kind", r.Kind, "trigger", r.Trigger)
	}
}

// Dropped counts requests that found no queue or a full one.
func (p *Pool) Dropped() uint64 {
	return p.dropped.Load()
}

// Run starts the workers of every registered cluster and blocks until ctx
// ends. Requests still queued at that point are dropped without a row: a
// canceled context could not make the calls they need.
func (p *Pool) Run(ctx context.Context) error {
	p.mu.Lock()
	queues := make([]*clusterQueue, 0, len(p.clusters))
	for _, q := range p.clusters {
		queues = append(queues, q)
	}
	p.mu.Unlock()
	var wg sync.WaitGroup
	for _, q := range queues {
		for i := 0; i < p.cfg.Workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
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
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capture/ -race -v`
Expected: PASS, no race reports. The cache and the drop counter are shared across workers; `-race` here is the check that the locking in Tasks 2 and 3 holds.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/capture/pool.go internal/capture/pool_test.go && git commit -m "capture: add per-cluster queues, workers and drop counter"`

---

### Task 5: Watcher clientset accessor

**Files:**
- Modify: `internal/k8s/watcher.go`
- Modify: `internal/k8s/watcher_test.go` (one assertion in `TestBackoffDoublesToAMinute`, or whatever the backoff test is named; it is the test containing `if r.w.Ready() {` near the end)

**Interfaces:**
- Consumes: `Watcher`, `runOnce`.
- Produces: `func (w *Watcher) Client() kubernetes.Interface`.

Test and trace: process 6.1 needs a clientset per cluster for `GetLogs`; the roadmap's Phase 4 handoff names this accessor as one of the two ways to get it. The assertion added is: a watcher that never connected returns nil. The connected case is covered by Task 7's manual run (the pool's rows prove the client was there).

- [ ] **Step 1: Add the assertion**

In `internal/k8s/watcher_test.go`, directly after the block

```go
	if r.w.Ready() {
		t.Error("watcher reported ready without a connection")
	}
```

add

```go
	if r.w.Client() != nil {
		t.Error("watcher offered a clientset without a connection")
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/k8s/ -run Backoff -v`
Expected: compile error, `r.w.Client undefined`. If no test name matches `Backoff`, run `go test ./internal/k8s/ -v` and confirm the compile error.

- [ ] **Step 3: Add the accessor**

In `internal/k8s/watcher.go`, add `"k8s.io/client-go/kubernetes"` to the import block and add to the `Watcher` struct, after `ready atomic.Bool`:

```go
	clientMu sync.Mutex
	current  kubernetes.Interface
```

Add after `Ready`:

```go
// Client returns the clientset of the current connection, nil before the
// first one succeeds. The capture pool reads logs through it so that a
// reconnect with an edited kubeconfig is picked up there too.
func (w *Watcher) Client() kubernetes.Interface {
	w.clientMu.Lock()
	defer w.clientMu.Unlock()
	return w.current
}
```

In `runOnce`, directly after the `clusterIdentity` call succeeds (after its `if err != nil { ... }` block), add:

```go
	w.clientMu.Lock()
	w.current = client
	w.clientMu.Unlock()
```

`sync` is already imported in `watcher.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/k8s/ -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/k8s/watcher.go internal/k8s/watcher_test.go && git commit -m "k8s: expose the connected clientset"`

---

### Task 6: Wire the pool into cmd/idios

**Files:**
- Modify: `cmd/idios/main.go`

**Interfaces:**
- Consumes: `capture.New`, `capture.Config`, `capture.Pool.AddCluster`, `capture.Pool.Run`, `capture.ClientLogs`, `k8s.Watcher.Client`, `processor.New`, `config.Config` fields `ArtifactsRoot`, `LogTailLines`, `LogMaxBytes`, `CaptureWorkersPerCluster`, `CaptureQueueSize`, `EarlyCaptureDebounce`, `StabilizationWindow`.
- Produces: a binary whose capture requests become files and rows. `logSink` is deleted.

No automated test: wiring of tested parts, verified by Task 7's manual run.

- [ ] **Step 1: Edit `cmd/idios/main.go`**

Replace the line

```go
	proc := processor.New(st.Writer, clock.Real{}, logSink{log: logger}, cfg.StabilizationWindow)
```

with

```go
	pool := capture.New(capture.Config{
		Root: cfg.ArtifactsRoot, TailLines: cfg.LogTailLines, MaxBytes: cfg.LogMaxBytes,
		Workers: cfg.CaptureWorkersPerCluster, QueueSize: cfg.CaptureQueueSize,
		EarlyDebounce: cfg.EarlyCaptureDebounce, StabilizationWindow: cfg.StabilizationWindow,
	}, st.Writer, clock.Real{}, logger)
	proc := processor.New(st.Writer, clock.Real{}, pool, cfg.StabilizationWindow)
```

Replace the cluster loop body so the pool learns each watcher's client before the watcher starts, and start the pool once after the loop:

```go
	var wg sync.WaitGroup
	for _, c := range clusters {
		skew := k8s.NewSkew(clock.Real{}, logger.With("cluster", c.ID))
		w := k8s.New(k8s.Config{ClusterID: c.ID, ContextName: c.ContextName, Namespaces: namespaces[c.ID]},
			k8s.KubeconfigClient(cfg.Kubeconfig, c.ContextName, skew), proc, logger)
		pool.AddCluster(c.ID, capture.ClientLogs(w.Client))
		logger.Info("watching", "cluster", c.ID, "context", c.ContextName, "namespaces", namespaces[c.ID])
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("watcher stopped", "cluster", c.ID, "err", err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := pool.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("capture pool stopped", "err", err)
		}
	}()
```

Delete the `logSink` type and its `Enqueue` method and the comment above them. Keep `var _ k8s.Handler = (*processor.Processor)(nil)`. Add `"idios/internal/capture"` to the import block. `log/slog` stays imported (the logger).

- [ ] **Step 2: Build and run the suite**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`
Expected: green.

- [ ] **Step 3: Commit**

Commit: `git add cmd/idios/main.go && git commit -m "cmd: capture logs and manifests through the pool"`

---

### Task 7: Manual verification against OrbStack (orchestrator)

No files change unless the run finds a bug. Skip only if the API is unreachable after `orb start`; say so in the report.

- [ ] **Step 1: Run against a crashing pod in `idios-smoke`**

```bash
KUBECONFIG=./kube/config kubectl get ns idios-smoke
rm -rf .storage && mkdir -p .storage
go build -o bin/idios ./cmd/idios
./bin/idios & sleep 3; kill -INT %1; wait
sqlite3 .storage/idios.db "INSERT INTO clusters (name, context_name, api_server_url, first_seen_at) VALUES ('orbstack', 'orbstack', '', strftime('%Y-%m-%dT%H:%M:%f000Z','now'));
INSERT INTO watched_namespaces (cluster_id, name, added_at) VALUES (1, 'idios-smoke', strftime('%Y-%m-%dT%H:%M:%f000Z','now'));"
KUBECONFIG=./kube/config kubectl -n idios-smoke run smoke-crash --image=busybox:1.36 --restart=Always -- sh -c 'echo hello from attempt; echo line two; exit 1'
./bin/idios & sleep 90; kill -INT %1; wait
sqlite3 -header .storage/idios.db "SELECT pod_uid, container_name, kind, restart_count, file_path, size_bytes, truncated, captured_early, capture_gap, capture_note FROM artifacts ORDER BY id;"
find .storage/artifacts -type f | sort
KUBECONFIG=./kube/config kubectl -n idios-smoke delete pod smoke-crash --wait=false
```

Expected: at least one `log_previous` row with a `file_path` like `1/idios-smoke/<uid>/smoke-crash/restart_000.log` whose file holds `hello from attempt\nline two\n`; a `pod_json` row whose file starts with `{"kind":"Pod","apiVersion":"v1"`; `log_current` rows for the `crash` incident, typically with `capture_gap = 'unknown'` or `'no_output'` while the container is in `CrashLoopBackOff` (a waiting container has no current instance), which is the truthful row; every file mode `0600` (`ls -l`), every directory `0700`; `.storage/artifacts/tmp` empty. If a `pod_json` file is missing while its row has a path, or a row has a path with no file, that is a bug in Task 3 and is fixed on the branch with its own test and commit before the merge.

- [ ] **Step 2: Clean up**

`rm -rf .storage bin` and confirm `KUBECONFIG=./kube/config kubectl -n idios-smoke get pods` shows `smoke-crash` gone or terminating. Nothing outside `idios-smoke` is touched.

---

### Task 8: Roadmap status and handoff

**Files:**
- Modify: `docs/plans/m1-recorder/roadmap.md` (Phase 5 section)

- [ ] **Step 1: Record the status**

Below the `### Phase 5: capture pool` heading, before "Delivers:", insert:

```markdown
Status: done, merged to `main` 2026-08-27 (fast-forward of
`phase-5-capture-pool`, last code commit `<hash>`). Plan:
`05-capture-pool.md`.
```

The orchestrator fills `<hash>` with the last code commit on the branch (the commit before this docs commit); after the fast-forward that hash is on `main`.

- [ ] **Step 2: Correct the Delivers and Tests lines**

Replace the Phase 5 "Delivers:" paragraph with:

```markdown
Delivers: `internal/capture`: `Pool` (one queue of `capture_queue_size` and
`capture_workers_per_cluster` workers per registered cluster, `Dropped()`
counter), `LogSource` seam with `ClientLogs` over a `kubernetes.Interface`
getter, worker logic (`GetLogs` with `TailLines+1` / `LimitBytes+1`, body
in memory, kubelet 200-error check, byte then line truncation, temp file
then rename, `capture_gap` mapping), early-capture cache (per-container
debounce and once-per-container final copies, 30 min expiry, 500-pod LRU),
file layout from storage doc Section 7, `pod_json` with `TypeMeta` set.
Writes `artifacts` rows through `store.UpsertArtifact`, file first.
`capture` imports `processor` for `CaptureRequest` and implements
`processor.CaptureSink`; `k8s.Watcher.Client()` supplies the clientset.
```

Replace the "Tests:" line with:

```markdown
Tests: fake `LogSource` per process doc Section 14.5 (`TestLogCaptureWritesFileThenRow`
table: content, tail+1 lines, max+1 bytes, empty, kubelet error line,
NotFound, BadRequest on previous and on current, Forbidden; the options the
fake received; file exists when the row is written), `pod_json` TypeMeta and
overwrite, early copy used only for pods worth keeping, early requests fill
the cache without a row, cache debounce/once/expiry/LRU, queue full drops
and counts, workers drain; `store.UpsertArtifact` keeps a file over a later
gap.
```

- [ ] **Step 3: Record what Phase 5 hands on**

Append to the Phase 5 section, after "Depends on: ...":

```markdown
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
with note `cluster not connected`. While a container sits in
`CrashLoopBackOff` the `log_current` capture for an `incident_open` is a
`400` on the current instance and lands as `unknown`; if the smoke run
shows that is the common case, a named gap value would need a schema CHECK
change and is a Phase 7 decision.
```

- [ ] **Step 4: Checkpoint**

Run: `make ascii`

Commit: `git add docs/plans/m1-recorder/roadmap.md && git commit -m "docs: record phase 5 completion and handoff in roadmap"`

---

## Self-review

**Spec coverage.**
- Process 2 dependency rules: `capture` imports `client-go`, `store`, `processor`, `clock`; not `ingest` (archtest, every checkpoint). The `processor` import is a decision recorded above.
- Process 6.1: bounded queue and N workers per cluster, drop and count (Task 4, tests); worker steps `TailLines + 1`, `LimitBytes + 1`, `Previous` as carried, temp file then rename under `tmp/`, 200-error check, extra line/byte dropped with `truncated`, `Writer.Tx` upsert, file first row second, a row for every attempt, gap mapping (Task 3, `TestLogCaptureWritesFileThenRow`); dedup by upsert and overwrite of `log_current` / `pod_json` (Task 1 test, Task 3 `TestPodJSONCarriesTypeMetaAndIsOverwritten`).
- Process 6.2: keyed by pod uid, one entry per container with bytes and reason (`held.final` is the reason class); `Unhealthy` debounce; the four bypass reasons once; kept after delete (nothing in the pool removes an entry on delete except a consult); consulted only after own fetch failed or was empty; `captured_early = 1` only for pods worth keeping; 30 min expiry; 500-pod LRU (Tasks 2, 3 tests).
- Process 10: file errors recorded in the row as `unknown` with the OS error (Task 3 `captureLog`, `capturePodJSON`).
- Process 12: `0600` files (`os.CreateTemp`), `0700` directories (Task 3 `writeFile`), checked by hand in Task 7.
- Process 13: the five capture settings flow from `config.Config` into `capture.Config` (Task 6).
- Process 14.5: every bullet is a row of `TestLogCaptureWritesFileThenRow`, plus the `Previous` flag the fake received and file-before-row.
- Storage 4: `Previous` is taken from the request as the processor set it; the dead-instance index is `RestartCount` from the request and names the file (`restart_%03d.log`).
- Storage 5.11: all columns written; CHECK satisfied by construction (gap rows have no path, file rows no gap); `incident_id` from the request; kubelet-error rule (Task 3).
- Storage 6.2 rules 4-6: `log_current` and `pod_json` requests are consumed as the processor emits them; rule 6's cache and gate (Task 3).
- Storage 7: layout and relative paths (`relPath`), write order (Task 3 test).
- Client-go 3.11: `GetLogs` options, `Previous` semantics carried, 200-error line, `TailLines`/`LimitBytes` detection, early trigger reasons and their two classes, gap mapping including `IsBadRequest` on `Previous = true`, `pod_json` with `TypeMeta` and no API call (Task 3).
- Roadmap Phase 3 handoff: `IncidentID` written to `artifacts.incident_id`; `RestartCount int64`; triggers `restart`, `incident_open`, `delete`, `early:<reason>` all handled; `capture` does not import `ingest`; a `delete` request only arrives for a pod worth keeping (the pool applies the same gate anyway); the same dead instance requested twice is an upsert (Task 1).
- Roadmap Phase 4 handoff: `k8s.Watcher` grows the accessor (Task 5); `cmd/idios` wires it (Task 6).

**Placeholder scan.** Every code step has full code. `<hash>` in Task 8 is filled by the orchestrator and is the only value not known while planning.

**Type consistency.** `LogSource.Logs(ctx, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error)` (Task 2) is what `fetch` calls (Task 3) and `fakeSource` implements (Task 3 test) and `ClientLogs` returns (Task 2). `TxRunner.Tx(ctx, func(*sql.Tx) error) error` matches `store.Writer.Tx` (Task 3 `New` receives `st.Writer` in Task 6) and `checkingWriter` (Task 3 test). `earlyCache.wants/put/take/len` signatures in Task 2 are the ones Task 3 calls (`wants(podUID, container, reason, now)`, `put(podUID, container, reason, body, truncated, now)`, `take(podUID, container)`). `Config` fields named in Task 3 are the ones Task 6 fills and the tests set (`QueueSize` mutated in `TestFullQueueDropsAndCounts` before `AddCluster`, which reads it when making the channel). `Pool.clusters map[int64]*clusterQueue` with `ch` is what `TestFullQueueDropsAndCounts` inspects. `store.UpsertArtifact(ctx, tx, Artifact)` (Task 1) is what `writeRow` calls. `Watcher.Client() kubernetes.Interface` (Task 5) is the getter type `ClientLogs(get func() kubernetes.Interface)` takes (Task 2, used in Task 6).

**`.ai` rules by name.** ascii-only: plan and code are ASCII. tests: every test has a trace above; variants are table rows; assertions compare whole rows, whole call lists and exact file contents. comments: only reasons and doc comments; the `podEntry`, `clusterQueue`, `clientLogs` types are unexported and self-evident. code-is-truth: no code comment names a document. scope: `TxRunner` and `LogSource` are the two seams, each with a production implementation today; `Dropped()` and `Client()` are named handoffs; nothing else beyond the tasks. commits: one per task, `area: subject`, no trailers, named paths.
