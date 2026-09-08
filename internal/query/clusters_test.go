package query

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

const seedConnectedAt = "2026-08-27T12:00:00.000000Z"

func seededClusters() []ClusterRow {
	return []ClusterRow{
		{Cluster: store.Cluster{
			ID: querytest.ClusterProd, Identity: sp("id-prod"), Name: "prod", ContextName: "prod",
			APIServerURL: "https://127.0.0.1:26443", FirstSeenAt: seedConnectedAt,
			LastConnectedAt: sp(seedConnectedAt),
		}, Namespaces: []string{"default", seedNS}},
		{Cluster: store.Cluster{
			ID: querytest.ClusterStaging, Identity: sp("id-staging"), Name: "staging", ContextName: "staging",
			APIServerURL: "https://127.0.0.1:26444", FirstSeenAt: seedConnectedAt,
			LastConnectedAt: sp(seedConnectedAt),
		}, Namespaces: []string{seedNS, "staging-web"}},
	}
}

// The scope list names every cluster, watched or not: a cluster added but not
// yet given a namespace must still be offered, so its namespaces are absent
// rather than the row.
func TestListClustersCarriesWatchedNamespaces(t *testing.T) {
	addedAt := "2026-08-27T12:01:00.000000Z"
	cases := []struct {
		name  string
		setup func(t *testing.T, st *store.Store)
		want  []ClusterRow
	}{
		{name: "seeded clusters", want: seededClusters()},
		{
			name: "a cluster without namespaces",
			setup: func(t *testing.T, st *store.Store) {
				t.Helper()
				ctx := context.Background()
				if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
					_, err := store.InsertCluster(ctx, tx, "dev", "dev", addedAt)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			},
			want: append(seededClusters(), ClusterRow{Cluster: store.Cluster{
				ID: 3, Name: "dev", ContextName: "dev", FirstSeenAt: addedAt,
			}}),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := querytest.Seed(t)
			if c.setup != nil {
				c.setup(t, st)
			}
			got, err := ListClusters(context.Background(), st.Reader.DB())
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("rows (-want +got):\n%s", diff)
			}
		})
	}
}
