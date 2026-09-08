package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/hasanMshawrab/idios/internal/api/mock"
)

// The application is built against the mock before a daemon has data, so the
// command has to serve the endpoint that is outside the proto contract as
// well as the generated ones, and stop when the process is asked to.
func TestMockServesArtifactContentUntilCancelled(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serveMock(ctx, ln) }()

	url := "http://" + ln.Addr().String() + "/v1/artifacts/1/content"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("content type %q, want %q", got, "text/plain; charset=utf-8")
	}
	if string(body) != mock.Content {
		t.Errorf("body %q, want %q", body, mock.Content)
	}

	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serveMock returned %v, want nil", err)
	}
}

// The prompt endpoint is outside the proto contract too, so the mock has to
// serve it for the application's Ask AI actions to work against the fixtures,
// and to refuse a mode that is neither of the two.
func TestMockServesPrompts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- serveMock(ctx, ln) }()

	cases := []struct {
		name string
		mode string
		code int
		want string
	}{
		{"the short prompt", "mcp", http.StatusOK, "mock prompt: mode mcp\n"},
		{"the snapshot", "snapshot", http.StatusOK, "mock prompt: mode snapshot\n"},
		{"a mode that is neither", "brief", http.StatusBadRequest, "mode must be mcp or snapshot\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			url := "http://" + ln.Addr().String() + "/v1/incidents/1/prompt?mode=" + c.mode
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != c.code {
				t.Errorf("status %d, want %d", resp.StatusCode, c.code)
			}
			if string(body) != c.want {
				t.Errorf("body %q, want %q", body, c.want)
			}
		})
	}

	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serveMock returned %v, want nil", err)
	}
}
