package api

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	sebufhttp "github.com/SebastienMelki/sebuf/http"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

const (
	deployment  = "Deployment"
	replicaSet  = "ReplicaSet"
	webNext     = "registry.example.com/web:1.4.3"
	workerImage = "registry.example.com/worker@sha256:7777"
)

func categoryCount(c idiosv1.Category, n int32) *idiosv1.CategoryCount {
	return &idiosv1.CategoryCount{Category: c, Count: n}
}

// seededWorkloadsWire is every workload querytest.Seed holds, in the order
// the tree lists them: down the identity chain, cluster first.
func seededWorkloadsWire() []*idiosv1.WorkloadRow {
	return []*idiosv1.WorkloadRow{
		{
			ClusterId: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: "CronJob", WorkloadName: "report",
			IncidentsByCategory: []*idiosv1.CategoryCount{categoryCount(idiosv1.Category_CATEGORY_JOB_FAILED, 1)},
			Occurrences:         1,
		},
		webWorkloadWire(),
		{
			ClusterId: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "mem-hog-5f6d7",
			IncidentsByCategory: []*idiosv1.CategoryCount{categoryCount(idiosv1.Category_CATEGORY_OOM, 1)},
			OpenIncidents:       1, Occurrences: 1,
			ImageTags: []*idiosv1.TagCount{{Tag: "2.0.0", Count: 1}}, LivePods: 1,
		},
		// The sidecar pod's ReplicaSet has never failed; the tree must still
		// show what is running under it.
		{
			ClusterId: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-1c2d3",
			LivePods: 1,
		},
		// An unschedulable pod has no image on its incident, so it names no
		// tag at all.
		{
			ClusterId: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-2b3c4",
			IncidentsByCategory: []*idiosv1.CategoryCount{categoryCount(idiosv1.Category_CATEGORY_SCHEDULING, 1)},
			Occurrences:         1, LivePods: 1,
		},
		{
			ClusterId: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-4e5f6",
			IncidentsByCategory: []*idiosv1.CategoryCount{categoryCount(idiosv1.Category_CATEGORY_CRASH, 1)},
			OpenIncidents:       1, Occurrences: 1,
			ImageTags: []*idiosv1.TagCount{{Tag: webTag, Count: 1}}, LivePods: 1,
		},
		{
			ClusterId: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-66c9d",
			IncidentsByCategory: []*idiosv1.CategoryCount{categoryCount(idiosv1.Category_CATEGORY_IMAGE_PULL, 1)},
			Occurrences:         2,
			ImageTags:           []*idiosv1.TagCount{{Tag: "does-not-exist", Count: 1}}, LivePods: 1,
		},
		{
			ClusterId: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-9a8b7",
			IncidentsByCategory: []*idiosv1.CategoryCount{categoryCount(idiosv1.Category_CATEGORY_CRASH, 1)},
			Occurrences:         1,
			ImageTags:           []*idiosv1.TagCount{{Tag: "3.1.0", Count: 1}}, LivePods: 1,
		},
		{
			ClusterId: querytest.ClusterStaging, Namespace: seedNS, WorkloadKind: deployment, WorkloadName: "web",
			IncidentsByCategory: []*idiosv1.CategoryCount{
				categoryCount(idiosv1.Category_CATEGORY_CRASH, 1),
				categoryCount(idiosv1.Category_CATEGORY_NODE_PRESSURE, 1),
			},
			OpenIncidents: 1, Occurrences: 2,
			ImageTags: []*idiosv1.TagCount{{Tag: webTag, Count: 1}}, LivePods: 1,
		},
		{
			ClusterId: querytest.ClusterStaging, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-5b8c7",
			IncidentsByCategory: []*idiosv1.CategoryCount{categoryCount(idiosv1.Category_CATEGORY_CONFIG, 1)},
			OpenIncidents:       1, Occurrences: 1,
			ImageTags: []*idiosv1.TagCount{{Tag: webTag, Count: 1}}, LivePods: 1,
		},
	}
}

// webWorkloadWire is the Deployment both crash-loop and restart-jump belong
// to, with its categories in the enum's order.
func webWorkloadWire() *idiosv1.WorkloadRow {
	return &idiosv1.WorkloadRow{
		ClusterId: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: deployment, WorkloadName: "web",
		IncidentsByCategory: []*idiosv1.CategoryCount{
			categoryCount(idiosv1.Category_CATEGORY_CRASH, 2),
			categoryCount(idiosv1.Category_CATEGORY_PROBE, 1),
		},
		OpenIncidents: 2, Occurrences: 4,
		ImageTags: []*idiosv1.TagCount{{Tag: webTag, Count: 3}}, LivePods: 1, DeletedPods: 1,
	}
}

