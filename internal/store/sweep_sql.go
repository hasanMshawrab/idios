package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
)

// execCount runs query and returns how many rows it changed.
func execCount(ctx context.Context, tx *sql.Tx, query string, args ...any) (int64, error) {
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// queryIDs runs a statement with a RETURNING id clause and sorts the ids:
// SQLite gives RETURNING rows in no defined order and forbids ORDER BY on
// them.
func queryIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.Sort(out)
	return out, nil
}

// CloseStableIncidents closes as recovered every open pod incident whose
// subject has been stable since before staleBefore and returns the ids it
// closed, in ascending order. Stable is judged per
// container kind because the kubelet sets ready differently for each: an
// init container is ready only once it exited 0, an app or sidecar only
// while running. A scheduling incident is stable once the pod has a node.
// Pod-level incidents of other categories match no container row and are
// left for the delete path, as are job incidents.
func CloseStableIncidents(ctx context.Context, tx *sql.Tx, closedAt, staleBefore string) ([]int64, error) {
	return queryIDs(ctx, tx, `
UPDATE incidents SET closed_at = ?, close_reason = ?
WHERE closed_at IS NULL AND subject_kind = 'pod' AND last_seen_at < ?
  AND ((category = ? AND EXISTS (
          SELECT 1 FROM pods p WHERE p.uid = incidents.pod_uid AND p.node_name IS NOT NULL))
    OR (category <> ? AND EXISTS (
          SELECT 1 FROM containers c
          WHERE c.pod_uid = incidents.pod_uid AND c.name = incidents.container_name
            AND ((c.kind IN (?, ?) AND c.state = ? AND c.ready = 1)
              OR (c.kind = ? AND c.state = ? AND c.exit_code = 0)))))
RETURNING id`,
		closedAt, CloseRecovered, staleBefore,
		CategoryScheduling, CategoryScheduling,
		ContainerKindApp, ContainerKindSidecar, StateRunning,
		ContainerKindInit, StateTerminated)
}

// CloseDeletedPodIncidents closes as pod_deleted every open incident whose
// pod is already marked deleted and returns the ids it closed, in ascending
// order. The delete path closes what is open when the DELETE is processed;
// anything past it (an incident opened by a delivery trailing the delete)
// would stay open forever, because a deleted pod's containers never
// stabilize. closed_at is the pod's own deleted_at, unless the incident
// opened after it: a close must not precede its open.
func CloseDeletedPodIncidents(ctx context.Context, tx *sql.Tx) ([]int64, error) {
	return queryIDs(ctx, tx, `
UPDATE incidents SET
    closed_at = MAX((SELECT p.deleted_at FROM pods p WHERE p.uid = incidents.pod_uid), opened_at),
    close_reason = ?
WHERE closed_at IS NULL AND subject_kind = 'pod'
  AND EXISTS (SELECT 1 FROM pods p WHERE p.uid = incidents.pod_uid AND p.deleted_at IS NOT NULL)
RETURNING id`, ClosePodDeleted)
}

// AttachLateEvents links unattached events to an incident closed at or
// after closedSince on the same subject, matching the container from
// field_path (or any container for a pod-level incident) and an equal or
// absent category. Among several candidates the most recently seen wins.
// The event itself must have been last seen at or after closedSince too;
// one seen before the window is old, not late, and attaching it would pin
// stale evidence to whatever closed most recently on the container.
func AttachLateEvents(ctx context.Context, tx *sql.Tx, closedSince string) (int64, error) {
	const candidate = `
SELECT i.id FROM incidents i
WHERE i.closed_at >= ?
  AND ((i.subject_kind = 'pod' AND i.pod_uid = k8s_events.involved_uid)
    OR (i.subject_kind = 'job' AND i.job_uid = k8s_events.involved_uid))
  AND (i.container_name = '' OR k8s_events.field_path GLOB 'spec.*{' || i.container_name || '}')
  AND (k8s_events.category IS NULL OR k8s_events.category = i.category)
ORDER BY i.last_seen_at DESC, i.id DESC LIMIT 1`
	return execCount(ctx, tx, `
UPDATE k8s_events SET incident_id = (`+candidate+`)
WHERE incident_id IS NULL AND k8s_events.last_ts >= ? AND EXISTS (`+candidate+`)`, closedSince, closedSince, closedSince)
}

// ArtifactFile is an artifacts row that owns a file on disk.
type ArtifactFile struct {
	ID       int64
	FilePath string
}

