package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/status"
)

const seedConnectedAt = "2026-08-27T12:00:00.000000Z"

// fakeRuntime reports a fixed snapshot, standing for the process the server
// runs in.
type fakeRuntime struct {
	snap status.Snapshot
}

func (r fakeRuntime) Snapshot() status.Snapshot { return r.snap }

// fakeKube answers the two discovery calls without a kubeconfig or a cluster.
type fakeKube struct {
	contexts  []KubeContext
	names     []string
	forbidden bool
	err       error
}

func (k fakeKube) Contexts(context.Context) ([]KubeContext, error) {
	return k.contexts, k.err
}

func (k fakeKube) Namespaces(context.Context, string) ([]string, bool, error) {
	return k.names, k.forbidden, k.err
}

// seededClustersWire is the two clusters of the seed with no runtime state.
func seededClustersWire() []*idiosv1.Cluster {
	return []*idiosv1.Cluster{
		{
			Id: querytest.ClusterProd, Identity: sp("id-prod"), Name: "prod", ContextName: "prod",
			ApiServerUrl: "https://127.0.0.1:26443", FirstSeenAt: seedConnectedAt,
			LastConnectedAt: sp(seedConnectedAt), Namespaces: []string{"default", seedNS},
		},
		{
			Id: querytest.ClusterStaging, Identity: sp("id-staging"), Name: "staging", ContextName: "staging",
			ApiServerUrl: "https://127.0.0.1:26444", FirstSeenAt: seedConnectedAt,
			LastConnectedAt: sp(seedConnectedAt), Namespaces: []string{seedNS, "staging-web"},
		},
	}
}

// The scope list is the one list that merges stored rows with process memory,
// and a cluster no watcher has reached yet is not ready rather than missing.
func TestClustersMergeRuntimeState(t *testing.T) {
	const lastEvent = "2026-08-27T15:29:00.000000Z"
	runtime := fakeRuntime{snap: status.Snapshot{Clusters: []status.Cluster{
		{ID: querytest.ClusterProd, Ready: true, LastEventAt: lastEvent, SkewSeconds: 1.5},
	}}}
	merged := seededClustersWire()
	merged[0].Ready, merged[0].LastEventAt, merged[0].SkewSeconds = true, sp(lastEvent), 1.5

	cases := []struct {
		name string
		opts []testOption
		req  *idiosv1.ListClustersRequest
		want []*idiosv1.Cluster
	}{
		{
			name: "no runtime leaves every cluster not ready",
			req:  &idiosv1.ListClustersRequest{},
			want: seededClustersWire(),
		},
		{
			name: "one cluster of two has a watcher",
			opts: []testOption{withRuntime(runtime)},
			req:  &idiosv1.ListClustersRequest{},
			want: merged,
		},
		{
			name: "cluster scope",
			opts: []testOption{withRuntime(runtime)},
			req:  &idiosv1.ListClustersRequest{ClusterIds: []int64{querytest.ClusterStaging}},
			want: merged[1:],
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := newTestServer(t, c.opts...)
			got, err := client.ListClusters(context.Background(), c.req)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(&idiosv1.ClustersResponse{Clusters: c.want}, got, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// A Role that cannot list namespaces is a fact the add-cluster flow must
// show, not an error and not an empty cluster.
func TestKubeNamespacesForbiddenMarker(t *testing.T) {
	cases := []struct {
		name string
		kube fakeKube
		want *idiosv1.KubeNamespacesResponse
	}{
		{
			name: "forbidden",
			kube: fakeKube{forbidden: true},
			want: &idiosv1.KubeNamespacesResponse{Forbidden: true},
		},
		{
			name: "the names the cluster answered",
			kube: fakeKube{names: []string{"default", "idios-smoke", "kube-system"}},
			want: &idiosv1.KubeNamespacesResponse{Names: []string{"default", "idios-smoke", "kube-system"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := newTestServer(t, withKube(c.kube))
			got, err := client.ListKubeNamespaces(context.Background(), &idiosv1.ListKubeNamespacesRequest{
				Context: "prod",
			})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// The contexts are what the kubeconfig names, with the server each points at
// and none of the credentials beside them.
func TestKubeContextsCarryNoCredentials(t *testing.T) {
	kube := fakeKube{contexts: []KubeContext{
		{Name: "orbstack", Cluster: "orbstack", Server: "https://127.0.0.1:26443"},
		{Name: "staging", Cluster: "staging-eu", Server: "https://staging.example.com"},
	}}
	client := newTestServer(t, withKube(kube))
	got, err := client.ListKubeContexts(context.Background(), &idiosv1.ListKubeContextsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	want := &idiosv1.KubeContextsResponse{Contexts: []*idiosv1.KubeContext{
		{Name: "orbstack", Cluster: "orbstack", Server: "https://127.0.0.1:26443"},
		{Name: "staging", Cluster: "staging-eu", Server: "https://staging.example.com"},
	}}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}

// A server built without a kubeconfig reader says so: the two endpoints that
// need one exist but this build cannot answer them, and an empty list would
// read as a cluster with no contexts.
func TestKubeEndpointsWithoutDiscoverySayWhatIsMissing(t *testing.T) {
	stack := newTestStack(t)
	for _, path := range []string{"/v1/kube/contexts", "/v1/kube/contexts/prod/namespaces"} {
		t.Run(path, func(t *testing.T) {
			code, _, body := get(t, stack.url, path)
			if code != http.StatusNotImplemented {
				t.Fatalf("status %d, want %d: %s", code, http.StatusNotImplemented, body)
			}
			want := `{"message":"kube discovery not configured"}`
			if got := string(compact(t, body)); got != want {
				t.Errorf("body %s, want %s", got, want)
			}
		})
	}
}

// A cluster that refuses the call fails the request rather than answering
// with no namespaces, and its message stays in the log: what reached the
// cluster is not the caller's business.
func TestKubeNamespacesReportsTheClusterError(t *testing.T) {
	stack := newTestStack(t, withKube(fakeKube{err: errors.New("connection refused")}))
	code, _, body := get(t, stack.url, "/v1/kube/contexts/prod/namespaces")
	if code != http.StatusInternalServerError {
		t.Fatalf("status %d, want %d: %s", code, http.StatusInternalServerError, body)
	}
	if got, want := string(compact(t, body)), `{"message":"internal error"}`; got != want {
		t.Errorf("body %s, want %s", got, want)
	}
}
