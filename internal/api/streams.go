package api

import (
	"context"
	"time"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/query"
)

// StreamIncidents sends the whole triage row of every incident that changes,
// scoped to the requested clusters.
func (s *Server) StreamIncidents(ctx context.Context, req *idiosv1.StreamIncidentsRequest, sender idiosv1.SSESender) error {
	return s.stream(ctx, notify.Incident, sender, func(id int64) error {
		rows, _, err := query.ListIncidents(ctx, s.db, query.IncidentFilter{
			ClusterIDs: req.GetClusterIds(), ID: id,
		}, query.Page{Limit: 1})
		if err != nil || len(rows) == 0 {
			return err
		}
		return sender.Send(incidentRow(rows[0]))
	})
}

// StreamClusters sends the whole cluster row of every watcher whose state
// changes, scoped to the requested clusters.
func (s *Server) StreamClusters(ctx context.Context, req *idiosv1.StreamClustersRequest, sender idiosv1.SSESender) error {
	return s.stream(ctx, notify.Cluster, sender, func(id int64) error {
		if !inScope(req.GetClusterIds(), id) {
			return nil
		}
		rows, err := query.ListClusters(ctx, s.db)
		if err != nil {
			return err
		}
		live := s.clusterState()
		for _, r := range rows {
			if r.ID == id {
				return sender.Send(cluster(r, live[id]))
			}
		}
		return nil
	})
}

// stream reloads and sends every row of kind that changes, until the client
// goes away or the subscription is dropped, which the client sees as the end
// of the stream and answers by reloading the list and subscribing again.
//
// Sends are throttled per row id: the first change of a window is reloaded at
// once, and a burst inside the window collapses into one trailing send when
// the window ends, so a crash loop cannot flood the connection and the client
// still ends on the row as it now stands. Another id inside the same window
// is not delayed: the window is per row, not per connection. The deadlines
// are monotonic runtime times rather than the injected clock, because they
// have to fire on their own and a stopped clock never would.
func (s *Server) stream(ctx context.Context, kind notify.Kind, sender idiosv1.SSESender, reload func(id int64) error) error {
	events := s.events.Subscribe(ctx)
	// The generated handler writes nothing until the first event, and a client
	// waits on the response head: without this the connection looks unanswered
	// until something happens to change, which on a quiet cluster is minutes.
	sender.Flush()
	window := s.cfg.APIStreamThrottle
	openUntil := map[int64]time.Time{}
	pending := map[int64]time.Time{}
	timer := time.NewTimer(window)
	timer.Stop()
	defer timer.Stop()
	var armed time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.stopping:
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if ev.Kind != kind {
				continue
			}
			now := time.Now()
			if until, open := openUntil[ev.ID]; open && now.Before(until) {
				pending[ev.ID] = until
				break
			}
			if err := reload(ev.ID); err != nil {
				return err
			}
			openUntil[ev.ID] = now.Add(window)
		case <-timer.C:
			armed = time.Time{}
			now := time.Now()
			for id, at := range pending {
				if now.Before(at) {
					continue
				}
				delete(pending, id)
				if err := reload(id); err != nil {
					return err
				}
				openUntil[id] = now.Add(window)
			}
		}
		armed = rearm(timer, armed, pending)
		prune(openUntil, pending)
	}
}

// prune drops the ids whose window has run out with nothing waiting behind
// it, so a connection open for a day holds only the rows changing now.
func prune(openUntil, pending map[int64]time.Time) {
	now := time.Now()
	for id, until := range openUntil {
		if _, waiting := pending[id]; !waiting && !now.Before(until) {
			delete(openUntil, id)
		}
	}
}

// rearm points timer at the earliest deadline still pending and reports the
// deadline it is now waiting for, zero when nothing is.
func rearm(timer *time.Timer, armed time.Time, pending map[int64]time.Time) time.Time {
	var next time.Time
	for _, at := range pending {
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	if next.IsZero() {
		timer.Stop()
		return time.Time{}
	}
	if next.Equal(armed) {
		return armed
	}
	timer.Stop()
	timer.Reset(time.Until(next))
	return next
}