func listStrings(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListExpiredPods returns the uids of pods deleted before cutoff.
func ListExpiredPods(ctx context.Context, tx *sql.Tx, cutoff string) ([]string, error) {
	return listStrings(ctx, tx, "SELECT uid FROM pods WHERE deleted_at < ? ORDER BY uid", cutoff)
}

// ListExpiredIncidents returns the ids of incidents closed before cutoff.
func ListExpiredIncidents(ctx context.Context, tx *sql.Tx, cutoff string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM incidents WHERE closed_at < ? ORDER BY id", cutoff)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func listArtifactFiles(ctx context.Context, tx *sql.Tx, where string, args ...any) ([]ArtifactFile, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, file_path FROM artifacts WHERE file_path IS NOT NULL"+where+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ArtifactFile
	for rows.Next() {
		var f ArtifactFile
		if err := rows.Scan(&f.ID, &f.FilePath); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListPodArtifactFiles returns the file-owning artifact rows of one pod.
func ListPodArtifactFiles(ctx context.Context, tx *sql.Tx, podUID string) ([]ArtifactFile, error) {
	return listArtifactFiles(ctx, tx, " AND pod_uid = ?", podUID)
}

// ListIncidentArtifactFiles returns the file-owning artifact rows of one
// incident.
func ListIncidentArtifactFiles(ctx context.Context, tx *sql.Tx, incidentID int64) ([]ArtifactFile, error) {
	return listArtifactFiles(ctx, tx, " AND incident_id = ?", incidentID)
}

// ListArtifactFiles returns every file-owning artifact row.
func ListArtifactFiles(ctx context.Context, tx *sql.Tx) ([]ArtifactFile, error) {
	return listArtifactFiles(ctx, tx, "")
}

// DeletePod removes the pod row; the cascades remove everything under it.
func DeletePod(ctx context.Context, tx *sql.Tx, uid string) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM pods WHERE uid = ?", uid)
	return err
}

// DeleteIncident removes the incident row. Its artifact, history and event
// rows are detached, not removed; callers delete the artifact rows they
// have taken the files of.
func DeleteIncident(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM incidents WHERE id = ?", id)
	return err
}

// DeleteExpiredContainerHistory removes transitions observed before cutoff
// unless they belong to an incident that is still open.
func DeleteExpiredContainerHistory(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, `
DELETE FROM container_state_history
WHERE observed_at < ? AND (incident_id IS NULL OR incident_id IN (SELECT id FROM incidents WHERE closed_at IS NOT NULL))`, cutoff)
}

// DeleteExpiredConditionHistory removes condition rows observed before
// cutoff. Conditions carry no incident_id, so age alone decides.
func DeleteExpiredConditionHistory(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, "DELETE FROM pod_condition_history WHERE observed_at < ?", cutoff)
}

// DeleteExpiredJobs removes jobs whose object is gone and that were deleted,
// or had finished, before cutoff. A job that still exists is never removed.
func DeleteExpiredJobs(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, "DELETE FROM jobs WHERE deleted_at < ? OR (deleted_at IS NOT NULL AND finished_at < ?)", cutoff, cutoff)
}

// DeleteExpiredRollouts removes rollout rows whose ReplicaSet was deleted
// before cutoff. last_seen_at is not a signal: the informers run with resync
// 0, so a stable ReplicaSet is quiet for as long as it lives and its history
// must outlive the silence.
func DeleteExpiredRollouts(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, "DELETE FROM rollout_history WHERE deleted_at < ?", cutoff)
}

// DeleteExpiredEventsBatch removes up to limit events last seen before
// cutoff whose subject has no open incident. Open incidents exist only on
// live objects, so that one check covers "still exists with an open
// incident".
func DeleteExpiredEventsBatch(ctx context.Context, tx *sql.Tx, cutoff string, limit int) (int64, error) {
	return execCount(ctx, tx, `
DELETE FROM k8s_events WHERE id IN (
  SELECT id FROM k8s_events
  WHERE last_ts < ?
    AND involved_uid NOT IN (SELECT pod_uid FROM incidents WHERE closed_at IS NULL AND pod_uid IS NOT NULL)
    AND involved_uid NOT IN (SELECT job_uid FROM incidents WHERE closed_at IS NULL AND subject_kind = 'job')
  ORDER BY id LIMIT ?)`, cutoff, limit)
}

// DeleteArtifactRows removes the given artifact rows.
func DeleteArtifactRows(ctx context.Context, tx *sql.Tx, ids []int64) (int64, error) {
	var n int64
	for _, id := range ids {
		k, err := execCount(ctx, tx, "DELETE FROM artifacts WHERE id = ?", id)
		if err != nil {
			return n, err
		}
		n += k
	}
	return n, nil
}

// DeleteIncidentArtifacts removes every artifact row of one incident, gap
// rows included; the caller has already removed the files.
func DeleteIncidentArtifacts(ctx context.Context, tx *sql.Tx, incidentID int64) (int64, error) {
	return execCount(ctx, tx, "DELETE FROM artifacts WHERE incident_id = ?", incidentID)
}

// DeleteExpiredSweepRuns removes sweep records older than cutoff.
func DeleteExpiredSweepRuns(ctx context.Context, tx *sql.Tx, cutoff string) (int64, error) {
	return execCount(ctx, tx, "DELETE FROM sweep_runs WHERE ran_at < ?", cutoff)
}

// InsertSweepRun records one sweep step.
func InsertSweepRun(ctx context.Context, tx *sql.Tx, r SweepRun) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO sweep_runs (ran_at, cutoff, table_name, rows_removed, files_removed, bytes_removed, duration_ms, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, r.RanAt, r.Cutoff, r.TableName, r.RowsRemoved, r.FilesRemoved, r.BytesRemoved, r.DurationMs, r.Error)
	return err
}

// ErrCheckpointBusy is returned when a reader kept the WAL from being
// truncated, so the file did not shrink on this pass.
var ErrCheckpointBusy = errors.New("wal checkpoint: busy, WAL not truncated")

// Checkpoint moves the WAL into the main file and truncates it. It runs
// outside Tx because a checkpoint inside an open transaction does nothing,
// and it reads the busy column because the pragma reports a blocked
// truncation as a row, not as an error.
func (w *Writer) Checkpoint(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var busy, logFrames, checkpointed int64
	if err := w.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return ErrCheckpointBusy
	}
	return nil
}
