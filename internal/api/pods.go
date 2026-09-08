package api

import (
	"context"

	sebufhttp "github.com/SebastienMelki/sebuf/http"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query"
)

// ListPods answers the pod list.
func (s *Server) ListPods(ctx context.Context, req *idiosv1.ListPodsRequest) (*idiosv1.PodsResponse, error) {
	live, err := validateListPods(req)
	if err != nil {
		return nil, err
	}
	rows, truncated, err := query.ListPods(ctx, s.db, query.PodFilter{
		ClusterIDs:   req.GetClusterIds(),
		Namespace:    req.GetNamespace(),
		WorkloadKind: req.GetWorkloadKind(),
		WorkloadName: req.GetWorkloadName(),
		PodName:      req.GetPodName(),
		JobUID:       req.GetJobUid(),
		Live:         live,
	}, query.Page{Limit: s.limit(req.GetLimit())})
	if err != nil {
		return nil, err
	}
	return &idiosv1.PodsResponse{Pods: mapAll(rows, podRow), Truncated: truncated}, nil
}

// GetPod answers the pod page.
func (s *Server) GetPod(ctx context.Context, req *idiosv1.GetPodRequest) (*idiosv1.PodDetail, error) {
	d, err := query.GetPod(ctx, s.db, req.GetUid())
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, &notFoundError{what: "pod", id: req.GetUid()}
	}
	return podDetail(d), nil
}

// A uid nothing was ever recorded for has no events and no history, which is
// the same answer as a pod that drew neither: the pod row is what tells a
// missing pod from a quiet one, so the two endpoints below never 404.

// PodEvents answers every event about the pod, newest first.
func (s *Server) PodEvents(ctx context.Context, req *idiosv1.PodEventsRequest) (*idiosv1.EventsResponse, error) {
	if err := validateLimit(req.GetLimit()); err != nil {
		return nil, err
	}
	events, truncated, err := query.PodEvents(ctx, s.db, req.GetUid(), query.Page{Limit: s.limit(req.GetLimit())})
	if err != nil {
		return nil, err
	}
	return &idiosv1.EventsResponse{Events: mapAll(events, event), Truncated: truncated}, nil
}

// PodHistory answers the pod's transitions and condition changes.
func (s *Server) PodHistory(ctx context.Context, req *idiosv1.PodHistoryRequest) (*idiosv1.HistoryResponse, error) {
	history, err := query.PodHistory(ctx, s.db, req.GetUid())
	if err != nil {
		return nil, err
	}
	return &idiosv1.HistoryResponse{
		Transitions: mapAll(history.Transitions, transition),
		Conditions:  mapAll(history.Conditions, condition),
	}, nil
}

// validateListPods rejects the filter values with a closed vocabulary and
// returns the parsed liveness filter. Every violation is reported at once so
// a caller fixes the whole request in one round trip.
func validateListPods(req *idiosv1.ListPodsRequest) (*bool, error) {
	var violations []*sebufhttp.FieldViolation
	live := boolFilter("live", req.GetLive(), &violations)
	if v := limitViolation(req.GetLimit()); v != nil {
		violations = append(violations, v)
	}
	if len(violations) == 0 {
		return live, nil
	}
	return nil, &sebufhttp.ValidationError{Violations: violations}
}

// boolFilter parses a two-valued query flag, nil when absent, and records any
// other spelling as a violation on field. The flag travels as a string
// because sebuf applies the enum spelling to JSON bodies only, never to
// query parameters.
func boolFilter(field, value string, violations *[]*sebufhttp.FieldViolation) *bool {
	switch value {
	case "":
		return nil
	case "true", "false":
		parsed := value == "true"
		return &parsed
	}
	*violations = append(*violations, &sebufhttp.FieldViolation{
		Field: field, Description: field + " must be true or false, got " + value,
	})
	return nil
}

// podRow maps one line of the pod list: the pod row flattened with the counts
// and the badge state the line shows.
func podRow(r query.PodRow) *idiosv1.PodRow {
	p := r.Pod
	return &idiosv1.PodRow{
		Uid:                 p.UID,
		ClusterId:           p.ClusterID,
		Namespace:           p.Namespace,
		Name:                p.Name,
		NodeName:            p.NodeName,
		Phase:               p.Phase,
		StatusReason:        p.StatusReason,
		StatusMessage:       p.StatusMessage,
		DeletionRequestedAt: p.DeletionRequestedAt,
		QosClass:            p.QOSClass,
		ControllerKind:      p.ControllerKind,
		ControllerName:      p.ControllerName,
		ControllerUid:       p.ControllerUID,
		WorkloadKind:        p.WorkloadKind,
		WorkloadName:        p.WorkloadName,
		CreatedAt:           p.CreatedAt,
		StartedAt:           p.StartedAt,
		FirstSeenAt:         p.FirstSeenAt,
		LastSeenAt:          p.LastSeenAt,
		DeletedAt:           p.DeletedAt,
		DeletionSource:      enumPtr(deletionSources, p.DeletionSource),
		DeletionReason:      enumPtr(deletionReasons, p.DeletionReason),
		OpenIncidents:       r.OpenIncidents,
		ContainerCount:      r.ContainerCount,
		WorstState:          containerStates[r.WorstState],
	}
}

// podDetail maps everything the pod page shows. Each container's Grafana
// link is per-run, so it is filled here rather than in the shared container
// mapper, which the incident page also uses for its single, incident-scoped
// link.
func podDetail(d *query.PodDetail) *idiosv1.PodDetail {
	containers := mapAll(d.Containers, container)
	for _, c := range containers {
		if url, ok := d.ContainerGrafanaURLs[c.Name]; ok {
			c.GrafanaUrl = &url
		}
	}
	return &idiosv1.PodDetail{
		Pod:          podRow(d.Pod),
		Containers:   containers,
		Conditions:   mapAll(d.Conditions, condition),
		Incidents:    mapAll(d.Incidents, incidentRow),
		Artifacts:    mapAll(d.Artifacts, artifact),
		Siblings:     mapAll(d.Siblings, siblingPod),
		SiblingTotal: int32(d.SiblingTotal),
	}
}

// siblingPod maps one other pod of the same controller; the badge is
// absent when nothing is open on it.
func siblingPod(p query.SiblingPod) *idiosv1.SiblingPod {
	s := &idiosv1.SiblingPod{
		Uid:          p.UID,
		Name:         p.Name,
		Phase:        p.Phase,
		DeletedAt:    p.DeletedAt,
		RestartCount: int32(p.RestartCount),
		Ready:        p.Ready,
	}
	if c, ok := categories[p.WorstOpenCategory]; ok {
		s.WorstOpenCategory = &c
	}
	return s
}
