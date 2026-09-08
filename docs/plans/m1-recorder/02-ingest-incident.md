# Phase 2: Ingest + Incident (pure logic) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Read `.ai/*.md` before writing any code; those rules override habits.

**Goal:** Two packages of pure functions: `internal/ingest` turns a Kubernetes object plus the store's snapshot rows into the row values and history rows to write, and `internal/incident` turns those changes plus the pod's existing incidents into open / attach / reopen / close operations. No SQL, no network, clock passed in. Every design-doc scenario is a table row with a JSON fixture of a real object.

**Architecture:** `ingest` diffs; `incident` classifies and decides lifecycle. `ingest` never names a category (it imports only `store`, `clock`, `k8s.io/api`, `k8s.io/apimachinery`); `incident` imports `ingest` for the change structs, so the dependency runs one way. History rows leave `ingest` with `Category` and `IncidentID` nil and `incident.Apply` returns, in parallel slices, what to put there. Capture requests are not built here: `ingest` reports dead container instances (`DeadInstance`) and `incident` reports which incidents opened; Phase 3 turns both into `CaptureRequest`s.

**Tech Stack:** Go 1.24, `k8s.io/api` and `k8s.io/apimachinery` at `v0.34.1` (the last line that builds with Go 1.24; `v0.35.0` requires Go 1.25), `github.com/google/go-cmp`, standard `testing`, `encoding/json` for fixtures.

**Spec:** `docs/design/data-storage.md` Sections 4, 5.3-5.10, 6.1-6.3; `docs/design/client-go-methods.md` Sections 3.3-3.10; `docs/design/process-architecture.md` Sections 4.1, 4.2, 4.4, 14.1, 14.3. Roadmap: `docs/plans/m1-recorder/roadmap.md` (Phase 2 section). The plan may cite these; the code must not (`.ai/code-is-truth.md`).

## Global Constraints

- `.ai/ascii-only.md`, `.ai/tests.md`, `.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`, `.ai/commits.md` apply to every line written. Every exported identifier gets a one-line doc comment even where a code block below omits it. Code blocks contain no comments beyond those shown; do not add tests, helpers or seams a task does not list.
- Every task ends with `go build ./... && go test ./... && make ascii` green, then one commit per `.ai/commits.md` (subject `area: what`, no trailers, `git add` named paths). `go vet ./...` must also be clean.
- Module path `idios`, `go 1.24.0` directive. Dependencies added by this phase: `k8s.io/api@v0.34.1`, `k8s.io/apimachinery@v0.34.1`. Never `client-go` (the archtest forbids it in these packages).
- Timestamps written to any store row go through `clock.Format`; nothing in these packages calls `time.Format`. Kubernetes timestamps are stored verbatim (UTC); process time is the `now time.Time` argument.
- Categories key on `reason` only. The only file that maps reasons to categories is `internal/incident/category.go`; a test fails if `.Phase`, `.ExitCode` or `.Message` appears in it, or if a function named `*Category` is defined anywhere else in `internal/ingest` or `internal/incident`.
- No schema change. If a task appears to need one, stop and report; do not add a migration.
- Store row types and constants (`store.Pod`, `store.Container`, `store.ContainerStateHistory`, `store.PodCondition`, `store.Incident`, `store.Job`, `store.RolloutHistory`, `store.K8sEvent`, `store.Category*`, `store.Close*`, `store.State*`, `store.ContainerKind*`, `store.Subject*`) are used as they exist in `internal/store/rows.go`. Do not add fields to them.
- Fixtures are JSON under `internal/ingest/testdata/<scenario>/`. They are decoded with `encoding/json` into `corev1.Pod`, `batchv1.Job`, `appsv1.ReplicaSet`, `corev1.Event`. Namespace is always `idios-smoke`; cluster id in tests is `1`; process time in tests is `testNow = 2026-08-27T12:00:00Z`.

## Decisions made here (the spec leaves them open)

- **Conditions at first sight are recorded.** `pod_condition_history` is also the snapshot of conditions (there is no other place), so with no stored row for a condition type the incoming condition is inserted. The "no history row on first sight" rule applies to `container_state_history`, whose snapshot is `containers`.
- **A gap inserts two history rows.** When `restart_count` jumped by more than one, the first row describes `lastState.terminated` with `gap_reconstructed = 1` and `restart_count` = the dead-instance index; the second is the normal row for the current state. Dead-instance indexes strictly between the snapshot's newest known dead index and the new one are reported as `DeadInstance{Unobservable: true}` (Phase 3 writes the `unobservable` artifact rows).
- **Dead-instance index** is `restart_count` when the current state is `waiting` or `terminated`, `restart_count - 1` when `running`. The snapshot's newest known dead index uses the same rule, and is `-1` when its `restart_count` is 0 and it is `running`. Phase 7's smoke run against the real kubelet is where this rule is confirmed; only `deadIndex` changes if it is off by one.
- **`opened_at` precedence for a container incident:** history row `K8sFinishedAt` -> the container row's `LastTerminatedAt` -> history row `K8sStartedAt` -> `now`. For a condition incident: the condition's `K8sTransitionAt` -> `now`. For a `status_reason` incident: `now`. On first sight the same order applies with the container's current state and `LastTerminatedAt`, then `created_at` before `now`.
- **`ContainerCategory` reads three reasons:** current state, current reason, and `lastState.terminated.reason`. A `running` state is never a problem. `waiting(CrashLoopBackOff)` is `oom` when the last termination was `OOMKilled`, otherwise `crash`. This keeps the storage rule "terminated (`state` or `lastState`) with `OOMKilled`" without reading exit codes.
- **Two problems with one key in one event coalesce** (both `node_pressure` rules fire on one eviction): one `Open` with `Occurrences = 2`, `LastReason` from the second.
- **Events attach without bumping.** Only the `probe` event goes through open/attach and bumps `occurrences`; any other event that matches an open incident (5.10 attach rule) only receives that incident's id.
- **`MapEvent` does not classify.** It fills every `k8s_events` column except `Category`; the processor sets `Category = incident.EventCategory(row.Reason)` so `ingest` stays free of category logic.
- **`MapReplicaSet`, not `DiffReplicaSet`.** Nothing is diffed: the upsert keeps `first_seen_at` in SQL. The roadmap's name is corrected here.
- **Pod deletion is Phase 3.** `deletion_reason` needs sibling rows (SQL). `DiffPod` only copies `metadata.deletionTimestamp` into `DeletionRequestedAt`.

## File structure

```
go.mod, go.sum                                   k8s.io/api, k8s.io/apimachinery v0.34.1
internal/ingest/types.go                         PodSnapshot, PodChanges, DeadInstance, OwnerResolver
internal/ingest/time.go                          k8sTime, k8sTimePtr
internal/ingest/containers.go                    mapContainers, imageTag, quantity helpers
internal/ingest/containers_test.go
internal/ingest/pod.go                           DiffPod, resolveWorkload, container/condition diff
internal/ingest/pod_test.go                      fixture loader, scenario table
internal/ingest/job.go                           DiffJob, JobChanges
internal/ingest/job_test.go
internal/ingest/replicaset.go                    MapReplicaSet
internal/ingest/replicaset_test.go
internal/ingest/event.go                         MapEvent, EarlyCaptureReason
internal/ingest/event_test.go
internal/ingest/testdata/<scenario>/*.json
internal/incident/category.go                    ContainerCategory, ConditionCategory, PodReasonCategory, EventCategory
internal/incident/category_test.go               mapping table + prohibition test
internal/incident/ops.go                         Ops, Open, Attach, Close, EventRef
internal/incident/apply.go                       Apply (pods)
internal/incident/apply_test.go                  scenario table through ingest.DiffPod
internal/incident/job.go                         ApplyJob
internal/incident/job_test.go
internal/incident/event.go                       FieldPathContainer, EventMatches, ApplyEvent
internal/incident/event_test.go
docs/plans/m1-recorder/roadmap.md              Phase 2 status
```

Every test below traces to a spec line; the trace is given next to it. If an executor thinks a test is missing, they add it with its trace; if they cannot name a trace, they do not add it.

---

### Task 1: Module deps, shared types, container mapping

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/ingest/types.go`, `internal/ingest/time.go`, `internal/ingest/containers.go`
- Test: `internal/ingest/containers_test.go`

**Interfaces:**
- Consumes: `store.Container`, `store.ContainerKind*`, `store.State*`, `clock.Format`.
- Produces: `type PodSnapshot`, `type PodChanges`, `type DeadInstance`, `type OwnerResolver`; `func k8sTime(t metav1.Time) string`, `func k8sTimePtr(t *metav1.Time) *string`; `func mapContainers(pod *corev1.Pod, now string) []store.Container`; `func imageTag(image string) *string`.

Tests and their trace:
- `TestContainerKindFollowsSpecList`: storage doc 5.5 `kind` column; client-go doc 3.5 (`sidecar` = init container with `RestartPolicy Always`).
- `TestContainerStateFromStatus`: client-go doc 3.5 (`State` has exactly one of Waiting/Running/Terminated; a spec container with no status entry yet is `waiting` with no reason) and storage doc 5.5 (`last_terminated_*` from `lastState`, `signal` stored).
- `TestImageTagParsing`: storage doc 5.5 `image_tag` "parsed from image; null for digest-pinned images".
- `TestResourceQuantitiesParsed`: client-go doc 3.5 (`MilliValue` / `Value`, text kept as written).

- [ ] **Step 1: Add the dependencies**

```bash
go get k8s.io/api@v0.34.1 k8s.io/apimachinery@v0.34.1
```

`go.mod` must still say `go 1.24.0`. If `go get` tries to raise it, the version is wrong; use exactly `v0.34.1`.

- [ ] **Step 2: Write the shared types**

`internal/ingest/types.go`:

```go
// Package ingest turns Kubernetes objects plus the store's snapshot rows into
// the row values and history rows to write. It has no SQL and no network.
package ingest

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/store"
)

// PodSnapshot is what the store holds for a pod before the current event.
// Conditions holds the latest row per condition type.
type PodSnapshot struct {
	Pod        store.Pod
	Containers []store.Container
	Conditions []store.PodCondition
}

// PodChanges is everything DiffPod decided for one pod event. History rows
// carry nil Category and IncidentID; the incident package fills them.
type PodChanges struct {
	FirstSight       bool
	Pod              store.Pod
	Containers       []store.Container
	History          []store.ContainerStateHistory
	Conditions       []store.PodCondition
	PodReasonChanged bool
	WorkloadChanged  bool
	DeadInstances    []DeadInstance
}

// DeadInstance is a container instance that died since the snapshot. Index
// is the dead-instance index; Previous says whether the kubelet holds it in
// lastState (true) or in state.terminated (false). Unobservable marks a
// middle instance of a restart_count jump that no log call can reach.
type DeadInstance struct {
	Container    string
	Index        int64
	Previous     bool
	Unobservable bool
}

// OwnerResolver answers owner lookups from the informer stores. A nil result
// means the object is not in the local store or has no controller.
type OwnerResolver interface {
	ReplicaSetOwner(namespace, name string) *metav1.OwnerReference
	JobOwner(namespace, name string) *metav1.OwnerReference
}
```

`internal/ingest/time.go`:

```go
package ingest

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/clock"
)

func k8sTime(t metav1.Time) string {
	return clock.Format(t.Time)
}

func k8sTimePtr(t *metav1.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := clock.Format(t.Time)
	return &s
}
```

- [ ] **Step 3: Write the failing tests**

`internal/ingest/containers_test.go`:

```go
package ingest

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func TestContainerKindFollowsSpecList(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "p1"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init-db", Image: "busybox:1.36"},
				{Name: "proxy", Image: "envoy:1.30", RestartPolicy: &always},
			},
			Containers:          []corev1.Container{{Name: "api", Image: "web:1"}},
			EphemeralContainers: []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug", Image: "busybox:1.36"}}},
		},
	}
	var got []struct{ Name, Kind string }
	for _, c := range mapContainers(pod, clock.Format(testNow)) {
		got = append(got, struct{ Name, Kind string }{c.Name, c.Kind})
	}
	want := []struct{ Name, Kind string }{
		{"init-db", store.ContainerKindInit}, {"proxy", store.ContainerKindSidecar},
		{"api", store.ContainerKindApp}, {"debug", store.ContainerKindEphemeral},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}

func TestContainerStateFromStatus(t *testing.T) {
	now := clock.Format(testNow)
	started := metav1.Date(2026, 8, 27, 11, 50, 0, 0, time.UTC)
	finished := metav1.Date(2026, 8, 27, 11, 55, 0, 0, time.UTC)
	base := store.Container{PodUID: "p1", Name: "api", Kind: store.ContainerKindApp, Image: "web:1.4.2", ImageTag: ptr("1.4.2"), UpdatedAt: now}
	cases := []struct {
		name   string
		status *corev1.ContainerStatus
		want   store.Container
	}{
		{"no status yet is waiting without reason", nil, func() store.Container {
			c := base
			c.State = store.StateWaiting
			return c
		}()},
		{"running", &corev1.ContainerStatus{
			Name: "api", Ready: true, RestartCount: 2, ImageID: "web@sha256:aa", ContainerID: "containerd://c2",
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}},
		}, func() store.Container {
			c := base
			c.ImageID, c.ContainerID = ptr("web@sha256:aa"), ptr("containerd://c2")
			c.State, c.Ready, c.RestartCount, c.RunningSince = store.StateRunning, true, 2, ptr(k8sTime(started))
			return c
		}()},
		{"waiting with last termination", &corev1.ContainerStatus{
			Name: "api", RestartCount: 1, ImageID: "web@sha256:aa", ContainerID: "containerd://c1",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off 10s"}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 137, Signal: 9, Reason: "OOMKilled", StartedAt: started, FinishedAt: finished, ContainerID: "containerd://c1"}},
		}, func() store.Container {
			c := base
			c.ImageID, c.ContainerID = ptr("web@sha256:aa"), ptr("containerd://c1")
			c.State, c.Reason, c.RestartCount = store.StateWaiting, ptr("CrashLoopBackOff"), 1
			c.LastTerminatedReason, c.LastTerminatedExitCode, c.LastTerminatedSignal = ptr("OOMKilled"), ptr[int64](137), ptr[int64](9)
			c.LastTerminatedAt = ptr(k8sTime(finished))
			return c
		}()},
		{"terminated", &corev1.ContainerStatus{
			Name: "api", ImageID: "web@sha256:aa", ContainerID: "containerd://c0",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error", StartedAt: started, FinishedAt: finished}},
		}, func() store.Container {
			c := base
			c.ImageID, c.ContainerID = ptr("web@sha256:aa"), ptr("containerd://c0")
			c.State, c.Reason, c.ExitCode, c.Signal = store.StateTerminated, ptr("Error"), ptr[int64](1), ptr[int64](0)
			return c
		}()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{UID: "p1"},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "web:1.4.2"}}},
			}
			if c.status != nil {
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{*c.status}
			}
			got := mapContainers(pod, now)
			if d := cmp.Diff([]store.Container{c.want}, got); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestImageTagParsing(t *testing.T) {
	cases := []struct {
		image string
		want  *string
	}{
		{"web:1.4.2", ptr("1.4.2")},
		{"registry.example.com:5000/team/web:1.4.2", ptr("1.4.2")},
		{"registry.example.com:5000/team/web", nil},
		{"web@sha256:0123456789abcdef", nil},
		{"web:1.4.2@sha256:0123456789abcdef", ptr("1.4.2")},
		{"busybox", nil},
	}
	for _, c := range cases {
		if d := cmp.Diff(c.want, imageTag(c.image)); d != "" {
			t.Errorf("imageTag(%q): %s", c.image, d)
		}
	}
}

func TestResourceQuantitiesParsed(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "p1"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "api", Image: "web:1",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			},
		}}},
	}
	got := mapContainers(pod, clock.Format(testNow))[0]
	want := store.Container{
		PodUID: "p1", Name: "api", Kind: store.ContainerKindApp, Image: "web:1", ImageTag: ptr("1"),
		CPURequest: ptr("500m"), MemRequest: ptr("512Mi"), MemLimit: ptr("1Gi"),
		CPURequestMillis: ptr[int64](500), MemRequestBytes: ptr[int64](512 << 20), MemLimitBytes: ptr[int64](1 << 30),
		State: store.StateWaiting, UpdatedAt: clock.Format(testNow),
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/ingest/`
Expected: FAIL, undefined `mapContainers`, `imageTag`.

- [ ] **Step 5: Implement**

`internal/ingest/containers.go`:

```go
package ingest

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"idios/internal/store"
)

// mapContainers zips the three spec lists with their status lists by name.
// Spec order is kept so rows are stable across events.
func mapContainers(pod *corev1.Pod, now string) []store.Container {
	statuses := map[string]*corev1.ContainerStatus{}
	for _, list := range [][]corev1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for i := range list {
			statuses[list[i].Name] = &list[i]
		}
	}
	var out []store.Container
	for _, c := range pod.Spec.InitContainers {
		kind := store.ContainerKindInit
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			kind = store.ContainerKindSidecar
		}
		out = append(out, mapContainer(string(pod.UID), kind, c.Name, c.Image, c.Resources, statuses[c.Name], now))
	}
	for _, c := range pod.Spec.Containers {
		out = append(out, mapContainer(string(pod.UID), store.ContainerKindApp, c.Name, c.Image, c.Resources, statuses[c.Name], now))
	}
	for _, c := range pod.Spec.EphemeralContainers {
		out = append(out, mapContainer(string(pod.UID), store.ContainerKindEphemeral, c.Name, c.Image, c.Resources, statuses[c.Name], now))
	}
	return out
}

