package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// testListLimit is the configured default the seed can exceed, so a request
// that sends no limit is told apart from one that sends the seed's size.
const testListLimit = 3

// testNow is what the server's clock reports, whatever the seed's clock did.
const testNow = "2026-08-27T15:30:00.000000Z"

// testStack is one running server over the seeded database, with the parts a
// test reaches past the generated client for: the store to add rows to, the
// configuration the handlers read paths and limits from, and the base URL.
type testStack struct {
	store         *store.Store
	writer        *store.Writer
	artifactsRoot string
	cfg           config.Config
	url           string
	server        *Server
}

// testOption tunes the server one test serves.
type testOption func(*config.Config, *Server)

// withRuntime gives the served server the process facts r reports.
func withRuntime(r Runtime) testOption {
	return func(_ *config.Config, s *Server) { s.WithRuntime(r) }
}

// withKube gives the served server a kubeconfig reader.
func withKube(k KubeDiscovery) testOption {
	return func(_ *config.Config, s *Server) { s.WithKube(k) }
}

// withNotifier gives the served server the broadcast its streams read.
func withNotifier(n *notify.Notifier) testOption {
	return func(_ *config.Config, s *Server) { s.WithNotifier(n) }
}

// withThrottle sets how long a stream collapses a burst on one row id.
func withThrottle(d time.Duration) testOption {
	return func(cfg *config.Config, _ *Server) { cfg.APIStreamThrottle = d }
}

// newTestStack serves the seeded database over loopback.
func newTestStack(t *testing.T, opts ...testOption) testStack {
	t.Helper()
	st, _ := querytest.Seed(t)
	cfg := config.Default()
	cfg.APIListLimit = testListLimit
	// The default data directory is the user's own; every path the handlers
	// report sizes for has to be this test's.
	cfg.DataDir = t.TempDir()
	cfg.ArtifactsRoot = t.TempDir()
	at, err := clock.Parse(testNow)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st.Reader.DB(), cfg, clock.NewFake(at), slog.New(slog.DiscardHandler)).WithWriter(st.Writer)
	for _, opt := range opts {
		opt(&cfg, srv)
	}
	srv.cfg = cfg
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return testStack{
		store: st, writer: st.Writer, artifactsRoot: cfg.ArtifactsRoot, cfg: cfg, url: httpSrv.URL, server: srv,
	}
}

// newTestServer returns a client of the generated contract for the seeded
// database.
func newTestServer(t *testing.T, opts ...testOption) idiosv1.IdiosServiceClient {
	t.Helper()
	return idiosv1.NewIdiosServiceClient(newTestStack(t, opts...).url)
}

// get reads path from the server, returning the status, the headers and the
// body. The generated client hides the status code, so a test about one asks
// over plain HTTP.
func get(t *testing.T, base, path string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+path, nil)
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
	return resp.StatusCode, resp.Header, body
}

// call sends method to path on base with body as the request body, returning
// the status, the headers and the response body. A write endpoint's status
// is asserted this way for the same reason get is: the generated client
// hides it.
func call(t *testing.T, base, method, path, body string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, respBody
}
