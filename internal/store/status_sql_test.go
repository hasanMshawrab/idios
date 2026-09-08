package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func TestStatusReadsGroupIncidentsArtifactsAndRows(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	now := clock.Format(clk.Now())
	c := insertCluster(t, s, "c")
	insertPod(t, s, c, "p1")
	insertPod(t, s, c, "p2")
	mustExec(t, s, "UPDATE pods SET deleted_at = ? WHERE uid = 'p2'", now)
	open1, _ := insertPodIncident(t, s, c, "p1", "api", CategoryCrash)
	if _, err := insertPodIncident(t, s, c, "p1", "web", CategoryCrash); err != nil {
		t.Fatal(err)
	}
	if _, err := insertPodIncident(t, s, c, "p1", "db", CategoryImagePull); err != nil {
		t.Fatal(err)
	}
	closed1, _ := insertPodIncident(t, s, c, "p2", "api", CategoryOOM)
	closeIncident(t, s, closed1, CloseRecovered)
	closed2, _ := insertPodIncident(t, s, c, "p2", "web", CategoryOOM)
	closeIncident(t, s, closed2, ClosePodDeleted)
	if err := insertArtifact(t, s, "p1", "api", ArtifactLogPrevious, 0, &open1); err != nil {
		t.Fatal(err)
	}
	mustExec(t, s, `INSERT INTO artifacts (pod_uid, container_name, kind, restart_count, capture_gap, captured_at) VALUES ('p1', 'api', 'log_current', -1, 'no_output', ?)`, now)
	mustExec(t, s, `INSERT INTO artifacts (pod_uid, container_name, kind, restart_count, capture_gap, captured_at) VALUES ('p1', 'web', 'log_current', -1, 'no_output', ?)`, now)
	mustExec(t, s, `INSERT INTO container_state_history (pod_uid, container_name, image, state, restart_count, observed_at) VALUES ('p1', 'api', 'img', 'waiting', 1, ?)`, now)
	mustExec(t, s, `INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, reason, first_ts, last_ts, raw_json) VALUES (?, 'e1', 'idios-smoke', 'Warning', 'Pod', 'p', 'p1', 'BackOff', ?, ?, '{}')`, c, now, now)

	db := s.Reader.DB()
	open, err := OpenIncidentsByCategory(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(map[string]int64{CategoryCrash: 2, CategoryImagePull: 1}, open); d != "" {
		t.Error("open:", d)
	}
	closed, err := ClosedIncidentsByReason(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(map[string]int64{CloseRecovered: 1, ClosePodDeleted: 1}, closed); d != "" {
		t.Error("closed:", d)
	}
	arts, err := ArtifactsByOutcome(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(map[string]int64{"file": 1, GapNoOutput: 2}, arts); d != "" {
		t.Error("artifacts:", d)
	}
	counts, err := CountRows(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(RowCounts{Pods: 2, LivePods: 1, Transitions: 1, Events: 1}, counts); d != "" {
		t.Error("counts:", d)
	}
}

func TestLatestSweepRunsKeepsTheNewestRowPerTable(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	t0 := clock.Format(clk.Now())
	t1 := clock.Format(clk.Now().Add(time.Hour))
	rows := []SweepRun{
		{RanAt: t0, Cutoff: t0, TableName: "pods", RowsRemoved: 3},
		{RanAt: t0, Cutoff: t0, TableName: "incidents", RowsRemoved: 1},
		{RanAt: t1, Cutoff: t1, TableName: "pods", RowsRemoved: 0, Error: ptr("pod p9: permission denied")},
	}
	inTx(t, s, func(tx *sql.Tx) error {
		for _, r := range rows {
			if err := InsertSweepRun(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
	got, err := LatestSweepRuns(ctx, s.Reader.DB())
	if err != nil {
		t.Fatal(err)
	}
	want := []SweepRun{
		{ID: 2, RanAt: t0, Cutoff: t0, TableName: "incidents", RowsRemoved: 1},
		{ID: 3, RanAt: t1, Cutoff: t1, TableName: "pods", Error: ptr("pod p9: permission denied")},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}
