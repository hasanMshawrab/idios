package api

import (
	"cmp"
	"context"
	"slices"

	sebufhttp "github.com/SebastienMelki/sebuf/http"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/query"
)

// ListIncidents answers the triage list.
func (s *Server) ListIncidents(ctx context.Context, req *idiosv1.ListIncidentsRequest) (*idiosv1.IncidentsResponse, error) {
	if err := validateListIncidents(req); err != nil {
		return nil, err
	}
	rows, truncated, err := query.ListIncidents(ctx, s.db, query.IncidentFilter{
		ClusterIDs:     req.GetClusterIds(),
		State:          req.GetState(),
		Category:       req.GetCategory(),
		Namespace:      req.GetNamespace(),
		WorkloadKind:   req.GetWorkloadKind(),
		WorkloadName:   req.GetWorkloadName(),
		PodUID:         req.GetPodUid(),
		JobUID:         req.GetJobUid(),
		NodeName:       req.GetNodeName(),
		PodName:        req.GetPodName(),
		AttentionSince: clock.Format(s.clk.Now().Add(-s.cfg.AttentionWindow)),
	}, query.Page{Limit: s.limit(req.GetLimit())})
	if err != nil {
		return nil, err
	}
	out := &idiosv1.IncidentsResponse{Truncated: truncated}
	for _, r := range rows {
		out.Incidents = append(out.Incidents, incidentRow(r))
	}
	return out, nil
}

// GetIncidentCounts answers the sidebar numbers.
func (s *Server) GetIncidentCounts(ctx context.Context, req *idiosv1.GetIncidentCountsRequest) (*idiosv1.IncidentCounts, error) {
	if err := validateIncidentCounts(req); err != nil {
		return nil, err
	}
	counts, err := query.CountIncidents(ctx, s.db, req.GetClusterIds(), req.GetState(), req.GetCategory(),
		clock.Format(s.clk.Now().Add(-s.cfg.AttentionWindow)))
	if err != nil {
		return nil, err
	}
	return incidentCounts(counts), nil
}

// validateListIncidents rejects the filter values with a closed vocabulary.
// Every violation is reported at once so a caller fixes the whole request in
// one round trip.
func validateListIncidents(req *idiosv1.ListIncidentsRequest) error {
	violations := facetViolations(req.GetState(), req.GetCategory())
	if v := limitViolation(req.GetLimit()); v != nil {
		violations = append(violations, v)
	}
	return validationError(violations)
}

// validateIncidentCounts rejects the facet selections with a closed
// vocabulary.
func validateIncidentCounts(req *idiosv1.GetIncidentCountsRequest) error {
	return validationError(facetViolations(req.GetState(), req.GetCategory()))
}

// facetViolations reports the state and category values outside their
// vocabulary. They travel as strings because sebuf applies the enum spelling
// to JSON bodies only, never to query parameters.
func facetViolations(state, category string) []*sebufhttp.FieldViolation {
	var violations []*sebufhttp.FieldViolation
	if state != "" {
		if _, ok := incidentStates[state]; !ok {
			violations = append(violations, &sebufhttp.FieldViolation{
				Field: "state", Description: "unknown incident state " + state,
			})
		}
	}
	if category != "" {
		if _, ok := categories[category]; !ok {
			violations = append(violations, &sebufhttp.FieldViolation{
				Field: "category", Description: "unknown category " + category,
			})
		}
	}
	return violations
}

// validationError turns the collected violations into a 400, or into no error
// when there are none.
func validationError(violations []*sebufhttp.FieldViolation) error {
	if len(violations) == 0 {
		return nil
	}
	return &sebufhttp.ValidationError{Violations: violations}
}

// incidentRow maps one triage row to the wire.
func incidentRow(r query.IncidentRow) *idiosv1.IncidentRow {
	return &idiosv1.IncidentRow{
		Id:                r.ID,
		ClusterId:         r.ClusterID,
		Namespace:         r.Namespace,
		SubjectKind:       subjectKinds[r.SubjectKind],
		PodUid:            r.PodUID,
		JobUid:            r.JobUID,
		ContainerName:     r.ContainerName,
		WorkloadKind:      r.WorkloadKind,
		WorkloadName:      r.WorkloadName,
		Category:          categories[r.Category],
		FirstReason:       r.FirstReason,
		LastReason:        r.LastReason,
		LastMessage:       r.LastMessage,
		Image:             r.Image,
		ImageTag:          r.ImageTag,
		ImageId:           r.ImageID,
		NodeName:          r.NodeName,
		Occurrences:       int32(r.Occurrences),
		OpenedAt:          r.OpenedAt,
		LastSeenAt:        r.LastSeenAt,
		ClosedAt:          r.ClosedAt,
		CloseReason:       enumPtr(closeReasons, r.CloseReason),
		AcknowledgedAt:    r.AcknowledgedAt,
		DismissedAt:       r.DismissedAt,
		Note:              r.Note,
		State:             incidentStates[r.State],
		PodName:           r.PodName,
		PodDeletedAt:      r.PodDeletedAt,
		PodDeletionReason: enumPtr(deletionReasons, r.PodDeletionReason),
		ContainerCount:    r.ContainerCount,
		ExitCode:          int32Ptr(r.ExitCode),
		Signal:            int32Ptr(r.Signal),
	}
}

// incidentCounts maps the grouped counts to the wire. Both lists are sorted
// in enum order so the sidebar renders stably.
func incidentCounts(c query.IncidentCounts) *idiosv1.IncidentCounts {
	out := &idiosv1.IncidentCounts{}
	for state, n := range c.ByState {
		out.ByState = append(out.ByState, &idiosv1.StateCount{State: incidentStates[state], Count: int32(n)})
	}
	slices.SortFunc(out.ByState, func(a, b *idiosv1.StateCount) int {
		return cmp.Compare(a.GetState(), b.GetState())
	})
	for category, n := range c.ByCategory {
		out.ByCategory = append(out.ByCategory, &idiosv1.CategoryCount{Category: categories[category], Count: int32(n)})
	}
	slices.SortFunc(out.ByCategory, func(a, b *idiosv1.CategoryCount) int {
		return cmp.Compare(a.GetCategory(), b.GetCategory())
	})
	return out
}
