package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

func loadReplicaSet(t *testing.T, path string) *appsv1.ReplicaSet {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var rs appsv1.ReplicaSet
	if err := json.Unmarshal(data, &rs); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &rs
}

func TestMapReplicaSet(t *testing.T) {
	now := clock.Format(testNow)
	cases := []struct {
		name string
		file string
		want []store.RolloutHistory
	}{
		{"bare replicaset never observed by its controller", "replicaset/bare.json", []store.RolloutHistory{
			{ClusterID: 1, Namespace: "idios-smoke", ReplicaSetUID: "rs-bare", ReplicaSetName: "standalone",
				ContainerName: "app", Image: "busybox:1.36", ImageTag: ptr("1.36"),
				CreatedAt: "2026-08-27T11:44:00.000000Z", Replicas: ptr[int64](1), FirstSeenAt: now, LastSeenAt: now},
		}},
		{"deployment-owned, one row per container", "replicaset/deploy-rev8.json", []store.RolloutHistory{
			{ClusterID: 1, Namespace: "idios-smoke", DeploymentName: "web", DeploymentUID: "dep-web", ReplicaSetUID: "rs-web-2", ReplicaSetName: "web-8e0a9d7c6",
				ContainerName: "api", Image: "registry.example.com/web:1.4.3", ImageTag: ptr("1.4.3"), Revision: ptr[int64](8),
				CreatedAt: "2026-08-27T11:58:00.000000Z", Replicas: ptr[int64](3), ReadyReplicas: ptr[int64](2), AvailableReplicas: ptr[int64](2),
				FirstSeenAt: now, LastSeenAt: now},
			{ClusterID: 1, Namespace: "idios-smoke", DeploymentName: "web", DeploymentUID: "dep-web", ReplicaSetUID: "rs-web-2", ReplicaSetName: "web-8e0a9d7c6",
				ContainerName: "worker", Image: "registry.example.com/worker@sha256:7777", Revision: ptr[int64](8),
				CreatedAt: "2026-08-27T11:58:00.000000Z", Replicas: ptr[int64](3), ReadyReplicas: ptr[int64](2), AvailableReplicas: ptr[int64](2),
				FirstSeenAt: now, LastSeenAt: now},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MapReplicaSet(loadReplicaSet(t, c.file), 1, testNow)
			if d := cmp.Diff(c.want, got); d != "" {
				t.Fatal(d)
			}
		})
	}
}
