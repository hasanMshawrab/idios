package processor

import (
	"context"
	"database/sql"

	corev1 "k8s.io/api/core/v1"

	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// Event stores the event, classifies it by reason, lets a probe event open
// or bump an incident, attaches it, and asks for an early capture when the
// reason announces the pod's removal and nothing is watching that container
// yet.
func (p *Processor) Event(ctx context.Context, clusterID int64, ev *corev1.Event) error {
	now := p.clk.Now()
	row, err := ingest.MapEvent(ev, clusterID)
	if err != nil {
		return err
	}
	row.Category = incident.EventCategory(row.Reason, row.SourceComponent)
	container := incident.FieldPathContainer(row.FieldPath)
	var reqs []CaptureRequest
	var touched []int64
	err = p.w.Tx(ctx, func(tx *sql.Tx) error {
		// The informers replay every Event object on a relist; a delivery
		// whose correlator counter and timestamp are not past the stored row
		// is old news and must not bump an incident or capture again.
		count, lastTS, seen, err := store.LoadEventSeen(ctx, tx, clusterID, row.EventUID)
		if err != nil {
			return err
		}
		if seen && row.Count <= count && row.LastTS <= lastTS {
			return nil
		}
		pod, err := store.LoadPod(ctx, tx, row.InvolvedUID)
		if err != nil {
			return err
		}
		var cont *store.Container
		if pod != nil && container != "" {
			if cont, err = store.LoadContainer(ctx, tx, pod.UID, container); err != nil {
				return err
			}
		}
		incidents, err := store.LoadIncidentsForSubject(ctx, tx, row.InvolvedUID)
		if err != nil {
			return err
		}
		ops, ref := incident.ApplyEvent(incidents, row, pod, cont, now, p.pol)
		res, err := p.execOps(ctx, tx, ops, row.InvolvedUID)
		if err != nil {
			return err
		}
		touched = res.touched
		switch {
		case ref.OpenIndex >= 0:
			row.IncidentID = ptr(res.openIDs[ref.OpenIndex])
		case ref.IncidentID != nil:
			row.IncidentID = ref.IncidentID
		}
		if err := store.UpsertEvent(ctx, tx, row); err != nil {
			return err
		}
		if pod == nil || row.InvolvedKind != "Pod" {
			return nil
		}
		base := CaptureRequest{ClusterID: clusterID, Namespace: pod.Namespace, PodUID: pod.UID, PodName: pod.Name, Kind: store.ArtifactLogCurrent, RestartCount: store.NoRestartIndex}
		names := map[int64]string{}
		for _, inc := range incidents {
			names[inc.ID] = inc.ContainerName
		}
		opened := map[int64]bool{}
		for i, o := range ops.Open {
			names[res.openIDs[i]] = o.Incident.ContainerName
			opened[res.openIDs[i]] = true
		}
		for _, a := range ops.Attach {
			if a.Reopen {
				opened[a.IncidentID] = true
			}
		}
		for _, id := range incidentsInOrder(res.openIDs, ops.Attach) {
			if !opened[id] {
				continue
			}
			r := base
			r.Container, r.Trigger, r.IncidentID = names[id], TriggerIncidentOpen, ptr(id)
			reqs = append(reqs, r)
		}
		if ingest.EarlyCaptureReason(row.Reason) && len(opened) == 0 && !hasOpenOn(incidents, container) {
			containers := []string{container}
			if container == "" {
				conts, err := store.LoadContainers(ctx, tx, pod.UID)
				if err != nil {
					return err
				}
				containers = containers[:0]
				for _, c := range conts {
					containers = append(containers, c.Name)
				}
			}
			for _, name := range containers {
				r := base
				r.Container, r.Trigger = name, TriggerEarlyPrefix+row.Reason
				reqs = append(reqs, r)
			}
		}
		return nil
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

// hasOpenOn says whether an open incident already covers the container: its
// own, or a pod-level one. An empty container means any open incident.
func hasOpenOn(incidents []store.Incident, container string) bool {
	for _, inc := range incidents {
		if inc.ClosedAt == nil && (container == "" || inc.ContainerName == container || inc.ContainerName == "") {
			return true
		}
	}
	return false
}
