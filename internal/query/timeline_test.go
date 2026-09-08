package query

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

func bp(v bool) *bool { return &v }

func timeline(t *testing.T, db store.Querier, id int64) []TimelineEntry {
	t.Helper()
	entries, err := IncidentTimeline(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// The timeline merges the incident's own lifecycle with the transitions,
// conditions, events, captures and rollouts of its span, ordered by
// Kubernetes time where there is one and by observed time otherwise.
func TestTimelineMergesSixKindsInOrder(t *testing.T) {
	st, _ := querytest.Seed(t)
	cases := []struct {
		name string
		id   int64
		want []TimelineEntry
	}{
		{"transitions, events, captures and rollouts", 1, []TimelineEntry{
			{Kind: TimelineLifecycle, ObservedAt: "2026-08-27T11:55:00.000000Z", Lifecycle: sp(LifecycleOpened)},
			{Kind: TimelineCapture, ObservedAt: querytest.ArtifactPodJSONAt,
				ArtifactID: ip(2), ArtifactKind: sp(store.ArtifactPodJSON)},
			{Kind: TimelineCapture, ObservedAt: querytest.ArtifactLogAt, ContainerName: sp("api"),
				RestartCount: ip(0), ArtifactID: ip(3), ArtifactKind: sp(store.ArtifactLogPrevious)},
			{Kind: TimelineCapture, ObservedAt: querytest.ArtifactGapAt, ContainerName: sp("api"),
				ArtifactID: ip(4), ArtifactKind: sp(store.ArtifactLogCurrent),
				CaptureGap: sp(store.GapNoOutput)},
			{Kind: TimelineEvent, K8sAt: sp("2026-08-27T11:59:00.000000Z"), ObservedAt: "2026-08-27T11:59:00.000000Z",
				Message:   sp("Readiness probe failed: HTTP probe failed with statuscode: 503"),
				EventType: sp("Warning"), EventReason: sp("Unhealthy"), Count: ip(3)},
			{Kind: TimelineEvent, K8sAt: sp("2026-08-27T11:59:50.500000Z"), ObservedAt: "2026-08-27T11:59:50.500000Z",
				Message: sp("Stopping container api"), EventType: sp("Normal"), EventReason: sp("Killing"), Count: ip(1)},
			{Kind: TimelineEvent, K8sAt: sp("2026-08-27T11:59:55.000000Z"), ObservedAt: "2026-08-27T11:59:55.000000Z",
				Message: sp("The node was low on resource: memory."), EventType: sp("Warning"), EventReason: sp("Evicted"), Count: ip(1)},
			{Kind: TimelineContainerTransition, ObservedAt: "2026-08-27T12:00:01.000000Z", ContainerName: sp("api"),
				State: sp(store.StateWaiting), Reason: sp("CrashLoopBackOff"), RestartCount: ip(1), GapReconstructed: bp(false)},
			{Kind: TimelineRollout, ObservedAt: "2026-08-27T12:00:25.000000Z",
				ReplicaSetName: sp("web-7d9f8c6b5"), ImageTag: sp(webTag), Revision: ip(7)},
			{Kind: TimelineRollout, ObservedAt: "2026-08-27T12:00:26.000000Z",
				ReplicaSetName: sp("web-8e0a9d7c6"), ImageTag: sp("1.4.3"), Revision: ip(8)},
		}},
		{"conditions between the open and the close", 4, []TimelineEntry{
			{Kind: TimelineLifecycle, ObservedAt: "2026-08-27T11:45:01.000000Z", Lifecycle: sp(LifecycleOpened)},
			{Kind: TimelineCondition, K8sAt: sp("2026-08-27T11:45:01.000000Z"), ObservedAt: "2026-08-27T12:00:07.000000Z",
				Reason: sp("Unschedulable"), ConditionType: sp("PodScheduled"), ConditionStatus: sp("False"),
				Message: sp("0/3 nodes are available: 3 Insufficient cpu. preemption: 0/3 nodes are available: 3 No preemption victims found for incoming pod.")},
			{Kind: TimelineCondition, K8sAt: sp("2026-08-27T11:58:00.000000Z"), ObservedAt: "2026-08-27T12:00:09.000000Z",
				Reason: sp(""), ConditionType: sp("PodScheduled"), ConditionStatus: sp("True")},
			{Kind: TimelineContainerTransition, K8sAt: sp("2026-08-27T11:58:05.000000Z"), ObservedAt: "2026-08-27T12:00:09.000000Z",
				ContainerName: sp("api"), State: sp(store.StateRunning), RestartCount: ip(0), GapReconstructed: bp(false)},
			{Kind: TimelineLifecycle, ObservedAt: "2026-08-27T12:00:09.000000Z", Lifecycle: sp(LifecycleClosed), CloseReason: sp(store.CloseRecovered)},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if diff := cmp.Diff(c.want, timeline(t, st.Reader.DB(), c.id)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// A restart counter that jumped leaves history rows the watcher never saw;
// they are marked so the screen can draw them hollow.
func TestTimelineMarksReconstructedTransitions(t *testing.T) {
	st, _ := querytest.Seed(t)
	cases := []struct {
		name string
		id   int64
		want []TimelineEntry
	}{
		{"reconstructed from a restart jump", 6, []TimelineEntry{
			{Kind: TimelineContainerTransition, K8sAt: sp("2026-08-27T11:58:00.000000Z"), ObservedAt: "2026-08-27T12:00:13.000000Z",
				ContainerName: sp("api"), State: sp(store.StateTerminated), Reason: sp("Error"), ExitCode: ip(1), Signal: ip(0),
				RestartCount: ip(3), GapReconstructed: bp(true)},
			{Kind: TimelineContainerTransition, K8sAt: sp("2026-08-27T11:59:00.000000Z"), ObservedAt: "2026-08-27T12:00:13.000000Z",
				ContainerName: sp("api"), State: sp(store.StateRunning), RestartCount: ip(4), GapReconstructed: bp(false)},
		}},
		{"observed transition", 1, []TimelineEntry{
			{Kind: TimelineContainerTransition, ObservedAt: "2026-08-27T12:00:01.000000Z", ContainerName: sp("api"),
				State: sp(store.StateWaiting), Reason: sp("CrashLoopBackOff"), RestartCount: ip(1), GapReconstructed: bp(false)},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []TimelineEntry
			for _, e := range timeline(t, st.Reader.DB(), c.id) {
				if e.Kind == TimelineContainerTransition {
					got = append(got, e)
				}
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// A sweep that removed rows from the span becomes one cut entry, so a gap in
// the history is not read as a quiet stretch. The seed's own sweep row cuts
// three days before every span and must not show.
func TestTimelineAddsCutWhenSweepRemovedRowsInSpan(t *testing.T) {
	const (
		inSpan   = "2026-08-27T11:58:00.000000Z"
		later    = "2026-08-27T11:59:30.000000Z"
		outSpan  = "2026-08-27T11:50:00.000000Z"
		sweptAt  = "2026-08-27T12:00:40.000000Z"
		sweptAt2 = "2026-08-27T12:00:50.000000Z"
	)
	cases := []struct {
		name string
		runs []store.SweepRun
		want []TimelineEntry
	}{
		{"rows removed inside the span", []store.SweepRun{
			{RanAt: sweptAt, Cutoff: inSpan, TableName: "k8s_events", RowsRemoved: 5},
		}, []TimelineEntry{{Kind: TimelineCut, K8sAt: sp(inSpan), ObservedAt: sweptAt}}},
		{"nothing removed", []store.SweepRun{
			{RanAt: sweptAt, Cutoff: inSpan, TableName: "k8s_events"},
		}, nil},
		{"cutoff before the span", []store.SweepRun{
			{RanAt: sweptAt, Cutoff: outSpan, TableName: "k8s_events", RowsRemoved: 5},
		}, nil},
		{"another table", []store.SweepRun{
			{RanAt: sweptAt, Cutoff: inSpan, TableName: "pods", RowsRemoved: 5},
		}, nil},
		{"the newest run stands for them all", []store.SweepRun{
			{RanAt: sweptAt, Cutoff: inSpan, TableName: "k8s_events", RowsRemoved: 5},
			{RanAt: sweptAt2, Cutoff: later, TableName: "container_state_history", RowsRemoved: 2},
		}, []TimelineEntry{{Kind: TimelineCut, K8sAt: sp(later), ObservedAt: sweptAt2}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := querytest.Seed(t)
			ctx := context.Background()
			for _, r := range c.runs {
				if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error { return store.InsertSweepRun(ctx, tx, r) }); err != nil {
					t.Fatal(err)
				}
			}
			var got []TimelineEntry
			for _, e := range timeline(t, st.Reader.DB(), 1) {
				if e.Kind == TimelineCut {
					got = append(got, e)
				}
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// An incident that exists always has its opened entry, so a nil timeline is
// the absence the API turns into 404, not an empty story.
func TestIncidentTimelineUnknownIdIsNil(t *testing.T) {
	st, _ := querytest.Seed(t)
	entries, err := IncidentTimeline(context.Background(), st.Reader.DB(), 999999)
	if err != nil {
		t.Fatal(err)
	}
	if entries != nil {
		t.Errorf("entries = %v, want nil", entries)
	}
}

// captureIDs are the artifact ids of the timeline's capture entries, sorted so
// the comparison is about the set and not about the merge order.
func captureIDs(entries []TimelineEntry) []int64 {
	var out []int64
	for _, e := range entries {
		if e.Kind == TimelineCapture {
			out = append(out, *e.ArtifactID)
		}
	}
	slices.Sort(out)
	return out
}

func artifactIDs(rows []store.Artifact) []int64 {
	var out []int64
	for _, a := range rows {
		out = append(out, a.ID)
	}
	slices.Sort(out)
	return out
}

// The detail lists every capture of its pod, whichever of that pod's incidents
// each one attached to, and the timeline shows the same set: an artifact the
// detail names and its own timeline leaves out reads as evidence that was
// never captured. A job incident resolves the pod the detail resolves, the
// last pod of its Job.
func TestTimelineCapturesAreTheArtifactsTheDetailLists(t *testing.T) {
	st, _ := querytest.Seed(t)
	querytest.SeedJobPods(t, st)
	cases := []struct {
		name string
		id   int64
		want []int64
	}{
		{"the incident the captures attached to", 1, []int64{2, 3, 4}},
		{"another incident of the same pod", 12, []int64{2, 3, 4}},
		{"a job incident, through the last pod of its Job", 11, []int64{5}},
		{"an incident whose pod was never captured", 4, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := st.Reader.DB()
			if diff := cmp.Diff(c.want, captureIDs(timeline(t, db, c.id))); diff != "" {
				t.Errorf("timeline captures (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(c.want, artifactIDs(detail(t, db, c.id).Artifacts)); diff != "" {
				t.Errorf("detail artifacts (-want +got):\n%s", diff)
			}
		})
	}
}
