package api

import (
	"context"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query"
	"github.com/hasanMshawrab/idios/internal/status"
)

// ListClusters answers the scope list: the stored rows merged with what the
// running process knows about each watcher.
func (s *Server) ListClusters(ctx context.Context, req *idiosv1.ListClustersRequest) (*idiosv1.ClustersResponse, error) {
	rows, err := query.ListClusters(ctx, s.db)
	if err != nil {
		return nil, err
	}
	live := s.clusterState()
	out := &idiosv1.ClustersResponse{}
	for _, r := range rows {
		if !inScope(req.GetClusterIds(), r.ID) {
			continue
		}
		out.Clusters = append(out.Clusters, cluster(r, live[r.ID]))
	}
	return out, nil
}

// ListKubeContexts answers the contexts of the kubeconfig the daemon reads.
func (s *Server) ListKubeContexts(ctx context.Context, _ *idiosv1.ListKubeContextsRequest) (*idiosv1.KubeContextsResponse, error) {
	if s.kube == nil {
		return nil, errNoKubeDiscovery
	}
	contexts, err := s.kube.Contexts(ctx)
	if err != nil {
		return nil, err
	}
	return &idiosv1.KubeContextsResponse{Contexts: mapAll(contexts, kubeContext)}, nil
}

// ListKubeNamespaces answers what one cluster says its namespaces are.
func (s *Server) ListKubeNamespaces(ctx context.Context, req *idiosv1.ListKubeNamespacesRequest) (*idiosv1.KubeNamespacesResponse, error) {
	if s.kube == nil {
		return nil, errNoKubeDiscovery
	}
	names, forbidden, err := s.kube.Namespaces(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	return &idiosv1.KubeNamespacesResponse{Names: names, Forbidden: forbidden}, nil
}

// inScope reports whether id is one of the requested clusters; no request
// means every cluster.
func inScope(ids []int64, id int64) bool {
	if len(ids) == 0 {
		return true
	}
	for _, want := range ids {
		if want == id {
			return true
		}
	}
	return false
}

// clusterState is the runtime state of each watcher, keyed by cluster. It is
// empty without a running process behind the listener, which leaves every
// cluster not ready rather than claiming a state nobody reported.
func (s *Server) clusterState() map[int64]status.Cluster {
	out := map[int64]status.Cluster{}
	if s.runtime == nil {
		return out
	}
	for _, c := range s.runtime.Snapshot().Clusters {
		out[c.ID] = c
	}
	return out
}

// cluster maps one stored cluster with the runtime state of its watcher. A
// cluster no watcher has reached carries the zero state: not ready, no last
// event, no skew.
func cluster(r query.ClusterRow, live status.Cluster) *idiosv1.Cluster {
	c := r.Cluster
	out := &idiosv1.Cluster{
		Id:                c.ID,
		Identity:          c.Identity,
		Name:              c.Name,
		ContextName:       c.ContextName,
		ApiServerUrl:      c.APIServerURL,
		FirstSeenAt:       c.FirstSeenAt,
		LastConnectedAt:   c.LastConnectedAt,
		LastError:         c.LastError,
		LastErrorAt:       c.LastErrorAt,
		Namespaces:        r.Namespaces,
		Ready:             live.Ready,
		SkewSeconds:       live.SkewSeconds,
		GrafanaUrl:        c.GrafanaURL,
		LokiDatasourceUid: c.LokiDatasourceUID,
		LogSelector:       c.LogSelector,
	}
	if live.LastEventAt != "" {
		out.LastEventAt = &live.LastEventAt
	}
	return out
}

// kubeContext maps one context of the kubeconfig.
func kubeContext(c KubeContext) *idiosv1.KubeContext {
	return &idiosv1.KubeContext{Name: c.Name, Cluster: c.Cluster, Server: c.Server}
}
