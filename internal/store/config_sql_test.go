package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func TestInsertClusterStartsUnconnectedAndIsFoundByName(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	now := clock.Format(clk.Now())
	var id int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		id, err = InsertCluster(ctx, tx, "orbstack", "orbstack-admin", now)
		return err
	})
	cases := []struct {
		name string
		want *Cluster
	}{
		{"orbstack", &Cluster{ID: id, Name: "orbstack", ContextName: "orbstack-admin", FirstSeenAt: now}},
		{"nowhere", nil},
	}
	for _, c := range cases {
		var got *Cluster
		inTx(t, s, func(tx *sql.Tx) (err error) {
			got, err = FindClusterByName(ctx, tx, c.name)
			return err
		})
		if d := cmp.Diff(c.want, got); d != "" {
			t.Errorf("FindClusterByName(%q): %s", c.name, d)
		}
	}
}

func TestRenameClusterChangesNameOnly(t *testing.T) {
	s, _ := openMigratedStore(t)
	ctx := context.Background()
	cid := insertCluster(t, s, "ks-uid")
	inTx(t, s, func(tx *sql.Tx) error {
		return MarkClusterConnected(ctx, tx, cid, nil, "https://127.0.0.1:26443", "2026-08-27T12:00:00.000000Z")
	})
	var ok bool
	inTx(t, s, func(tx *sql.Tx) (err error) {
		ok, err = RenameCluster(ctx, tx, 999, "x")
		return err
	})
	if ok {
		t.Fatal("RenameCluster(999) = true, want false")
	}
	want := &Cluster{ID: cid, Identity: ptr("ks-uid"), Name: "production", ContextName: "ctx", APIServerURL: "https://127.0.0.1:26443",
		FirstSeenAt: clock.Format(testEpoch), LastConnectedAt: ptr("2026-08-27T12:00:00.000000Z")}
	for i := range 2 {
		inTx(t, s, func(tx *sql.Tx) (err error) {
			ok, err = RenameCluster(ctx, tx, cid, "production")
			return err
		})
		if !ok {
			t.Fatalf("call %d: RenameCluster = false, want true", i)
		}
		var got *Cluster
		inTx(t, s, func(tx *sql.Tx) (err error) {
			got, err = GetCluster(ctx, tx, cid)
			return err
		})
		if d := cmp.Diff(want, got); d != "" {
			t.Fatalf("call %d: %s", i, d)
		}
	}
}

// clusterRowCounts sizes the rows a removed cluster's cascade takes and the
// artifact row its pod owns, which the schema does not key on cluster_id.
type clusterRowCounts struct {
	Namespaces, Pods, Incidents, Events, Artifacts int64
}

