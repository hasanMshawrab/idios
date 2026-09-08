package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The captured pod object is the one surface that carries environment
// values, so no call may hand them back: what read_pod_json returns is the
// sanitized copy, never the captured bytes.
func TestReadPodJSONReturnsTheSanitizedCopy(t *testing.T) {
	raw, err := os.ReadFile("../sanitize/testdata/pod.json")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("../sanitize/testdata/pod_sanitized.json")
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, golden); err != nil {
		t.Fatal(err)
	}
	s := New(fakeDaemon(t, `{"id":"92","podUid":"pod-crash","kind":"pod_json","restartCount":-1,`+
		`"capturedAt":"2026-08-27T14:38:05.000000Z","filePath":"prod/pod-crash/pod.json"}`, string(raw)), "test")

	res, _, err := s.readPodJSON(context.Background(), nil, artifactArgs{ArtifactID: 92})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("read_pod_json returned %T, want text", res.Content[0])
	}
	if got.Text != want.String() {
		t.Errorf("read_pod_json =\n%s\nwant\n%s", got.Text, want.String())
	}
}

// fakeDaemon serves one artifact row and its captured bytes, which is
// everything the artifact tools ask a daemon for.
func fakeDaemon(t *testing.T, row, content string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/artifacts/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(row))
	})
	mux.HandleFunc("GET /v1/artifacts/{id}/content", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(content))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}
