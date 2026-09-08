package incident

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/store"
)

func TestTickClosesStableIncidentsThenAttachesTheirLateEvents(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now)
	s, err := store.Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(ctx, clk); err != nil {
		t.Fatal(err)
	}
	nowS := clock.Format(now)
	stale := clock.Format(now.Add(-11 * time.Minute))
	fresh := clock.Format(now.Add(-1 * time.Minute))
	var recoveredID, stillOpenID int64
	err = s.Writer.Tx(ctx, func(tx *sql.Tx) error {
		stmts := []struct {
			q    string
			args []any
		}{
			{"INSERT INTO clusters (id, identity, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'ctx', 'u', ?)", []any{nowS}},
			{`INSERT INTO pods (uid, cluster_id, namespace, name, node_name, phase, created_at, first_seen_at, last_seen_at)
VALUES ('p1', 1, 'idios-smoke', 'pod', 'n1', 'Running', ?, ?, ?)`, []any{stale, stale, nowS}},
			{`INSERT INTO containers (pod_uid, name, kind, image, state, ready, updated_at) VALUES ('p1', 'api', 'app', 'img', 'running', 1, ?)`, []any{nowS}},
			{`INSERT INTO containers (pod_uid, name, kind, image, state, ready, updated_at) VALUES ('p1', 'web', 'app', 'img', 'waiting', 0, ?)`, []any{nowS}},
			{`INSERT INTO incidents (id, cluster_id, namespace, subject_kind, pod_uid, container_name, category, first_reason, last_reason, opened_at, last_seen_at)
VALUES (10, 1, 'idios-smoke', 'pod', 'p1', 'api', 'crash', 'Error', 'CrashLoopBackOff', ?, ?)`, []any{stale, stale}},
			{`INSERT INTO incidents (id, cluster_id, namespace, subject_kind, pod_uid, container_name, category, first_reason, last_reason, opened_at, last_seen_at)
VALUES (11, 1, 'idios-smoke', 'pod', 'p1', 'web', 'image_pull', 'ErrImagePull', 'ImagePullBackOff', ?, ?)`, []any{stale, fresh}},
			{`INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, first_ts, last_ts, raw_json)
VALUES (1, 'e1', 'idios-smoke', 'Normal', 'Pod', 'pod', 'p1', 'spec.containers{api}', 'Killing', ?, ?, '{}')`, []any{nowS, nowS}},
			{`INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, first_ts, last_ts, raw_json)
VALUES (1, 'e2', 'idios-smoke', 'Warning', 'Pod', 'pod', 'p1', 'spec.containers{web}', 'BackOff', ?, ?, '{}')`, []any{nowS, nowS}},
		}
		for _, st := range stmts {
			if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	recoveredID, stillOpenID = 10, 11

	c := NewCloser(s.Writer, clk, 10*time.Minute, 10*time.Minute, 30*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := &recorder{}
	c.SetNotifier(rec)
	if at, _ := c.Last(); !at.IsZero() {
		t.Fatal("Last() set before any tick")
	}
	got, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(TickResult{Closed: 1, Attached: 1}, got); d != "" {
		t.Fatal(d)
	}
	if at, last := c.Last(); !at.Equal(now) || last != got {
		t.Fatalf("Last() = %v, %+v; want %v, %+v", at, last, now, got)
	}

	type incidentState struct {
		ID          int64
		ClosedAt    *string
		CloseReason *string
	}
	type eventState struct {
		UID        string
		IncidentID *int64
	}
	var incs []incidentState
	var evs []eventState
	rows, err := s.Reader.DB().QueryContext(ctx, "SELECT id, closed_at, close_reason FROM incidents ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var i incidentState
		if err := rows.Scan(&i.ID, &i.ClosedAt, &i.CloseReason); err != nil {
			t.Fatal(err)
		}
		incs = append(incs, i)
	}
	_ = rows.Close()
	rows, err = s.Reader.DB().QueryContext(ctx, "SELECT event_uid, incident_id FROM k8s_events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var e eventState
		if err := rows.Scan(&e.UID, &e.IncidentID); err != nil {
			t.Fatal(err)
		}
		evs = append(evs, e)
	}
	_ = rows.Close()
	recovered := store.CloseRecovered
	wantIncs := []incidentState{{recoveredID, &nowS, &recovered}, {stillOpenID, nil, nil}}
	wantEvs := []eventState{{"e1", &recoveredID}, {"e2", nil}}
	if d := cmp.Diff(wantIncs, incs); d != "" {
		t.Fatal(d)
	}
	if d := cmp.Diff(wantEvs, evs); d != "" {
		t.Fatal(d)
	}
	if d := cmp.Diff([]notify.Event{{Kind: notify.Incident, ID: recoveredID}}, rec.take()); d != "" {
		t.Fatal(d)
	}

	again, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(TickResult{}, again); d != "" {
		t.Fatalf("second tick is not idempotent: %s", d)
	}
	if d := cmp.Diff([]notify.Event(nil), rec.take()); d != "" {
		t.Fatalf("a tick that closed nothing notified: %s", d)
	}
}

// A status line reading "closed 0" after an hour of work is the last tick
// alone; the totals are what says the closer has done anything.
func TestTotalsAccumulateAcrossTicks(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	s, err := store.Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(ctx, clk); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if err := s.Writer.Tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, q, args...)
			return err
		}); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	nowS := clock.Format(start)
	stale := clock.Format(start.Add(-11 * time.Minute))
	fresh := clock.Format(start.Add(-1 * time.Minute))
	exec("INSERT INTO clusters (id, identity, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'ctx', 'u', ?)", nowS)
	exec(`INSERT INTO pods (uid, cluster_id, namespace, name, node_name, phase, created_at, first_seen_at, last_seen_at)
VALUES ('p1', 1, 'idios-smoke', 'pod', 'n1', 'Running', ?, ?, ?)`, stale, stale, nowS)
	for _, name := range []string{"api", "web"} {
		exec(`INSERT INTO containers (pod_uid, name, kind, image, state, ready, updated_at) VALUES ('p1', ?, 'app', 'img', 'running', 1, ?)`, name, nowS)
	}
	exec(`INSERT INTO incidents (id, cluster_id, namespace, subject_kind, pod_uid, container_name, category, first_reason, last_reason, opened_at, last_seen_at)
VALUES (10, 1, 'idios-smoke', 'pod', 'p1', 'api', 'crash', 'Error', 'CrashLoopBackOff', ?, ?)`, stale, stale)
	exec(`INSERT INTO incidents (id, cluster_id, namespace, subject_kind, pod_uid, container_name, category, first_reason, last_reason, opened_at, last_seen_at)
VALUES (11, 1, 'idios-smoke', 'pod', 'p1', 'web', 'crash', 'Error', 'CrashLoopBackOff', ?, ?)`, stale, fresh)
	exec(`INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, first_ts, last_ts, raw_json)
VALUES (1, 'e1', 'idios-smoke', 'Normal', 'Pod', 'pod', 'p1', 'spec.containers{api}', 'Killing', ?, ?, '{}')`, nowS, nowS)

	c := NewCloser(s.Writer, clk, 10*time.Minute, 10*time.Minute, 30*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	first, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(10 * time.Minute)
	later := clock.Format(clk.Now())
	exec(`INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, first_ts, last_ts, raw_json)
VALUES (1, 'e2', 'idios-smoke', 'Normal', 'Pod', 'pod', 'p1', 'spec.containers{web}', 'Killing', ?, ?, '{}')`, later, later)
	second, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}

	one := TickResult{Closed: 1, Attached: 1}
	if d := cmp.Diff([]TickResult{one, one}, []TickResult{first, second}); d != "" {
		t.Fatal(d)
	}
	at, last := c.Last()
	if !at.Equal(clk.Now()) {
		t.Errorf("Last() ran at %v, want %v", at, clk.Now())
	}
	if d := cmp.Diff(one, last); d != "" {
		t.Errorf("Last() is not the second tick alone: %s", d)
	}
	if d := cmp.Diff(TickResult{Closed: 2, Attached: 2}, c.Totals()); d != "" {
		t.Error(d)
	}
}

