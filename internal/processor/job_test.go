package processor

import (
	"context"
	"database/sql"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

func loadJob(t *testing.T, path string) *batchv1.Job {
	t.Helper()
	var job batchv1.Job
	fixture(t, path, &job)
	return &job
}

func (h *harness) feedJobs(t *testing.T, files ...string) {
	t.Helper()
	for _, f := range files {
		if err := h.p.Job(context.Background(), 1, loadJob(t, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
}

func (h *harness) job(t *testing.T, uid string) *store.Job {
	t.Helper()
	var out *store.Job
	h.tx(t, func(tx *sql.Tx) (err error) { out, err = store.LoadJob(context.Background(), tx, uid); return err })
	return out
}

func TestJobFailedOpensAndDeleteCloses(t *testing.T) {
	now := clock.Format(testNow)
	failed := store.Incident{
		ID: 1, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectJob, JobUID: ptr("job-report-1"),
		WorkloadKind: "CronJob", WorkloadName: "report", Category: store.CategoryJobFailed,
		FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", LastMessage: ptr("Job has reached the specified backoff limit"),
		Occurrences: 1, OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: now,
	}
	closed := failed
	closed.ClosedAt, closed.CloseReason = ptr(now), ptr(store.CloseJobFinished)
	cases := []struct {
		name          string
		files         []string
		uid           string
		wantCondition *string
		wantIncidents []store.Incident
		wantAfterDel  []store.Incident
	}{
		{"failed job", []string{"job-failed/before.json", "job-failed/after.json"}, "job-report-1", ptr("Failed"), []store.Incident{failed}, []store.Incident{closed}},
		{"completed job", []string{"job-complete/before.json", "job-complete/after.json"}, "job-report-2", ptr("Complete"), nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.feedJobs(t, c.files...)
			job := h.job(t, c.uid)
			if job == nil {
				t.Fatal("job row missing")
			}
			diff(t, c.wantCondition, job.ConditionType)
			diff(t, c.wantIncidents, h.incidents(t, c.uid))
			if err := h.p.JobDeleted(context.Background(), c.uid); err != nil {
				t.Fatal(err)
			}
			if err := h.p.JobDeleted(context.Background(), c.uid); err != nil {
				t.Fatal(err)
			}
			diff(t, ptr(now), h.job(t, c.uid).DeletedAt)
			diff(t, c.wantAfterDel, h.incidents(t, c.uid))
			if len(h.sink.reqs) != 0 {
				t.Errorf("job path enqueued %d requests", len(h.sink.reqs))
			}
		})
	}
}

// ownedBy makes a fixture Job a run of the named CronJob, so one fixture pair
// can stand for several runs.
func ownedBy(job *batchv1.Job, cronJobName, cronJobUID string) *batchv1.Job {
	job.OwnerReferences = []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: cronJobName, UID: k8stypes.UID(cronJobUID), Controller: ptr(true)}}
	return job
}

func (h *harness) feedJob(t *testing.T, job *batchv1.Job) {
	t.Helper()
	if err := h.p.Job(context.Background(), 1, job); err != nil {
		t.Fatalf("%s: %v", job.Name, err)
	}
}

func TestALaterRunClosesAnEarlierRunsIncident(t *testing.T) {
	h := newHarness(t)
	h.feedJobs(t, "job-failed/before.json", "job-failed/after.json")
	other := loadJob(t, "job-failed/after.json")
	other.UID, other.Name = "job-other-1", "cleanup-28812345"
	h.feedJob(t, ownedBy(other, "cleanup", "cj-cleanup"))
	later := testNow.Add(time.Minute)
	h.clk.Set(later)
	h.feedJob(t, ownedBy(loadJob(t, "job-complete/before.json"), "report", "cj-report"))
	h.feedJob(t, ownedBy(loadJob(t, "job-complete/after.json"), "report", "cj-report"))

	failed := func(id int64, uid, name string) store.Incident {
		return store.Incident{
			ID: id, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectJob, JobUID: ptr(uid),
			WorkloadKind: "CronJob", WorkloadName: name, Category: store.CategoryJobFailed,
			FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", LastMessage: ptr("Job has reached the specified backoff limit"),
			Occurrences: 1, OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: clock.Format(testNow),
		}
	}
	earlier := failed(1, "job-report-1", "report")
	earlier.ClosedAt, earlier.CloseReason = ptr(clock.Format(later)), ptr(store.CloseJobFinished)
	want := []store.Incident{earlier, failed(2, "job-other-1", "cleanup")}
	var got []store.Incident
	for _, uid := range []string{"job-report-1", "job-other-1", "job-report-2"} {
		got = append(got, h.incidents(t, uid)...)
	}
	diff(t, want, got)
}
