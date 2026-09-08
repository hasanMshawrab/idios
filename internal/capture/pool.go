package capture

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/processor"
	"github.com/hasanMshawrab/idios/internal/store"
)

// Config sizes the pool and the captures it makes. Root is the artifacts
// root every file path is relative to.
type Config struct {
	Root                string
	TailLines           int
	MaxBytes            int64
	Workers             int
	QueueSize           int
	EarlyDebounce       time.Duration
	StabilizationWindow time.Duration
}

type clusterQueue struct {
	src    LogSource
	ch     chan processor.CaptureRequest
	cancel context.CancelFunc
}

// Pool captures logs and manifests for every registered cluster and writes
// the artifacts rows. It implements processor.CaptureSink.
type Pool struct {
	cfg   Config
	w     TxRunner
	clk   clock.Clock
	log   *slog.Logger
	cache *earlyCache

	mu       sync.Mutex
	clusters map[int64]*clusterQueue
	runCtx   context.Context
	wg       sync.WaitGroup

	queued, completed, dropped atomic.Uint64
	gapsMu                     sync.Mutex
	gaps                       map[string]uint64
}

// Stats counts requests and rows through the pool. Queued and Dropped count
// requests; Completed and Gaps count artifact rows written by writeRow, so
// an early copy (which only fills the cache) or a request abandoned at
// shutdown never appears in them. Gaps buckets Completed by capture_gap,
// under "file" for rows that captured one.
type Stats struct {
	Queued, Completed, Dropped uint64
	Gaps                       map[string]uint64
}

// New returns a Pool with no clusters; AddCluster registers them and Run
// starts their workers.
func New(cfg Config, w TxRunner, clk clock.Clock, log *slog.Logger) *Pool {
	return &Pool{cfg: cfg, w: w, clk: clk, log: log, cache: newEarlyCache(cfg.EarlyDebounce), clusters: map[int64]*clusterQueue{}, gaps: map[string]uint64{}}
}

func ptr[T any](v T) *T { return &v }

var _ processor.CaptureSink = (*Pool)(nil)

// AddCluster registers the log source for one cluster row. While Run is
// active the cluster's workers start at once; before it, Run starts them.
// Registering an id again replaces its queue and stops the old workers.
func (p *Pool) AddCluster(clusterID int64, src LogSource) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if old := p.clusters[clusterID]; old != nil && old.cancel != nil {
		old.cancel()
	}
	q := &clusterQueue{src: src, ch: make(chan processor.CaptureRequest, p.cfg.QueueSize)}
	p.clusters[clusterID] = q
	if p.runCtx != nil {
		p.startWorkers(q)
	}
}

// RemoveCluster stops the cluster's workers. Requests still queued are
// discarded without a row and without being counted; only a later Enqueue
// for the id counts as dropped.
func (p *Pool) RemoveCluster(clusterID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if q := p.clusters[clusterID]; q != nil && q.cancel != nil {
		q.cancel()
	}
	delete(p.clusters, clusterID)
}

// startWorkers is called with p.mu held and p.runCtx set.
func (p *Pool) startWorkers(q *clusterQueue) {
	ctx, cancel := context.WithCancel(p.runCtx)
	q.cancel = cancel
	for i := 0; i < p.cfg.Workers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for {
				// select picks a ready case at random, so a worker can take
				// a queued request after cancel; the calls it needs would
				// fail, so the queue is checked second.
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case r := <-q.ch:
					p.process(ctx, q.src, r)
				}
			}
		}()
	}
}

// Enqueue hands a request to its cluster's queue without waiting. When the
// queue is full the API server is slow or down; blocking here would stall
// the informer and make the pod table stale as well, so the request is
// dropped and counted.
func (p *Pool) Enqueue(r processor.CaptureRequest) {
	p.mu.Lock()
	q := p.clusters[r.ClusterID]
	p.mu.Unlock()
	if q == nil {
		p.dropped.Add(1)
		p.log.Warn("capture request for unknown cluster", "cluster", r.ClusterID, "pod", r.PodName, "kind", r.Kind)
		return
	}
	select {
	case q.ch <- r:
		p.queued.Add(1)
	default:
		p.dropped.Add(1)
		p.log.Warn("capture queue full", "cluster", r.ClusterID, "pod", r.PodName, "container", r.Container, "kind", r.Kind, "trigger", r.Trigger)
	}
}

// Stats reports the counters at this instant.
func (p *Pool) Stats() Stats {
	p.gapsMu.Lock()
	gaps := make(map[string]uint64, len(p.gaps))
	for k, v := range p.gaps {
		gaps[k] = v
	}
	p.gapsMu.Unlock()
	return Stats{Queued: p.queued.Load(), Completed: p.completed.Load(), Dropped: p.dropped.Load(), Gaps: gaps}
}

func (p *Pool) countCompleted(a store.Artifact) {
	key := "file"
	if a.CaptureGap != nil {
		key = *a.CaptureGap
	}
	p.completed.Add(1)
	p.gapsMu.Lock()
	p.gaps[key]++
	p.gapsMu.Unlock()
}

// Run starts the workers of every registered cluster, starts those of any
// cluster added later, and blocks until ctx ends. Requests still queued at
// that point are discarded without a row and without being counted: a
// canceled context could not make the calls they need.
func (p *Pool) Run(ctx context.Context) error {
	p.mu.Lock()
	p.runCtx = ctx
	for _, q := range p.clusters {
		p.startWorkers(q)
	}
	p.mu.Unlock()
	<-ctx.Done()
	// Clearing runCtx under the lock orders every AddCluster before the
	// wait, so no worker joins the group while it is being waited on.
	p.mu.Lock()
	p.runCtx = nil
	p.mu.Unlock()
	p.wg.Wait()
	return ctx.Err()
}
