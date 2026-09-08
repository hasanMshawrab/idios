package query

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/hasanMshawrab/idios/internal/store"
)

// Timeline entry kinds.
const (
	TimelineContainerTransition = "container_transition"
	TimelineCondition           = "condition"
	TimelineEvent               = "event"
	TimelineCapture             = "capture"
	TimelineRollout             = "rollout"
	TimelineLifecycle           = "lifecycle"
	TimelineCut                 = "cut"
)

// Lifecycle steps carried by a lifecycle entry.
const (
	LifecycleOpened = "opened"
	LifecycleClosed = "closed"
)

// kindOrder ranks the kinds for entries sharing a timestamp, so the order is
// total whatever order the six queries ran in.
var kindOrder = map[string]int{
	TimelineContainerTransition: 0,
	TimelineCondition:           1,
	TimelineEvent:               2,
	TimelineCapture:             3,
	TimelineRollout:             4,
	TimelineLifecycle:           5,
	TimelineCut:                 6,
}

// TimelineEntry is one line of the incident timeline. Only the fields its
// Kind carries are set.
type TimelineEntry struct {
	Kind             string
	K8sAt            *string
	ObservedAt       string
	ContainerName    *string
	State            *string
	Reason           *string
	ExitCode         *int64
	Signal           *int64
	RestartCount     *int64
	GapReconstructed *bool
	ConditionType    *string
	ConditionStatus  *string
	Message          *string
	EventType        *string
	EventReason      *string
	Count            *int64
	ArtifactID       *int64
	ArtifactKind     *string
	CaptureGap       *string
	ReplicaSetName   *string
	ImageTag         *string
	Revision         *int64
	Lifecycle        *string
	CloseReason      *string
}

// sortable pairs an entry with the source row id that breaks the last tie.
type sortable struct {
	entry TimelineEntry
	id    int64
}