func mapContainer(podUID, kind, name, image string, res corev1.ResourceRequirements, st *corev1.ContainerStatus, now string) store.Container {
	c := store.Container{
		PodUID: podUID, Name: name, Kind: kind, Image: image, ImageTag: imageTag(image),
		State: store.StateWaiting, UpdatedAt: now,
	}
	c.CPURequest, c.CPURequestMillis = quantityMillis(res.Requests, corev1.ResourceCPU)
	c.CPULimit, c.CPULimitMillis = quantityMillis(res.Limits, corev1.ResourceCPU)
	c.MemRequest, c.MemRequestBytes = quantityValue(res.Requests, corev1.ResourceMemory)
	c.MemLimit, c.MemLimitBytes = quantityValue(res.Limits, corev1.ResourceMemory)
	if st == nil {
		return c
	}
	c.ImageID = nonEmpty(st.ImageID)
	c.ContainerID = nonEmpty(st.ContainerID)
	c.Ready = st.Ready
	c.RestartCount = int64(st.RestartCount)
	switch {
	case st.State.Running != nil:
		c.State = store.StateRunning
		c.RunningSince = k8sTimePtr(&st.State.Running.StartedAt)
	case st.State.Terminated != nil:
		t := st.State.Terminated
		c.State = store.StateTerminated
		c.Reason = nonEmpty(t.Reason)
		c.ExitCode = ptrInt64(int64(t.ExitCode))
		c.Signal = ptrInt64(int64(t.Signal))
	case st.State.Waiting != nil:
		c.State = store.StateWaiting
		c.Reason = nonEmpty(st.State.Waiting.Reason)
	}
	if lt := st.LastTerminationState.Terminated; lt != nil {
		c.LastTerminatedReason = nonEmpty(lt.Reason)
		c.LastTerminatedExitCode = ptrInt64(int64(lt.ExitCode))
		c.LastTerminatedSignal = ptrInt64(int64(lt.Signal))
		c.LastTerminatedAt = k8sTimePtr(&lt.FinishedAt)
	}
	return c
}

// imageTag returns the tag part of an image reference. The registry may
// carry a port, so only a colon after the last slash counts, and a digest
// suffix is cut first because a tag may precede it.
func imageTag(image string) *string {
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= slash {
		return nil
	}
	tag := image[colon+1:]
	return &tag
}

func quantityMillis(list corev1.ResourceList, name corev1.ResourceName) (*string, *int64) {
	q, ok := list[name]
	if !ok {
		return nil, nil
	}
	return ptrString(q.String()), ptrInt64(q.MilliValue())
}

