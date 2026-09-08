package api

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/query"
	"github.com/hasanMshawrab/idios/internal/store"
)

// AcknowledgeIncident stamps acknowledged_at once and answers the row as it
// stands after the write.
func (s *Server) AcknowledgeIncident(ctx context.Context, req *idiosv1.AcknowledgeIncidentRequest) (*idiosv1.IncidentRow, error) {
	id := req.GetId()
	at := clock.Format(s.clk.Now())
	return s.incidentAction(ctx, id, func(tx *sql.Tx) (bool, error) {
		return store.AcknowledgeIncident(ctx, tx, id, at)
	})
}

// UnacknowledgeIncident clears acknowledged_at and answers the row as it
// stands after the write.
func (s *Server) UnacknowledgeIncident(ctx context.Context, req *idiosv1.UnacknowledgeIncidentRequest) (*idiosv1.IncidentRow, error) {
	id := req.GetId()
	return s.incidentAction(ctx, id, func(tx *sql.Tx) (bool, error) {
		return store.UnacknowledgeIncident(ctx, tx, id)
	})
}

// ResolveIncident closes an open incident as manual and answers the row as
// it stands after the write.
func (s *Server) ResolveIncident(ctx context.Context, req *idiosv1.ResolveIncidentRequest) (*idiosv1.IncidentRow, error) {
	id := req.GetId()
	at := clock.Format(s.clk.Now())
	return s.incidentAction(ctx, id, func(tx *sql.Tx) (bool, error) {
		return store.ResolveIncident(ctx, tx, id, at)
	})
}

// UnresolveIncident reopens a manually closed incident and answers the row
// as it stands after the write.
func (s *Server) UnresolveIncident(ctx context.Context, req *idiosv1.UnresolveIncidentRequest) (*idiosv1.IncidentRow, error) {
	id := req.GetId()
	return s.incidentAction(ctx, id, func(tx *sql.Tx) (bool, error) {
		return store.UnresolveIncident(ctx, tx, id)
	})
}

// DismissIncident stamps dismissed_at once and answers the row as it stands
// after the write.
func (s *Server) DismissIncident(ctx context.Context, req *idiosv1.DismissIncidentRequest) (*idiosv1.IncidentRow, error) {
	id := req.GetId()
	at := clock.Format(s.clk.Now())
	return s.incidentAction(ctx, id, func(tx *sql.Tx) (bool, error) {
		return store.DismissIncident(ctx, tx, id, at)
	})
}

// UndismissIncident clears dismissed_at and answers the row as it stands
// after the write.
func (s *Server) UndismissIncident(ctx context.Context, req *idiosv1.UndismissIncidentRequest) (*idiosv1.IncidentRow, error) {
	id := req.GetId()
	return s.incidentAction(ctx, id, func(tx *sql.Tx) (bool, error) {
		return store.UndismissIncident(ctx, tx, id)
	})
}

// SetIncidentNote replaces the note, clearing it on an empty string, and
// answers the row as it stands after the write.
func (s *Server) SetIncidentNote(ctx context.Context, req *idiosv1.SetIncidentNoteRequest) (*idiosv1.IncidentRow, error) {
	id := req.GetId()
	note := req.GetNote()
	return s.incidentAction(ctx, id, func(tx *sql.Tx) (bool, error) {
		return store.SetIncidentNote(ctx, tx, id, note)
	})
}

// DeleteIncident removes the incident's captured files, detaches the
// history and event rows it had claimed, and removes the row itself. It
// answers the same empty response whether or not the id existed.
func (s *Server) DeleteIncident(ctx context.Context, req *idiosv1.DeleteIncidentRequest) (*idiosv1.DeleteIncidentResponse, error) {
	if s.writer == nil {
		return nil, errNoWriter
	}
	id := req.GetId()
	if err := s.writer.Tx(ctx, func(tx *sql.Tx) error {
		files, err := store.ListIncidentArtifactFiles(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := removeArtifactFiles(s.cfg.ArtifactsRoot, files); err != nil {
			return err
		}
		if _, err := store.DeleteIncidentArtifacts(ctx, tx, id); err != nil {
			return err
		}
		return store.DeleteIncident(ctx, tx, id)
	}); err != nil {
		return nil, err
	}
	// The stream reloads the row by id; a deleted row sends nothing, and the
	// notify here is only for symmetry with every other write.
	s.events.Notify(notify.Incident, id)
	return &idiosv1.DeleteIncidentResponse{}, nil
}

// incidentAction runs one store write, then answers the row as the list
// shows it and tells the stream. The notify happens after the commit: a
// subscriber reloads the row on the event, and a row still inside an open
// transaction is the old row a concurrent reader would see anyway.
func (s *Server) incidentAction(ctx context.Context, id int64, write func(*sql.Tx) (bool, error)) (*idiosv1.IncidentRow, error) {
	if s.writer == nil {
		return nil, errNoWriter
	}
	notFound := &notFoundError{what: "incident", id: strconv.FormatInt(id, 10)}
	var ok bool
	if err := s.writer.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		ok, err = write(tx)
		return err
	}); err != nil {
		return nil, err
	}
	if !ok {
		return nil, notFound
	}
	// The commit is what changed the row; a failed reload must not hide it
	// from the subscribers.
	s.events.Notify(notify.Incident, id)
	rows, _, err := query.ListIncidents(ctx, s.db, query.IncidentFilter{ID: id}, query.Page{Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, notFound
	}
	return incidentRow(rows[0]), nil
}
