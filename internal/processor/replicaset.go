package processor

import (
	"context"
	"database/sql"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// ReplicaSet upserts one rollout_history row per template container.
func (p *Processor) ReplicaSet(ctx context.Context, clusterID int64, rs *appsv1.ReplicaSet) error {
	rows := ingest.MapReplicaSet(rs, clusterID, p.clk.Now())
	return p.w.Tx(ctx, func(tx *sql.Tx) error {
		for _, r := range rows {
			if err := store.UpsertRolloutHistory(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplicaSetDeleted marks the ReplicaSet's rows gone.
func (p *Processor) ReplicaSetDeleted(ctx context.Context, uid string) error {
	nowS := clock.Format(p.clk.Now())
	return p.w.Tx(ctx, func(tx *sql.Tx) error {
		return store.MarkReplicaSetDeleted(ctx, tx, uid, nowS)
	})
}
