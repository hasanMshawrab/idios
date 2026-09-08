package incident

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	batchv1 "k8s.io/api/batch/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

func loadJob(t *testing.T, path string) *batchv1.Job {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "ingest", "testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.Job
	if err := json.Unmarshal(data, &job); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &job
}

func jobScenario(t *testing.T, files ...string) ingest.JobChanges {
	t.Helper()
	var snap *store.Job
	var ch ingest.JobChanges
	for _, f := range files {
		ch = ingest.DiffJob(snap, loadJob(t, f), 1, testNow)
		row := ch.Job
		snap = &row
	}
	return ch
}

func TestApplyJob(t *testing.T) {
	now := clock.Format(testNow)
	openJobIncident := store.Incident{
		ID: 11, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectJob, JobUID: ptr("job-report-1"),
		WorkloadKind: "CronJob", WorkloadName: "report", Category: store.CategoryJobFailed,
		FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", Occurrences: 1,
		OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: "2026-08-27T11:57:05.000000Z",
	}
	failedOpen := store.Incident{
		ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectJob, JobUID: ptr("job-report-1"),
		WorkloadKind: "CronJob", WorkloadName: "report", Category: store.CategoryJobFailed,
		FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", LastMessage: ptr("Job has reached the specified backoff limit"),
		Occurrences: 1, OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: now,
	}
	// One window past the failed fixture's finish; every row that names it
	// sits outside the window.
	late := testNow.Add(10 * time.Minute)
	lateS := clock.Format(late)
	failedHistory := failedOpen
	failedHistory.LastSeenAt, failedHistory.ClosedAt, failedHistory.CloseReason = lateS, ptr("2026-08-27T11:57:00.000000Z"), ptr(store.CloseJobFinished)
	failedLate := failedOpen
	failedLate.LastSeenAt = lateS
	manualClosed := openJobIncident
	manualClosed.ClosedAt, manualClosed.CloseReason = ptr("2026-08-27T11:58:00.000000Z"), ptr(store.CloseManual)
	cases := []struct {
		name      string
		files     []string
		incidents []store.Incident
		now       time.Time
		want      Ops
	}{
		{"failed at first sight past the window opens closed as history", []string{"job-failed/after.json"}, nil, late,
			Ops{Open: []Open{{Incident: failedHistory}}}},
		{"failed at first sight inside the window opens", []string{"job-failed/after.json"}, nil, testNow,
			Ops{Open: []Open{{Incident: failedOpen}}}},
		{"failed after a sighting opens whatever the age", []string{"job-failed/before.json", "job-failed/after.json"}, nil, late,
			Ops{Open: []Open{{Incident: failedLate}}}},
		{"failed job opens with cronjob as workload", []string{"job-failed/before.json", "job-failed/after.json"}, nil, testNow,
			Ops{Open: []Open{{Incident: failedOpen}}}},
		{"failed job with open incident attaches", []string{"job-failed/before.json", "job-failed/after.json"}, []store.Incident{openJobIncident}, testNow,
			Ops{Attach: []Attach{{IncidentID: 11, LastReason: "BackoffLimitExceeded", LastMessage: ptr("Job has reached the specified backoff limit"), LastSeenAt: now}}}},
		{"failed stays failed is nothing", []string{"job-failed/before.json", "job-failed/after.json", "job-failed/after.json"}, []store.Incident{openJobIncident}, testNow, Ops{}},
		{"manually closed job incident never reopens", []string{"job-failed/before.json", "job-failed/after.json"}, []store.Incident{manualClosed}, testNow,
			Ops{Open: []Open{{Incident: failedOpen}}}},
		{"complete closes open incidents", []string{"job-complete/before.json", "job-complete/after.json"},
			[]store.Incident{func() store.Incident {
				i := openJobIncident
				i.ID, i.JobUID, i.WorkloadKind, i.WorkloadName = 12, ptr("job-report-2"), "Job", "report-28812350"
				return i
			}()}, testNow,
			Ops{Close: []Close{{IncidentID: 12, Reason: store.CloseJobFinished, ClosedAt: now}}}},
		{"complete with nothing open is nothing", []string{"job-complete/before.json", "job-complete/after.json"}, nil, testNow, Ops{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ApplyJob(c.incidents, jobScenario(t, c.files...), c.now, Policy{StabilizationWindow: 10 * time.Minute})
			if d := cmp.Diff(c.want, got, cmpopts.EquateEmpty()); d != "" {
				t.Fatal(d)
			}
		})
	}
}
