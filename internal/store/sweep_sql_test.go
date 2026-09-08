package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func loadAllIncidents(t *testing.T, s *Store) []Incident {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), "SELECT "+incidentColumns+" FROM incidents ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []Incident
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func insertContainer(t *testing.T, s *Store, podUID, name, kind, state string, ready bool, exitCode *int64) {
	t.Helper()
	mustExec(t, s, `
INSERT INTO containers (pod_uid, name, kind, image, state, ready, exit_code, updated_at)
VALUES (?, ?, ?, 'img', ?, ?, ?, ?)`, podUID, name, kind, state, boolInt(ready), exitCode, clock.Format(testEpoch))
}

func TestCloseStableIncidentsReturnsEveryClosedIDAscending(t *testing.T) {
	const (
		stale = "2026-08-27T11:40:00.000000Z"
		edge  = "2026-08-27T11:50:00.000000Z"
		nowS  = "2026-08-27T12:00:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	mustExec(t, s, "UPDATE pods SET node_name = 'n1' WHERE uid = 'p1'")
	for _, in := range []struct{ container, category string }{
		{"api", CategoryCrash}, {"web", CategoryOOM},
	} {
		insertContainer(t, s, "p1", in.container, ContainerKindApp, StateRunning, true, nil)
		id, err := insertPodIncident(t, s, cid, "p1", in.container, in.category)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, s, "UPDATE incidents SET last_seen_at = ? WHERE id = ?", stale, id)
	}
	var got []int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		got, err = CloseStableIncidents(context.Background(), tx, nowS, edge)
		return err
	})
	if d := cmp.Diff([]int64{1, 2}, got); d != "" {
		t.Fatal(d)
	}
}

func TestCloseStableIncidentsClosesOnlyStableStaleContainers(t *testing.T) {
	const (
		stale  = "2026-08-27T11:40:00.000000Z"
		fresh  = "2026-08-27T11:55:00.000000Z"
		edge   = "2026-08-27T11:50:00.000000Z"
		nowS   = "2026-08-27T12:00:00.000000Z"
		noNode = ""
	)
	cases := []struct {
		name       string
		kind       string
		state      string
		ready      bool
		exitCode   *int64
		category   string
		container  string
		nodeName   string
		lastSeenAt string
		job        bool
		wantClosed bool
	}{
		{"app running and ready closes", ContainerKindApp, StateRunning, true, nil, CategoryCrash, "api", "n1", stale, false, true},
		{"sidecar running and ready closes", ContainerKindSidecar, StateRunning, true, nil, CategoryCrash, "proxy", "n1", stale, false, true},
		{"app running not ready stays", ContainerKindApp, StateRunning, false, nil, CategoryCrash, "api", "n1", stale, false, false},
		{"app waiting stays", ContainerKindApp, StateWaiting, false, nil, CategoryCrash, "api", "n1", stale, false, false},
		{"app terminated exit 0 stays", ContainerKindApp, StateTerminated, false, ptr(int64(0)), CategoryCrash, "api", "n1", stale, false, false},
		{"init terminated exit 0 closes", ContainerKindInit, StateTerminated, true, ptr(int64(0)), CategoryConfig, "init-db", "n1", stale, false, true},
		{"init terminated exit 1 stays", ContainerKindInit, StateTerminated, false, ptr(int64(1)), CategoryCrash, "init-db", "n1", stale, false, false},
		{"init running stays", ContainerKindInit, StateRunning, false, nil, CategoryCrash, "init-db", "n1", stale, false, false},
		{"stable but seen within window stays", ContainerKindApp, StateRunning, true, nil, CategoryCrash, "api", "n1", fresh, false, false},
		{"stable seen exactly at the edge stays", ContainerKindApp, StateRunning, true, nil, CategoryCrash, "api", "n1", edge, false, false},
		{"scheduling with node closes", ContainerKindApp, StateWaiting, false, nil, CategoryScheduling, "", "n1", stale, false, true},
		{"scheduling without node stays", ContainerKindApp, StateWaiting, false, nil, CategoryScheduling, "", noNode, stale, false, false},
		{"pod-level node_pressure never closes here", ContainerKindApp, StateRunning, true, nil, CategoryNodePressure, "", "n1", stale, false, false},
		{"job incident never closes here", ContainerKindApp, StateRunning, true, nil, CategoryJobFailed, "", "n1", stale, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			cid := insertCluster(t, s, "c")
			insertPod(t, s, cid, "p1")
			if tc.nodeName != noNode {
				mustExec(t, s, "UPDATE pods SET node_name = ? WHERE uid = 'p1'", tc.nodeName)
			}
			name := tc.container
			if name == "" {
				name = "api"
			}
			insertContainer(t, s, "p1", name, tc.kind, tc.state, tc.ready, tc.exitCode)
			var id int64
			var err error
			if tc.job {
				insertJob(t, s, cid, "j1")
				id, err = insertJobIncident(t, s, cid, "j1", tc.category)
			} else {
				id, err = insertPodIncident(t, s, cid, "p1", tc.container, tc.category)
			}
			if err != nil {
				t.Fatal(err)
			}
			mustExec(t, s, "UPDATE incidents SET last_seen_at = ? WHERE id = ?", tc.lastSeenAt, id)

			var closed []int64
			inTx(t, s, func(tx *sql.Tx) (err error) {
				closed, err = CloseStableIncidents(context.Background(), tx, nowS, edge)
				return err
			})
			got := loadAllIncidents(t, s)[0]
			type outcome struct {
				Closed      []int64
				ClosedAt    *string
				CloseReason *string
			}
			want := outcome{}
			if tc.wantClosed {
				want = outcome{[]int64{id}, ptr(nowS), ptr(CloseRecovered)}
			}
			if d := cmp.Diff(want, outcome{closed, got.ClosedAt, got.CloseReason}); d != "" {
				t.Fatal(d)
			}
		})
	}
}

