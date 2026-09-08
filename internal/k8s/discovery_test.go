package k8s

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const twoContexts = `apiVersion: v1
kind: Config
current-context: b-staging
clusters:
- name: prod
  cluster:
    server: https://prod.example.invalid:6443
- name: staging
  cluster:
    server: https://staging.example.invalid:6443
contexts:
- name: b-staging
  context:
    cluster: staging
    user: dev
- name: a-prod
  context:
    cluster: prod
    user: dev
- name: c-dangling
  context:
    cluster: deleted
    user: dev
users:
- name: dev
  user:
    token: secret-token
`

func TestListContextsNamesClusterAndServerOnly(t *testing.T) {
	got, err := ListContexts(writeConfig(t, twoContexts))
	if err != nil {
		t.Fatal(err)
	}
	want := []KubeContext{
		{Name: "a-prod", Cluster: "prod", Server: "https://prod.example.invalid:6443"},
		{Name: "b-staging", Cluster: "staging", Server: "https://staging.example.invalid:6443"},
		{Name: "c-dangling", Cluster: "deleted"},
	}
	diff(t, want, got)
}

func TestListNamespacesReportsForbiddenRatherThanFailing(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		body          string
		want          []string
		wantForbidden bool
		wantErr       bool
	}{
		{"a Role that may not list says so", http.StatusForbidden,
			`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`, nil, true, false},
		{"names come back sorted", http.StatusOK,
			`{"kind":"NamespaceList","apiVersion":"v1","items":[{"metadata":{"name":"prod"}},{"metadata":{"name":"default"}}]}`,
			[]string{"default", "prod"}, false, false},
		{"any other refusal is an error", http.StatusInternalServerError,
			`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"InternalError","code":500}`, nil, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			names, forbidden, err := ListNamespaces(context.Background(), writeKubeconfig(t, srv.URL), "orbstack")
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error: %v", err, c.wantErr)
			}
			diff(t, [2]any{c.want, c.wantForbidden}, [2]any{names, forbidden})
		})
	}
}
