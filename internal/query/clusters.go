package query

import (
	"context"
	"database/sql"

	"github.com/hasanMshawrab/idios/internal/store"
)

// ClusterRow is one cluster with the namespaces it watches. The runtime state
// the scope list also shows is merged in by the caller: this package never
// reads process memory.
type ClusterRow struct {
	store.Cluster
	Namespaces []string
}

const clusterRowColumns = `id, identity, name, context_name, api_server_url, first_seen_at,
       last_connected_at, last_error, last_error_at, grafana_url, loki_datasource_uid, log_selector`

func scanClusterRow(r *sql.Rows) (ClusterRow, error) {
	var row ClusterRow
	c := &row.Cluster
	err := r.Scan(&c.ID, &c.Identity, &c.Name, &c.ContextName, &c.APIServerURL, &c.FirstSeenAt,
		&c.LastConnectedAt, &c.LastError, &c.LastErrorAt, &c.GrafanaURL, &c.LokiDatasourceUID, &c.LogSelector)
	return row, err
}

type watchedNamespace struct {
	clusterID int64
	name      string
}

func scanWatchedNamespace(r *sql.Rows) (watchedNamespace, error) {
	var n watchedNamespace
	err := r.Scan(&n.clusterID, &n.name)
	return n, err
}

// ListClusters returns every cluster in id order, each carrying its watched
// namespaces. A cluster with none carries a nil slice.
func ListClusters(ctx context.Context, db store.Querier) ([]ClusterRow, error) {
	rows, err := collect(ctx, db, scanClusterRow, "SELECT "+clusterRowColumns+" FROM clusters ORDER BY id")
	if err != nil {
		return nil, err
	}
	namespaces, err := collect(ctx, db, scanWatchedNamespace,
		"SELECT cluster_id, name FROM watched_namespaces ORDER BY cluster_id, name")
	if err != nil {
		return nil, err
	}
	byID := map[int64]*ClusterRow{}
	for i := range rows {
		byID[rows[i].ID] = &rows[i]
	}
	for _, n := range namespaces {
		if row := byID[n.clusterID]; row != nil {
			row.Namespaces = append(row.Namespaces, n.name)
		}
	}
	return rows, nil
}