func quantityValue(list corev1.ResourceList, name corev1.ResourceName) (*string, *int64) {
	q, ok := list[name]
	if !ok {
		return nil, nil
	}
	return ptrString(q.String()), ptrInt64(q.Value())
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrString(s string) *string { return &s }

func ptrInt64(v int64) *int64 { return &v }

var _ = resource.Quantity{}
```

Remove the trailing `var _ = resource.Quantity{}` line if the `resource` import is otherwise used; it is not (quantities come typed from `ResourceList`), so drop both the line and the import.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ingest/ -v`
Expected: PASS. Ready for a `running` status is `true` only when the status says so; the `no status yet` row must have `Ready false` and `RestartCount 0`.

- [ ] **Step 7: Checkpoint**

Run: `go mod tidy && go build ./... && go vet ./... && go test ./... && make ascii`

`go mod tidy` keeps `k8s.io/api` and `k8s.io/apimachinery` because `ingest` imports them. Confirm `go.mod` still reads `go 1.24.0`.

Commit: `git add go.mod go.sum internal/ingest && git commit -m "ingest: map pod containers to store rows"`

---

### Task 2: `DiffPod`: pod row, workload resolution, container transitions, dead instances

**Files:**
- Create: `internal/ingest/pod.go`
- Create: fixtures under `internal/ingest/testdata/` (listed in Step 1)
- Test: `internal/ingest/pod_test.go`

**Interfaces:**
- Consumes: `mapContainers`, `imageTag`, `k8sTime`, `k8sTimePtr`, `PodSnapshot`, `PodChanges`, `DeadInstance`, `OwnerResolver`, `store.Pod`, `store.ContainerStateHistory`, `clock.Format`.
- Produces: `func DiffPod(snap *PodSnapshot, pod *corev1.Pod, clusterID int64, r OwnerResolver, now time.Time) PodChanges`; `func statusIndex(pod *corev1.Pod) map[string]*corev1.ContainerStatus`; `func deadIndex(c store.Container) int64`; test helpers `loadPod`, `runScenario`, `snapshotFrom`, `fakeResolver`. Conditions and `PodReasonChanged` stay zero until Task 3.

Tests and their trace (process doc 14.1 bullets; storage doc Section 4 rules):
- `TestDiffPodScenarios`, one row each:
  - `crash-loop`: "running -> waiting(CrashLoopBackOff) with restart_count +1 and no terminated state in between" -> one history row, one dead instance with `Previous = true`.
  - `oom`: "lastState.terminated.reason = OOMKilled, exit 137, signal 9" -> history row carries the waiting state; the container row carries the last termination.
  - `oom-exit-zero`: "OOM with exit code 0 (seen in the wild)" -> the terminated row stores exit 0 and reason OOMKilled verbatim.
  - `image-pull`: "ErrImagePull then ImagePullBackOff then Pulled and running", three snapshots chained -> the last step yields exactly one `running` row.
  - `config`: "CreateContainerConfigError" after `ContainerCreating` -> one waiting row, no dead instance (storage doc 4: "first sight with a differing snapshot row" is this shape).
  - `init-then-success`: "init container failure, then success with ready = true on a terminated init container" -> `init-db` row terminated Completed, app row running; the completed init instance is a dead instance with `Previous = false`.
  - `sidecar`: "sidecar init container running alongside app containers", first sight -> kinds `sidecar` and `app`, no history.
  - `multi-container`: "multi-container pod where only one container fails" -> exactly one history row.
  - `restart-jump`: "restart_count jump of 3 between two snapshots" -> a `gap_reconstructed` row for `lastState`, a normal row for the current state, one unobservable dead instance plus the observed one.
  - `job-never-error`: "restartPolicy: Never Job pod whose container ends in state.terminated with reason Error" -> dead instance index 0, `Previous = false`.
  - `dead-index-running`: "waiting(CrashLoopBackOff) with restart_count = 1 -> index 1; running with restart_count = 2 -> index 1" -> the running step reports index 1.
  - `first-sight-healthy` and `first-sight-config-error`: "first sight with no snapshot row" -> `FirstSight = true`, no history row either way.
- `TestWorkloadResolution`: storage doc 5.3 `workload_*` ("ReplicaSet -> Deployment owner chain, and a bare ReplicaSet", "Job -> CronJob owner chain, and a Job pod with no CronJob owner", fallback to `controller_*`, `WorkloadChanged` when a later event resolves the chain).
- `TestPodRowMapsEveryField`: client-go doc 3.3 column table, one rich fixture compared as a whole row, plus `first_seen_at` kept from the snapshot.

- [ ] **Step 1: Write the fixtures**

All files are `internal/ingest/testdata/<scenario>/<file>.json`. Every pod is in namespace `idios-smoke`. Fixtures omit `conditions` unless the scenario is about them (Task 3).

`crash-loop/before.json`:

```json
{"metadata":{"name":"web-7d9f8c6b5-abcde","namespace":"idios-smoke","uid":"pod-crash","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","name":"web-7d9f8c6b5","uid":"rs-web-1","controller":true}]},
 "spec":{"nodeName":"node-a","restartPolicy":"Always","containers":[{"name":"api","image":"registry.example.com/web:1.4.2"}]},
 "status":{"phase":"Running","qosClass":"BestEffort","startTime":"2026-08-27T11:45:05Z",
  "containerStatuses":[{"name":"api","image":"registry.example.com/web:1.4.2","imageID":"registry.example.com/web@sha256:1111","containerID":"containerd://aaa","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-08-27T11:45:08Z"}}}]}}
```

`crash-loop/after.json`:

```json
{"metadata":{"name":"web-7d9f8c6b5-abcde","namespace":"idios-smoke","uid":"pod-crash","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","name":"web-7d9f8c6b5","uid":"rs-web-1","controller":true}]},
 "spec":{"nodeName":"node-a","restartPolicy":"Always","containers":[{"name":"api","image":"registry.example.com/web:1.4.2"}]},
 "status":{"phase":"Running","qosClass":"BestEffort","startTime":"2026-08-27T11:45:05Z",
  "containerStatuses":[{"name":"api","image":"registry.example.com/web:1.4.2","imageID":"registry.example.com/web@sha256:1111","containerID":"containerd://aaa","ready":false,"restartCount":1,
   "state":{"waiting":{"reason":"CrashLoopBackOff","message":"back-off 10s restarting failed container=api pod=web-7d9f8c6b5-abcde_idios-smoke(pod-crash)"}},
   "lastState":{"terminated":{"exitCode":1,"reason":"Error","startedAt":"2026-08-27T11:45:08Z","finishedAt":"2026-08-27T11:55:00Z","containerID":"containerd://aaa"}}}]}}
```

`oom/before.json`: identical to `crash-loop/before.json` except `"uid":"pod-oom"`, name `mem-hog-5f6d7-qwert`, ReplicaSet `mem-hog-5f6d7` uid `rs-memhog-1`, image `registry.example.com/mem-hog:2.0.0`, `imageID` `registry.example.com/mem-hog@sha256:2222`, `containerID` `containerd://mmm`, `qosClass` `Burstable`, and the container has `"resources":{"limits":{"memory":"10Mi"},"requests":{"memory":"10Mi"}}`.

`oom/after.json`: the same pod with `"ready":false,"restartCount":1`, `"state":{"waiting":{"reason":"CrashLoopBackOff","message":"back-off 10s restarting failed container=api pod=mem-hog-5f6d7-qwert_idios-smoke(pod-oom)"}}`, `"lastState":{"terminated":{"exitCode":137,"signal":9,"reason":"OOMKilled","startedAt":"2026-08-27T11:45:08Z","finishedAt":"2026-08-27T11:52:30Z","containerID":"containerd://mmm"}}`.

`oom-exit-zero/before.json`: a Job pod:

```json
{"metadata":{"name":"report-28812345-x9k2p","namespace":"idios-smoke","uid":"pod-oom0","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"batch/v1","kind":"Job","name":"report-28812345","uid":"job-report-1","controller":true}]},
 "spec":{"nodeName":"node-a","restartPolicy":"Never","containers":[{"name":"report","image":"registry.example.com/report:0.9.1","resources":{"limits":{"memory":"64Mi"}}}]},
 "status":{"phase":"Running","qosClass":"Burstable","startTime":"2026-08-27T11:45:05Z",
  "containerStatuses":[{"name":"report","image":"registry.example.com/report:0.9.1","imageID":"registry.example.com/report@sha256:3333","containerID":"containerd://rrr","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-08-27T11:45:08Z"}}}]}}
```

`oom-exit-zero/after.json`: same pod, `"phase":"Failed"`, container `"ready":false,"restartCount":0`, `"state":{"terminated":{"exitCode":0,"signal":9,"reason":"OOMKilled","startedAt":"2026-08-27T11:45:08Z","finishedAt":"2026-08-27T11:58:00Z","containerID":"containerd://rrr"}}`, no `lastState`.

`image-pull/s1.json`:

```json
{"metadata":{"name":"web-66c9d-pull1","namespace":"idios-smoke","uid":"pod-pull","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","name":"web-66c9d","uid":"rs-web-2","controller":true}]},
 "spec":{"nodeName":"node-a","restartPolicy":"Always","containers":[{"name":"api","image":"registry.example.com/web:does-not-exist"}]},
 "status":{"phase":"Pending","qosClass":"BestEffort","startTime":"2026-08-27T11:45:05Z",
  "containerStatuses":[{"name":"api","image":"registry.example.com/web:does-not-exist","imageID":"","ready":false,"restartCount":0,"state":{"waiting":{"reason":"ErrImagePull","message":"rpc error: code = NotFound desc = failed to pull and unpack image"}}}]}}
```

`image-pull/s2.json`: same with `"state":{"waiting":{"reason":"ImagePullBackOff","message":"Back-off pulling image \"registry.example.com/web:does-not-exist\""}}`.

`image-pull/s3.json`: same with `"phase":"Running"`, `"imageID":"registry.example.com/web@sha256:4444","containerID":"containerd://ppp","ready":true`, `"state":{"running":{"startedAt":"2026-08-27T11:59:00Z"}}`.

`config/before.json`:

```json
{"metadata":{"name":"web-5b8c7-cfg01","namespace":"idios-smoke","uid":"pod-cfg","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","name":"web-5b8c7","uid":"rs-web-3","controller":true}]},
 "spec":{"nodeName":"node-a","restartPolicy":"Always","containers":[{"name":"api","image":"registry.example.com/web:1.4.2"}]},
 "status":{"phase":"Pending","qosClass":"BestEffort","startTime":"2026-08-27T11:45:05Z",
  "containerStatuses":[{"name":"api","image":"registry.example.com/web:1.4.2","imageID":"","ready":false,"restartCount":0,"state":{"waiting":{"reason":"ContainerCreating"}}}]}}
```

`config/after.json`: same with `"state":{"waiting":{"reason":"CreateContainerConfigError","message":"configmap \"web-settings\" not found"}}`.

`init-then-success/before.json`:

```json
{"metadata":{"name":"web-9a8b7-init1","namespace":"idios-smoke","uid":"pod-init","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","name":"web-9a8b7","uid":"rs-web-4","controller":true}]},
 "spec":{"nodeName":"node-a","restartPolicy":"Always","initContainers":[{"name":"init-db","image":"registry.example.com/migrate:3.1.0"}],"containers":[{"name":"api","image":"registry.example.com/web:1.4.2"}]},
 "status":{"phase":"Pending","qosClass":"BestEffort","startTime":"2026-08-27T11:45:05Z",
  "initContainerStatuses":[{"name":"init-db","image":"registry.example.com/migrate:3.1.0","imageID":"registry.example.com/migrate@sha256:5555","containerID":"containerd://iii0","ready":false,"restartCount":0,"state":{"terminated":{"exitCode":1,"reason":"Error","startedAt":"2026-08-27T11:45:08Z","finishedAt":"2026-08-27T11:45:20Z","containerID":"containerd://iii0"}}}],
  "containerStatuses":[{"name":"api","image":"registry.example.com/web:1.4.2","imageID":"","ready":false,"restartCount":0,"state":{"waiting":{"reason":"PodInitializing"}}}]}}
```

`init-then-success/after.json`: `"phase":"Running"`; init status `"containerID":"containerd://iii1","ready":true,"restartCount":1,"state":{"terminated":{"exitCode":0,"reason":"Completed","startedAt":"2026-08-27T11:45:40Z","finishedAt":"2026-08-27T11:45:50Z","containerID":"containerd://iii1"}},"lastState":{"terminated":{"exitCode":1,"reason":"Error","startedAt":"2026-08-27T11:45:08Z","finishedAt":"2026-08-27T11:45:20Z","containerID":"containerd://iii0"}}`; app status `"imageID":"registry.example.com/web@sha256:1111","containerID":"containerd://aaa2","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-08-27T11:46:00Z"}}`.

`sidecar/pod.json`:

```json
{"metadata":{"name":"web-1c2d3-side1","namespace":"idios-smoke","uid":"pod-side","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","name":"web-1c2d3","uid":"rs-web-5","controller":true}]},
 "spec":{"nodeName":"node-a","restartPolicy":"Always","initContainers":[{"name":"proxy","image":"registry.example.com/envoy:1.30.0","restartPolicy":"Always"}],"containers":[{"name":"api","image":"registry.example.com/web:1.4.2"}]},
 "status":{"phase":"Running","qosClass":"BestEffort","startTime":"2026-08-27T11:45:05Z",
  "initContainerStatuses":[{"name":"proxy","image":"registry.example.com/envoy:1.30.0","imageID":"registry.example.com/envoy@sha256:6666","containerID":"containerd://sss","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-08-27T11:45:06Z"}}}],
  "containerStatuses":[{"name":"api","image":"registry.example.com/web:1.4.2","imageID":"registry.example.com/web@sha256:1111","containerID":"containerd://aaa3","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-08-27T11:45:08Z"}}}]}}
```

`multi-container/before.json`:

```json
{"metadata":{"name":"web-4e5f6-multi","namespace":"idios-smoke","uid":"pod-multi","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","name":"web-4e5f6","uid":"rs-web-6","controller":true}]},
 "spec":{"nodeName":"node-a","restartPolicy":"Always","containers":[{"name":"api","image":"registry.example.com/web:1.4.2"},{"name":"worker","image":"registry.example.com/worker:1.4.2"}]},
 "status":{"phase":"Running","qosClass":"BestEffort","startTime":"2026-08-27T11:45:05Z",
  "containerStatuses":[
   {"name":"api","image":"registry.example.com/web:1.4.2","imageID":"registry.example.com/web@sha256:1111","containerID":"containerd://aaa4","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-08-27T11:45:08Z"}}},
   {"name":"worker","image":"registry.example.com/worker:1.4.2","imageID":"registry.example.com/worker@sha256:7777","containerID":"containerd://www","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-08-27T11:45:09Z"}}}]}}
```

`multi-container/after.json`: `api` unchanged; `worker` becomes `"ready":false,"restartCount":1,"state":{"waiting":{"reason":"CrashLoopBackOff","message":"back-off 10s restarting failed container=worker pod=web-4e5f6-multi_idios-smoke(pod-multi)"}},"lastState":{"terminated":{"exitCode":2,"reason":"Error","startedAt":"2026-08-27T11:45:09Z","finishedAt":"2026-08-27T11:57:00Z","containerID":"containerd://www"}}`.

`restart-jump/before.json`: the `crash-loop` pod (`uid` `pod-jump`, name `web-7d9f8c6b5-jump1`) with `"ready":false,"restartCount":1,"state":{"waiting":{"reason":"CrashLoopBackOff"}},"lastState":{"terminated":{"exitCode":1,"reason":"Error","startedAt":"2026-08-27T11:39:00Z","finishedAt":"2026-08-27T11:40:00Z","containerID":"containerd://j1"}}`, `"containerID":"containerd://j1"`.

`restart-jump/after.json`: same pod with `"ready":true,"restartCount":4,"containerID":"containerd://j4","state":{"running":{"startedAt":"2026-08-27T11:59:00Z"}},"lastState":{"terminated":{"exitCode":1,"reason":"Error","startedAt":"2026-08-27T11:57:00Z","finishedAt":"2026-08-27T11:58:00Z","containerID":"containerd://j3"}}`.

`job-never-error/before.json`: the `oom-exit-zero` pod with `uid` `pod-jobfail`, name `import-28812346-b7c3d`, Job `import-28812346` uid `job-import-1`, container `worker`, image `registry.example.com/import:5.2.0`, imageID `...import@sha256:8888`, containerID `containerd://jjj`, no resources, running rc 0.

`job-never-error/after.json`: `"phase":"Failed"`, container `"ready":false,"restartCount":0,"state":{"terminated":{"exitCode":1,"reason":"Error","startedAt":"2026-08-27T11:45:08Z","finishedAt":"2026-08-27T11:50:00Z","containerID":"containerd://jjj"}}`.

`dead-index-running/before.json`: the `crash-loop/after.json` pod with `uid` `pod-idx`, name `web-7d9f8c6b5-idx01`, `containerID` `containerd://k1`, lastState `containerID` `containerd://k1`, `finishedAt` `2026-08-27T11:55:00Z`.

`dead-index-running/after.json`: same pod, `"ready":true,"restartCount":2,"containerID":"containerd://k2","state":{"running":{"startedAt":"2026-08-27T11:56:00Z"}}`, `lastState` unchanged from before.

`first-sight-healthy/pod.json`: `crash-loop/before.json` with `uid` `pod-fresh`, name `web-7d9f8c6b5-fresh`, `containerID` `containerd://fff`.

`first-sight-config-error/pod.json`: `config/after.json` with `uid` `pod-cfg-first`, name `web-5b8c7-cfg02`.

Owner-chain fixtures for `TestWorkloadResolution` are built in Go, not JSON (they are three lines of `ObjectMeta`).

- [ ] **Step 2: Write the failing tests**

`internal/ingest/pod_test.go`:

```go
package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

type fakeResolver struct {
	rs   map[string]*metav1.OwnerReference
	jobs map[string]*metav1.OwnerReference
}

func (f fakeResolver) ReplicaSetOwner(ns, name string) *metav1.OwnerReference { return f.rs[ns+"/"+name] }
func (f fakeResolver) JobOwner(ns, name string) *metav1.OwnerReference        { return f.jobs[ns+"/"+name] }

func owner(kind, name, uid string) *metav1.OwnerReference {
	return &metav1.OwnerReference{Kind: kind, Name: name, UID: types.UID(uid)}
}

func loadPod(t *testing.T, path string) *corev1.Pod {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var pod corev1.Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &pod
}

// snapshotFrom is what the processor will persist and reload between two
// events: the upserted rows plus the latest condition row per type.
func snapshotFrom(prev *PodSnapshot, ch PodChanges) *PodSnapshot {
	byType := map[string]store.PodCondition{}
	var order []string
	if prev != nil {
		for _, c := range prev.Conditions {
			byType[c.Type] = c
			order = append(order, c.Type)
		}
	}
	for _, c := range ch.Conditions {
		if _, seen := byType[c.Type]; !seen {
			order = append(order, c.Type)
		}
		byType[c.Type] = c
	}
	snap := &PodSnapshot{Pod: ch.Pod, Containers: ch.Containers}
	for _, ty := range order {
		snap.Conditions = append(snap.Conditions, byType[ty])
	}
	return snap
}

func runScenario(t *testing.T, r OwnerResolver, files ...string) PodChanges {
	t.Helper()
	var snap *PodSnapshot
	var ch PodChanges
	for _, f := range files {
		ch = DiffPod(snap, loadPod(t, f), 1, r, testNow)
		snap = snapshotFrom(snap, ch)
	}
	return ch
}

func TestDiffPodScenarios(t *testing.T) {
	now := clock.Format(testNow)
	web := "registry.example.com/web:1.4.2"
	webID := ptr("registry.example.com/web@sha256:1111")
	ts := func(s string) *string { return ptr(s) }
	cases := []struct {
		name  string
		files []string
		want  PodChanges
	}{
		{"crash-loop", []string{"crash-loop/before.json", "crash-loop/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-crash", ContainerName: "api", Image: web, ImageID: webID, ContainerID: ptr("containerd://aaa"),
				State: store.StateWaiting, Reason: ptr("CrashLoopBackOff"), RestartCount: 1, ObservedAt: now,
			}},
			DeadInstances: []DeadInstance{{Container: "api", Index: 1, Previous: true}},
		}},
		{"oom", []string{"oom/before.json", "oom/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-oom", ContainerName: "api", Image: "registry.example.com/mem-hog:2.0.0", ImageID: ptr("registry.example.com/mem-hog@sha256:2222"), ContainerID: ptr("containerd://mmm"),
				State: store.StateWaiting, Reason: ptr("CrashLoopBackOff"), RestartCount: 1, ObservedAt: now,
			}},
			DeadInstances: []DeadInstance{{Container: "api", Index: 1, Previous: true}},
		}},
		{"oom-exit-zero", []string{"oom-exit-zero/before.json", "oom-exit-zero/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-oom0", ContainerName: "report", Image: "registry.example.com/report:0.9.1", ImageID: ptr("registry.example.com/report@sha256:3333"), ContainerID: ptr("containerd://rrr"),
				State: store.StateTerminated, Reason: ptr("OOMKilled"), ExitCode: ptr[int64](0), Signal: ptr[int64](9), RestartCount: 0,
				K8sStartedAt: ts("2026-08-27T11:45:08.000000Z"), K8sFinishedAt: ts("2026-08-27T11:58:00.000000Z"), ObservedAt: now,
			}},
			PodReasonChanged: true,
			DeadInstances:    []DeadInstance{{Container: "report", Index: 0, Previous: false}},
		}},
		{"image-pull", []string{"image-pull/s1.json", "image-pull/s2.json", "image-pull/s3.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-pull", ContainerName: "api", Image: "registry.example.com/web:does-not-exist", ImageID: ptr("registry.example.com/web@sha256:4444"), ContainerID: ptr("containerd://ppp"),
				State: store.StateRunning, RestartCount: 0, K8sStartedAt: ts("2026-08-27T11:59:00.000000Z"), ObservedAt: now,
			}},
			PodReasonChanged: true,
		}},
		{"config", []string{"config/before.json", "config/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-cfg", ContainerName: "api", Image: web,
				State: store.StateWaiting, Reason: ptr("CreateContainerConfigError"), RestartCount: 0, ObservedAt: now,
			}},
		}},
		{"init-then-success", []string{"init-then-success/before.json", "init-then-success/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{
				{
					PodUID: "pod-init", ContainerName: "init-db", Image: "registry.example.com/migrate:3.1.0", ImageID: ptr("registry.example.com/migrate@sha256:5555"), ContainerID: ptr("containerd://iii1"),
					State: store.StateTerminated, Reason: ptr("Completed"), ExitCode: ptr[int64](0), Signal: ptr[int64](0), RestartCount: 1,
					K8sStartedAt: ts("2026-08-27T11:45:40.000000Z"), K8sFinishedAt: ts("2026-08-27T11:45:50.000000Z"), ObservedAt: now,
				},
				{
					PodUID: "pod-init", ContainerName: "api", Image: web, ImageID: webID, ContainerID: ptr("containerd://aaa2"),
					State: store.StateRunning, RestartCount: 0, K8sStartedAt: ts("2026-08-27T11:46:00.000000Z"), ObservedAt: now,
				},
			},
			PodReasonChanged: true,
			DeadInstances:    []DeadInstance{{Container: "init-db", Index: 1, Previous: false}},
		}},
		{"sidecar", []string{"sidecar/pod.json"}, PodChanges{FirstSight: true}},
		{"multi-container", []string{"multi-container/before.json", "multi-container/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-multi", ContainerName: "worker", Image: "registry.example.com/worker:1.4.2", ImageID: ptr("registry.example.com/worker@sha256:7777"), ContainerID: ptr("containerd://www"),
				State: store.StateWaiting, Reason: ptr("CrashLoopBackOff"), RestartCount: 1, ObservedAt: now,
			}},
			DeadInstances: []DeadInstance{{Container: "worker", Index: 1, Previous: true}},
		}},
		{"restart-jump", []string{"restart-jump/before.json", "restart-jump/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{
				{
					PodUID: "pod-jump", ContainerName: "api", Image: web, ImageID: webID, ContainerID: ptr("containerd://j3"),
					State: store.StateTerminated, Reason: ptr("Error"), ExitCode: ptr[int64](1), Signal: ptr[int64](0), RestartCount: 3,
					K8sStartedAt: ts("2026-08-27T11:57:00.000000Z"), K8sFinishedAt: ts("2026-08-27T11:58:00.000000Z"), ObservedAt: now, GapReconstructed: true,
				},
				{
					PodUID: "pod-jump", ContainerName: "api", Image: web, ImageID: webID, ContainerID: ptr("containerd://j4"),
					State: store.StateRunning, RestartCount: 4, K8sStartedAt: ts("2026-08-27T11:59:00.000000Z"), ObservedAt: now,
				},
			},
			DeadInstances: []DeadInstance{
				{Container: "api", Index: 2, Previous: true, Unobservable: true},
				{Container: "api", Index: 3, Previous: true},
			},
		}},
		{"job-never-error", []string{"job-never-error/before.json", "job-never-error/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-jobfail", ContainerName: "worker", Image: "registry.example.com/import:5.2.0", ImageID: ptr("registry.example.com/import@sha256:8888"), ContainerID: ptr("containerd://jjj"),
				State: store.StateTerminated, Reason: ptr("Error"), ExitCode: ptr[int64](1), Signal: ptr[int64](0), RestartCount: 0,
				K8sStartedAt: ts("2026-08-27T11:45:08.000000Z"), K8sFinishedAt: ts("2026-08-27T11:50:00.000000Z"), ObservedAt: now,
			}},
			PodReasonChanged: true,
			DeadInstances:    []DeadInstance{{Container: "worker", Index: 0, Previous: false}},
		}},
		{"dead-index-running", []string{"dead-index-running/before.json", "dead-index-running/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-idx", ContainerName: "api", Image: web, ImageID: webID, ContainerID: ptr("containerd://k2"),
				State: store.StateRunning, RestartCount: 2, K8sStartedAt: ts("2026-08-27T11:56:00.000000Z"), ObservedAt: now,
			}},
			DeadInstances: []DeadInstance{{Container: "api", Index: 1, Previous: true}},
		}},
		{"first-sight-healthy", []string{"first-sight-healthy/pod.json"}, PodChanges{FirstSight: true}},
		{"first-sight-config-error", []string{"first-sight-config-error/pod.json"}, PodChanges{FirstSight: true}},
	}
	ignore := cmpopts.IgnoreFields(PodChanges{}, "Pod", "Containers", "Conditions")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runScenario(t, fakeResolver{}, c.files...)
			if d := cmp.Diff(c.want, got, ignore); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestWorkloadResolution(t *testing.T) {
	type want struct {
		ControllerKind, ControllerName, ControllerUID, WorkloadKind, WorkloadName string
	}
	deploy := fakeResolver{rs: map[string]*metav1.OwnerReference{"idios-smoke/web-7d9f8c6b5": owner("Deployment", "web", "dep-web")}}
	cron := fakeResolver{jobs: map[string]*metav1.OwnerReference{"idios-smoke/report-28812345": owner("CronJob", "report", "cj-report")}}
	cases := []struct {
		name     string
		file     string
		resolver OwnerResolver
		want     want
	}{
		{"ReplicaSet to Deployment", "crash-loop/before.json", deploy, want{"ReplicaSet", "web-7d9f8c6b5", "rs-web-1", "Deployment", "web"}},
		{"bare ReplicaSet falls back to controller", "crash-loop/before.json", fakeResolver{}, want{"ReplicaSet", "web-7d9f8c6b5", "rs-web-1", "ReplicaSet", "web-7d9f8c6b5"}},
		{"Job to CronJob", "oom-exit-zero/before.json", cron, want{"Job", "report-28812345", "job-report-1", "CronJob", "report"}},
		{"Job without CronJob falls back to controller", "oom-exit-zero/before.json", fakeResolver{}, want{"Job", "report-28812345", "job-report-1", "Job", "report-28812345"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := runScenario(t, c.resolver, c.file).Pod
			got := want{p.ControllerKind, p.ControllerName, p.ControllerUID, p.WorkloadKind, p.WorkloadName}
			if got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}

	t.Run("no controller", func(t *testing.T) {
		pod := loadPod(t, "crash-loop/before.json")
		pod.OwnerReferences = nil
		p := DiffPod(nil, pod, 1, fakeResolver{}, testNow).Pod
		got := want{p.ControllerKind, p.ControllerName, p.ControllerUID, p.WorkloadKind, p.WorkloadName}
		if w := (want{"none", "", "", "none", ""}); got != w {
			t.Fatalf("got %+v, want %+v", got, w)
		}
	})

	t.Run("later resolution flags WorkloadChanged", func(t *testing.T) {
		first := DiffPod(nil, loadPod(t, "crash-loop/before.json"), 1, fakeResolver{}, testNow)
		if first.WorkloadChanged {
			t.Fatal("first sight reported WorkloadChanged")
		}
		second := DiffPod(snapshotFrom(nil, first), loadPod(t, "crash-loop/before.json"), 1, deploy, testNow)
		if !second.WorkloadChanged || second.Pod.WorkloadKind != "Deployment" || second.Pod.WorkloadName != "web" {
			t.Fatalf("WorkloadChanged = %v, workload = %s/%s", second.WorkloadChanged, second.Pod.WorkloadKind, second.Pod.WorkloadName)
		}
		third := DiffPod(snapshotFrom(nil, second), loadPod(t, "crash-loop/before.json"), 1, deploy, testNow)
		if third.WorkloadChanged {
			t.Fatal("unchanged workload reported WorkloadChanged")
		}
	})
}

func TestPodRowMapsEveryField(t *testing.T) {
	now := clock.Format(testNow)
	pod := loadPod(t, "oom-exit-zero/after.json")
	deletion := metav1.Date(2026, 8, 27, 11, 59, 0, 0, time.UTC)
	pod.DeletionTimestamp = &deletion
	pod.Status.Reason = "Evicted"
	pod.Status.Message = "The node was low on resource: memory."

	first := DiffPod(nil, loadPod(t, "oom-exit-zero/before.json"), 1, fakeResolver{}, testNow)
	earlier := clock.Format(testNow.Add(-time.Hour))
	first.Pod.FirstSeenAt = earlier
	got := DiffPod(snapshotFrom(nil, first), pod, 1, fakeResolver{}, testNow)

	want := store.Pod{
		UID: "pod-oom0", ClusterID: 1, Namespace: "idios-smoke", Name: "report-28812345-x9k2p",
		NodeName: ptr("node-a"), Phase: "Failed", StatusReason: ptr("Evicted"), StatusMessage: ptr("The node was low on resource: memory."),
		DeletionRequestedAt: ptr("2026-08-27T11:59:00.000000Z"), QOSClass: ptr("Burstable"),
		ControllerKind: "Job", ControllerName: "report-28812345", ControllerUID: "job-report-1",
		WorkloadKind: "Job", WorkloadName: "report-28812345",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: ptr("2026-08-27T11:45:05.000000Z"),
		FirstSeenAt: earlier, LastSeenAt: now,
	}
	if d := cmp.Diff(want, got.Pod); d != "" {
		t.Fatal(d)
	}
	wantContainers := []store.Container{{
		PodUID: "pod-oom0", Name: "report", Kind: store.ContainerKindApp, Image: "registry.example.com/report:0.9.1", ImageTag: ptr("0.9.1"),
		ImageID: ptr("registry.example.com/report@sha256:3333"), ContainerID: ptr("containerd://rrr"),
		MemLimit: ptr("64Mi"), MemLimitBytes: ptr[int64](64 << 20),
		State: store.StateTerminated, Reason: ptr("OOMKilled"), ExitCode: ptr[int64](0), Signal: ptr[int64](9), RestartCount: 0, UpdatedAt: now,
	}}
	if d := cmp.Diff(wantContainers, got.Containers); d != "" {
		t.Fatal(d)
	}
}
```

Add `"time"` and `"k8s.io/apimachinery/pkg/types"` to the import block.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ingest/`
Expected: FAIL, undefined `DiffPod`.

- [ ] **Step 4: Implement**

`internal/ingest/pod.go`:

```go
package ingest

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

// DiffPod compares pod with the store's snapshot and returns what to write.
// A nil snap is first sight: rows are recorded, no transition is inferred.
func DiffPod(snap *PodSnapshot, pod *corev1.Pod, clusterID int64, r OwnerResolver, now time.Time) PodChanges {
	nowS := clock.Format(now)
	ch := PodChanges{
		FirstSight: snap == nil,
		Pod:        mapPod(pod, clusterID, r, nowS),
		Containers: mapContainers(pod, nowS),
	}
	if snap == nil {
		return ch
	}
	ch.Pod.FirstSeenAt = snap.Pod.FirstSeenAt
	ch.Pod.DeletedAt, ch.Pod.DeletionSource, ch.Pod.DeletionReason = snap.Pod.DeletedAt, snap.Pod.DeletionSource, snap.Pod.DeletionReason
	ch.WorkloadChanged = ch.Pod.WorkloadKind != snap.Pod.WorkloadKind || ch.Pod.WorkloadName != snap.Pod.WorkloadName
	ch.PodReasonChanged = ch.Pod.Phase != snap.Pod.Phase || !equalPtr(ch.Pod.StatusReason, snap.Pod.StatusReason)

	prev := map[string]store.Container{}
	for _, c := range snap.Containers {
		prev[c.Name] = c
	}
	statuses := statusIndex(pod)
	for _, c := range ch.Containers {
		old, ok := prev[c.Name]
		if !ok {
			continue
		}
		ch.History = append(ch.History, diffContainer(old, c, statuses[c.Name], nowS)...)
		ch.DeadInstances = append(ch.DeadInstances, deadInstances(old, c)...)
	}
	return ch
}

func mapPod(pod *corev1.Pod, clusterID int64, r OwnerResolver, nowS string) store.Pod {
	p := store.Pod{
		UID: string(pod.UID), ClusterID: clusterID, Namespace: pod.Namespace, Name: pod.Name,
		NodeName: nonEmpty(pod.Spec.NodeName), Phase: string(pod.Status.Phase),
		StatusReason: nonEmpty(pod.Status.Reason), StatusMessage: nonEmpty(pod.Status.Message),
		DeletionRequestedAt: k8sTimePtr(pod.DeletionTimestamp), QOSClass: nonEmpty(string(pod.Status.QOSClass)),
		ControllerKind: "none", WorkloadKind: "none",
		CreatedAt: k8sTime(pod.CreationTimestamp), StartedAt: k8sTimePtr(pod.Status.StartTime),
		FirstSeenAt: nowS, LastSeenAt: nowS,
	}
	if ctrl := metav1.GetControllerOf(pod); ctrl != nil {
		p.ControllerKind, p.ControllerName, p.ControllerUID = ctrl.Kind, ctrl.Name, string(ctrl.UID)
		p.WorkloadKind, p.WorkloadName = resolveWorkload(ctrl, pod.Namespace, r)
	}
	return p
}

// resolveWorkload walks one owner step through the informer stores. A miss
// falls back to the controller itself; the next event corrects it.
func resolveWorkload(ctrl *metav1.OwnerReference, ns string, r OwnerResolver) (kind, name string) {
	var owner *metav1.OwnerReference
	switch ctrl.Kind {
	case "ReplicaSet":
		owner = r.ReplicaSetOwner(ns, ctrl.Name)
	case "Job":
		owner = r.JobOwner(ns, ctrl.Name)
	}
	if owner == nil {
		return ctrl.Kind, ctrl.Name
	}
	return owner.Kind, owner.Name
}

func statusIndex(pod *corev1.Pod) map[string]*corev1.ContainerStatus {
	out := map[string]*corev1.ContainerStatus{}
	for _, list := range [][]corev1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for i := range list {
			out[list[i].Name] = &list[i]
		}
	}
	return out
}

