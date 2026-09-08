package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
)

// A second cluster of an already-used name is refused with the id of the row
// that has it, and an empty request reports both missing fields at once.
func TestAddClusterRefusesASecondRowOfTheSameName(t *testing.T) {
	stack := newTestStack(t)
	client := idiosv1.NewIdiosServiceClient(stack.url)

	got, err := client.AddCluster(context.Background(), &idiosv1.AddClusterRequest{ContextName: "orbstack", Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	want := &idiosv1.Cluster{Id: 3, Name: "dev", ContextName: "orbstack", FirstSeenAt: testNow}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "a second cluster of the same name",
			body:       `{"contextName":"orbstack","name":"dev"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"violations":[{"field":"name","description":"cluster \"dev\" exists (id 3)"}]}`,
		},
		{
			name:       "an empty context and name",
			body:       `{"contextName":"","name":""}`,
			wantStatus: http.StatusBadRequest,
			wantBody: `{"violations":[` +
				`{"field":"context_name","description":"context_name is required"},` +
				`{"field":"name","description":"name is required"}]}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, body := call(t, stack.url, http.MethodPost, "/v1/clusters", c.body)
			if code != c.wantStatus {
				t.Fatalf("status %d, want %d: %s", code, c.wantStatus, body)
			}
			if got := string(compact(t, body)); got != c.wantBody {
				t.Errorf("body %s, want %s", got, c.wantBody)
			}
		})
	}
}

// Rename, add-namespace and remove-namespace all answer the state after the
// call is the state asked for, unchanged on a repeat, and the scope list
// agrees.
func TestClusterWritesAreIdempotentAndAnswerTheRow(t *testing.T) {
	renamed := seededClustersWire()[0]
	renamed.Name = "production"
	namespaceAdded := seededClustersWire()[1]
	namespaceAdded.Namespaces = []string{seedNS, "payments", "staging-web"}
	namespaceRemoved := seededClustersWire()[1]
	namespaceRemoved.Namespaces = []string{seedNS}

	cases := []struct {
		name  string
		index int
		want  *idiosv1.Cluster
		call  func(context.Context, idiosv1.IdiosServiceClient) (*idiosv1.Cluster, error)
	}{
		{
			name: "rename prod", index: 0, want: renamed,
			call: func(ctx context.Context, c idiosv1.IdiosServiceClient) (*idiosv1.Cluster, error) {
				return c.RenameCluster(ctx, &idiosv1.RenameClusterRequest{Id: querytest.ClusterProd, Name: "production"})
			},
		},
		{
			name: "add namespace to staging", index: 1, want: namespaceAdded,
			call: func(ctx context.Context, c idiosv1.IdiosServiceClient) (*idiosv1.Cluster, error) {
				return c.AddWatchedNamespace(ctx, &idiosv1.AddWatchedNamespaceRequest{Id: querytest.ClusterStaging, Name: "payments"})
			},
		},
		{
			name: "remove namespace from staging", index: 1, want: namespaceRemoved,
			call: func(ctx context.Context, c idiosv1.IdiosServiceClient) (*idiosv1.Cluster, error) {
				return c.RemoveWatchedNamespace(ctx, &idiosv1.RemoveWatchedNamespaceRequest{Id: querytest.ClusterStaging, Name: "staging-web"})
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := newTestServer(t)
			ctx := context.Background()
			for i := range 2 {
				got, err := c.call(ctx, client)
				if err != nil {
					t.Fatalf("call %d: %v", i, err)
				}
				if diff := cmp.Diff(c.want, got, protocmp.Transform()); diff != "" {
					t.Errorf("call %d: -want +got\n%s", i, diff)
				}
			}
			listWant := seededClustersWire()
			listWant[c.index] = c.want
			listGot, err := client.ListClusters(ctx, &idiosv1.ListClustersRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(&idiosv1.ClustersResponse{Clusters: listWant}, listGot, protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// Every cluster endpoint that names an id answers 404 the same way when the
// id is not one of the stored rows.
func TestClusterWritesSayWhenTheIdIsUnknown(t *testing.T) {
	stack := newTestStack(t)
	const wantBody = `{"message":"cluster 999 not found"}`

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"rename", http.MethodPatch, "/v1/clusters/999", `{"name":"x"}`},
		{"add namespace", http.MethodPost, "/v1/clusters/999/namespaces", `{"name":"payments"}`},
		{"remove namespace", http.MethodDelete, "/v1/clusters/999/namespaces/staging-web", ""},
		{"set grafana", http.MethodPatch, "/v1/clusters/999/grafana", `{"grafanaUrl":"","lokiDatasourceUid":"","logSelector":""}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, body := call(t, stack.url, c.method, c.path, c.body)
			if code != http.StatusNotFound {
				t.Fatalf("status %d, want %d: %s", code, http.StatusNotFound, body)
			}
			if got := string(compact(t, body)); got != wantBody {
				t.Errorf("body %s, want %s", got, wantBody)
			}
		})
	}
}