// The delete path closes what is open when the DELETE arrives; an incident
// opened by a delivery trailing the delete slipped past it and stayed open
// forever, because a deleted pod's containers never stabilize.
func TestCloseDeletedPodIncidentsClosesOpenIncidentsOnDeletedPods(t *testing.T) {
	const (
		deletedAt = "2026-08-27T11:50:00.000000Z"
		before    = "2026-08-27T11:45:00.000000Z"
		after     = "2026-08-27T11:50:03.000000Z"
	)
	epochS := clock.Format(testEpoch)
	cases := []struct {
		name         string
		podDeleted   bool
		openedAt     string
		preClose     *string
		job          bool
		wantClosedAt *string
	}{
		{"opened before the delete closes at the delete", true, before, nil, false, ptr(deletedAt)},
		{"opened after the delete closes at its own open", true, after, nil, false, ptr(after)},
		{"live pod stays open", false, before, nil, false, nil},
		{"already closed keeps its close", true, before, ptr(CloseRecovered), false, nil},
		{"job incident is untouched", true, before, nil, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			cid := insertCluster(t, s, "c")
			insertPod(t, s, cid, "p1")
			if tc.podDeleted {
				mustExec(t, s, "UPDATE pods SET deleted_at = ?, deletion_source = 'watch' WHERE uid = 'p1'", deletedAt)
			}
			var id int64
			var err error
			if tc.job {
				insertJob(t, s, cid, "j1")
				id, err = insertJobIncident(t, s, cid, "j1", CategoryJobFailed)
			} else {
				id, err = insertPodIncident(t, s, cid, "p1", "api", CategoryProbe)
			}
			if err != nil {
				t.Fatal(err)
			}
			mustExec(t, s, "UPDATE incidents SET opened_at = ? WHERE id = ?", tc.openedAt, id)
			if tc.preClose != nil {
				closeIncident(t, s, id, *tc.preClose)
			}

			var closed []int64
			inTx(t, s, func(tx *sql.Tx) (err error) {
				closed, err = CloseDeletedPodIncidents(context.Background(), tx)
				return err
			})
			got := loadAllIncidents(t, s)[0]
			type outcome struct {
				Closed      []int64
				ClosedAt    *string
				CloseReason *string
			}
			want := outcome{}
			switch {
			case tc.wantClosedAt != nil:
				want = outcome{[]int64{id}, tc.wantClosedAt, ptr(ClosePodDeleted)}
			case tc.preClose != nil:
				want = outcome{nil, ptr(epochS), tc.preClose}
			}
			if d := cmp.Diff(want, outcome{closed, got.ClosedAt, got.CloseReason}); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func insertEvent(t *testing.T, s *Store, clusterID int64, uid, involvedUID, fieldPath string, category *string, incidentID *int64) int64 {
	t.Helper()
	ts := clock.Format(testEpoch)
	res := mustExec(t, s, `
INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason,
    first_ts, last_ts, category, incident_id, raw_json)
VALUES (?, ?, 'idios-smoke', 'Warning', 'Pod', 'pod-p1', ?, ?, 'BackOff', ?, ?, ?, ?, '{}')`,
		clusterID, uid, involvedUID, fieldPath, ts, ts, category, incidentID)
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func eventIncidentIDs(t *testing.T, s *Store) map[string]*int64 {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), "SELECT event_uid, incident_id FROM k8s_events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]*int64{}
	for rows.Next() {
		var uid string
		var id *int64
		if err := rows.Scan(&uid, &id); err != nil {
			t.Fatal(err)
		}
		out[uid] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAttachLateEventsFollowsAttachRuleOnRecentlyClosedIncidents(t *testing.T) {
	const (
		recent = "2026-08-27T11:55:00.000000Z"
		old    = "2026-08-27T11:00:00.000000Z"
		edge   = "2026-08-27T11:50:00.000000Z"
	)
	type inc struct {
		container  string
		category   string
		closedAt   *string
		lastSeenAt string
	}
	type ev struct {
		uid        string
		involved   string
		fieldPath  string
		category   *string
		preAttach  bool
		wantAttach int    // index into incidents, -1 for none
		lastTS     string // "" leaves the helper's value (testEpoch)
	}
	cases := []struct {
		name      string
		incidents []inc
		events    []ev
		wantN     int64
	}{
		{"container and category match", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", ptr(CategoryCrash), false, 0, ""}}, 1},
		{"null category attaches", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, 0, ""}}, 1},
		{"category mismatch does not attach", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", ptr(CategoryImagePull), false, -1, ""}}, 0},
		{"other container does not attach", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{web}", nil, false, -1, ""}}, 0},
		{"init container path matches", []inc{{"init-db", CategoryConfig, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.initContainers{init-db}", nil, false, 0, ""}}, 1},
		{"pod-level incident takes any container", []inc{{"", CategoryNodePressure, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, 0, ""}, {"e2", "p1", "", nil, false, 0, ""}}, 2},
		{"other pod does not attach", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p2", "spec.containers{api}", nil, false, -1, ""}}, 0},
		{"closed before the window does not attach", []inc{{"api", CategoryCrash, ptr(old), old}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, -1, ""}}, 0},
		{"open incident is not the closer's job", []inc{{"api", CategoryCrash, nil, recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, -1, ""}}, 0},
		{"already attached event is left alone", []inc{{"api", CategoryCrash, ptr(recent), recent}, {"api", CategoryOOM, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, true, 1, ""}}, 0},
		{"latest last_seen_at wins", []inc{{"api", CategoryCrash, ptr(recent), old}, {"api", CategoryOOM, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, 1, ""}}, 1},
		{"event seen before the window does not attach", []inc{{"api", CategoryCrash, ptr(recent), recent}},
			[]ev{{"e1", "p1", "spec.containers{api}", nil, false, -1, old}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			cid := insertCluster(t, s, "c")
			insertPod(t, s, cid, "p1")
			insertPod(t, s, cid, "p2")
			var ids []int64
			for _, in := range tc.incidents {
				id, err := insertPodIncident(t, s, cid, "p1", in.container, in.category)
				if err != nil {
					t.Fatal(err)
				}
				mustExec(t, s, "UPDATE incidents SET last_seen_at = ? WHERE id = ?", in.lastSeenAt, id)
				if in.closedAt != nil {
					mustExec(t, s, "UPDATE incidents SET closed_at = ?, close_reason = ? WHERE id = ?", *in.closedAt, CloseRecovered, id)
				}
				ids = append(ids, id)
			}
			want := map[string]*int64{}
			for _, e := range tc.events {
				var pre *int64
				if e.preAttach {
					pre = ptr(ids[e.wantAttach])
				}
				insertEvent(t, s, cid, e.uid, e.involved, e.fieldPath, e.category, pre)
				if e.lastTS != "" {
					mustExec(t, s, "UPDATE k8s_events SET last_ts = ? WHERE event_uid = ?", e.lastTS, e.uid)
				}
				if e.wantAttach >= 0 {
					want[e.uid] = ptr(ids[e.wantAttach])
				} else {
					want[e.uid] = nil
				}
			}

			var n int64
			inTx(t, s, func(tx *sql.Tx) (err error) {
				n, err = AttachLateEvents(context.Background(), tx, edge)
				return err
			})
			if n != tc.wantN {
				t.Fatalf("attached %d, want %d", n, tc.wantN)
			}
			if d := cmp.Diff(want, eventIncidentIDs(t, s)); d != "" {
				t.Fatal(d)
			}
		})
	}
}

// A pod incident of a Job carries the job uid, and the Job's own events are
// not evidence on the pod's row: the subject columns decide the attach, not
// the presence of the uid.
func TestAttachLateEventsKeepsJobEventsOffPodIncidents(t *testing.T) {
	const (
		recent = "2026-08-27T11:55:00.000000Z"
		edge   = "2026-08-27T11:50:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	id, err := insertPodIncident(t, s, cid, "p1", "", CategoryCrash)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, s, "UPDATE incidents SET job_uid = 'j1', last_seen_at = ?, closed_at = ?, close_reason = ? WHERE id = ?",
		recent, recent, CloseRecovered, id)
	insertEvent(t, s, cid, "e-job", "j1", "", nil, nil)
	insertEvent(t, s, cid, "e-pod", "p1", "", nil, nil)

	var n int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		n, err = AttachLateEvents(context.Background(), tx, edge)
		return err
	})
	if n != 1 {
		t.Fatalf("attached %d, want 1", n)
	}
	want := map[string]*int64{"e-job": nil, "e-pod": ptr(id)}
	if d := cmp.Diff(want, eventIncidentIDs(t, s)); d != "" {
		t.Fatal(d)
	}
}

func TestExpiredEventsGoInBatchesExceptOnSubjectsWithOpenIncidents(t *testing.T) {
	const (
		cutoff = "2026-08-24T12:00:00.000000Z"
		old    = "2026-08-20T12:00:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "open")
	insertPod(t, s, cid, "closed")
	insertJob(t, s, cid, "jopen")
	podID, err := insertPodIncident(t, s, cid, "open", "api", CategoryCrash)
	if err != nil {
		t.Fatal(err)
	}
	// The open pod incident names the Job above its pod. What keeps a subject's
	// events is an open incident on that subject, so the Job's own events go.
	mustExec(t, s, "UPDATE incidents SET job_uid = 'jpodonly' WHERE id = ?", podID)
	if _, err := insertJobIncident(t, s, cid, "jopen", CategoryJobFailed); err != nil {
		t.Fatal(err)
	}
	id, err := insertPodIncident(t, s, cid, "closed", "api", CategoryCrash)
	if err != nil {
		t.Fatal(err)
	}
	closeIncident(t, s, id, CloseRecovered)
	for i, involved := range []string{"open", "jopen", "closed", "closed", "closed", "unknown", "jpodonly"} {
		insertEvent(t, s, cid, "e"+string(rune('0'+i)), involved, "", nil, nil)
	}
	mustExec(t, s, "UPDATE k8s_events SET last_ts = ? WHERE event_uid <> 'e5'", old)

	var batches []int64
	for {
		var n int64
		inTx(t, s, func(tx *sql.Tx) (err error) {
			n, err = DeleteExpiredEventsBatch(context.Background(), tx, cutoff, 2)
			return err
		})
		batches = append(batches, n)
		if n == 0 {
			break
		}
	}
	if d := cmp.Diff([]int64{2, 2, 0}, batches); d != "" {
		t.Fatal(d)
	}
	want := map[string]*int64{"e0": nil, "e1": nil, "e5": nil}
	if d := cmp.Diff(want, eventIncidentIDs(t, s)); d != "" {
		t.Fatal(d)
	}
}

func TestExpiredHistoryStaysOnlyWhileItsIncidentIsOpen(t *testing.T) {
	const (
		cutoff = "2026-08-24T12:00:00.000000Z"
		old    = "2026-08-20T12:00:00.000000Z"
		recent = "2026-08-26T12:00:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	openID, err := insertPodIncident(t, s, cid, "p1", "api", CategoryCrash)
	if err != nil {
		t.Fatal(err)
	}
	closedID, err := insertPodIncident(t, s, cid, "p1", "api", CategoryOOM)
	if err != nil {
		t.Fatal(err)
	}
	closeIncident(t, s, closedID, CloseRecovered)
	rows := []struct {
		name       string
		incidentID *int64
		observedAt string
		survives   bool
	}{
		{"old-open", &openID, old, true},
		{"old-closed", &closedID, old, false},
		{"old-unattached", nil, old, false},
		{"recent-unattached", nil, recent, true},
	}
	for _, r := range rows {
		mustExec(t, s, `
INSERT INTO container_state_history (pod_uid, container_name, incident_id, image, state, restart_count, observed_at)
VALUES ('p1', ?, ?, 'img', 'waiting', 0, ?)`, r.name, r.incidentID, r.observedAt)
		mustExec(t, s, `
INSERT INTO pod_condition_history (pod_uid, type, status, reason, observed_at) VALUES ('p1', ?, 'False', 'r', ?)`, r.name, r.observedAt)
	}

	var gotC, gotP int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		if gotC, err = DeleteExpiredContainerHistory(context.Background(), tx, cutoff); err != nil {
			return err
		}
		gotP, err = DeleteExpiredConditionHistory(context.Background(), tx, cutoff)
		return err
	})
	if gotC != 2 || gotP != 3 {
		t.Fatalf("removed %d container rows and %d condition rows, want 2 and 3", gotC, gotP)
	}
	var survivors []string
	rs, err := s.Reader.DB().QueryContext(context.Background(), "SELECT container_name FROM container_state_history ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rs.Close() }()
	for rs.Next() {
		var n string
		if err := rs.Scan(&n); err != nil {
			t.Fatal(err)
		}
		survivors = append(survivors, n)
	}
	if d := cmp.Diff([]string{"old-open", "recent-unattached"}, survivors); d != "" {
		t.Fatal(d)
	}
	var conditionSurvivor string
	if err := s.Reader.DB().QueryRowContext(context.Background(), "SELECT type FROM pod_condition_history").Scan(&conditionSurvivor); err != nil {
		t.Fatal(err)
	}
	if conditionSurvivor != "recent-unattached" {
		t.Fatalf("condition survivor %q, want recent-unattached", conditionSurvivor)
	}
}

func TestExpiredJobsGoOnlyWhenTheObjectIsGone(t *testing.T) {
	const (
		cutoff = "2026-08-24T12:00:00.000000Z"
		old    = "2026-08-20T12:00:00.000000Z"
		recent = "2026-08-26T12:00:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	jobs := []struct {
		uid        string
		deletedAt  *string
		finishedAt *string
	}{
		{"deleted-old", ptr(old), nil},
		{"deleted-recent-finished-old", ptr(recent), ptr(old)},
		{"deleted-recent", ptr(recent), ptr(recent)},
		{"live-finished-old", nil, ptr(old)},
		{"live", nil, nil},
	}
	for _, j := range jobs {
		insertJob(t, s, cid, j.uid)
		mustExec(t, s, "UPDATE jobs SET deleted_at = ?, finished_at = ? WHERE uid = ?", j.deletedAt, j.finishedAt, j.uid)
	}
	var n int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		n, err = DeleteExpiredJobs(context.Background(), tx, cutoff)
		return err
	})
	if n != 2 {
		t.Fatalf("removed %d, want 2", n)
	}
	var survivors []string
	rs, err := s.Reader.DB().QueryContext(context.Background(), "SELECT uid FROM jobs ORDER BY uid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rs.Close() }()
	for rs.Next() {
		var u string
		if err := rs.Scan(&u); err != nil {
			t.Fatal(err)
		}
		survivors = append(survivors, u)
	}
	if d := cmp.Diff([]string{"deleted-recent", "live", "live-finished-old"}, survivors); d != "" {
		t.Fatal(d)
	}
}

// The informers run with resync 0, so a stable ReplicaSet is never re-seen
// while it lives; sweeping by last_seen_at threw away the history of the
// revision that was still running.
func TestRolloutSweepKeepsALiveReplicaSet(t *testing.T) {
	const (
		cutoff = "2026-08-24T12:00:00.000000Z"
		old    = "2026-08-20T12:00:00.000000Z"
		recent = "2026-08-26T12:00:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	rows := []struct {
		uid       string
		deletedAt *string
	}{
		{"deleted-old", ptr(old)},
		{"deleted-recent", ptr(recent)},
		{"live-quiet", nil},
	}
	for _, r := range rows {
		mustExec(t, s, `
INSERT INTO rollout_history (cluster_id, namespace, replicaset_uid, replicaset_name, container_name, image, created_at, first_seen_at, last_seen_at, deleted_at)
VALUES (?, 'idios-smoke', ?, ?, 'api', 'img', ?, ?, ?, ?)`, cid, r.uid, r.uid, old, old, old, r.deletedAt)
	}
	var n int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		n, err = DeleteExpiredRollouts(context.Background(), tx, cutoff)
		return err
	})
	if n != 1 {
		t.Fatalf("removed %d, want 1", n)
	}
	var survivors []string
	rs, err := s.Reader.DB().QueryContext(context.Background(), "SELECT replicaset_uid FROM rollout_history ORDER BY replicaset_uid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rs.Close() }()
	for rs.Next() {
		var u string
		if err := rs.Scan(&u); err != nil {
			t.Fatal(err)
		}
		survivors = append(survivors, u)
	}
	if d := cmp.Diff([]string{"deleted-recent", "live-quiet"}, survivors); d != "" {
		t.Fatal(d)
	}
}

func TestCheckpointTruncatesTheWAL(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	for i := 0; i < 50; i++ {
		insertPod(t, s, cid, "p"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	wal := s.path + "-wal"
	before, err := os.Stat(wal)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() == 0 {
		t.Fatal("WAL is empty before the checkpoint; the test proves nothing")
	}
	if err := s.Writer.Checkpoint(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(wal)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != 0 {
		t.Fatalf("WAL is %d bytes after TRUNCATE checkpoint, want 0", after.Size())
	}
}

func TestCheckpointReportsBusyWhileAReaderHoldsTheWAL(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out busy_timeout")
	}
	s, _ := openMigratedStore(t)
	ctx := context.Background()
	insertCluster(t, s, "c")
	rtx, err := s.Reader.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := rtx.QueryRowContext(ctx, "SELECT COUNT(*) FROM clusters").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := s.Writer.Checkpoint(ctx); !errors.Is(err, ErrCheckpointBusy) {
		t.Fatalf("Checkpoint with an open reader = %v, want ErrCheckpointBusy", err)
	}
	if err := rtx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := s.Writer.Checkpoint(ctx); err != nil {
		t.Fatalf("Checkpoint after the reader left = %v", err)
	}
}
