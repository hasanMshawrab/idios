package processor

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/store"
)

// opsResult is what execOps wrote, so history rows and requests can name
// incident ids.
type opsResult struct {
	openIDs []int64
	touched []int64
	history map[int]int64
}

// execOps runs attaches first so a reopen never collides with an open of the
// same key, then opens (adopting earlier events of the subject), then closes.
func (p *Processor) execOps(ctx context.Context, tx *sql.Tx, ops incident.Ops, involvedUID string) (opsResult, error) {
	res := opsResult{history: map[int]int64{}}
	for _, a := range ops.Attach {
		if err := store.AttachIncident(ctx, tx, a.IncidentID, a.Reopen, a.LastReason, a.LastMessage, a.LastSeenAt); err != nil {
			return res, err
		}
		res.touched = append(res.touched, a.IncidentID)
		for _, i := range a.HistoryIndexes {
			res.history[i] = a.IncidentID
		}
	}
	for _, o := range ops.Open {
		id, err := store.OpenIncident(ctx, tx, o.Incident)
		if err != nil {
			return res, err
		}
		res.openIDs = append(res.openIDs, id)
		res.touched = append(res.touched, id)
		for _, i := range o.HistoryIndexes {
			res.history[i] = id
		}
		openedAt, err := clock.Parse(o.Incident.OpenedAt)
		if err != nil {
			return res, fmt.Errorf("incident %d opened_at %q: %w", id, o.Incident.OpenedAt, err)
		}
		since := clock.Format(openedAt.Add(-p.pol.StabilizationWindow))
		if _, err := store.AttachEvents(ctx, tx, id, involvedUID, o.Incident.ContainerName, o.Incident.Category, since); err != nil {
			return res, err
		}
	}
	for _, c := range ops.Close {
		if err := store.CloseIncident(ctx, tx, c.IncidentID, c.Reason, c.ClosedAt); err != nil {
			return res, err
		}
		res.touched = append(res.touched, c.IncidentID)
	}
	return res, nil
}

// openIncidentIDs names the rows a statement that updates every open
// incident of one subject reaches. An empty category means any.
func openIncidentIDs(incidents []store.Incident, category string) []int64 {
	var out []int64
	for _, inc := range incidents {
		if inc.ClosedAt == nil && (category == "" || inc.Category == category) {
			out = append(out, inc.ID)
		}
	}
	return out
}

// touchedByContainer returns, per container name, the incident this event
// opened or attached to, and the container name of every incident id known.
func touchedByContainer(incidents []store.Incident, ops incident.Ops, res opsResult) (byContainer map[string]*int64, names map[int64]string) {
	names = map[int64]string{}
	for _, inc := range incidents {
		names[inc.ID] = inc.ContainerName
	}
	byContainer = map[string]*int64{}
	for i, o := range ops.Open {
		id := res.openIDs[i]
		names[id] = o.Incident.ContainerName
		byContainer[o.Incident.ContainerName] = &id
	}
	for _, a := range ops.Attach {
		id := a.IncidentID
		byContainer[names[id]] = &id
	}
	return byContainer, names
}
