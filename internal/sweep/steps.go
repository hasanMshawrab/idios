package sweep

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hasanMshawrab/idios/internal/store"
)

const eventBatch = 1000

// steps lists the pass in dependency order: pods first so their cascades do
// the work, then what is left on live pods, then the independent tables.
func (s *Sweeper) steps() []step {
	return []step{
		{"pods", s.sweepPods},
		{"incidents", s.sweepIncidents},
		{"container_state_history", s.countStep(store.DeleteExpiredContainerHistory)},
		{"pod_condition_history", s.countStep(store.DeleteExpiredConditionHistory)},
		{"jobs", s.countStep(store.DeleteExpiredJobs)},
		{"rollout_history", s.countStep(store.DeleteExpiredRollouts)},
		{"k8s_events", s.sweepEvents},
		{"orphan_files", s.sweepOrphanFiles},
		{"orphan_rows", s.sweepOrphanRows},
		{"sweep_runs", s.countStep(store.DeleteExpiredSweepRuns)},
		{"wal_checkpoint", s.checkpoint},
	}
}

func (s *Sweeper) countStep(fn func(context.Context, *sql.Tx, string) (int64, error)) func(context.Context, string) result {
	return func(ctx context.Context, cutoff string) result {
		var r result
		err := s.w.Tx(ctx, func(tx *sql.Tx) (err error) {
			r.rows, err = fn(ctx, tx, cutoff)
			return err
		})
		if err != nil {
			r.errs = append(r.errs, err)
		}
		return r
	}
}

// sweepPods removes each expired pod in its own transaction: files first,
// then the row, whose cascades take everything under it. A pod whose file
// cannot be deleted is skipped whole and tried again next pass.
func (s *Sweeper) sweepPods(ctx context.Context, cutoff string) result {
	var r result
	var uids []string
	err := s.w.Tx(ctx, func(tx *sql.Tx) (err error) {
		uids, err = store.ListExpiredPods(ctx, tx, cutoff)
		return err
	})
	if err != nil {
		r.errs = append(r.errs, err)
		return r
	}
	for _, uid := range uids {
		var files, bytes int64
		err := s.w.Tx(ctx, func(tx *sql.Tx) error {
			arts, err := store.ListPodArtifactFiles(ctx, tx, uid)
			if err != nil {
				return err
			}
			if files, bytes, err = s.removeFiles(arts); err != nil {
				return fmt.Errorf("pod %s: %w", uid, err)
			}
			s.pruneDirs(arts)
			return store.DeletePod(ctx, tx, uid)
		})
		if err != nil {
			r.errs = append(r.errs, err)
			continue
		}
		r.rows++
		r.files += files
		r.bytes += bytes
	}
	return r
}

// sweepIncidents removes each expired incident in its own transaction:
// files, then their rows, then the incident. The schema only detaches
// artifact rows from a deleted incident, so a row whose file is gone, or a
// gap row that never had one, would otherwise linger. History and event
// rows are detached and age out on their own.
func (s *Sweeper) sweepIncidents(ctx context.Context, cutoff string) result {
	var r result
	var ids []int64
	err := s.w.Tx(ctx, func(tx *sql.Tx) (err error) {
		ids, err = store.ListExpiredIncidents(ctx, tx, cutoff)
		return err
	})
	if err != nil {
		r.errs = append(r.errs, err)
		return r
	}
	for _, id := range ids {
		var files, bytes int64
		err := s.w.Tx(ctx, func(tx *sql.Tx) error {
			arts, err := store.ListIncidentArtifactFiles(ctx, tx, id)
			if err != nil {
				return err
			}
			if files, bytes, err = s.removeFiles(arts); err != nil {
				return fmt.Errorf("incident %d: %w", id, err)
			}
			if _, err := store.DeleteIncidentArtifacts(ctx, tx, id); err != nil {
				return err
			}
			return store.DeleteIncident(ctx, tx, id)
		})
		if err != nil {
			r.errs = append(r.errs, err)
			continue
		}
		r.rows++
		r.files += files
		r.bytes += bytes
	}
	return r
}

// sweepEvents deletes in batches, one transaction each, so a large backlog
// never holds the writer for the whole table.
func (s *Sweeper) sweepEvents(ctx context.Context, cutoff string) result {
	var r result
	for {
		var n int64
		err := s.w.Tx(ctx, func(tx *sql.Tx) (err error) {
			n, err = store.DeleteExpiredEventsBatch(ctx, tx, cutoff, eventBatch)
			return err
		})
		if err != nil {
			r.errs = append(r.errs, err)
			return r
		}
		r.rows += n
		if n < eventBatch {
			return r
		}
	}
}
