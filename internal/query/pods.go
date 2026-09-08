package query

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/hasanMshawrab/idios/internal/store"
)

// PodFilter narrows the pod list. An empty ClusterIDs means every cluster,
// an empty string field means the filter is not applied, and a nil Live
// means live and deleted pods together.
type PodFilter struct {
	ClusterIDs   []int64
	Namespace    string
	WorkloadKind string
	WorkloadName string
	Live         *bool
	// PodName keeps the row with this exact name.
	PodName string
	// JobUID keeps the pods this Job started; the column is the pod's
	// controller uid, which is the Job's for a pod a Job created.
	JobUID string
}

// PodRow is one line of the pod list: the pod row, the two counts the line
// shows, and the state its badge takes.
type PodRow struct {
	store.Pod
	OpenIncidents  int32
	ContainerCount int32
	WorstState     string
}

// SiblingPod is one other pod of the same controller, as the pod page's
// rail lists it. WorstOpenCategory is empty when no incident of the pod
// is open.
type SiblingPod struct {
	UID               string
	Name              string
	Phase             string
	DeletedAt         *string
	RestartCount      int64
	Ready             bool
	WorstOpenCategory string
}

// PodDetail is the pod screen: the row, every container, the latest
// condition per type, the incidents on the pod, and the captured files.
type PodDetail struct {
	Pod        PodRow
	Containers []store.Container
	Conditions []store.PodCondition
	Incidents  []IncidentRow
	Artifacts  []store.Artifact
	// Siblings are the controller's other pods in this cluster, five at
	// most; SiblingTotal counts every pod of the controller, this one
	// included, because the rail's label names the set the pod belongs to.
	Siblings     []SiblingPod
	SiblingTotal int64
	// ContainerGrafanaURLs is the Explore link for each container's own run,
	// keyed by container name; nil when the cluster is unconfigured for
	// Grafana.
	ContainerGrafanaURLs map[string]string
}

// PodHistoryRows is what one pod did over time, oldest first.
type PodHistoryRows struct {
	Transitions []store.ContainerStateHistory
	Conditions  []store.PodCondition
}

// An incident someone still has to deal with is neither closed nor
// dismissed. The badge ranks only the containers that serve the pod's
// traffic: an init container ends terminated on every healthy pod, so
// counting it would paint the whole list red.
const podSelect = `
SELECT ` + podColumns + `,
       (SELECT COUNT(*) FROM incidents i WHERE i.pod_uid = p.uid AND i.closed_at IS NULL AND i.dismissed_at IS NULL),
       (SELECT COUNT(*) FROM containers c WHERE c.pod_uid = p.uid),
       (SELECT CASE MAX(CASE c.state WHEN 'terminated' THEN 3 WHEN 'waiting' THEN 2 ELSE 1 END)
                 WHEN 3 THEN 'terminated' WHEN 2 THEN 'waiting' WHEN 1 THEN 'running' ELSE '' END
          FROM containers c WHERE c.pod_uid = p.uid AND c.kind IN ('app', 'sidecar'))
FROM pods p`

func scanPodRow(r *sql.Rows) (PodRow, error) {
	var row PodRow
	p := &row.Pod
	err := r.Scan(&p.UID, &p.ClusterID, &p.Namespace, &p.Name, &p.NodeName, &p.Phase, &p.StatusReason, &p.StatusMessage,
		&p.DeletionRequestedAt, &p.QOSClass, &p.ControllerKind, &p.ControllerName, &p.ControllerUID,
		&p.WorkloadKind, &p.WorkloadName, &p.CreatedAt, &p.StartedAt, &p.FirstSeenAt, &p.LastSeenAt,
		&p.DeletedAt, &p.DeletionSource, &p.DeletionReason,
		&row.OpenIncidents, &row.ContainerCount, &row.WorstState)
	return row, err
}

