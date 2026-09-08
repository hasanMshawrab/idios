package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

type fakeResolver struct {
	rs   map[string]*metav1.OwnerReference
	jobs map[string]*metav1.OwnerReference
}

func (f fakeResolver) ReplicaSetOwner(ns, name string) *metav1.OwnerReference {
	return f.rs[ns+"/"+name]
}
func (f fakeResolver) JobOwner(ns, name string) *metav1.OwnerReference { return f.jobs[ns+"/"+name] }

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

func runScenario(t *testing.T, r OwnerResolver, stored Workload, files ...string) PodChanges {
	t.Helper()
	var snap *PodSnapshot
	var ch PodChanges
	for _, f := range files {
		ch = DiffPod(snap, loadPod(t, f), 1, r, stored, testNow)
		snap = snapshotFrom(snap, ch)
	}
	return ch
}

func TestDiffPodScenarios(t *testing.T) {
	now := clock.Format(testNow)
	web := "registry.example.com/web:1.4.2"
	webID := ptr("registry.example.com/web@sha256:1111")
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
				K8sStartedAt: ptr("2026-08-27T11:45:08.000000Z"), K8sFinishedAt: ptr("2026-08-27T11:58:00.000000Z"), ObservedAt: now,
			}},
			PodReasonChanged: true,
			DeadInstances:    []DeadInstance{{Container: "report", Index: 0, Previous: false}},
		}},
		{"image-pull", []string{"image-pull/s1.json", "image-pull/s2.json", "image-pull/s3.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-pull", ContainerName: "api", Image: "registry.example.com/web:does-not-exist", ImageID: ptr("registry.example.com/web@sha256:4444"), ContainerID: ptr("containerd://ppp"),
				State: store.StateRunning, RestartCount: 0, K8sStartedAt: ptr("2026-08-27T11:59:00.000000Z"), ObservedAt: now,
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
					K8sStartedAt: ptr("2026-08-27T11:45:40.000000Z"), K8sFinishedAt: ptr("2026-08-27T11:45:50.000000Z"), ObservedAt: now,
				},
				{
					PodUID: "pod-init", ContainerName: "api", Image: web, ImageID: webID, ContainerID: ptr("containerd://aaa2"),
					State: store.StateRunning, RestartCount: 0, K8sStartedAt: ptr("2026-08-27T11:46:00.000000Z"), ObservedAt: now,
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
					State: store.StateTerminated, Reason: ptr("Error"), Message: ptr("panic: read of a closed queue"), ExitCode: ptr[int64](1), Signal: ptr[int64](0), RestartCount: 3,
					K8sStartedAt: ptr("2026-08-27T11:57:00.000000Z"), K8sFinishedAt: ptr("2026-08-27T11:58:00.000000Z"), ObservedAt: now, GapReconstructed: true,
				},
				{
					PodUID: "pod-jump", ContainerName: "api", Image: web, ImageID: webID, ContainerID: ptr("containerd://j4"),
					State: store.StateRunning, RestartCount: 4, K8sStartedAt: ptr("2026-08-27T11:59:00.000000Z"), ObservedAt: now,
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
				K8sStartedAt: ptr("2026-08-27T11:45:08.000000Z"), K8sFinishedAt: ptr("2026-08-27T11:50:00.000000Z"), ObservedAt: now,
			}},
			PodReasonChanged: true,
			DeadInstances:    []DeadInstance{{Container: "worker", Index: 0, Previous: false}},
		}},
		{"dead-index-running", []string{"dead-index-running/before.json", "dead-index-running/after.json"}, PodChanges{
			History: []store.ContainerStateHistory{{
				PodUID: "pod-idx", ContainerName: "api", Image: web, ImageID: webID, ContainerID: ptr("containerd://k2"),
				State: store.StateRunning, RestartCount: 2, K8sStartedAt: ptr("2026-08-27T11:56:00.000000Z"), ObservedAt: now,
			}},
			DeadInstances: []DeadInstance{{Container: "api", Index: 1, Previous: true}},
		}},
		{"first-sight-healthy", []string{"first-sight-healthy/pod.json"}, PodChanges{FirstSight: true}},
		{"first-sight-config-error", []string{"first-sight-config-error/pod.json"}, PodChanges{FirstSight: true}},
	}
	ignore := cmpopts.IgnoreFields(PodChanges{}, "Pod", "Containers", "Conditions", "CurrentConditions")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runScenario(t, fakeResolver{}, Workload{}, c.files...)
			if d := cmp.Diff(c.want, got, ignore); d != "" {
				t.Fatal(d)
			}
		})
	}
}

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
			got := runScenario(t, fakeResolver{}, Workload{}, c.files...)
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
		stored   Workload
		want     want
	}{
		{"ReplicaSet to Deployment", "crash-loop/before.json", deploy, Workload{}, want{"ReplicaSet", "web-7d9f8c6b5", "rs-web-1", "Deployment", "web"}},
		{"bare ReplicaSet falls back to controller", "crash-loop/before.json", fakeResolver{}, Workload{}, want{"ReplicaSet", "web-7d9f8c6b5", "rs-web-1", "ReplicaSet", "web-7d9f8c6b5"}},
		{"Job to CronJob", "oom-exit-zero/before.json", cron, Workload{}, want{"Job", "report-28812345", "job-report-1", "CronJob", "report"}},
		{"Job without CronJob falls back to controller", "oom-exit-zero/before.json", fakeResolver{}, Workload{}, want{"Job", "report-28812345", "job-report-1", "Job", "report-28812345"}},
		{"pruned Job resolves from the store", "oom-exit-zero/before.json", fakeResolver{}, Workload{Kind: "CronJob", Name: "report"}, want{"Job", "report-28812345", "job-report-1", "CronJob", "report"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := runScenario(t, c.resolver, c.stored, c.file).Pod
			got := want{p.ControllerKind, p.ControllerName, p.ControllerUID, p.WorkloadKind, p.WorkloadName}
			if got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}

	t.Run("no controller", func(t *testing.T) {
		pod := loadPod(t, "crash-loop/before.json")
		pod.OwnerReferences = nil
		p := DiffPod(nil, pod, 1, fakeResolver{}, Workload{}, testNow).Pod
		got := want{p.ControllerKind, p.ControllerName, p.ControllerUID, p.WorkloadKind, p.WorkloadName}
		if w := (want{"none", "", "", "none", ""}); got != w {
			t.Fatalf("got %+v, want %+v", got, w)
		}
	})

	t.Run("later resolution flags WorkloadChanged", func(t *testing.T) {
		first := DiffPod(nil, loadPod(t, "crash-loop/before.json"), 1, fakeResolver{}, Workload{}, testNow)
		if first.WorkloadChanged {
			t.Fatal("first sight reported WorkloadChanged")
		}
		second := DiffPod(snapshotFrom(nil, first), loadPod(t, "crash-loop/before.json"), 1, deploy, Workload{}, testNow)
		if !second.WorkloadChanged || second.Pod.WorkloadKind != "Deployment" || second.Pod.WorkloadName != "web" {
			t.Fatalf("WorkloadChanged = %v, workload = %s/%s", second.WorkloadChanged, second.Pod.WorkloadKind, second.Pod.WorkloadName)
		}
		third := DiffPod(snapshotFrom(nil, second), loadPod(t, "crash-loop/before.json"), 1, deploy, Workload{}, testNow)
		if third.WorkloadChanged {
			t.Fatal("unchanged workload reported WorkloadChanged")
		}
	})
}

// A pruned Job or ReplicaSet leaves both the informer store and its own row,
// so a late event on its pod resolves no further than the controller and must
// not rewrite the owner the pod already had.
func TestPrunedControllerKeepsTheOwner(t *testing.T) {
	deploy := fakeResolver{rs: map[string]*metav1.OwnerReference{"idios-smoke/web-7d9f8c6b5": owner("Deployment", "web", "dep-web")}}
	adopted := func(t *testing.T) *corev1.Pod {
		t.Helper()
		pod := loadPod(t, "crash-loop/before.json")
		pod.OwnerReferences = []metav1.OwnerReference{*owner("ReplicaSet", "web-6f5d4c3b2", "rs-web-2")}
		pod.OwnerReferences[0].Controller = ptr(true)
		return pod
	}
	cases := []struct {
		name            string
		pod             func(*testing.T) *corev1.Pod
		wantKind        string
		wantName        string
		wantControllers [2]string
		wantChanged     bool
	}{
		{"same controller keeps the Deployment", func(t *testing.T) *corev1.Pod { return loadPod(t, "crash-loop/before.json") },
			"Deployment", "web", [2]string{"web-7d9f8c6b5", "rs-web-1"}, false},
		{"adoption by another ReplicaSet wins over the stale owner", adopted,
			"ReplicaSet", "web-6f5d4c3b2", [2]string{"web-6f5d4c3b2", "rs-web-2"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			first := DiffPod(nil, loadPod(t, "crash-loop/before.json"), 1, deploy, Workload{}, testNow)
			got := DiffPod(snapshotFrom(nil, first), c.pod(t), 1, fakeResolver{}, Workload{}, testNow)
			want := first.Pod
			want.ControllerName, want.ControllerUID = c.wantControllers[0], c.wantControllers[1]
			want.WorkloadKind, want.WorkloadName = c.wantKind, c.wantName
			if d := cmp.Diff(want, got.Pod); d != "" {
				t.Error(d)
			}
			if got.WorkloadChanged != c.wantChanged {
				t.Errorf("WorkloadChanged = %v, want %v", got.WorkloadChanged, c.wantChanged)
			}
		})
	}
}

func TestPodRowMapsEveryField(t *testing.T) {
	now := clock.Format(testNow)
	pod := loadPod(t, "oom-exit-zero/after.json")
	deletion := metav1.Date(2026, 8, 27, 11, 59, 0, 0, time.UTC)
	pod.DeletionTimestamp = &deletion
	pod.Status.Reason = "Evicted"
	pod.Status.Message = "The node was low on resource: memory."

	first := DiffPod(nil, loadPod(t, "oom-exit-zero/before.json"), 1, fakeResolver{}, Workload{}, testNow)
	earlier := clock.Format(testNow.Add(-time.Hour))
	first.Pod.FirstSeenAt = earlier
	got := DiffPod(snapshotFrom(nil, first), pod, 1, fakeResolver{}, Workload{}, testNow)

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

// The API server rewrites deletionTimestamp when a later delete arrives with
// a shorter grace period, so the value that means "termination began" is the
// first one seen, never the last.
func TestFirstDeletionTimestampIsKept(t *testing.T) {
	first := "2026-08-27T11:59:30.000000Z"
	later := "2026-08-27T11:59:50.000000Z"
	withDeletion := func(ts *string) *corev1.Pod {
		pod := loadPod(t, "disruption-api/after.json")
		pod.DeletionTimestamp = nil
		if ts != nil {
			parsed, err := clock.Parse(*ts)
			if err != nil {
				t.Fatal(err)
			}
			at := metav1.NewTime(parsed)
			pod.DeletionTimestamp = &at
		}
		return pod
	}
	cases := []struct {
		name     string
		snapshot *string
		object   *string
		want     *string
	}{
		{"first sight takes the object's", nil, ptr(first), ptr(first)},
		{"snapshot without one takes the object's", nil, ptr(first), ptr(first)},
		{"snapshot with one keeps it over a later one", ptr(first), ptr(later), ptr(first)},
		{"snapshot with one keeps it when the object has none", ptr(first), nil, ptr(first)},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var snap *PodSnapshot
			if i > 0 {
				before := DiffPod(nil, withDeletion(c.snapshot), 1, fakeResolver{}, Workload{}, testNow)
				snap = snapshotFrom(nil, before)
			}
			got := DiffPod(snap, withDeletion(c.object), 1, fakeResolver{}, Workload{}, testNow)
			if d := cmp.Diff(c.want, got.Pod.DeletionRequestedAt); d != "" {
				t.Error(d)
			}
		})
	}
}

