package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	sebufhttp "github.com/SebastienMelki/sebuf/http"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
)

// sidePodUID is the healthy sidecar pod of the seed, which carries no
// incident and so is not one of querytest's incident constants.
const sidePodUID = "pod-side"

// listLimitAboveSeed is a limit no case of the seed reaches, so a case that
// is not about truncation sees every matching row.
const listLimitAboveSeed = 50

// crashPodRowWire is the pod-crash line of the list: two incidents nobody has
// finished with, one container, and the badge its container's state takes.
func crashPodRowWire() *idiosv1.PodRow {
	return &idiosv1.PodRow{
		Uid: querytest.CrashPodUID, ClusterId: querytest.ClusterProd, Namespace: seedNS, Name: "web-7d9f8c6b5-abcde",
		NodeName: sp("node-a"), Phase: "Running", QosClass: sp("BestEffort"),
		ControllerKind: "ReplicaSet", ControllerName: "web-7d9f8c6b5", ControllerUid: "rs-web-1",
		WorkloadKind: "Deployment", WorkloadName: "web",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:45:05.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:00.000000Z", LastSeenAt: "2026-08-27T12:00:01.000000Z",
		OpenIncidents: 2, ContainerCount: 1,
		WorstState: idiosv1.ContainerState_CONTAINER_STATE_WAITING,
	}
}

// The liveness filter is the stored vocabulary as a query string: it selects
// the pods still in the cluster, the ones that are gone, or both, and any
// other spelling is a violation rather than a silent "both".
func TestListPodsLiveFilterValues(t *testing.T) {
	client := newTestServer(t)
	live := []string{querytest.EvictPodUID, querytest.ConfigPodUID, sidePodUID, querytest.InitPodUID,
		querytest.MultiPodUID, querytest.UnschedPod, querytest.PullPodUID, querytest.OOMPodUID, querytest.CrashPodUID}
	all := []string{querytest.EvictPodUID, querytest.ConfigPodUID, sidePodUID, querytest.InitPodUID,
		querytest.JumpPodUID, querytest.MultiPodUID, querytest.UnschedPod, querytest.PullPodUID,
		querytest.OOMPodUID, querytest.CrashPodUID}
	cases := []struct {
		name     string
		live     string
		want     []string
		wantBad  bool
		wantHint string
	}{
		{name: "true is the live pods", live: "true", want: live},
		{name: "false is the deleted ones", live: "false", want: []string{querytest.JumpPodUID}},
		{name: "absent is both", live: "", want: all},
		{name: "anything else is a violation", live: "yes", wantBad: true, wantHint: "live must be true or false, got yes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := client.ListPods(context.Background(), &idiosv1.ListPodsRequest{
				Live: c.live, Limit: listLimitAboveSeed,
			})
			if c.wantBad {
				assertViolations(t, err, []*sebufhttp.FieldViolation{{Field: "live", Description: c.wantHint}})
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, uidsOf(got)); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
			if got.GetTruncated() {
				t.Error("truncated below the limit")
			}
		})
	}
}

// uidsOf is the pod list as the uids it names, which is what a filter case
// asserts: the whole row is compared where the mapping is the subject.
func uidsOf(resp *idiosv1.PodsResponse) []string {
	var uids []string
	for _, p := range resp.GetPods() {
		uids = append(uids, p.GetUid())
	}
	return uids
}