// ListPods returns the pod rows matching f, most recently seen first. The
// second result says the limit was hit.
func ListPods(ctx context.Context, db store.Querier, f PodFilter, p Page) ([]PodRow, bool, error) {
	var preds []string
	var args []any
	if pred, next := clusterFilter("p.cluster_id", f.ClusterIDs, args); pred != "" {
		preds, args = append(preds, pred), next
	}
	for _, eq := range []struct {
		column string
		value  string
	}{
		{"p.namespace", f.Namespace},
		{"p.name", f.PodName},
		{"p.controller_uid", f.JobUID},
		{"p.workload_kind", f.WorkloadKind},
		{"p.workload_name", f.WorkloadName},
	} {
		if eq.value != "" {
			preds = append(preds, eq.column+" = ?")
			args = append(args, eq.value)
		}
	}
	if f.Live != nil {
		if *f.Live {
			preds = append(preds, "p.deleted_at IS NULL")
		} else {
			preds = append(preds, "p.deleted_at IS NOT NULL")
		}
	}
	limit, args := limitClause(p, args)
	// The uid breaks ties: pods written in the same second share a
	// last_seen_at, and without it a page would shuffle between requests.
	out, err := collect(ctx, db, scanPodRow, podSelect+where(preds)+" ORDER BY p.last_seen_at DESC, p.uid"+limit, args...)
	if err != nil {
		return nil, false, err
	}
	out, truncated := truncate(out, p)
	return out, truncated, nil
}

// Containers come back in the order the pod runs them, so a reader can
// follow the startup sequence down the screen.
const podContainerSelect = `
SELECT ` + containerColumns + ` FROM containers WHERE pod_uid = ?
ORDER BY CASE kind WHEN 'init' THEN 0 WHEN 'sidecar' THEN 1 WHEN 'app' THEN 2 ELSE 3 END, name`

