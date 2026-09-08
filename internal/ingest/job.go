package ingest

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// JobChanges is what DiffJob decided for one Job event. Failed and Completed
// are true only on the event where the condition appeared.
type JobChanges struct {
	FirstSight bool
	Job        store.Job
	Failed     bool
	Completed  bool
}

// DiffJob maps job to its row and reports terminal condition transitions.
// Success and failure come from conditions, never from the counters: failed
// can exceed backoffLimit and says nothing for OnFailure jobs.
func DiffJob(snap *store.Job, job *batchv1.Job, clusterID int64, now time.Time) JobChanges {
	nowS := clock.Format(now)
	row := store.Job{
		UID: string(job.UID), ClusterID: clusterID, Namespace: job.Namespace, Name: job.Name,
		Active: int64(job.Status.Active), Succeeded: int64(job.Status.Succeeded), Failed: int64(job.Status.Failed),
		BackoffLimit: ptrInt32(job.Spec.BackoffLimit), Completions: ptrInt32(job.Spec.Completions), Parallelism: ptrInt32(job.Spec.Parallelism),
		ActiveDeadlineSeconds: copyInt64(job.Spec.ActiveDeadlineSeconds),
		RestartPolicy:         string(job.Spec.Template.Spec.RestartPolicy),
		CreatedAt:             k8sTime(job.CreationTimestamp), StartedAt: k8sTimePtr(job.Status.StartTime), FinishedAt: k8sTimePtr(job.Status.CompletionTime),
		FirstSeenAt: nowS, LastSeenAt: nowS,
	}
	if ctrl := metav1.GetControllerOf(job); ctrl != nil && ctrl.Kind == "CronJob" {
		row.CronJobUID, row.CronJobName = ptrString(string(ctrl.UID)), ptrString(ctrl.Name)
	}
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete, batchv1.JobFailed, batchv1.JobSuspended:
		default:
			continue
		}
		row.ConditionType, row.ConditionReason, row.ConditionMessage = ptrString(string(c.Type)), nonEmpty(c.Reason), nonEmpty(c.Message)
		// completionTime is unset on a failed job, so the Failed condition's
		// transition time is the only end time there is.
		if c.Type == batchv1.JobFailed && row.FinishedAt == nil {
			row.FinishedAt = k8sTimePtr(&c.LastTransitionTime)
		}
		break
	}
	ch := JobChanges{FirstSight: snap == nil, Job: row}
	var prevType *string
	if snap != nil {
		ch.Job.FirstSeenAt, ch.Job.DeletedAt = snap.FirstSeenAt, snap.DeletedAt
		prevType = snap.ConditionType
	}
	ch.Failed = became(row.ConditionType, prevType, string(batchv1.JobFailed))
	ch.Completed = became(row.ConditionType, prevType, string(batchv1.JobComplete))
	return ch
}

func became(cur, prev *string, want string) bool {
	return cur != nil && *cur == want && (prev == nil || *prev != want)
}

func ptrInt32(v *int32) *int64 {
	if v == nil {
		return nil
	}
	return ptrInt64(int64(*v))
}

// copyInt64 detaches the value from the API object it was read off.
func copyInt64(v *int64) *int64 {
	if v == nil {
		return nil
	}
	return ptrInt64(*v)
}
