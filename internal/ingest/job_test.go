package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

func loadJob(t *testing.T, path string) *batchv1.Job {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.Job
	if err := json.Unmarshal(data, &job); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &job
}

func TestDiffJobScenarios(t *testing.T) {
	now := clock.Format(testNow)
	failedRow := store.Job{
		UID: "job-report-1", ClusterID: 1, Namespace: "idios-smoke", Name: "report-28812345",
		CronJobUID: ptr("cj-report"), CronJobName: ptr("report"), Active: 0, Succeeded: 0, Failed: 3,
		BackoffLimit: ptr[int64](2), Completions: ptr[int64](1), Parallelism: ptr[int64](1),
		ActiveDeadlineSeconds: ptr[int64](900), RestartPolicy: "Never",
		ConditionType: ptr("Failed"), ConditionReason: ptr("BackoffLimitExceeded"), ConditionMessage: ptr("Job has reached the specified backoff limit"),
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: ptr("2026-08-27T11:45:02.000000Z"), FinishedAt: ptr("2026-08-27T11:57:00.000000Z"),
		FirstSeenAt: now, LastSeenAt: now,
	}
	completeRow := store.Job{
		UID: "job-report-2", ClusterID: 1, Namespace: "idios-smoke", Name: "report-28812350",
		Succeeded: 1, BackoffLimit: ptr[int64](2), Completions: ptr[int64](1), Parallelism: ptr[int64](1), RestartPolicy: "Never",
		ConditionType: ptr("Complete"),
		CreatedAt:     "2026-08-27T11:45:00.000000Z", StartedAt: ptr("2026-08-27T11:50:02.000000Z"), FinishedAt: ptr("2026-08-27T11:52:00.000000Z"),
		FirstSeenAt: now, LastSeenAt: now,
	}
	cases := []struct {
		name  string
		files []string
		want  JobChanges
	}{
		{"failed at first sight", []string{"job-failed/after.json"}, JobChanges{FirstSight: true, Job: failedRow, Failed: true}},
		{"running then failed", []string{"job-failed/before.json", "job-failed/after.json"}, JobChanges{Job: failedRow, Failed: true}},
		{"failed stays failed", []string{"job-failed/before.json", "job-failed/after.json", "job-failed/after.json"}, JobChanges{Job: failedRow}},
		{"running then complete, no cronjob", []string{"job-complete/before.json", "job-complete/after.json"}, JobChanges{Job: completeRow, Completed: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var snap *store.Job
			var got JobChanges
			for _, f := range c.files {
				got = DiffJob(snap, loadJob(t, f), 1, testNow)
				row := got.Job
				snap = &row
			}
			if d := cmp.Diff(c.want, got); d != "" {
				t.Fatal(d)
			}
		})
	}
}