// Every screen is scoped by the selected clusters before its own filters, and
// each filter the request carries reaches the query unchanged.
func TestListPodsFiltersReachTheQuery(t *testing.T) {
	client := newTestServer(t)
	cases := []struct {
		name string
		req  *idiosv1.ListPodsRequest
		want []string
	}{
		{
			name: "one cluster",
			req:  &idiosv1.ListPodsRequest{ClusterIds: []int64{querytest.ClusterStaging}},
			want: []string{querytest.EvictPodUID, querytest.ConfigPodUID},
		},
		{
			name: "a namespace nothing runs in",
			req:  &idiosv1.ListPodsRequest{Namespace: "default"},
			want: nil,
		},
		{
			name: "workload kind",
			req:  &idiosv1.ListPodsRequest{WorkloadKind: "Deployment"},
			want: []string{querytest.EvictPodUID, querytest.JumpPodUID, querytest.CrashPodUID},
		},
		{
			name: "workload name",
			req:  &idiosv1.ListPodsRequest{WorkloadName: "web-4e5f6"},
			want: []string{querytest.MultiPodUID},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.req.Limit = listLimitAboveSeed
			got, err := client.ListPods(context.Background(), c.req)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, uidsOf(got)); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// A request that names no limit gets the daemon's configured default and is
// told the answer was cut short; a limit no page can have is a violation.
func TestPodListsBoundThePage(t *testing.T) {
	client := newTestServer(t)
	t.Run("pods fall back to the configured default", func(t *testing.T) {
		got, err := client.ListPods(context.Background(), &idiosv1.ListPodsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{querytest.EvictPodUID, querytest.ConfigPodUID, sidePodUID}
		if diff := cmp.Diff(want, uidsOf(got)); diff != "" {
			t.Errorf("-want +got\n%s", diff)
		}
		if !got.GetTruncated() {
			t.Errorf("not truncated at the configured limit of %d", testListLimit)
		}
	})
	t.Run("events stop at the requested limit", func(t *testing.T) {
		got, err := client.PodEvents(context.Background(), &idiosv1.PodEventsRequest{
			Uid: querytest.CrashPodUID, Limit: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]int64{3}, eventIDsOf(got)); diff != "" {
			t.Errorf("-want +got\n%s", diff)
		}
		if !got.GetTruncated() {
			t.Error("not truncated at a limit below the count")
		}
	})
	t.Run("events fall back to the configured default", func(t *testing.T) {
		got, err := client.PodEvents(context.Background(), &idiosv1.PodEventsRequest{Uid: querytest.CrashPodUID})
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]int64{3, 2, 1}, eventIDsOf(got)); diff != "" {
			t.Errorf("-want +got\n%s", diff)
		}
		if got.GetTruncated() {
			t.Error("truncated at the configured limit with fewer events than that")
		}
	})
	negative := []*sebufhttp.FieldViolation{{Field: "limit", Description: "limit must not be negative"}}
	t.Run("a negative pod limit", func(t *testing.T) {
		_, err := client.ListPods(context.Background(), &idiosv1.ListPodsRequest{Limit: -1})
		assertViolations(t, err, negative)
	})
	t.Run("a negative event limit", func(t *testing.T) {
		_, err := client.PodEvents(context.Background(), &idiosv1.PodEventsRequest{
			Uid: querytest.CrashPodUID, Limit: -1,
		})
		assertViolations(t, err, negative)
	})
}

// eventIDsOf is the event list as the ids it names, in the order it sent
// them.
func eventIDsOf(resp *idiosv1.EventsResponse) []int64 {
	var ids []int64
	for _, e := range resp.GetEvents() {
		ids = append(ids, e.GetId())
	}
	return ids
}

// assertViolations checks err is the 400 the generated client parses as a
// validation error, carrying exactly these violations.
func assertViolations(t *testing.T, err error, want []*sebufhttp.FieldViolation) {
	t.Helper()
	var invalid *sebufhttp.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("got %v, want a validation error", err)
	}
	if diff := cmp.Diff(&sebufhttp.ValidationError{Violations: want}, invalid, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}

// The pod page carries the flat row, every container, the incidents on the
// pod and the captured files; a uid nothing was recorded for is a 404.
func TestGetPodReturnsTheMappedDetail(t *testing.T) {
	stack := newTestStack(t)
	client := idiosv1.NewIdiosServiceClient(stack.url)
	want := &idiosv1.PodDetail{
		Pod:        crashPodRowWire(),
		Containers: crashContainersWire(),
		Incidents:  wireRows(t, 1, 12),
		Artifacts:  crashArtifactsWire(),
		Siblings: []*idiosv1.SiblingPod{{
			Uid: querytest.JumpPodUID, Name: "web-7d9f8c6b5-jump1", Phase: "Running",
			DeletedAt: sp(deleted), RestartCount: 4, Ready: true,
		}},
		SiblingTotal: 2,
	}
	got, err := client.GetPod(context.Background(), &idiosv1.GetPodRequest{Uid: querytest.CrashPodUID})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
	code, _, body := get(t, stack.url, "/v1/pods/pod-gone")
	if code != http.StatusNotFound {
		t.Fatalf("status %d, want %d: %s", code, http.StatusNotFound, body)
	}
	if got, want := string(compact(t, body)), `{"message":"pod pod-gone not found"}`; got != want {
		t.Errorf("body %s, want %s", got, want)
	}
}

// The pod's events are every event about it, newest first, including the one
// no incident claimed: an event nobody attached is often the only trace of
// what happened.
func TestPodEventsAreNewestFirstIncludingUnattached(t *testing.T) {
	client := newTestServer(t)
	want := &idiosv1.EventsResponse{Events: []*idiosv1.K8SEvent{
		{Id: 3, ClusterId: querytest.ClusterProd, EventUid: "ev-evicted-1", Namespace: seedNS, Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUid: querytest.CrashPodUID,
			Reason: "Evicted", Message: "The node was low on resource: memory.", SourceComponent: "kubelet", Count: 1,
			FirstTs: "2026-08-27T11:59:55.000000Z", LastTs: "2026-08-27T11:59:55.000000Z",
			Category: ep(idiosv1.Category_CATEGORY_NODE_PRESSURE)},
		{Id: 2, ClusterId: querytest.ClusterProd, EventUid: "ev-killing-1", Namespace: seedNS, Type: "Normal",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUid: querytest.CrashPodUID,
			FieldPath: "spec.containers{api}", Reason: "Killing", Message: "Stopping container api",
			SourceComponent: "kubelet", Count: 1,
			FirstTs: "2026-08-27T11:59:50.500000Z", LastTs: "2026-08-27T11:59:50.500000Z", IncidentId: ip(12)},
		{Id: 1, ClusterId: querytest.ClusterProd, EventUid: "ev-unhealthy-1", Namespace: seedNS, Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUid: querytest.CrashPodUID,
			FieldPath: "spec.containers{api}", Reason: "Unhealthy",
			Message:         "Readiness probe failed: HTTP probe failed with statuscode: 503",
			SourceComponent: "kubelet", Count: 3,
			FirstTs: "2026-08-27T11:58:00.000000Z", LastTs: "2026-08-27T11:59:00.000000Z",
			Category: ep(idiosv1.Category_CATEGORY_PROBE), IncidentId: ip(12)},
	}}
	got, err := client.PodEvents(context.Background(), &idiosv1.PodEventsRequest{
		Uid: querytest.CrashPodUID, Limit: listLimitAboveSeed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}

// History reads forwards: the transitions and the condition changes both come
// back oldest first.
func TestPodHistoryReturnsTheMappedHistory(t *testing.T) {
	client := newTestServer(t)
	want := &idiosv1.HistoryResponse{
		Transitions: []*idiosv1.ContainerStateHistory{{
			Id: 5, PodUid: querytest.UnschedPod, ContainerName: "api", Image: web, ImageId: sp(webID),
			ContainerId: sp("containerd://uuu"), State: idiosv1.ContainerState_CONTAINER_STATE_RUNNING,
			K8SStartedAt: sp("2026-08-27T11:58:05.000000Z"), ObservedAt: "2026-08-27T12:00:09.000000Z",
		}},
		Conditions: []*idiosv1.PodCondition{
			{Id: 1, PodUid: querytest.UnschedPod, Type: "PodScheduled", Status: "False", Reason: "Unschedulable",
				Message: sp("0/3 nodes are available: 3 Insufficient cpu. preemption: 0/3 nodes are available: " +
					"3 No preemption victims found for incoming pod."),
				K8STransitionAt: sp("2026-08-27T11:45:01.000000Z"), ObservedAt: "2026-08-27T12:00:07.000000Z"},
			{Id: 2, PodUid: querytest.UnschedPod, Type: "PodScheduled", Status: "True",
				K8STransitionAt: sp("2026-08-27T11:58:00.000000Z"), ObservedAt: "2026-08-27T12:00:09.000000Z"},
		},
	}
	got, err := client.PodHistory(context.Background(), &idiosv1.PodHistoryRequest{Uid: querytest.UnschedPod})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}
