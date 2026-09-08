package processor

import (
	"context"
	"database/sql"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// PodDeleted records the removal, infers why, closes the pod's incidents
// and, for a watch delete of a pod worth keeping, asks for each container's
// last log so the pool can fall back to its early-capture cache.
func (p *Processor) PodDeleted(ctx context.Context, uid, source string) error {
	now := p.clk.Now()
	nowS := clock.Format(now)
	var reqs []CaptureRequest
	var closed []int64
	err := p.w.Tx(ctx, func(tx *sql.Tx) error {
		pod, err := store.LoadPod(ctx, tx, uid)
		if err != nil || pod == nil || pod.DeletedAt != nil {
			return err
		}
		var own, newest *int64
		var siblings []string
		if pod.ControllerUID != "" {
			if own, newest, err = store.LoadRolloutRevisions(ctx, tx, pod.ControllerUID); err != nil {
				return err
			}
			if siblings, err = store.LoadLiveSiblingCreatedAt(ctx, tx, pod.ControllerUID, uid); err != nil {
				return err
			}
		}
		conditions, err := store.LoadLatestConditions(ctx, tx, uid)
		if err != nil {
			return err
		}
		disrupted := false
		for _, c := range conditions {
			if c.Type == "DisruptionTarget" && c.Status == "True" {
				disrupted = true
			}
		}
		reason := ingest.DeletionReason(pod.ControllerKind, pod.CreatedAt, own, newest, siblings, disrupted)
		if err := store.MarkPodDeleted(ctx, tx, uid, nowS, source, reason); err != nil {
			return err
		}
		keep, err := store.HasRecentIncidentOrFailure(ctx, tx, uid, clock.Format(now.Add(-p.pol.StabilizationWindow)))
		if err != nil {
			return err
		}
		incidents, err := store.LoadIncidentsForSubject(ctx, tx, uid)
		if err != nil {
			return err
		}
		if closed, err = store.CloseOpenIncidents(ctx, tx, uid, store.ClosePodDeleted, nowS); err != nil {
			return err
		}
		// Reconcile runs at startup with an empty early-capture cache; the
		// requests would only produce pod_deleted gaps.
		if !keep || source != store.DeletionSourceWatch {
			return nil
		}
		open := map[string]*int64{}
		for _, inc := range incidents {
			if inc.ClosedAt == nil {
				open[inc.ContainerName] = ptr(inc.ID)
			}
		}
		containers, err := store.LoadContainers(ctx, tx, uid)
		if err != nil {
			return err
		}
		for _, c := range containers {
			id := open[c.Name]
			if id == nil {
				id = open[""]
			}
			reqs = append(reqs, CaptureRequest{
				ClusterID: pod.ClusterID, Namespace: pod.Namespace, PodUID: uid, PodName: pod.Name, Container: c.Name,
				Kind: store.ArtifactLogCurrent, RestartCount: store.NoRestartIndex, Trigger: TriggerDelete, IncidentID: id,
			})
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.notifyIncidents(closed)
	for _, r := range reqs {
		p.sink.Enqueue(r)
	}
	return nil
}
