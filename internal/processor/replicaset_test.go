package processor

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

func loadReplicaSet(t *testing.T, path string) *appsv1.ReplicaSet {
	t.Helper()
	var rs appsv1.ReplicaSet
	fixture(t, path, &rs)
	return &rs
}

func (h *harness) feedReplicaSets(t *testing.T, files ...string) {
	t.Helper()
	for _, f := range files {
		if err := h.p.ReplicaSet(context.Background(), 1, loadReplicaSet(t, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
}

func (h *harness) rollouts(t *testing.T) []store.RolloutHistory {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT id, cluster_id, namespace, deployment_name, deployment_uid, replicaset_uid, replicaset_name, container_name, image, image_tag,
       revision, first_seen_at, last_seen_at, deleted_at FROM rollout_history ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.RolloutHistory
	for rows.Next() {
		var r store.RolloutHistory
		if err := rows.Scan(&r.ID, &r.ClusterID, &r.Namespace, &r.DeploymentName, &r.DeploymentUID, &r.ReplicaSetUID, &r.ReplicaSetName,
			&r.ContainerName, &r.Image, &r.ImageTag, &r.Revision, &r.FirstSeenAt, &r.LastSeenAt, &r.DeletedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestReplicaSetRowsPerContainerAndDelete(t *testing.T) {
	h := newHarness(t)
	h.feedReplicaSets(t, "replicaset/deploy.json", "replicaset/deploy-rev8.json")
	if err := h.p.ReplicaSetDeleted(context.Background(), "rs-web-1"); err != nil {
		t.Fatal(err)
	}
	now := clock.Format(testNow)
	row := func(id int64, rsUID, rsName, container, image string, tag *string, rev int64, deleted *string) store.RolloutHistory {
		return store.RolloutHistory{ID: id, ClusterID: 1, Namespace: "idios-smoke", DeploymentName: "web", DeploymentUID: "dep-web",
			ReplicaSetUID: rsUID, ReplicaSetName: rsName, ContainerName: container, Image: image, ImageTag: tag, Revision: ptr(rev),
			FirstSeenAt: now, LastSeenAt: now, DeletedAt: deleted}
	}
	want := []store.RolloutHistory{
		row(1, "rs-web-1", "web-7d9f8c6b5", "api", "registry.example.com/web:1.4.2", ptr("1.4.2"), 7, ptr(now)),
		row(2, "rs-web-1", "web-7d9f8c6b5", "worker", "registry.example.com/worker@sha256:7777", nil, 7, ptr(now)),
		row(3, "rs-web-2", "web-8e0a9d7c6", "api", "registry.example.com/web:1.4.3", ptr("1.4.3"), 8, nil),
		row(4, "rs-web-2", "web-8e0a9d7c6", "worker", "registry.example.com/worker@sha256:7777", nil, 8, nil),
	}
	diff(t, want, h.rollouts(t))
}
