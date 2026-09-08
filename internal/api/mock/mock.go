// Package mock answers every operation of the contract with the committed
// wire fixture of its response, so the application can be built against a
// stable target before a daemon has recorded anything.
package mock

import (
	"context"

	"github.com/hasanMshawrab/idios/api/testdata"
	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
)

// Content is what the artifact content endpoint serves: the mock has no
// captured files, and an empty body is indistinguishable from a log that
// was empty.
const Content = "mock log line\n"

// Prompt is what the prompt endpoint serves for one mode, and whether the
// mode is one of the two. The fixtures are wire messages and not the read
// models a prompt is composed from, so the mock answers with a fixed text of
// the right shape rather than composing one.
func Prompt(mode string) (string, bool) {
	switch mode {
	case "mcp", "snapshot":
		return "mock prompt: mode " + mode + "\n", true
	default:
		return "", false
	}
}

// Server serves the fixtures. It holds nothing: every answer is decoded
// fresh, so a caller that mutates a response cannot change the next one.
type Server struct{}

// New returns the fixture server.
func New() Server { return Server{} }

var _ idiosv1.IdiosServiceServer = Server{}

// ListClusters answers the clusters fixture.
func (Server) ListClusters(context.Context, *idiosv1.ListClustersRequest) (*idiosv1.ClustersResponse, error) {
	return testdata.Decode("clusters", &idiosv1.ClustersResponse{})
}

// ListKubeContexts answers the kube_contexts fixture.
func (Server) ListKubeContexts(context.Context, *idiosv1.ListKubeContextsRequest) (*idiosv1.KubeContextsResponse, error) {
	return testdata.Decode("kube_contexts", &idiosv1.KubeContextsResponse{})
}

// ListKubeNamespaces answers the kube_namespaces fixture.
func (Server) ListKubeNamespaces(context.Context, *idiosv1.ListKubeNamespacesRequest) (*idiosv1.KubeNamespacesResponse, error) {
	return testdata.Decode("kube_namespaces", &idiosv1.KubeNamespacesResponse{})
}

// ListIncidents answers the incidents fixture.
func (Server) ListIncidents(context.Context, *idiosv1.ListIncidentsRequest) (*idiosv1.IncidentsResponse, error) {
	return testdata.Decode("incidents", &idiosv1.IncidentsResponse{})
}

// GetIncidentCounts answers the incident_counts fixture.
func (Server) GetIncidentCounts(context.Context, *idiosv1.GetIncidentCountsRequest) (*idiosv1.IncidentCounts, error) {
	return testdata.Decode("incident_counts", &idiosv1.IncidentCounts{})
}

// GetIncident answers the incident_detail fixture.
func (Server) GetIncident(context.Context, *idiosv1.GetIncidentRequest) (*idiosv1.IncidentDetail, error) {
	return testdata.Decode("incident_detail", &idiosv1.IncidentDetail{})
}

// IncidentTimeline answers the timeline fixture.
func (Server) IncidentTimeline(context.Context, *idiosv1.IncidentTimelineRequest) (*idiosv1.TimelineResponse, error) {
	return testdata.Decode("timeline", &idiosv1.TimelineResponse{})
}

// ListPods answers the pods fixture.
func (Server) ListPods(context.Context, *idiosv1.ListPodsRequest) (*idiosv1.PodsResponse, error) {
	return testdata.Decode("pods", &idiosv1.PodsResponse{})
}

// GetPod answers the pod_detail fixture.
func (Server) GetPod(context.Context, *idiosv1.GetPodRequest) (*idiosv1.PodDetail, error) {
	return testdata.Decode("pod_detail", &idiosv1.PodDetail{})
}

// PodEvents answers the events fixture.
func (Server) PodEvents(context.Context, *idiosv1.PodEventsRequest) (*idiosv1.EventsResponse, error) {
	return testdata.Decode("events", &idiosv1.EventsResponse{})
}

// PodHistory answers the history fixture.
func (Server) PodHistory(context.Context, *idiosv1.PodHistoryRequest) (*idiosv1.HistoryResponse, error) {
	return testdata.Decode("history", &idiosv1.HistoryResponse{})
}

// GetArtifact answers the artifact fixture.
func (Server) GetArtifact(context.Context, *idiosv1.GetArtifactRequest) (*idiosv1.Artifact, error) {
	return testdata.Decode("artifact", &idiosv1.Artifact{})
}

// ListWorkloads answers the workloads fixture.
func (Server) ListWorkloads(context.Context, *idiosv1.ListWorkloadsRequest) (*idiosv1.WorkloadsResponse, error) {
	return testdata.Decode("workloads", &idiosv1.WorkloadsResponse{})
}

// GetWorkload answers the workload_detail fixture.
func (Server) GetWorkload(context.Context, *idiosv1.GetWorkloadRequest) (*idiosv1.WorkloadDetail, error) {
	return testdata.Decode("workload_detail", &idiosv1.WorkloadDetail{})
}

// ListJobs answers the jobs fixture.
func (Server) ListJobs(context.Context, *idiosv1.ListJobsRequest) (*idiosv1.JobsResponse, error) {
	return testdata.Decode("jobs", &idiosv1.JobsResponse{})
}

