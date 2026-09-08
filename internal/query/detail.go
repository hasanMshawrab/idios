package query

import (
	"context"
	"database/sql"

	"github.com/hasanMshawrab/idios/internal/store"
)

// IncidentDetail is everything the incident page shows. A slice is nil when
// the incident has none of that kind. Events and Artifacts are the subject
// pod's whole record, not only the rows that attached to this incident. A job
// incident carries LastPodName, and its pod parts are the newest pod the
// recorder saw for the Job: the failure a Job reports happened inside a pod,
// so the page must not stop at the jobs row. Job is set for a job incident
// and for a pod incident of a Job alike: the run's condition is what tells a
// retry that succeeded from a run that failed.
type IncidentDetail struct {
	Incident         IncidentRow
	Pod              *store.Pod
	Containers       []store.Container
	Artifacts        []store.Artifact
	Events           []store.K8sEvent
	Conditions       []store.PodCondition
	Job              *JobRow
	RelatedIncidents []IncidentRow
	LastPodName      *string
	// GrafanaURL is the Explore link scoped to the subject container, or the
	// whole pod when there is none; nil when there is no pod or the
	// cluster is unconfigured for Grafana.
	GrafanaURL *string
}

// GetIncident assembles the detail of one incident, or nil when there is no
// incident with that id. p bounds the related incidents.
func GetIncident(ctx context.Context, db store.Querier, id int64, p Page) (*IncidentDetail, error) {
	incidents, err := collect(ctx, db, scanIncidentRow, incidentSelect+" WHERE i.id = ?", id)
	if err != nil || len(incidents) == 0 {
		return nil, err
	}
	incident := incidents[0]
	d := &IncidentDetail{Incident: incident}
	var podUID string
	if incident.PodUID != nil {
		podUID = *incident.PodUID
	}
	// Every incident that names a Job carries its run, whether the Job is the
	// subject or only the owner of the failing pod: the condition is what
	// separates a retry that succeeded from a run that failed.
	if incident.JobUID != nil {
		if d.Job, err = loadJob(ctx, db, *incident.JobUID); err != nil {
			return nil, err
		}
	}
	// A pod incident of a Job has a pod of its own: only a job incident
	// borrows the Job's last pod.
	if incident.SubjectKind == store.SubjectJob && incident.JobUID != nil {
		// A pruned pod of the Job stays in pods as deleted, so the newest
		// row by creation is the last pod there was, gone or not.
		lastUID, lastName, err := lastPod(ctx, db, *incident.JobUID)
		if err != nil {
			return nil, err
		}
		if lastName != "" {
			d.LastPodName, podUID = &lastName, lastUID
		}
	}
	// A capture attaches to whichever of the pod's incidents was open when it
	// ran, so the rows of this one are only part of the story; the pod's files
	// come whole and IncidentID on each row says whose evidence it is. A job
	// incident captures nothing itself and reads its borrowed pod's files the
	// same way.
	if podUID != "" {
		d.Artifacts, err = ListPodArtifacts(ctx, db, podUID)
	} else {
		d.Artifacts, err = collect(ctx, db, scanArtifact, "SELECT "+artifactColumns+
			" FROM artifacts WHERE incident_id = ? ORDER BY container_name, kind, restart_count", id)
	}
	if err != nil {
		return nil, err
	}
	if d.RelatedIncidents, err = RelatedIncidents(ctx, db, incident, p); err != nil {
		return nil, err
	}
	// An event attaches to the one incident that was open when it arrived, so
	// an incident that opened late holds none of the pod's earlier evidence
	// even though that stream is the whole story. The pod's events come whole
	// and IncidentID on each row says whose evidence it is.
	if podUID != "" {
		d.Events, err = collect(ctx, db, scanEvent, "SELECT "+eventColumns+
			" FROM k8s_events WHERE incident_id = ? OR involved_uid = ? ORDER BY last_ts, id", id, podUID)
	} else {
		d.Events, err = collect(ctx, db, scanEvent, "SELECT "+eventColumns+
			" FROM k8s_events WHERE incident_id = ? ORDER BY last_ts, id", id)
	}
	if err != nil {
		return nil, err
	}
	if podUID == "" {
		return d, nil
	}
	if d.Pod, err = loadPod(ctx, db, podUID); err != nil {
		return nil, err
	}
	if d.Containers, err = detailContainers(ctx, db, podUID, incident.ContainerName); err != nil {
		return nil, err
	}
	if d.Conditions, err = latestConditions(ctx, db, podUID); err != nil {
		return nil, err
	}
	if err := attachIncidentGrafana(ctx, db, d); err != nil {
		return nil, err
	}
	return d, nil
}