// diffContainer returns zero, one or two history rows: a reconstructed row
// for lastState when restart_count jumped by more than one, then the row for
// the current state when the transition key differs from the snapshot.
func diffContainer(old, c store.Container, st *corev1.ContainerStatus, nowS string) []store.ContainerStateHistory {
	var rows []store.ContainerStateHistory
	if st != nil && c.RestartCount-old.RestartCount > 1 && st.LastTerminationState.Terminated != nil {
		lt := st.LastTerminationState.Terminated
		rows = append(rows, store.ContainerStateHistory{
			PodUID: c.PodUID, ContainerName: c.Name, Image: c.Image, ImageID: c.ImageID, ContainerID: nonEmpty(lt.ContainerID),
			State: store.StateTerminated, Reason: nonEmpty(lt.Reason), ExitCode: ptrInt64(int64(lt.ExitCode)), Signal: ptrInt64(int64(lt.Signal)),
			RestartCount: deadIndex(c), K8sStartedAt: k8sTimePtr(&lt.StartedAt), K8sFinishedAt: k8sTimePtr(&lt.FinishedAt),
			ObservedAt: nowS, GapReconstructed: true,
		})
	}
	if sameTransitionKey(old, c) {
		return rows
	}
	row := store.ContainerStateHistory{
		PodUID: c.PodUID, ContainerName: c.Name, Image: c.Image, ImageID: c.ImageID, ContainerID: c.ContainerID,
		State: c.State, Reason: c.Reason, ExitCode: c.ExitCode, Signal: c.Signal, RestartCount: c.RestartCount, ObservedAt: nowS,
	}
	if st != nil {
		switch {
		case st.State.Terminated != nil:
			row.K8sStartedAt = k8sTimePtr(&st.State.Terminated.StartedAt)
			row.K8sFinishedAt = k8sTimePtr(&st.State.Terminated.FinishedAt)
		case st.State.Running != nil:
			row.K8sStartedAt = k8sTimePtr(&st.State.Running.StartedAt)
		}
	}
	return append(rows, row)
}

func sameTransitionKey(a, b store.Container) bool {
	return a.State == b.State && equalPtr(a.Reason, b.Reason) && equalPtr(a.ExitCode, b.ExitCode) &&
		a.RestartCount == b.RestartCount && equalPtr(a.ImageID, b.ImageID) && equalPtr(a.ContainerID, b.ContainerID)
}

// deadIndex is the index of the newest dead instance. restart_count labels
// the newest container; while that one is running, the dead one is the
// previous index.
func deadIndex(c store.Container) int64 {
	if c.State == store.StateRunning {
		return c.RestartCount - 1
	}
	return c.RestartCount
}

// deadInstances reports a new death when the counter rose, the last
// termination time moved, or the container itself terminated. In a crash
// loop the terminated state is usually never seen, so the counter and the
// lastState timestamp are the triggers that matter.
func deadInstances(old, c store.Container) []DeadInstance {
	rose := c.RestartCount > old.RestartCount
	lastMoved := c.LastTerminatedAt != nil && !equalPtr(c.LastTerminatedAt, old.LastTerminatedAt)
	terminated := c.State == store.StateTerminated && old.State != store.StateTerminated
	if !rose && !lastMoved && !terminated {
		return nil
	}
	idx := deadIndex(c)
	previous := c.State != store.StateTerminated
	var out []DeadInstance
	if c.RestartCount-old.RestartCount > 1 {
		known := int64(-1)
		if old.LastTerminatedAt != nil {
			known = deadIndex(old)
		}
		for i := known + 1; i < idx; i++ {
			out = append(out, DeadInstance{Container: c.Name, Index: i, Previous: true, Unobservable: true})
		}
	}
	return append(out, DeadInstance{Container: c.Name, Index: idx, Previous: previous})
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
```

Remove `statusIndex` duplication from `containers.go`: change `mapContainers` to call `statusIndex(pod)` instead of building its own map.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ingest/ -v -run 'DiffPod|Workload|PodRow'`
Expected: PASS. If `restart-jump` reports two unobservable instances, `known` was computed from `RestartCount` instead of `deadIndex(old)`. If `init-then-success` has no dead instance, `rose` is being ignored when the state was already `terminated`.

- [ ] **Step 6: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/ingest && git commit -m "ingest: add DiffPod with container transitions and dead instances"`

---

### Task 3: `DiffPod`: conditions and pod-level state

**Files:**
- Modify: `internal/ingest/pod.go`
- Create: fixtures `unschedulable/`, `evicted/`, `disruption-api/`, `disruption-kubelet/`
- Test: `internal/ingest/pod_test.go` (add `TestDiffPodConditionsAndPodState`)

**Interfaces:**
- Consumes: Task 2 helpers.
- Produces: `PodChanges.Conditions` filled; `func diffConditions(prev []store.PodCondition, pod *corev1.Pod, nowS string) []store.PodCondition`.

Tests and their trace:
- `TestDiffPodConditionsAndPodState`:
  - `unschedulable` first sight: storage doc 4 ("first sight ... PodScheduled=False with Unschedulable" is a recorded state) and the decision above that conditions at first sight are rows.
  - `unschedulable` message-only change: storage doc 5.4 "message is deliberately not in the key".
  - `unschedulable` then scheduled: process doc 14.1 "pending unschedulable ... then scheduled".
  - `evicted`: 14.1 "status.reason = Evicted (the fixture also has phase = Failed)": `PodReasonChanged` and the raw reason on the row.
  - `disruption-api` / `disruption-kubelet`: 14.1 "DisruptionTarget with EvictionByEvictionAPI and with TerminationByKubelet"; the kubelet fixture also carries `status.reason = Evicted` so Task 7 can test coalescing.

- [ ] **Step 1: Write the fixtures**

`unschedulable/s1.json`:

```json
{"metadata":{"name":"web-2b3c4-pend1","namespace":"idios-smoke","uid":"pod-unsched","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","name":"web-2b3c4","uid":"rs-web-7","controller":true}]},
 "spec":{"restartPolicy":"Always","containers":[{"name":"api","image":"registry.example.com/web:1.4.2","resources":{"requests":{"cpu":"8"}}}]},
 "status":{"phase":"Pending","qosClass":"Burstable",
  "conditions":[{"type":"PodScheduled","status":"False","reason":"Unschedulable","message":"0/3 nodes are available: 3 Insufficient cpu. preemption: 0/3 nodes are available: 3 No preemption victims found for incoming pod.","lastTransitionTime":"2026-08-27T11:45:01Z"}]}}
```

`unschedulable/s2.json`: same with the message `"0/4 nodes are available: 4 Insufficient cpu. preemption: 0/4 nodes are available: 4 No preemption victims found for incoming pod."` (same `lastTransitionTime`).

`unschedulable/s3.json`: `"spec"` gains `"nodeName":"node-b"`; `"status"` becomes `"phase":"Running","qosClass":"Burstable","startTime":"2026-08-27T11:58:00Z","conditions":[{"type":"PodScheduled","status":"True","lastTransitionTime":"2026-08-27T11:58:00Z"}],"containerStatuses":[{"name":"api","image":"registry.example.com/web:1.4.2","imageID":"registry.example.com/web@sha256:1111","containerID":"containerd://uuu","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-08-27T11:58:05Z"}}}]`.

`evicted/before.json`: `crash-loop/before.json` with `uid` `pod-evict`, name `web-7d9f8c6b5-evic1`, `containerID` `containerd://eee`, and `"conditions":[{"type":"Ready","status":"True","lastTransitionTime":"2026-08-27T11:45:10Z"}]`.

`evicted/after.json`: same pod with `"phase":"Failed","reason":"Evicted","message":"The node was low on resource: memory. Threshold quantity: 100Mi, available: 52Mi. Container api was using 900Mi, request is 0, has larger consumption of memory."`, condition `Ready` `"status":"False","reason":"PodFailed"`, and the container `"ready":false,"restartCount":0,"state":{"terminated":{"exitCode":137,"signal":9,"reason":"Error","startedAt":"2026-08-27T11:45:08Z","finishedAt":"2026-08-27T11:57:30Z","containerID":"containerd://eee"}}`.

`disruption-api/before.json`: `crash-loop/before.json` with `uid` `pod-drain`, name `web-7d9f8c6b5-drain`, `containerID` `containerd://ddd`, `"conditions":[{"type":"Ready","status":"True","lastTransitionTime":"2026-08-27T11:45:10Z"}]`.

`disruption-api/after.json`: same with `"deletionTimestamp":"2026-08-27T11:59:30Z"` in `metadata`, and conditions `[{"type":"DisruptionTarget","status":"True","reason":"EvictionByEvictionAPI","message":"Eviction API: evicting","lastTransitionTime":"2026-08-27T11:59:30Z"},{"type":"Ready","status":"True","lastTransitionTime":"2026-08-27T11:45:10Z"}]`. Container unchanged.

`disruption-kubelet/before.json`: `crash-loop/before.json` with `uid` `pod-press`, name `web-7d9f8c6b5-press`, `containerID` `containerd://kkk`, `"conditions":[{"type":"Ready","status":"True","lastTransitionTime":"2026-08-27T11:45:10Z"}]`.

`disruption-kubelet/after.json`: same with `"phase":"Failed","reason":"Evicted","message":"The node was low on resource: memory."` and conditions `[{"type":"DisruptionTarget","status":"True","reason":"TerminationByKubelet","message":"The node was low on resource: memory.","lastTransitionTime":"2026-08-27T11:59:40Z"},{"type":"Ready","status":"True","lastTransitionTime":"2026-08-27T11:45:10Z"}]`. Container unchanged.

- [ ] **Step 2: Write the failing test**

Append to `internal/ingest/pod_test.go`:

```go
func TestDiffPodConditionsAndPodState(t *testing.T) {
	now := clock.Format(testNow)
	cond := func(uid, ty, status, reason string, msg *string, at string) store.PodCondition {
		return store.PodCondition{PodUID: uid, Type: ty, Status: status, Reason: reason, Message: msg, K8sTransitionAt: ptr(at), ObservedAt: now}
	}
	msg3 := "0/3 nodes are available: 3 Insufficient cpu. preemption: 0/3 nodes are available: 3 No preemption victims found for incoming pod."
	cases := []struct {
		name             string
		files            []string
		wantConditions   []store.PodCondition
		wantReasonChange bool
		wantHistoryRows  int
		wantStatusReason *string
		wantDeletionReq  *string
	}{
		{"unschedulable first sight", []string{"unschedulable/s1.json"},
			[]store.PodCondition{cond("pod-unsched", "PodScheduled", "False", "Unschedulable", ptr(msg3), "2026-08-27T11:45:01.000000Z")},
			false, 0, nil, nil},
		{"unschedulable message change is not a transition", []string{"unschedulable/s1.json", "unschedulable/s2.json"},
			nil, false, 0, nil, nil},
		{"unschedulable then scheduled", []string{"unschedulable/s1.json", "unschedulable/s2.json", "unschedulable/s3.json"},
			[]store.PodCondition{cond("pod-unsched", "PodScheduled", "True", "", nil, "2026-08-27T11:58:00.000000Z")},
			true, 1, nil, nil},
		{"evicted", []string{"evicted/before.json", "evicted/after.json"},
			[]store.PodCondition{cond("pod-evict", "Ready", "False", "PodFailed", nil, "2026-08-27T11:45:10.000000Z")},
			true, 1, ptr("Evicted"), nil},
		{"disruption by eviction api", []string{"disruption-api/before.json", "disruption-api/after.json"},
			[]store.PodCondition{cond("pod-drain", "DisruptionTarget", "True", "EvictionByEvictionAPI", ptr("Eviction API: evicting"), "2026-08-27T11:59:30.000000Z")},
			false, 0, nil, ptr("2026-08-27T11:59:30.000000Z")},
		{"disruption by kubelet", []string{"disruption-kubelet/before.json", "disruption-kubelet/after.json"},
			[]store.PodCondition{cond("pod-press", "DisruptionTarget", "True", "TerminationByKubelet", ptr("The node was low on resource: memory."), "2026-08-27T11:59:40.000000Z")},
			true, 0, ptr("Evicted"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runScenario(t, fakeResolver{}, c.files...)
			if d := cmp.Diff(c.wantConditions, got.Conditions); d != "" {
				t.Error(d)
			}
			if got.PodReasonChanged != c.wantReasonChange {
				t.Errorf("PodReasonChanged = %v, want %v", got.PodReasonChanged, c.wantReasonChange)
			}
			if len(got.History) != c.wantHistoryRows {
				t.Errorf("history rows = %d, want %d", len(got.History), c.wantHistoryRows)
			}
			if d := cmp.Diff(c.wantStatusReason, got.Pod.StatusReason); d != "" {
				t.Errorf("StatusReason: %s", d)
			}
			if d := cmp.Diff(c.wantDeletionReq, got.Pod.DeletionRequestedAt); d != "" {
				t.Errorf("DeletionRequestedAt: %s", d)
			}
		})
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/ingest/ -run Conditions`
Expected: FAIL, `Conditions` empty in every row that expects one.

- [ ] **Step 4: Implement**

In `internal/ingest/pod.go`, inside `DiffPod`: before `if snap == nil { return ch }` add

```go
	var prevConditions []store.PodCondition
	if snap != nil {
		prevConditions = snap.Conditions
	}
	ch.Conditions = diffConditions(prevConditions, pod, nowS)
```

and add:

```go
// diffConditions keys on (type, status, reason). The message is left out:
// the Unschedulable text changes with the node count and would flood the
// table; the latest wording lives on the incident instead.
func diffConditions(prev []store.PodCondition, pod *corev1.Pod, nowS string) []store.PodCondition {
	last := map[string]store.PodCondition{}
	for _, c := range prev {
		last[c.Type] = c
	}
	var rows []store.PodCondition
	for _, c := range pod.Status.Conditions {
		ty := string(c.Type)
		if p, ok := last[ty]; ok && p.Status == string(c.Status) && p.Reason == c.Reason {
			continue
		}
		rows = append(rows, store.PodCondition{
			PodUID: string(pod.UID), Type: ty, Status: string(c.Status), Reason: c.Reason,
			Message: nonEmpty(c.Message), K8sTransitionAt: k8sTimePtr(&c.LastTransitionTime), ObservedAt: nowS,
		})
	}
	return rows
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ingest/ -v`
Expected: PASS, including the Task 2 scenarios (their fixtures have no conditions, so `Conditions` stays nil and is ignored there anyway).

- [ ] **Step 6: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/ingest && git commit -m "ingest: diff pod conditions by type, status and reason"`

---

### Task 4: `DiffJob` and `MapReplicaSet`

**Files:**
- Create: `internal/ingest/job.go`, `internal/ingest/replicaset.go`
- Create: fixtures `job-failed/`, `job-complete/`, `replicaset/`
- Test: `internal/ingest/job_test.go`, `internal/ingest/replicaset_test.go`

**Interfaces:**
- Consumes: `k8sTime`, `k8sTimePtr`, `nonEmpty`, `imageTag`, `store.Job`, `store.RolloutHistory`.
- Produces: `type JobChanges struct { FirstSight bool; Job store.Job; Failed bool; Completed bool }`; `func DiffJob(snap *store.Job, job *batchv1.Job, clusterID int64, now time.Time) JobChanges`; `func MapReplicaSet(rs *appsv1.ReplicaSet, clusterID int64, now time.Time) []store.RolloutHistory`; test helpers `loadJob`, `loadReplicaSet`.

Tests and their trace:
- `TestDiffJobScenarios`: storage doc 5.8 ("read from condition_type / condition_reason, not from counter arithmetic"; `finished_at` from the `Failed` condition's transition time); process doc 14.1 "Job -> CronJob owner chain, and a Job pod with no CronJob owner" (the Job side of that chain: `cronjob_uid`); `Failed` / `Completed` fire once, on the transition, not on every later update.
- `TestMapReplicaSet`: storage doc 5.9 (one row per template container, `revision` from the annotation); 14.1 "ReplicaSet -> Deployment owner chain, and a bare ReplicaSet".

- [ ] **Step 1: Write the fixtures**

`job-failed/before.json`:

```json
{"metadata":{"name":"report-28812345","namespace":"idios-smoke","uid":"job-report-1","creationTimestamp":"2026-08-27T11:45:00Z","ownerReferences":[{"apiVersion":"batch/v1","kind":"CronJob","name":"report","uid":"cj-report","controller":true}]},
 "spec":{"backoffLimit":2,"completions":1,"parallelism":1,"template":{"spec":{"restartPolicy":"Never","containers":[{"name":"report","image":"registry.example.com/report:0.9.1"}]}}},
 "status":{"active":1,"startTime":"2026-08-27T11:45:02Z"}}
