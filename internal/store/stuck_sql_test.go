package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// A pod waiting on a missing volume or a mistyped ConfigMap reports no
// reason any classifier reads, so elapsed time is the only signal; the pods
// that are merely starting, already spoken for, or gone must not be swept up
// with it.
func TestStuckCandidatesAreLiveWaitingPodsWithNoIncident(t *testing.T) {
	const (
		old    = "2026-08-27T11:40:00.000000Z"
		edge   = "2026-08-27T11:50:00.000000Z"
		recent = "2026-08-27T11:55:00.000000Z"
	)
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")

	pod := func(uid, createdAt string, deleted bool) {
		t.Helper()
		var deletedAt *string
		if deleted {
			d := recent
			deletedAt = &d
		}
		mustExec(t, s, `
INSERT INTO pods (uid, cluster_id, namespace, name, phase, workload_kind, workload_name,
                  created_at, first_seen_at, last_seen_at, deleted_at)
VALUES (?, ?, 'idios-smoke', ?, 'Pending', 'Deployment', 'web', ?, ?, ?, ?)`,
			uid, cid, "pod-"+uid, createdAt, createdAt, recent, deletedAt)
	}
	container := func(podUID, name, kind, state string, reason *string, updatedAt string) {
		t.Helper()
		mustExec(t, s, `
INSERT INTO containers (pod_uid, name, kind, image, state, reason, ready, updated_at)
VALUES (?, ?, ?, 'img', ?, ?, 0, ?)`, podUID, name, kind, state, reason, updatedAt)
	}
	waiting := ptr("ContainerCreating")

	pod("p-closed", old, false)
	container("p-closed", "api", ContainerKindApp, StateWaiting, waiting, old)
	id, err := insertPodIncident(t, s, cid, "p-closed", "api", CategoryCrash)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, s, "UPDATE incidents SET closed_at = ?, close_reason = ? WHERE id = ?", recent, CloseRecovered, id)

	pod("p-deleted", old, true)
	container("p-deleted", "api", ContainerKindApp, StateWaiting, waiting, old)

	pod("p-empty", old, false)

	pod("p-fresh-container", old, false)
	container("p-fresh-container", "api", ContainerKindApp, StateWaiting, waiting, recent)

	pod("p-init", old, false)
	container("p-init", "api", ContainerKindApp, StateWaiting, ptr("PodInitializing"), old)
	container("p-init", "wait-for-db", ContainerKindInit, StateWaiting, ptr("CreateContainerConfigError"), old)

	pod("p-mixed", old, false)
	container("p-mixed", "api", ContainerKindApp, StateRunning, nil, old)
	container("p-mixed", "web", ContainerKindApp, StateWaiting, waiting, old)

	pod("p-new", recent, false)
	container("p-new", "api", ContainerKindApp, StateWaiting, waiting, old)

	pod("p-noreason", old, false)
	container("p-noreason", "api", ContainerKindApp, StateWaiting, nil, old)

	pod("p-open", old, false)
	container("p-open", "api", ContainerKindApp, StateWaiting, waiting, old)
	if _, err := insertPodIncident(t, s, cid, "p-open", "api", CategoryImagePull); err != nil {
		t.Fatal(err)
	}

	pod("p-terminating", old, false)
	mustExec(t, s, "UPDATE pods SET deletion_requested_at = ? WHERE uid = ?", old, "p-terminating")
	container("p-terminating", "api", ContainerKindApp, StateWaiting, waiting, old)

	pod("p-running", old, false)
	container("p-running", "api", ContainerKindApp, StateRunning, nil, old)

	ctx := context.Background()
	var got []StuckCandidate
	if err := s.Writer.Tx(ctx, func(tx *sql.Tx) (err error) {
		got, err = ListStuckCandidates(ctx, tx, edge)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := []StuckCandidate{
		{PodUID: "p-closed", Namespace: "idios-smoke", ContainerName: "api", Reason: "ContainerCreating",
			ClusterID: cid, WorkloadKind: "Deployment", WorkloadName: "web", Since: old},
		{PodUID: "p-init", Namespace: "idios-smoke", ContainerName: "wait-for-db", Reason: "CreateContainerConfigError",
			ClusterID: cid, WorkloadKind: "Deployment", WorkloadName: "web", Since: old},
		{PodUID: "p-noreason", Namespace: "idios-smoke", ContainerName: "api", Reason: "",
			ClusterID: cid, WorkloadKind: "Deployment", WorkloadName: "web", Since: old},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}
