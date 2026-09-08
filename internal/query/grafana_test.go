package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/grafana"
	"github.com/hasanMshawrab/idios/internal/store"
)

// grafanaClusterID is the one cluster this file configures for Grafana;
// grafanaPlainClusterID stays unconfigured, all three columns empty.
const (
	grafanaClusterID      int64 = 1
	grafanaPlainClusterID int64 = 2
)

var (
	grafanaCfg = grafana.Config{BaseURL: "https://logs.example.grafana.net", DatasourceUID: "grafanacloud-logs", Selector: grafana.DefaultSelector}
	grafanaAt  = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
)

// grafanaStore builds a store with one Grafana-configured cluster and one
// unconfigured cluster, and no other rows: this feature's window rules need
// scenarios querytest.Seed does not have (a deleted pod, a container that
// never ran), so each test inserts exactly the rows its scenario needs.
func grafanaStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background(), clock.NewFake(grafanaAt)); err != nil {
		t.Fatal(err)
	}
	grafanaTx(t, st, func(ctx context.Context, tx *sql.Tx) error {
		at := clock.Format(grafanaAt)
		id, err := store.InsertCluster(ctx, tx, "grafana-cluster", "grafana-cluster", at)
		if err != nil || id != grafanaClusterID {
			return err
		}
		if _, err := store.SetClusterGrafana(ctx, tx, id, grafanaCfg.BaseURL, grafanaCfg.DatasourceUID, grafanaCfg.Selector); err != nil {
			return err
		}
		id2, err := store.InsertCluster(ctx, tx, "plain-cluster", "plain-cluster", at)
		if err != nil || id2 != grafanaPlainClusterID {
			return err
		}
		return nil
	})
	return st
}

