// Package status keeps the counters the running process can answer for and
// the snapshot it publishes for idios status, which runs as another process.
package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
)

const (
	// Interval is how often the daemon writes its snapshot.
	Interval = 10 * time.Second
	// Stale is the snapshot age past which the daemon is presumed gone.
	Stale = 3 * Interval
	// FileName is the snapshot's name under the data directory.
	FileName = "status.json"

	ringSize = 1024
)

// Counters is what the process counts about itself. Every method is safe
// for concurrent use.
type Counters struct {
	clk clock.Clock

	mu            sync.Mutex
	txCount       uint64
	txErrors      uint64
	durations     []time.Duration
	next          int
	handlerErrors uint64
	handlerPanics uint64
	lastSeen      map[int64]time.Time
}

// New returns empty counters that stamp events with clk.
func New(clk clock.Clock) *Counters {
	return &Counters{clk: clk, lastSeen: map[int64]time.Time{}}
}

// ObserveTx records one transaction. It satisfies store.TxObserver.
func (c *Counters) ObserveTx(d time.Duration, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.txCount++
	if err != nil {
		c.txErrors++
	}
	// A ring of recent durations: p99 over the whole run would hide a slow
	// hour behind a fast day.
	if len(c.durations) < ringSize {
		c.durations = append(c.durations, d)
		return
	}
	c.durations[c.next] = d
	c.next = (c.next + 1) % ringSize
}

// HandlerError counts an informer handler that returned an error.
func (c *Counters) HandlerError() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlerErrors++
}

// HandlerPanic counts a panic recovered at the handler boundary.
func (c *Counters) HandlerPanic() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlerPanics++
}

// ObjectSeen stamps the cluster with the current time.
func (c *Counters) ObjectSeen(clusterID int64) {
	now := c.clk.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSeen[clusterID] = now
}

// LastSeen returns when the cluster last delivered an object.
func (c *Counters) LastSeen(clusterID int64) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.lastSeen[clusterID]
	return t, ok
}

// Writer is the transaction counters at one instant.
type Writer struct {
	Transactions uint64  `json:"transactions"`
	Errors       uint64  `json:"errors"`
	P99Ms        float64 `json:"p99_ms"`
}

// Writer reports the transaction counters and the p99 of recent durations.
func (c *Counters) Writer() Writer {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := Writer{Transactions: c.txCount, Errors: c.txErrors}
	if n := len(c.durations); n > 0 {
		sorted := make([]time.Duration, n)
		copy(sorted, c.durations)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		idx := (n*99 + 99) / 100
		if idx > 0 {
			idx--
		}
		w.P99Ms = float64(sorted[idx]) / float64(time.Millisecond)
	}
	return w
}

// Handlers is the handler boundary counters at one instant.
type Handlers struct {
	Errors uint64 `json:"errors"`
	Panics uint64 `json:"panics"`
}

// Handlers reports the handler boundary counters.
func (c *Counters) Handlers() Handlers {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Handlers{Errors: c.handlerErrors, Panics: c.handlerPanics}
}

// Cluster is one watcher's runtime state.
type Cluster struct {
	ID          int64   `json:"id"`
	Ready       bool    `json:"ready"`
	LastEventAt string  `json:"last_event_at,omitempty"`
	SkewSeconds float64 `json:"skew_seconds"`
}

// Capture is the pool's counters at one instant.
type Capture struct {
	Queued    uint64            `json:"queued"`
	Completed uint64            `json:"completed"`
	Dropped   uint64            `json:"dropped"`
	Gaps      map[string]uint64 `json:"gaps,omitempty"`
}

// Closer is the last successful closer tick and the totals behind it.
type Closer struct {
	LastTickAt    string `json:"last_tick_at,omitempty"`
	Closed        int64  `json:"closed"`
	Attached      int64  `json:"attached"`
	Opened        int64  `json:"opened"`
	ClosedTotal   int64  `json:"closed_total"`
	AttachedTotal int64  `json:"attached_total"`
	OpenedTotal   int64  `json:"opened_total"`
}

// Snapshot is what the daemon publishes; everything the database already
// holds is left out of it.
type Snapshot struct {
	WrittenAt string    `json:"written_at"`
	PID       int       `json:"pid"`
	Version   string    `json:"version"`
	Clusters  []Cluster `json:"clusters,omitempty"`
	Writer    Writer    `json:"writer"`
	Handlers  Handlers  `json:"handlers"`
	Capture   Capture   `json:"capture"`
	Closer    Closer    `json:"closer"`
}

// WriteFile publishes s at path through a temp file in the same directory,
// so a reader sees the old snapshot or the new one, never a partial one.
func WriteFile(path string, s Snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".status-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// ReadFile loads the snapshot at path.
func ReadFile(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}
