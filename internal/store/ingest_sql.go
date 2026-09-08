package store

import (
	"context"
	"database/sql"
	"errors"
)

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// LoadPod returns the pods row for uid, or nil when there is none.
func LoadPod(ctx context.Context, tx *sql.Tx, uid string) (*Pod, error) {
	p, err := scanPod(tx.QueryRowContext(ctx, "SELECT "+podColumns+" FROM pods WHERE uid = ?", uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertPod writes every column; the caller has already carried first_seen_at
// and the deletion columns over from the snapshot.
func UpsertPod(ctx context.Context, tx *sql.Tx, p Pod) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO pods (`+podColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (uid) DO UPDATE SET
    cluster_id = excluded.cluster_id, namespace = excluded.namespace, name = excluded.name,
    node_name = excluded.node_name, phase = excluded.phase, status_reason = excluded.status_reason,
    status_message = excluded.status_message, deletion_requested_at = excluded.deletion_requested_at,
    qos_class = excluded.qos_class, controller_kind = excluded.controller_kind,
    controller_name = excluded.controller_name, controller_uid = excluded.controller_uid,
    workload_kind = excluded.workload_kind, workload_name = excluded.workload_name,
    created_at = excluded.created_at, started_at = excluded.started_at,
    first_seen_at = excluded.first_seen_at, last_seen_at = excluded.last_seen_at,
    deleted_at = excluded.deleted_at, deletion_source = excluded.deletion_source,
    deletion_reason = excluded.deletion_reason`,
		p.UID, p.ClusterID, p.Namespace, p.Name, p.NodeName, p.Phase, p.StatusReason, p.StatusMessage,
		p.DeletionRequestedAt, p.QOSClass, p.ControllerKind, p.ControllerName, p.ControllerUID,
		p.WorkloadKind, p.WorkloadName, p.CreatedAt, p.StartedAt, p.FirstSeenAt, p.LastSeenAt,
		p.DeletedAt, p.DeletionSource, p.DeletionReason)
	return err
}

const containerColumns = `id, pod_uid, name, kind, image, image_tag, image_id, container_id,
cpu_request, cpu_limit, mem_request, mem_limit, cpu_request_millis, cpu_limit_millis,
mem_request_bytes, mem_limit_bytes, state, reason, message, exit_code, signal, ready, restart_count,
running_since, last_terminated_reason, last_terminated_exit_code, last_terminated_signal,
last_terminated_at, updated_at`

func scanContainer(r rowScanner) (Container, error) {
	var c Container
	var ready int64
	err := r.Scan(&c.ID, &c.PodUID, &c.Name, &c.Kind, &c.Image, &c.ImageTag, &c.ImageID, &c.ContainerID,
		&c.CPURequest, &c.CPULimit, &c.MemRequest, &c.MemLimit, &c.CPURequestMillis, &c.CPULimitMillis,
		&c.MemRequestBytes, &c.MemLimitBytes, &c.State, &c.Reason, &c.Message, &c.ExitCode, &c.Signal, &ready, &c.RestartCount,
		&c.RunningSince, &c.LastTerminatedReason, &c.LastTerminatedExitCode, &c.LastTerminatedSignal,
		&c.LastTerminatedAt, &c.UpdatedAt)
	c.Ready = ready == 1
	return c, err
}

// LoadContainers returns the pod's container rows in insertion order.
func LoadContainers(ctx context.Context, tx *sql.Tx, podUID string) ([]Container, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+containerColumns+" FROM containers WHERE pod_uid = ? ORDER BY id", podUID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Container
	for rows.Next() {
		c, err := scanContainer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LoadContainer returns one container row, or nil when there is none.
func LoadContainer(ctx context.Context, tx *sql.Tx, podUID, name string) (*Container, error) {
	c, err := scanContainer(tx.QueryRowContext(ctx, "SELECT "+containerColumns+" FROM containers WHERE pod_uid = ? AND name = ?", podUID, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertContainers writes every column of each row on (pod_uid, name).
func UpsertContainers(ctx context.Context, tx *sql.Tx, cs []Container) error {
	for _, c := range cs {
		_, err := tx.ExecContext(ctx, `
INSERT INTO containers (pod_uid, name, kind, image, image_tag, image_id, container_id,
    cpu_request, cpu_limit, mem_request, mem_limit, cpu_request_millis, cpu_limit_millis,
    mem_request_bytes, mem_limit_bytes, state, reason, message, exit_code, signal, ready, restart_count,
    running_since, last_terminated_reason, last_terminated_exit_code, last_terminated_signal,
    last_terminated_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (pod_uid, name) DO UPDATE SET
    kind = excluded.kind, image = excluded.image, image_tag = excluded.image_tag,
    image_id = excluded.image_id, container_id = excluded.container_id,
    cpu_request = excluded.cpu_request, cpu_limit = excluded.cpu_limit,
    mem_request = excluded.mem_request, mem_limit = excluded.mem_limit,
    cpu_request_millis = excluded.cpu_request_millis, cpu_limit_millis = excluded.cpu_limit_millis,
    mem_request_bytes = excluded.mem_request_bytes, mem_limit_bytes = excluded.mem_limit_bytes,
    state = excluded.state, reason = excluded.reason, message = excluded.message,
    exit_code = excluded.exit_code,
    signal = excluded.signal, ready = excluded.ready, restart_count = excluded.restart_count,
    running_since = excluded.running_since, last_terminated_reason = excluded.last_terminated_reason,
    last_terminated_exit_code = excluded.last_terminated_exit_code,
    last_terminated_signal = excluded.last_terminated_signal,
    last_terminated_at = excluded.last_terminated_at, updated_at = excluded.updated_at`,
			c.PodUID, c.Name, c.Kind, c.Image, c.ImageTag, c.ImageID, c.ContainerID,
			c.CPURequest, c.CPULimit, c.MemRequest, c.MemLimit, c.CPURequestMillis, c.CPULimitMillis,
			c.MemRequestBytes, c.MemLimitBytes, c.State, c.Reason, c.Message, c.ExitCode, c.Signal, boolInt(c.Ready), c.RestartCount,
			c.RunningSince, c.LastTerminatedReason, c.LastTerminatedExitCode, c.LastTerminatedSignal,
			c.LastTerminatedAt, c.UpdatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

const conditionColumns = `id, pod_uid, type, status, reason, message, k8s_transition_at, observed_at`

func scanCondition(r rowScanner) (PodCondition, error) {
	var c PodCondition
	err := r.Scan(&c.ID, &c.PodUID, &c.Type, &c.Status, &c.Reason, &c.Message, &c.K8sTransitionAt, &c.ObservedAt)
	return c, err
}

// LoadLatestConditions returns the newest history row per condition type.
func LoadLatestConditions(ctx context.Context, tx *sql.Tx, podUID string) ([]PodCondition, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT `+conditionColumns+` FROM pod_condition_history h
WHERE pod_uid = ? AND id = (SELECT MAX(id) FROM pod_condition_history WHERE pod_uid = h.pod_uid AND type = h.type)
ORDER BY id`, podUID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PodCondition
	for rows.Next() {
		c, err := scanCondition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertConditions appends condition history rows.
func InsertConditions(ctx context.Context, tx *sql.Tx, cs []PodCondition) error {
	for _, c := range cs {
		_, err := tx.ExecContext(ctx, `
INSERT INTO pod_condition_history (pod_uid, type, status, reason, message, k8s_transition_at, observed_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, c.PodUID, c.Type, c.Status, c.Reason, c.Message, c.K8sTransitionAt, c.ObservedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

const incidentColumns = `id, cluster_id, namespace, subject_kind, pod_uid, job_uid, container_name, workload_kind, workload_name,
category, first_reason, last_reason, last_message, image, image_tag, image_id, node_name, occurrences, opened_at, last_seen_at,
closed_at, close_reason, acknowledged_at, dismissed_at, note`

func scanIncident(r rowScanner) (Incident, error) {
	var i Incident
	err := r.Scan(&i.ID, &i.ClusterID, &i.Namespace, &i.SubjectKind, &i.PodUID, &i.JobUID, &i.ContainerName, &i.WorkloadKind, &i.WorkloadName,
		&i.Category, &i.FirstReason, &i.LastReason, &i.LastMessage, &i.Image, &i.ImageTag, &i.ImageID, &i.NodeName, &i.Occurrences, &i.OpenedAt, &i.LastSeenAt,
		&i.ClosedAt, &i.CloseReason, &i.AcknowledgedAt, &i.DismissedAt, &i.Note)
	return i, err
}

// LoadIncidentsForSubject returns every incident, open or closed, whose own
// subject is uid. Closed rows are needed for reopen. A pod incident carries
// the uid of the Job above it too, and that is not its subject: only the
// job incidents answer for a job uid.
func LoadIncidentsForSubject(ctx context.Context, tx *sql.Tx, uid string) ([]Incident, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+incidentColumns+` FROM incidents
WHERE (subject_kind = 'pod' AND pod_uid = ?) OR (subject_kind = 'job' AND job_uid = ?) ORDER BY id`, uid, uid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Incident
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// OpenIncident inserts inc and returns its id. A replay that hits the open
// row's partial unique key bumps that row instead of failing.
func OpenIncident(ctx context.Context, tx *sql.Tx, inc Incident) (int64, error) {
	// SQLite matches a partial index by the text of its predicate, so these
	// must stay spelled as the incidents_open_* indexes spell them.
	target := "(pod_uid, container_name, category) WHERE closed_at IS NULL AND subject_kind = 'pod'"
	if inc.SubjectKind == SubjectJob {
		target = "(job_uid, category) WHERE closed_at IS NULL AND subject_kind = 'job'"
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
INSERT INTO incidents (cluster_id, namespace, subject_kind, pod_uid, job_uid, container_name, workload_kind, workload_name,
    category, first_reason, last_reason, last_message, image, image_tag, image_id, node_name, occurrences, opened_at, last_seen_at,
    closed_at, close_reason)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT `+target+` DO UPDATE SET
    occurrences = incidents.occurrences + 1, last_reason = excluded.last_reason,
    last_message = excluded.last_message, last_seen_at = excluded.last_seen_at
RETURNING id`,
		inc.ClusterID, inc.Namespace, inc.SubjectKind, inc.PodUID, inc.JobUID, inc.ContainerName, inc.WorkloadKind, inc.WorkloadName,
		inc.Category, inc.FirstReason, inc.LastReason, inc.LastMessage, inc.Image, inc.ImageTag, inc.ImageID, inc.NodeName, inc.Occurrences, inc.OpenedAt, inc.LastSeenAt,
		inc.ClosedAt, inc.CloseReason,
	).Scan(&id)
	return id, err
}

// AttachIncident bumps an incident; with reopen it also clears the close and
// the dismissal, keeping acknowledged_at.
func AttachIncident(ctx context.Context, tx *sql.Tx, id int64, reopen bool, lastReason string, lastMessage *string, lastSeenAt string) error {
	set := ""
	if reopen {
		set = ", closed_at = NULL, close_reason = NULL, dismissed_at = NULL"
	}
	_, err := tx.ExecContext(ctx, `
UPDATE incidents SET occurrences = occurrences + 1, last_reason = ?, last_message = ?, last_seen_at = ?`+set+` WHERE id = ?`,
		lastReason, lastMessage, lastSeenAt, id)
	return err
}

// CloseIncident closes one open incident.
func CloseIncident(ctx context.Context, tx *sql.Tx, id int64, reason, closedAt string) error {
	_, err := tx.ExecContext(ctx, "UPDATE incidents SET closed_at = ?, close_reason = ? WHERE id = ? AND closed_at IS NULL", closedAt, reason, id)
	return err
}

// CloseOpenIncidents closes every open incident whose own subject is
// subjectUID and returns the ids it closed, in ascending order. A pod
// incident of that job is not its subject: its pod closes it, on the pod's
// own reason.
func CloseOpenIncidents(ctx context.Context, tx *sql.Tx, subjectUID, reason, closedAt string) ([]int64, error) {
	return queryIDs(ctx, tx, `
UPDATE incidents SET closed_at = ?, close_reason = ?
WHERE closed_at IS NULL
  AND ((subject_kind = 'pod' AND pod_uid = ?) OR (subject_kind = 'job' AND job_uid = ?))
RETURNING id`, closedAt, reason, subjectUID, subjectUID)
}

// SetIncidentWorkload corrects workload_* on a pod's open incidents.
func SetIncidentWorkload(ctx context.Context, tx *sql.Tx, podUID, kind, name string) error {
	_, err := tx.ExecContext(ctx, "UPDATE incidents SET workload_kind = ?, workload_name = ? WHERE pod_uid = ? AND closed_at IS NULL", kind, name, podUID)
	return err
}

// SetOpenIncidentMessage refreshes last_message on a pod's open incident of
// one category without touching last_seen_at.
func SetOpenIncidentMessage(ctx context.Context, tx *sql.Tx, podUID, category string, message *string) error {
	_, err := tx.ExecContext(ctx, "UPDATE incidents SET last_message = ? WHERE pod_uid = ? AND category = ? AND closed_at IS NULL", message, podUID, category)
	return err
}

// InsertHistory appends one container transition row.
func InsertHistory(ctx context.Context, tx *sql.Tx, h ContainerStateHistory) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO container_state_history (pod_uid, container_name, incident_id, image, image_id, container_id, state, reason, message, exit_code, signal,
    restart_count, category, k8s_started_at, k8s_finished_at, observed_at, gap_reconstructed)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.PodUID, h.ContainerName, h.IncidentID, h.Image, h.ImageID, h.ContainerID, h.State, h.Reason, h.Message, h.ExitCode, h.Signal,
		h.RestartCount, h.Category, h.K8sStartedAt, h.K8sFinishedAt, h.ObservedAt, boolInt(h.GapReconstructed))
	return err
}

// AttachEvents links unattached events of the subject to incidentID when the
// container from field_path matches (or the incident is pod-level), the
// category is equal or absent, and last_ts is not before sinceTS.
func AttachEvents(ctx context.Context, tx *sql.Tx, incidentID int64, involvedUID, container, category, sinceTS string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
UPDATE k8s_events SET incident_id = ?
WHERE incident_id IS NULL AND involved_uid = ? AND last_ts >= ?
  AND (category IS NULL OR category = ?)
  AND (? = '' OR field_path GLOB ?)`,
		incidentID, involvedUID, sinceTS, category, container, "spec.*{"+container+"}")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// LoadEventSeen returns the stored count and last_ts of an event, with seen
// false when there is no row. Together they say whether a delivery carries
// anything new: a relist replays every Event object unchanged.
func LoadEventSeen(ctx context.Context, tx *sql.Tx, clusterID int64, eventUID string) (count int64, lastTS string, seen bool, err error) {
	err = tx.QueryRowContext(ctx, "SELECT count, last_ts FROM k8s_events WHERE cluster_id = ? AND event_uid = ?", clusterID, eventUID).Scan(&count, &lastTS)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return count, lastTS, true, nil
}

// UpsertEvent writes the event on (cluster_id, event_uid). An existing
// incident_id survives a bump whose own lookup found nothing.
func UpsertEvent(ctx context.Context, tx *sql.Tx, ev K8sEvent) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, message,
    source_component, count, first_ts, last_ts, category, incident_id, raw_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (cluster_id, event_uid) DO UPDATE SET
    type = excluded.type, message = excluded.message, source_component = excluded.source_component,
    count = excluded.count, first_ts = excluded.first_ts, last_ts = excluded.last_ts, category = excluded.category,
    incident_id = COALESCE(excluded.incident_id, k8s_events.incident_id), raw_json = excluded.raw_json`,
		ev.ClusterID, ev.EventUID, ev.Namespace, ev.Type, ev.InvolvedKind, ev.InvolvedName, ev.InvolvedUID, ev.FieldPath, ev.Reason, ev.Message,
		ev.SourceComponent, ev.Count, ev.FirstTS, ev.LastTS, ev.Category, ev.IncidentID, ev.RawJSON)
	return err
}

// InsertArtifactGap records an instance no capture can reach. A second
// report of the same instance is ignored.
func InsertArtifactGap(ctx context.Context, tx *sql.Tx, a Artifact) error {
	_, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO artifacts (pod_uid, incident_id, container_name, kind, restart_count, capture_gap, captured_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, a.PodUID, a.IncidentID, a.ContainerName, a.Kind, a.RestartCount, a.CaptureGap, a.CapturedAt)
	return err
}

// HasArtifactFile reports whether a file was already captured for the key.
func HasArtifactFile(ctx context.Context, tx *sql.Tx, podUID, container, kind string, restartCount int64) (bool, error) {
	var n int64
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM artifacts WHERE pod_uid = ? AND container_name = ? AND kind = ? AND restart_count = ? AND file_path IS NOT NULL`,
		podUID, container, kind, restartCount).Scan(&n)
	return n > 0, err
}

const jobColumns = `uid, cluster_id, namespace, name, cronjob_uid, cronjob_name, active, succeeded, failed, backoff_limit, completions,
parallelism, active_deadline_seconds, restart_policy, condition_type, condition_reason, condition_message, created_at, started_at, finished_at,
first_seen_at, last_seen_at, deleted_at`

func scanJob(r rowScanner) (Job, error) {
	var j Job
	err := r.Scan(&j.UID, &j.ClusterID, &j.Namespace, &j.Name, &j.CronJobUID, &j.CronJobName, &j.Active, &j.Succeeded, &j.Failed,
		&j.BackoffLimit, &j.Completions, &j.Parallelism, &j.ActiveDeadlineSeconds, &j.RestartPolicy, &j.ConditionType, &j.ConditionReason, &j.ConditionMessage,
		&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.FirstSeenAt, &j.LastSeenAt, &j.DeletedAt)
	return j, err
}

// LoadJob returns the jobs row for uid, or nil when there is none.
func LoadJob(ctx context.Context, tx *sql.Tx, uid string) (*Job, error) {
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE uid = ?", uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// UpsertJob writes every column of the job row.
func UpsertJob(ctx context.Context, tx *sql.Tx, j Job) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO jobs (`+jobColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (uid) DO UPDATE SET
    cluster_id = excluded.cluster_id, namespace = excluded.namespace, name = excluded.name,
    cronjob_uid = excluded.cronjob_uid, cronjob_name = excluded.cronjob_name, active = excluded.active,
    succeeded = excluded.succeeded, failed = excluded.failed, backoff_limit = excluded.backoff_limit,
    completions = excluded.completions, parallelism = excluded.parallelism,
    active_deadline_seconds = excluded.active_deadline_seconds, restart_policy = excluded.restart_policy,
    condition_type = excluded.condition_type, condition_reason = excluded.condition_reason,
    condition_message = excluded.condition_message, created_at = excluded.created_at, started_at = excluded.started_at,
    finished_at = excluded.finished_at, first_seen_at = excluded.first_seen_at, last_seen_at = excluded.last_seen_at,
    deleted_at = excluded.deleted_at`,
		j.UID, j.ClusterID, j.Namespace, j.Name, j.CronJobUID, j.CronJobName, j.Active, j.Succeeded, j.Failed, j.BackoffLimit, j.Completions,
		j.Parallelism, j.ActiveDeadlineSeconds, j.RestartPolicy, j.ConditionType, j.ConditionReason, j.ConditionMessage, j.CreatedAt, j.StartedAt, j.FinishedAt,
		j.FirstSeenAt, j.LastSeenAt, j.DeletedAt)
	return err
}

// MarkJobDeleted sets deleted_at once and returns how many rows changed.
func MarkJobDeleted(ctx context.Context, tx *sql.Tx, uid, deletedAt string) (int64, error) {
	res, err := tx.ExecContext(ctx, "UPDATE jobs SET deleted_at = ? WHERE uid = ? AND deleted_at IS NULL", deletedAt, uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpsertRolloutHistory writes one (replicaset, container) row; first_seen_at
// is kept and a returning ReplicaSet is undeleted. The replica counts are
// rewritten on every event because a scaling ReplicaSet changes them.
func UpsertRolloutHistory(ctx context.Context, tx *sql.Tx, r RolloutHistory) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO rollout_history (cluster_id, namespace, deployment_name, deployment_uid, replicaset_uid, replicaset_name, container_name,
    image, image_tag, revision, created_at, replicas, ready_replicas, available_replicas, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (replicaset_uid, container_name) DO UPDATE SET
    deployment_name = excluded.deployment_name, deployment_uid = excluded.deployment_uid, replicaset_name = excluded.replicaset_name,
    image = excluded.image, image_tag = excluded.image_tag, revision = excluded.revision,
    created_at = excluded.created_at, replicas = excluded.replicas,
    ready_replicas = excluded.ready_replicas, available_replicas = excluded.available_replicas,
    last_seen_at = excluded.last_seen_at, deleted_at = NULL`,
		r.ClusterID, r.Namespace, r.DeploymentName, r.DeploymentUID, r.ReplicaSetUID, r.ReplicaSetName, r.ContainerName,
		r.Image, r.ImageTag, r.Revision, r.CreatedAt, r.Replicas, r.ReadyReplicas, r.AvailableReplicas, r.FirstSeenAt, r.LastSeenAt)
	return err
}

// MarkReplicaSetDeleted sets deleted_at on every row of the ReplicaSet.
func MarkReplicaSetDeleted(ctx context.Context, tx *sql.Tx, replicasetUID, deletedAt string) error {
	_, err := tx.ExecContext(ctx, "UPDATE rollout_history SET deleted_at = ? WHERE replicaset_uid = ? AND deleted_at IS NULL", deletedAt, replicasetUID)
	return err
}

// MarkPodDeleted records the deletion on the pod row.
func MarkPodDeleted(ctx context.Context, tx *sql.Tx, uid, deletedAt, source, reason string) error {
	_, err := tx.ExecContext(ctx, "UPDATE pods SET deleted_at = ?, deletion_source = ?, deletion_reason = ? WHERE uid = ?", deletedAt, source, reason, uid)
	return err
}

// LoadRolloutRevisions returns the ReplicaSet's own revision and the highest
// revision among ReplicaSets of the same Deployment. Both are nil when the
// ReplicaSet is unknown or owned by no Deployment.
func LoadRolloutRevisions(ctx context.Context, tx *sql.Tx, replicasetUID string) (own, newest *int64, err error) {
	var depUID string
	err = tx.QueryRowContext(ctx, "SELECT deployment_uid, revision FROM rollout_history WHERE replicaset_uid = ? LIMIT 1", replicasetUID).Scan(&depUID, &own)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil || depUID == "" {
		return nil, nil, err
	}
	err = tx.QueryRowContext(ctx, "SELECT MAX(revision) FROM rollout_history WHERE deployment_uid = ?", depUID).Scan(&newest)
	return own, newest, err
}

// LoadControllerWorkload returns the owner one step above a pod's controller.
// The jobs and rollout_history rows outlive the controller they describe, so
// they still answer after a pruned Job or ReplicaSet has left the informer
// store.
func LoadControllerWorkload(ctx context.Context, tx *sql.Tx, kind, uid string) (string, string, bool, error) {
	var query, ownerKind string
	switch kind {
	case "Job":
		query, ownerKind = "SELECT cronjob_name FROM jobs WHERE uid = ?", "CronJob"
	case "ReplicaSet":
		query, ownerKind = "SELECT deployment_name FROM rollout_history WHERE replicaset_uid = ? LIMIT 1", "Deployment"
	default:
		return "", "", false, nil
	}
	var name *string
	err := tx.QueryRowContext(ctx, query, uid).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if name == nil || *name == "" {
		return "", "", false, nil
	}
	return ownerKind, *name, true, nil
}

// LoadLiveSiblingCreatedAt returns created_at of the controller's other live
// pods.
func LoadLiveSiblingCreatedAt(ctx context.Context, tx *sql.Tx, controllerUID, exceptUID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT created_at FROM pods WHERE controller_uid = ? AND uid <> ? AND deleted_at IS NULL", controllerUID, exceptUID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// HasRecentIncidentOrFailure says whether a pod's logs are worth keeping: an
// incident open or closed at or after closedSince, or a container whose
// current or last exit code is non-zero.
func HasRecentIncidentOrFailure(ctx context.Context, tx *sql.Tx, podUID, closedSince string) (bool, error) {
	var n int64
	err := tx.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM incidents WHERE pod_uid = ? AND (closed_at IS NULL OR closed_at >= ?))
     + (SELECT COUNT(*) FROM containers WHERE pod_uid = ? AND (exit_code <> 0 OR last_terminated_exit_code <> 0))`,
		podUID, closedSince, podUID).Scan(&n)
	return n > 0, err
}
