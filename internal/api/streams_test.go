package api

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// The two timings the throttle test depends on. The burst it fires has to
// land inside one window, so the window is far longer than five notifies and
// one UPDATE need; and settle has to outlast a window, or a trailing send
// still to come would read as silence. Raising streamThrottle raises settle
// with it.
const (
	streamThrottle = 200 * time.Millisecond
	settle         = 3 * streamThrottle
)

// streamWait is how long a test waits for a row the server should already be
// sending, far above what loopback and a temp-file database need.
const streamWait = 5 * time.Second

// streamRows reads the stream into a channel, closed when the server ends it.
// Next blocks, so the rows have to arrive on their own goroutine for a test
// to be able to time out instead of hanging.
func streamRows[T proto.Message](s *idiosv1.IdiosServiceEventStream[T], fresh func() T) <-chan T {
	out := make(chan T, 64)
	go func() {
		defer close(out)
		for {
			row := fresh()
			if !s.Next(row) {
				return
			}
			out <- row
		}
	}()
	return out
}

// recv takes the next row, failing the test when none arrives.
func recv[T proto.Message](t *testing.T, rows <-chan T) T {
	t.Helper()
	select {
	case row, ok := <-rows:
		if !ok {
			t.Fatal("stream ended")
		}
		return row
	case <-time.After(streamWait):
		t.Fatal("no row within " + streamWait.String())
	}
	var zero T
	return zero
}

// quiet fails the test when another row arrives.
func quiet[T proto.Message](t *testing.T, rows <-chan T) {
	t.Helper()
	select {
	case row := <-rows:
		t.Fatalf("unexpected row %v", row)
	case <-time.After(settle):
	}
}

// openStream opens an SSE stream and returns the rows it sends. The handler
// flushes the response head after subscribing, and the client's call returns
// on that head, so a notify made after this returns cannot be missed: the
// broadcast has no replay and an event sent before the subscription would be
// lost.
func openStream[T proto.Message](t *testing.T,
	open func() (*idiosv1.IdiosServiceEventStream[T], error), fresh func() T,
) <-chan T {
	t.Helper()
	stream, err := open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	return streamRows(stream, fresh)
}