func grafanaTx(t *testing.T, st *store.Store, fn func(ctx context.Context, tx *sql.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func grafanaPod(clusterID int64, uid, namespace, name, node string, firstSeenAt string) store.Pod {
	return store.Pod{
		UID: uid, ClusterID: clusterID, Namespace: namespace, Name: name, NodeName: sp(node),
		Phase: "Running", ControllerKind: "ReplicaSet", ControllerName: name, ControllerUID: "rs-" + uid,
		WorkloadKind: "Deployment", WorkloadName: "web",
		CreatedAt: firstSeenAt, FirstSeenAt: firstSeenAt, LastSeenAt: firstSeenAt,
	}
}

// TestGetIncidentGrafanaURL covers presentation.md's "Grafana links" window
// rules and field placement for IncidentDetail.GrafanaURL: an open incident
// ends in the literal now, a closed one is padded epoch millis of its own
// timestamps, a job incident with no pod carries no link, and a pod-level
// incident (no container_name) drops the container matcher.
func TestGetIncidentGrafanaURL(t *testing.T) {
	st := grafanaStore(t)
	ctx := context.Background()

	openedAt := "2026-08-27T12:10:00.000000Z"
	closedAt := "2026-08-27T12:40:00.000000Z"
	opened, err := clock.Parse(openedAt)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := clock.Parse(closedAt)
	if err != nil {
		t.Fatal(err)
	}

	var openID, closedID, jobID, noContainerID, unconfiguredID int64
	grafanaTx(t, st, func(ctx context.Context, tx *sql.Tx) error {
		for _, p := range []store.Pod{
			grafanaPod(grafanaClusterID, "pod-open", "shop", "web-open", "node-a", openedAt),
			grafanaPod(grafanaClusterID, "pod-closed", "shop", "web-closed", "node-a", openedAt),
			grafanaPod(grafanaClusterID, "pod-nocontainer", "shop", "web-nc", "node-a", openedAt),
			grafanaPod(grafanaPlainClusterID, "pod-plain", "shop", "web-plain", "node-a", openedAt),
		} {
			if err := store.UpsertPod(ctx, tx, p); err != nil {
				return err
			}
		}
		var err error
		if openID, err = store.OpenIncident(ctx, tx, store.Incident{
			ClusterID: grafanaClusterID, Namespace: "shop", SubjectKind: store.SubjectPod, PodUID: sp("pod-open"),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash,
			FirstReason: "Error", LastReason: "Error", OpenedAt: openedAt, LastSeenAt: openedAt,
		}); err != nil {
			return err
		}
		if closedID, err = store.OpenIncident(ctx, tx, store.Incident{
			ClusterID: grafanaClusterID, Namespace: "shop", SubjectKind: store.SubjectPod, PodUID: sp("pod-closed"),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash,
			FirstReason: "Error", LastReason: "Error", OpenedAt: openedAt, LastSeenAt: openedAt,
			ClosedAt: sp(closedAt), CloseReason: sp(store.CloseRecovered),
		}); err != nil {
			return err
		}
		if jobID, err = store.OpenIncident(ctx, tx, store.Incident{
			ClusterID: grafanaClusterID, Namespace: "shop", SubjectKind: store.SubjectJob, JobUID: sp("job-g-1"),
			WorkloadKind: "CronJob", WorkloadName: "report", Category: store.CategoryJobFailed,
			FirstReason: "Failed", LastReason: "Failed", OpenedAt: openedAt, LastSeenAt: openedAt,
		}); err != nil {
			return err
		}
		if noContainerID, err = store.OpenIncident(ctx, tx, store.Incident{
			ClusterID: grafanaClusterID, Namespace: "shop", SubjectKind: store.SubjectPod, PodUID: sp("pod-nocontainer"),
			WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryScheduling,
			FirstReason: "Unschedulable", LastReason: "Unschedulable", OpenedAt: openedAt, LastSeenAt: openedAt,
		}); err != nil {
			return err
		}
		unconfiguredID, err = store.OpenIncident(ctx, tx, store.Incident{
			ClusterID: grafanaPlainClusterID, Namespace: "shop", SubjectKind: store.SubjectPod, PodUID: sp("pod-plain"),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash,
			FirstReason: "Error", LastReason: "Error", OpenedAt: openedAt, LastSeenAt: openedAt,
		})
		return err
	})

	openWant := grafana.ExploreURL(grafanaCfg,
		grafana.Values{Namespace: "shop", Pod: "web-open", Container: "api", Workload: "web", Node: "node-a", Cluster: "grafana-cluster"},
		grafana.Window{From: opened.Add(-grafana.Pad)})
	closedWant := grafana.ExploreURL(grafanaCfg,
		grafana.Values{Namespace: "shop", Pod: "web-closed", Container: "api", Workload: "web", Node: "node-a", Cluster: "grafana-cluster"},
		grafana.Window{From: opened.Add(-grafana.Pad), To: closed.Add(grafana.Pad)})
	noContainerWant := grafana.ExploreURL(grafanaCfg,
		grafana.Values{Namespace: "shop", Pod: "web-nc", Container: "", Workload: "web", Node: "node-a", Cluster: "grafana-cluster"},
		grafana.Window{From: opened.Add(-grafana.Pad)})

	cases := []struct {
		name string
		id   int64
		want *string
	}{
		{"open incident ends in now", openID, &openWant},
		{"closed incident is padded epoch millis", closedID, &closedWant},
		{"job incident with no pod carries no link", jobID, nil},
		{"pod-level incident drops the container matcher", noContainerID, &noContainerWant},
		{"unconfigured cluster produces no link", unconfiguredID, nil},
	}
	for _, c := range cases {
		d, err := GetIncident(ctx, st.Reader.DB(), c.id, Page{})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if (d.GrafanaURL == nil) != (c.want == nil) || (d.GrafanaURL != nil && *d.GrafanaURL != *c.want) {
			t.Errorf("%s: GrafanaURL = %v, want %v", c.name, d.GrafanaURL, c.want)
		}
	}
}

// TestGetPodGrafanaContainerURLs covers presentation.md's per-container
// window rules for PodDetail.ContainerGrafanaURLs: every bound prefers the
// kubelet's clock - running from running_since open-ended, terminated
// bracketed by the state history's k8s times with running_since and
// updated_at standing in, waiting and a missing start from the pod's
// created_at - plus a deleted pod capping an open end and an unconfigured
// cluster carrying no links at all. The kubelet times sit hours before the
// observation times so a bound built from the wrong clock cannot pass.
func TestGetPodGrafanaContainerURLs(t *testing.T) {
	st := grafanaStore(t)
	ctx := context.Background()

	podCreatedAt := "2026-08-27T02:00:00.000000Z"
	k8sStartedAt := "2026-08-27T02:00:33.000000Z"
	k8sFinishedAt := "2026-08-27T02:01:20.000000Z"
	firstSeenAt := "2026-08-27T12:00:00.000000Z"
	runningSince := "2026-08-27T12:05:00.000000Z"
	updatedAt := "2026-08-27T12:20:00.000000Z"
	deletedAt := "2026-08-27T12:25:00.000000Z"

	created, err := clock.Parse(podCreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	k8sStart, err := clock.Parse(k8sStartedAt)
	if err != nil {
		t.Fatal(err)
	}
	k8sFinish, err := clock.Parse(k8sFinishedAt)
	if err != nil {
		t.Fatal(err)
	}
	running, err := clock.Parse(runningSince)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := clock.Parse(updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := clock.Parse(deletedAt)
	if err != nil {
		t.Fatal(err)
	}

	runningPod := grafanaPod(grafanaClusterID, "pod-running", "shop", "web-running", "node-a", firstSeenAt)
	termPod := grafanaPod(grafanaClusterID, "pod-term", "shop", "web-term", "node-a", firstSeenAt)
	waitPod := grafanaPod(grafanaClusterID, "pod-wait", "shop", "web-wait", "node-a", firstSeenAt)
	waitPod.CreatedAt = podCreatedAt
	noStartPod := grafanaPod(grafanaClusterID, "pod-nostart", "shop", "web-nostart", "node-a", firstSeenAt)
	noStartPod.CreatedAt = podCreatedAt
	postPod := grafanaPod(grafanaClusterID, "pod-postmortem", "shop", "web-postmortem", "node-a", firstSeenAt)
	postPod.CreatedAt = podCreatedAt
	deletedPod := grafanaPod(grafanaClusterID, "pod-deleted", "shop", "web-deleted", "node-a", firstSeenAt)
	deletedPod.DeletedAt = sp(deletedAt)
	plainPod := grafanaPod(grafanaPlainClusterID, "pod-unconf", "shop", "web-unconf", "node-a", firstSeenAt)

	grafanaTx(t, st, func(ctx context.Context, tx *sql.Tx) error {
		for _, p := range []store.Pod{runningPod, termPod, waitPod, noStartPod, postPod, deletedPod, plainPod} {
			if err := store.UpsertPod(ctx, tx, p); err != nil {
				return err
			}
		}
		// The post-mortem pod was ingested hours after its one run; only
		// the state history knows when the kubelet actually ran it.
		if err := store.InsertHistory(ctx, tx, store.ContainerStateHistory{
			PodUID: "pod-postmortem", ContainerName: "api", Image: "web:1",
			State: store.StateTerminated, K8sStartedAt: sp(k8sStartedAt),
			K8sFinishedAt: sp(k8sFinishedAt), ObservedAt: firstSeenAt,
		}); err != nil {
			return err
		}
		return store.UpsertContainers(ctx, tx, []store.Container{
			{PodUID: "pod-running", Name: "api", Kind: store.ContainerKindApp, Image: "web:1", State: store.StateRunning,
				RunningSince: sp(runningSince), UpdatedAt: runningSince},
			{PodUID: "pod-term", Name: "api", Kind: store.ContainerKindApp, Image: "web:1", State: store.StateTerminated,
				RunningSince: sp(runningSince), UpdatedAt: updatedAt},
			{PodUID: "pod-wait", Name: "api", Kind: store.ContainerKindApp, Image: "web:1", State: store.StateWaiting,
				UpdatedAt: firstSeenAt},
			{PodUID: "pod-nostart", Name: "api", Kind: store.ContainerKindApp, Image: "web:1", State: store.StateRunning,
				UpdatedAt: firstSeenAt},
			{PodUID: "pod-postmortem", Name: "api", Kind: store.ContainerKindApp, Image: "web:1", State: store.StateTerminated,
				UpdatedAt: firstSeenAt},
			{PodUID: "pod-deleted", Name: "api", Kind: store.ContainerKindApp, Image: "web:1", State: store.StateRunning,
				RunningSince: sp(runningSince), UpdatedAt: runningSince},
			{PodUID: "pod-unconf", Name: "api", Kind: store.ContainerKindApp, Image: "web:1", State: store.StateRunning,
				RunningSince: sp(runningSince), UpdatedAt: runningSince},
		})
	})

	values := func(podName string) grafana.Values {
		return grafana.Values{Namespace: "shop", Pod: podName, Container: "api", Workload: "web", Node: "node-a", Cluster: "grafana-cluster"}
	}
	runningWant := grafana.ExploreURL(grafanaCfg, values("web-running"), grafana.Window{From: running.Add(-grafana.Pad)})
	termWant := grafana.ExploreURL(grafanaCfg, values("web-term"), grafana.Window{From: running.Add(-grafana.Pad), To: updated.Add(grafana.Pad)})
	waitWant := grafana.ExploreURL(grafanaCfg, values("web-wait"), grafana.Window{From: created.Add(-grafana.Pad)})
	noStartWant := grafana.ExploreURL(grafanaCfg, values("web-nostart"), grafana.Window{From: created.Add(-grafana.Pad)})
	postWant := grafana.ExploreURL(grafanaCfg, values("web-postmortem"), grafana.Window{From: k8sStart.Add(-grafana.Pad), To: k8sFinish.Add(grafana.Pad)})
	deletedWant := grafana.ExploreURL(grafanaCfg, values("web-deleted"), grafana.Window{From: running.Add(-grafana.Pad), To: deleted.Add(grafana.Pad)})

	cases := []struct {
		name string
		uid  string
		want map[string]string
	}{
		{"running container from running_since", "pod-running", map[string]string{"api": runningWant}},
		{"terminated container ends at updated_at", "pod-term", map[string]string{"api": termWant}},
		{"waiting container starts at pod created_at", "pod-wait", map[string]string{"api": waitWant}},
		{"running container without running_since falls back to created_at", "pod-nostart", map[string]string{"api": noStartWant}},
		{"post-mortem terminated container uses the kubelet's own run times", "pod-postmortem", map[string]string{"api": postWant}},
		{"deleted pod caps the open end at deleted_at", "pod-deleted", map[string]string{"api": deletedWant}},
		{"unconfigured cluster produces no links", "pod-unconf", nil},
	}
	for _, c := range cases {
		d, err := GetPod(ctx, st.Reader.DB(), c.uid)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(d.ContainerGrafanaURLs) != len(c.want) {
			t.Fatalf("%s: ContainerGrafanaURLs = %v, want %v", c.name, d.ContainerGrafanaURLs, c.want)
		}
		for k, want := range c.want {
			if got := d.ContainerGrafanaURLs[k]; got != want {
				t.Errorf("%s: [%s] = %s, want %s", c.name, k, got, want)
			}
		}
	}
}
