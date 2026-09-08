package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func exec(t *testing.T, s *Store, query string, args ...any) (sql.Result, error) {
	t.Helper()
	var res sql.Result
	err := s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		var err error
		res, err = tx.ExecContext(context.Background(), query, args...)
		return err
	})
	return res, err
}

func mustExec(t *testing.T, s *Store, query string, args ...any) sql.Result {
	t.Helper()
	res, err := exec(t, s, query, args...)
	if err != nil {
		t.Fatalf("exec: %v\n%s", err, query)
	}
	return res
}

func insertPod(t *testing.T, s *Store, clusterID int64, uid string) {
	t.Helper()
	ts := clock.Format(testEpoch)
	mustExec(t, s, `
INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at)
VALUES (?, ?, 'idios-smoke', ?, 'Running', ?, ?, ?)`, uid, clusterID, "pod-"+uid, ts, ts, ts)
}

func insertJob(t *testing.T, s *Store, clusterID int64, uid string) {
	t.Helper()
	ts := clock.Format(testEpoch)
	mustExec(t, s, `
INSERT INTO jobs (uid, cluster_id, namespace, name, restart_policy, created_at, first_seen_at, last_seen_at)
VALUES (?, ?, 'idios-smoke', ?, 'Never', ?, ?, ?)`, uid, clusterID, "job-"+uid, ts, ts, ts)
}

