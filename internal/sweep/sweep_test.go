package sweep

import (
	"context"
	"database/sql"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

const (
	oldTS    = "2026-08-20T12:00:00.000000Z"
	recentTS = "2026-08-26T12:00:00.000000Z"
	nowTS    = "2026-08-27T12:00:00.000000Z"
	cutoffTS = "2026-08-24T12:00:00.000000Z"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

type harness struct {
	s    *store.Store
	clk  *clock.Fake
	root string
	sw   *Sweeper
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clk := clock.NewFake(testNow)
	if err := s.Migrate(context.Background(), clk); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "artifacts")
	sw := New(Config{Root: root, Retention: 3 * 24 * time.Hour, Interval: time.Hour}, s.Writer, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &harness{s: s, clk: clk, root: root, sw: sw}
}

func (h *harness) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	err := h.s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
}

func (h *harness) strings(t *testing.T, q string) []string {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (h *harness) sweepRuns(t *testing.T) []store.SweepRun {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT ran_at, cutoff, table_name, rows_removed, files_removed, bytes_removed, duration_ms, error FROM sweep_runs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.SweepRun
	for rows.Next() {
		var r store.SweepRun
		if err := rows.Scan(&r.RanAt, &r.Cutoff, &r.TableName, &r.RowsRemoved, &r.FilesRemoved, &r.BytesRemoved, &r.DurationMs, &r.Error); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// seedRows builds one database in which every retention rule has a row that
// goes and a row that stays.
func (h *harness) seedRows(t *testing.T) {
	t.Helper()
	x := func(q string, args ...any) { h.exec(t, q, args...) }
	x("INSERT INTO clusters (id, identity, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'ctx', 'u', ?)", oldTS)
	pod := `INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at, deleted_at, deletion_source)
VALUES (?, 1, 'idios-smoke', ?, 'Running', ?, ?, ?, ?, ?)`
	x(pod, "gone-old", "gone-old", oldTS, oldTS, oldTS, oldTS, "watch")
	x(pod, "gone-recent", "gone-recent", oldTS, oldTS, recentTS, recentTS, "watch")
	x(pod, "live", "live", oldTS, oldTS, nowTS, nil, nil)
	ctr := `INSERT INTO containers (pod_uid, name, kind, image, state, ready, updated_at) VALUES (?, 'api', 'app', 'img', 'running', 1, ?)`
	x(ctr, "gone-old", oldTS)
	x(ctr, "live", nowTS)
	inc := `INSERT INTO incidents (id, cluster_id, namespace, subject_kind, pod_uid, job_uid, container_name, category, first_reason, last_reason,
    opened_at, last_seen_at, closed_at, close_reason) VALUES (?, 1, 'idios-smoke', ?, ?, ?, 'api', 'crash', 'r', 'r', ?, ?, ?, ?)`
	x(inc, 10, "pod", "gone-old", nil, oldTS, oldTS, oldTS, "pod_deleted")
	x(inc, 20, "pod", "live", nil, oldTS, oldTS, oldTS, "recovered")
	x(inc, 21, "pod", "live", nil, oldTS, nowTS, nil, nil)
	x(inc, 22, "pod", "live", nil, oldTS, recentTS, recentTS, "recovered")
	hist := `INSERT INTO container_state_history (id, pod_uid, container_name, incident_id, image, state, restart_count, observed_at)
VALUES (?, ?, 'api', ?, 'img', 'waiting', 0, ?)`
	x(hist, 1, "gone-old", 10, oldTS)
	x(hist, 2, "live", 20, oldTS)
	x(hist, 3, "live", 21, oldTS)
	x(hist, 4, "live", 22, oldTS)
	x(hist, 5, "live", nil, oldTS)
	x(hist, 6, "live", nil, recentTS)
	cond := `INSERT INTO pod_condition_history (id, pod_uid, type, status, observed_at) VALUES (?, 'live', 'Ready', 'False', ?)`
	x(cond, 1, oldTS)
	x(cond, 2, recentTS)
	job := `INSERT INTO jobs (uid, cluster_id, namespace, name, created_at, first_seen_at, last_seen_at, deleted_at, finished_at)
VALUES (?, 1, 'idios-smoke', ?, ?, ?, ?, ?, ?)`
	x(job, "job-gone-old", "j", oldTS, oldTS, oldTS, oldTS, oldTS)
	x(job, "job-finished-gone", "j", oldTS, oldTS, recentTS, recentTS, oldTS)
	x(job, "job-live-old", "j", oldTS, oldTS, nowTS, nil, oldTS)
	x(`INSERT INTO incidents (id, cluster_id, namespace, subject_kind, job_uid, category, first_reason, last_reason, opened_at, last_seen_at, closed_at, close_reason)
VALUES (30, 1, 'idios-smoke', 'job', 'job-gone-old', 'job_failed', 'r', 'r', ?, ?, ?, 'job_finished')`, oldTS, recentTS, recentTS)
	rs := `INSERT INTO rollout_history (cluster_id, namespace, replicaset_uid, replicaset_name, container_name, image, created_at, first_seen_at, last_seen_at, deleted_at)
VALUES (1, 'idios-smoke', ?, ?, 'api', 'img', ?, ?, ?, ?)`
	x(rs, "rs-old", "rs-old", oldTS, oldTS, oldTS, oldTS)
	x(rs, "rs-quiet", "rs-quiet", oldTS, oldTS, oldTS, nil)
	x(rs, "rs-live", "rs-live", oldTS, oldTS, recentTS, nil)
	ev := `INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, reason, first_ts, last_ts, raw_json)
VALUES (1, ?, 'idios-smoke', 'Warning', 'Pod', 'p', ?, 'BackOff', ?, ?, '{}')`
	x(ev, "ev-live-old", "live", oldTS, oldTS)
	x(ev, "ev-gone-recent-old", "gone-recent", oldTS, oldTS)
	x(ev, "ev-recent", "live", recentTS, recentTS)
	art := `INSERT INTO artifacts (id, pod_uid, incident_id, container_name, kind, restart_count, file_path, captured_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	x(art, 1, "gone-old", 10, "api", "log_previous", 0, "1/idios-smoke/gone-old/api/restart_000.log", oldTS)
	x(art, 2, "live", 20, "api", "log_current", -1, "1/idios-smoke/live/api/current.log", oldTS)
	x(art, 3, "live", nil, "", "pod_json", -1, "1/idios-smoke/live/pod.json", recentTS)
	x(art, 4, "live", nil, "api", "log_previous", 1, "1/idios-smoke/live/api/restart_001.log", recentTS)
	x(`INSERT INTO artifacts (id, pod_uid, incident_id, container_name, kind, restart_count, capture_gap, captured_at)
VALUES (5, 'live', 20, 'api', 'log_previous', 0, 'no_output', ?)`, oldTS)
	run := `INSERT INTO sweep_runs (ran_at, cutoff, table_name) VALUES (?, ?, 'pods')`
	x(run, oldTS, oldTS)
	x(run, recentTS, recentTS)
}

// seedFiles puts one file behind every artifacts row that should own one,
// two orphans (a stale temp file and a log with no row), and one temp file
// young enough to be a capture in flight. Row 4 gets no file on purpose.
func (h *harness) seedFiles(t *testing.T) {
	t.Helper()
	files := []struct {
		rel     string
		content string
		mtime   time.Time
	}{
		{"1/idios-smoke/gone-old/api/restart_000.log", "12345", testNow.Add(-7 * 24 * time.Hour)},
		{"1/idios-smoke/live/api/current.log", "1234567", testNow.Add(-7 * 24 * time.Hour)},
		{"1/idios-smoke/live/pod.json", "{}", testNow.Add(-time.Hour)},
		{"1/idios-smoke/live/api/restart_009.log", "abc", testNow.Add(-time.Hour)},
		{"tmp/capture-fresh", "xx", testNow},
		{"tmp/capture-stale", "yyyy", testNow.Add(-20 * time.Minute)},
	}
	for _, f := range files {
		p := filepath.Join(h.root, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, f.mtime, f.mtime); err != nil {
			t.Fatal(err)
		}
	}
}

func (h *harness) files(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(h.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(h.root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type survivors struct {
	Pods      []string
	Incidents []string
	History   []string
	Condition []string
	Jobs      []string
	Rollouts  []string
	Events    []string
	Artifacts []string
}

func (h *harness) survivors(t *testing.T) survivors {
	t.Helper()
	return survivors{
		Pods:      h.strings(t, "SELECT uid FROM pods ORDER BY uid"),
		Incidents: h.strings(t, "SELECT CAST(id AS TEXT) FROM incidents ORDER BY id"),
		History:   h.strings(t, "SELECT CAST(id AS TEXT) FROM container_state_history ORDER BY id"),
		Condition: h.strings(t, "SELECT CAST(id AS TEXT) FROM pod_condition_history ORDER BY id"),
		Jobs:      h.strings(t, "SELECT uid FROM jobs ORDER BY uid"),
		Rollouts:  h.strings(t, "SELECT replicaset_uid FROM rollout_history ORDER BY replicaset_uid"),
		Events:    h.strings(t, "SELECT event_uid FROM k8s_events ORDER BY event_uid"),
		Artifacts: h.strings(t, "SELECT CAST(id AS TEXT) FROM artifacts ORDER BY id"),
	}
}

func runRow(table string, rows, files, bytes int64) store.SweepRun {
	return store.SweepRun{RanAt: nowTS, Cutoff: cutoffTS, TableName: table, RowsRemoved: rows, FilesRemoved: files, BytesRemoved: bytes}
}

func TestSweepRemovesExpiredRowsAndFilesInOrderAndRecordsEveryStep(t *testing.T) {
	h := newHarness(t)
	h.seedRows(t)
	h.seedFiles(t)

	if err := h.sw.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := survivors{
		Pods: []string{"gone-recent", "live"},
		// 30 is the job incident of a job the jobs rule just removed. Incidents
		// do not hang off the jobs row, so it goes on its own close date, and
		// the screens read a job incident whose jobs row is gone already.
		Incidents: []string{"21", "22", "30"},
		History:   []string{"3", "6"},
		Condition: []string{"2"},
		Jobs:      []string{"job-live-old"},
		Rollouts:  []string{"rs-live", "rs-quiet"},
		Events:    []string{"ev-live-old", "ev-recent"},
		Artifacts: []string{"3"},
	}
	if d := cmp.Diff(want, h.survivors(t)); d != "" {
		t.Fatal(d)
	}
	if d := cmp.Diff([]string{"1/idios-smoke/live/pod.json", "tmp/capture-fresh"}, h.files(t)); d != "" {
		t.Fatal(d)
	}
	if _, err := os.Stat(filepath.Join(h.root, "1", "idios-smoke", "gone-old")); !os.IsNotExist(err) {
		t.Fatalf("pod directory of a swept pod still exists (err=%v)", err)
	}
	wantRuns := []store.SweepRun{
		{RanAt: recentTS, Cutoff: recentTS, TableName: "pods"},
		runRow("pods", 1, 1, 5),
		runRow("incidents", 1, 1, 7),
		runRow("container_state_history", 3, 0, 0),
		runRow("pod_condition_history", 1, 0, 0),
		runRow("jobs", 2, 0, 0),
		runRow("rollout_history", 1, 0, 0),
		runRow("k8s_events", 1, 0, 0),
		runRow("orphan_files", 0, 2, 7),
		runRow("orphan_rows", 1, 0, 0),
		runRow("sweep_runs", 1, 0, 0),
		runRow("wal_checkpoint", 0, 0, 0),
	}
	if d := cmp.Diff(wantRuns, h.sweepRuns(t)); d != "" {
		t.Fatal(d)
	}
}

func TestPodWhoseFileCannotBeDeletedIsSkippedAndRecorded(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	h := newHarness(t)
	h.seedRows(t)
	h.seedFiles(t)
	locked := filepath.Join(h.root, "1", "idios-smoke", "gone-old", "api")
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	if err := h.sw.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	if d := cmp.Diff([]string{"gone-old", "gone-recent", "live"}, h.survivors(t).Pods); d != "" {
		t.Fatal(d)
	}
	if _, err := os.Stat(filepath.Join(locked, "restart_000.log")); err != nil {
		t.Fatalf("file of the skipped pod: %v", err)
	}
	runs := h.sweepRuns(t)
	var pods store.SweepRun
	for _, r := range runs {
		if r.TableName == "pods" && r.RanAt == nowTS {
			pods = r
		}
	}
	if pods.Error == nil || !strings.Contains(*pods.Error, "gone-old") {
		t.Fatalf("pods row error = %v, want it to name the pod", pods.Error)
	}
	pods.Error = nil
	if d := cmp.Diff(runRow("pods", 0, 0, 0), pods); d != "" {
		t.Fatal(d)
	}
}

func TestOrphanPassToleratesAMissingRoot(t *testing.T) {
	h := newHarness(t)
	h.seedRows(t)
	if err := h.sw.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, r := range h.sweepRuns(t) {
		if r.TableName == "orphan_files" && r.RanAt == nowTS {
			if d := cmp.Diff(runRow("orphan_files", 0, 0, 0), r); d != "" {
				t.Fatal(d)
			}
			return
		}
	}
	t.Fatal("no orphan_files row for this pass")
}
