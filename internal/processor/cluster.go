package processor

import (
	"context"
	"database/sql"
	"errors"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// ClusterConnected records a successful connection to the cluster row.
func (p *Processor) ClusterConnected(ctx context.Context, clusterID int64, identity *string, apiServerURL string) error {
	nowS := clock.Format(p.clk.Now())
	err := p.w.Tx(ctx, func(tx *sql.Tx) error {
		return store.MarkClusterConnected(ctx, tx, clusterID, identity, apiServerURL, nowS)
	})
	if err != nil {
		return err
	}
	p.notifyCluster(clusterID)
	return nil
}

// ClusterError records the cluster's most recent failure.
func (p *Processor) ClusterError(ctx context.Context, clusterID int64, msg string) error {
	nowS := clock.Format(p.clk.Now())
	err := p.w.Tx(ctx, func(tx *sql.Tx) error {
		return store.SetClusterError(ctx, tx, clusterID, msg, nowS)
	})
	if err != nil {
		return err
	}
	p.notifyCluster(clusterID)
	return nil
}

// Reconcile marks the cluster's live pod and job rows that live rejects. A
// row whose namespace is no longer watched is unwatched, not gone; the rest
// vanished while nothing was watching. Every row is its own transaction and
// a failing row does not stop the others.
func (p *Processor) Reconcile(ctx context.Context, clusterID int64, watched []string, live func(namespace, uid string) bool) error {
	isWatched := map[string]bool{}
	for _, ns := range watched {
		isWatched[ns] = true
	}
	var pods, jobs []store.LiveObject
	err := p.w.Tx(ctx, func(tx *sql.Tx) (err error) {
		if pods, err = store.LoadLivePods(ctx, tx, clusterID); err != nil {
			return err
		}
		jobs, err = store.LoadLiveJobs(ctx, tx, clusterID)
		return err
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, o := range pods {
		if live(o.Namespace, o.UID) {
			continue
		}
		source := store.DeletionSourceReconcile
		if !isWatched[o.Namespace] {
			source = store.DeletionSourceUnwatched
		}
		if err := p.PodDeleted(ctx, o.UID, source); err != nil {
			errs = append(errs, err)
		}
	}
	for _, o := range jobs {
		if live(o.Namespace, o.UID) {
			continue
		}
		if err := p.JobDeleted(ctx, o.UID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
