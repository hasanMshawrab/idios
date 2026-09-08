package incident

import (
	"strings"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// EventRef says which incident an event row belongs to: an entry of
// Ops.Open (OpenIndex >= 0) or an existing incident (IncidentID).
type EventRef struct {
	OpenIndex  int
	IncidentID *int64
}

// FieldPathContainer extracts the container name from an event's
// involvedObject.fieldPath, e.g. spec.containers{api}.
func FieldPathContainer(fieldPath string) string {
	open := strings.IndexByte(fieldPath, '{')
	if open < 0 || !strings.HasSuffix(fieldPath, "}") || !strings.HasPrefix(fieldPath, "spec.") {
		return ""
	}
	return fieldPath[open+1 : len(fieldPath)-1]
}

// EventMatches is the attach rule: same subject, same container or a
// pod-level incident, and the event's category equal or absent.
func EventMatches(ev store.K8sEvent, inc store.Incident) bool {
	switch inc.SubjectKind {
	case store.SubjectPod:
		if deref(inc.PodUID) != ev.InvolvedUID {
			return false
		}
	case store.SubjectJob:
		if deref(inc.JobUID) != ev.InvolvedUID {
			return false
		}
	default:
		return false
	}
	if inc.ContainerName != "" && FieldPathContainer(ev.FieldPath) != inc.ContainerName {
		return false
	}
	return ev.Category == nil || *ev.Category == inc.Category
}

// ApplyEvent decides what an event does to incidents. Only a probe event
// opens or bumps, and only while the pod exists, the container is not
// ready, and the container has been running for at least pol.ProbeGrace:
// Unhealthy fires for every routine readiness miss during a rollout, a
// single event on a container that is ready again is noise, and the kubelet
// starts probing the moment a container starts, so a miss within the grace
// of running_since is the container merely booting. A miss that persists
// repeats past the grace and opens then. A deleted pod's containers stay not
// ready forever, and its pod_deleted close is final, so a delivery trailing
// the delete would open a fresh incident that no close path can ever reach;
// it falls through to a plain attach instead. A pod already terminating is
// the same dead end before the delete watch event lands, so it is gated the
// same way.
// Every other event just attaches to the qualifying open incident with the
// latest last_seen_at.
//
// incidents must be the involved pod's own rows. ev.Category must already be
// set by the caller, from EventCategory(ev.Reason, ev.SourceComponent);
// with a nil category no probe incident ever opens.
func ApplyEvent(incidents []store.Incident, ev store.K8sEvent, pod *store.Pod, container *store.Container, now time.Time, pol Policy) (Ops, EventRef) {
	nowS := clock.Format(now)
	ref := EventRef{OpenIndex: -1}
	var ops Ops
	if deref(ev.Category) == store.CategoryProbe && pod != nil && pod.DeletedAt == nil && pod.DeletionRequestedAt == nil && container != nil && !container.Ready &&
		now.Sub(runningSince(*container, now)) >= pol.ProbeGrace {
		k := key{container.Name, store.CategoryProbe}
		if target, reopen, ok := resolve(incidents, pod.UID, k); ok {
			ops.Attach = append(ops.Attach, Attach{
				IncidentID: target.ID, Reopen: reopen, LastReason: ev.Reason, LastMessage: ptrString(ev.Message), LastSeenAt: nowS,
			})
			id := target.ID
			ref.IncidentID = &id
			return ops, ref
		}
		ops.Open = append(ops.Open, Open{Incident: store.Incident{
			ClusterID: pod.ClusterID, Namespace: pod.Namespace, SubjectKind: store.SubjectPod, PodUID: &pod.UID,
			JobUID: jobOfPod(*pod), ContainerName: container.Name,
			WorkloadKind: pod.WorkloadKind, WorkloadName: pod.WorkloadName, Category: store.CategoryProbe,
			FirstReason: ev.Reason, LastReason: ev.Reason, LastMessage: ptrString(ev.Message),
			Image: &container.Image, ImageTag: container.ImageTag, ImageID: container.ImageID, NodeName: pod.NodeName,
			Occurrences: 1, OpenedAt: ev.LastTS, LastSeenAt: nowS,
		}})
		ref.OpenIndex = 0
		return ops, ref
	}
	var best *store.Incident
	for i := range incidents {
		inc := &incidents[i]
		if inc.ClosedAt != nil || !EventMatches(ev, *inc) {
			continue
		}
		if best == nil || inc.LastSeenAt > best.LastSeenAt {
			best = inc
		}
	}
	if best != nil {
		id := best.ID
		ref.IncidentID = &id
	}
	return ops, ref
}

// runningSince is when the container's current run started; a container the
// kubelet has not reported running waits out the grace from first sight.
func runningSince(c store.Container, now time.Time) time.Time {
	if c.RunningSince == nil {
		return now
	}
	t, err := clock.Parse(*c.RunningSince)
	if err != nil {
		return now
	}
	return t
}

func ptrString(s string) *string { return &s }
