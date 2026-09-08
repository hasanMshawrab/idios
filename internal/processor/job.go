package processor

import (
	"context"
	"database/sql"

	batchv1 "k8s.io/api/batch/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// Job handles an add or update of a Job.
func (p *Processor) Job(ctx context.Context, clusterID int64, job *batchv1.Job) error {
	now := p.clk.Now()
	uid := string(job.UID)
	var touched []int64
	err := p.w.Tx(ctx, func(tx *sql.Tx) error {
		snap, err := store.LoadJob(ctx, tx, uid)
		if err != nil {
			return err
		}
		incidents, err := store.LoadIncidentsForSubject(ctx, tx, uid)
		if err != nil {
			return err
		}
		changes := ingest.DiffJob(snap, job, clusterID, now)
		ops := incident.ApplyJob(incidents, changes, now, p.pol)
		if err := store.UpsertJob(ctx, tx, changes.Job); err != nil {
			return err
		}
		res, err := p.execOps(ctx, tx, ops, uid)
		if err != nil {
			return err
		}
		touched = res.touched
		if changes.Completed && changes.Job.CronJobUID != nil {
			closed, err := store.CloseCronJobIncidentsBefore(ctx, tx, *changes.Job.CronJobUID, uid, changes.Job.StartedAt, clock.Format(now))
			if err != nil {
				return err
			}
			touched = append(touched, closed...)
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.notifyIncidents(touched)
	return nil
}

// JobDeleted marks the job gone and closes its incidents. A repeat is a
// no-op.
func (p *Processor) JobDeleted(ctx context.Context, uid string) error {
	nowS := clock.Format(p.clk.Now())
	var closed []int64
	err := p.w.Tx(ctx, func(tx *sql.Tx) error {
		n, err := store.MarkJobDeleted(ctx, tx, uid, nowS)
		if err != nil || n == 0 {
			return err
		}
		closed, err = store.CloseOpenIncidents(ctx, tx, uid, store.CloseJobFinished, nowS)
		return err
	})
	if err != nil {
		return err
	}
	p.notifyIncidents(closed)
	return nil
}