func countClusterRows(t *testing.T, s *Store, clusterID int64, podUID string) clusterRowCounts {
	t.Helper()
	var c clusterRowCounts
	err := s.Reader.DB().QueryRowContext(context.Background(), `
SELECT (SELECT COUNT(*) FROM watched_namespaces WHERE cluster_id = ?),
       (SELECT COUNT(*) FROM pods WHERE cluster_id = ?),
       (SELECT COUNT(*) FROM incidents WHERE cluster_id = ?),
       (SELECT COUNT(*) FROM k8s_events WHERE cluster_id = ?),
       (SELECT COUNT(*) FROM artifacts WHERE pod_uid = ?)`,
		clusterID, clusterID, clusterID, clusterID, podUID).
		Scan(&c.Namespaces, &c.Pods, &c.Incidents, &c.Events, &c.Artifacts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// data-storage.md 5.1: grafana_url, loki_datasource_uid and log_selector
// travel together and clearing grafana_url clears all three.
func TestSetClusterGrafanaSetsThenClears(t *testing.T) {
	s, _ := openMigratedStore(t)
	ctx := context.Background()
	cid := insertCluster(t, s, "ks-uid")

	var ok bool
	inTx(t, s, func(tx *sql.Tx) (err error) {
		ok, err = SetClusterGrafana(ctx, tx, 999, "https://logs.example.grafana.net", "grafanacloud-logs", "{namespace=\"$namespace\"}")
		return err
	})
	if ok {
		t.Fatal("SetClusterGrafana(999) = true, want false")
	}

	inTx(t, s, func(tx *sql.Tx) (err error) {
		ok, err = SetClusterGrafana(ctx, tx, cid, "https://logs.example.grafana.net", "grafanacloud-logs", "{namespace=\"$namespace\"}")
		return err
	})
	if !ok {
		t.Fatal("SetClusterGrafana(cid) = false, want true")
	}
	var got *Cluster
	inTx(t, s, func(tx *sql.Tx) (err error) {
		got, err = GetCluster(ctx, tx, cid)
		return err
	})
	want := &Cluster{ID: cid, Identity: ptr("ks-uid"), Name: "c", ContextName: "ctx", APIServerURL: "https://127.0.0.1:26443",
		FirstSeenAt: clock.Format(testEpoch),
		GrafanaURL:  "https://logs.example.grafana.net", LokiDatasourceUID: "grafanacloud-logs", LogSelector: "{namespace=\"$namespace\"}"}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatalf("after set: %s", d)
	}

	inTx(t, s, func(tx *sql.Tx) (err error) {
		ok, err = SetClusterGrafana(ctx, tx, cid, "", "", "")
		return err
	})
	if !ok {
		t.Fatal("SetClusterGrafana clear = false, want true")
	}
	inTx(t, s, func(tx *sql.Tx) (err error) {
		got, err = GetCluster(ctx, tx, cid)
		return err
	})
	want.GrafanaURL, want.LokiDatasourceUID, want.LogSelector = "", "", ""
	if d := cmp.Diff(want, got); d != "" {
		t.Fatalf("after clear: %s", d)
	}
}

func TestRemoveClusterCascades(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	now := clock.Format(clk.Now())
	c1 := insertCluster(t, s, "c1")
	c2 := insertCluster(t, s, "c2")
	for _, cid := range []int64{c1, c2} {
		inTx(t, s, func(tx *sql.Tx) (err error) {
			_, err = AddWatchedNamespace(ctx, tx, cid, "idios-smoke", now)
			return err
		})
	}
	insertPod(t, s, c1, "p1")
	insertPod(t, s, c2, "p2")
	inc1, err := insertPodIncident(t, s, c1, "p1", "api", CategoryCrash)
	if err != nil {
		t.Fatal(err)
	}
	inc2, err := insertPodIncident(t, s, c2, "p2", "api", CategoryCrash)
	if err != nil {
		t.Fatal(err)
	}
	insertEvent(t, s, c1, "e1", "p1", "", nil, ptr(inc1))
	insertEvent(t, s, c2, "e2", "p2", "", nil, ptr(inc2))
	if err := insertArtifact(t, s, "p1", "api", ArtifactLogCurrent, 0, ptr(inc1)); err != nil {
		t.Fatal(err)
	}
	if err := insertArtifact(t, s, "p2", "api", ArtifactLogCurrent, 0, ptr(inc2)); err != nil {
		t.Fatal(err)
	}

	before := clusterRowCounts{Namespaces: 1, Pods: 1, Incidents: 1, Events: 1, Artifacts: 1}
	if d := cmp.Diff(before, countClusterRows(t, s, c1, "p1")); d != "" {
		t.Fatalf("cluster 1 before removal: %s", d)
	}
	if d := cmp.Diff(before, countClusterRows(t, s, c2, "p2")); d != "" {
		t.Fatalf("cluster 2 before removal: %s", d)
	}

	for i, want := range []bool{true, false} {
		var ok bool
		inTx(t, s, func(tx *sql.Tx) (err error) {
			ok, err = RemoveCluster(ctx, tx, c1)
			return err
		})
		if ok != want {
			t.Fatalf("call %d: RemoveCluster = %v, want %v", i, ok, want)
		}
		if d := cmp.Diff(clusterRowCounts{}, countClusterRows(t, s, c1, "p1")); d != "" {
			t.Fatalf("call %d, cluster 1: %s", i, d)
		}
		if d := cmp.Diff(before, countClusterRows(t, s, c2, "p2")); d != "" {
			t.Fatalf("call %d, cluster 2: %s", i, d)
		}
	}
}

func TestRemoveWatchedNamespaceLeavesTheOthers(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	now := clock.Format(clk.Now())
	cid := insertCluster(t, s, "c")
	var ok bool
	inTx(t, s, func(tx *sql.Tx) (err error) {
		ok, err = RemoveWatchedNamespace(ctx, tx, 999, "b")
		return err
	})
	if ok {
		t.Fatal("RemoveWatchedNamespace(999) = true, want false")
	}
	for _, name := range []string{"a", "b"} {
		inTx(t, s, func(tx *sql.Tx) (err error) {
			_, err = AddWatchedNamespace(ctx, tx, cid, name, now)
			return err
		})
	}
	for i, want := range []bool{true, false} {
		inTx(t, s, func(tx *sql.Tx) (err error) {
			ok, err = RemoveWatchedNamespace(ctx, tx, cid, "a")
			return err
		})
		if ok != want {
			t.Fatalf("call %d: RemoveWatchedNamespace = %v, want %v", i, ok, want)
		}
	}
	var got []string
	inTx(t, s, func(tx *sql.Tx) (err error) {
		got, err = ListWatchedNamespaces(ctx, tx, cid)
		return err
	})
	if d := cmp.Diff([]string{"b"}, got); d != "" {
		t.Fatal(d)
	}
}

func TestAddWatchedNamespaceIsIdempotentPerCluster(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	now := clock.Format(clk.Now())
	var c1, c2 int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		if c1, err = InsertCluster(ctx, tx, "a", "a", now); err != nil {
			return err
		}
		c2, err = InsertCluster(ctx, tx, "b", "b", now)
		return err
	})
	steps := []struct {
		name    string
		cluster int64
		ns      string
		want    bool
	}{
		{"first add", c1, "idios-smoke", true},
		{"same namespace again", c1, "idios-smoke", false},
		{"same name in another cluster", c2, "idios-smoke", true},
		{"second namespace", c1, "payments", true},
	}
	for _, st := range steps {
		var got bool
		inTx(t, s, func(tx *sql.Tx) (err error) {
			got, err = AddWatchedNamespace(ctx, tx, st.cluster, st.ns, now)
			return err
		})
		if got != st.want {
			t.Errorf("%s: added = %v, want %v", st.name, got, st.want)
		}
	}
	var got []string
	inTx(t, s, func(tx *sql.Tx) (err error) {
		got, err = ListWatchedNamespaces(ctx, tx, c1)
		return err
	})
	if d := cmp.Diff([]string{"idios-smoke", "payments"}, got); d != "" {
		t.Fatal(d)
	}
}
