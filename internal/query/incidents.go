package query

import (
	"context"
	"database/sql"
	"strings"

	"github.com/hasanMshawrab/idios/internal/store"
)

// IncidentFilter narrows the triage list. An empty ClusterIDs means every
// cluster; an empty string field means the filter is not applied; a zero ID
// means every incident, and a set one narrows to that single row, which is
// how a stream reloads the row an event named.
type IncidentFilter struct {
	ClusterIDs   []int64
	ID           int64
	State        string
	Category     string
	Namespace    string
	WorkloadKind string
	WorkloadName string
	PodUID       string
	JobUID       string
	NodeName     string
	// PodName keeps the rows of the pod with this exact name.
	PodName string
	// AttentionSince is the attention window's cutoff, a clock.Format string,
	// applied only when State is StateAttention.
	AttentionSince string
}

// attentionExpr derives whether an incident belongs to the attention set: open,
// or closed within the window bound to the parameter, and never dismissed;
// dismissal is checked ahead of the open/closed split rather than only inside
// the closed leg, because a row can be dismissed while still open (closed_at
// NULL) and dismissal must drop it from attention either way. Acknowledgement
// only excludes a closed row: an open, acknowledged row is still attention,
// the way an open row of any kind is.
const attentionExpr = `(i.dismissed_at IS NULL AND (i.closed_at IS NULL OR (i.closed_at >= ? AND i.acknowledged_at IS NULL)))`

// IncidentRow is one line of the triage list: the incident row, its derived
// state, and the pod and container facts the line shows.
type IncidentRow struct {
	store.Incident
	State             string
	PodName           *string
	PodDeletedAt      *string
	PodDeletionReason *string
	ContainerCount    int32
	ExitCode          *int64
	Signal            *int64
}

// incidentColumns is every column of the incidents table, in schema order,
// as scanIncidentRow expects them.
const incidentColumns = `i.id, i.cluster_id, i.namespace, i.subject_kind, i.pod_uid, i.job_uid, i.container_name, i.workload_kind,
       i.workload_name, i.category, i.first_reason, i.last_reason, i.last_message, i.image, i.image_tag, i.image_id,
       i.node_name, i.occurrences, i.opened_at, i.last_seen_at, i.closed_at, i.close_reason, i.acknowledged_at, i.dismissed_at, i.note`

// The container join is on the incident's subject container, so a pod-level
// incident carries no exit code. A container sitting in terminated has had
// its last termination and its lastState is empty on the first one, so its
// current exit code is the one to show.
const incidentSelect = `
SELECT ` + incidentColumns + `,
       ` + stateExpr + `,
       p.name, p.deleted_at, p.deletion_reason,
       (SELECT COUNT(*) FROM containers pc WHERE pc.pod_uid = i.pod_uid),
       COALESCE(c.last_terminated_exit_code, CASE WHEN c.state = 'terminated' THEN c.exit_code END),
       COALESCE(c.last_terminated_signal, CASE WHEN c.state = 'terminated' THEN c.signal END)
FROM incidents i
LEFT JOIN pods p ON p.uid = i.pod_uid
LEFT JOIN containers c ON c.pod_uid = i.pod_uid AND c.name = i.container_name`

func scanIncidentRow(rows *sql.Rows) (IncidentRow, error) {
	var r IncidentRow
	i := &r.Incident
	err := rows.Scan(&i.ID, &i.ClusterID, &i.Namespace, &i.SubjectKind, &i.PodUID, &i.JobUID, &i.ContainerName, &i.WorkloadKind,
		&i.WorkloadName, &i.Category, &i.FirstReason, &i.LastReason, &i.LastMessage, &i.Image, &i.ImageTag, &i.ImageID,
		&i.NodeName, &i.Occurrences, &i.OpenedAt, &i.LastSeenAt, &i.ClosedAt, &i.CloseReason, &i.AcknowledgedAt, &i.DismissedAt, &i.Note,
		&r.State, &r.PodName, &r.PodDeletedAt, &r.PodDeletionReason, &r.ContainerCount, &r.ExitCode, &r.Signal)
	return r, err
}