// An incident opened by a delivery trailing the pod's delete slips past the
// delete path's close; the tick is what catches it. The seeded container is
// an init that exited 0, the one shape a deleted pod can present that the
// recovered close would also take, so this pins that pod_deleted wins.
func TestTickClosesIncidentsOnDeletedPods(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now)
	s, err := store.Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(ctx, clk); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if err := s.Writer.Tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, q, args...)
			return err
		}); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	nowS := clock.Format(now)
	deletedAt := clock.Format(now.Add(-30 * time.Minute))
	opened := clock.Format(now.Add(-30*time.Minute + 3*time.Second))
	stale := clock.Format(now.Add(-11 * time.Minute))
	exec("INSERT INTO clusters (id, identity, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'ctx', 'u', ?)", nowS)
	exec(`INSERT INTO pods (uid, cluster_id, namespace, name, node_name, phase, created_at, first_seen_at, last_seen_at, deleted_at, deletion_source)
VALUES ('p1', 1, 'idios-smoke', 'pod', 'n1', 'Succeeded', ?, ?, ?, ?, 'watch')`, deletedAt, deletedAt, deletedAt, deletedAt)
	exec(`INSERT INTO containers (pod_uid, name, kind, image, state, ready, exit_code, updated_at) VALUES ('p1', 'init-db', 'init', 'img', 'terminated', 1, 0, ?)`, deletedAt)
	exec(`INSERT INTO incidents (id, cluster_id, namespace, subject_kind, pod_uid, container_name, category, first_reason, last_reason, opened_at, last_seen_at)
VALUES (10, 1, 'idios-smoke', 'pod', 'p1', 'init-db', 'probe', 'Unhealthy', 'Unhealthy', ?, ?)`, opened, stale)

	c := NewCloser(s.Writer, clk, 10*time.Minute, 10*time.Minute, 30*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := &recorder{}
	c.SetNotifier(rec)
	got, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(TickResult{Closed: 1}, got); d != "" {
		t.Fatal(d)
	}
	inc := loadIncidents(t, s)[0]
	wantClose := []*string{ptr(opened), ptr(store.ClosePodDeleted)}
	if d := cmp.Diff(wantClose, []*string{inc.ClosedAt, inc.CloseReason}); d != "" {
		t.Fatal(d)
	}
	if d := cmp.Diff([]notify.Event{{Kind: notify.Incident, ID: 10}}, rec.take()); d != "" {
		t.Fatal(d)
	}

	again, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(TickResult{}, again); d != "" {
		t.Fatalf("second tick is not idempotent: %s", d)
	}
}

