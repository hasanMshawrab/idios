package processor

import (
	"context"
	"database/sql"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// Pod handles an add or update. The store snapshot, not the informer's old
// object, is the baseline, so cold start, relist and steady state share one
// path.
func (p *Processor) Pod(ctx context.Context, clusterID int64, pod *corev1.Pod, r ingest.OwnerResolver) error {
	now := p.clk.Now()
	nowS := clock.Format(now)
	uid := string(pod.UID)
	var reqs []CaptureRequest
	var touched []int64
	err := p.w.Tx(ctx, func(tx *sql.Tx) error {
		snap, err := loadSnapshot(ctx, tx, uid)
		if err != nil {
			return err
		}
		incidents, err := store.LoadIncidentsForSubject(ctx, tx, uid)
		if err != nil {
			return err
		}
		var stored ingest.Workload
		if ctrl := metav1.GetControllerOf(pod); ctrl != nil {
			kind, name, ok, err := store.LoadControllerWorkload(ctx, tx, ctrl.Kind, string(ctrl.UID))
			if err != nil {
				return err
			}
			if ok {
				stored = ingest.Workload{Kind: kind, Name: name}
			}
		}
		changes := ingest.DiffPod(snap, pod, clusterID, r, stored, now)
		ops := incident.Apply(incidents, changes, now, p.pol)

		if err := store.UpsertPod(ctx, tx, changes.Pod); err != nil {
			return err
		}
		if err := store.UpsertContainers(ctx, tx, changes.Containers); err != nil {
			return err
		}
		if err := store.InsertConditions(ctx, tx, changes.Conditions); err != nil {
			return err
		}
		res, err := p.execOps(ctx, tx, ops, uid)
		if err != nil {
			return err
		}
		touched = res.touched
		for i, h := range changes.History {
			h.Category = ops.HistoryCategories[i]
			if id, ok := res.history[i]; ok {
				h.IncidentID = &id
			}
			if err := store.InsertHistory(ctx, tx, h); err != nil {
				return err
			}
		}
		// Both statements rewrite a column the incident list shows, so the
		// rows they reach changed even when no op named them.
		if changes.WorkloadChanged {
			if err := store.SetIncidentWorkload(ctx, tx, uid, changes.Pod.WorkloadKind, changes.Pod.WorkloadName); err != nil {
				return err
			}
			touched = append(touched, openIncidentIDs(incidents, "")...)
		}
		if msg, ok := schedulingMessage(pod); ok {
			if err := store.SetOpenIncidentMessage(ctx, tx, uid, store.CategoryScheduling, msg); err != nil {
				return err
			}
			touched = append(touched, openIncidentIDs(incidents, store.CategoryScheduling)...)
		}
		byContainer, _ := touchedByContainer(incidents, ops, res)
		for _, d := range changes.DeadInstances {
			if !d.Unobservable {
				continue
			}
			err := store.InsertArtifactGap(ctx, tx, store.Artifact{
				PodUID: uid, IncidentID: byContainer[d.Container], ContainerName: d.Container, Kind: store.ArtifactLogPrevious,
				RestartCount: d.Index, CaptureGap: ptr(store.GapUnobservable), CapturedAt: nowS,
			})
			if err != nil {
				return err
			}
		}
		reqs, err = p.podRequests(ctx, tx, clusterID, pod, changes, ops, res, incidents)
		return err
	})
	if err != nil {
		return err
	}
	p.notifyIncidents(touched)
	for _, r := range reqs {
		p.sink.Enqueue(r)
	}
	return nil
}

func loadSnapshot(ctx context.Context, tx *sql.Tx, uid string) (*ingest.PodSnapshot, error) {
	pod, err := store.LoadPod(ctx, tx, uid)
	if err != nil || pod == nil {
		return nil, err
	}
	containers, err := store.LoadContainers(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	conditions, err := store.LoadLatestConditions(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	return &ingest.PodSnapshot{Pod: *pod, Containers: containers, Conditions: conditions}, nil
}

// schedulingMessage returns the current Unschedulable wording. The condition
// key excludes the message, so a node-count change is no transition; the
// incident still shows the latest text.
func schedulingMessage(pod *corev1.Pod) (*string, bool) {
	for _, c := range pod.Status.Conditions {
		if incident.ConditionCategory(string(c.Type), string(c.Status), c.Reason) != store.CategoryScheduling {
			continue
		}
		if c.Message == "" {
			return nil, true
		}
		return ptr(c.Message), true
	}
	return nil, false
}
