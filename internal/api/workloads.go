package api

import (
	"cmp"
	"context"
	"slices"
	"strconv"

	sebufhttp "github.com/SebastienMelki/sebuf/http"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query"
)

// ListWorkloads answers the workload tree.
func (s *Server) ListWorkloads(ctx context.Context, req *idiosv1.ListWorkloadsRequest) (*idiosv1.WorkloadsResponse, error) {
	if err := validateLimit(req.GetLimit()); err != nil {
		return nil, err
	}
	rows, truncated, err := query.ListWorkloads(ctx, s.db, query.WorkloadFilter{
		ClusterIDs: req.GetClusterIds(),
		Namespace:  req.GetNamespace(),
	}, query.Page{Limit: s.limit(req.GetLimit())})
	if err != nil {
		return nil, err
	}
	return &idiosv1.WorkloadsResponse{Workloads: mapAll(rows, workloadRow), Truncated: truncated}, nil
}

// GetWorkload answers the workload page.
func (s *Server) GetWorkload(ctx context.Context, req *idiosv1.GetWorkloadRequest) (*idiosv1.WorkloadDetail, error) {
	var violations []*sebufhttp.FieldViolation
	live := boolFilter("pods_live", req.GetPodsLive(), &violations)
	if v := limitViolation(req.GetPodsLimit()); v != nil {
		violations = append(violations, v)
	}
	if len(violations) > 0 {
		return nil, &sebufhttp.ValidationError{Violations: violations}
	}
	key := query.WorkloadKey{
		ClusterID: req.GetClusterId(), Namespace: req.GetNamespace(),
		Kind: req.GetKind(), Name: req.GetName(),
	}
	d, err := query.GetWorkload(ctx, s.db, key, live, query.Page{Limit: s.limit(req.GetPodsLimit())})
	if err != nil {
		return nil, err
	}
	if d == nil {
		id := strconv.FormatInt(key.ClusterID, 10) + "/" + key.Namespace + "/" + key.Kind + "/" + key.Name
		return nil, &notFoundError{what: "workload", id: id}
	}
	return workloadDetail(d), nil
}

// ListJobs answers the job list with the counts of the scope it was cut from.
func (s *Server) ListJobs(ctx context.Context, req *idiosv1.ListJobsRequest) (*idiosv1.JobsResponse, error) {
	var violations []*sebufhttp.FieldViolation
	live := boolFilter("live", req.GetLive(), &violations)
	failed := boolFilter("failed", req.GetFailed(), &violations)
	if v := limitViolation(req.GetLimit()); v != nil {
		violations = append(violations, v)
	}
	if len(violations) > 0 {
		return nil, &sebufhttp.ValidationError{Violations: violations}
	}
	f := query.JobFilter{
		UID:         req.GetJobUid(),
		ClusterIDs:  req.GetClusterIds(),
		Namespace:   req.GetNamespace(),
		CronJobUID:  req.GetCronjobUid(),
		CronJobName: req.GetCronjobName(),
		Live:        live,
		Failed:      failed != nil && *failed,
	}
	rows, truncated, err := query.ListJobs(ctx, s.db, f, query.Page{Limit: s.limit(req.GetLimit())})
	if err != nil {
		return nil, err
	}
	counts, err := query.CountJobs(ctx, s.db, f)
	if err != nil {
		return nil, err
	}
	return &idiosv1.JobsResponse{
		Jobs: mapAll(rows, jobRow), Truncated: truncated,
		Total: int32(counts.Total), FailedTotal: int32(counts.Failed),
	}, nil
}