```

`job-failed/after.json`: same with `"status":{"failed":3,"startTime":"2026-08-27T11:45:02Z","conditions":[{"type":"FailureTarget","status":"True","reason":"BackoffLimitExceeded","message":"Job has reached the specified backoff limit","lastTransitionTime":"2026-08-27T11:57:00Z"},{"type":"Failed","status":"True","reason":"BackoffLimitExceeded","message":"Job has reached the specified backoff limit","lastTransitionTime":"2026-08-27T11:57:00Z"}]}`.

`job-complete/before.json`: `job-failed/before.json` with `uid` `job-report-2`, name `report-28812350`, `"startTime":"2026-08-27T11:50:02Z"`, and no `ownerReferences`.

`job-complete/after.json`: same with `"status":{"succeeded":1,"startTime":"2026-08-27T11:50:02Z","completionTime":"2026-08-27T11:52:00Z","conditions":[{"type":"SuccessCriteriaMet","status":"True","lastTransitionTime":"2026-08-27T11:52:00Z"},{"type":"Complete","status":"True","lastTransitionTime":"2026-08-27T11:52:00Z"}]}`.

`replicaset/deploy.json`:

```json
{"metadata":{"name":"web-7d9f8c6b5","namespace":"idios-smoke","uid":"rs-web-1","creationTimestamp":"2026-08-27T11:44:00Z","annotations":{"deployment.kubernetes.io/revision":"7"},"ownerReferences":[{"apiVersion":"apps/v1","kind":"Deployment","name":"web","uid":"dep-web","controller":true}]},
 "spec":{"replicas":3,"template":{"spec":{"containers":[{"name":"api","image":"registry.example.com/web:1.4.2"},{"name":"worker","image":"registry.example.com/worker@sha256:7777"}]}}}}
```

`replicaset/bare.json`:

```json
{"metadata":{"name":"standalone","namespace":"idios-smoke","uid":"rs-bare","creationTimestamp":"2026-08-27T11:44:00Z"},
 "spec":{"replicas":1,"template":{"spec":{"containers":[{"name":"app","image":"busybox:1.36"}]}}}}
```

- [ ] **Step 2: Write the failing tests**

`internal/ingest/job_test.go`:

```go
package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

func loadJob(t *testing.T, path string) *batchv1.Job {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.Job
	if err := json.Unmarshal(data, &job); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &job
}

func TestDiffJobScenarios(t *testing.T) {
	now := clock.Format(testNow)
	failedRow := store.Job{
		UID: "job-report-1", ClusterID: 1, Namespace: "idios-smoke", Name: "report-28812345",
		CronJobUID: ptr("cj-report"), CronJobName: ptr("report"), Active: 0, Succeeded: 0, Failed: 3,
		BackoffLimit: ptr[int64](2), Completions: ptr[int64](1), Parallelism: ptr[int64](1), RestartPolicy: "Never",
		ConditionType: ptr("Failed"), ConditionReason: ptr("BackoffLimitExceeded"), ConditionMessage: ptr("Job has reached the specified backoff limit"),
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: ptr("2026-08-27T11:45:02.000000Z"), FinishedAt: ptr("2026-08-27T11:57:00.000000Z"),
		FirstSeenAt: now, LastSeenAt: now,
	}
	completeRow := store.Job{
		UID: "job-report-2", ClusterID: 1, Namespace: "idios-smoke", Name: "report-28812350",
		Succeeded: 1, BackoffLimit: ptr[int64](2), Completions: ptr[int64](1), Parallelism: ptr[int64](1), RestartPolicy: "Never",
		ConditionType: ptr("Complete"),
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: ptr("2026-08-27T11:50:02.000000Z"), FinishedAt: ptr("2026-08-27T11:52:00.000000Z"),
		FirstSeenAt: now, LastSeenAt: now,
	}
	cases := []struct {
		name  string
		files []string
		want  JobChanges
	}{
		{"failed at first sight", []string{"job-failed/after.json"}, JobChanges{FirstSight: true, Job: failedRow, Failed: true}},
		{"running then failed", []string{"job-failed/before.json", "job-failed/after.json"}, JobChanges{Job: failedRow, Failed: true}},
		{"failed stays failed", []string{"job-failed/before.json", "job-failed/after.json", "job-failed/after.json"}, JobChanges{Job: failedRow}},
		{"running then complete, no cronjob", []string{"job-complete/before.json", "job-complete/after.json"}, JobChanges{Job: completeRow, Completed: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var snap *store.Job
			var got JobChanges
			for _, f := range c.files {
				got = DiffJob(snap, loadJob(t, f), 1, testNow)
				row := got.Job
				snap = &row
			}
			if d := cmp.Diff(c.want, got); d != "" {
				t.Fatal(d)
			}
		})
	}
}
```

`internal/ingest/replicaset_test.go`:

```go
package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

func loadReplicaSet(t *testing.T, path string) *appsv1.ReplicaSet {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var rs appsv1.ReplicaSet
	if err := json.Unmarshal(data, &rs); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &rs
}

func TestMapReplicaSet(t *testing.T) {
	now := clock.Format(testNow)
	cases := []struct {
		name string
		file string
		want []store.RolloutHistory
	}{
		{"deployment-owned, one row per container", "replicaset/deploy.json", []store.RolloutHistory{
			{ClusterID: 1, Namespace: "idios-smoke", DeploymentName: "web", DeploymentUID: "dep-web", ReplicaSetUID: "rs-web-1", ReplicaSetName: "web-7d9f8c6b5",
				ContainerName: "api", Image: "registry.example.com/web:1.4.2", ImageTag: ptr("1.4.2"), Revision: ptr[int64](7), FirstSeenAt: now, LastSeenAt: now},
			{ClusterID: 1, Namespace: "idios-smoke", DeploymentName: "web", DeploymentUID: "dep-web", ReplicaSetUID: "rs-web-1", ReplicaSetName: "web-7d9f8c6b5",
				ContainerName: "worker", Image: "registry.example.com/worker@sha256:7777", Revision: ptr[int64](7), FirstSeenAt: now, LastSeenAt: now},
		}},
		{"bare replicaset", "replicaset/bare.json", []store.RolloutHistory{
			{ClusterID: 1, Namespace: "idios-smoke", ReplicaSetUID: "rs-bare", ReplicaSetName: "standalone",
				ContainerName: "app", Image: "busybox:1.36", ImageTag: ptr("1.36"), FirstSeenAt: now, LastSeenAt: now},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MapReplicaSet(loadReplicaSet(t, c.file), 1, testNow)
			if d := cmp.Diff(c.want, got); d != "" {
				t.Fatal(d)
			}
		})
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ingest/ -run 'Job|ReplicaSet'`
Expected: FAIL, undefined `DiffJob`, `MapReplicaSet`.

- [ ] **Step 4: Implement**

`internal/ingest/job.go`:

```go
package ingest

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

// JobChanges is what DiffJob decided for one Job event. Failed and Completed
// are true only on the event where the condition appeared.
type JobChanges struct {
	FirstSight bool
	Job        store.Job
	Failed     bool
	Completed  bool
}

// DiffJob maps job to its row and reports terminal condition transitions.
// Success and failure come from conditions, never from the counters: failed
// can exceed backoffLimit and says nothing for OnFailure jobs.
func DiffJob(snap *store.Job, job *batchv1.Job, clusterID int64, now time.Time) JobChanges {
	nowS := clock.Format(now)
	row := store.Job{
		UID: string(job.UID), ClusterID: clusterID, Namespace: job.Namespace, Name: job.Name,
		Active: int64(job.Status.Active), Succeeded: int64(job.Status.Succeeded), Failed: int64(job.Status.Failed),
		BackoffLimit: ptrInt32(job.Spec.BackoffLimit), Completions: ptrInt32(job.Spec.Completions), Parallelism: ptrInt32(job.Spec.Parallelism),
		RestartPolicy: string(job.Spec.Template.Spec.RestartPolicy),
		CreatedAt:     k8sTime(job.CreationTimestamp), StartedAt: k8sTimePtr(job.Status.StartTime), FinishedAt: k8sTimePtr(job.Status.CompletionTime),
		FirstSeenAt: nowS, LastSeenAt: nowS,
	}
	if ctrl := metav1.GetControllerOf(job); ctrl != nil && ctrl.Kind == "CronJob" {
		row.CronJobUID, row.CronJobName = ptrString(string(ctrl.UID)), ptrString(ctrl.Name)
	}
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete, batchv1.JobFailed, batchv1.JobSuspended:
		default:
			continue
		}
		row.ConditionType, row.ConditionReason, row.ConditionMessage = ptrString(string(c.Type)), nonEmpty(c.Reason), nonEmpty(c.Message)
		if c.Type == batchv1.JobFailed && row.FinishedAt == nil {
			row.FinishedAt = k8sTimePtr(&c.LastTransitionTime)
		}
		break
	}
	ch := JobChanges{FirstSight: snap == nil, Job: row}
	var prevType *string
	if snap != nil {
		ch.Job.FirstSeenAt, ch.Job.DeletedAt = snap.FirstSeenAt, snap.DeletedAt
		prevType = snap.ConditionType
	}
	ch.Failed = became(row.ConditionType, prevType, string(batchv1.JobFailed))
	ch.Completed = became(row.ConditionType, prevType, string(batchv1.JobComplete))
	return ch
}

func became(cur, prev *string, want string) bool {
	return cur != nil && *cur == want && (prev == nil || *prev != want)
}

func ptrInt32(v *int32) *int64 {
	if v == nil {
		return nil
	}
	return ptrInt64(int64(*v))
}
```

`internal/ingest/replicaset.go`:

```go
package ingest

import (
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

// MapReplicaSet returns one rollout_history row per template container.
// Revision is read on every event because a rollback reuses the old
// ReplicaSet and bumps its annotation.
func MapReplicaSet(rs *appsv1.ReplicaSet, clusterID int64, now time.Time) []store.RolloutHistory {
	nowS := clock.Format(now)
	base := store.RolloutHistory{
		ClusterID: clusterID, Namespace: rs.Namespace, ReplicaSetUID: string(rs.UID), ReplicaSetName: rs.Name,
		FirstSeenAt: nowS, LastSeenAt: nowS,
	}
	if ctrl := metav1.GetControllerOf(rs); ctrl != nil && ctrl.Kind == "Deployment" {
		base.DeploymentName, base.DeploymentUID = ctrl.Name, string(ctrl.UID)
	}
	if v, err := strconv.ParseInt(rs.Annotations["deployment.kubernetes.io/revision"], 10, 64); err == nil {
		base.Revision = ptrInt64(v)
	}
	var rows []store.RolloutHistory
	for _, c := range rs.Spec.Template.Spec.Containers {
		row := base
		row.ContainerName, row.Image, row.ImageTag = c.Name, c.Image, imageTag(c.Image)
		rows = append(rows, row)
	}
	return rows
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ingest/ -v -run 'Job|ReplicaSet'`
Expected: PASS. `failed stays failed` must report `Failed: false`: the third event has the same condition as the snapshot.

- [ ] **Step 6: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/ingest && git commit -m "ingest: add DiffJob and MapReplicaSet"`

---

### Task 5: `MapEvent` and `EarlyCaptureReason`

**Files:**
- Create: `internal/ingest/event.go`
- Create: fixtures `event-series/event.json`, `event-legacy/event.json`
- Test: `internal/ingest/event_test.go`

**Interfaces:**
- Consumes: `clock.Format`, `store.K8sEvent`.
- Produces: `func MapEvent(ev *corev1.Event, clusterID int64) (store.K8sEvent, error)` (Category always nil; the processor sets it from `incident.EventCategory`); `func EarlyCaptureReason(reason string) bool`; test helper `loadEvent`.

Tests and their trace:
- `TestMapEventTimestampPrecedence`: storage doc 5.10 "Timestamp precedence" and process doc 14.1 "Event with only eventTime / series, and an Event with only firstTimestamp / lastTimestamp"; client-go doc 3.10 (`source_component` from `Source.Component` or `ReportingController`, `raw_json` is the marshalled struct).
- `TestEarlyCaptureReasonIgnoresType`: storage doc 6.2 rule 6 and 14.1 "Killing event with type = Normal triggers early capture": the decision takes only the reason, so `Normal` cannot filter it out. The cache behaviour on delete is Phase 5.

- [ ] **Step 1: Write the fixtures**

`event-series/event.json`:

```json
{"metadata":{"name":"web-7d9f8c6b5-abcde.18a2b3c4d5e6f7a8","namespace":"idios-smoke","uid":"ev-series-1","creationTimestamp":"2026-08-27T11:55:00Z"},
 "involvedObject":{"kind":"Pod","namespace":"idios-smoke","name":"web-7d9f8c6b5-abcde","uid":"pod-crash","fieldPath":"spec.containers{api}"},
 "reason":"BackOff","message":"Back-off restarting failed container api in pod web-7d9f8c6b5-abcde_idios-smoke(pod-crash)","type":"Warning",
 "eventTime":"2026-08-27T11:55:00.123456Z","series":{"count":5,"lastObservedTime":"2026-08-27T11:59:30.654321Z"},
 "reportingComponent":"kubelet","reportingInstance":"node-a","source":{}}
```

`event-legacy/event.json`:

```json
{"metadata":{"name":"web-66c9d-pull1.18a2b3c4d5e6f7b9","namespace":"idios-smoke","uid":"ev-legacy-1","creationTimestamp":"2026-08-27T11:45:10Z"},
 "involvedObject":{"kind":"Pod","namespace":"idios-smoke","name":"web-66c9d-pull1","uid":"pod-pull","fieldPath":"spec.containers{api}"},
 "reason":"Failed","message":"Failed to pull image \"registry.example.com/web:does-not-exist\": not found","type":"Warning",
 "firstTimestamp":"2026-08-27T11:45:10Z","lastTimestamp":"2026-08-27T11:49:40Z","count":3,
 "source":{"component":"kubelet","host":"node-a"}}
```

- [ ] **Step 2: Write the failing tests**

`internal/ingest/event_test.go`:

```go
package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"

	"idios/internal/store"
)

func loadEvent(t *testing.T, path string) *corev1.Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var ev corev1.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &ev
}

func TestMapEventTimestampPrecedence(t *testing.T) {
	cases := []struct {
		name string
		file string
		want store.K8sEvent
	}{
		{"eventTime and series win", "event-series/event.json", store.K8sEvent{
			ClusterID: 1, EventUID: "ev-series-1", Namespace: "idios-smoke", Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUID: "pod-crash", FieldPath: "spec.containers{api}",
			Reason: "BackOff", Message: "Back-off restarting failed container api in pod web-7d9f8c6b5-abcde_idios-smoke(pod-crash)",
			SourceComponent: "kubelet", Count: 5,
			FirstTS: "2026-08-27T11:55:00.123456Z", LastTS: "2026-08-27T11:59:30.654321Z",
		}},
		{"legacy timestamps and count", "event-legacy/event.json", store.K8sEvent{
			ClusterID: 1, EventUID: "ev-legacy-1", Namespace: "idios-smoke", Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-66c9d-pull1", InvolvedUID: "pod-pull", FieldPath: "spec.containers{api}",
			Reason: "Failed", Message: "Failed to pull image \"registry.example.com/web:does-not-exist\": not found",
			SourceComponent: "kubelet", Count: 3,
			FirstTS: "2026-08-27T11:45:10.000000Z", LastTS: "2026-08-27T11:49:40.000000Z",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := loadEvent(t, c.file)
			raw, err := json.Marshal(ev)
			if err != nil {
				t.Fatal(err)
			}
			c.want.RawJSON = string(raw)
			got, err := MapEvent(ev, 1)
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(c.want, got); d != "" {
				t.Fatal(d)
			}
		})
	}

	t.Run("creationTimestamp is the last resort", func(t *testing.T) {
		ev := loadEvent(t, "event-legacy/event.json")
		ev.FirstTimestamp, ev.LastTimestamp = metav1.Time{}, metav1.Time{}
		ev.Count = 0
		got, err := MapEvent(ev, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got.FirstTS != "2026-08-27T11:45:10.000000Z" || got.LastTS != "2026-08-27T11:45:10.000000Z" || got.Count != 1 {
			t.Fatalf("first=%s last=%s count=%d", got.FirstTS, got.LastTS, got.Count)
		}
	})
}

func TestEarlyCaptureReasonIgnoresType(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
	}{
		{"Killing", true}, {"Preempting", true}, {"Preempted", true}, {"Evicted", true}, {"Unhealthy", true},
		{"BackOff", false}, {"Pulled", false}, {"Scheduled", false}, {"", false},
	}
	for _, c := range cases {
		if got := EarlyCaptureReason(c.reason); got != c.want {
			t.Errorf("EarlyCaptureReason(%q) = %v, want %v", c.reason, got, c.want)
		}
	}
}
```

Add `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` to the imports.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ingest/ -run 'Event|EarlyCapture'`
Expected: FAIL, undefined `MapEvent`, `EarlyCaptureReason`.

- [ ] **Step 4: Implement**

`internal/ingest/event.go`:

```go
package ingest

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"idios/internal/clock"
	"idios/internal/store"
)

// MapEvent maps a core/v1 Event to its row. Category is left nil; the
// caller classifies. Newer components fill eventTime and series with
// microseconds and leave the whole-second pair empty, so the first non-zero
// source wins in that order.
func MapEvent(ev *corev1.Event, clusterID int64) (store.K8sEvent, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return store.K8sEvent{}, fmt.Errorf("marshal event %s: %w", ev.UID, err)
	}
	row := store.K8sEvent{
		ClusterID: clusterID, EventUID: string(ev.UID), Namespace: ev.Namespace, Type: ev.Type,
		InvolvedKind: ev.InvolvedObject.Kind, InvolvedName: ev.InvolvedObject.Name, InvolvedUID: string(ev.InvolvedObject.UID),
		FieldPath: ev.InvolvedObject.FieldPath, Reason: ev.Reason, Message: ev.Message,
		SourceComponent: ev.Source.Component, Count: 1, RawJSON: string(raw),
	}
	if row.SourceComponent == "" {
		row.SourceComponent = ev.ReportingController
	}
	switch {
	case ev.Series != nil && ev.Series.Count > 0:
		row.Count = int64(ev.Series.Count)
	case ev.Count > 0:
		row.Count = int64(ev.Count)
	}
	created := ev.CreationTimestamp.Time
	row.FirstTS = clock.Format(firstNonZero(ev.EventTime.Time, ev.FirstTimestamp.Time, created))
	last := []time.Time{ev.EventTime.Time, ev.LastTimestamp.Time, ev.FirstTimestamp.Time, created}
	if ev.Series != nil {
		last = append([]time.Time{ev.Series.LastObservedTime.Time}, last...)
	}
	row.LastTS = clock.Format(firstNonZero(last...))
	return row, nil
}

