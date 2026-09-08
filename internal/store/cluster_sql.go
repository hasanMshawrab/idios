package store

import (
	"context"
	"database/sql"
)

// LiveObject is a pod or job row not yet marked deleted.
type LiveObject struct {
	UID       string
	Namespace string
}

// clusterColumns lists the clusters columns in the order scanCluster expects.
const clusterColumns = `id, identity, name, context_name, api_server_url, first_seen_at, last_connected_at, last_error, last_error_at, grafana_url, loki_datasource_uid, log_selector`

func scanCluster(r rowScanner) (Cluster, error) {
	var c Cluster
	err := r.Scan(&c.ID, &c.Identity, &c.Name, &c.ContextName, &c.APIServerURL, &c.FirstSeenAt, &c.LastConnectedAt, &c.LastError, &c.LastErrorAt,
		&c.GrafanaURL, &c.LokiDatasourceUID, &c.LogSelector)
	return c, err
}

// ListClusters returns every cluster row in id order.
func ListClusters(ctx context.Context, db Querier) ([]Cluster, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+clusterColumns+" FROM clusters ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Cluster
	for rows.Next() {
		c, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListWatchedNamespaces returns the namespace names watched in one cluster.
func ListWatchedNamespaces(ctx context.Context, tx *sql.Tx, clusterID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name FROM watched_namespaces WHERE cluster_id = ? ORDER BY name", clusterID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkClusterConnected records a successful connection and clears the last
// error. A nil identity keeps the one an earlier connection learned.
func MarkClusterConnected(ctx context.Context, tx *sql.Tx, id int64, identity *string, apiServerURL, at string) error {
	_, err := tx.ExecContext(ctx, `
UPDATE clusters SET identity = COALESCE(?, identity), api_server_url = ?, last_connected_at = ?, last_error = NULL, last_error_at = NULL
WHERE id = ?`, identity, apiServerURL, at, id)
	return err
}

// SetClusterError records the most recent failure in words a user can act on.
func SetClusterError(ctx context.Context, tx *sql.Tx, id int64, msg, at string) error {
	_, err := tx.ExecContext(ctx, "UPDATE clusters SET last_error = ?, last_error_at = ? WHERE id = ?", msg, at, id)
	return err
}

// LoadLivePods returns the cluster's pod rows not yet marked deleted.
func LoadLivePods(ctx context.Context, tx *sql.Tx, clusterID int64) ([]LiveObject, error) {
	return loadLive(ctx, tx, "pods", clusterID)
}

// LoadLiveJobs returns the cluster's job rows not yet marked deleted.
func LoadLiveJobs(ctx context.Context, tx *sql.Tx, clusterID int64) ([]LiveObject, error) {
	return loadLive(ctx, tx, "jobs", clusterID)
}

func loadLive(ctx context.Context, tx *sql.Tx, table string, clusterID int64) ([]LiveObject, error) {
	rows, err := tx.QueryContext(ctx, "SELECT uid, namespace FROM "+table+" WHERE cluster_id = ? AND deleted_at IS NULL ORDER BY uid", clusterID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []LiveObject
	for rows.Next() {
		var o LiveObject
		if err := rows.Scan(&o.UID, &o.Namespace); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