// workloadRow maps one line of the workload tree. The categories are sorted
// into the enum's order so the tree renders the same way twice; the tags keep
// the order the query put them in, commonest first.
func workloadRow(w query.WorkloadRow) *idiosv1.WorkloadRow {
	out := &idiosv1.WorkloadRow{
		ClusterId:     w.ClusterID,
		Namespace:     w.Namespace,
		WorkloadKind:  w.WorkloadKind,
		WorkloadName:  w.WorkloadName,
		PodUid:        w.PodUID,
		PodName:       w.PodName,
		OpenIncidents: int32(w.OpenIncidents),
		Occurrences:   int32(w.Occurrences),
		ImageTags:     mapAll(w.ImageTags, tagCount),
		LivePods:      int32(w.LivePods),
		DeletedPods:   int32(w.DeletedPods),
	}
	for category, n := range w.IncidentsByCategory {
		out.IncidentsByCategory = append(out.IncidentsByCategory,
			&idiosv1.CategoryCount{Category: categories[category], Count: int32(n)})
	}
	slices.SortFunc(out.IncidentsByCategory, func(a, b *idiosv1.CategoryCount) int {
		return cmp.Compare(a.GetCategory(), b.GetCategory())
	})
	return out
}

// tagCount maps one image tag and how often it was running at an open.
func tagCount(t query.TagCount) *idiosv1.TagCount {
	return &idiosv1.TagCount{Tag: t.Tag, Count: int32(t.Count)}
}

// workloadDetail maps everything the workload page shows.
func workloadDetail(d *query.WorkloadDetail) *idiosv1.WorkloadDetail {
	return &idiosv1.WorkloadDetail{
		Workload:       workloadRow(d.Workload),
		Rollouts:       mapAll(d.Rollouts, rollout),
		RestartsByHour: mapAll(d.RestartsByHour, hourBucket),
		Pods:           mapAll(d.Pods, podRow),
		PodsTruncated:  d.PodsTruncated,
		Runs:           mapAll(d.Runs, jobRow),
	}
}

// rollout maps one ReplicaSet of a Deployment.
func rollout(r query.Rollout) *idiosv1.Rollout {
	return &idiosv1.Rollout{
		ReplicasetUid:     r.ReplicaSetUID,
		ReplicasetName:    r.ReplicaSetName,
		Revision:          r.Revision,
		Images:            r.Images,
		CreatedAt:         r.CreatedAt,
		Replicas:          int32Ptr(r.Replicas),
		ReadyReplicas:     int32Ptr(r.ReadyReplicas),
		AvailableReplicas: int32Ptr(r.AvailableReplicas),
		FirstSeenAt:       r.FirstSeenAt,
		LastSeenAt:        r.LastSeenAt,
		DeletedAt:         r.DeletedAt,
		Incidents:         int32(r.Incidents),
	}
}

// hourBucket maps one hour of the restart chart.
func hourBucket(b query.HourBucket) *idiosv1.HourBucket {
	return &idiosv1.HourBucket{Hour: b.Hour, Restarts: int32(b.Restarts), Reconstructed: b.Reconstructed}
}

// jobRow maps one run of the job list.
func jobRow(r query.JobRow) *idiosv1.JobRow {
	j := r.Job
	return &idiosv1.JobRow{
		Uid:                   j.UID,
		ClusterId:             j.ClusterID,
		Namespace:             j.Namespace,
		Name:                  j.Name,
		CronjobUid:            j.CronJobUID,
		CronjobName:           j.CronJobName,
		Active:                int32(j.Active),
		Succeeded:             int32(j.Succeeded),
		Failed:                int32(j.Failed),
		BackoffLimit:          int32Ptr(j.BackoffLimit),
		Completions:           int32Ptr(j.Completions),
		Parallelism:           int32Ptr(j.Parallelism),
		RestartPolicy:         j.RestartPolicy,
		ConditionType:         j.ConditionType,
		ConditionReason:       j.ConditionReason,
		ConditionMessage:      j.ConditionMessage,
		CreatedAt:             j.CreatedAt,
		StartedAt:             j.StartedAt,
		FinishedAt:            j.FinishedAt,
		FirstSeenAt:           j.FirstSeenAt,
		LastSeenAt:            j.LastSeenAt,
		DeletedAt:             j.DeletedAt,
		Complete:              r.Complete,
		ActiveDeadlineSeconds: j.ActiveDeadlineSeconds,
	}
}