func firstNonZero(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// EarlyCaptureReason says whether an event announces a pod's imminent
// removal or a probe failure. It matches the reason only: Killing and
// Preempting are Normal events, and a type filter would drop them.
func EarlyCaptureReason(reason string) bool {
	switch reason {
	case "Killing", "Preempted", "Preempting", "Evicted", "Unhealthy":
		return true
	}
	return false
}
```

Add `"time"` to the imports.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ingest/ -v -run 'Event|EarlyCapture'`
Expected: PASS. If `LastTS` on the legacy fixture is the creation time, `metav1.Time` decoded as zero; check the fixture key spelling (`lastTimestamp`).

- [ ] **Step 6: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/ingest && git commit -m "ingest: add MapEvent and EarlyCaptureReason"`

---

### Task 6: `incident` categories and the prohibition test

**Files:**
- Create: `internal/incident/category.go`
- Test: `internal/incident/category_test.go`

**Interfaces:**
- Consumes: `store.Category*`, `store.State*`.
- Produces: `func ContainerCategory(state, reason, lastTerminatedReason string) string`, `func ConditionCategory(condType, status, reason string) string`, `func PodReasonCategory(reason string) string`, `func EventCategory(reason string) *string`. An empty string (or nil) means "not a problem".

Tests and their trace:
- `TestContainerCategoryKeysOnReason`: storage doc 6.1 table rows `oom`, `crash`, `image_pull`, `config`; "A terminated reason of Completed opens nothing"; "OOM with exit code 0 ... the reason wins" (the function has no exit code to read).
- `TestConditionAndPodReasonCategories`: 6.1 rows `scheduling` ("not SchedulingGated"), `node_pressure`, `rescheduled`.
- `TestEventCategoryTable`: storage doc 5.10 event classification table including `BackOff` -> NULL.
- `TestCategoriesNeverReadPhaseExitCodeOrMessage`: process doc 14.3 prohibition test, and the roadmap rule that the mapping lives in one file.

- [ ] **Step 1: Write the failing tests**

`internal/incident/category_test.go`:

```go
package incident

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"idios/internal/store"
)

func ptr[T any](v T) *T { return &v }

func TestContainerCategoryKeysOnReason(t *testing.T) {
	cases := []struct {
		state, reason, last string
		want                string
	}{
		{store.StateWaiting, "CrashLoopBackOff", "Error", store.CategoryCrash},
		{store.StateWaiting, "CrashLoopBackOff", "OOMKilled", store.CategoryOOM},
		{store.StateWaiting, "CrashLoopBackOff", "", store.CategoryCrash},
		{store.StateTerminated, "OOMKilled", "", store.CategoryOOM},
		{store.StateTerminated, "Error", "", store.CategoryCrash},
		{store.StateTerminated, "Error", "OOMKilled", store.CategoryCrash},
		{store.StateTerminated, "Completed", "", ""},
		{store.StateTerminated, "ContainerCannotRun", "", store.CategoryConfig},
		{store.StateTerminated, "StartError", "", store.CategoryConfig},
		{store.StateWaiting, "ErrImagePull", "", store.CategoryImagePull},
		{store.StateWaiting, "ImagePullBackOff", "", store.CategoryImagePull},
		{store.StateWaiting, "InvalidImageName", "", store.CategoryImagePull},
		{store.StateWaiting, "CreateContainerConfigError", "", store.CategoryConfig},
		{store.StateWaiting, "CreateContainerError", "", store.CategoryConfig},
		{store.StateWaiting, "ContainerCreating", "", ""},
		{store.StateWaiting, "PodInitializing", "OOMKilled", ""},
		{store.StateWaiting, "", "", ""},
		{store.StateRunning, "", "OOMKilled", ""},
		{store.StateRunning, "", "", ""},
	}
	for _, c := range cases {
		if got := ContainerCategory(c.state, c.reason, c.last); got != c.want {
			t.Errorf("ContainerCategory(%q, %q, %q) = %q, want %q", c.state, c.reason, c.last, got, c.want)
		}
	}
}

func TestConditionAndPodReasonCategories(t *testing.T) {
	conds := []struct {
		ty, status, reason string
		want               string
	}{
		{"PodScheduled", "False", "Unschedulable", store.CategoryScheduling},
		{"PodScheduled", "False", "SchedulingGated", ""},
		{"PodScheduled", "True", "", ""},
		{"DisruptionTarget", "True", "TerminationByKubelet", store.CategoryNodePressure},
		{"DisruptionTarget", "True", "EvictionByEvictionAPI", store.CategoryRescheduled},
		{"DisruptionTarget", "True", "PreemptionByScheduler", store.CategoryRescheduled},
		{"DisruptionTarget", "True", "DeletionByTaintManager", store.CategoryRescheduled},
		{"DisruptionTarget", "True", "DeletionByPodGC", store.CategoryRescheduled},
		{"DisruptionTarget", "False", "TerminationByKubelet", ""},
		{"Ready", "False", "", ""},
	}
	for _, c := range conds {
		if got := ConditionCategory(c.ty, c.status, c.reason); got != c.want {
			t.Errorf("ConditionCategory(%q, %q, %q) = %q, want %q", c.ty, c.status, c.reason, got, c.want)
		}
	}
	reasons := []struct{ reason, want string }{
		{"Evicted", store.CategoryNodePressure}, {"Preempting", ""}, {"NodeLost", ""}, {"", ""},
	}
	for _, c := range reasons {
		if got := PodReasonCategory(c.reason); got != c.want {
			t.Errorf("PodReasonCategory(%q) = %q, want %q", c.reason, got, c.want)
		}
	}
}

func TestEventCategoryTable(t *testing.T) {
	cases := []struct {
		reason string
		want   *string
	}{
		{"FailedScheduling", ptr(store.CategoryScheduling)},
		{"Failed", ptr(store.CategoryImagePull)},
		{"ErrImagePull", ptr(store.CategoryImagePull)},
		{"ImagePullBackOff", ptr(store.CategoryImagePull)},
		{"InvalidImageName", ptr(store.CategoryImagePull)},
		{"CreateContainerConfigError", ptr(store.CategoryConfig)},
		{"CreateContainerError", ptr(store.CategoryConfig)},
		{"Unhealthy", ptr(store.CategoryProbe)},
		{"OOMKilling", ptr(store.CategoryOOM)},
		{"Evicted", ptr(store.CategoryNodePressure)},
		{"BackOff", nil},
		{"Killing", nil}, {"Preempted", nil}, {"Preempting", nil}, {"Pulling", nil}, {"Pulled", nil},
		{"Scheduled", nil}, {"Started", nil}, {"Created", nil}, {"", nil},
	}
	for _, c := range cases {
		if d := cmp.Diff(c.want, EventCategory(c.reason)); d != "" {
			t.Errorf("EventCategory(%q): %s", c.reason, d)
		}
	}
}