// A termination's message travels with the history row it produces, capped
// as everywhere else; a waiting state has none of its own, and a
// reconstructed row answers for the instance it reconstructs.
func TestTerminatedMessageOnHistoryRows(t *testing.T) {
	started := metav1.Date(2026, 8, 27, 11, 50, 0, 0, time.UTC)
	finished := metav1.Date(2026, 8, 27, 11, 55, 0, 0, time.UTC)
	running := corev1.ContainerStatus{
		Name: "api", Ready: true, ContainerID: "containerd://c0",
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}},
	}
	terminated := func(msg string) corev1.ContainerStatus {
		return corev1.ContainerStatus{
			Name: "api", ContainerID: "containerd://c0",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 1, Reason: "Error", Message: msg, StartedAt: started, FinishedAt: finished}},
		}
	}
	restarted := func(restarts int32, msg string) corev1.ContainerStatus {
		return corev1.ContainerStatus{
			Name: "api", Ready: true, RestartCount: restarts, ContainerID: "containerd://c" + strconv.Itoa(int(restarts)),
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: finished}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 137, Reason: "OOMKilled", Message: msg, StartedAt: started, FinishedAt: finished, ContainerID: "containerd://c0"}},
		}
	}
	cases := []struct {
		name string
		next corev1.ContainerStatus
		want []*string
	}{
		{"terminated carries its message", terminated("panic: config missing"), []*string{ptr("panic: config missing")}},
		{"terminated without one carries none", terminated(""), []*string{nil}},
		{"waiting carries none of its own", corev1.ContainerStatus{
			Name: "api", RestartCount: 1,
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off 10s"}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 1, Reason: "Error", Message: "panic: config missing", StartedAt: started, FinishedAt: finished}},
		}, []*string{nil}},
		{"a reconstructed row takes the dead instance's message", restarted(3, "out of memory"), []*string{ptr("out of memory"), nil}},
		{"a reconstructed row is capped too", restarted(3, strings.Repeat("a", 5000)), []*string{ptr(strings.Repeat("a", 4096)), nil}},
	}
	podWith := func(st corev1.ContainerStatus) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{UID: "pod-msg", Namespace: "ns", Name: "api-0"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "registry.example.com/web:1.4.2"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{st}},
		}
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap := snapshotFrom(nil, DiffPod(nil, podWith(running), 1, fakeResolver{}, Workload{}, testNow))
			got := DiffPod(snap, podWith(c.next), 1, fakeResolver{}, Workload{}, testNow)
			var messages []*string
			for _, h := range got.History {
				messages = append(messages, h.Message)
			}
			if d := cmp.Diff(c.want, messages); d != "" {
				t.Fatal(d)
			}
		})
	}
}
