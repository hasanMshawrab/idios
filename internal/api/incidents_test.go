package api

import (
	"context"
	"errors"
	"testing"

	sebufhttp "github.com/SebastienMelki/sebuf/http"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
)

func sp(v string) *string { return &v }
func i32(v int32) *int32  { return &v }
func i64(v int64) *int64  { return &v }

func closeReason(v idiosv1.CloseReason) *idiosv1.CloseReason          { return &v }
func deletionReason(v idiosv1.DeletionReason) *idiosv1.DeletionReason { return &v }

const (
	seedNS  = "idios-smoke"
	web     = "registry.example.com/web:1.4.2"
	webID   = "registry.example.com/web@sha256:1111"
	webTag  = "1.4.2"
	actedAt = "2026-08-27T12:00:31.000000Z"
	deleted = "2026-08-27T12:00:30.000000Z"
)

// wireSeed is every incident querytest.Seed holds, as the wire spells it.
func wireSeed() map[int64]*idiosv1.IncidentRow {
	rows := map[int64]*idiosv1.IncidentRow{
		1: {
			Id: 1, ClusterId: querytest.ClusterProd, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.CrashPodUID),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web",
			Category: idiosv1.Category_CATEGORY_CRASH, FirstReason: "CrashLoopBackOff", LastReason: "CrashLoopBackOff",
			Image: sp(web), ImageTag: sp(webTag), ImageId: sp(webID), Occurrences: 1,
			OpenedAt: "2026-08-27T11:55:00.000000Z", LastSeenAt: actedAt,
			State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, PodName: sp("web-7d9f8c6b5-abcde"),
			ContainerCount: 1, ExitCode: i32(1), Signal: i32(0),
		},
		2: {
			Id: 2, ClusterId: querytest.ClusterProd, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.OOMPodUID),
			ContainerName: "api", WorkloadKind: "ReplicaSet", WorkloadName: "mem-hog-5f6d7",
			Category: idiosv1.Category_CATEGORY_OOM, FirstReason: "OOMKilled", LastReason: "OOMKilled",
			Image: sp("registry.example.com/mem-hog:2.0.0"), ImageTag: sp("2.0.0"),
			ImageId: sp("registry.example.com/mem-hog@sha256:2222"), Occurrences: 1,
			OpenedAt: "2026-08-27T11:52:30.000000Z", LastSeenAt: "2026-08-27T12:00:03.000000Z",
			AcknowledgedAt: sp(actedAt), State: idiosv1.IncidentState_INCIDENT_STATE_ACKNOWLEDGED,
			PodName: sp("mem-hog-5f6d7-qwert"), ContainerCount: 1, ExitCode: i32(137), Signal: i32(9),
		},
		3: {
			Id: 3, ClusterId: querytest.ClusterProd, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.PullPodUID),
			ContainerName: "api", WorkloadKind: "ReplicaSet", WorkloadName: "web-66c9d",
			Category: idiosv1.Category_CATEGORY_IMAGE_PULL, FirstReason: "ErrImagePull", LastReason: "ImagePullBackOff",
			Image: sp("registry.example.com/web:does-not-exist"), ImageTag: sp("does-not-exist"), Occurrences: 2,
			OpenedAt: "2026-08-27T11:45:00.000000Z", LastSeenAt: "2026-08-27T12:00:05.000000Z",
			DismissedAt: sp(actedAt), State: idiosv1.IncidentState_INCIDENT_STATE_DISMISSED,
			PodName: sp("web-66c9d-pull1"), ContainerCount: 1,
		},
		4: {
			Id: 4, ClusterId: querytest.ClusterProd, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.UnschedPod),
			WorkloadKind: "ReplicaSet", WorkloadName: "web-2b3c4",
			Category: idiosv1.Category_CATEGORY_SCHEDULING, FirstReason: "Unschedulable", LastReason: "Unschedulable",
			LastMessage: sp("0/4 nodes are available: 4 Insufficient cpu. preemption: 0/4 nodes are available: 4 No preemption victims found for incoming pod."),
			Occurrences: 1, OpenedAt: "2026-08-27T11:45:01.000000Z", LastSeenAt: "2026-08-27T12:00:07.000000Z",
			ClosedAt: sp("2026-08-27T12:00:09.000000Z"), CloseReason: closeReason(idiosv1.CloseReason_CLOSE_REASON_RECOVERED),
			State: idiosv1.IncidentState_INCIDENT_STATE_RECOVERED, PodName: sp("web-2b3c4-pend1"), ContainerCount: 1,
		},
		5: {
			Id: 5, ClusterId: querytest.ClusterProd, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.MultiPodUID),
			ContainerName: "worker", WorkloadKind: "ReplicaSet", WorkloadName: "web-4e5f6",
			Category: idiosv1.Category_CATEGORY_CRASH, FirstReason: "CrashLoopBackOff", LastReason: "CrashLoopBackOff",
			Image: sp("registry.example.com/worker:1.4.2"), ImageTag: sp(webTag),
			ImageId: sp("registry.example.com/worker@sha256:7777"), Occurrences: 1,
			OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: "2026-08-27T12:00:11.000000Z",
			State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, PodName: sp("web-4e5f6-multi"),
			ContainerCount: 2, ExitCode: i32(2), Signal: i32(0),
		},
		6: {
			Id: 6, ClusterId: querytest.ClusterProd, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.JumpPodUID),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web",
			Category: idiosv1.Category_CATEGORY_CRASH, FirstReason: "CrashLoopBackOff", LastReason: "Error",
			LastMessage: sp("panic: read of a closed queue"),
			Image:       sp(web), ImageTag: sp(webTag), ImageId: sp(webID), Occurrences: 2,
			OpenedAt: "2026-08-27T11:40:00.000000Z", LastSeenAt: "2026-08-27T12:00:13.000000Z",
			ClosedAt: sp(deleted), CloseReason: closeReason(idiosv1.CloseReason_CLOSE_REASON_POD_DELETED),
			State: idiosv1.IncidentState_INCIDENT_STATE_POD_DELETED, PodName: sp("web-7d9f8c6b5-jump1"),
			PodDeletedAt:      sp(deleted),
			PodDeletionReason: deletionReason(idiosv1.DeletionReason_DELETION_REASON_ROLLOUT),
			ContainerCount:    1, ExitCode: i32(1), Signal: i32(0),
		},
		7: {
			Id: 7, ClusterId: querytest.ClusterProd, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.InitPodUID),
			ContainerName: "init-db", WorkloadKind: "ReplicaSet", WorkloadName: "web-9a8b7",
			Category: idiosv1.Category_CATEGORY_CRASH, FirstReason: "Error", LastReason: "Error",
			Image: sp("registry.example.com/migrate:3.1.0"), ImageTag: sp("3.1.0"),
			ImageId: sp("registry.example.com/migrate@sha256:5555"), Occurrences: 1,
			OpenedAt: "2026-08-27T11:45:00.000000Z", LastSeenAt: "2026-08-27T12:00:14.000000Z",
			ClosedAt: sp(actedAt), CloseReason: closeReason(idiosv1.CloseReason_CLOSE_REASON_RECOVERED),
			DismissedAt: sp(actedAt), State: idiosv1.IncidentState_INCIDENT_STATE_DISMISSED,
			PodName: sp("web-9a8b7-init1"), ContainerCount: 2, ExitCode: i32(1), Signal: i32(0),
		},
		8: {
			Id: 8, ClusterId: querytest.ClusterStaging, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.ConfigPodUID),
			ContainerName: "api", WorkloadKind: "ReplicaSet", WorkloadName: "web-5b8c7",
			Category:    idiosv1.Category_CATEGORY_CONFIG,
			FirstReason: "CreateContainerConfigError", LastReason: "CreateContainerConfigError",
			Image: sp(web), ImageTag: sp(webTag), Occurrences: 1,
			OpenedAt: "2026-08-27T12:00:18.000000Z", LastSeenAt: "2026-08-27T12:00:18.000000Z",
			State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, PodName: sp("web-5b8c7-cfg01"), ContainerCount: 1,
		},
		9: {
			Id: 9, ClusterId: querytest.ClusterStaging, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.EvictPodUID),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web",
			Category: idiosv1.Category_CATEGORY_CRASH, FirstReason: "Error", LastReason: "Error",
			Image: sp(web), ImageTag: sp(webTag), ImageId: sp(webID), Occurrences: 1,
			OpenedAt: "2026-08-27T11:57:30.000000Z", LastSeenAt: "2026-08-27T12:00:20.000000Z",
			State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, PodName: sp("web-7d9f8c6b5-evic1"),
			ContainerCount: 1, ExitCode: i32(137), Signal: i32(9),
		},
		10: {
			Id: 10, ClusterId: querytest.ClusterStaging, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.EvictPodUID),
			WorkloadKind: "Deployment", WorkloadName: "web",
			Category: idiosv1.Category_CATEGORY_NODE_PRESSURE, FirstReason: "Evicted", LastReason: "Evicted",
			LastMessage: sp("The node was low on resource: memory. Threshold quantity: 100Mi, available: 52Mi. Container api was using 900Mi, request is 0, has larger consumption of memory."),
			Occurrences: 1, OpenedAt: "2026-08-27T12:00:20.000000Z", LastSeenAt: "2026-08-27T12:00:20.000000Z",
			ClosedAt: sp(actedAt), CloseReason: closeReason(idiosv1.CloseReason_CLOSE_REASON_MANUAL),
			State: idiosv1.IncidentState_INCIDENT_STATE_MANUAL, PodName: sp("web-7d9f8c6b5-evic1"), ContainerCount: 1,
		},
		11: {
			Id: 11, ClusterId: querytest.ClusterProd, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_JOB, JobUid: sp(querytest.FailedJobUID),
			WorkloadKind: "CronJob", WorkloadName: "report",
			Category: idiosv1.Category_CATEGORY_JOB_FAILED, FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded",
			LastMessage: sp("Job has reached the specified backoff limit"), Occurrences: 1,
			OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: "2026-08-27T12:00:22.000000Z",
			ClosedAt: sp(actedAt), CloseReason: closeReason(idiosv1.CloseReason_CLOSE_REASON_JOB_FINISHED),
			State: idiosv1.IncidentState_INCIDENT_STATE_JOB_FINISHED,
		},
		12: {
			Id: 12, ClusterId: querytest.ClusterProd, Namespace: seedNS,
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp(querytest.CrashPodUID),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web",
			Category: idiosv1.Category_CATEGORY_PROBE, FirstReason: "Unhealthy", LastReason: "Unhealthy",
			LastMessage: sp("Readiness probe failed: HTTP probe failed with statuscode: 503"),
			Image:       sp(web), ImageTag: sp(webTag), ImageId: sp(webID), Occurrences: 1,
			OpenedAt: "2026-08-27T11:59:00.000000Z", LastSeenAt: "2026-08-27T12:00:27.000000Z",
			State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, PodName: sp("web-7d9f8c6b5-abcde"),
			ContainerCount: 1, ExitCode: i32(1), Signal: i32(0),
		},
	}
	// Every seeded pod is placed on node-a, and the node is denormalised onto
	// the incident at open. Incident 4 opened while its pod was unschedulable
	// and 11 has no pod at all, so neither names a node.
	for id, r := range rows {
		if id != 4 && id != 11 {
			r.NodeName = sp("node-a")
		}
	}
	return rows
}

