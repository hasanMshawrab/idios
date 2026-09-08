package api

import (
	"context"
	"strconv"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query"
)

// GetArtifact answers one artifact row.
func (s *Server) GetArtifact(ctx context.Context, req *idiosv1.GetArtifactRequest) (*idiosv1.Artifact, error) {
	a, err := query.GetArtifact(ctx, s.db, req.GetId())
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, &notFoundError{what: "artifact", id: strconv.FormatInt(req.GetId(), 10)}
	}
	return artifact(*a), nil
}
