package store

import (
	"context"
	"database/sql"
)

// StuckCandidate is a live pod that has been waiting since before the edge
// with no open incident, and the container holding it up.
type StuckCandidate struct {
	PodUID, Namespace, ContainerName, Reason string
	ClusterID                                int64
	WorkloadKind, WorkloadName               string
	Since                                    string
}

// ListStuckCandidates returns the live pods whose containers have all been
// waiting since before edge, with no incident of any category open on them.
// Init containers are not part of "all": a pod held by one has its app
// containers waiting too, and the init container is the one that names the
// reason, so it is preferred as the candidate's container. A pod whose
// container rows have not arrived yet is not a candidate; there is nothing
// to name. A pod already terminating is excluded too: it is on its way out
// on its own, not stuck.
func ListStuckCandidates(ctx context.Context, tx *sql.Tx, edge string) ([]StuckCandidate, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT p.uid, p.namespace, p.cluster_id, p.workload_kind, p.workload_name, p.created_at,
       c.name, COALESCE(c.reason, '')
FROM pods p JOIN containers c ON c.pod_uid = p.uid
WHERE p.deleted_at IS NULL AND p.deletion_requested_at IS NULL AND p.created_at < ?
  AND c.kind IN (?, ?, ?) AND c.state = ? AND c.updated_at < ?
  AND EXISTS (SELECT 1 FROM containers a WHERE a.pod_uid = p.uid AND a.kind IN (?, ?))
  AND NOT EXISTS (
        SELECT 1 FROM containers a
        WHERE a.pod_uid = p.uid AND a.kind IN (?, ?)
          AND NOT (a.state = ? AND a.updated_at < ?))
  AND NOT EXISTS (SELECT 1 FROM incidents i WHERE i.pod_uid = p.uid AND i.closed_at IS NULL)
ORDER BY p.uid, c.kind <> ?, c.name`,
		edge,
		ContainerKindInit, ContainerKindApp, ContainerKindSidecar, StateWaiting, edge,
		ContainerKindApp, ContainerKindSidecar,
		ContainerKindApp, ContainerKindSidecar, StateWaiting, edge,
		ContainerKindInit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []StuckCandidate
	for rows.Next() {
		var c StuckCandidate
		if err := rows.Scan(&c.PodUID, &c.Namespace, &c.ClusterID, &c.WorkloadKind, &c.WorkloadName, &c.Since,
			&c.ContainerName, &c.Reason); err != nil {
			return nil, err
		}
		// One candidate per pod: the ORDER BY put the container that names
		// the reason first.
		if len(out) > 0 && out[len(out)-1].PodUID == c.PodUID {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