type recorder struct{ events []notify.Event }

func (r *recorder) Notify(k notify.Kind, id int64) {
	r.events = append(r.events, notify.Event{Kind: k, ID: id})
}

func (r *recorder) take() []notify.Event {
	out := r.events
	r.events = nil
	return out
}

// loadIncidents reads every incident row whole, so a test can compare the
// row the closer wrote against the row it meant to write.
func loadIncidents(t *testing.T, s *store.Store) []store.Incident {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), `
SELECT id, cluster_id, namespace, subject_kind, pod_uid, job_uid, container_name, workload_kind, workload_name,
       category, first_reason, last_reason, last_message, image, image_tag, image_id, occurrences,
       opened_at, last_seen_at, closed_at, close_reason, acknowledged_at, dismissed_at, note
FROM incidents ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.Incident
	for rows.Next() {
		var i store.Incident
		if err := rows.Scan(&i.ID, &i.ClusterID, &i.Namespace, &i.SubjectKind, &i.PodUID, &i.JobUID, &i.ContainerName,
			&i.WorkloadKind, &i.WorkloadName, &i.Category, &i.FirstReason, &i.LastReason, &i.LastMessage,
			&i.Image, &i.ImageTag, &i.ImageID, &i.Occurrences, &i.OpenedAt, &i.LastSeenAt, &i.ClosedAt,
			&i.CloseReason, &i.AcknowledgedAt, &i.DismissedAt, &i.Note); err != nil {
			t.Fatal(err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A pod waiting on a missing volume gets no event with resync 0, so the tick
// is the only thing that can find it; and the row it opens must name the
// container, or the closer could never close it as recovered.
func TestTickOpensStuckIncidents(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now)
	s, err := store.Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(ctx, clk); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if err := s.Writer.Tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, q, args...)
			return err
		}); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	nowS := clock.Format(now)
	created := clock.Format(now.Add(-time.Hour))
	exec("INSERT INTO clusters (id, identity, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'ctx', 'u', ?)", nowS)
	exec(`INSERT INTO pods (uid, cluster_id, namespace, name, phase, workload_kind, workload_name, created_at, first_seen_at, last_seen_at)
VALUES ('p1', 1, 'idios-smoke', 'pod', 'Pending', 'StatefulSet', 'db', ?, ?, ?)`, created, created, nowS)
	exec(`INSERT INTO containers (pod_uid, name, kind, image, state, reason, ready, updated_at)
VALUES ('p1', 'api', 'app', 'img', 'waiting', 'ContainerCreating', 0, ?)`, created)

	c := NewCloser(s.Writer, clk, 10*time.Minute, 10*time.Minute, 30*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := &recorder{}
	c.SetNotifier(rec)
	got, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(TickResult{Opened: 1}, got); d != "" {
		t.Fatal(d)
	}
	want := []store.Incident{{
		ID: 1, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr("p1"),
		ContainerName: "api", WorkloadKind: "StatefulSet", WorkloadName: "db", Category: store.CategoryStuck,
		FirstReason: "ContainerCreating", LastReason: "ContainerCreating", Occurrences: 1,
		OpenedAt: created, LastSeenAt: nowS,
	}}
	if d := cmp.Diff(want, loadIncidents(t, s)); d != "" {
		t.Fatal(d)
	}
	if d := cmp.Diff([]notify.Event{{Kind: notify.Incident, ID: 1}}, rec.take()); d != "" {
		t.Fatal(d)
	}

	again, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(TickResult{}, again); d != "" {
		t.Fatalf("a pod with the incident already open opened a second one: %s", d)
	}

	clk.Advance(20 * time.Minute)
	exec("UPDATE containers SET state = 'running', reason = NULL, ready = 1, updated_at = ? WHERE pod_uid = 'p1'", clock.Format(clk.Now()))
	closed, err := c.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(TickResult{Closed: 1}, closed); d != "" {
		t.Fatal(d)
	}
	want[0].ClosedAt, want[0].CloseReason = ptr(clock.Format(clk.Now())), ptr(store.CloseRecovered)
	if d := cmp.Diff(want, loadIncidents(t, s)); d != "" {
		t.Fatal(d)
	}
}