// commit runs one statement against the seeded database, standing for what a
// recorder or a human action commits before it notifies.
func commit(t *testing.T, st *store.Store, statement string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, statement, args...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// listedIncident is the row the triage list shows for id right now, which is
// what the stream promises to send.
func listedIncident(t *testing.T, client idiosv1.IdiosServiceClient, id int64) *idiosv1.IncidentRow {
	t.Helper()
	resp, err := client.ListIncidents(context.Background(), &idiosv1.ListIncidentsRequest{
		PodUid: querytest.CrashPodUID, Limit: listLimitAboveSeed,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range resp.GetIncidents() {
		if row.GetId() == id {
			return row
		}
	}
	t.Fatalf("incident %d is not in the list", id)
	return nil
}

// incidentStream opens the incident stream and returns the rows it sends.
func incidentStream(t *testing.T, client idiosv1.IdiosServiceClient, clusterIDs []int64) <-chan *idiosv1.IncidentRow {
	t.Helper()
	return openStream(t, func() (*idiosv1.IdiosServiceEventStream[*idiosv1.IncidentRow], error) {
		return client.StreamIncidents(context.Background(), &idiosv1.StreamIncidentsRequest{ClusterIds: clusterIDs})
	}, func() *idiosv1.IncidentRow { return &idiosv1.IncidentRow{} })
}

// Every event carries the whole row as it stands when it is sent, so the
// client replaces by id and never reconciles a diff.
func TestIncidentStreamSendsWholeRowsInOrder(t *testing.T) {
	const id = int64(1)
	events := notify.New()
	stack := newTestStack(t, withNotifier(events), withThrottle(0))
	client := idiosv1.NewIdiosServiceClient(stack.url)
	rows := incidentStream(t, client, nil)

	steps := []struct {
		name      string
		statement string
	}{
		{"a notify on an unchanged row re-sends it", ""},
		{"an update bumping occurrences", `UPDATE incidents SET occurrences = occurrences + 1, last_reason = 'Error' WHERE id = ?`},
		{"a close", `UPDATE incidents SET closed_at = '2026-08-27T15:00:00.000000Z', close_reason = 'recovered' WHERE id = ?`},
		{"a reopen", `UPDATE incidents SET closed_at = NULL, close_reason = NULL, occurrences = occurrences + 1 WHERE id = ?`},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			if s.statement != "" {
				commit(t, stack.store, s.statement, id)
			}
			events.Notify(notify.Incident, id)
			want := listedIncident(t, client, id)
			if diff := cmp.Diff(want, recv(t, rows), protocmp.Transform()); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}

// A crash loop can change one incident many times a second. The first change
// of a window goes out at once, the rest collapse into one trailing send of
// the row as it then stands, and another incident in the same window waits
// for nothing: the window is per incident, not per connection.
func TestIncidentStreamThrottlesPerId(t *testing.T) {
	const (
		busy  = int64(1)
		other = int64(12)
	)
	events := notify.New()
	stack := newTestStack(t, withNotifier(events), withThrottle(streamThrottle))
	client := idiosv1.NewIdiosServiceClient(stack.url)
	rows := incidentStream(t, client, nil)

	commit(t, stack.store, `UPDATE incidents SET occurrences = 99, last_reason = 'Error' WHERE id = ?`, busy)
	events.Notify(notify.Incident, busy)
	leading := recv(t, rows)
	if leading.GetId() != busy || leading.GetOccurrences() != 99 {
		t.Fatalf("leading row is %d with %d occurrences", leading.GetId(), leading.GetOccurrences())
	}

	commit(t, stack.store, `UPDATE incidents SET occurrences = 105 WHERE id = ?`, busy)
	for range 5 {
		events.Notify(notify.Incident, busy)
	}
	events.Notify(notify.Incident, other)

	// The other incident's first change is not held behind the busy one's
	// window, so it arrives before the trailing send.
	if got := recv(t, rows); got.GetId() != other {
		t.Fatalf("row %d arrived before the other incident's", got.GetId())
	}
	trailing := recv(t, rows)
	if trailing.GetId() != busy || trailing.GetOccurrences() != 105 {
		t.Fatalf("trailing row is %d with %d occurrences", trailing.GetId(), trailing.GetOccurrences())
	}
	quiet(t, rows)
}

// The stream is scoped by the selected clusters like every list, so an
// incident outside them is not sent at all.
func TestIncidentStreamHonoursClusterScope(t *testing.T) {
	const (
		prodIncident    = int64(1)
		stagingIncident = int64(8)
	)
	events := notify.New()
	stack := newTestStack(t, withNotifier(events), withThrottle(0))
	client := idiosv1.NewIdiosServiceClient(stack.url)
	rows := incidentStream(t, client, []int64{querytest.ClusterStaging})

	events.Notify(notify.Incident, prodIncident)
	quiet(t, rows)
	events.Notify(notify.Incident, stagingIncident)
	if got := recv(t, rows); got.GetId() != stagingIncident {
		t.Errorf("row %d, want %d", got.GetId(), stagingIncident)
	}
}

// A cluster event sends the same merged row the scope list shows, so the
// window that is watching it replaces one line and reloads nothing.
func TestClusterStreamSendsMergedRow(t *testing.T) {
	const lastEvent = "2026-08-27T15:29:00.000000Z"
	events := notify.New()
	runtime := fakeRuntime{snap: statusRuntime().snap}
	stack := newTestStack(t, withNotifier(events), withThrottle(0), withRuntime(runtime))
	client := idiosv1.NewIdiosServiceClient(stack.url)
	rows := openStream(t, func() (*idiosv1.IdiosServiceEventStream[*idiosv1.Cluster], error) {
		return client.StreamClusters(context.Background(), &idiosv1.StreamClustersRequest{})
	}, func() *idiosv1.Cluster { return &idiosv1.Cluster{} })

	events.Notify(notify.Cluster, querytest.ClusterProd)
	want := seededClustersWire()[0]
	want.Ready, want.LastEventAt, want.SkewSeconds = true, sp(lastEvent), 1.5
	if diff := cmp.Diff(want, recv(t, rows), protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}

// Shutdown does not cancel a stream handler's request context, so every
// stream selects on a server-wide channel that stopping the server closes;
// without it a client holding a stream would never see the connection end.
func TestStreamsEndWhenTheServerShutsDown(t *testing.T) {
	events := notify.New()
	stack := newTestStack(t, withNotifier(events), withThrottle(0))
	client := idiosv1.NewIdiosServiceClient(stack.url)
	incidents := incidentStream(t, client, nil)
	clusters := openStream(t, func() (*idiosv1.IdiosServiceEventStream[*idiosv1.Cluster], error) {
		return client.StreamClusters(context.Background(), &idiosv1.StreamClustersRequest{})
	}, func() *idiosv1.Cluster { return &idiosv1.Cluster{} })

	stack.server.stopStreams()

	select {
	case _, ok := <-incidents:
		if ok {
			t.Fatal("incident stream sent a row instead of closing")
		}
	case <-time.After(streamWait):
		t.Fatal("incident stream did not close within " + streamWait.String())
	}
	select {
	case _, ok := <-clusters:
		if ok {
			t.Fatal("cluster stream sent a row instead of closing")
		}
	case <-time.After(streamWait):
		t.Fatal("cluster stream did not close within " + streamWait.String())
	}
}

// freeLoopbackAddr hands Run a port nothing else is using, the way the OS
// would if APIListen asked for one.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// Run's Shutdown waits out shutdownGrace only for a request-and-response
// handler still writing a body; a stream unblocks on stopStreams instead, so
// a client holding one open must not make Run wait out the whole grace
// period before returning.
func TestRunReturnsPromptlyWithAStreamOpen(t *testing.T) {
	st, _ := querytest.Seed(t)
	cfg := config.Default()
	cfg.APIListen = freeLoopbackAddr(t)
	at, err := clock.Parse(testNow)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st.Reader.DB(), cfg, clock.NewFake(at), slog.New(slog.DiscardHandler)).WithNotifier(notify.New())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(ctx) }()

	base := "http://" + cfg.APIListen
	deadline := time.Now().Add(streamWait)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/status", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not answer /v1/status within %s: %v", streamWait, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	client := idiosv1.NewIdiosServiceClient(base)
	stream, err := client.StreamIncidents(context.Background(), &idiosv1.StreamIncidentsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })

	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return within one second")
	}

	if stream.Next(&idiosv1.IncidentRow{}) {
		t.Fatal("stream.Next returned true after the server shut down")
	}
}