func insertPodIncident(t *testing.T, s *Store, clusterID int64, podUID, container, category string) (int64, error) {
	t.Helper()
	ts := clock.Format(testEpoch)
	res, err := exec(t, s, `
INSERT INTO incidents (cluster_id, namespace, subject_kind, pod_uid, container_name, category,
                       first_reason, last_reason, opened_at, last_seen_at)
VALUES (?, 'idios-smoke', 'pod', ?, ?, ?, 'r', 'r', ?, ?)`, clusterID, podUID, container, category, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func insertJobIncident(t *testing.T, s *Store, clusterID int64, jobUID, category string) (int64, error) {
	t.Helper()
	ts := clock.Format(testEpoch)
	res, err := exec(t, s, `
INSERT INTO incidents (cluster_id, namespace, subject_kind, job_uid, category,
                       first_reason, last_reason, opened_at, last_seen_at)
VALUES (?, 'idios-smoke', 'job', ?, ?, 'r', 'r', ?, ?)`, clusterID, jobUID, category, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func closeIncident(t *testing.T, s *Store, id int64, reason string) {
	t.Helper()
	mustExec(t, s, "UPDATE incidents SET closed_at = ?, close_reason = ? WHERE id = ?", clock.Format(testEpoch), reason, id)
}

func insertArtifact(t *testing.T, s *Store, podUID, container, kind string, restart int, incidentID *int64) error {
	t.Helper()
	_, err := exec(t, s, `
INSERT INTO artifacts (pod_uid, incident_id, container_name, kind, restart_count, file_path, captured_at)
VALUES (?, ?, ?, ?, ?, 'f', ?)`, podUID, incidentID, container, kind, restart, clock.Format(testEpoch))
	return err
}

func nullInt(t *testing.T, s *Store, query string, args ...any) sql.NullInt64 {
	t.Helper()
	var v sql.NullInt64
	if err := s.Reader.DB().QueryRowContext(context.Background(), query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestStrictRejectsWrongType(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	_, err := exec(t, s, `
INSERT INTO containers (pod_uid, name, kind, image, state, restart_count, updated_at)
VALUES ('p1', 'api', 'app', 'img', 'running', 'three', ?)`, clock.Format(testEpoch))
	if err == nil {
		t.Fatal("TEXT accepted in INTEGER column")
	}
}

func TestCascades(t *testing.T) {
	ts := clock.Format(testEpoch)
	seed := func(t *testing.T) (*Store, int64, int64) {
		s, _ := openMigratedStore(t)
		cid := insertCluster(t, s, "c")
		mustExec(t, s, "INSERT INTO watched_namespaces (cluster_id, name, added_at) VALUES (?, 'idios-smoke', ?)", cid, ts)
		insertPod(t, s, cid, "p1")
		insertJob(t, s, cid, "j1")
		mustExec(t, s, `INSERT INTO containers (pod_uid, name, kind, image, state, updated_at) VALUES ('p1', 'api', 'app', 'img', 'running', ?)`, ts)
		inc, err := insertPodIncident(t, s, cid, "p1", "api", "crash")
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, s, `INSERT INTO container_state_history (pod_uid, container_name, incident_id, image, state, restart_count, observed_at)
			VALUES ('p1', 'api', ?, 'img', 'waiting', 1, ?)`, inc, ts)
		mustExec(t, s, `INSERT INTO pod_condition_history (pod_uid, type, status, observed_at) VALUES ('p1', 'Ready', 'False', ?)`, ts)
		mustExec(t, s, `INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, reason, first_ts, last_ts, incident_id, raw_json)
			VALUES (?, 'e1', 'idios-smoke', 'Warning', 'Pod', 'p', 'p1', 'BackOff', ?, ?, ?, '{}')`, cid, ts, ts, inc)
		if err := insertArtifact(t, s, "p1", "api", "log_previous", 0, &inc); err != nil {
			t.Fatal(err)
		}
		mustExec(t, s, `INSERT INTO rollout_history (cluster_id, namespace, replicaset_uid, replicaset_name, container_name, image, created_at, first_seen_at, last_seen_at)
			VALUES (?, 'idios-smoke', 'rs1', 'web-abc', 'web', 'web:1', ?, ?, ?)`, cid, ts, ts, ts)
		return s, cid, inc
	}

	t.Run("delete pod removes everything under it", func(t *testing.T) {
		s, _, _ := seed(t)
		mustExec(t, s, "DELETE FROM pods WHERE uid = 'p1'")
		for _, table := range []string{"containers", "incidents", "container_state_history", "pod_condition_history", "artifacts"} {
			if n := countRows(t, s, table); n != 0 {
				t.Errorf("%s = %d rows, want 0", table, n)
			}
		}
		if n := countRows(t, s, "k8s_events"); n != 1 {
			t.Errorf("k8s_events = %d, want 1 (swept by age, not cascade)", n)
		}
	})

	t.Run("delete cluster removes everything", func(t *testing.T) {
		s, cid, _ := seed(t)
		mustExec(t, s, "DELETE FROM clusters WHERE id = ?", cid)
		for _, table := range []string{"watched_namespaces", "pods", "jobs", "incidents", "k8s_events", "rollout_history", "artifacts"} {
			if n := countRows(t, s, table); n != 0 {
				t.Errorf("%s = %d rows, want 0", table, n)
			}
		}
	})

	t.Run("delete incident detaches but keeps evidence", func(t *testing.T) {
		s, _, inc := seed(t)
		mustExec(t, s, "DELETE FROM incidents WHERE id = ?", inc)
		for _, q := range []string{
			"SELECT incident_id FROM container_state_history",
			"SELECT incident_id FROM k8s_events",
			"SELECT incident_id FROM artifacts",
		} {
			if v := nullInt(t, s, q); v.Valid {
				t.Errorf("%s: incident_id = %d, want NULL", q, v.Int64)
			}
		}
		for _, table := range []string{"container_state_history", "k8s_events", "artifacts"} {
			if n := countRows(t, s, table); n != 1 {
				t.Errorf("%s = %d rows, want 1", table, n)
			}
		}
	})
}

func TestOpenIncidentUniqueness(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	insertJob(t, s, cid, "j1")

	podCase := func(container string) func() (int64, error) {
		return func() (int64, error) { return insertPodIncident(t, s, cid, "p1", container, "crash") }
	}
	jobCase := func() (int64, error) { return insertJobIncident(t, s, cid, "j1", "job_failed") }

	cases := []struct {
		name string
		open func() (int64, error)
	}{
		{"container-level pod incident", podCase("api")},
		{"pod-level incident with container_name ''", podCase("")},
		{"job incident", jobCase},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			first, err := c.open()
			if err != nil {
				t.Fatalf("first open: %v", err)
			}
			if _, err := c.open(); err == nil {
				t.Fatal("second open with same key accepted")
			}
			closeIncident(t, s, first, "recovered")
			if _, err := c.open(); err != nil {
				t.Fatalf("open after close rejected: %v", err)
			}
		})
	}
	if _, err := insertPodIncident(t, s, cid, "p1", "api", "oom"); err != nil {
		t.Fatalf("different category rejected: %v", err)
	}
}

func TestArtifactUniqueness(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	cases := []struct {
		name      string
		container string
		kind      string
		restart   int
	}{
		{"log_previous per dead instance", "api", "log_previous", 0},
		{"pod_json uses '' and -1", "", "pod_json", -1},
		{"log_current uses -1", "api", "log_current", -1},
	}
	for _, c := range cases {
		if err := insertArtifact(t, s, "p1", c.container, c.kind, c.restart, nil); err != nil {
			t.Fatalf("%s: first insert: %v", c.name, err)
		}
		if err := insertArtifact(t, s, "p1", c.container, c.kind, c.restart, nil); err == nil {
			t.Errorf("%s: duplicate accepted", c.name)
		}
	}
	if err := insertArtifact(t, s, "p1", "api", "log_previous", 1, nil); err != nil {
		t.Fatalf("next restart index rejected: %v", err)
	}
}

func TestIncidentChecks(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	insertJob(t, s, cid, "j1")
	ts := clock.Format(testEpoch)
	cases := []struct {
		name string
		cols string
		vals []any
	}{
		{"pod kind without pod_uid",
			"cluster_id, namespace, subject_kind, category, first_reason, last_reason, opened_at, last_seen_at",
			[]any{cid, "n", "pod", "crash", "r", "r", ts, ts}},
		{"job kind with pod_uid too",
			"cluster_id, namespace, subject_kind, pod_uid, job_uid, category, first_reason, last_reason, opened_at, last_seen_at",
			[]any{cid, "n", "job", "p1", "j1", "job_failed", "r", "r", ts, ts}},
		{"unknown category",
			"cluster_id, namespace, subject_kind, pod_uid, category, first_reason, last_reason, opened_at, last_seen_at",
			[]any{cid, "n", "pod", "p1", "weird", "r", "r", ts, ts}},
		{"closed_at without close_reason",
			"cluster_id, namespace, subject_kind, pod_uid, category, first_reason, last_reason, opened_at, last_seen_at, closed_at",
			[]any{cid, "n", "pod", "p1", "crash", "r", "r", ts, ts, ts}},
	}
	for _, c := range cases {
		placeholders := "?"
		for i := 1; i < len(c.vals); i++ {
			placeholders += ", ?"
		}
		if _, err := exec(t, s, "INSERT INTO incidents ("+c.cols+") VALUES ("+placeholders+")", c.vals...); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}
