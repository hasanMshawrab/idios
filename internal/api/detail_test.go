package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
)

func ip(v int64) *int64 { return &v }
func bp(v bool) *bool   { return &v }

// ep points at a wire enum value, which every optional enum field takes.
func ep[T ~int32](v T) *T { return &v }

// The pod-crash rows the incident and pod pages of the seed share.
func crashPodWire() *idiosv1.Pod {
	return &idiosv1.Pod{
		Uid: querytest.CrashPodUID, ClusterId: querytest.ClusterProd, Namespace: seedNS, Name: "web-7d9f8c6b5-abcde",
		NodeName: sp("node-a"), Phase: "Running", QosClass: sp("BestEffort"),
		ControllerKind: "ReplicaSet", ControllerName: "web-7d9f8c6b5", ControllerUid: "rs-web-1",
		WorkloadKind: "Deployment", WorkloadName: "web",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:45:05.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:00.000000Z", LastSeenAt: "2026-08-27T12:00:01.000000Z",
	}
}

func crashContainersWire() []*idiosv1.Container {
	return []*idiosv1.Container{{
		Id: 1, PodUid: querytest.CrashPodUID, Name: "api", Kind: idiosv1.ContainerKind_CONTAINER_KIND_APP,
		Image: web, ImageTag: sp(webTag), ImageId: sp(webID), ContainerId: sp("containerd://aaa"),
		State: idiosv1.ContainerState_CONTAINER_STATE_WAITING, Reason: sp("CrashLoopBackOff"), RestartCount: 1,
		LastTerminatedReason: sp("Error"), LastTerminatedExitCode: i32(1), LastTerminatedSignal: i32(0),
		LastTerminatedAt: sp("2026-08-27T11:55:00.000000Z"), UpdatedAt: "2026-08-27T12:00:01.000000Z",
	}}
}

func crashArtifactsWire() []*idiosv1.Artifact {
	return []*idiosv1.Artifact{
		{Id: 2, PodUid: querytest.CrashPodUID, IncidentId: ip(querytest.CrashIncidentID),
			Kind: idiosv1.ArtifactKind_ARTIFACT_KIND_POD_JSON, RestartCount: -1,
			FilePath: sp("prod/pod-crash/pod.json"), SizeBytes: 2048, CapturedAt: querytest.ArtifactPodJSONAt},
		{Id: 4, PodUid: querytest.CrashPodUID, IncidentId: ip(querytest.CrashIncidentID), ContainerName: "api",
			Kind: idiosv1.ArtifactKind_ARTIFACT_KIND_LOG_CURRENT, RestartCount: -1,
			CaptureGap:  ep(idiosv1.CaptureGap_CAPTURE_GAP_NO_OUTPUT),
			CaptureNote: sp("container produced no output"), CapturedAt: querytest.ArtifactGapAt},
		{Id: 3, PodUid: querytest.CrashPodUID, IncidentId: ip(querytest.CrashIncidentID), ContainerName: "api",
			Kind:     idiosv1.ArtifactKind_ARTIFACT_KIND_LOG_PREVIOUS,
			FilePath: sp("prod/pod-crash/api-0.log"), SizeBytes: 4096, Truncated: true,
			CapturedAt: querytest.ArtifactLogAt},
	}
}

// crashEventsWire is pod-crash's whole event stream, oldest first: two rows
// that attached to incident 12 and one that attached to nothing.
func crashEventsWire() []*idiosv1.K8SEvent {
	return []*idiosv1.K8SEvent{
		{Id: 1, ClusterId: querytest.ClusterProd, EventUid: "ev-unhealthy-1", Namespace: seedNS, Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUid: querytest.CrashPodUID,
			FieldPath: "spec.containers{api}", Reason: "Unhealthy",
			Message:         "Readiness probe failed: HTTP probe failed with statuscode: 503",
			SourceComponent: "kubelet", Count: 3,
			FirstTs: "2026-08-27T11:58:00.000000Z", LastTs: "2026-08-27T11:59:00.000000Z",
			Category: ep(idiosv1.Category_CATEGORY_PROBE), IncidentId: ip(12)},
		{Id: 2, ClusterId: querytest.ClusterProd, EventUid: "ev-killing-1", Namespace: seedNS, Type: "Normal",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUid: querytest.CrashPodUID,
			FieldPath: "spec.containers{api}", Reason: "Killing", Message: "Stopping container api",
			SourceComponent: "kubelet", Count: 1,
			FirstTs: "2026-08-27T11:59:50.500000Z", LastTs: "2026-08-27T11:59:50.500000Z", IncidentId: ip(12)},
		{Id: 3, ClusterId: querytest.ClusterProd, EventUid: "ev-evicted-1", Namespace: seedNS, Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUid: querytest.CrashPodUID,
			Reason: "Evicted", Message: "The node was low on resource: memory.", SourceComponent: "kubelet", Count: 1,
			FirstTs: "2026-08-27T11:59:55.000000Z", LastTs: "2026-08-27T11:59:55.000000Z",
			Category: ep(idiosv1.Category_CATEGORY_NODE_PRESSURE)},
	}
}