// ListIncidents returns the triage rows matching f, newest activity first.
// The second result says the limit was hit.
func ListIncidents(ctx context.Context, db store.Querier, f IncidentFilter, p Page) ([]IncidentRow, bool, error) {
	var preds []string
	var args []any
	if pred, next := clusterFilter("i.cluster_id", f.ClusterIDs, args); pred != "" {
		preds, args = append(preds, pred), next
	}
	if f.ID != 0 {
		preds = append(preds, "i.id = ?")
		args = append(args, f.ID)
	}
	switch f.State {
	case "":
	case StateAttention:
		preds = append(preds, attentionExpr)
		args = append(args, f.AttentionSince)
	default:
		preds = append(preds, "("+stateExpr+") = ?")
		args = append(args, f.State)
	}
	for _, eq := range []struct {
		expr  string
		value string
	}{
		{"i.category", f.Category},
		{"i.namespace", f.Namespace},
		{"i.workload_kind", f.WorkloadKind},
		{"i.workload_name", f.WorkloadName},
		{"i.pod_uid", f.PodUID},
		{"i.job_uid", f.JobUID},
		{"i.node_name", f.NodeName},
		{"p.name", f.PodName},
	} {
		if eq.value != "" {
			preds = append(preds, eq.expr+" = ?")
			args = append(args, eq.value)
		}
	}
	limit, args := limitClause(p, args)
	rows, err := db.QueryContext(ctx, incidentSelect+where(preds)+" ORDER BY i.last_seen_at DESC, i.id DESC"+limit, args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	var out []IncidentRow
	for rows.Next() {
		r, err := scanIncidentRow(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	out, truncated := truncate(out, p)
	return out, truncated, nil
}

// RelatedIncidents are the other incidents of subject's pod and of subject's
// Job, newest activity first and bounded by p. Both scopes matter: a pod's
// four incidents are one death seen four ways, and a Job's retries are one
// failure seen once per run. A subject with neither uid is related to
// nothing.
func RelatedIncidents(ctx context.Context, db store.Querier, subject IncidentRow, p Page) ([]IncidentRow, error) {
	var scopes []string
	args := []any{subject.ID}
	for _, uid := range []struct {
		column string
		value  *string
	}{
		{"i.pod_uid", subject.PodUID},
		{"i.job_uid", subject.JobUID},
	} {
		if uid.value != nil && *uid.value != "" {
			scopes = append(scopes, uid.column+" = ?")
			args = append(args, *uid.value)
		}
	}
	if len(scopes) == 0 {
		return nil, nil
	}
	preds := []string{"i.id <> ?", "(" + strings.Join(scopes, " OR ") + ")"}
	limit, args := limitClause(p, args)
	rows, err := collect(ctx, db, scanIncidentRow,
		incidentSelect+where(preds)+" ORDER BY i.last_seen_at DESC, i.id DESC"+limit, args...)
	if err != nil {
		return nil, err
	}
	rows, _ = truncate(rows, p)
	return rows, nil
}

// IncidentCounts are the sidebar numbers, faceted: ByState is counted over
// the incidents of the selected category and ByCategory over those of the
// selected state, so clicking a count never lands on an empty list.
type IncidentCounts struct {
	ByState    map[string]int64
	ByCategory map[string]int64
}

// CountIncidents counts the incidents of clusterIDs, or of every cluster when
// it is empty, cross-filtering each facet by the other's selection. An empty
// state or category is not applied. attentionSince is the attention window's
// cutoff, bound into the grouped attentionExpr column regardless of whether
// state or category selects attention, since ByState always reports the
// attention count alongside every other state.
func CountIncidents(ctx context.Context, db store.Querier, clusterIDs []int64, state, category, attentionSince string) (IncidentCounts, error) {
	out := IncidentCounts{ByState: map[string]int64{}, ByCategory: map[string]int64{}}
	var preds []string
	var whereArgs []any
	if pred, next := clusterFilter("i.cluster_id", clusterIDs, whereArgs); pred != "" {
		preds, whereArgs = append(preds, pred), next
	}
	// attentionExpr's placeholder sits in the SELECT list, ahead of the WHERE
	// clause's own placeholders, so its argument goes first.
	args := append([]any{attentionSince}, whereArgs...)
	rows, err := db.QueryContext(ctx, `SELECT `+stateExpr+`, i.category, `+attentionExpr+`, COUNT(*) FROM incidents i`+
		where(preds)+` GROUP BY 1, 2, 3`, args...)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var rowState, rowCategory string
		var isAttention bool
		var n int64
		if err := rows.Scan(&rowState, &rowCategory, &isAttention, &n); err != nil {
			return out, err
		}
		if category == "" || category == rowCategory {
			out.ByState[rowState] += n
			if isAttention {
				out.ByState[StateAttention] += n
			}
		}
		if state == "" || state == rowState || (state == StateAttention && isAttention) {
			out.ByCategory[rowCategory] += n
		}
	}
	return out, rows.Err()
}
