package query

import (
	"context"

	"github.com/hasanMshawrab/idios/internal/store"
)

// GetArtifact returns one artifact row, or nil when no artifact has that id.
func GetArtifact(ctx context.Context, db store.Querier, id int64) (*store.Artifact, error) {
	rows, err := collect(ctx, db, scanArtifact, "SELECT "+artifactColumns+" FROM artifacts WHERE id = ?", id)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// ListPodArtifacts returns every artifact captured for one pod, whichever
// incident each capture attached to.
func ListPodArtifacts(ctx context.Context, db store.Querier, podUID string) ([]store.Artifact, error) {
	return collect(ctx, db, scanArtifact, "SELECT "+artifactColumns+
		" FROM artifacts WHERE pod_uid = ? ORDER BY container_name, kind, restart_count", podUID)
}
