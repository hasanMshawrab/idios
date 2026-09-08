package incident

import (
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// ApplyJob opens or attaches a job_failed incident when the Failed condition
// appeared and closes every open incident on the job when it completed.
func ApplyJob(incidents []store.Incident, changes ingest.JobChanges, now time.Time, pol Policy) Ops {
	nowS := clock.Format(now)
	var ops Ops
	job := changes.Job
	if changes.Failed {
		reason := deref(job.ConditionReason)
		if target, reopen, ok := resolveJob(incidents, job.UID); ok {
			ops.Attach = append(ops.Attach, Attach{
				IncidentID: target.ID, Reopen: reopen, LastReason: reason, LastMessage: job.ConditionMessage, LastSeenAt: nowS,
			})
		} else {
			inc := store.Incident{
				ClusterID: job.ClusterID, Namespace: job.Namespace, SubjectKind: store.SubjectJob, JobUID: &job.UID,
				WorkloadKind: "Job", WorkloadName: job.Name, Category: store.CategoryJobFailed,
				FirstReason: reason, LastReason: reason, LastMessage: job.ConditionMessage,
				Occurrences: 1, OpenedAt: firstSet(job.FinishedAt, &nowS), LastSeenAt: nowS,
			}
			if job.CronJobName != nil {
				inc.WorkloadKind, inc.WorkloadName = "CronJob", *job.CronJobName
			}
			// A failure first seen long after it finished is a run that ended
			// before the daemon looked: history, not something to act on.
			if changes.FirstSight && finishedBefore(job.FinishedAt, now.Add(-pol.StabilizationWindow)) {
				reason := store.CloseJobFinished
				inc.ClosedAt, inc.CloseReason = job.FinishedAt, &reason
			}
			ops.Open = append(ops.Open, Open{Incident: inc})
		}
	}
	if changes.Completed {
		for _, inc := range incidents {
			if inc.SubjectKind == store.SubjectJob && deref(inc.JobUID) == job.UID && inc.ClosedAt == nil {
				ops.Close = append(ops.Close, Close{IncidentID: inc.ID, Reason: store.CloseJobFinished, ClosedAt: nowS})
			}
		}
	}
	return ops
}

// resolveJob mirrors resolve for job incidents. job_finished is final
// because the Job either completed or is gone, and a manual close is final
// because a person said they were done with it: a recurrence opens a new
// incident instead of reviving that one.
func resolveJob(incidents []store.Incident, jobUID string) (target store.Incident, reopen, ok bool) {
	var latest *store.Incident
	for i := range incidents {
		inc := &incidents[i]
		if inc.SubjectKind != store.SubjectJob || deref(inc.JobUID) != jobUID || inc.Category != store.CategoryJobFailed {
			continue
		}
		if inc.ClosedAt == nil {
			return *inc, false, true
		}
		if reason := deref(inc.CloseReason); reason == store.CloseJobFinished || reason == store.CloseManual {
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

func finishedBefore(finishedAt *string, cutoff time.Time) bool {
	if finishedAt == nil {
		return false
	}
	t, err := clock.Parse(*finishedAt)
	return err == nil && t.Before(cutoff)
}