// collect runs q and scans every row with scan.
func collect[T any](ctx context.Context, db store.Querier, scan func(*sql.Rows) (T, error), q string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// podColumns is every column of the pods table, in schema order, aliased for
// the selects that join counts onto it.
const podColumns = `p.uid, p.cluster_id, p.namespace, p.name, p.node_name, p.phase, p.status_reason, p.status_message,
       p.deletion_requested_at, p.qos_class, p.controller_kind, p.controller_name, p.controller_uid,
       p.workload_kind, p.workload_name, p.created_at, p.started_at, p.first_seen_at, p.last_seen_at,
       p.deleted_at, p.deletion_source, p.deletion_reason`

const containerColumns = `id, pod_uid, name, kind, image, image_tag, image_id, container_id,
       cpu_request, cpu_limit, mem_request, mem_limit, cpu_request_millis, cpu_limit_millis,
       mem_request_bytes, mem_limit_bytes, state, reason, message, exit_code, signal, ready, restart_count,
       running_since, last_terminated_reason, last_terminated_exit_code, last_terminated_signal,
       last_terminated_at, updated_at`

func scanContainer(r *sql.Rows) (store.Container, error) {
	var c store.Container
	var ready int64
	err := r.Scan(&c.ID, &c.PodUID, &c.Name, &c.Kind, &c.Image, &c.ImageTag, &c.ImageID, &c.ContainerID,
		&c.CPURequest, &c.CPULimit, &c.MemRequest, &c.MemLimit, &c.CPURequestMillis, &c.CPULimitMillis,
		&c.MemRequestBytes, &c.MemLimitBytes, &c.State, &c.Reason, &c.Message, &c.ExitCode, &c.Signal, &ready, &c.RestartCount,
		&c.RunningSince, &c.LastTerminatedReason, &c.LastTerminatedExitCode, &c.LastTerminatedSignal,
		&c.LastTerminatedAt, &c.UpdatedAt)
	c.Ready = ready == 1
	return c, err
}

const conditionColumns = `id, pod_uid, type, status, reason, message, k8s_transition_at, observed_at`

func scanCondition(r *sql.Rows) (store.PodCondition, error) {
	var c store.PodCondition
	err := r.Scan(&c.ID, &c.PodUID, &c.Type, &c.Status, &c.Reason, &c.Message, &c.K8sTransitionAt, &c.ObservedAt)
	return c, err
}

// latestConditions returns the newest row per condition type, which is the
// pod's condition set as Kubernetes last reported it.
func latestConditions(ctx context.Context, db store.Querier, podUID string) ([]store.PodCondition, error) {
	return collect(ctx, db, scanCondition, "SELECT "+conditionColumns+` FROM pod_condition_history h
WHERE pod_uid = ? AND id = (SELECT MAX(id) FROM pod_condition_history WHERE pod_uid = h.pod_uid AND type = h.type)
ORDER BY type`, podUID)
}

const artifactColumns = `id, pod_uid, incident_id, container_name, kind, restart_count, file_path,
       size_bytes, truncated, captured_early, capture_gap, capture_note, captured_at`

func scanArtifact(r *sql.Rows) (store.Artifact, error) {
	var a store.Artifact
	var truncated, early int64
	err := r.Scan(&a.ID, &a.PodUID, &a.IncidentID, &a.ContainerName, &a.Kind, &a.RestartCount, &a.FilePath,
		&a.SizeBytes, &truncated, &early, &a.CaptureGap, &a.CaptureNote, &a.CapturedAt)
	a.Truncated, a.CapturedEarly = truncated == 1, early == 1
	return a, err
}

// eventColumns leaves out raw_json: it is the whole Event object again, and
// no screen shows it.
const eventColumns = `id, cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid,
       field_path, reason, message, source_component, count, first_ts, last_ts, category, incident_id`

func scanEvent(r *sql.Rows) (store.K8sEvent, error) {
	var e store.K8sEvent
	err := r.Scan(&e.ID, &e.ClusterID, &e.EventUID, &e.Namespace, &e.Type, &e.InvolvedKind, &e.InvolvedName, &e.InvolvedUID,
		&e.FieldPath, &e.Reason, &e.Message, &e.SourceComponent, &e.Count, &e.FirstTS, &e.LastTS, &e.Category, &e.IncidentID)
	return e, err
}

// loadPod returns the stored pod row, or nil when the incident's pod has been
// swept. It reads through the list select and keeps the stored columns: the
// detail page counts nothing the pod row already carries.
func loadPod(ctx context.Context, db store.Querier, uid string) (*store.Pod, error) {
	pods, err := collect(ctx, db, scanPodRow, podSelect+" WHERE p.uid = ?", uid)
	if err != nil || len(pods) == 0 {
		return nil, err
	}
	return &pods[0].Pod, nil
}

// loadJob returns the job row a job incident is about, or nil when it has
// been swept.
func loadJob(ctx context.Context, db store.Querier, uid string) (*JobRow, error) {
	jobs, err := collect(ctx, db, scanJobRow, jobSelect+" WHERE uid = ?", uid)
	if err != nil || len(jobs) == 0 {
		return nil, err
	}
	return &jobs[0], nil
}

func lastPod(ctx context.Context, db store.Querier, controllerUID string) (uid, name string, err error) {
	type ref struct{ uid, name string }
	refs, err := collect(ctx, db, func(r *sql.Rows) (ref, error) {
		var p ref
		err := r.Scan(&p.uid, &p.name)
		return p, err
	}, "SELECT uid, name FROM pods WHERE controller_uid = ? ORDER BY created_at DESC, uid LIMIT 1", controllerUID)
	if err != nil || len(refs) == 0 {
		return "", "", err
	}
	return refs[0].uid, refs[0].name, nil
}

// detailContainers returns the subject container alone, or every container of
// the pod when the incident names none.
func detailContainers(ctx context.Context, db store.Querier, podUID, name string) ([]store.Container, error) {
	q := "SELECT " + containerColumns + " FROM containers WHERE pod_uid = ?"
	args := []any{podUID}
	if name != "" {
		q += " AND name = ?"
		args = append(args, name)
	}
	return collect(ctx, db, scanContainer, q+" ORDER BY name", args...)
}