// jumpPodRowWire is the pod the rollout replaced: gone from the cluster, and
// still a line of its workload's pod list.
func jumpPodRowWire() *idiosv1.PodRow {
	return &idiosv1.PodRow{
		Uid: querytest.JumpPodUID, ClusterId: querytest.ClusterProd, Namespace: seedNS, Name: "web-7d9f8c6b5-jump1",
		NodeName: sp("node-a"), Phase: "Running", QosClass: sp("BestEffort"),
		ControllerKind: replicaSet, ControllerName: "web-7d9f8c6b5", ControllerUid: "rs-web-1",
		WorkloadKind: deployment, WorkloadName: "web",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:45:05.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:12.000000Z", LastSeenAt: "2026-08-27T12:00:13.000000Z",
		DeletedAt:      sp(deleted),
		DeletionSource: ep(idiosv1.DeletionSource_DELETION_SOURCE_WATCH),
		DeletionReason: deletionReason(idiosv1.DeletionReason_DELETION_REASON_ROLLOUT),
		ContainerCount: 1, WorstState: idiosv1.ContainerState_CONTAINER_STATE_RUNNING,
	}
}

// controllerLessPodUID is the one controller-less pod this test adds, to
// carry pod_uid and pod_name on the wire, a Deployment row's two fields
// left empty.
const controllerLessPodUID = "pod-none-crash"

