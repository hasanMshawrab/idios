package store

import (
	"context"
	"database/sql"
)

// CloseCronJobIncidentsBefore closes the open incidents of the other Jobs of
// one CronJob that started no later than this one and returns their ids. A
// later run that worked is the answer to an earlier run that did not.
func CloseCronJobIncidentsBefore(ctx context.Context, tx *sql.Tx, cronJobUID, exceptJobUID string, startedAt *string, closedAt string) ([]int64, error) {
	return queryIDs(ctx, tx, `
UPDATE incidents SET closed_at = ?, close_reason = 'job_finished'
WHERE closed_at IS NULL AND subject_kind = 'job'
  AND job_uid IN (SELECT uid FROM jobs WHERE cronjob_uid = ? AND uid <> ? AND (? IS NULL OR started_at IS NULL OR started_at <= ?))
RETURNING id`, closedAt, cronJobUID, exceptJobUID, startedAt, startedAt)
}
