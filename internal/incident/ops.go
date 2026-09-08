package incident

import "github.com/hasanMshawrab/idios/internal/store"

// Ops is what the processor executes after Apply. HistoryCategories is
// parallel to the History slice it was computed from.
type Ops struct {
	Open              []Open
	Attach            []Attach
	Close             []Close
	HistoryCategories []*string
}

// Open is a new incident row; HistoryIndexes name the history rows that
// take its id once inserted.
type Open struct {
	Incident       store.Incident
	HistoryIndexes []int
}

// Attach bumps an existing incident. Reopen also clears closed_at,
// close_reason and dismissed_at; acknowledged_at is kept.
type Attach struct {
	IncidentID     int64
	Reopen         bool
	LastReason     string
	LastMessage    *string
	LastSeenAt     string
	HistoryIndexes []int
}

// Close ends an incident with the given close_reason.
type Close struct {
	IncidentID int64
	Reason     string
	ClosedAt   string
}
