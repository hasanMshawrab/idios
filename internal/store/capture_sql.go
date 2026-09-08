package store

import (
	"context"
	"database/sql"
)

// UpsertArtifact writes the row for one capture key. A later capture with a
// file replaces whatever is there; a later gap keeps an existing file row,
// because a failed retry must not turn a captured log into a gap pointing
// at an orphan file.
func UpsertArtifact(ctx context.Context, tx *sql.Tx, a Artifact) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO artifacts (pod_uid, incident_id, container_name, kind, restart_count, file_path, size_bytes, truncated, captured_early, capture_gap, capture_note, captured_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (pod_uid, container_name, kind, restart_count) DO UPDATE SET
    incident_id = COALESCE(excluded.incident_id, artifacts.incident_id),
    file_path = excluded.file_path, size_bytes = excluded.size_bytes, truncated = excluded.truncated,
    captured_early = excluded.captured_early, capture_gap = excluded.capture_gap, capture_note = excluded.capture_note,
    captured_at = excluded.captured_at
WHERE excluded.file_path IS NOT NULL OR artifacts.file_path IS NULL`,
		a.PodUID, a.IncidentID, a.ContainerName, a.Kind, a.RestartCount, a.FilePath, a.SizeBytes, boolInt(a.Truncated), boolInt(a.CapturedEarly), a.CaptureGap, a.CaptureNote, a.CapturedAt)
	return err
}