// wireRows returns the seeded wire rows for ids, in the order given.
func wireRows(t *testing.T, ids ...int64) []*idiosv1.IncidentRow {
	t.Helper()
	all := wireSeed()
	var out []*idiosv1.IncidentRow
	for _, id := range ids {
		row, ok := all[id]
		if !ok {
			t.Fatalf("no seeded incident %d", id)
		}
		out = append(out, row)
	}
	return out
}

// The triage list returns the mapped incident rows, scoped by cluster_ids
// before anything else, and says when the limit cut the answer short.
func TestListIncidentsReturnsMappedRows(t *testing.T) {
	client := newTestServer(t)
	// A limit above the seed's size, so a case that is not about truncation
	// sees the whole set whatever the configured default is.
	const all = 50
	cases := []struct {
		name          string
		req           *idiosv1.ListIncidentsRequest
		want          []*idiosv1.IncidentRow
		wantTruncated bool
	}{
		{
			name: "every cluster, newest activity first",
			req:  &idiosv1.ListIncidentsRequest{Limit: all},
			want: wireRows(t, 1, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2),
		},
		{
			name: "one cluster",
			req:  &idiosv1.ListIncidentsRequest{ClusterIds: []int64{querytest.ClusterStaging}, Limit: all},
			want: wireRows(t, 10, 9, 8),
		},
		{
			// The node filter is what makes a disruption storm one request; it
			// finds placed pods only, so the unschedulable incident 4 and the
			// pod-less job incident 11 are not in the answer.
			name: "the node the pods were placed on",
			req:  &idiosv1.ListIncidentsRequest{NodeName: "node-a", Limit: all},
			want: wireRows(t, 1, 12, 10, 9, 8, 7, 6, 5, 3, 2),
		},
		{
			name: "a filter that matches nothing",
			req:  &idiosv1.ListIncidentsRequest{Namespace: "kube-system", Limit: all},
			want: nil,
		},
		{
			// The default attention_window is 24h and testNow sits minutes after
			// every seeded close, so the cutoff falls before all of them: attention
			// is every row but the two that are dismissed (3, 7).
			name: "state attention",
			req:  &idiosv1.ListIncidentsRequest{State: "attention", Limit: all},
			want: wireRows(t, 1, 12, 11, 10, 9, 8, 6, 5, 4, 2),
		},
		{
			name:          "a limit below the count truncates",
			req:           &idiosv1.ListIncidentsRequest{Limit: 1},
			want:          wireRows(t, 1),
			wantTruncated: true,
		},
		{
			name:          "no limit falls back to the configured default",
			req:           &idiosv1.ListIncidentsRequest{},
			want:          wireRows(t, 1, 12, 11),
			wantTruncated: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := client.ListIncidents(context.Background(), c.req)
			if err != nil {
				t.Fatal(err)
			}
			want := &idiosv1.IncidentsResponse{Incidents: c.want, Truncated: c.wantTruncated}
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// The sidebar gets one count per state over the selected category and one per
// category over the selected state, each facet blind to its own selection.
func TestIncidentCountsGroupTheSeed(t *testing.T) {
	client := newTestServer(t)
	cases := []struct {
		name string
		req  *idiosv1.GetIncidentCountsRequest
		want *idiosv1.IncidentCounts
	}{
		{
			name: "every cluster",
			req:  &idiosv1.GetIncidentCountsRequest{},
			want: &idiosv1.IncidentCounts{
				ByState: []*idiosv1.StateCount{
					{State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, Count: 5},
					{State: idiosv1.IncidentState_INCIDENT_STATE_ACKNOWLEDGED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_RECOVERED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_POD_DELETED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_JOB_FINISHED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_MANUAL, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_DISMISSED, Count: 2},
					{State: idiosv1.IncidentState_INCIDENT_STATE_ATTENTION, Count: 10},
				},
				ByCategory: []*idiosv1.CategoryCount{
					{Category: idiosv1.Category_CATEGORY_OOM, Count: 1},
					{Category: idiosv1.Category_CATEGORY_CRASH, Count: 5},
					{Category: idiosv1.Category_CATEGORY_IMAGE_PULL, Count: 1},
					{Category: idiosv1.Category_CATEGORY_CONFIG, Count: 1},
					{Category: idiosv1.Category_CATEGORY_PROBE, Count: 1},
					{Category: idiosv1.Category_CATEGORY_SCHEDULING, Count: 1},
					{Category: idiosv1.Category_CATEGORY_NODE_PRESSURE, Count: 1},
					{Category: idiosv1.Category_CATEGORY_JOB_FAILED, Count: 1},
				},
			},
		},
		{
			name: "one cluster",
			req:  &idiosv1.GetIncidentCountsRequest{ClusterIds: []int64{querytest.ClusterStaging}},
			want: &idiosv1.IncidentCounts{
				ByState: []*idiosv1.StateCount{
					{State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, Count: 2},
					{State: idiosv1.IncidentState_INCIDENT_STATE_MANUAL, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_ATTENTION, Count: 3},
				},
				ByCategory: []*idiosv1.CategoryCount{
					{Category: idiosv1.Category_CATEGORY_CRASH, Count: 1},
					{Category: idiosv1.Category_CATEGORY_CONFIG, Count: 1},
					{Category: idiosv1.Category_CATEGORY_NODE_PRESSURE, Count: 1},
				},
			},
		},
		{
			name: "state selected leaves by_state whole",
			req:  &idiosv1.GetIncidentCountsRequest{State: "open"},
			want: &idiosv1.IncidentCounts{
				ByState: []*idiosv1.StateCount{
					{State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, Count: 5},
					{State: idiosv1.IncidentState_INCIDENT_STATE_ACKNOWLEDGED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_RECOVERED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_POD_DELETED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_JOB_FINISHED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_MANUAL, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_DISMISSED, Count: 2},
					{State: idiosv1.IncidentState_INCIDENT_STATE_ATTENTION, Count: 10},
				},
				ByCategory: []*idiosv1.CategoryCount{
					{Category: idiosv1.Category_CATEGORY_CRASH, Count: 3},
					{Category: idiosv1.Category_CATEGORY_CONFIG, Count: 1},
					{Category: idiosv1.Category_CATEGORY_PROBE, Count: 1},
				},
			},
		},
		{
			name: "category selected leaves by_category whole",
			req:  &idiosv1.GetIncidentCountsRequest{Category: "crash"},
			want: &idiosv1.IncidentCounts{
				ByState: []*idiosv1.StateCount{
					{State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, Count: 3},
					{State: idiosv1.IncidentState_INCIDENT_STATE_POD_DELETED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_DISMISSED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_ATTENTION, Count: 4},
				},
				ByCategory: []*idiosv1.CategoryCount{
					{Category: idiosv1.Category_CATEGORY_OOM, Count: 1},
					{Category: idiosv1.Category_CATEGORY_CRASH, Count: 5},
					{Category: idiosv1.Category_CATEGORY_IMAGE_PULL, Count: 1},
					{Category: idiosv1.Category_CATEGORY_CONFIG, Count: 1},
					{Category: idiosv1.Category_CATEGORY_PROBE, Count: 1},
					{Category: idiosv1.Category_CATEGORY_SCHEDULING, Count: 1},
					{Category: idiosv1.Category_CATEGORY_NODE_PRESSURE, Count: 1},
					{Category: idiosv1.Category_CATEGORY_JOB_FAILED, Count: 1},
				},
			},
		},
		{
			name: "both selected cross-filter each other",
			req:  &idiosv1.GetIncidentCountsRequest{State: "open", Category: "crash"},
			want: &idiosv1.IncidentCounts{
				ByState: []*idiosv1.StateCount{
					{State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, Count: 3},
					{State: idiosv1.IncidentState_INCIDENT_STATE_POD_DELETED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_DISMISSED, Count: 1},
					{State: idiosv1.IncidentState_INCIDENT_STATE_ATTENTION, Count: 4},
				},
				ByCategory: []*idiosv1.CategoryCount{
					{Category: idiosv1.Category_CATEGORY_CRASH, Count: 3},
					{Category: idiosv1.Category_CATEGORY_CONFIG, Count: 1},
					{Category: idiosv1.Category_CATEGORY_PROBE, Count: 1},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := client.GetIncidentCounts(context.Background(), c.req)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// A filter value outside its closed vocabulary is a 400 whose
// body names every field at fault.
func TestBadFilterValueIs400WithViolations(t *testing.T) {
	client := newTestServer(t)
	cases := []struct {
		name string
		req  *idiosv1.ListIncidentsRequest
		want []*sebufhttp.FieldViolation
	}{
		{
			name: "unknown state",
			req:  &idiosv1.ListIncidentsRequest{State: "bogus"},
			want: []*sebufhttp.FieldViolation{{Field: "state", Description: "unknown incident state bogus"}},
		},
		{
			name: "unknown category",
			req:  &idiosv1.ListIncidentsRequest{Category: "flaky"},
			want: []*sebufhttp.FieldViolation{{Field: "category", Description: "unknown category flaky"}},
		},
		{
			name: "negative limit",
			req:  &idiosv1.ListIncidentsRequest{Limit: -1},
			want: []*sebufhttp.FieldViolation{{Field: "limit", Description: "limit must not be negative"}},
		},
		{
			name: "every violation at once",
			req:  &idiosv1.ListIncidentsRequest{State: "bogus", Category: "flaky", Limit: -1},
			want: []*sebufhttp.FieldViolation{
				{Field: "state", Description: "unknown incident state bogus"},
				{Field: "category", Description: "unknown category flaky"},
				{Field: "limit", Description: "limit must not be negative"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := client.ListIncidents(context.Background(), c.req)
			// The generated client parses a 400 body as ValidationError and
			// anything else as Error, so the type is the status.
			var invalid *sebufhttp.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("got %v, want a validation error", err)
			}
			want := &sebufhttp.ValidationError{Violations: c.want}
			if diff := cmp.Diff(want, invalid, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// The counts share the list's vocabularies, so a bad facet selection is the
// same 400 rather than a silently unfiltered count.
func TestBadCountsFacetIs400WithViolations(t *testing.T) {
	client := newTestServer(t)
	cases := []struct {
		name string
		req  *idiosv1.GetIncidentCountsRequest
		want []*sebufhttp.FieldViolation
	}{
		{
			name: "unknown state",
			req:  &idiosv1.GetIncidentCountsRequest{State: "bogus"},
			want: []*sebufhttp.FieldViolation{{Field: "state", Description: "unknown incident state bogus"}},
		},
		{
			name: "unknown category",
			req:  &idiosv1.GetIncidentCountsRequest{Category: "flaky"},
			want: []*sebufhttp.FieldViolation{{Field: "category", Description: "unknown category flaky"}},
		},
		{
			name: "every violation at once",
			req:  &idiosv1.GetIncidentCountsRequest{State: "bogus", Category: "flaky"},
			want: []*sebufhttp.FieldViolation{
				{Field: "state", Description: "unknown incident state bogus"},
				{Field: "category", Description: "unknown category flaky"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := client.GetIncidentCounts(context.Background(), c.req)
			var invalid *sebufhttp.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("got %v, want a validation error", err)
			}
			want := &sebufhttp.ValidationError{Violations: c.want}
			if diff := cmp.Diff(want, invalid, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}
