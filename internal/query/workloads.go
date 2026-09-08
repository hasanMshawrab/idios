package query

import (
	"context"
	"database/sql"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// WorkloadFilter narrows the workload list. An empty ClusterIDs means every
// cluster; an empty Namespace means the filter is not applied.
type WorkloadFilter struct {
	ClusterIDs []int64
	Namespace  string
}

// TagCount is how often one image tag was the one running when an incident
// opened. An empty Tag is an image pinned by digest, which carries no tag.
type TagCount struct {
	Tag   string
	Count int64
}

// WorkloadKey addresses one workload: the identity chain down to the name a
// service keeps while its pods rotate. PodUID splits kind "none" into one key
// per controller-less pod; it is empty for every real workload.
type WorkloadKey struct {
	ClusterID int64
	Namespace string
	Kind      string
	Name      string
	PodUID    string
}

// WorkloadRow is one line of the workload tree: the identity, what has gone
// wrong under it, and how many of its pods are still there.
// IncidentsByCategory is always present, empty when the workload has never
// failed; the slices are nil when they hold nothing. PodUID and PodName are
// set only for kind "none": a controller-less pod is its own row rather than
// one that collapses with every other pod the namespace has no controller
// for.
type WorkloadRow struct {
	ClusterID           int64
	Namespace           string
	WorkloadKind        string
	WorkloadName        string
	PodUID              string
	PodName             string
	IncidentsByCategory map[string]int64
	OpenIncidents       int64
	Occurrences         int64
	ImageTags           []TagCount
	LivePods            int64
	DeletedPods         int64
}

// Rollout is one ReplicaSet of a Deployment, with the images it ran and the
// incidents its pods raised.
type Rollout struct {
	ReplicaSetUID     string
	ReplicaSetName    string
	Revision          *int64
	Images            []string
	CreatedAt         string
	Replicas          *int64
	ReadyReplicas     *int64
	AvailableReplicas *int64
	FirstSeenAt       string
	LastSeenAt        string
	DeletedAt         *string
	Incidents         int64
}

// HourBucket is one hour of the restart chart. Reconstructed marks an hour
// holding a transition inferred from a counter jump rather than observed.
type HourBucket struct {
	Hour          string
	Restarts      int64
	Reconstructed bool
}

// WorkloadDetail is the workload screen: the list row, the rollouts behind
// it, its restarts over time, its pods, and the runs of a CronJob or Job
// workload, which answer whether this failure recurs.
type WorkloadDetail struct {
	Workload       WorkloadRow
	Rollouts       []Rollout
	RestartsByHour []HourBucket
	Pods           []PodRow
	PodsTruncated  bool
	Runs           []JobRow
}

// The pod_uid equality is the same CASE both arms of the union compute: a
// plain i.pod_uid = k.pod_uid would compare a real workload's empty
// k.pod_uid against the real uid every pod incident carries, whatever its
// kind, and drop every one of that workload's incidents.
const incidentOfWorkload = `i.cluster_id = k.cluster_id AND i.namespace = k.namespace
          AND i.workload_kind = k.workload_kind AND i.workload_name = k.workload_name
          AND (CASE WHEN i.workload_kind = 'none' THEN COALESCE(i.pod_uid, '') ELSE '' END) = k.pod_uid`

const podOfWorkload = `p.cluster_id = k.cluster_id AND p.namespace = k.namespace
          AND p.workload_kind = k.workload_kind AND p.workload_name = k.workload_name
          AND (CASE WHEN p.workload_kind = 'none' THEN p.uid ELSE '' END) = k.pod_uid`

// A workload exists as soon as either table names it: a service whose pods
// are healthy has no incidents and must still stand in the tree. An incident
// nobody has finished with is neither closed nor dismissed.
//
// The alias on the union's first arm is not optional: a compound SELECT
// takes its column names from its first arm, and an unaliased CASE gets an
// implicit name nothing can reference, so the outer query fails to prepare.
const workloadSelect = `
SELECT k.cluster_id, k.namespace, k.workload_kind, k.workload_name, k.pod_uid,
       COALESCE((SELECT name FROM pods WHERE uid = k.pod_uid), '') AS pod_name,
       (SELECT COUNT(*) FROM incidents i WHERE ` + incidentOfWorkload + `
          AND i.closed_at IS NULL AND i.dismissed_at IS NULL),
       (SELECT COALESCE(SUM(i.occurrences), 0) FROM incidents i WHERE ` + incidentOfWorkload + `),
       (SELECT COUNT(*) FROM pods p WHERE ` + podOfWorkload + ` AND p.deleted_at IS NULL),
       (SELECT COUNT(*) FROM pods p WHERE ` + podOfWorkload + ` AND p.deleted_at IS NOT NULL)
FROM (SELECT cluster_id, namespace, workload_kind, workload_name,
             CASE WHEN workload_kind = 'none' THEN COALESCE(pod_uid, '') ELSE '' END AS pod_uid
      FROM incidents
      UNION
      SELECT cluster_id, namespace, workload_kind, workload_name,
             CASE WHEN workload_kind = 'none' THEN uid ELSE '' END
      FROM pods) k`

const workloadOrder = ` ORDER BY k.cluster_id, k.namespace, k.workload_kind, k.workload_name, pod_name, k.pod_uid`

func scanWorkloadRow(r *sql.Rows) (WorkloadRow, error) {
	w := WorkloadRow{IncidentsByCategory: map[string]int64{}}
	err := r.Scan(&w.ClusterID, &w.Namespace, &w.WorkloadKind, &w.WorkloadName, &w.PodUID, &w.PodName,
		&w.OpenIncidents, &w.Occurrences, &w.LivePods, &w.DeletedPods)
	return w, err
}

func (w WorkloadRow) key() WorkloadKey {
	return WorkloadKey{
		ClusterID: w.ClusterID, Namespace: w.Namespace, Kind: w.WorkloadKind, Name: w.WorkloadName, PodUID: w.PodUID,
	}
}

// filterPredicates narrows a table aliased as alias to the requested scope.
func filterPredicates(alias string, f WorkloadFilter) ([]string, []any) {
	var preds []string
	var args []any
	if pred, next := clusterFilter(alias+".cluster_id", f.ClusterIDs, args); pred != "" {
		preds, args = append(preds, pred), next
	}
	if f.Namespace != "" {
		preds = append(preds, alias+".namespace = ?")
		args = append(args, f.Namespace)
	}
	return preds, args
}

// keyPredicates narrows a table aliased as alias to one workload. The
// pod_uid equality is a CASE and not a plain column comparison, for the
// reason incidentOfWorkload's comment gives: incidents.pod_uid holds a real
// uid on every pod incident, so a plain equality against an empty k.PodUID
// would drop every real workload's incidents. pods has no pod_uid column of
// its own, so its twin reads uid instead.
func keyPredicates(alias string, k WorkloadKey) ([]string, []any) {
	podCol := alias + ".pod_uid"
	if alias == "p" {
		podCol = alias + ".uid"
	}
	return []string{
			alias + ".cluster_id = ?", alias + ".namespace = ?",
			alias + ".workload_kind = ?", alias + ".workload_name = ?",
			"CASE WHEN " + alias + ".workload_kind = 'none' THEN COALESCE(" + podCol + ", '') ELSE '' END = ?",
		},
		[]any{k.ClusterID, k.Namespace, k.Kind, k.Name, k.PodUID}
}

// ListWorkloads returns one row per workload seen in either table, ordered
// down the identity chain. The second result says the limit was hit.
func ListWorkloads(ctx context.Context, db store.Querier, f WorkloadFilter, p Page) ([]WorkloadRow, bool, error) {
	preds, args := filterPredicates("k", f)
	limit, limitArgs := limitClause(p, args)
	rows, err := collect(ctx, db, scanWorkloadRow, workloadSelect+where(preds)+workloadOrder+limit, limitArgs...)
	if err != nil {
		return nil, false, err
	}
	rows, truncated := truncate(rows, p)
	incidentPreds, incidentArgs := filterPredicates("i", f)
	if err := attachIncidentFacts(ctx, db, rows, incidentPreds, incidentArgs); err != nil {
		return nil, false, err
	}
	return rows, truncated, nil
}

// GetWorkload returns everything the workload screen shows, or nil when
// neither an incident nor a pod names that workload. live and p bound the
// pod list the same way ListPods does for the standalone endpoint.
func GetWorkload(ctx context.Context, db store.Querier, k WorkloadKey, live *bool, p Page) (*WorkloadDetail, error) {
	preds, args := keyPredicates("k", k)
	rows, err := collect(ctx, db, scanWorkloadRow, workloadSelect+where(preds), args...)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	incidentPreds, incidentArgs := keyPredicates("i", k)
	if err := attachIncidentFacts(ctx, db, rows, incidentPreds, incidentArgs); err != nil {
		return nil, err
	}
	d := &WorkloadDetail{Workload: rows[0]}
	// Only a Deployment has ReplicaSets under it; rollout_history is keyed by
	// the Deployment name and holds nothing for any other kind.
	if k.Kind == "Deployment" {
		if d.Rollouts, err = workloadRollouts(ctx, db, k); err != nil {
			return nil, err
		}
	}
	if d.RestartsByHour, err = restartsByHour(ctx, db, k); err != nil {
		return nil, err
	}
	if d.Runs, err = workloadRuns(ctx, db, k, p); err != nil {
		return nil, err
	}
	d.Pods, d.PodsTruncated, err = ListPods(ctx, db, PodFilter{
		ClusterIDs: []int64{k.ClusterID}, Namespace: k.Namespace, WorkloadKind: k.Kind, WorkloadName: k.Name,
		Live: live,
	}, p)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// workloadRuns returns the runs of a CronJob or Job workload, newest first
// and bounded by p. A CronJob's runs are the Jobs it created; a Job workload
// is one run, named by the row itself. No other kind has runs at all.
func workloadRuns(ctx context.Context, db store.Querier, k WorkloadKey, p Page) ([]JobRow, error) {
	f := JobFilter{ClusterIDs: []int64{k.ClusterID}, Namespace: k.Namespace}
	switch k.Kind {
	case "CronJob":
		f.CronJobName = k.Name
	case "Job":
		f.Name = k.Name
	default:
		return nil, nil
	}
	rows, _, err := ListJobs(ctx, db, f, p)
	return rows, err
}

// attachIncidentFacts fills the two grouped fields of rows. They are read in
// their own passes because a row of the list is one workload and these are
// many rows per workload.
func attachIncidentFacts(ctx context.Context, db store.Querier, rows []WorkloadRow, preds []string, args []any) error {
	if len(rows) == 0 {
		return nil
	}
	byKey := map[WorkloadKey]*WorkloadRow{}
	for i := range rows {
		byKey[rows[i].key()] = &rows[i]
	}
	const podUIDCase = `CASE WHEN i.workload_kind = 'none' THEN COALESCE(i.pod_uid, '') ELSE '' END`
	categories, err := collect(ctx, db, scanWorkloadCount, `
SELECT i.cluster_id, i.namespace, i.workload_kind, i.workload_name, `+podUIDCase+`, i.category, COUNT(*)
FROM incidents i`+where(preds)+" GROUP BY 1, 2, 3, 4, 5, 6", args...)
	if err != nil {
		return err
	}
	for _, c := range categories {
		if row := byKey[c.key]; row != nil {
			row.IncidentsByCategory[c.value] = c.count
		}
	}
	// An incident with no image at all names no version, so it contributes no
	// tag; only a digest-pinned image counts as the empty tag.
	tagPreds := append(append([]string{}, preds...), "i.image IS NOT NULL")
	tags, err := collect(ctx, db, scanWorkloadCount, `
SELECT i.cluster_id, i.namespace, i.workload_kind, i.workload_name, `+podUIDCase+`, COALESCE(i.image_tag, ''), COUNT(*)
FROM incidents i`+where(tagPreds)+" GROUP BY 1, 2, 3, 4, 5, 6 ORDER BY 1, 2, 3, 4, 5, COUNT(*) DESC, 6", args...)
	if err != nil {
		return err
	}
	for _, t := range tags {
		if row := byKey[t.key]; row != nil {
			row.ImageTags = append(row.ImageTags, TagCount{Tag: t.value, Count: t.count})
		}
	}
	return nil
}

// workloadCount is one grouped count of a workload's incidents, by whatever
// column the query grouped on.
type workloadCount struct {
	key   WorkloadKey
	value string
	count int64
}

func scanWorkloadCount(r *sql.Rows) (workloadCount, error) {
	var c workloadCount
	err := r.Scan(&c.key.ClusterID, &c.key.Namespace, &c.key.Kind, &c.key.Name, &c.key.PodUID, &c.value, &c.count)
	return c, err
}

// A ReplicaSet holds one row per container, so the rows are grouped in Go to
// keep the images of a revision together. A revision the annotation never
// carried sorts last, which SQLite's DESC does on its own. The incident count
// is scoped to the cluster: two clusters can hold the same ReplicaSet uid.
const rolloutSelect = `
SELECT r.replicaset_uid, r.replicaset_name, r.revision, r.created_at, r.replicas, r.ready_replicas, r.available_replicas,
       r.first_seen_at, r.last_seen_at, r.deleted_at, r.image,
       (SELECT COUNT(*) FROM incidents i JOIN pods p ON p.uid = i.pod_uid
          WHERE p.controller_uid = r.replicaset_uid
            AND p.cluster_id = r.cluster_id AND i.cluster_id = r.cluster_id)
FROM rollout_history r
WHERE r.cluster_id = ? AND r.namespace = ? AND r.deployment_name = ?
ORDER BY r.revision DESC, r.replicaset_uid, r.container_name`

type rolloutRow struct {
	rollout Rollout
	image   string
}

func scanRolloutRow(r *sql.Rows) (rolloutRow, error) {
	var row rolloutRow
	o := &row.rollout
	err := r.Scan(&o.ReplicaSetUID, &o.ReplicaSetName, &o.Revision, &o.CreatedAt, &o.Replicas, &o.ReadyReplicas, &o.AvailableReplicas,
		&o.FirstSeenAt, &o.LastSeenAt, &o.DeletedAt, &row.image, &o.Incidents)
	return row, err
}

func workloadRollouts(ctx context.Context, db store.Querier, k WorkloadKey) ([]Rollout, error) {
	rows, err := collect(ctx, db, scanRolloutRow, rolloutSelect, k.ClusterID, k.Namespace, k.Name)
	if err != nil {
		return nil, err
	}
	var out []Rollout
	for _, row := range rows {
		if n := len(out); n > 0 && out[n-1].ReplicaSetUID == row.rollout.ReplicaSetUID {
			out[n-1].Images = append(out[n-1].Images, row.image)
			continue
		}
		row.rollout.Images = []string{row.image}
		out = append(out, row.rollout)
	}
	return out, nil
}

// hourSuffix is everything after the hour in a stored timestamp. The layout
// is fixed width, so an hour bucket is the first thirteen characters of the
// stamp and this constant tail.
var hourSuffix = clock.Format(time.Time{})[13:]

// A restart is an increment of restart_count, not a row in a given state: a
// history row records the state a container was seen in, and a crash loop is
// usually only ever seen waiting in CrashLoopBackOff, its dead instance never
// observed. The counter only moves forward, so the increment between two
// consecutive rows of one container is the number of restarts the later row
// witnessed; the first row of a container witnesses none, having nothing
// before it, which is also the limitation of counting this way: the restarts
// a container had already accumulated when it was first seen are lost.
// The reconstructed flag marks an hour whose history was partly inferred, so
// it reads every row of the hour and not only the rows that carried an
// increment; an hour that inferred nothing new is no hour of restarts and
// does not become a bucket.
const restartsSelect = `
SELECT substr(observed_at, 1, 13), SUM(CASE WHEN delta > 0 THEN delta ELSE 0 END), MAX(gap_reconstructed)
FROM (SELECT h.observed_at, h.gap_reconstructed,
             h.restart_count - LAG(h.restart_count) OVER (
                 PARTITION BY h.pod_uid, h.container_name ORDER BY h.observed_at, h.id) AS delta
      FROM container_state_history h JOIN pods p ON p.uid = h.pod_uid
      WHERE p.cluster_id = ? AND p.namespace = ? AND p.workload_kind = ? AND p.workload_name = ?)
GROUP BY 1 HAVING SUM(CASE WHEN delta > 0 THEN delta ELSE 0 END) > 0 ORDER BY 1`

func scanHourBucket(r *sql.Rows) (HourBucket, error) {
	var b HourBucket
	var reconstructed int64
	err := r.Scan(&b.Hour, &b.Restarts, &reconstructed)
	b.Hour += hourSuffix
	b.Reconstructed = reconstructed == 1
	return b, err
}

func restartsByHour(ctx context.Context, db store.Querier, k WorkloadKey) ([]HourBucket, error) {
	return collect(ctx, db, scanHourBucket, restartsSelect, k.ClusterID, k.Namespace, k.Kind, k.Name)
}

// JobFilter narrows the job list. An empty ClusterIDs means every cluster; an
// empty string field means the filter is not applied; a nil Live means both
// the runs still in the cluster and the deleted ones. UID names one run and is
// the only filter that does. CronJobName and Name are the filters a workload
// row can ask with: the row is a name chain and carries no uid, and a Job
// workload's name is the run's own.
type JobFilter struct {
	ClusterIDs  []int64
	UID         string
	Namespace   string
	Name        string
	CronJobUID  string
	CronJobName string
	Live        *bool
	Failed      bool
}

// JobCounts says how many runs the scope of a JobFilter holds, so a bounded
// page can say how many rows it stands for. Live and Failed of the filter
// are not applied: they are what the counts break the scope down by.
type JobCounts struct {
	Total  int64
	Failed int64
	Live   int64
}

// JobRow is one run in the job list. Complete says the run finished its work,
// which only the condition reports.
type JobRow struct {
	store.Job
	Complete bool
}

const jobColumns = `uid, cluster_id, namespace, name, cronjob_uid, cronjob_name, active, succeeded, failed,
       backoff_limit, completions, parallelism, active_deadline_seconds, restart_policy, condition_type,
       condition_reason, condition_message, created_at, started_at, finished_at, first_seen_at, last_seen_at, deleted_at`

// Success is read from the condition and never from the counters: failed can
// exceed the backoff limit, and an OnFailure job's retries never touch it.
// The comparison is wrapped because a running job has no condition at all.
const jobSelect = `SELECT ` + jobColumns + `, COALESCE(condition_type = 'Complete', 0) FROM jobs`

func scanJobRow(r *sql.Rows) (JobRow, error) {
	var row JobRow
	j := &row.Job
	var complete int64
	err := r.Scan(&j.UID, &j.ClusterID, &j.Namespace, &j.Name, &j.CronJobUID, &j.CronJobName, &j.Active, &j.Succeeded,
		&j.Failed, &j.BackoffLimit, &j.Completions, &j.Parallelism, &j.ActiveDeadlineSeconds, &j.RestartPolicy, &j.ConditionType,
		&j.ConditionReason, &j.ConditionMessage, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.FirstSeenAt,
		&j.LastSeenAt, &j.DeletedAt, &complete)
	row.Complete = complete == 1
	return row, err
}

// jobScope is the part of f that both the list and its counts apply.
func jobScope(f JobFilter) ([]string, []any) {
	var preds []string
	var args []any
	if pred, next := clusterFilter("cluster_id", f.ClusterIDs, args); pred != "" {
		preds, args = append(preds, pred), next
	}
	for _, eq := range []struct {
		column string
		value  string
	}{
		// The primary key is the narrowest predicate there is.
		{"uid", f.UID},
		{"namespace", f.Namespace},
		{"name", f.Name},
		{"cronjob_uid", f.CronJobUID},
		{"cronjob_name", f.CronJobName},
	} {
		if eq.value != "" {
			preds = append(preds, eq.column+" = ?")
			args = append(args, eq.value)
		}
	}
	return preds, args
}

// A failed run is one whose condition says so. The failed counter counts
// pods and passes the backoff limit on a run that then succeeds, so it is
// never read for this.
const jobFailed = "condition_type = 'Failed'"

// ListJobs returns the job rows matching f, most recently started first. A
// job that never started sorts last, which SQLite's DESC does on its own.
// The second result says the limit was hit.
func ListJobs(ctx context.Context, db store.Querier, f JobFilter, p Page) ([]JobRow, bool, error) {
	preds, args := jobScope(f)
	if f.Live != nil {
		if *f.Live {
			preds = append(preds, "deleted_at IS NULL")
		} else {
			preds = append(preds, "deleted_at IS NOT NULL")
		}
	}
	if f.Failed {
		preds = append(preds, jobFailed)
	}
	limit, args := limitClause(p, args)
	out, err := collect(ctx, db, scanJobRow, jobSelect+where(preds)+" ORDER BY started_at DESC, uid"+limit, args...)
	if err != nil {
		return nil, false, err
	}
	out, truncated := truncate(out, p)
	return out, truncated, nil
}

// CountJobs counts the runs in the scope of f.
func CountJobs(ctx context.Context, db store.Querier, f JobFilter) (JobCounts, error) {
	preds, args := jobScope(f)
	var c JobCounts
	err := db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(CASE WHEN `+jobFailed+` THEN 1 END),
       COUNT(CASE WHEN deleted_at IS NULL THEN 1 END) FROM jobs`+where(preds), args...).
		Scan(&c.Total, &c.Failed, &c.Live)
	return c, err
}
