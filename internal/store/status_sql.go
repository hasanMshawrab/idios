package store

import (
	"context"
	"database/sql"
)

// Querier is what the display reads need; *sql.DB (the Reader) and *sql.Tx
// both provide it.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var (
	_ Querier = (*sql.DB)(nil)
	_ Querier = (*sql.Tx)(nil)
)

func countBy(ctx context.Context, db Querier, query string) (map[string]int64, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// OpenIncidentsByCategory counts open incidents per category.
func OpenIncidentsByCategory(ctx context.Context, db Querier) (map[string]int64, error) {
	return countBy(ctx, db, "SELECT category, COUNT(*) FROM incidents WHERE closed_at IS NULL GROUP BY category")
}

// ClosedIncidentsByReason counts closed incidents per close_reason.
func ClosedIncidentsByReason(ctx context.Context, db Querier) (map[string]int64, error) {
	return countBy(ctx, db, "SELECT close_reason, COUNT(*) FROM incidents WHERE closed_at IS NOT NULL GROUP BY close_reason")
}

// ArtifactsByOutcome counts artifact rows per capture_gap, under "file" for
// rows that captured one.
func ArtifactsByOutcome(ctx context.Context, db Querier) (map[string]int64, error) {
	return countBy(ctx, db, "SELECT COALESCE(capture_gap, 'file'), COUNT(*) FROM artifacts GROUP BY 1")
}

// RowCounts sizes the observation tables.
type RowCounts struct {
	Pods, LivePods, Transitions, Events int64
}

// CountRows returns the pod, live pod, container transition and event counts.
func CountRows(ctx context.Context, db Querier) (RowCounts, error) {
	var c RowCounts
	err := db.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM pods), (SELECT COUNT(*) FROM pods WHERE deleted_at IS NULL),
       (SELECT COUNT(*) FROM container_state_history), (SELECT COUNT(*) FROM k8s_events)`).
		Scan(&c.Pods, &c.LivePods, &c.Transitions, &c.Events)
	return c, err
}

// LatestSweepRuns returns the newest sweep_runs row of every table, in id
// order, so the last pass can be read without the rows before it.
func LatestSweepRuns(ctx context.Context, db Querier) ([]SweepRun, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, ran_at, cutoff, table_name, rows_removed, files_removed, bytes_removed, duration_ms, error
FROM sweep_runs WHERE id IN (SELECT MAX(id) FROM sweep_runs GROUP BY table_name) ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SweepRun
	for rows.Next() {
		var r SweepRun
		if err := rows.Scan(&r.ID, &r.RanAt, &r.Cutoff, &r.TableName, &r.RowsRemoved, &r.FilesRemoved, &r.BytesRemoved, &r.DurationMs, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
