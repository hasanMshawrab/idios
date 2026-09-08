package processor

import (
	"context"
	"database/sql"
	"testing"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

type deletion struct {
	UID    string
	At     *string
	Source *string
}

func (h *harness) podDeletions(t *testing.T) []deletion {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), "SELECT uid, deleted_at, deletion_source FROM pods ORDER BY uid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []deletion
	for rows.Next() {
		var d deletion
		if err := rows.Scan(&d.UID, &d.At, &d.Source); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func (h *harness) jobDeletions(t *testing.T) []deletion {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), "SELECT uid, deleted_at FROM jobs ORDER BY uid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []deletion
	for rows.Next() {
		var d deletion
		if err := rows.Scan(&d.UID, &d.At); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func TestReconcileMarksRowsTheStoresNoLongerHold(t *testing.T) {
	h := newHarness(t)
	now := clock.Format(testNow)
	earlier := "2026-08-27T11:00:00.000000Z"
	h.exec(t, `INSERT INTO clusters (id, identity, name, context_name, api_server_url, first_seen_at) VALUES (2, 'c2', 'c2', 'other', 'https://10.0.0.2', ?)`, now)
	insertPod := func(uid string, cluster int64, ns string, deletedAt, source *string) {
		h.exec(t, `INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at, deleted_at, deletion_source)
VALUES (?, ?, ?, ?, 'Running', ?, ?, ?, ?, ?)`, uid, cluster, ns, "pod-"+uid, earlier, earlier, earlier, deletedAt, source)
	}
	insertPod("p-done", 1, "idios-smoke", ptr(earlier), ptr(store.DeletionSourceWatch))
	insertPod("p-gone", 1, "idios-smoke", nil, nil)
	insertPod("p-live", 1, "idios-smoke", nil, nil)
	insertPod("p-other", 2, "idios-smoke", nil, nil)
	insertPod("p-unwatched", 1, "legacy", nil, nil)
	for _, uid := range []string{"j-gone", "j-live"} {
		h.exec(t, `INSERT INTO jobs (uid, cluster_id, namespace, name, restart_policy, created_at, first_seen_at, last_seen_at) VALUES (?, 1, 'idios-smoke', ?, 'Never', ?, ?, ?)`,
			uid, "job-"+uid, earlier, earlier, earlier)
	}
	podInc := h.seedIncident(t, "p-gone", "api", store.CategoryCrash, nil)
	var jobInc int64
	h.tx(t, func(tx *sql.Tx) (err error) {
		jobInc, err = store.OpenIncident(context.Background(), tx, store.Incident{
			ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectJob, JobUID: ptr("j-gone"), WorkloadKind: "Job", WorkloadName: "job-j-gone",
			Category: store.CategoryJobFailed, FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", Occurrences: 1, OpenedAt: earlier, LastSeenAt: earlier,
		})
		return err
	})

	live := map[string]bool{"p-live": true, "j-live": true}
	if err := h.p.Reconcile(context.Background(), 1, []string{"idios-smoke"}, func(_, uid string) bool { return live[uid] }); err != nil {
		t.Fatal(err)
	}

	diff(t, []deletion{
		{"p-done", ptr(earlier), ptr(store.DeletionSourceWatch)},
		{"p-gone", ptr(now), ptr(store.DeletionSourceReconcile)},
		{"p-live", nil, nil},
		{"p-other", nil, nil},
		{"p-unwatched", ptr(now), ptr(store.DeletionSourceUnwatched)},
	}, h.podDeletions(t))
	diff(t, []deletion{{"j-gone", ptr(now), nil}, {"j-live", nil, nil}}, h.jobDeletions(t))
	closes := map[int64][2]*string{}
	for _, uid := range []string{"p-gone", "j-gone"} {
		for _, inc := range h.incidents(t, uid) {
			closes[inc.ID] = [2]*string{inc.ClosedAt, inc.CloseReason}
		}
	}
	diff(t, map[int64][2]*string{
		podInc: {ptr(now), ptr(store.ClosePodDeleted)},
		jobInc: {ptr(now), ptr(store.CloseJobFinished)},
	}, closes)
	if len(h.sink.reqs) != 0 {
		t.Errorf("reconcile enqueued %d capture requests, want none: %+v", len(h.sink.reqs), h.sink.reqs)
	}
}