// GetPod returns everything the pod screen shows, or nil when no pod has
// that uid.
func GetPod(ctx context.Context, db store.Querier, uid string) (*PodDetail, error) {
	pods, err := collect(ctx, db, scanPodRow, podSelect+" WHERE p.uid = ?", uid)
	if err != nil || len(pods) == 0 {
		return nil, err
	}
	d := PodDetail{Pod: pods[0]}
	if d.Containers, err = collect(ctx, db, scanContainer, podContainerSelect, uid); err != nil {
		return nil, err
	}
	if d.Conditions, err = latestConditions(ctx, db, uid); err != nil {
		return nil, err
	}
	if d.Incidents, _, err = ListIncidents(ctx, db, IncidentFilter{PodUID: uid}, Page{}); err != nil {
		return nil, err
	}
	d.Artifacts, err = collect(ctx, db, scanArtifact, "SELECT "+artifactColumns+
		" FROM artifacts WHERE pod_uid = ? ORDER BY container_name, kind, restart_count", uid)
	if err != nil {
		return nil, err
	}
	if d.Siblings, err = podSiblings(ctx, db, d.Pod.ClusterID, d.Pod.ControllerUID, uid); err != nil {
		return nil, err
	}
	if d.SiblingTotal, err = siblingTotal(ctx, db, d.Pod.ClusterID, d.Pod.ControllerUID); err != nil {
		return nil, err
	}
	if err := attachPodGrafana(ctx, db, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// siblingLimit is how many siblings the pod page's rail shows; the
// total says how many more there are.
const siblingLimit = 5

func scanSibling(rows *sql.Rows) (SiblingPod, error) {
	var s SiblingPod
	var ready int64
	err := rows.Scan(&s.UID, &s.Name, &s.Phase, &s.DeletedAt, &s.RestartCount, &ready, &s.WorstOpenCategory)
	s.Ready = ready == 1
	return s, err
}

// podSiblings returns the controller's other pods in the same cluster,
// pods with an open incident first, worst category first, then live pods,
// then deleted ones, newest created first inside each group, at most
// siblingLimit of them. Two clusters can hold the same controller uid,
// since a uid is only unique within one; an empty controller uid means
// the pod has no controller, which makes it nobody's sibling. An
// incident dismissed while open is not the person's problem any more, so
// it does not colour the sibling.
func podSiblings(ctx context.Context, db store.Querier, clusterID int64, controllerUID, podUID string) ([]SiblingPod, error) {
	if controllerUID == "" {
		return nil, nil
	}
	return collect(ctx, db, scanSibling, `
SELECT p.uid, p.name, p.phase, p.deleted_at,
       COALESCE((SELECT SUM(c.restart_count) FROM containers c WHERE c.pod_uid = p.uid), 0),
       (SELECT COUNT(*) FROM containers c WHERE c.pod_uid = p.uid AND c.kind IN ('app', 'sidecar')) > 0
       AND NOT EXISTS (SELECT 1 FROM containers c WHERE c.pod_uid = p.uid AND c.kind IN ('app', 'sidecar') AND c.ready = 0),
       COALESCE((SELECT i.category FROM incidents i
                  WHERE i.pod_uid = p.uid AND i.closed_at IS NULL AND i.dismissed_at IS NULL
                  ORDER BY `+categoryRank("i.category")+`, i.id LIMIT 1), '') AS worst
FROM pods p
WHERE p.cluster_id = ? AND p.controller_uid = ? AND p.uid <> ?
ORDER BY CASE WHEN worst <> '' THEN 0 WHEN p.deleted_at IS NULL THEN 1 ELSE 2 END,
         `+categoryRank("worst")+`, p.created_at DESC, p.uid
LIMIT `+strconv.Itoa(siblingLimit), clusterID, controllerUID, podUID)
}

// siblingTotal counts every pod of the controller in the cluster, the
// pod itself included; zero for a pod with no controller.
func siblingTotal(ctx context.Context, db store.Querier, clusterID int64, controllerUID string) (int64, error) {
	if controllerUID == "" {
		return 0, nil
	}
	var n int64
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pods WHERE cluster_id = ? AND controller_uid = ?`,
		clusterID, controllerUID).Scan(&n)
	return n, err
}

// PodEvents returns every event about the pod, attached to an incident or
// not, newest first. The second result says the limit was hit.
func PodEvents(ctx context.Context, db store.Querier, uid string, p Page) ([]store.K8sEvent, bool, error) {
	args := []any{uid}
	limit, args := limitClause(p, args)
	out, err := collect(ctx, db, scanEvent, "SELECT "+eventColumns+
		" FROM k8s_events WHERE involved_uid = ? ORDER BY last_ts DESC, id DESC"+limit, args...)
	if err != nil {
		return nil, false, err
	}
	out, truncated := truncate(out, p)
	return out, truncated, nil
}

const historyColumns = `id, pod_uid, container_name, incident_id, image, image_id, container_id, state, reason,
       message, exit_code, signal, restart_count, category, k8s_started_at, k8s_finished_at, observed_at, gap_reconstructed`

func scanHistory(r *sql.Rows) (store.ContainerStateHistory, error) {
	var h store.ContainerStateHistory
	var reconstructed int64
	err := r.Scan(&h.ID, &h.PodUID, &h.ContainerName, &h.IncidentID, &h.Image, &h.ImageID, &h.ContainerID, &h.State, &h.Reason,
		&h.Message, &h.ExitCode, &h.Signal, &h.RestartCount, &h.Category, &h.K8sStartedAt, &h.K8sFinishedAt, &h.ObservedAt, &reconstructed)
	h.GapReconstructed = reconstructed == 1
	return h, err
}

// PodHistory returns the pod's container transitions and its condition
// changes, both oldest first, as the story reads.
func PodHistory(ctx context.Context, db store.Querier, uid string) (PodHistoryRows, error) {
	var out PodHistoryRows
	var err error
	out.Transitions, err = collect(ctx, db, scanHistory, "SELECT "+historyColumns+
		" FROM container_state_history WHERE pod_uid = ? ORDER BY observed_at, id", uid)
	if err != nil {
		return PodHistoryRows{}, err
	}
	out.Conditions, err = collect(ctx, db, scanCondition, "SELECT "+conditionColumns+
		" FROM pod_condition_history WHERE pod_uid = ? ORDER BY observed_at, id", uid)
	if err != nil {
		return PodHistoryRows{}, err
	}
	return out, nil
}
