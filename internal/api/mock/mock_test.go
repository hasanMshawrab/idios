package mock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/api/testdata"
	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
)

// fixture is the committed message the operation under test must answer with.
func fixture[T proto.Message](t *testing.T, name string, msg T) T {
	t.Helper()
	out, err := testdata.Decode(name, msg)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// notRunning is the status fixture as the mock serves it.
func notRunning(s *idiosv1.Status) *idiosv1.Status {
	s.DaemonRunning = false
	return s
}

// nthEvent reads n messages off a stream, closes it, and returns the last.
func nthEvent[T proto.Message](stream *idiosv1.IdiosServiceEventStream[T], err error, msg T, n int) (proto.Message, error) {
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	for range n {
		if !stream.Next(msg) {
			return nil, stream.Err()
		}
	}
	return msg, nil
}

// The application is built against the mock before a daemon has data, so
// every operation has to answer with the fixture its screens were written
// from, unchanged by the round trip.
func TestMockServesEveryFixture(t *testing.T) {
	mux := http.NewServeMux()
	if err := idiosv1.RegisterIdiosServiceServer(New(), idiosv1.WithMux(mux)); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := idiosv1.NewIdiosServiceClient(srv.URL)

	cases := []struct {
		name string
		call func(context.Context) (proto.Message, error)
		want proto.Message
	}{
		{"clusters", func(ctx context.Context) (proto.Message, error) {
			return client.ListClusters(ctx, &idiosv1.ListClustersRequest{})
		}, fixture(t, "clusters", &idiosv1.ClustersResponse{})},
		{"kube_contexts", func(ctx context.Context) (proto.Message, error) {
			return client.ListKubeContexts(ctx, &idiosv1.ListKubeContextsRequest{})
		}, fixture(t, "kube_contexts", &idiosv1.KubeContextsResponse{})},
		{"kube_namespaces", func(ctx context.Context) (proto.Message, error) {
			return client.ListKubeNamespaces(ctx, &idiosv1.ListKubeNamespacesRequest{Context: "orbstack"})
		}, fixture(t, "kube_namespaces", &idiosv1.KubeNamespacesResponse{})},
		{"incidents", func(ctx context.Context) (proto.Message, error) {
			return client.ListIncidents(ctx, &idiosv1.ListIncidentsRequest{})
		}, fixture(t, "incidents", &idiosv1.IncidentsResponse{})},
		{"incident_counts", func(ctx context.Context) (proto.Message, error) {
			return client.GetIncidentCounts(ctx, &idiosv1.GetIncidentCountsRequest{})
		}, fixture(t, "incident_counts", &idiosv1.IncidentCounts{})},
		{"incident_detail", func(ctx context.Context) (proto.Message, error) {
			return client.GetIncident(ctx, &idiosv1.GetIncidentRequest{Id: 412})
		}, fixture(t, "incident_detail", &idiosv1.IncidentDetail{})},
		{"timeline", func(ctx context.Context) (proto.Message, error) {
			return client.IncidentTimeline(ctx, &idiosv1.IncidentTimelineRequest{Id: 412})
		}, fixture(t, "timeline", &idiosv1.TimelineResponse{})},
		{"pods", func(ctx context.Context) (proto.Message, error) {
			return client.ListPods(ctx, &idiosv1.ListPodsRequest{})
		}, fixture(t, "pods", &idiosv1.PodsResponse{})},
		{"pod_detail", func(ctx context.Context) (proto.Message, error) {
			return client.GetPod(ctx, &idiosv1.GetPodRequest{Uid: "pod-crash"})
		}, fixture(t, "pod_detail", &idiosv1.PodDetail{})},
		{"events", func(ctx context.Context) (proto.Message, error) {
			return client.PodEvents(ctx, &idiosv1.PodEventsRequest{Uid: "pod-crash"})
		}, fixture(t, "events", &idiosv1.EventsResponse{})},
		{"history", func(ctx context.Context) (proto.Message, error) {
			return client.PodHistory(ctx, &idiosv1.PodHistoryRequest{Uid: "pod-crash"})
		}, fixture(t, "history", &idiosv1.HistoryResponse{})},
		{"artifact", func(ctx context.Context) (proto.Message, error) {
			return client.GetArtifact(ctx, &idiosv1.GetArtifactRequest{Id: 81})
		}, fixture(t, "artifact", &idiosv1.Artifact{})},
		{"workloads", func(ctx context.Context) (proto.Message, error) {
			return client.ListWorkloads(ctx, &idiosv1.ListWorkloadsRequest{})
		}, fixture(t, "workloads", &idiosv1.WorkloadsResponse{})},
		{"workload_detail", func(ctx context.Context) (proto.Message, error) {
			return client.GetWorkload(ctx, &idiosv1.GetWorkloadRequest{
				ClusterId: 1, Namespace: "idios-smoke", Kind: "Deployment", Name: "checkout-api",
			})
		}, fixture(t, "workload_detail", &idiosv1.WorkloadDetail{})},
		{"jobs", func(ctx context.Context) (proto.Message, error) {
			return client.ListJobs(ctx, &idiosv1.ListJobsRequest{})
		}, fixture(t, "jobs", &idiosv1.JobsResponse{})},
		// The fixture was captured from a daemon; nothing records behind this
		// server, so the flag it carries is the one field that is not served
		// as written.
		{"status", func(ctx context.Context) (proto.Message, error) {
			return client.GetStatus(ctx, &idiosv1.GetStatusRequest{})
		}, notRunning(fixture(t, "status", &idiosv1.Status{}))},
		{"incident_row stream", func(ctx context.Context) (proto.Message, error) {
			stream, err := client.StreamIncidents(ctx, &idiosv1.StreamIncidentsRequest{})
			return nthEvent(stream, err, &idiosv1.IncidentRow{}, 1)
		}, fixture(t, "incident_row", &idiosv1.IncidentRow{})},
		// The cluster stream carries one cluster per event, so the fixture's
		// list arrives as its rows in order and not as a single message.
		{"cluster stream", func(ctx context.Context) (proto.Message, error) {
			stream, err := client.StreamClusters(ctx, &idiosv1.StreamClustersRequest{})
			return nthEvent(stream, err, &idiosv1.Cluster{}, 1)
		}, fixture(t, "clusters", &idiosv1.ClustersResponse{}).GetClusters()[0]},
		{"cluster stream second event", func(ctx context.Context) (proto.Message, error) {
			stream, err := client.StreamClusters(ctx, &idiosv1.StreamClustersRequest{})
			return nthEvent(stream, err, &idiosv1.Cluster{}, 2)
		}, fixture(t, "clusters", &idiosv1.ClustersResponse{}).GetClusters()[1]},
		{"acknowledge incident", func(ctx context.Context) (proto.Message, error) {
			return client.AcknowledgeIncident(ctx, &idiosv1.AcknowledgeIncidentRequest{Id: 412})
		}, fixture(t, "incident_row", &idiosv1.IncidentRow{})},
		{"unacknowledge incident", func(ctx context.Context) (proto.Message, error) {
			return client.UnacknowledgeIncident(ctx, &idiosv1.UnacknowledgeIncidentRequest{Id: 412})
		}, fixture(t, "incident_row", &idiosv1.IncidentRow{})},
		{"resolve incident", func(ctx context.Context) (proto.Message, error) {
			return client.ResolveIncident(ctx, &idiosv1.ResolveIncidentRequest{Id: 412})
		}, fixture(t, "incident_row", &idiosv1.IncidentRow{})},
		{"dismiss incident", func(ctx context.Context) (proto.Message, error) {
			return client.DismissIncident(ctx, &idiosv1.DismissIncidentRequest{Id: 412})
		}, fixture(t, "incident_row", &idiosv1.IncidentRow{})},
		{"undismiss incident", func(ctx context.Context) (proto.Message, error) {
			return client.UndismissIncident(ctx, &idiosv1.UndismissIncidentRequest{Id: 412})
		}, fixture(t, "incident_row", &idiosv1.IncidentRow{})},
		{"set incident note", func(ctx context.Context) (proto.Message, error) {
			return client.SetIncidentNote(ctx, &idiosv1.SetIncidentNoteRequest{Id: 412, Note: "fixed in PR 123"})
		}, fixture(t, "incident_row", &idiosv1.IncidentRow{})},
		{"delete incident", func(ctx context.Context) (proto.Message, error) {
			return client.DeleteIncident(ctx, &idiosv1.DeleteIncidentRequest{Id: 412})
		}, &idiosv1.DeleteIncidentResponse{}},
		{"add cluster", func(ctx context.Context) (proto.Message, error) {
			return client.AddCluster(ctx, &idiosv1.AddClusterRequest{ContextName: "orbstack", Name: "prod"})
		}, fixture(t, "clusters", &idiosv1.ClustersResponse{}).GetClusters()[0]},
		{"rename cluster", func(ctx context.Context) (proto.Message, error) {
			return client.RenameCluster(ctx, &idiosv1.RenameClusterRequest{Id: 1, Name: "prod"})
		}, fixture(t, "clusters", &idiosv1.ClustersResponse{}).GetClusters()[0]},
		{"delete cluster", func(ctx context.Context) (proto.Message, error) {
			return client.DeleteCluster(ctx, &idiosv1.DeleteClusterRequest{Id: 1})
		}, &idiosv1.DeleteClusterResponse{}},
		{"add watched namespace", func(ctx context.Context) (proto.Message, error) {
			return client.AddWatchedNamespace(ctx, &idiosv1.AddWatchedNamespaceRequest{Id: 1, Name: "shop"})
		}, fixture(t, "clusters", &idiosv1.ClustersResponse{}).GetClusters()[0]},
		{"remove watched namespace", func(ctx context.Context) (proto.Message, error) {
			return client.RemoveWatchedNamespace(ctx, &idiosv1.RemoveWatchedNamespaceRequest{Id: 1, Name: "shop"})
		}, fixture(t, "clusters", &idiosv1.ClustersResponse{}).GetClusters()[0]},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got, err := c.call(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s (-want +got):\n%s", c.name, diff)
			}
		})
	}
}