// Categories key on reason alone: phase lies (CrashLoopBackOff reports
// Running), exit codes are ambiguous (137 is OOM or a grace-period kill) and
// messages are prose. Only category.go may map reasons, and it may not touch
// those fields.
func TestCategoriesNeverReadPhaseExitCodeOrMessage(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "category.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Phase", "ExitCode", "Message", "LastMessage", "StatusMessage", "LastTerminatedExitCode":
			t.Errorf("category.go:%d reads .%s", fset.Position(sel.Pos()).Line, sel.Sel.Name)
		}
		return true
	})

	for _, dir := range []string{".", "../ingest"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || (dir == "." && name == "category.go") {
				continue
			}
			pf, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range pf.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && strings.HasSuffix(fn.Name.Name, "Category") {
					t.Errorf("%s/%s defines %s; category mapping belongs in incident/category.go", dir, name, fn.Name.Name)
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/incident/`
Expected: FAIL, undefined functions (and the prohibition test fails to parse a missing `category.go`).

- [ ] **Step 3: Implement**

`internal/incident/category.go`:

```go
// Package incident turns ingest changes into incident operations: open,
// attach, reopen, close. It is pure; the processor executes the operations.
package incident

import "idios/internal/store"

// ContainerCategory classifies a container state by its reasons alone. A
// running container is never a problem. CrashLoopBackOff after an OOMKilled
// termination is oom, not crash: the loop is the symptom, the kill the cause.
func ContainerCategory(state, reason, lastTerminatedReason string) string {
	switch state {
	case store.StateWaiting:
		switch reason {
		case "CrashLoopBackOff":
			if lastTerminatedReason == "OOMKilled" {
				return store.CategoryOOM
			}
			return store.CategoryCrash
		case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
			return store.CategoryImagePull
		case "CreateContainerConfigError", "CreateContainerError":
			return store.CategoryConfig
		}
	case store.StateTerminated:
		switch reason {
		case "OOMKilled":
			return store.CategoryOOM
		case "Error":
			return store.CategoryCrash
		case "ContainerCannotRun", "StartError":
			return store.CategoryConfig
		}
	}
	return ""
}

// ConditionCategory classifies a pod condition. SchedulingGated is
// intentional and opens nothing. TerminationByKubelet also covers graceful
// node shutdown; the two differ only in message, so they share a category.
func ConditionCategory(condType, status, reason string) string {
	switch {
	case condType == "PodScheduled" && status == "False" && reason == "Unschedulable":
		return store.CategoryScheduling
	case condType == "DisruptionTarget" && status == "True":
		switch reason {
		case "TerminationByKubelet":
			return store.CategoryNodePressure
		case "EvictionByEvictionAPI", "PreemptionByScheduler", "DeletionByTaintManager", "DeletionByPodGC":
			return store.CategoryRescheduled
		}
	}
	return ""
}

// PodReasonCategory classifies pods.status_reason.
func PodReasonCategory(reason string) string {
	if reason == "Evicted" {
		return store.CategoryNodePressure
	}
	return ""
}

// EventCategory classifies an event reason; nil is "no category". BackOff
// is nil on purpose: image back-off and restart back-off share the reason
// and differ only in message.
func EventCategory(reason string) *string {
	var c string
	switch reason {
	case "FailedScheduling":
		c = store.CategoryScheduling
	case "Failed", "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
		c = store.CategoryImagePull
	case "CreateContainerConfigError", "CreateContainerError":
		c = store.CategoryConfig
	case "Unhealthy":
		c = store.CategoryProbe
	case "OOMKilling":
		c = store.CategoryOOM
	case "Evicted":
		c = store.CategoryNodePressure
	default:
		return nil
	}
	return &c
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/incident/ -v`
Expected: PASS, four tests.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/incident && git commit -m "incident: map reasons to categories"`

---

### Task 7: `incident.Apply` for pods

**Files:**
- Create: `internal/incident/ops.go`, `internal/incident/apply.go`
- Test: `internal/incident/apply_test.go`

**Interfaces:**
- Consumes: `ingest.PodChanges`, `ingest.PodSnapshot`, `ingest.DiffPod` (tests only), `ContainerCategory`, `ConditionCategory`, `PodReasonCategory`, `store.Incident`, `store.ClosePodDeleted`, `store.SubjectPod`, `clock.Format`.
- Produces:
  - `type Ops struct { Open []Open; Attach []Attach; Close []Close; HistoryCategories []*string }` (`HistoryCategories` is parallel to `changes.History`; Phase 3 copies it into each row's `Category`).
  - `type Open struct { Incident store.Incident; HistoryIndexes []int }` (`Incident.ID` is 0; the indexes name the history rows whose `incident_id` becomes the new id).
  - `type Attach struct { IncidentID int64; Reopen bool; LastReason string; LastMessage *string; LastSeenAt string; HistoryIndexes []int }` (`Reopen` clears `closed_at`, `close_reason`, `dismissed_at`; every attach bumps `occurrences` by one).
  - `type Close struct { IncidentID int64; Reason string; ClosedAt string }` (used by Task 8).
  - `func Apply(incidents []store.Incident, changes ingest.PodChanges, now time.Time) Ops`.
  - Test helpers `loadPod`, `snapshotFrom`, `scenario`, `incident`.

Phase 3 reads `changes.WorkloadChanged` directly to correct `workload_*` on open incidents; `Ops` does not repeat it.

Tests and their trace (each is a row of `TestApplyScenarios` unless named):
- crash loop opens one `crash` incident with the container's image at open time and `opened_at` from the last termination: storage doc 6.2 step 4, 5.7 `image*` "at open time", 4 (`opened_at` from `lastState.terminated.finishedAt`).
- crash loop with an open incident attaches, bumps and does not open: 6.2 steps 1-2, "one incident with occurrences climbing, not 47".
- reopen a `recovered` incident, never a `pod_deleted` one, and the latest closed one wins: 6.3 reopen paragraph, 14.1 "reopen" bullet.
- `oom`: category from `lastState`, `first_reason = OOMKilled`: 6.1 `oom` row ("state or lastState").
- `oom-exit-zero`: 14.1 "the reason wins".
- `image-pull` resolving to running produces no operation: 6.3 (`recovered` is the closer's job, "handlers only open, attach and reopen").
- `config` and `first-sight-config-error`: storage doc 4 first-sight rule ("incident, no history row", `opened_at` falls back to `creationTimestamp`); `first-sight-healthy` opens nothing.
- `init-then-success`: 6.1 "Completed opens nothing".
- `restart-jump`: the reconstructed row carries the category and opens the incident with its own `finishedAt`; the running row is not a problem.
- `job-never-error`: 14.1 bullet, a `crash` incident.
- `unschedulable` first sight opens `scheduling` at the condition's transition time with the message on `last_message`; a message-only change produces nothing: 6.1 `scheduling`, 5.4.
- `evicted`: 14.1 "the category came from the reason" (pod-level `node_pressure` next to the container's `crash`).
- `disruption-api`: 6.1 `rescheduled`.
- `disruption-kubelet`: 6.1 "Both node_pressure rules can fire for the same eviction ... one incident" (coalescing decision above).

- [ ] **Step 1: Write the failing tests**

`internal/incident/apply_test.go`:

```go
package incident

import (
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
	"idios/internal/ingest"
	"idios/internal/store"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

type noOwners struct{}

func (noOwners) ReplicaSetOwner(string, string) *metav1.OwnerReference { return nil }
func (noOwners) JobOwner(string, string) *metav1.OwnerReference        { return nil }

func loadPod(t *testing.T, path string) *corev1.Pod {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "ingest", "testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var pod corev1.Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &pod
}

func snapshotFrom(prev *ingest.PodSnapshot, ch ingest.PodChanges) *ingest.PodSnapshot {
	byType := map[string]store.PodCondition{}
	var order []string
	if prev != nil {
		for _, c := range prev.Conditions {
			byType[c.Type] = c
			order = append(order, c.Type)
		}
	}
	for _, c := range ch.Conditions {
		if _, seen := byType[c.Type]; !seen {
			order = append(order, c.Type)
		}
		byType[c.Type] = c
	}
	snap := &ingest.PodSnapshot{Pod: ch.Pod, Containers: ch.Containers}
	for _, ty := range order {
		snap.Conditions = append(snap.Conditions, byType[ty])
	}
	return snap
}

func scenario(t *testing.T, files ...string) ingest.PodChanges {
	t.Helper()
	var snap *ingest.PodSnapshot
	var ch ingest.PodChanges
	for _, f := range files {
		ch = ingest.DiffPod(snap, loadPod(t, f), 1, noOwners{}, testNow)
		snap = snapshotFrom(snap, ch)
	}
	return ch
}

func incident(id int64, podUID, container, category string, closedAt, closeReason *string) store.Incident {
	return store.Incident{
		ID: id, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr(podUID),
		ContainerName: container, WorkloadKind: "ReplicaSet", WorkloadName: "web-7d9f8c6b5", Category: category,
		FirstReason: "CrashLoopBackOff", LastReason: "CrashLoopBackOff", Occurrences: 3,
		OpenedAt: "2026-08-27T10:00:00.000000Z", LastSeenAt: "2026-08-27T10:30:00.000000Z", ClosedAt: closedAt, CloseReason: closeReason,
	}
}

func TestApplyScenarios(t *testing.T) {
	now := clock.Format(testNow)
	web := "registry.example.com/web:1.4.2"
	webID := ptr("registry.example.com/web@sha256:1111")
	open := func(podUID, workloadKind, workloadName, container, category, reason string, openedAt string) store.Incident {
		return store.Incident{
			ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr(podUID), ContainerName: container,
			WorkloadKind: workloadKind, WorkloadName: workloadName, Category: category, FirstReason: reason, LastReason: reason,
			Occurrences: 1, OpenedAt: openedAt, LastSeenAt: now,
		}
	}
	withImage := func(i store.Incident, image string, tag *string, id *string) store.Incident {
		i.Image, i.ImageTag, i.ImageID = ptr(image), tag, id
		return i
	}
	withMessage := func(i store.Incident, msg string) store.Incident {
		i.LastMessage = ptr(msg)
		return i
	}
	crash := withImage(open("pod-crash", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "CrashLoopBackOff", "2026-08-27T11:55:00.000000Z"), web, ptr("1.4.2"), webID)
	crashAttach := Attach{IncidentID: 7, LastReason: "CrashLoopBackOff", LastSeenAt: now, HistoryIndexes: []int{0}}
	catCrash := []*string{ptr(store.CategoryCrash)}
	msg3 := "0/3 nodes are available: 3 Insufficient cpu. preemption: 0/3 nodes are available: 3 No preemption victims found for incoming pod."
	evictMsg := "The node was low on resource: memory. Threshold quantity: 100Mi, available: 52Mi. Container api was using 900Mi, request is 0, has larger consumption of memory."

	cases := []struct {
		name      string
		files     []string
		incidents []store.Incident
		want      Ops
	}{
		{"crash loop opens", []string{"crash-loop/before.json", "crash-loop/after.json"}, nil,
			Ops{Open: []Open{{Incident: crash, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"crash loop attaches to open incident", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-crash", "api", store.CategoryCrash, nil, nil)},
			Ops{Attach: []Attach{crashAttach}, HistoryCategories: catCrash}},
		{"different container does not attach", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-crash", "worker", store.CategoryCrash, nil, nil)},
			Ops{Open: []Open{{Incident: crash, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"recovered incident reopens", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-crash", "api", store.CategoryCrash, ptr("2026-08-27T11:00:00.000000Z"), ptr(store.CloseRecovered))},
			Ops{Attach: []Attach{{IncidentID: 7, Reopen: true, LastReason: "CrashLoopBackOff", LastSeenAt: now, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"pod_deleted incident never reopens", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-crash", "api", store.CategoryCrash, ptr("2026-08-27T11:00:00.000000Z"), ptr(store.ClosePodDeleted))},
			Ops{Open: []Open{{Incident: crash, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"latest closed incident reopens", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{
				incident(5, "pod-crash", "api", store.CategoryCrash, ptr("2026-08-27T11:00:00.000000Z"), ptr(store.CloseRecovered)),
				incident(6, "pod-crash", "api", store.CategoryCrash, ptr("2026-08-27T11:30:00.000000Z"), ptr(store.CloseManual)),
			},
			Ops{Attach: []Attach{{IncidentID: 6, Reopen: true, LastReason: "CrashLoopBackOff", LastSeenAt: now, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"oom from lastState", []string{"oom/before.json", "oom/after.json"}, nil,
			Ops{Open: []Open{{Incident: withImage(open("pod-oom", "ReplicaSet", "mem-hog-5f6d7", "api", store.CategoryOOM, "OOMKilled", "2026-08-27T11:52:30.000000Z"),
				"registry.example.com/mem-hog:2.0.0", ptr("2.0.0"), ptr("registry.example.com/mem-hog@sha256:2222")), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryOOM)}}},
		{"oom with exit code zero", []string{"oom-exit-zero/before.json", "oom-exit-zero/after.json"}, nil,
			Ops{Open: []Open{{Incident: withImage(open("pod-oom0", "Job", "report-28812345", "report", store.CategoryOOM, "OOMKilled", "2026-08-27T11:58:00.000000Z"),
				"registry.example.com/report:0.9.1", ptr("0.9.1"), ptr("registry.example.com/report@sha256:3333")), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryOOM)}}},
		{"image pull resolving is not an operation", []string{"image-pull/s1.json", "image-pull/s2.json", "image-pull/s3.json"},
			[]store.Incident{incident(3, "pod-pull", "api", store.CategoryImagePull, nil, nil)},
			Ops{HistoryCategories: []*string{nil}}},
		{"config error after creating", []string{"config/before.json", "config/after.json"}, nil,
			Ops{Open: []Open{{Incident: withImage(open("pod-cfg", "ReplicaSet", "web-5b8c7", "api", store.CategoryConfig, "CreateContainerConfigError", now), web, ptr("1.4.2"), nil), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryConfig)}}},
		{"first sight config error opens without history", []string{"first-sight-config-error/pod.json"}, nil,
			Ops{Open: []Open{{Incident: withImage(open("pod-cfg-first", "ReplicaSet", "web-5b8c7", "api", store.CategoryConfig, "CreateContainerConfigError", "2026-08-27T11:45:00.000000Z"), web, ptr("1.4.2"), nil)}}}},
		{"first sight healthy opens nothing", []string{"first-sight-healthy/pod.json"}, nil, Ops{}},
		{"completed init container opens nothing", []string{"init-then-success/before.json", "init-then-success/after.json"}, nil,
			Ops{HistoryCategories: []*string{nil, nil}}},
		{"restart jump opens from the reconstructed row", []string{"restart-jump/before.json", "restart-jump/after.json"}, nil,
			Ops{Open: []Open{{Incident: withImage(open("pod-jump", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "Error", "2026-08-27T11:58:00.000000Z"), web, ptr("1.4.2"), webID), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryCrash), nil}}},
		{"job pod terminated with error", []string{"job-never-error/before.json", "job-never-error/after.json"}, nil,
			Ops{Open: []Open{{Incident: withImage(open("pod-jobfail", "Job", "import-28812346", "worker", store.CategoryCrash, "Error", "2026-08-27T11:50:00.000000Z"),
				"registry.example.com/import:5.2.0", ptr("5.2.0"), ptr("registry.example.com/import@sha256:8888")), HistoryIndexes: []int{0}}},
				HistoryCategories: catCrash}},
		{"unschedulable at first sight", []string{"unschedulable/s1.json"}, nil,
			Ops{Open: []Open{{Incident: withMessage(open("pod-unsched", "ReplicaSet", "web-2b3c4", "", store.CategoryScheduling, "Unschedulable", "2026-08-27T11:45:01.000000Z"), msg3)}}}},
		{"unschedulable message change is nothing", []string{"unschedulable/s1.json", "unschedulable/s2.json"},
			[]store.Incident{incident(9, "pod-unsched", "", store.CategoryScheduling, nil, nil)}, Ops{}},
		{"evicted opens pod-level node_pressure next to the container crash", []string{"evicted/before.json", "evicted/after.json"}, nil,
			Ops{Open: []Open{
				{Incident: withImage(open("pod-evict", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "Error", "2026-08-27T11:57:30.000000Z"), web, ptr("1.4.2"), webID), HistoryIndexes: []int{0}},
				{Incident: withMessage(open("pod-evict", "ReplicaSet", "web-7d9f8c6b5", "", store.CategoryNodePressure, "Evicted", now), evictMsg)},
			}, HistoryCategories: catCrash}},
		{"disruption by eviction api is rescheduled", []string{"disruption-api/before.json", "disruption-api/after.json"}, nil,
			Ops{Open: []Open{{Incident: withMessage(open("pod-drain", "ReplicaSet", "web-7d9f8c6b5", "", store.CategoryRescheduled, "EvictionByEvictionAPI", "2026-08-27T11:59:30.000000Z"), "Eviction API: evicting")}}}},
		{"both node_pressure rules coalesce", []string{"disruption-kubelet/before.json", "disruption-kubelet/after.json"}, nil,
			Ops{Open: []Open{{Incident: func() store.Incident {
				i := withMessage(open("pod-press", "ReplicaSet", "web-7d9f8c6b5", "", store.CategoryNodePressure, "TerminationByKubelet", "2026-08-27T11:59:40.000000Z"), "The node was low on resource: memory.")
				i.LastReason, i.Occurrences = "Evicted", 2
				return i
			}()}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Apply(c.incidents, scenario(t, c.files...), testNow)
			if d := cmp.Diff(c.want, got, cmpopts.EquateEmpty()); d != "" {
				t.Fatal(d)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/incident/ -run Apply`
Expected: FAIL, undefined `Apply`, `Ops`.

- [ ] **Step 3: Implement**

`internal/incident/ops.go`:

```go
package incident

import "idios/internal/store"

// Ops is what the processor executes after Apply. HistoryCategories is
// parallel to the History slice it was computed from.
type Ops struct {
	Open              []Open
	Attach            []Attach
	Close             []Close
	HistoryCategories []*string
}

// Open is a new incident row; HistoryIndexes name the history rows that
// take its id once inserted.
type Open struct {
	Incident       store.Incident
	HistoryIndexes []int
}

// Attach bumps an existing incident. Reopen also clears closed_at,
// close_reason and dismissed_at; acknowledged_at is kept.
type Attach struct {
	IncidentID     int64
	Reopen         bool
	LastReason     string
	LastMessage    *string
	LastSeenAt     string
	HistoryIndexes []int
}

// Close ends an incident with the given close_reason.
type Close struct {
	IncidentID int64
	Reason     string
	ClosedAt   string
}
```

`internal/incident/apply.go`:

```go
package incident

import (
	"time"

	"idios/internal/clock"
	"idios/internal/ingest"
	"idios/internal/store"
)

type problem struct {
	container    string
	category     string
	reason       string
	message      *string
	openedAt     string
	image        *string
	imageTag     *string
	imageID      *string
	historyIndex int
}

type key struct{ container, category string }

// Apply decides open, attach and reopen for every problem in changes.
// Problems are gathered from history rows, then first-sight container
// states, then conditions, then the pod's status reason, and resolved in
// that order against the pod's incidents.
func Apply(incidents []store.Incident, changes ingest.PodChanges, now time.Time) Ops {
	nowS := clock.Format(now)
	ops := Ops{HistoryCategories: make([]*string, len(changes.History))}
	containers := map[string]store.Container{}
	for _, c := range changes.Containers {
		containers[c.Name] = c
	}

	var problems []problem
	for i, row := range changes.History {
		c := containers[row.ContainerName]
		cat := ContainerCategory(row.State, deref(row.Reason), deref(c.LastTerminatedReason))
		if cat == "" {
			continue
		}
		ops.HistoryCategories[i] = &cat
		reason := deref(row.Reason)
		if cat == store.CategoryOOM && row.State == store.StateWaiting {
			reason = deref(c.LastTerminatedReason)
		}
		problems = append(problems, problem{
			container: c.Name, category: cat, reason: reason,
			openedAt: firstSet(row.K8sFinishedAt, c.LastTerminatedAt, row.K8sStartedAt, &nowS),
			image:    &c.Image, imageTag: c.ImageTag, imageID: c.ImageID, historyIndex: i,
		})
	}
	if changes.FirstSight {
		for _, c := range changes.Containers {
			cat := ContainerCategory(c.State, deref(c.Reason), deref(c.LastTerminatedReason))
			if cat == "" {
				continue
			}
			reason := deref(c.Reason)
			if cat == store.CategoryOOM && c.State == store.StateWaiting {
				reason = deref(c.LastTerminatedReason)
			}
			problems = append(problems, problem{
				container: c.Name, category: cat, reason: reason,
				openedAt: firstSet(c.LastTerminatedAt, &changes.Pod.CreatedAt),
				image:    &c.Image, imageTag: c.ImageTag, imageID: c.ImageID, historyIndex: -1,
			})
		}
	}
	for _, cond := range changes.Conditions {
		cat := ConditionCategory(cond.Type, cond.Status, cond.Reason)
		if cat == "" {
			continue
		}
		problems = append(problems, problem{
			category: cat, reason: cond.Reason, message: cond.Message,
			openedAt: firstSet(cond.K8sTransitionAt, &nowS), historyIndex: -1,
		})
	}
	if changes.PodReasonChanged || changes.FirstSight {
		if cat := PodReasonCategory(deref(changes.Pod.StatusReason)); cat != "" {
			openedAt := nowS
			if changes.FirstSight {
				openedAt = changes.Pod.CreatedAt
			}
			problems = append(problems, problem{
				category: cat, reason: deref(changes.Pod.StatusReason), message: changes.Pod.StatusMessage,
				openedAt: openedAt, historyIndex: -1,
			})
		}
	}

	opens := map[key]int{}
	attaches := map[key]int{}
	for _, p := range problems {
		k := key{p.container, p.category}
		switch {
		case has(opens, k):
			o := &ops.Open[opens[k]]
			o.Incident.Occurrences++
			o.Incident.LastReason, o.Incident.LastMessage = p.reason, p.message
			o.HistoryIndexes = appendIndex(o.HistoryIndexes, p.historyIndex)
		case has(attaches, k):
			a := &ops.Attach[attaches[k]]
			a.LastReason, a.LastMessage = p.reason, p.message
			a.HistoryIndexes = appendIndex(a.HistoryIndexes, p.historyIndex)
		default:
			if target, reopen, ok := resolve(incidents, k); ok {
				attaches[k] = len(ops.Attach)
				ops.Attach = append(ops.Attach, Attach{
					IncidentID: target.ID, Reopen: reopen, LastReason: p.reason, LastMessage: p.message,
					LastSeenAt: nowS, HistoryIndexes: appendIndex(nil, p.historyIndex),
				})
				continue
			}
			opens[k] = len(ops.Open)
			ops.Open = append(ops.Open, Open{
				Incident: store.Incident{
					ClusterID: changes.Pod.ClusterID, Namespace: changes.Pod.Namespace, SubjectKind: store.SubjectPod,
					PodUID: &changes.Pod.UID, ContainerName: p.container,
					WorkloadKind: changes.Pod.WorkloadKind, WorkloadName: changes.Pod.WorkloadName,
					Category: p.category, FirstReason: p.reason, LastReason: p.reason, LastMessage: p.message,
					Image: p.image, ImageTag: p.imageTag, ImageID: p.imageID,
					Occurrences: 1, OpenedAt: p.openedAt, LastSeenAt: nowS,
				},
				HistoryIndexes: appendIndex(nil, p.historyIndex),
			})
		}
	}
	return ops
}

// resolve finds the open incident for k, else the most recently closed one
// that can be reopened. A pod_deleted close is final: its pod is gone.
func resolve(incidents []store.Incident, k key) (target store.Incident, reopen, ok bool) {
	var latest *store.Incident
	for i := range incidents {
		inc := &incidents[i]
		if inc.SubjectKind != store.SubjectPod || inc.ContainerName != k.container || inc.Category != k.category {
			continue
		}
		if inc.ClosedAt == nil {
			return *inc, false, true
		}
		if deref(inc.CloseReason) == store.ClosePodDeleted {
			continue
		}
		if latest == nil || *inc.ClosedAt > *latest.ClosedAt {
			latest = inc
		}
	}
	if latest == nil {
		return store.Incident{}, false, false
	}
	return *latest, true, true
}

func has(m map[key]int, k key) bool {
	_, ok := m[k]
	return ok
}

func appendIndex(idx []int, i int) []int {
	if i < 0 {
		return idx
	}
	return append(idx, i)
}

func firstSet(vals ...*string) string {
	for _, v := range vals {
		if v != nil && *v != "" {
			return *v
		}
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/incident/ -v -run Apply`
Expected: PASS. `unschedulable message change is nothing` must yield an empty `Ops` even though an open incident exists: no condition row means no problem. The closed-incident comparison relies on the fixed-width layout sorting as text; if `latest closed incident reopens` picks id 5, the comparison is on the wrong field.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/incident && git commit -m "incident: add Apply for pod changes"`

---

### Task 8: `ApplyJob`, event attach rule, `ApplyEvent`

**Files:**
- Create: `internal/incident/job.go`, `internal/incident/event.go`
- Test: `internal/incident/job_test.go`, `internal/incident/event_test.go`

**Interfaces:**
- Consumes: `ingest.JobChanges`, `ingest.DiffJob` (tests), `Ops`, `Open`, `Attach`, `Close`, `resolve`-style lookup, `EventCategory`, `store.K8sEvent`, `store.CategoryJobFailed`, `store.CategoryProbe`, `store.CloseJobFinished`, `store.SubjectJob`.
- Produces:
  - `func ApplyJob(incidents []store.Incident, changes ingest.JobChanges, now time.Time) Ops`.
  - `func FieldPathContainer(fieldPath string) string`.
  - `func EventMatches(ev store.K8sEvent, inc store.Incident) bool`.
  - `type EventRef struct { OpenIndex int; IncidentID *int64 }` (`OpenIndex` is -1 unless the event attaches to `Ops.Open[OpenIndex]`).
  - `func ApplyEvent(incidents []store.Incident, ev store.K8sEvent, pod *store.Pod, container *store.Container, now time.Time) (Ops, EventRef)`. `pod` and `container` are the store rows for `ev.InvolvedUID` and `FieldPathContainer(ev.FieldPath)`, nil when absent.

Tests and their trace:
- `TestApplyJob`: storage doc 6.1 `job_failed` ("Job condition Failed (any reason)"), 6.3 "Job reaches Complete -> job_finished", 5.7 `workload_*` from the CronJob owner (14.1 "Job -> CronJob owner chain, and a Job pod with no CronJob owner"), and `Failed` firing once (Task 4) so a later update attaches rather than opens twice.
- `TestFieldPathContainer`: storage doc 5.10 attach rule, "container parsed from field_path (`spec.containers{api}` / `spec.initContainers{init-db}`)".
- `TestEventMatchesAttachRule`: 5.10 attach rule, every clause as a row.
- `TestApplyEventProbeGate`: 6.1 `probe` row ("Event reason Unhealthy and the container's snapshot row has ready = 0 at that moment. The second half is the gate"); 5.10 "If several open incidents qualify, the one with the latest last_seen_at wins"; process doc 4.4 "No event ever opens an incident on its own except probe".

- [ ] **Step 1: Write the failing tests**

`internal/incident/job_test.go`:

```go
package incident

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	batchv1 "k8s.io/api/batch/v1"

	"idios/internal/clock"
	"idios/internal/ingest"
	"idios/internal/store"
)

func loadJob(t *testing.T, path string) *batchv1.Job {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "ingest", "testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.Job
	if err := json.Unmarshal(data, &job); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &job
}

func jobScenario(t *testing.T, files ...string) ingest.JobChanges {
	t.Helper()
	var snap *store.Job
	var ch ingest.JobChanges
	for _, f := range files {
		ch = ingest.DiffJob(snap, loadJob(t, f), 1, testNow)
		row := ch.Job
		snap = &row
	}
	return ch
}

func TestApplyJob(t *testing.T) {
	now := clock.Format(testNow)
	openJobIncident := store.Incident{
		ID: 11, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectJob, JobUID: ptr("job-report-1"),
		WorkloadKind: "CronJob", WorkloadName: "report", Category: store.CategoryJobFailed,
		FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", Occurrences: 1,
		OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: "2026-08-27T11:57:05.000000Z",
	}
	failedOpen := store.Incident{
		ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectJob, JobUID: ptr("job-report-1"),
		WorkloadKind: "CronJob", WorkloadName: "report", Category: store.CategoryJobFailed,
		FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", LastMessage: ptr("Job has reached the specified backoff limit"),
		Occurrences: 1, OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: now,
	}
	cases := []struct {
		name      string
		files     []string
		incidents []store.Incident
		want      Ops
	}{
		{"failed job opens with cronjob as workload", []string{"job-failed/before.json", "job-failed/after.json"}, nil,
			Ops{Open: []Open{{Incident: failedOpen}}}},
		{"failed at first sight opens", []string{"job-failed/after.json"}, nil,
			Ops{Open: []Open{{Incident: failedOpen}}}},
		{"failed job with open incident attaches", []string{"job-failed/before.json", "job-failed/after.json"}, []store.Incident{openJobIncident},
			Ops{Attach: []Attach{{IncidentID: 11, LastReason: "BackoffLimitExceeded", LastMessage: ptr("Job has reached the specified backoff limit"), LastSeenAt: now}}}},
		{"failed stays failed is nothing", []string{"job-failed/before.json", "job-failed/after.json", "job-failed/after.json"}, []store.Incident{openJobIncident}, Ops{}},
		{"complete closes open incidents", []string{"job-complete/before.json", "job-complete/after.json"},
			[]store.Incident{func() store.Incident {
				i := openJobIncident
				i.ID, i.JobUID, i.WorkloadKind, i.WorkloadName = 12, ptr("job-report-2"), "Job", "report-28812350"
				return i
			}()},
			Ops{Close: []Close{{IncidentID: 12, Reason: store.CloseJobFinished, ClosedAt: now}}}},
		{"complete with nothing open is nothing", []string{"job-complete/before.json", "job-complete/after.json"}, nil, Ops{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ApplyJob(c.incidents, jobScenario(t, c.files...), testNow)
			if d := cmp.Diff(c.want, got, cmpopts.EquateEmpty()); d != "" {
				t.Fatal(d)
			}
		})
	}
}
```

`internal/incident/event_test.go`:

```go
package incident

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"idios/internal/clock"
	"idios/internal/store"
)

func TestFieldPathContainer(t *testing.T) {
	cases := []struct{ in, want string }{
		{"spec.containers{api}", "api"},
		{"spec.initContainers{init-db}", "init-db"},
		{"spec.ephemeralContainers{debug}", "debug"},
		{"", ""},
		{"spec.containers", ""},
		{"metadata.labels", ""},
	}
	for _, c := range cases {
		if got := FieldPathContainer(c.in); got != c.want {
			t.Errorf("FieldPathContainer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEventMatchesAttachRule(t *testing.T) {
	podInc := store.Incident{SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryCrash}
	podLevel := store.Incident{SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "", Category: store.CategoryNodePressure}
	jobInc := store.Incident{SubjectKind: store.SubjectJob, JobUID: ptr("job-1"), Category: store.CategoryJobFailed}
	ev := func(uid, fieldPath string, cat *string) store.K8sEvent {
		return store.K8sEvent{InvolvedUID: uid, FieldPath: fieldPath, Category: cat}
	}
	cases := []struct {
		name string
		ev   store.K8sEvent
		inc  store.Incident
		want bool
	}{
		{"same pod, container and category", ev("pod-crash", "spec.containers{api}", ptr(store.CategoryCrash)), podInc, true},
		{"uncategorised event on the container", ev("pod-crash", "spec.containers{api}", nil), podInc, true},
		{"different category", ev("pod-crash", "spec.containers{api}", ptr(store.CategoryImagePull)), podInc, false},
		{"different container", ev("pod-crash", "spec.containers{worker}", nil), podInc, false},
		{"pod-level event on a container incident", ev("pod-crash", "", nil), podInc, false},
		{"pod-level incident takes container events", ev("pod-crash", "spec.containers{api}", nil), podLevel, true},
		{"pod-level incident takes pod events of its category", ev("pod-crash", "", ptr(store.CategoryNodePressure)), podLevel, true},
		{"different pod", ev("pod-other", "spec.containers{api}", nil), podInc, false},
		{"job incident by job uid", ev("job-1", "", nil), jobInc, true},
		{"job incident, other uid", ev("pod-crash", "", nil), jobInc, false},
	}
	for _, c := range cases {
		if got := EventMatches(c.ev, c.inc); got != c.want {
			t.Errorf("%s: EventMatches = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestApplyEventProbeGate(t *testing.T) {
	now := clock.Format(testNow)
	pod := &store.Pod{UID: "pod-crash", ClusterID: 1, Namespace: "idios-smoke", WorkloadKind: "Deployment", WorkloadName: "web"}
	notReady := &store.Container{PodUID: "pod-crash", Name: "api", Kind: store.ContainerKindApp, Image: "registry.example.com/web:1.4.2", ImageTag: ptr("1.4.2"), ImageID: ptr("sha"), State: store.StateRunning, Ready: false}
	ready := &store.Container{PodUID: "pod-crash", Name: "api", Kind: store.ContainerKindApp, Image: "registry.example.com/web:1.4.2", State: store.StateRunning, Ready: true}
	unhealthy := store.K8sEvent{
		ClusterID: 1, EventUID: "ev-1", Namespace: "idios-smoke", Type: "Warning", InvolvedKind: "Pod", InvolvedUID: "pod-crash", FieldPath: "spec.containers{api}",
		Reason: "Unhealthy", Message: "Readiness probe failed: HTTP probe failed with statuscode: 503", Category: ptr(store.CategoryProbe),
		FirstTS: "2026-08-27T11:58:00.000000Z", LastTS: "2026-08-27T11:59:00.000000Z",
	}
	backoff := unhealthy
	backoff.EventUID, backoff.Reason, backoff.Message, backoff.Category = "ev-2", "BackOff", "Back-off restarting failed container", nil

	openProbe := store.Incident{ID: 21, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryProbe, LastSeenAt: "2026-08-27T11:50:00.000000Z"}
	openCrash := store.Incident{ID: 22, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryCrash, LastSeenAt: "2026-08-27T11:40:00.000000Z"}
	openOOM := store.Incident{ID: 23, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryOOM, LastSeenAt: "2026-08-27T11:45:00.000000Z"}
	closedCrash := store.Incident{ID: 24, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryCrash, LastSeenAt: "2026-08-27T11:59:00.000000Z", ClosedAt: ptr("2026-08-27T11:59:30.000000Z"), CloseReason: ptr(store.CloseRecovered)}

	probeOpen := Open{Incident: store.Incident{
		ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api",
		WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryProbe, FirstReason: "Unhealthy", LastReason: "Unhealthy",
		LastMessage: ptr(unhealthy.Message), Image: ptr("registry.example.com/web:1.4.2"), ImageTag: ptr("1.4.2"), ImageID: ptr("sha"),
		Occurrences: 1, OpenedAt: "2026-08-27T11:59:00.000000Z", LastSeenAt: now,
	}}
	cases := []struct {
		name      string
		ev        store.K8sEvent
		container *store.Container
		incidents []store.Incident
		wantOps   Ops
		wantRef   EventRef
	}{
		{"unhealthy on a not-ready container opens probe", unhealthy, notReady, nil, Ops{Open: []Open{probeOpen}}, EventRef{OpenIndex: 0}},
		{"unhealthy on a ready container is noise", unhealthy, ready, nil, Ops{}, EventRef{OpenIndex: -1}},
		{"unhealthy without a container row opens nothing", unhealthy, nil, nil, Ops{}, EventRef{OpenIndex: -1}},
		{"unhealthy attaches to the open probe incident", unhealthy, notReady, []store.Incident{openProbe},
			Ops{Attach: []Attach{{IncidentID: 21, LastReason: "Unhealthy", LastMessage: ptr(unhealthy.Message), LastSeenAt: now}}}, EventRef{OpenIndex: -1, IncidentID: ptr[int64](21)}},
		{"backoff attaches to the matching open incident without an operation", backoff, notReady, []store.Incident{openCrash}, Ops{}, EventRef{OpenIndex: -1, IncidentID: ptr[int64](22)}},
		{"latest last_seen_at wins", backoff, notReady, []store.Incident{openCrash, openOOM}, Ops{}, EventRef{OpenIndex: -1, IncidentID: ptr[int64](23)}},
		{"closed incidents do not take events here", backoff, notReady, []store.Incident{closedCrash}, Ops{}, EventRef{OpenIndex: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotOps, gotRef := ApplyEvent(c.incidents, c.ev, pod, c.container, testNow)
			if d := cmp.Diff(c.wantOps, gotOps, cmpopts.EquateEmpty()); d != "" {
				t.Error(d)
			}
			if d := cmp.Diff(c.wantRef, gotRef); d != "" {
				t.Error(d)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/incident/`
Expected: FAIL, undefined `ApplyJob`, `FieldPathContainer`, `EventMatches`, `ApplyEvent`, `EventRef`.

- [ ] **Step 3: Implement**

`internal/incident/job.go`:

```go
package incident

import (
	"time"

	"idios/internal/clock"
	"idios/internal/ingest"
	"idios/internal/store"
)

// ApplyJob opens or attaches a job_failed incident when the Failed condition
// appeared and closes every open incident on the job when it completed.
func ApplyJob(incidents []store.Incident, changes ingest.JobChanges, now time.Time) Ops {
	nowS := clock.Format(now)
	var ops Ops
	job := changes.Job
	if changes.Failed {
		reason := deref(job.ConditionReason)
		if target, reopen, ok := resolveJob(incidents, job.UID); ok {
			ops.Attach = append(ops.Attach, Attach{
				IncidentID: target.ID, Reopen: reopen, LastReason: reason, LastMessage: job.ConditionMessage, LastSeenAt: nowS,
			})
		} else {
			inc := store.Incident{
				ClusterID: job.ClusterID, Namespace: job.Namespace, SubjectKind: store.SubjectJob, JobUID: &job.UID,
				WorkloadKind: "Job", WorkloadName: job.Name, Category: store.CategoryJobFailed,
				FirstReason: reason, LastReason: reason, LastMessage: job.ConditionMessage,
				Occurrences: 1, OpenedAt: firstSet(job.FinishedAt, &nowS), LastSeenAt: nowS,
			}
			if job.CronJobName != nil {
				inc.WorkloadKind, inc.WorkloadName = "CronJob", *job.CronJobName
			}
			ops.Open = append(ops.Open, Open{Incident: inc})
		}
	}
	if changes.Completed {
		for _, inc := range incidents {
			if inc.SubjectKind == store.SubjectJob && deref(inc.JobUID) == job.UID && inc.ClosedAt == nil {
				ops.Close = append(ops.Close, Close{IncidentID: inc.ID, Reason: store.CloseJobFinished, ClosedAt: nowS})
			}
		}
	}
	return ops
}

// resolveJob mirrors resolve for job incidents. job_finished is final: the
// Job either completed or is gone.
func resolveJob(incidents []store.Incident, jobUID string) (target store.Incident, reopen, ok bool) {
	var latest *store.Incident
	for i := range incidents {
		inc := &incidents[i]
		if inc.SubjectKind != store.SubjectJob || deref(inc.JobUID) != jobUID || inc.Category != store.CategoryJobFailed {
			continue
		}
		if inc.ClosedAt == nil {
			return *inc, false, true
		}
		if deref(inc.CloseReason) == store.CloseJobFinished {
			continue
		}
		if latest == nil || *inc.ClosedAt > *latest.ClosedAt {
			latest = inc
		}
	}
	if latest == nil {
		return store.Incident{}, false, false
	}
	return *latest, true, true
}
```

`internal/incident/event.go`:

```go
package incident

import (
	"strings"
	"time"

	"idios/internal/clock"
	"idios/internal/store"
)

// EventRef says which incident an event row belongs to: an entry of
// Ops.Open (OpenIndex >= 0) or an existing incident (IncidentID).
type EventRef struct {
	OpenIndex  int
	IncidentID *int64
}

// FieldPathContainer extracts the container name from an event's
// involvedObject.fieldPath, e.g. spec.containers{api}.
func FieldPathContainer(fieldPath string) string {
	open := strings.IndexByte(fieldPath, '{')
	if open < 0 || !strings.HasSuffix(fieldPath, "}") || !strings.HasPrefix(fieldPath, "spec.") {
		return ""
	}
	return fieldPath[open+1 : len(fieldPath)-1]
}

// EventMatches is the attach rule: same subject, same container or a
// pod-level incident, and the event's category equal or absent.
func EventMatches(ev store.K8sEvent, inc store.Incident) bool {
	switch inc.SubjectKind {
	case store.SubjectPod:
		if deref(inc.PodUID) != ev.InvolvedUID {
			return false
		}
	case store.SubjectJob:
		if deref(inc.JobUID) != ev.InvolvedUID {
			return false
		}
	default:
		return false
	}
	if inc.ContainerName != "" && FieldPathContainer(ev.FieldPath) != inc.ContainerName {
		return false
	}
	return ev.Category == nil || *ev.Category == inc.Category
}

// ApplyEvent decides what an event does to incidents. Only a probe event
// opens or bumps, and only while the container is not ready: Unhealthy fires
// for every routine readiness miss during a rollout, and a single event on a
// container that is ready again is noise. Every other event just attaches
// to the qualifying open incident with the latest last_seen_at.
func ApplyEvent(incidents []store.Incident, ev store.K8sEvent, pod *store.Pod, container *store.Container, now time.Time) (Ops, EventRef) {
	nowS := clock.Format(now)
	ref := EventRef{OpenIndex: -1}
	var ops Ops
	if deref(ev.Category) == store.CategoryProbe && pod != nil && container != nil && !container.Ready {
		k := key{container.Name, store.CategoryProbe}
		if target, reopen, ok := resolve(incidents, k); ok {
			ops.Attach = append(ops.Attach, Attach{
				IncidentID: target.ID, Reopen: reopen, LastReason: ev.Reason, LastMessage: ptrString(ev.Message), LastSeenAt: nowS,
			})
			id := target.ID
			ref.IncidentID = &id
			return ops, ref
		}
		ops.Open = append(ops.Open, Open{Incident: store.Incident{
			ClusterID: pod.ClusterID, Namespace: pod.Namespace, SubjectKind: store.SubjectPod, PodUID: &pod.UID, ContainerName: container.Name,
			WorkloadKind: pod.WorkloadKind, WorkloadName: pod.WorkloadName, Category: store.CategoryProbe,
			FirstReason: ev.Reason, LastReason: ev.Reason, LastMessage: ptrString(ev.Message),
			Image: &container.Image, ImageTag: container.ImageTag, ImageID: container.ImageID,
			Occurrences: 1, OpenedAt: ev.LastTS, LastSeenAt: nowS,
		}})
		ref.OpenIndex = 0
		return ops, ref
	}
	var best *store.Incident
	for i := range incidents {
		inc := &incidents[i]
		if inc.ClosedAt != nil || !EventMatches(ev, *inc) {
			continue
		}
		if best == nil || inc.LastSeenAt > best.LastSeenAt {
			best = inc
		}
	}
	if best != nil {
		id := best.ID
		ref.IncidentID = &id
	}
	return ops, ref
}

func ptrString(s string) *string { return &s }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/incident/ -v`
Expected: PASS. `unhealthy attaches to the open probe incident` returns both an `Attach` and `IncidentID`; the closed incident case returns no ref because the closer, not the handler, does late attaches.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/incident && git commit -m "incident: add ApplyJob, event attach rule and ApplyEvent"`

---

### Task 9: Roadmap status

**Files:**
- Modify: `docs/plans/m1-recorder/roadmap.md`

- [ ] **Step 1: Record the phase**

Under `### Phase 2: ingest + incident, pure logic` add, as the first paragraph after the heading:

```
Status: done, merged to `main` <date> (`<merge commit>`). Plan:
`02-ingest-incident.md`.
```

In the Phase 2 section's "Delivers" line replace `DiffReplicaSet` with `MapReplicaSet`. In the Phase 3 section add after "Also the deletion_reason inference (storage doc 5.3) since it needs SQL.":

```
Phase 2 hands Phase 3: `ingest.DeadInstance` (log_previous requests, one
`unobservable` artifact row per `Unobservable` instance), `incident.Ops`
(`HistoryCategories` copied into history rows, `Open`/`Attach`/`Close`
executed, `Open.HistoryIndexes` and `EventRef` resolved after insert),
`changes.WorkloadChanged` (correct `workload_*` on the pod's open
incidents), and `incident.EventCategory` (set `k8s_events.category` before
`ApplyEvent`). A message-only change of an `Unschedulable` condition produces
no operation, so `last_message` on a `scheduling` incident only updates on a
real transition; Phase 3 decides whether to refresh it from the condition
directly.
```

In "Facts a new session needs" add:

```
- `k8s.io/api` and `k8s.io/apimachinery` are pinned to v0.34.1; v0.35.0
  requires Go 1.25.
- Fixture pods for tests are real-shaped JSON under
  `internal/ingest/testdata/<scenario>/`; `internal/incident` tests read them
  through `../ingest/testdata` and drive `ingest.DiffPod` first.
```

In the "Plan files" list replace `<date>-02-ingest-incident.md (written when Phase 2 starts)` with `02-ingest-incident.md (Phase 2)`.

- [ ] **Step 2: Checkpoint**

Run: `make ascii`

Commit: `git add docs/plans/m1-recorder/roadmap.md && git commit -m "docs: record phase 2 completion and handoff in roadmap"`

The `<date>` and `<merge commit>` placeholders are filled by the orchestrator at merge time, in this same commit, since the merge is a fast-forward of the branch onto `main`.

---

## Self-review

**Spec coverage (process doc 14.1 bullets -> task):** crash loop (2, 7); OOM (2, 7); OOM exit 0 (2, 7); image pull (2, 7); config (2, 7); pending unschedulable then scheduled (3, 7); evicted (3, 7); disruption x2 (3, 7); init failure then success (2, 7); sidecar (1, 2); multi-container (2); restart_count jump (2, 7); `restartPolicy: Never` Job pod (2, 7); dead-instance index (2); first sight x3 (2, 7; "differing snapshot row" is `config`); reopen (7); `Killing` early capture (5; cache behaviour on delete is Phase 5 by the roadmap); Job -> CronJob and no CronJob (2, 4, 8); ReplicaSet -> Deployment and bare (2, 4); event timestamp pairs (5). 14.3 prohibition test (6). Storage doc 5.10 attach rule (8), 6.1 table (6), 6.2 open/attach/reopen (7, 8), 6.3 `job_finished` (8). Not in this phase by design: `recovered` closes (closer, Phase 6), `pod_deleted` closes and `deletion_reason` (Phase 3), event attach to a newly opened incident's earlier events (SQL, Phase 3), capture cache (Phase 5).

**Known spec gap left open:** `last_message` refresh on an `Unschedulable` message-only change (Task 9 hands it to Phase 3).

**`.ai` rules:** `ascii-only` (every block is ASCII; `make ascii` at each checkpoint); `tests` (every test has a trace above it; variants are rows; `cmp.Diff` on whole structs; edge cases first: nil status, no controller, `Completed`, message-only change, `pod_deleted` reopen ban, ready container gate); `comments` (blocks carry only why-comments and doc comments); `code-is-truth` (no code block names a document or section; reasons are inline); `scope` (no `Close` for pods, no capture types, no SQL; `Close` exists because Task 8 uses it); `commits` (one per task, named subjects, no trailers).

**Type consistency:** `PodChanges.History []store.ContainerStateHistory` <-> `Ops.HistoryCategories []*string` <-> `Open.HistoryIndexes []int` (Tasks 1, 7). `DeadInstance{Container, Index int64, Previous, Unobservable}` (Tasks 1, 2). `OwnerResolver.ReplicaSetOwner/JobOwner(namespace, name string) *metav1.OwnerReference` (Tasks 1, 2, 7's `noOwners`). `JobChanges{FirstSight, Job, Failed, Completed}` (Tasks 4, 8). `ApplyEvent(incidents, ev, pod, container, now) (Ops, EventRef)` (Task 8 test and code agree on parameter order). `ContainerCategory(state, reason, lastTerminatedReason)` (Tasks 6, 7). `ptrString` is defined in both `ingest` and `incident` (separate packages). `resolve` (Task 7) is reused by Task 8's `ApplyEvent`; `resolveJob` is its job twin.

