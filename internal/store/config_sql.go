package store

import (
	"context"
	"database/sql"
	"errors"
)

// InsertCluster adds a cluster row the watcher has not reached yet: no
// identity and an empty api_server_url, both filled on the first connection.
func InsertCluster(ctx context.Context, tx *sql.Tx, name, contextName, now string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
INSERT INTO clusters (name, context_name, api_server_url, first_seen_at) VALUES (?, ?, '', ?)`, name, contextName, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindClusterByName returns the cluster row with that name, nil when there
// is none. The schema does not make names unique; the CLI refuses a second
// row of the same name, so the lowest id is the one it created.
func FindClusterByName(ctx context.Context, tx *sql.Tx, name string) (*Cluster, error) {
	c, err := scanCluster(tx.QueryRowContext(ctx, "SELECT "+clusterColumns+" FROM clusters WHERE name = ? ORDER BY id LIMIT 1", name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AddWatchedNamespace records that name is watched in the cluster and
// reports whether the row is new.
func AddWatchedNamespace(ctx context.Context, tx *sql.Tx, clusterID int64, name, now string) (bool, error) {
	n, err := execCount(ctx, tx, `
INSERT INTO watched_namespaces (cluster_id, name, added_at) VALUES (?, ?, ?)
ON CONFLICT (cluster_id, name) DO NOTHING`, clusterID, name, now)
	return n == 1, err
}

// GetCluster returns one cluster row, nil when there is none.
func GetCluster(ctx context.Context, tx *sql.Tx, id int64) (*Cluster, error) {
	c, err := scanCluster(tx.QueryRowContext(ctx, "SELECT "+clusterColumns+" FROM clusters WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// RenameCluster sets the friendly name and reports whether the row exists.
func RenameCluster(ctx context.Context, tx *sql.Tx, id int64, name string) (bool, error) {
	n, err := execCount(ctx, tx, "UPDATE clusters SET name = ? WHERE id = ?", name, id)
	return n == 1, err
}

// SetClusterGrafana sets the three Grafana columns together and reports
// whether the row exists; an empty grafanaURL is the clear.
func SetClusterGrafana(ctx context.Context, tx *sql.Tx, id int64, grafanaURL, datasourceUID, selector string) (bool, error) {
	n, err := execCount(ctx, tx, "UPDATE clusters SET grafana_url = ?, loki_datasource_uid = ?, log_selector = ? WHERE id = ?",
		grafanaURL, datasourceUID, selector, id)
	return n == 1, err
}

// RemoveCluster deletes the row; the cascades take its namespaces, pods,
// jobs, incidents, events and rollouts. Reports whether a row was there.
func RemoveCluster(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	n, err := execCount(ctx, tx, "DELETE FROM clusters WHERE id = ?", id)
	return n == 1, err
}

// RemoveWatchedNamespace stops watching name in the cluster and reports
// whether a row was there.
func RemoveWatchedNamespace(ctx context.Context, tx *sql.Tx, clusterID int64, name string) (bool, error) {
	n, err := execCount(ctx, tx, "DELETE FROM watched_namespaces WHERE cluster_id = ? AND name = ?", clusterID, name)
	return n == 1, err
}
