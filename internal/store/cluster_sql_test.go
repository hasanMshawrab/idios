package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func TestClusterConnectedClearsErrorKeepsKnownIdentity(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "ks-uid")
	ctx := context.Background()
	inTx(t, s, func(tx *sql.Tx) error {
		return SetClusterError(ctx, tx, cid, "connect: dial tcp 127.0.0.1:26443: connection refused", "2026-08-27T11:59:00.000000Z")
	})
	inTx(t, s, func(tx *sql.Tx) error {
		return MarkClusterConnected(ctx, tx, cid, nil, "https://127.0.0.1:26443", "2026-08-27T12:00:00.000000Z")
	})
	var got []Cluster
	inTx(t, s, func(tx *sql.Tx) (err error) { got, err = ListClusters(ctx, tx); return err })
	want := []Cluster{{ID: cid, Identity: ptr("ks-uid"), Name: "c", ContextName: "ctx", APIServerURL: "https://127.0.0.1:26443",
		FirstSeenAt: clock.Format(testEpoch), LastConnectedAt: ptr("2026-08-27T12:00:00.000000Z")}}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}

func TestWatchedNamespacesPerCluster(t *testing.T) {
	s, _ := openMigratedStore(t)
	a := insertCluster(t, s, "a")
	b := insertCluster(t, s, "b")
	ts := clock.Format(testEpoch)
	mustExec(t, s, "INSERT INTO watched_namespaces (cluster_id, name, added_at) VALUES (?, 'payments', ?), (?, 'idios-smoke', ?), (?, 'other', ?)", a, ts, a, ts, b, ts)
	var got []string
	inTx(t, s, func(tx *sql.Tx) (err error) {
		got, err = ListWatchedNamespaces(context.Background(), tx, a)
		return err
	})
	if d := cmp.Diff([]string{"idios-smoke", "payments"}, got); d != "" {
		t.Fatal(d)
	}
}
