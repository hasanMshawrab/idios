package store

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func ptr[T any](v T) *T { return &v }

func TestScanPodRoundTrip(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ts := clock.Format(testEpoch)
	mustExec(t, s, `
INSERT INTO pods (uid, cluster_id, namespace, name, node_name, phase, status_reason, qos_class,
                  controller_kind, controller_name, controller_uid, workload_kind, workload_name,
                  created_at, first_seen_at, last_seen_at, deleted_at, deletion_source, deletion_reason)
VALUES ('p1', ?, 'idios-smoke', 'web-abc-xyz', 'node-a', 'Failed', 'Evicted', 'Burstable',
        'ReplicaSet', 'web-abc', 'rs-1', 'Deployment', 'web', ?, ?, ?, ?, 'watch', 'rollout')`,
		cid, ts, ts, ts, ts)

	got, err := scanPod(s.Reader.DB().QueryRowContext(context.Background(), "SELECT "+podColumns+" FROM pods WHERE uid = 'p1'"))
	if err != nil {
		t.Fatal(err)
	}
	want := Pod{
		UID: "p1", ClusterID: cid, Namespace: "idios-smoke", Name: "web-abc-xyz",
		NodeName: ptr("node-a"), Phase: "Failed", StatusReason: ptr("Evicted"), QOSClass: ptr("Burstable"),
		ControllerKind: "ReplicaSet", ControllerName: "web-abc", ControllerUID: "rs-1",
		WorkloadKind: "Deployment", WorkloadName: "web",
		CreatedAt: ts, FirstSeenAt: ts, LastSeenAt: ts,
		DeletedAt: ptr(ts), DeletionSource: ptr(DeletionSourceWatch), DeletionReason: ptr(DeletionReasonRollout),
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}
