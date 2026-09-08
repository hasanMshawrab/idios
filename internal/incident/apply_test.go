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

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
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

func scenario(t *testing.T, now time.Time, files ...string) ingest.PodChanges {
	t.Helper()
	var snap *ingest.PodSnapshot
	var ch ingest.PodChanges
	for _, f := range files {
		ch = ingest.DiffPod(snap, loadPod(t, f), 1, noOwners{}, ingest.Workload{}, now)
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
	// The node is filled from the pod row at open, and every fixture pod here
	// is placed on node-a; the unschedulable one has no node yet, which
	// unplaced spells out below.
	open := func(podUID, workloadKind, workloadName, container, category, reason string, openedAt string) store.Incident {
		return store.Incident{
			ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr(podUID), ContainerName: container,
			WorkloadKind: workloadKind, WorkloadName: workloadName, Category: category, FirstReason: reason, LastReason: reason,
			NodeName: ptr("node-a"), Occurrences: 1, OpenedAt: openedAt, LastSeenAt: now,
		}
	}
	// A scheduling incident opens before placement, so it names no node and is
	// never back-filled with one.
	unplaced := func(i store.Incident) store.Incident {
		i.NodeName = nil
		return i
	}
	withImage := func(i store.Incident, image string, tag *string, id *string) store.Incident {
		i.Image, i.ImageTag, i.ImageID = ptr(image), tag, id
		return i
	}
	withMessage := func(i store.Incident, msg string) store.Incident {
		i.LastMessage = ptr(msg)
		return i
	}
	// A pod of a Job carries the job uid so the Job's own incident and the pod
	// incidents under it can be listed together; every other case above and
	// below leaves it nil, which is the other half of the rule.
	withJob := func(i store.Incident, jobUID string) store.Incident {
		i.JobUID = ptr(jobUID)
		return i
	}
	crash := withImage(open("pod-crash", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "CrashLoopBackOff", "2026-08-27T11:55:00.000000Z"), web, ptr("1.4.2"), webID)
	crashAttach := Attach{IncidentID: 7, LastReason: "CrashLoopBackOff", LastSeenAt: now, HistoryIndexes: []int{0}}
	catCrash := []*string{ptr(store.CategoryCrash)}
	msg3 := "0/3 nodes are available: 3 Insufficient cpu. preemption: 0/3 nodes are available: 3 No preemption victims found for incoming pod."
	evictMsg := "The node was low on resource: memory. Threshold quantity: 100Mi, available: 52Mi. Container api was using 900Mi, request is 0, has larger consumption of memory."

	unsched := unplaced(withMessage(open("pod-unsched", "ReplicaSet", "web-2b3c4", "", store.CategoryScheduling, "Unschedulable", "2026-08-27T11:45:01.000000Z"), msg3))
	unsched4 := withMessage(unsched, "0/4 nodes are available: 4 Insufficient cpu. preemption: 0/4 nodes are available: 4 No preemption victims found for incoming pod.")
	schedOpen := incident(9, "pod-unsched", "", store.CategoryScheduling, nil, nil)
	schedClosed := incident(9, "pod-unsched", "", store.CategoryScheduling, ptr("2026-08-27T11:50:00.000000Z"), ptr(store.CloseRecovered))

	cases := []struct {
		name      string
		files     []string
		incidents []store.Incident
		now       time.Time
		want      Ops
	}{
		{"unschedulable inside the grace window is nothing", []string{"unschedulable/s1.json"}, nil, testNow.Add(-14*time.Minute - 29*time.Second), Ops{}},
		{"unschedulable past the grace window opens", []string{"unschedulable/s1.json"}, nil, time.Time{}, Ops{Open: []Open{{Incident: unsched}}}},
		{"unschedulable persisting past the window opens", []string{"unschedulable/s1.json", "unschedulable/s2.json"}, nil, time.Time{}, Ops{Open: []Open{{Incident: unsched4}}}},
		{"unschedulable persisting does not attach", []string{"unschedulable/s1.json", "unschedulable/s2.json"}, []store.Incident{schedOpen}, time.Time{}, Ops{}},
		{"unschedulable persisting does not reopen a closed incident", []string{"unschedulable/s1.json", "unschedulable/s2.json"}, []store.Incident{schedClosed}, time.Time{}, Ops{Open: []Open{{Incident: unsched4}}}},
		{"scheduled closes the open scheduling incident", []string{"unschedulable/s1.json", "unschedulable/s3.json"}, []store.Incident{schedOpen}, time.Time{},
			Ops{Close: []Close{{IncidentID: 9, Reason: store.CloseRecovered, ClosedAt: now}}, HistoryCategories: []*string{nil}}},
		{"scheduled with no incident is nothing", []string{"unschedulable/s1.json", "unschedulable/s3.json"}, nil, time.Time{}, Ops{HistoryCategories: []*string{nil}}},
		{"crash loop opens", []string{"crash-loop/before.json", "crash-loop/after.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: crash, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"crash loop attaches to open incident", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-crash", "api", store.CategoryCrash, nil, nil)}, time.Time{},
			Ops{Attach: []Attach{crashAttach}, HistoryCategories: catCrash}},
		{"different container does not attach", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-crash", "worker", store.CategoryCrash, nil, nil)}, time.Time{},
			Ops{Open: []Open{{Incident: crash, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"different pod does not attach", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-other", "api", store.CategoryCrash, nil, nil)}, time.Time{},
			Ops{Open: []Open{{Incident: crash, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"recovered incident reopens", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-crash", "api", store.CategoryCrash, ptr("2026-08-27T11:00:00.000000Z"), ptr(store.CloseRecovered))}, time.Time{},
			Ops{Attach: []Attach{{IncidentID: 7, Reopen: true, LastReason: "CrashLoopBackOff", LastSeenAt: now, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"pod_deleted incident never reopens", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-crash", "api", store.CategoryCrash, ptr("2026-08-27T11:00:00.000000Z"), ptr(store.ClosePodDeleted))}, time.Time{},
			Ops{Open: []Open{{Incident: crash, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"manually closed incident never reopens", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{incident(7, "pod-crash", "api", store.CategoryCrash, ptr("2026-08-27T11:00:00.000000Z"), ptr(store.CloseManual))}, time.Time{},
			Ops{Open: []Open{{Incident: crash, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"latest closed incident reopens, skipping a manual close", []string{"crash-loop/before.json", "crash-loop/after.json"},
			[]store.Incident{
				incident(5, "pod-crash", "api", store.CategoryCrash, ptr("2026-08-27T11:00:00.000000Z"), ptr(store.CloseRecovered)),
				incident(6, "pod-crash", "api", store.CategoryCrash, ptr("2026-08-27T11:30:00.000000Z"), ptr(store.CloseManual)),
			}, time.Time{},
			Ops{Attach: []Attach{{IncidentID: 5, Reopen: true, LastReason: "CrashLoopBackOff", LastSeenAt: now, HistoryIndexes: []int{0}}}, HistoryCategories: catCrash}},
		{"oom from lastState", []string{"oom/before.json", "oom/after.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withImage(open("pod-oom", "ReplicaSet", "mem-hog-5f6d7", "api", store.CategoryOOM, "OOMKilled", "2026-08-27T11:52:30.000000Z"),
				"registry.example.com/mem-hog:2.0.0", ptr("2.0.0"), ptr("registry.example.com/mem-hog@sha256:2222")), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryOOM)}}},
		{"oom with exit code zero", []string{"oom-exit-zero/before.json", "oom-exit-zero/after.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withJob(withImage(open("pod-oom0", "Job", "report-28812345", "report", store.CategoryOOM, "OOMKilled", "2026-08-27T11:58:00.000000Z"),
				"registry.example.com/report:0.9.1", ptr("0.9.1"), ptr("registry.example.com/report@sha256:3333")), "job-report-1"), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryOOM)}}},
		{"image pull resolving is not an operation", []string{"image-pull/s1.json", "image-pull/s2.json", "image-pull/s3.json"},
			[]store.Incident{incident(3, "pod-pull", "api", store.CategoryImagePull, nil, nil)}, time.Time{},
			Ops{HistoryCategories: []*string{nil}}},
		{"config error after creating", []string{"config/before.json", "config/after.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withImage(open("pod-cfg", "ReplicaSet", "web-5b8c7", "api", store.CategoryConfig, "CreateContainerConfigError", now), web, ptr("1.4.2"), nil), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryConfig)}}},
		{"first sight config error opens without history", []string{"first-sight-config-error/pod.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withImage(open("pod-cfg-first", "ReplicaSet", "web-5b8c7", "api", store.CategoryConfig, "CreateContainerConfigError", "2026-08-27T11:45:00.000000Z"), web, ptr("1.4.2"), nil)}}}},
		{"first sight healthy opens nothing", []string{"first-sight-healthy/pod.json"}, nil, time.Time{}, Ops{}},
		{"completed init container opens nothing", []string{"init-then-success/before.json", "init-then-success/after.json"}, nil, time.Time{},
			Ops{HistoryCategories: []*string{nil, nil}}},
		{"restart jump opens from the reconstructed row", []string{"restart-jump/before.json", "restart-jump/after.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withMessage(withImage(open("pod-jump", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "Error", "2026-08-27T11:58:00.000000Z"), web, ptr("1.4.2"), webID), "panic: read of a closed queue"), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryCrash), nil}}},
		{"job pod terminated with error", []string{"job-never-error/before.json", "job-never-error/after.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withJob(withImage(open("pod-jobfail", "Job", "import-28812346", "worker", store.CategoryCrash, "Error", "2026-08-27T11:50:00.000000Z"),
				"registry.example.com/import:5.2.0", ptr("5.2.0"), ptr("registry.example.com/import@sha256:8888")), "job-import-1"), HistoryIndexes: []int{0}}},
				HistoryCategories: catCrash}},
		{"evicted opens pod-level node_pressure next to the container crash", []string{"evicted/before.json", "evicted/after.json"}, nil, time.Time{},
			Ops{Open: []Open{
				{Incident: withImage(open("pod-evict", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "Error", "2026-08-27T11:57:30.000000Z"), web, ptr("1.4.2"), webID), HistoryIndexes: []int{0}},
				{Incident: withMessage(open("pod-evict", "ReplicaSet", "web-7d9f8c6b5", "", store.CategoryNodePressure, "Evicted", now), evictMsg)},
			}, HistoryCategories: catCrash}},
		{"disruption by eviction api opens nothing", []string{"disruption-api/before.json", "disruption-api/after.json"}, nil, time.Time{}, Ops{}},
		{"disruption by taint manager is rescheduled", []string{"disruption-api/before.json", "disruption-taint/after.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withMessage(open("pod-drain", "ReplicaSet", "web-7d9f8c6b5", "", store.CategoryRescheduled, "DeletionByTaintManager", "2026-08-27T11:59:30.000000Z"), "Taint manager: deleting due to NoExecute taint")}}}},
		{"both node_pressure rules coalesce", []string{"disruption-kubelet/before.json", "disruption-kubelet/after.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: func() store.Incident {
				i := withMessage(open("pod-press", "ReplicaSet", "web-7d9f8c6b5", "", store.CategoryNodePressure, "TerminationByKubelet", "2026-08-27T11:59:40.000000Z"), "The node was low on resource: memory.")
				i.LastReason, i.Occurrences = "Evicted", 2
				return i
			}()}}}},
		{"unclean exit on a pod with nothing open opens", []string{"terminating-exit/before.json", "terminating-exit/after.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withImage(open("pod-term", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryUncleanExit, "Error", "2026-08-27T11:59:40.000000Z"), web, ptr("1.4.2"), webID), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryUncleanExit)}}},
		{"unclean exit attaches to the container's open crash", []string{"terminating-exit/before.json", "terminating-exit/after.json"},
			[]store.Incident{incident(7, "pod-term", "api", store.CategoryCrash, nil, nil)}, time.Time{},
			Ops{Attach: []Attach{{IncidentID: 7, LastReason: "Error", LastSeenAt: now, HistoryIndexes: []int{0}}}, HistoryCategories: []*string{ptr(store.CategoryCrash)}}},
		{"unclean exit attaches to the container's open probe", []string{"terminating-exit/before.json", "terminating-exit/after.json"},
			[]store.Incident{incident(7, "pod-term", "api", store.CategoryProbe, nil, nil)}, time.Time{},
			Ops{Attach: []Attach{{IncidentID: 7, LastReason: "Error", LastSeenAt: now, HistoryIndexes: []int{0}}}, HistoryCategories: []*string{ptr(store.CategoryProbe)}}},
		{"unclean exit under a pod-level rescheduled opens nothing", []string{"terminating-exit/before.json", "terminating-exit/after.json"},
			[]store.Incident{incident(7, "pod-term", "", store.CategoryRescheduled, nil, nil)}, time.Time{},
			Ops{HistoryCategories: []*string{ptr(store.CategoryUncleanExit)}}},
		{"oom while terminating is still oom", []string{"terminating-exit/before.json", "terminating-exit/oom.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withImage(open("pod-term", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryOOM, "OOMKilled", "2026-08-27T11:59:40.000000Z"), web, ptr("1.4.2"), webID), HistoryIndexes: []int{0}}},
				HistoryCategories: []*string{ptr(store.CategoryOOM)}}},
		{"first sight image pull on a terminating pod opens", []string{"terminating-exit/first-sight-pull.json"}, nil, time.Time{},
			Ops{Open: []Open{{Incident: withImage(open("pod-term-pull", "ReplicaSet", "web-66c9d", "api", store.CategoryImagePull, "ImagePullBackOff", "2026-08-27T11:45:00.000000Z"),
				"registry.example.com/web:does-not-exist", ptr("does-not-exist"), nil)}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now := c.now
			if now.IsZero() {
				now = testNow
			}
			got := Apply(c.incidents, scenario(t, now, c.files...), now, Policy{SchedulingGrace: time.Minute})
			if d := cmp.Diff(c.want, got, cmpopts.EquateEmpty()); d != "" {
				t.Fatal(d)
			}
		})
	}
}

// The kubelet's reading of the termination log is the one line that says why
// the process died, so it becomes the incident's last_message when the
// termination opens the incident and when it bumps an open one.
func TestTerminatedMessageBecomesLastMessage(t *testing.T) {
	now := clock.Format(testNow)
	pod := store.Pod{
		UID: "pod-msg", ClusterID: 1, Namespace: "idios-smoke", Name: "web-0", NodeName: ptr("node-a"),
		WorkloadKind: "ReplicaSet", WorkloadName: "web-7d9f8c6b5", CreatedAt: "2026-08-27T11:00:00.000000Z",
	}
	container := store.Container{
		PodUID: "pod-msg", Name: "api", Kind: store.ContainerKindApp, Image: "registry.example.com/web:1.4.2",
		State: store.StateTerminated, Reason: ptr("Error"), Message: ptr("panic: config missing"),
		ExitCode: ptr[int64](1), UpdatedAt: now,
	}
	changes := ingest.PodChanges{
		Pod: pod, Containers: []store.Container{container},
		History: []store.ContainerStateHistory{{
			PodUID: "pod-msg", ContainerName: "api", Image: container.Image, State: store.StateTerminated,
			Reason: ptr("Error"), Message: ptr("panic: config missing"), ExitCode: ptr[int64](1),
			K8sFinishedAt: ptr("2026-08-27T11:58:00.000000Z"), ObservedAt: now,
		}},
	}
	t.Run("on open", func(t *testing.T) {
		ops := Apply(nil, changes, testNow, Policy{})
		if len(ops.Open) != 1 {
			t.Fatalf("opens = %d, want 1", len(ops.Open))
		}
		if d := cmp.Diff(ptr("panic: config missing"), ops.Open[0].Incident.LastMessage); d != "" {
			t.Fatal(d)
		}
	})
	t.Run("on attach", func(t *testing.T) {
		ops := Apply([]store.Incident{incident(7, "pod-msg", "api", store.CategoryCrash, nil, nil)}, changes, testNow, Policy{})
		want := []Attach{{IncidentID: 7, LastReason: "Error", LastMessage: ptr("panic: config missing"), LastSeenAt: now, HistoryIndexes: []int{0}}}
		if d := cmp.Diff(want, ops.Attach); d != "" {
			t.Fatal(d)
		}
	})
	t.Run("first sight takes it from the container row", func(t *testing.T) {
		first := changes
		first.FirstSight, first.History = true, nil
		ops := Apply(nil, first, testNow, Policy{})
		if len(ops.Open) != 1 {
			t.Fatalf("opens = %d, want 1", len(ops.Open))
		}
		if d := cmp.Diff(ptr("panic: config missing"), ops.Open[0].Incident.LastMessage); d != "" {
			t.Fatal(d)
		}
	})
}