// IncidentTimeline merges the incident's span from six sources, ordered by
// Kubernetes time where there is one and observed time otherwise. It returns
// nil when there is no incident with that id: a timeline of one that exists
// always carries its opened entry.
func IncidentTimeline(ctx context.Context, db store.Querier, id int64) ([]TimelineEntry, error) {
	var podUID, jobUID, closedAt, closeReason *string
	var subjectKind, openedAt, until string
	err := db.QueryRowContext(ctx, `
SELECT subject_kind, pod_uid, job_uid, opened_at, closed_at, close_reason, COALESCE(closed_at, last_seen_at)
FROM incidents WHERE id = ?`, id).Scan(&subjectKind, &podUID, &jobUID, &openedAt, &closedAt, &closeReason, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	capturePod, err := capturePodUID(ctx, db, subjectKind, podUID, jobUID)
	if err != nil {
		return nil, err
	}

	all := lifecycleEntries(openedAt, closedAt, closeReason)
	for _, source := range []func() ([]sortable, error){
		func() ([]sortable, error) { return transitionEntries(ctx, db, id, podUID, openedAt, until) },
		func() ([]sortable, error) { return conditionEntries(ctx, db, podUID, openedAt, until) },
		func() ([]sortable, error) { return eventEntries(ctx, db, id, podUID, openedAt, until) },
		func() ([]sortable, error) { return captureEntries(ctx, db, id, capturePod) },
		func() ([]sortable, error) { return rolloutEntries(ctx, db, podUID, openedAt, until) },
		func() ([]sortable, error) { return cutEntries(ctx, db, openedAt, until) },
	} {
		part, err := source()
		if err != nil {
			return nil, err
		}
		all = append(all, part...)
	}

	sort.SliceStable(all, func(a, b int) bool {
		x, y := all[a], all[b]
		if k := strings.Compare(at(x.entry), at(y.entry)); k != 0 {
			return k < 0
		}
		if k := strings.Compare(x.entry.ObservedAt, y.entry.ObservedAt); k != 0 {
			return k < 0
		}
		if kindOrder[x.entry.Kind] != kindOrder[y.entry.Kind] {
			return kindOrder[x.entry.Kind] < kindOrder[y.entry.Kind]
		}
		return x.id < y.id
	})
	out := make([]TimelineEntry, 0, len(all))
	for _, s := range all {
		out = append(out, s.entry)
	}
	return out, nil
}

// at is the time an entry sorts on: Kubernetes time when the source carries
// one, observed time otherwise.
func at(e TimelineEntry) string {
	if e.K8sAt != nil {
		return *e.K8sAt
	}
	return e.ObservedAt
}

// transitionEntries takes the transitions charged to the incident plus the
// pod's own transitions inside the span: a container that recovered while the
// incident was open never carries its id, and the recovery is the part of the
// story the reader is looking for.
func transitionEntries(ctx context.Context, db store.Querier, id int64, podUID *string, from, to string) ([]sortable, error) {
	q := `
SELECT id, container_name, state, reason, exit_code, signal, restart_count,
       COALESCE(k8s_finished_at, k8s_started_at), observed_at, gap_reconstructed
FROM container_state_history WHERE incident_id = ?`
	args := []any{id}
	if podUID != nil {
		q += " OR (pod_uid = ? AND observed_at >= ? AND observed_at <= ?)"
		args = append(args, *podUID, from, to)
	}
	return collect(ctx, db, func(r *sql.Rows) (sortable, error) {
		var s sortable
		var gap int64
		var name string
		var state string
		var restarts int64
		e := &s.entry
		err := r.Scan(&s.id, &name, &state, &e.Reason, &e.ExitCode, &e.Signal, &restarts,
			&e.K8sAt, &e.ObservedAt, &gap)
		e.Kind, e.ContainerName, e.State, e.RestartCount = TimelineContainerTransition, &name, &state, &restarts
		reconstructed := gap == 1
		e.GapReconstructed = &reconstructed
		return s, err
	}, q, args...)
}

func conditionEntries(ctx context.Context, db store.Querier, podUID *string, from, to string) ([]sortable, error) {
	if podUID == nil {
		return nil, nil
	}
	return collect(ctx, db, func(r *sql.Rows) (sortable, error) {
		var s sortable
		var condType, status string
		e := &s.entry
		err := r.Scan(&s.id, &condType, &status, &e.Reason, &e.Message, &e.K8sAt, &e.ObservedAt)
		e.Kind, e.ConditionType, e.ConditionStatus = TimelineCondition, &condType, &status
		return s, err
	}, `
SELECT id, type, status, reason, message, k8s_transition_at, observed_at
FROM pod_condition_history WHERE pod_uid = ? AND observed_at >= ? AND observed_at <= ?`, *podUID, from, to)
}

// eventEntries takes the events attached to the incident plus the pod's own
// events inside the span. An event carries no observed time of its own, so
// its last Kubernetes time serves as both.
func eventEntries(ctx context.Context, db store.Querier, id int64, podUID *string, from, to string) ([]sortable, error) {
	q := "SELECT id, type, reason, message, count, last_ts FROM k8s_events WHERE incident_id = ?"
	args := []any{id}
	if podUID != nil {
		q += " OR (involved_uid = ? AND last_ts >= ? AND last_ts <= ?)"
		args = append(args, *podUID, from, to)
	}
	return collect(ctx, db, func(r *sql.Rows) (sortable, error) {
		var s sortable
		var evType, reason, message, lastTS string
		var count int64
		e := &s.entry
		err := r.Scan(&s.id, &evType, &reason, &message, &count, &lastTS)
		e.Kind, e.EventType, e.EventReason = TimelineEvent, &evType, &reason
		e.Message, e.Count = &message, &count
		e.ObservedAt, e.K8sAt = lastTS, &lastTS
		return s, err
	}, q, args...)
}

// capturePodUID is the pod whose captures this incident answers for, which
// for a job incident is the last pod of its Job: a job incident captures
// nothing itself, and the detail lists that pod's files, so the timeline must
// resolve the pod the same way or show fewer.
func capturePodUID(ctx context.Context, db store.Querier, subjectKind string, podUID, jobUID *string) (*string, error) {
	if podUID != nil || subjectKind != store.SubjectJob || jobUID == nil {
		return podUID, nil
	}
	uid, name, err := lastPod(ctx, db, *jobUID)
	if err != nil || name == "" {
		return nil, err
	}
	return &uid, nil
}

// captureEntries reports one entry per captured file or gap of the incident's
// pod, whichever of that pod's incidents each capture attached to, so the
// timeline shows every file the detail lists. The empty container name and the
// -1 restart index are placeholders the unique index needs (pod_json has no
// container; pod_json and log_current have no dead instance), so they are
// reported as absent rather than as values.
func captureEntries(ctx context.Context, db store.Querier, id int64, podUID *string) ([]sortable, error) {
	q := "SELECT id, container_name, kind, restart_count, capture_gap, captured_at FROM artifacts WHERE incident_id = ?"
	args := []any{id}
	if podUID != nil {
		q += " OR pod_uid = ?"
		args = append(args, *podUID)
	}
	return collect(ctx, db, func(r *sql.Rows) (sortable, error) {
		var s sortable
		var name, kind string
		var restarts int64
		e := &s.entry
		err := r.Scan(&s.id, &name, &kind, &restarts, &e.CaptureGap, &e.ObservedAt)
		artifactID := s.id
		e.Kind, e.ArtifactID, e.ArtifactKind = TimelineCapture, &artifactID, &kind
		if name != "" {
			e.ContainerName = &name
		}
		if restarts != store.NoRestartIndex {
			e.RestartCount = &restarts
		}
		return s, err
	}, q, args...)
}

// rolloutEntries reports one entry per ReplicaSet of the pod's workload that
// first appeared inside the span. Its containers are one row each and their
// image tags differ, so the first row by id stands for the ReplicaSet: SQLite
// takes bare columns from the very row that produced a single MIN or MAX
// aggregate, which is what makes this select one row and not a mixture.
func rolloutEntries(ctx context.Context, db store.Querier, podUID *string, from, to string) ([]sortable, error) {
	if podUID == nil {
		return nil, nil
	}
	return collect(ctx, db, func(r *sql.Rows) (sortable, error) {
		var s sortable
		var name string
		e := &s.entry
		err := r.Scan(&s.id, &name, &e.ImageTag, &e.Revision, &e.ObservedAt)
		e.Kind, e.ReplicaSetName = TimelineRollout, &name
		return s, err
	}, `
SELECT MIN(r.id), r.replicaset_name, r.image_tag, r.revision, r.first_seen_at
FROM rollout_history r
JOIN pods p ON p.cluster_id = r.cluster_id AND p.namespace = r.namespace AND p.workload_name = r.deployment_name
WHERE p.uid = ? AND r.first_seen_at >= ? AND r.first_seen_at <= ?
GROUP BY r.replicaset_uid`, *podUID, from, to)
}

// cutEntries marks a sweep that removed history from the span, so an empty
// stretch is not read as a quiet one.
func cutEntries(ctx context.Context, db store.Querier, from, to string) ([]sortable, error) {
	return collect(ctx, db, func(r *sql.Rows) (sortable, error) {
		var s sortable
		e := &s.entry
		err := r.Scan(&s.id, &e.K8sAt, &e.ObservedAt)
		e.Kind = TimelineCut
		return s, err
	}, `
SELECT id, cutoff, ran_at FROM sweep_runs
WHERE rows_removed > 0 AND cutoff >= ? AND cutoff <= ?
  AND table_name IN ('container_state_history', 'pod_condition_history', 'k8s_events')
ORDER BY ran_at DESC, id DESC LIMIT 1`, from, to)
}

func lifecycleEntries(openedAt string, closedAt, closeReason *string) []sortable {
	opened := LifecycleOpened
	out := []sortable{{entry: TimelineEntry{Kind: TimelineLifecycle, ObservedAt: openedAt, Lifecycle: &opened}}}
	if closedAt != nil {
		closed := LifecycleClosed
		out = append(out, sortable{id: 1, entry: TimelineEntry{
			Kind: TimelineLifecycle, ObservedAt: *closedAt, Lifecycle: &closed, CloseReason: closeReason,
		}})
	}
	return out
}
