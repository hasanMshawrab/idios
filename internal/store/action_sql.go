package store

import (
	"context"
	"database/sql"
)

// AcknowledgeIncident stamps acknowledged_at once and reports whether the
// incident exists. A second call keeps the first stamp.
func AcknowledgeIncident(ctx context.Context, tx *sql.Tx, id int64, at string) (bool, error) {
	// SQLite counts every matched row as changed regardless of whether the
	// COALESCE actually replaced a value, so RowsAffected alone answers both
	// the idempotent write and the existence check.
	n, err := execCount(ctx, tx, "UPDATE incidents SET acknowledged_at = COALESCE(acknowledged_at, ?) WHERE id = ?", at, id)
	return n == 1, err
}

// UnacknowledgeIncident clears acknowledged_at and reports whether the
// incident exists.
func UnacknowledgeIncident(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	n, err := execCount(ctx, tx, "UPDATE incidents SET acknowledged_at = NULL WHERE id = ?", id)
	return n == 1, err
}

// ResolveIncident closes an open incident as manual and reports whether the
// incident exists. A closed incident keeps its close.
func ResolveIncident(ctx context.Context, tx *sql.Tx, id int64, closedAt string) (bool, error) {
	n, err := execCount(ctx, tx,
		"UPDATE incidents SET closed_at = COALESCE(closed_at, ?), close_reason = COALESCE(close_reason, ?) WHERE id = ?",
		closedAt, CloseManual, id)
	return n == 1, err
}

// UnresolveIncident reopens an incident a person closed by hand and reports
// whether the incident exists. A system close is not a human action and is
// left alone.
func UnresolveIncident(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	n, err := execCount(ctx, tx,
		`UPDATE incidents SET
		    closed_at = CASE WHEN close_reason = ? THEN NULL ELSE closed_at END,
		    close_reason = CASE WHEN close_reason = ? THEN NULL ELSE close_reason END
		 WHERE id = ?`,
		CloseManual, CloseManual, id)
	return n == 1, err
}

// DismissIncident stamps dismissed_at once and reports whether the incident
// exists. A second call keeps the first stamp.
func DismissIncident(ctx context.Context, tx *sql.Tx, id int64, at string) (bool, error) {
	n, err := execCount(ctx, tx, "UPDATE incidents SET dismissed_at = COALESCE(dismissed_at, ?) WHERE id = ?", at, id)
	return n == 1, err
}

// UndismissIncident clears dismissed_at and reports whether the incident
// exists.
func UndismissIncident(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	n, err := execCount(ctx, tx, "UPDATE incidents SET dismissed_at = NULL WHERE id = ?", id)
	return n == 1, err
}

// SetIncidentNote replaces the note; an empty note clears it.
func SetIncidentNote(ctx context.Context, tx *sql.Tx, id int64, note string) (bool, error) {
	n, err := execCount(ctx, tx, "UPDATE incidents SET note = NULLIF(?, '') WHERE id = ?", note, id)
	return n == 1, err
}