// addControllerLessPod gives the seed one pod with no controller and an open
// incident on it, so the list holds a kind "none" row with its own identity.
func addControllerLessPod(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		if err := store.UpsertPod(ctx, tx, store.Pod{
			UID: controllerLessPodUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "one-off-migrate",
			Phase: "Running", WorkloadKind: "none",
			CreatedAt:   "2026-08-27T11:45:00.000000Z",
			FirstSeenAt: "2026-08-27T13:10:00.000000Z", LastSeenAt: "2026-08-27T13:10:00.000000Z",
		}); err != nil {
			return err
		}
		at := "2026-08-27T13:10:01.000000Z"
		_, err := store.OpenIncident(ctx, tx, store.Incident{
			ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod,
			PodUID: sp(controllerLessPodUID), WorkloadKind: "none", Category: store.CategoryCrash,
			FirstReason: "Error", LastReason: "Error", Occurrences: 1, OpenedAt: at, LastSeenAt: at,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// controllerLessRowWire is addControllerLessPod's row, as the wire spells
// it: pod_uid and pod_name are the only fields the split adds.
func controllerLessRowWire() *idiosv1.WorkloadRow {
	return &idiosv1.WorkloadRow{
		ClusterId: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: "none",
		PodUid: controllerLessPodUID, PodName: "one-off-migrate",
		IncidentsByCategory: []*idiosv1.CategoryCount{categoryCount(idiosv1.Category_CATEGORY_CRASH, 1)},
		OpenIncidents:       1, Occurrences: 1, LivePods: 1,
	}
}

// The tree groups on the identity a service keeps while its pods rotate, and
// every screen is scoped by the selected clusters before its own filters. A
// controller-less pod is its own row: pod_uid and pod_name are set only for
// it, and the Deployment row the split does not touch carries neither.
func TestListWorkloadsReturnsMappedRows(t *testing.T) {
	stack := newTestStack(t)
	addControllerLessPod(t, stack.store)
	client := idiosv1.NewIdiosServiceClient(stack.url)
	// none sorts after every real kind alphabetically, and cluster 1 is where
	// it was seeded, so it lands between the ReplicaSet rows and cluster 2.
	all := append(append(seededWorkloadsWire()[:8:8], controllerLessRowWire()), seededWorkloadsWire()[8:]...)
	cases := []struct {
		name          string
		req           *idiosv1.ListWorkloadsRequest
		want          []*idiosv1.WorkloadRow
		wantTruncated bool
	}{
		{
			name: "every cluster",
			req:  &idiosv1.ListWorkloadsRequest{Limit: listLimitAboveSeed},
			want: all,
		},
		{
			name: "one cluster",
			req:  &idiosv1.ListWorkloadsRequest{ClusterIds: []int64{querytest.ClusterStaging}, Limit: listLimitAboveSeed},
			want: all[9:],
		},
		{
			name:          "no limit falls back to the configured default",
			req:           &idiosv1.ListWorkloadsRequest{},
			want:          all[:testListLimit],
			wantTruncated: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := client.ListWorkloads(context.Background(), c.req)
			if err != nil {
				t.Fatal(err)
			}
			want := &idiosv1.WorkloadsResponse{Workloads: c.want, Truncated: c.wantTruncated}
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// The workload page answers "did failures start after a deploy" and "is this
// flapping" beside the pods the workload has now; a workload neither an
// incident nor a pod names is a 404.
func TestGetWorkloadReturnsTheMappedDetail(t *testing.T) {
	stack := newTestStack(t)
	client := idiosv1.NewIdiosServiceClient(stack.url)
	want := &idiosv1.WorkloadDetail{
		Workload: webWorkloadWire(),
		Rollouts: []*idiosv1.Rollout{
			{
				ReplicasetUid: "rs-web-2", ReplicasetName: "web-8e0a9d7c6", Revision: ip(8),
				Images:    []string{webNext, workerImage},
				CreatedAt: "2026-08-27T11:58:00.000000Z", Replicas: i32(3), ReadyReplicas: i32(2), AvailableReplicas: i32(2),
				FirstSeenAt: "2026-08-27T12:00:26.000000Z", LastSeenAt: "2026-08-27T12:00:26.000000Z", Incidents: 1,
			},
			{
				ReplicasetUid: "rs-web-1", ReplicasetName: "web-7d9f8c6b5", Revision: ip(7),
				Images:    []string{web, workerImage},
				CreatedAt: "2026-08-27T11:44:00.000000Z", Replicas: i32(3),
				FirstSeenAt: "2026-08-27T12:00:25.000000Z", LastSeenAt: "2026-08-27T12:00:25.000000Z", Incidents: 3,
			},
		},
		RestartsByHour: []*idiosv1.HourBucket{
			{Hour: "2026-08-27T12:00:00.000000Z", Restarts: 1, Reconstructed: true},
		},
		Pods: []*idiosv1.PodRow{jumpPodRowWire(), crashPodRowWire()},
	}
	got, err := client.GetWorkload(context.Background(), &idiosv1.GetWorkloadRequest{
		ClusterId: querytest.ClusterProd, Namespace: seedNS, Kind: deployment, Name: "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
	code, _, body := get(t, stack.url, "/v1/workloads/1/idios-smoke/Deployment/nothing")
	if code != http.StatusNotFound {
		t.Fatalf("status %d, want %d: %s", code, http.StatusNotFound, body)
	}
	want404 := `{"message":"workload 1/idios-smoke/Deployment/nothing not found"}`
	if got := string(compact(t, body)); got != want404 {
		t.Errorf("body %s, want %s", got, want404)
	}

	// A second live pod on the same workload, added after the count above was
	// asserted, gives pods_live=true&pods_limit=1 something to truncate.
	addSecondLiveWebPod(t, stack.store)
	boundedWorkload := webWorkloadWire()
	boundedWorkload.LivePods = 2
	wantBounded := &idiosv1.WorkloadDetail{
		Workload:       boundedWorkload,
		Rollouts:       want.Rollouts,
		RestartsByHour: want.RestartsByHour,
		Pods:           []*idiosv1.PodRow{secondLiveWebPodWire()},
		PodsTruncated:  true,
	}
	gotBounded, err := client.GetWorkload(context.Background(), &idiosv1.GetWorkloadRequest{
		ClusterId: querytest.ClusterProd, Namespace: seedNS, Kind: deployment, Name: "web",
		PodsLive: "true", PodsLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(wantBounded, gotBounded, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}

// addSecondLiveWebPod gives the web Deployment a second live pod, newer than
// pod-crash, so a filtered and bounded request has something to cut off.
func addSecondLiveWebPod(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		return store.UpsertPod(ctx, tx, store.Pod{
			UID: "pod-web-live2", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-7d9f8c6b5-live2",
			Phase: "Running", NodeName: sp("node-a"), QOSClass: sp("BestEffort"),
			ControllerKind: replicaSet, ControllerName: "web-7d9f8c6b5", ControllerUID: "rs-web-1",
			WorkloadKind: deployment, WorkloadName: "web",
			CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:45:05.000000Z"),
			FirstSeenAt: "2026-08-27T13:00:00.000000Z", LastSeenAt: "2026-08-27T13:00:00.000000Z",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

// secondLiveWebPodWire is addSecondLiveWebPod's row, as the wire spells it.
func secondLiveWebPodWire() *idiosv1.PodRow {
	return &idiosv1.PodRow{
		Uid: "pod-web-live2", ClusterId: querytest.ClusterProd, Namespace: seedNS, Name: "web-7d9f8c6b5-live2",
		NodeName: sp("node-a"), Phase: "Running", QosClass: sp("BestEffort"),
		ControllerKind: replicaSet, ControllerName: "web-7d9f8c6b5", ControllerUid: "rs-web-1",
		WorkloadKind: deployment, WorkloadName: "web",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:45:05.000000Z"),
		FirstSeenAt: "2026-08-27T13:00:00.000000Z", LastSeenAt: "2026-08-27T13:00:00.000000Z",
	}
}

// completeJobWire and failedJobWire are the two runs the seed drives through
// the processor. Complete is read from the condition: the failed run's
// counter passed the backoff limit and the complete run's succeeded counter
// counts pods, and neither says whether the work was done.
func completeJobWire() *idiosv1.JobRow {
	return &idiosv1.JobRow{
		Uid: "job-report-2", ClusterId: querytest.ClusterProd, Namespace: seedNS, Name: "report-28812350",
		Succeeded: 1, BackoffLimit: i32(2), Completions: i32(1), Parallelism: i32(1), RestartPolicy: "Never",
		ConditionType: sp("Complete"),
		CreatedAt:     "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:50:02.000000Z"),
		FinishedAt:  sp("2026-08-27T11:52:00.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:23.000000Z", LastSeenAt: "2026-08-27T12:00:24.000000Z",
		Complete: true,
	}
}

func failedJobWire() *idiosv1.JobRow {
	return &idiosv1.JobRow{
		Uid: querytest.FailedJobUID, ClusterId: querytest.ClusterProd, Namespace: seedNS, Name: "report-28812345",
		CronjobUid: sp("cj-report"), CronjobName: sp("report"), Failed: 3,
		BackoffLimit: i32(2), Completions: i32(1), Parallelism: i32(1),
		ActiveDeadlineSeconds: i64(900), RestartPolicy: "Never",
		ConditionType: sp("Failed"), ConditionReason: sp("BackoffLimitExceeded"),
		ConditionMessage: sp("Job has reached the specified backoff limit"),
		CreatedAt:        "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:45:02.000000Z"),
		FinishedAt:  sp("2026-08-27T11:57:00.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:21.000000Z", LastSeenAt: "2026-08-27T12:00:22.000000Z",
	}
}

// The job list is most recently started first, carries the condition each
// run ended on, and counts the scope it was cut from.
func TestListJobsReturnsMappedRows(t *testing.T) {
	client := newTestServer(t)
	cases := []struct {
		name string
		req  *idiosv1.ListJobsRequest
		want *idiosv1.JobsResponse
	}{
		{
			name: "newest start first",
			req:  &idiosv1.ListJobsRequest{Limit: listLimitAboveSeed},
			want: &idiosv1.JobsResponse{Jobs: []*idiosv1.JobRow{completeJobWire(), failedJobWire()}, Total: 2, FailedTotal: 1},
		},
		{
			name: "one cronjob",
			req:  &idiosv1.ListJobsRequest{CronjobUid: "cj-report", Limit: listLimitAboveSeed},
			want: &idiosv1.JobsResponse{Jobs: []*idiosv1.JobRow{failedJobWire()}, Total: 1, FailedTotal: 1},
		},
		{
			name: "one run by uid",
			req:  &idiosv1.ListJobsRequest{JobUid: querytest.FailedJobUID, Limit: listLimitAboveSeed},
			want: &idiosv1.JobsResponse{Jobs: []*idiosv1.JobRow{failedJobWire()}, Total: 1, FailedTotal: 1},
		},
		{
			name: "a cluster with no jobs",
			req:  &idiosv1.ListJobsRequest{ClusterIds: []int64{querytest.ClusterStaging}, Limit: listLimitAboveSeed},
			want: &idiosv1.JobsResponse{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := client.ListJobs(context.Background(), c.req)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// A flag outside its two-valued vocabulary is a violation, and every bad
// flag of one request is reported in the same 400.
func TestListJobsRejectsUnknownFlagValues(t *testing.T) {
	client := newTestServer(t)
	_, err := client.ListJobs(context.Background(), &idiosv1.ListJobsRequest{Live: "yes", Failed: "1"})
	assertViolations(t, err, []*sebufhttp.FieldViolation{
		{Field: "live", Description: "live must be true or false, got yes"},
		{Field: "failed", Description: "failed must be true or false, got 1"},
	})
}
