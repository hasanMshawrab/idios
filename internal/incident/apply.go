package incident

import (
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

type problem struct {
	container    string
	category     string
	reason       string
	message      *string
	openedAt     string
	image        *string
	imageTag     *string
	imageID      *string
	historyIndex int
}

type key struct{ container, category string }

// Apply decides open, attach, reopen and close for every problem in changes.
// Problems are gathered from history rows, then first-sight container
// states, then conditions, then the pod's status reason, and resolved in
// that order against the pod's incidents.
func Apply(incidents []store.Incident, changes ingest.PodChanges, now time.Time, pol Policy) Ops {
	nowS := clock.Format(now)
	ops := Ops{HistoryCategories: make([]*string, len(changes.History))}
	containers := map[string]store.Container{}
	for _, c := range changes.Containers {
		containers[c.Name] = c
	}
	terminating := changes.Pod.DeletionRequestedAt != nil

	var problems []problem
	for i, row := range changes.History {
		c := containers[row.ContainerName]
		cat := ContainerCategory(row.State, deref(row.Reason), deref(c.LastTerminatedReason), terminating)
		if cat == "" {
			continue
		}
		ops.HistoryCategories[i] = &cat
		if cat == store.CategoryUncleanExit {
			var opens bool
			if cat, opens = uncleanExitOutcome(incidents, changes.Pod.UID, c.Name); !opens {
				continue
			}
		}
		reason := deref(row.Reason)
		if cat == store.CategoryOOM && row.State == store.StateWaiting {
			reason = deref(c.LastTerminatedReason)
		}
		problems = append(problems, problem{
			container: c.Name, category: cat, reason: reason, message: row.Message,
			openedAt: firstSet(row.K8sFinishedAt, c.LastTerminatedAt, row.K8sStartedAt, &nowS),
			image:    &c.Image, imageTag: c.ImageTag, imageID: c.ImageID, historyIndex: i,
		})
	}
	if changes.FirstSight {
		for _, c := range changes.Containers {
			cat := ContainerCategory(c.State, deref(c.Reason), deref(c.LastTerminatedReason), terminating)
			if cat == "" {
				continue
			}
			if cat == store.CategoryUncleanExit {
				var opens bool
				if cat, opens = uncleanExitOutcome(incidents, changes.Pod.UID, c.Name); !opens {
					continue
				}
			}
			reason := deref(c.Reason)
			if cat == store.CategoryOOM && c.State == store.StateWaiting {
				reason = deref(c.LastTerminatedReason)
			}
			problems = append(problems, problem{
				container: c.Name, category: cat, reason: reason, message: c.Message,
				openedAt: firstSet(c.LastTerminatedAt, &changes.Pod.CreatedAt),
				image:    &c.Image, imageTag: c.ImageTag, imageID: c.ImageID, historyIndex: -1,
			})
		}
	}
	changed := map[string]bool{}
	for _, cond := range changes.Conditions {
		changed[cond.Type] = true
	}
	// A persisting condition may open, never attach: the scheduler rewrites
	// the Unschedulable message on every attempt and the pod update it causes
	// would otherwise count retries as occurrences.
	var persisting []problem
	for _, cond := range changes.CurrentConditions {
		if cond.Type == "PodScheduled" && cond.Status == "True" {
			if inc, ok := openIncident(incidents, changes.Pod.UID, key{"", store.CategoryScheduling}); ok {
				ops.Close = append(ops.Close, Close{IncidentID: inc.ID, Reason: store.CloseRecovered, ClosedAt: nowS})
			}
			continue
		}
		cat := ConditionCategory(cond.Type, cond.Status, cond.Reason)
		if cat == "" {
			continue
		}
		if cat == store.CategoryScheduling && now.Sub(transitionTime(cond, now)) < pol.SchedulingGrace {
			continue
		}
		p := problem{
			category: cat, reason: cond.Reason, message: cond.Message,
			openedAt: firstSet(cond.K8sTransitionAt, &nowS), historyIndex: -1,
		}
		switch {
		case changed[cond.Type]:
			problems = append(problems, p)
		case cat == store.CategoryScheduling:
			persisting = append(persisting, p)
		}
	}
	if changes.PodReasonChanged || changes.FirstSight {
		if cat := PodReasonCategory(deref(changes.Pod.StatusReason)); cat != "" {
			openedAt := nowS
			if changes.FirstSight {
				openedAt = changes.Pod.CreatedAt
			}
			problems = append(problems, problem{
				category: cat, reason: deref(changes.Pod.StatusReason), message: changes.Pod.StatusMessage,
				openedAt: openedAt, historyIndex: -1,
			})
		}
	}

	opens := map[key]int{}
	attaches := map[key]int{}
	for _, p := range problems {
		k := key{p.container, p.category}
		switch {
		case has(opens, k):
			o := &ops.Open[opens[k]]
			o.Incident.Occurrences++
			o.Incident.LastReason, o.Incident.LastMessage = p.reason, p.message
			o.HistoryIndexes = appendIndex(o.HistoryIndexes, p.historyIndex)
		case has(attaches, k):
			a := &ops.Attach[attaches[k]]
			a.LastReason, a.LastMessage = p.reason, p.message
			a.HistoryIndexes = appendIndex(a.HistoryIndexes, p.historyIndex)
		default:
			if target, reopen, ok := resolve(incidents, changes.Pod.UID, k); ok {
				attaches[k] = len(ops.Attach)
				ops.Attach = append(ops.Attach, Attach{
					IncidentID: target.ID, Reopen: reopen, LastReason: p.reason, LastMessage: p.message,
					LastSeenAt: nowS, HistoryIndexes: appendIndex(nil, p.historyIndex),
				})
				continue
			}
			opens[k] = len(ops.Open)
			ops.Open = append(ops.Open, Open{Incident: newIncident(changes.Pod, p, nowS), HistoryIndexes: appendIndex(nil, p.historyIndex)})
		}
	}
	for _, p := range persisting {
		k := key{p.container, p.category}
		if has(opens, k) || has(attaches, k) {
			continue
		}
		if _, ok := openIncident(incidents, changes.Pod.UID, k); ok {
			continue
		}
		opens[k] = len(ops.Open)
		ops.Open = append(ops.Open, Open{Incident: newIncident(changes.Pod, p, nowS)})
	}
	return ops
}

// transitionTime is when the condition took its current value; a condition
// the API sends without one waits out the window from first sight.
func transitionTime(cond store.PodCondition, now time.Time) time.Time {
	if cond.K8sTransitionAt == nil {
		return now
	}
	t, err := clock.Parse(*cond.K8sTransitionAt)
	if err != nil {
		return now
	}
	return t
}

// uncleanExitOutcome decides what a bad death on a terminating pod becomes
// against the pod's open incidents. An open incident on the same container,
// of any category, takes the termination as an occurrence, so the history
// row and the incident it names never disagree; an open incident anywhere
// else on the pod means the pod died because of something already recorded
// (a preemption, a taint, a sibling's failure) and the termination is
// history alone; nothing open is the one case that opens unclean_exit. A
// crash-looping container that also mishandles SIGTERM therefore never has
// the SIGTERM defect named: its death is the end of the crash.
func uncleanExitOutcome(incidents []store.Incident, podUID, container string) (category string, opens bool) {
	anyOpen := false
	for _, inc := range incidents {
		if inc.SubjectKind != store.SubjectPod || deref(inc.PodUID) != podUID || inc.ClosedAt != nil {
			continue
		}
		if inc.ContainerName == container {
			return inc.Category, true
		}
		anyOpen = true
	}
	return store.CategoryUncleanExit, !anyOpen
}

// openIncident finds the open incident for k on podUID.
func openIncident(incidents []store.Incident, podUID string, k key) (store.Incident, bool) {
	for _, inc := range incidents {
		if inc.SubjectKind == store.SubjectPod && deref(inc.PodUID) == podUID &&
			inc.ContainerName == k.container && inc.Category == k.category && inc.ClosedAt == nil {
			return inc, true
		}
	}
	return store.Incident{}, false
}

// resolve finds the open incident for k on podUID, else the most recently
// closed one that can be reopened. A pod_deleted close is final because its
// pod is gone, and a manual close is final because a person said they were
// done with it: a recurrence opens a new incident instead of reviving that
// one.
func resolve(incidents []store.Incident, podUID string, k key) (target store.Incident, reopen, ok bool) {
	var latest *store.Incident
	for i := range incidents {
		inc := &incidents[i]
		if inc.SubjectKind != store.SubjectPod || deref(inc.PodUID) != podUID ||
			inc.ContainerName != k.container || inc.Category != k.category {
			continue
		}
		if inc.ClosedAt == nil {
			return *inc, false, true
		}
		if reason := deref(inc.CloseReason); reason == store.ClosePodDeleted || reason == store.CloseManual {
			continue
		}
		if latest == nil || *inc.ClosedAt > *latest.ClosedAt {
			latest = inc
		}
	}
	if latest == nil {
		return store.Incident{}, false, false
	}
	return *latest, true, true
}

// controllerKindJob is the owner kind that makes a pod's incident part of a
// Job's failure.
const controllerKindJob = "Job"

// jobOfPod is the Job a pod belongs to, for an incident to carry alongside its
// pod. It is denormalized at open like the pod's workload, because the pods of
// a failed Job are pruned and their incidents outlive the rows that would
// answer the question later.
func jobOfPod(pod store.Pod) *string {
	if pod.ControllerKind != controllerKindJob || pod.ControllerUID == "" {
		return nil
	}
	uid := pod.ControllerUID
	return &uid
}

func newIncident(pod store.Pod, p problem, nowS string) store.Incident {
	return store.Incident{
		ClusterID: pod.ClusterID, Namespace: pod.Namespace, SubjectKind: store.SubjectPod,
		PodUID: &pod.UID, JobUID: jobOfPod(pod), ContainerName: p.container,
		WorkloadKind: pod.WorkloadKind, WorkloadName: pod.WorkloadName,
		Category: p.category, FirstReason: p.reason, LastReason: p.reason, LastMessage: p.message,
		Image: p.image, ImageTag: p.imageTag, ImageID: p.imageID, NodeName: pod.NodeName,
		Occurrences: 1, OpenedAt: p.openedAt, LastSeenAt: nowS,
	}
}

func has(m map[key]int, k key) bool {
	_, ok := m[k]
	return ok
}

func appendIndex(idx []int, i int) []int {
	if i < 0 {
		return idx
	}
	return append(idx, i)
}

func firstSet(vals ...*string) string {
	for _, v := range vals {
		if v != nil && *v != "" {
			return *v
		}
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
