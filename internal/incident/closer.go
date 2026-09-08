package incident

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/store"
)

// Closer periodically closes stabilized pod incidents and attaches events
// that arrived after their incident closed. The only state it keeps is the
// last tick's time and result and the totals since the process started, for
// reporting; every decision comes from the database, so a restart mid-window
// changes nothing.
type Closer struct {
	w          *store.Writer
	clk        clock.Clock
	window     time.Duration
	stuckAfter time.Duration
	interval   time.Duration
	log        *slog.Logger

	events Notifier

	mu     sync.Mutex
	lastAt time.Time
	last   TickResult
	total  TickResult
}

// Notifier is told the id of every incident a committed tick closed.
type Notifier interface {
	Notify(notify.Kind, int64)
}

// SetNotifier makes the closer report the incidents it closes. Without one
// it closes silently. Call it before the closer runs: the field is not
// synchronized.
func (c *Closer) SetNotifier(n Notifier) { c.events = n }

// TickResult counts what one tick changed.
type TickResult struct {
	Closed   int64
	Attached int64
	Opened   int64
}

// NewCloser returns a Closer that ticks every interval, treats an incident
// as stable once last_seen_at is older than window, and opens a stuck
// incident for a pod that has been waiting for longer than stuckAfter.
func NewCloser(w *store.Writer, clk clock.Clock, window, stuckAfter, interval time.Duration, log *slog.Logger) *Closer {
	return &Closer{w: w, clk: clk, window: window, stuckAfter: stuckAfter, interval: interval, log: log}
}

// Tick runs one close-then-attach-then-open pass in a single transaction.
// Closing first lets an event attach to the incident closed in the same
// tick, and leaves the pod whose incident just closed free to be found
// stuck. The tick is the only thing that can find a stuck pod: the
// informers run with resync 0, so a pod that is doing nothing delivers no
// event, which is the condition itself. Deleted pods are closed before the
// stable pass: an init container that exited 0 looks stable after its pod
// is gone, and that close must say pod_deleted, not recovered.
func (c *Closer) Tick(ctx context.Context) (TickResult, error) {
	now := c.clk.Now()
	nowS := clock.Format(now)
	edge := clock.Format(now.Add(-c.window))
	stuckEdge := clock.Format(now.Add(-c.stuckAfter))
	var r TickResult
	var touched []int64
	err := c.w.Tx(ctx, func(tx *sql.Tx) error {
		closed, err := store.CloseDeletedPodIncidents(ctx, tx)
		if err != nil {
			return err
		}
		stable, err := store.CloseStableIncidents(ctx, tx, nowS, edge)
		if err != nil {
			return err
		}
		closed = append(closed, stable...)
		touched = closed
		r.Closed = int64(len(closed))
		if r.Attached, err = store.AttachLateEvents(ctx, tx, edge); err != nil {
			return err
		}
		candidates, err := store.ListStuckCandidates(ctx, tx, stuckEdge)
		if err != nil {
			return err
		}
		for _, op := range StuckOps(candidates, now).Open {
			id, err := store.OpenIncident(ctx, tx, op.Incident)
			if err != nil {
				return err
			}
			touched = append(touched, id)
			r.Opened++
		}
		return nil
	})
	if err != nil {
		return TickResult{}, err
	}
	c.mu.Lock()
	c.lastAt, c.last = now, r
	c.total.Closed += r.Closed
	c.total.Attached += r.Attached
	c.total.Opened += r.Opened
	c.mu.Unlock()
	// After the commit: a reader reloads the row on receipt and would find
	// the incident still open if told any earlier.
	if c.events != nil {
		for _, id := range touched {
			c.events.Notify(notify.Incident, id)
		}
	}
	return r, nil
}

// Last reports when the most recent successful tick ran and what it
// changed; a zero time means none has run yet.
func (c *Closer) Last() (time.Time, TickResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastAt, c.last
}

// Totals reports what every successful tick has changed since the process
// started.
func (c *Closer) Totals() TickResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

// Run ticks until ctx is done and returns ctx.Err(). A failed tick is
// logged and the next one runs; nothing is retried early.
func (c *Closer) Run(ctx context.Context) error {
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			r, err := c.Tick(ctx)
			if err != nil {
				c.log.Error("closer tick", "err", err)
				continue
			}
			if r.Closed > 0 || r.Attached > 0 || r.Opened > 0 {
				c.log.Info("closer tick", "closed", r.Closed, "attached", r.Attached, "opened", r.Opened)
			}
		}
	}
}