// GetStatus answers the status fixture, with the running flag cleared: no
// recorder stands behind this server, whatever the fixture was captured from.
func (Server) GetStatus(context.Context, *idiosv1.GetStatusRequest) (*idiosv1.Status, error) {
	s, err := testdata.Decode("status", &idiosv1.Status{})
	if err != nil {
		return s, err
	}
	s.DaemonRunning = false
	return s, nil
}

// StreamIncidents sends the incident_row fixture and then holds the
// subscription open: a stream that ended would look to the application like
// a daemon that stopped, and it would reconnect in a loop.
func (Server) StreamIncidents(ctx context.Context, _ *idiosv1.StreamIncidentsRequest, sender idiosv1.SSESender) error {
	row, err := testdata.Decode("incident_row", &idiosv1.IncidentRow{})
	if err != nil {
		return err
	}
	if err := sender.Send(row); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

// StreamClusters sends the clusters of the clusters fixture one at a time,
// which is the shape of this stream, and then holds the subscription open,
// for the reason StreamIncidents does.
func (Server) StreamClusters(ctx context.Context, _ *idiosv1.StreamClustersRequest, sender idiosv1.SSESender) error {
	list, err := testdata.Decode("clusters", &idiosv1.ClustersResponse{})
	if err != nil {
		return err
	}
	for _, c := range list.GetClusters() {
		if err := sender.Send(c); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

// AcknowledgeIncident answers the incident_row fixture.
func (Server) AcknowledgeIncident(context.Context, *idiosv1.AcknowledgeIncidentRequest) (*idiosv1.IncidentRow, error) {
	return testdata.Decode("incident_row", &idiosv1.IncidentRow{})
}

// UnacknowledgeIncident answers the incident_row fixture.
func (Server) UnacknowledgeIncident(context.Context, *idiosv1.UnacknowledgeIncidentRequest) (*idiosv1.IncidentRow, error) {
	return testdata.Decode("incident_row", &idiosv1.IncidentRow{})
}

// ResolveIncident answers the incident_row fixture.
func (Server) ResolveIncident(context.Context, *idiosv1.ResolveIncidentRequest) (*idiosv1.IncidentRow, error) {
	return testdata.Decode("incident_row", &idiosv1.IncidentRow{})
}

// UnresolveIncident answers the incident_row fixture.
func (Server) UnresolveIncident(context.Context, *idiosv1.UnresolveIncidentRequest) (*idiosv1.IncidentRow, error) {
	return testdata.Decode("incident_row", &idiosv1.IncidentRow{})
}

// DismissIncident answers the incident_row fixture.
func (Server) DismissIncident(context.Context, *idiosv1.DismissIncidentRequest) (*idiosv1.IncidentRow, error) {
	return testdata.Decode("incident_row", &idiosv1.IncidentRow{})
}

// UndismissIncident answers the incident_row fixture.
func (Server) UndismissIncident(context.Context, *idiosv1.UndismissIncidentRequest) (*idiosv1.IncidentRow, error) {
	return testdata.Decode("incident_row", &idiosv1.IncidentRow{})
}

// SetIncidentNote answers the incident_row fixture.
func (Server) SetIncidentNote(context.Context, *idiosv1.SetIncidentNoteRequest) (*idiosv1.IncidentRow, error) {
	return testdata.Decode("incident_row", &idiosv1.IncidentRow{})
}

// DeleteIncident answers the empty response.
func (Server) DeleteIncident(context.Context, *idiosv1.DeleteIncidentRequest) (*idiosv1.DeleteIncidentResponse, error) {
	return &idiosv1.DeleteIncidentResponse{}, nil
}

// firstCluster answers the first cluster of the clusters fixture, which
// every mock cluster write reuses in place of a real mutation.
func firstCluster() (*idiosv1.Cluster, error) {
	list, err := testdata.Decode("clusters", &idiosv1.ClustersResponse{})
	if err != nil {
		return nil, err
	}
	return list.GetClusters()[0], nil
}

// AddCluster answers the first cluster of the clusters fixture.
func (Server) AddCluster(context.Context, *idiosv1.AddClusterRequest) (*idiosv1.Cluster, error) {
	return firstCluster()
}

// RenameCluster answers the first cluster of the clusters fixture.
func (Server) RenameCluster(context.Context, *idiosv1.RenameClusterRequest) (*idiosv1.Cluster, error) {
	return firstCluster()
}

// SetClusterGrafana answers the first cluster of the clusters fixture.
func (Server) SetClusterGrafana(context.Context, *idiosv1.SetClusterGrafanaRequest) (*idiosv1.Cluster, error) {
	return firstCluster()
}

// DeleteCluster answers the empty response.
func (Server) DeleteCluster(context.Context, *idiosv1.DeleteClusterRequest) (*idiosv1.DeleteClusterResponse, error) {
	return &idiosv1.DeleteClusterResponse{}, nil
}

// AddWatchedNamespace answers the first cluster of the clusters fixture.
func (Server) AddWatchedNamespace(context.Context, *idiosv1.AddWatchedNamespaceRequest) (*idiosv1.Cluster, error) {
	return firstCluster()
}

// RemoveWatchedNamespace answers the first cluster of the clusters fixture.
func (Server) RemoveWatchedNamespace(context.Context, *idiosv1.RemoveWatchedNamespaceRequest) (*idiosv1.Cluster, error) {
	return firstCluster()
}
