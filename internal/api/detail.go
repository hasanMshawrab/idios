package api

import (
	"context"
	"strconv"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query"
)

// GetIncident answers the incident detail page.
func (s *Server) GetIncident(ctx context.Context, req *idiosv1.GetIncidentRequest) (*idiosv1.IncidentDetail, error) {
	d, err := query.GetIncident(ctx, s.db, req.GetId(), query.Page{Limit: s.limit(0)})
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, &notFoundError{what: "incident", id: strconv.FormatInt(req.GetId(), 10)}
	}
	return incidentDetail(d), nil
}

// IncidentTimeline answers the merged history of one incident. An incident
// that exists always carries its opened entry, so an empty result is an
// incident that does not.
func (s *Server) IncidentTimeline(ctx context.Context, req *idiosv1.IncidentTimelineRequest) (*idiosv1.TimelineResponse, error) {
	entries, err := query.IncidentTimeline(ctx, s.db, req.GetId())
	if err != nil {
		return nil, err
	}
	if entries == nil {
		return nil, &notFoundError{what: "incident", id: strconv.FormatInt(req.GetId(), 10)}
	}
	return &idiosv1.TimelineResponse{Entries: mapAll(entries, timelineEntry)}, nil
}

// incidentDetail maps everything the detail page shows.
func incidentDetail(d *query.IncidentDetail) *idiosv1.IncidentDetail {
	return &idiosv1.IncidentDetail{
		Incident:         incidentRow(d.Incident),
		Pod:              pod(d.Pod),
		Containers:       mapAll(d.Containers, container),
		Artifacts:        mapAll(d.Artifacts, artifact),
		Events:           mapAll(d.Events, event),
		Conditions:       mapAll(d.Conditions, condition),
		Job:              jobRowPtr(d.Job),
		LastPodName:      d.LastPodName,
		GrafanaUrl:       d.GrafanaURL,
		RelatedIncidents: mapAll(d.RelatedIncidents, incidentRow),
	}
}

// jobRowPtr maps the job card, absent for an incident under no Job.
func jobRowPtr(r *query.JobRow) *idiosv1.JobRow {
	if r == nil {
		return nil
	}
	return jobRow(*r)
}

// timelineEntry maps one timeline line. Only the fields the entry's kind
// carries are set; the rest stay absent.
func timelineEntry(e query.TimelineEntry) *idiosv1.TimelineEntry {
	return &idiosv1.TimelineEntry{
		Kind:             timelineKinds[e.Kind],
		K8SAt:            e.K8sAt,
		ObservedAt:       e.ObservedAt,
		ContainerName:    e.ContainerName,
		State:            enumPtr(containerStates, e.State),
		Reason:           e.Reason,
		ExitCode:         int32Ptr(e.ExitCode),
		Signal:           int32Ptr(e.Signal),
		RestartCount:     int32Ptr(e.RestartCount),
		GapReconstructed: e.GapReconstructed,
		ConditionType:    e.ConditionType,
		ConditionStatus:  e.ConditionStatus,
		Message:          e.Message,
		EventType:        e.EventType,
		EventReason:      e.EventReason,
		Count:            int32Ptr(e.Count),
		ArtifactId:       e.ArtifactID,
		ArtifactKind:     enumPtr(artifactKinds, e.ArtifactKind),
		CaptureGap:       enumPtr(captureGaps, e.CaptureGap),
		ReplicasetName:   e.ReplicaSetName,
		ImageTag:         e.ImageTag,
		Revision:         e.Revision,
		Lifecycle:        enumPtr(lifecycleSteps, e.Lifecycle),
		CloseReason:      enumPtr(closeReasons, e.CloseReason),
	}
}
