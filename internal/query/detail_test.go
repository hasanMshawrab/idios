package query

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

func detail(t *testing.T, db store.Querier, id int64) *IncidentDetail {
	t.Helper()
	d, err := GetIncident(context.Background(), db, id, Page{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The pod-crash rows two incidents of the seed share.
func crashPod() *store.Pod {
	return &store.Pod{
		UID: querytest.CrashPodUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-7d9f8c6b5-abcde",
		NodeName: sp("node-a"), Phase: "Running", QOSClass: sp("BestEffort"),
		ControllerKind: "ReplicaSet", ControllerName: "web-7d9f8c6b5", ControllerUID: "rs-web-1",
		WorkloadKind: "Deployment", WorkloadName: "web",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:45:05.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:00.000000Z", LastSeenAt: "2026-08-27T12:00:01.000000Z",
	}
}

func crashContainers() []store.Container {
	return []store.Container{{
		ID: 1, PodUID: querytest.CrashPodUID, Name: "api", Kind: store.ContainerKindApp,
		Image: web, ImageTag: sp(webTag), ImageID: sp(webID), ContainerID: sp("containerd://aaa"),
		State: store.StateWaiting, Reason: sp("CrashLoopBackOff"), RestartCount: 1,
		LastTerminatedReason: sp("Error"), LastTerminatedExitCode: ip(1), LastTerminatedSignal: ip(0),
		LastTerminatedAt: sp("2026-08-27T11:55:00.000000Z"), UpdatedAt: "2026-08-27T12:00:01.000000Z",
	}}
}

func crashArtifacts() []store.Artifact {
	return []store.Artifact{
		{ID: 2, PodUID: querytest.CrashPodUID, IncidentID: ip(1), Kind: store.ArtifactPodJSON,
			RestartCount: store.NoRestartIndex, FilePath: sp("prod/pod-crash/pod.json"), SizeBytes: 2048,
			CapturedAt: querytest.ArtifactPodJSONAt},
		{ID: 4, PodUID: querytest.CrashPodUID, IncidentID: ip(1), ContainerName: "api", Kind: store.ArtifactLogCurrent,
			RestartCount: store.NoRestartIndex, CaptureGap: sp(store.GapNoOutput),
			CaptureNote: sp("container produced no output"), CapturedAt: querytest.ArtifactGapAt},
		{ID: 3, PodUID: querytest.CrashPodUID, IncidentID: ip(1), ContainerName: "api", Kind: store.ArtifactLogPrevious,
			FilePath: sp("prod/pod-crash/api-0.log"), SizeBytes: 4096, Truncated: true,
			CapturedAt: querytest.ArtifactLogAt},
	}
}

// crashEvents is pod-crash's whole event stream, which every incident of that
// pod carries: the probe and the Killing rows attached to incident 12, and the
// Evicted row that attached to nothing.
func crashEvents() []store.K8sEvent {
	return []store.K8sEvent{
		{ID: 1, ClusterID: querytest.ClusterProd, EventUID: "ev-unhealthy-1", Namespace: seedNS, Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUID: querytest.CrashPodUID,
			FieldPath: "spec.containers{api}", Reason: "Unhealthy",
			Message:         "Readiness probe failed: HTTP probe failed with statuscode: 503",
			SourceComponent: "kubelet", Count: 3, FirstTS: "2026-08-27T11:58:00.000000Z",
			LastTS: "2026-08-27T11:59:00.000000Z", Category: sp(store.CategoryProbe), IncidentID: ip(12)},
		{ID: 2, ClusterID: querytest.ClusterProd, EventUID: "ev-killing-1", Namespace: seedNS, Type: "Normal",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUID: querytest.CrashPodUID,
			FieldPath: "spec.containers{api}", Reason: "Killing", Message: "Stopping container api",
			SourceComponent: "kubelet", Count: 1, FirstTS: "2026-08-27T11:59:50.500000Z",
			LastTS: "2026-08-27T11:59:50.500000Z", IncidentID: ip(12)},
		{ID: 3, ClusterID: querytest.ClusterProd, EventUID: "ev-evicted-1", Namespace: seedNS, Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUID: querytest.CrashPodUID,
			Reason: "Evicted", Message: "The node was low on resource: memory.",
			SourceComponent: "kubelet", Count: 1, FirstTS: "2026-08-27T11:59:55.000000Z",
			LastTS: "2026-08-27T11:59:55.000000Z", Category: sp(store.CategoryNodePressure)},
	}
}

// The detail page shows the incident, its pod and containers, the captured
// files, the pod's events and the latest condition per type; an unknown id
// is not an error but an absence.
func TestGetIncidentAssemblesDetail(t *testing.T) {
	st, _ := querytest.Seed(t)
	cases := []struct {
		name string
		id   int64
		want *IncidentDetail
	}{
		{"unknown id", 999999, nil},
		// The captures attached to incident 1, and this row lists them all the
		// same: a capture runs for the pod, not for whichever of its incidents
		// happened to be open.
		{"the pod's events and files, whichever incident they attached to", 12, &IncidentDetail{
			Incident:         seeded()[12],
			Pod:              crashPod(),
			Containers:       crashContainers(),
			Artifacts:        crashArtifacts(),
			Events:           crashEvents(),
			RelatedIncidents: []IncidentRow{seeded()[1]},
		}},
		// The two events of incident 12 and the unattached Evicted row are the
		// pod's story even though none of them attached here.
		{"container incident carries its own container and the pod's events", 1, &IncidentDetail{
			Incident:         seeded()[1],
			Pod:              crashPod(),
			Containers:       crashContainers(),
			Artifacts:        crashArtifacts(),
			Events:           crashEvents(),
			RelatedIncidents: []IncidentRow{seeded()[12]},
		}},
		{"pod level incident carries every container", 4, &IncidentDetail{
			Incident: seeded()[4],
			Pod: &store.Pod{
				UID: querytest.UnschedPod, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-2b3c4-pend1",
				NodeName: sp("node-b"), Phase: "Running", QOSClass: sp("Burstable"),
				ControllerKind: "ReplicaSet", ControllerName: "web-2b3c4", ControllerUID: "rs-web-7",
				WorkloadKind: "ReplicaSet", WorkloadName: "web-2b3c4",
				CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:58:00.000000Z"),
				FirstSeenAt: "2026-08-27T12:00:07.000000Z", LastSeenAt: "2026-08-27T12:00:09.000000Z",
			},
			Containers: []store.Container{{
				ID: 4, PodUID: querytest.UnschedPod, Name: "api", Kind: store.ContainerKindApp,
				Image: web, ImageTag: sp(webTag), ImageID: sp(webID), ContainerID: sp("containerd://uuu"),
				CPURequest: sp("8"), CPURequestMillis: ip(8000), State: store.StateRunning, Ready: true,
				RunningSince: sp("2026-08-27T11:58:05.000000Z"), UpdatedAt: "2026-08-27T12:00:09.000000Z",
			}},
			Conditions: []store.PodCondition{{
				ID: 2, PodUID: querytest.UnschedPod, Type: "PodScheduled", Status: "True",
				K8sTransitionAt: sp("2026-08-27T11:58:00.000000Z"), ObservedAt: "2026-08-27T12:00:09.000000Z",
			}},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if diff := cmp.Diff(c.want, detail(t, st.Reader.DB(), c.id)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// A job incident carries the Job and, through it, its newest pod whole: the
// pod row, its containers, its files and its events regardless of which
// incident they attach to. The newest pod is the pruned retry, which stays
// in pods as a deleted row, not the older one that is still there. The
// failure a Job reports happened inside a pod, so the page must not stop at
// the jobs row.
func TestJobIncidentDetailCarriesTheJobAndItsLastPodWhole(t *testing.T) {
	st, _ := querytest.Seed(t)
	pods := querytest.SeedJobPods(t, st)
	job := failedJob()
	want := &IncidentDetail{
		Incident: seeded()[11],
		Job:      &job,
		Pod:      &pods[1],
		Containers: []store.Container{{
			ID: 14, PodUID: pods[1].UID, Name: "report", Kind: store.ContainerKindApp,
			Image: "registry.example.com/report:0.9.1", ImageTag: sp("0.9.1"),
			State: store.StateTerminated, Reason: sp("Error"), ExitCode: ip(1), Signal: ip(0),
			UpdatedAt: "2026-08-27T11:57:00.000000Z",
		}},
		Artifacts: []store.Artifact{{
			ID: 5, PodUID: pods[1].UID, ContainerName: "report", Kind: store.ArtifactLogPrevious,
			FilePath: sp("prod/pod-report-2/report-0.log"), SizeBytes: 1024,
			CapturedAt: "2026-08-27T11:57:01.000000Z",
		}},
		Events: []store.K8sEvent{{
			ID: 4, ClusterID: querytest.ClusterProd, EventUID: "ev-report-killing-1", Namespace: seedNS,
			Type: "Normal", InvolvedKind: "Pod", InvolvedName: pods[1].Name, InvolvedUID: pods[1].UID,
			FieldPath: "spec.containers{report}", Reason: "Killing", Message: "Stopping container report",
			SourceComponent: "kubelet", Count: 1,
			FirstTS: "2026-08-27T11:56:58.000000Z", LastTS: "2026-08-27T11:56:58.000000Z",
		}},
		LastPodName: sp(pods[1].Name),
	}
	if diff := cmp.Diff(want, detail(t, st.Reader.DB(), 11)); diff != "" {
		t.Error(diff)
	}
}

// An event attaches to the one incident that was open when it arrived, so a
// pod incident that opened late would show nothing. Its detail carries the
// pod's whole stream instead, oldest first, with incident_id on each row
// saying whether it is this incident's own evidence, another incident's or
// nobody's.
func TestPodIncidentDetailCarriesThePodsWholeEventStream(t *testing.T) {
	st, _ := querytest.Seed(t)
	ctx := context.Background()
	own := store.K8sEvent{
		ClusterID: querytest.ClusterProd, EventUID: "ev-backoff-1", Namespace: seedNS, Type: "Warning",
		InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUID: querytest.CrashPodUID,
		FieldPath: "spec.containers{api}", Reason: "BackOff",
		Message: "Back-off restarting failed container api", SourceComponent: "kubelet", Count: 2,
		FirstTS: "2026-08-27T11:59:57.000000Z", LastTS: "2026-08-27T11:59:58.000000Z",
		Category: sp(store.CategoryCrash), IncidentID: ip(1), RawJSON: "{}",
	}
	if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error { return store.UpsertEvent(ctx, tx, own) }); err != nil {
		t.Fatal(err)
	}
	want := append(crashEvents(), store.K8sEvent{
		ID: 4, ClusterID: own.ClusterID, EventUID: own.EventUID, Namespace: own.Namespace, Type: own.Type,
		InvolvedKind: own.InvolvedKind, InvolvedName: own.InvolvedName, InvolvedUID: own.InvolvedUID,
		FieldPath: own.FieldPath, Reason: own.Reason, Message: own.Message,
		SourceComponent: own.SourceComponent, Count: own.Count, FirstTS: own.FirstTS, LastTS: own.LastTS,
		Category: own.Category, IncidentID: own.IncidentID,
	})
	if diff := cmp.Diff(want, detail(t, st.Reader.DB(), 1).Events); diff != "" {
		t.Error(diff)
	}
}

// A pod incident that names a Job carries the run as well: the condition is
// the single fact that separates a retry that succeeded from a run that
// failed, and it was withheld from pod incidents. Its related incidents are
// the Job's own row, because a retry's sibling hangs off the job uid and not
// off the pod.
func TestPodIncidentOfAJobCarriesTheRunAndTheJobsIncidents(t *testing.T) {
	st, _ := querytest.Seed(t)
	pods := querytest.SeedJobPods(t, st)
	ctx := context.Background()
	var id int64
	err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = store.OpenIncident(ctx, tx, store.Incident{
			ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod,
			PodUID: &pods[1].UID, JobUID: sp(querytest.FailedJobUID), ContainerName: "report",
			WorkloadKind: "CronJob", WorkloadName: "report", Category: store.CategoryCrash,
			FirstReason: "Error", LastReason: "Error", NodeName: sp("node-a"), Occurrences: 1,
			OpenedAt: "2026-08-27T11:56:00.000000Z", LastSeenAt: "2026-08-27T11:57:00.000000Z",
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	d := detail(t, st.Reader.DB(), id)
	job := failedJob()
	if diff := cmp.Diff(&job, d.Job); diff != "" {
		t.Errorf("job (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]IncidentRow{seeded()[11]}, d.RelatedIncidents); diff != "" {
		t.Errorf("related incidents (-want +got):\n%s", diff)
	}
}
