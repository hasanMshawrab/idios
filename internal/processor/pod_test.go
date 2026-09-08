package processor

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
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

// The node comes from the pod row at open; every fixture pod of these tests
// but the unschedulable one is placed on node-a.
func podIncident(id int64, podUID, workloadKind, workloadName, container, category, first, last, openedAt string) store.Incident {
	return store.Incident{
		ID: id, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr(podUID), ContainerName: container,
		WorkloadKind: workloadKind, WorkloadName: workloadName, Category: category, FirstReason: first, LastReason: last,
		NodeName: ptr("node-a"), Occurrences: 1, OpenedAt: openedAt, LastSeenAt: clock.Format(testNow),
	}
}

// unplaced is the incident of a pod that has no node yet: a scheduling
// incident opens before placement, and the node is never back-filled.
func unplaced(i store.Incident) store.Incident {
	i.NodeName = nil
	return i
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
	jump.Occurrences, jump.LastMessage = 2, ptr("panic: read of a closed queue")
	unsched := unplaced(podIncident(1, "pod-unsched", "ReplicaSet", "web-2b3c4", "", store.CategoryScheduling, "Unschedulable", "Unschedulable", "2026-08-27T11:45:01.000000Z"))
	unsched.LastMessage = ptr("0/4 nodes are available: 4 Insufficient cpu. preemption: 0/4 nodes are available: 4 No preemption victims found for incoming pod.")
	unsched.ClosedAt, unsched.CloseReason = ptr(now), ptr(store.CloseRecovered)
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
					State: store.StateTerminated, Reason: ptr("Error"), Message: ptr("panic: read of a closed queue"), ExitCode: ptr[int64](1), Signal: ptr[int64](0), RestartCount: 3, Category: ptr(store.CategoryCrash),
					K8sStartedAt: ptr("2026-08-27T11:57:00.000000Z"), K8sFinishedAt: ptr("2026-08-27T11:58:00.000000Z"), ObservedAt: now, GapReconstructed: true},
				{ID: 2, PodUID: "pod-jump", ContainerName: "api", Image: web, ImageID: ptr(webID), ContainerID: ptr("containerd://j4"),
					State: store.StateRunning, RestartCount: 4, K8sStartedAt: ptr("2026-08-27T11:59:00.000000Z"), ObservedAt: now},
			},
			[]store.Artifact{{ID: 1, PodUID: "pod-jump", IncidentID: ptr[int64](1), ContainerName: "api", Kind: store.ArtifactLogPrevious, RestartCount: 2,
				CaptureGap: ptr(store.GapUnobservable), CapturedAt: now}},
			[2]string{"ReplicaSet", "web-7d9f8c6b5"}},
		{"first sight healthy records the snapshot only", nil, steps("first-sight-healthy/pod.json"), "pod-fresh", nil, nil, nil, [2]string{"ReplicaSet", "web-7d9f8c6b5"}},
		{"unschedulable keeps the latest message without a new row and closes on scheduling", nil, steps("unschedulable/s1.json", "unschedulable/s2.json", "unschedulable/s3.json"), "pod-unsched",
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

// The condition rows are written whatever the incident decision is; that is
// TestUnschedulableConditionRows' concern and not repeated here.
func TestSchedulingWaitsOutTheGrace(t *testing.T) {
	h := newHarness(t)
	h.clk.Set(time.Date(2026, 8, 27, 11, 45, 31, 0, time.UTC))
	h.feed(t, steps("unschedulable/s1.json"))
	diff(t, []store.Incident(nil), h.incidents(t, "pod-unsched"))

	h.clk.Set(testNow)
	h.feed(t, steps("unschedulable/s2.json"))
	open := unplaced(podIncident(1, "pod-unsched", "ReplicaSet", "web-2b3c4", "", store.CategoryScheduling, "Unschedulable", "Unschedulable", "2026-08-27T11:45:01.000000Z"))
	open.LastMessage = ptr("0/4 nodes are available: 4 Insufficient cpu. preemption: 0/4 nodes are available: 4 No preemption victims found for incoming pod.")
	diff(t, []store.Incident{open}, h.incidents(t, "pod-unsched"))

	h.feed(t, steps("unschedulable/s3.json"))
	closed := open
	closed.ClosedAt, closed.CloseReason = ptr(clock.Format(testNow)), ptr(store.CloseRecovered)
	diff(t, []store.Incident{closed}, h.incidents(t, "pod-unsched"))
}

func TestUnschedulableConditionRows(t *testing.T) {
	h := newHarness(t)
	h.feed(t, steps("unschedulable/s1.json", "unschedulable/s2.json", "unschedulable/s3.json"))
	now := clock.Format(testNow)
	want := []store.PodCondition{
		{ID: 1, PodUID: "pod-unsched", Type: "PodScheduled", Status: "False", Reason: "Unschedulable",
			Message:         ptr("0/3 nodes are available: 3 Insufficient cpu. preemption: 0/3 nodes are available: 3 No preemption victims found for incoming pod."),
			K8sTransitionAt: ptr("2026-08-27T11:45:01.000000Z"), ObservedAt: now},
		{ID: 2, PodUID: "pod-unsched", Type: "PodScheduled", Status: "True", K8sTransitionAt: ptr("2026-08-27T11:58:00.000000Z"), ObservedAt: now},
	}
	diff(t, want, h.conditions(t, "pod-unsched"))
}

// A disruption that opens no incident still records the condition that named
// it, so the pod's timeline shows why it left without an incident row of its
// own.
func TestDisruptionConditionRowWithNoIncident(t *testing.T) {
	h := newHarness(t)
	h.feed(t, steps("disruption-api/before.json", "disruption-api/after.json"))
	now := clock.Format(testNow)
	want := []store.PodCondition{
		{ID: 1, PodUID: "pod-drain", Type: "Ready", Status: "True", K8sTransitionAt: ptr("2026-08-27T11:45:10.000000Z"), ObservedAt: now},
		{ID: 2, PodUID: "pod-drain", Type: "DisruptionTarget", Status: "True", Reason: "EvictionByEvictionAPI",
			Message:         ptr("Eviction API: evicting"),
			K8sTransitionAt: ptr("2026-08-27T11:59:30.000000Z"), ObservedAt: now},
	}
	diff(t, want, h.conditions(t, "pod-drain"))
	diff(t, []store.Incident(nil), h.incidents(t, "pod-drain"))
}

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
		{"clean single run: no incident, no log, only the manifest", steps("init-clean-exit/before.json", "init-clean-exit/after.json"), nil, []CaptureRequest{
			request("pod-clean", "web-3c4d5f6-clean1", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerRestart, nil),
		}},
		{"clean exit with an incident on the container still captures", steps("oom-exit-zero/before.json", "oom-exit-zero/after.json"), nil, []CaptureRequest{
			request("pod-oom0", "report-28812345-x9k2p", "report", store.ArtifactLogPrevious, 0, false, TriggerRestart, one),
			request("pod-oom0", "report-28812345-x9k2p", "report", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, one),
			request("pod-oom0", "report-28812345-x9k2p", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerRestart, nil),
		}},
		{"clean exit after a restart still captures", steps("init-then-success/before.json", "init-then-success/after.json"), nil, []CaptureRequest{
			request("pod-init", "web-9a8b7-init1", "init-db", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, one),
			request("pod-init", "web-9a8b7-init1", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerIncidentOpen, nil),
			request("pod-init", "web-9a8b7-init1", "init-db", store.ArtifactLogPrevious, 1, false, TriggerRestart, nil),
			request("pod-init", "web-9a8b7-init1", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerRestart, nil),
		}},
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
		{"unclean exit captures like any death", steps("terminating-exit/before.json", "terminating-exit/after.json"), nil, []CaptureRequest{
			request("pod-term", "web-7d9f8c6b5-term1", "api", store.ArtifactLogPrevious, 0, false, TriggerRestart, one),
			request("pod-term", "web-7d9f8c6b5-term1", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, one),
			request("pod-term", "web-7d9f8c6b5-term1", "", store.ArtifactPodJSON, store.NoRestartIndex, false, TriggerRestart, nil),
		}},
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

func TestFailedTransactionHasNoSideEffects(t *testing.T) {
	h := newHarness(t)
	if err := h.p.Pod(context.Background(), 99, loadPod(t, "crash-loop/after.json"), noOwners{}); err == nil {
		t.Fatal("pod for an unknown cluster id was accepted")
	}
	if pod := h.pod(t, "pod-crash"); pod != nil {
		t.Fatalf("pod row written despite failed transaction: %+v", pod)
	}
	if len(h.sink.reqs) != 0 {
		t.Fatalf("sink received %d requests from a failed transaction", len(h.sink.reqs))
	}
}

type cronOwners struct{}

func (cronOwners) ReplicaSetOwner(string, string) *metav1.OwnerReference { return nil }
func (cronOwners) JobOwner(string, string) *metav1.OwnerReference {
	return &metav1.OwnerReference{Kind: "CronJob", Name: "report", UID: "cj-report"}
}

// A CronJob prunes its finished Job, so the pod's cascade-deletion event finds
// no Job in the informer store. The pod and its open incident must stay on the
// CronJob rather than fall back to the run.
func TestPrunedJobKeepsCronJobWorkload(t *testing.T) {
	h := newHarness(t)
	h.feedJobs(t, "job-failed/before.json")
	h.feed(t, []step{{"oom-exit-zero/before.json", cronOwners{}}})

	pod := loadPod(t, "oom-exit-zero/after.json")
	deletion := metav1.NewTime(testNow)
	pod.DeletionTimestamp = &deletion
	if err := h.p.Pod(context.Background(), 1, pod, noOwners{}); err != nil {
		t.Fatal(err)
	}

	now := clock.Format(testNow)
	want := store.Pod{
		UID: "pod-oom0", ClusterID: 1, Namespace: "idios-smoke", Name: "report-28812345-x9k2p",
		NodeName: ptr("node-a"), Phase: "Failed", QOSClass: ptr("Burstable"),
		DeletionRequestedAt: ptr(now),
		ControllerKind:      "Job", ControllerName: "report-28812345", ControllerUID: "job-report-1",
		WorkloadKind: "CronJob", WorkloadName: "report",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: ptr("2026-08-27T11:45:05.000000Z"),
		FirstSeenAt: now, LastSeenAt: now,
	}
	diff(t, &want, h.pod(t, "pod-oom0"))

	incidents := h.incidents(t, "pod-oom0")
	if len(incidents) != 1 {
		t.Fatalf("incidents = %d, want 1", len(incidents))
	}
	diff(t, [2]string{"CronJob", "report"}, [2]string{incidents[0].WorkloadKind, incidents[0].WorkloadName})
}