// The detail page carries the incident, the pod and container snapshots, the
// pod's captured files and its whole event stream, each row naming the
// incident it attached to, and the other incidents of that pod.
func TestGetIncidentReturnsTheMappedDetail(t *testing.T) {
	client := newTestServer(t)
	cases := []struct {
		name string
		id   int64
		want *idiosv1.IncidentDetail
	}{
		{
			name: "a container incident with its pod, artifacts, events and related incidents",
			id:   1,
			want: &idiosv1.IncidentDetail{
				Incident:         wireRows(t, 1)[0],
				Pod:              crashPodWire(),
				Containers:       crashContainersWire(),
				Artifacts:        crashArtifactsWire(),
				Events:           crashEventsWire(),
				RelatedIncidents: wireRows(t, 12),
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := client.GetIncident(context.Background(), &idiosv1.GetIncidentRequest{Id: c.id})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// A job incident carries the job card and, through its newest pod, the pod
// parts: the failure a Job reports happened inside a pod, so the page shows
// that pod's snapshot, files and events whole.
func TestJobIncidentDetailCarriesTheJobCardAndItsLastPod(t *testing.T) {
	stack := newTestStack(t)
	pods := querytest.SeedJobPods(t, stack.store)
	client := idiosv1.NewIdiosServiceClient(stack.url)
	want := &idiosv1.IncidentDetail{
		Incident: wireRows(t, 11)[0],
		Job:      failedJobWire(),
		Pod: &idiosv1.Pod{
			Uid: pods[1].UID, ClusterId: querytest.ClusterProd, Namespace: seedNS, Name: pods[1].Name,
			NodeName: sp("node-a"), Phase: "Failed", QosClass: sp("BestEffort"),
			ControllerKind: "Job", ControllerName: "report-28812345", ControllerUid: querytest.FailedJobUID,
			WorkloadKind: "CronJob", WorkloadName: "report",
			CreatedAt: "2026-08-27T11:51:00.000000Z", StartedAt: sp("2026-08-27T11:51:02.000000Z"),
			FirstSeenAt: "2026-08-27T12:00:21.000000Z", LastSeenAt: "2026-08-27T12:00:22.000000Z",
			DeletedAt:      sp("2026-08-27T11:57:30.000000Z"),
			DeletionSource: ep(idiosv1.DeletionSource_DELETION_SOURCE_WATCH),
			DeletionReason: ep(idiosv1.DeletionReason_DELETION_REASON_JOB_PRUNED),
		},
		Containers: []*idiosv1.Container{{
			Id: 14, PodUid: pods[1].UID, Name: "report", Kind: idiosv1.ContainerKind_CONTAINER_KIND_APP,
			Image: "registry.example.com/report:0.9.1", ImageTag: sp("0.9.1"),
			State: idiosv1.ContainerState_CONTAINER_STATE_TERMINATED, Reason: sp("Error"),
			ExitCode: i32(1), Signal: i32(0), UpdatedAt: "2026-08-27T11:57:00.000000Z",
		}},
		Artifacts: []*idiosv1.Artifact{{
			Id: 5, PodUid: pods[1].UID, ContainerName: "report",
			Kind:     idiosv1.ArtifactKind_ARTIFACT_KIND_LOG_PREVIOUS,
			FilePath: sp("prod/pod-report-2/report-0.log"), SizeBytes: 1024,
			CapturedAt: "2026-08-27T11:57:01.000000Z",
		}},
		Events: []*idiosv1.K8SEvent{{
			Id: 4, ClusterId: querytest.ClusterProd, EventUid: "ev-report-killing-1", Namespace: seedNS,
			Type: "Normal", InvolvedKind: "Pod", InvolvedName: pods[1].Name, InvolvedUid: pods[1].UID,
			FieldPath: "spec.containers{report}", Reason: "Killing", Message: "Stopping container report",
			SourceComponent: "kubelet", Count: 1,
			FirstTs: "2026-08-27T11:56:58.000000Z", LastTs: "2026-08-27T11:56:58.000000Z",
		}},
		LastPodName: sp(pods[1].Name),
	}
	got, err := client.GetIncident(context.Background(), &idiosv1.GetIncidentRequest{Id: 11})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}

// The timeline merges the incident's lifecycle with the captures, events,
// transitions and rollouts of its span, ordered by Kubernetes time where
// there is one and by observed time otherwise.
func TestIncidentTimelineReturnsTheMergedEntries(t *testing.T) {
	client := newTestServer(t)
	want := &idiosv1.TimelineResponse{Entries: []*idiosv1.TimelineEntry{
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_LIFECYCLE, ObservedAt: "2026-08-27T11:55:00.000000Z",
			Lifecycle: ep(idiosv1.LifecycleStep_LIFECYCLE_STEP_OPENED)},
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_CAPTURE, ObservedAt: querytest.ArtifactPodJSONAt,
			ArtifactId: ip(2), ArtifactKind: ep(idiosv1.ArtifactKind_ARTIFACT_KIND_POD_JSON)},
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_CAPTURE, ObservedAt: querytest.ArtifactLogAt,
			ContainerName: sp("api"), RestartCount: i32(0), ArtifactId: ip(3),
			ArtifactKind: ep(idiosv1.ArtifactKind_ARTIFACT_KIND_LOG_PREVIOUS)},
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_CAPTURE, ObservedAt: querytest.ArtifactGapAt,
			ContainerName: sp("api"), ArtifactId: ip(4),
			ArtifactKind: ep(idiosv1.ArtifactKind_ARTIFACT_KIND_LOG_CURRENT),
			CaptureGap:   ep(idiosv1.CaptureGap_CAPTURE_GAP_NO_OUTPUT)},
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_EVENT, K8SAt: sp("2026-08-27T11:59:00.000000Z"),
			ObservedAt: "2026-08-27T11:59:00.000000Z",
			Message:    sp("Readiness probe failed: HTTP probe failed with statuscode: 503"),
			EventType:  sp("Warning"), EventReason: sp("Unhealthy"), Count: i32(3)},
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_EVENT, K8SAt: sp("2026-08-27T11:59:50.500000Z"),
			ObservedAt: "2026-08-27T11:59:50.500000Z", Message: sp("Stopping container api"),
			EventType: sp("Normal"), EventReason: sp("Killing"), Count: i32(1)},
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_EVENT, K8SAt: sp("2026-08-27T11:59:55.000000Z"),
			ObservedAt: "2026-08-27T11:59:55.000000Z", Message: sp("The node was low on resource: memory."),
			EventType: sp("Warning"), EventReason: sp("Evicted"), Count: i32(1)},
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_CONTAINER_TRANSITION, ObservedAt: "2026-08-27T12:00:01.000000Z",
			ContainerName: sp("api"), State: ep(idiosv1.ContainerState_CONTAINER_STATE_WAITING),
			Reason: sp("CrashLoopBackOff"), RestartCount: i32(1), GapReconstructed: bp(false)},
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_ROLLOUT, ObservedAt: "2026-08-27T12:00:25.000000Z",
			ReplicasetName: sp("web-7d9f8c6b5"), ImageTag: sp(webTag), Revision: ip(7)},
		{Kind: idiosv1.TimelineKind_TIMELINE_KIND_ROLLOUT, ObservedAt: "2026-08-27T12:00:26.000000Z",
			ReplicasetName: sp("web-8e0a9d7c6"), ImageTag: sp("1.4.3"), Revision: ip(8)},
	}}
	got, err := client.IncidentTimeline(context.Background(), &idiosv1.IncidentTimelineRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}

// An id the database does not hold is a 404 naming it, and an id that is not
// a number never reaches the handler.
func TestGetIncidentUnknownIdIs404(t *testing.T) {
	stack := newTestStack(t)
	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"unknown incident", "/v1/incidents/999999", http.StatusNotFound, `{"message":"incident 999999 not found"}`},
		{"unknown timeline", "/v1/incidents/999999/timeline", http.StatusNotFound, `{"message":"incident 999999 not found"}`},
		{"an id that is not a number", "/v1/incidents/abc", http.StatusBadRequest, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, body := get(t, stack.url, c.path)
			if code != c.wantStatus {
				t.Fatalf("status %d, want %d: %s", code, c.wantStatus, body)
			}
			if c.wantBody == "" {
				return
			}
			if got := string(compact(t, body)); got != c.wantBody {
				t.Errorf("body %s, want %s", got, c.wantBody)
			}
		})
	}
}