// Deleting a cluster removes its rows, by cascade, and its artifact
// directory, whether or not the cluster still existed to be deleted.
func TestDeleteClusterRemovesRowsAndDirectory(t *testing.T) {
	stack := newTestStack(t)
	client := idiosv1.NewIdiosServiceClient(stack.url)
	ctx := context.Background()

	writeArtifactFile(t, stack.artifactsRoot, "1/idios-smoke/pod-crash/x.log", "log")
	writeArtifactFile(t, stack.artifactsRoot, "2/idios-smoke/config-pod/x.log", "log")

	for i := range 2 {
		got, err := client.DeleteCluster(ctx, &idiosv1.DeleteClusterRequest{Id: querytest.ClusterProd})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if diff := cmp.Diff(&idiosv1.DeleteClusterResponse{}, got, protocmp.Transform()); diff != "" {
			t.Errorf("call %d: -want +got\n%s", i, diff)
		}
	}

	listGot, err := client.ListClusters(ctx, &idiosv1.ListClustersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(&idiosv1.ClustersResponse{Clusters: seededClustersWire()[1:]}, listGot, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}

	incidents, err := client.ListIncidents(ctx, &idiosv1.ListIncidentsRequest{Limit: listLimitAboveSeed})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range incidents.GetIncidents() {
		if row.GetClusterId() != querytest.ClusterStaging {
			t.Errorf("incident %d belongs to cluster %d, want only %d", row.GetId(), row.GetClusterId(), querytest.ClusterStaging)
		}
	}

	if _, err := os.Stat(filepath.Join(stack.artifactsRoot, "1")); !os.IsNotExist(err) {
		t.Errorf("cluster 1 directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stack.artifactsRoot, "2")); err != nil {
		t.Errorf("cluster 2 directory missing: %v", err)
	}
}

// A cluster write's row reaches the cluster stream the same way a runtime
// state change does.
func TestClusterWritesEmitOnTheStream(t *testing.T) {
	events := notify.New()
	stack := newTestStack(t, withNotifier(events), withThrottle(0))
	client := idiosv1.NewIdiosServiceClient(stack.url)
	rows := openStream(t, func() (*idiosv1.IdiosServiceEventStream[*idiosv1.Cluster], error) {
		return client.StreamClusters(context.Background(), &idiosv1.StreamClustersRequest{})
	}, func() *idiosv1.Cluster { return &idiosv1.Cluster{} })

	resp, err := client.RenameCluster(context.Background(),
		&idiosv1.RenameClusterRequest{Id: querytest.ClusterProd, Name: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(resp, recv(t, rows), protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}

// A non-empty grafana_url triggers the other two checks; each invalid field
// is reported on its own, and all three together when every field is wrong.
func TestSetClusterGrafanaValidatesTheRequest(t *testing.T) {
	stack := newTestStack(t)
	cases := []struct {
		name     string
		body     string
		wantBody string
	}{
		{
			name:     "grafana_url not absolute http(s)",
			body:     `{"grafanaUrl":"not-a-url","lokiDatasourceUid":"grafanacloud-logs","logSelector":"{pod=\"$pod\"}"}`,
			wantBody: `{"violations":[{"field":"grafana_url","description":"grafana_url must be an absolute http or https URL"}]}`,
		},
		{
			name:     "loki_datasource_uid empty",
			body:     `{"grafanaUrl":"https://logs.example.grafana.net","lokiDatasourceUid":"","logSelector":"{pod=\"$pod\"}"}`,
			wantBody: `{"violations":[{"field":"loki_datasource_uid","description":"loki_datasource_uid is required"}]}`,
		},
		{
			name:     "log_selector missing $pod",
			body:     `{"grafanaUrl":"https://logs.example.grafana.net","lokiDatasourceUid":"grafanacloud-logs","logSelector":"{namespace=\"$namespace\"}"}`,
			wantBody: `{"violations":[{"field":"log_selector","description":"log_selector is required and must contain $pod"}]}`,
		},
		{
			name: "every field wrong at once",
			body: `{"grafanaUrl":"not-a-url","lokiDatasourceUid":"","logSelector":""}`,
			wantBody: `{"violations":[` +
				`{"field":"grafana_url","description":"grafana_url must be an absolute http or https URL"},` +
				`{"field":"loki_datasource_uid","description":"loki_datasource_uid is required"},` +
				`{"field":"log_selector","description":"log_selector is required and must contain $pod"}]}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, body := call(t, stack.url, http.MethodPatch, "/v1/clusters/1/grafana", c.body)
			if code != http.StatusBadRequest {
				t.Fatalf("status %d, want %d: %s", code, http.StatusBadRequest, body)
			}
			if got := string(compact(t, body)); got != c.wantBody {
				t.Errorf("body %s, want %s", got, c.wantBody)
			}
		})
	}
}

// Setting a configuration answers the cluster with the three fields filled;
// clearing it with an empty grafana_url blanks all three and skips the
// other checks, and both calls are idempotent.
func TestSetClusterGrafanaSetsThenClears(t *testing.T) {
	client := newTestServer(t)
	ctx := context.Background()

	configured := seededClustersWire()[0]
	configured.GrafanaUrl = "https://logs.example.grafana.net"
	configured.LokiDatasourceUid = "grafanacloud-logs"
	configured.LogSelector = `{namespace="$namespace", pod="$pod"}`
	for i := range 2 {
		got, err := client.SetClusterGrafana(ctx, &idiosv1.SetClusterGrafanaRequest{
			Id: querytest.ClusterProd, GrafanaUrl: configured.GrafanaUrl,
			LokiDatasourceUid: configured.LokiDatasourceUid, LogSelector: configured.LogSelector,
		})
		if err != nil {
			t.Fatalf("set call %d: %v", i, err)
		}
		if diff := cmp.Diff(configured, got, protocmp.Transform()); diff != "" {
			t.Errorf("set call %d: -want +got\n%s", i, diff)
		}
	}

	cleared := seededClustersWire()[0]
	for i := range 2 {
		got, err := client.SetClusterGrafana(ctx, &idiosv1.SetClusterGrafanaRequest{Id: querytest.ClusterProd})
		if err != nil {
			t.Fatalf("clear call %d: %v", i, err)
		}
		if diff := cmp.Diff(cleared, got, protocmp.Transform()); diff != "" {
			t.Errorf("clear call %d: -want +got\n%s", i, diff)
		}
	}
}
