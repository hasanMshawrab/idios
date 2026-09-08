package main

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/hasanMshawrab/idios/internal/capture"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/k8s"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/processor"
	"github.com/hasanMshawrab/idios/internal/status"
	"github.com/hasanMshawrab/idios/internal/store"
)

// reloadInterval is how often the clusters and watched_namespaces tables
// are read again. The CLI writes them from another process, so polling is
// the only signal.
const reloadInterval = 10 * time.Second

// clusterSpec is what a watcher was built from; a row change that leaves it
// equal needs no restart.
type clusterSpec struct {
	contextName string
	namespaces  []string
}

func (a clusterSpec) equal(b clusterSpec) bool {
	return a.contextName == b.contextName && slices.Equal(a.namespaces, b.namespaces)
}

func specsFromRows(clusters []store.Cluster, namespaces map[int64][]string) map[int64]clusterSpec {
	out := map[int64]clusterSpec{}
	for _, c := range clusters {
		ns := slices.Clone(namespaces[c.ID])
		slices.Sort(ns)
		out[c.ID] = clusterSpec{contextName: c.ContextName, namespaces: ns}
	}
	return out
}

// reloadPlan says which watchers to stop and which to start so that running
// matches want. A changed cluster is in both lists: stopped, then started
// again from its new rows.
func reloadPlan(running, want map[int64]clusterSpec) (start, stop []int64) {
	for id, r := range running {
		if w, ok := want[id]; !ok || !w.equal(r) {
			stop = append(stop, id)
		}
	}
	for id, w := range want {
		if r, ok := running[id]; !ok || !r.equal(w) {
			start = append(start, id)
		}
	}
	slices.Sort(start)
	slices.Sort(stop)
	return start, stop
}

type runningCluster struct {
	id     int64
	spec   clusterSpec
	w      *k8s.Watcher
	skew   *k8s.Skew
	cancel context.CancelFunc
	done   chan struct{}
}

// supervisor keeps one watcher per cluster row and follows the tables.
type supervisor struct {
	cfg      config.Config
	st       *store.Store
	proc     *processor.Processor
	pool     *capture.Pool
	counters *status.Counters
	events   *notify.Notifier
	log      *slog.Logger

	mu      sync.Mutex
	running map[int64]*runningCluster
}

func newSupervisor(cfg config.Config, st *store.Store, proc *processor.Processor, pool *capture.Pool, counters *status.Counters, events *notify.Notifier, log *slog.Logger) *supervisor {
	return &supervisor{cfg: cfg, st: st, proc: proc, pool: pool, counters: counters, events: events, log: log, running: map[int64]*runningCluster{}}
}

func (s *supervisor) loadSpecs(ctx context.Context) (map[int64]clusterSpec, error) {
	var clusters []store.Cluster
	namespaces := map[int64][]string{}
	err := s.st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if clusters, err = store.ListClusters(ctx, tx); err != nil {
			return err
		}
		for _, c := range clusters {
			if namespaces[c.ID], err = store.ListWatchedNamespaces(ctx, tx, c.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return specsFromRows(clusters, namespaces), nil
}

// reload reads the tables and restarts exactly the watchers whose rows
// changed. Stops happen before starts so a changed cluster's old workers
// are gone before its new queue is registered.
func (s *supervisor) reload(ctx context.Context) error {
	want, err := s.loadSpecs(ctx)
	if err != nil {
		return fmt.Errorf("load clusters: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := map[int64]clusterSpec{}
	for id, rc := range s.running {
		current[id] = rc.spec
	}
	start, stop := reloadPlan(current, want)
	for _, id := range stop {
		s.stopLocked(id)
	}
	for _, id := range start {
		s.running[id] = s.start(ctx, id, want[id])
	}
	// A restarted cluster is watching a different namespace set, and a
	// stopped one is no longer watched at all: both change the row a client
	// is showing. A restart is in both lists and is one change.
	notified := map[int64]bool{}
	notifyOnce := func(id int64) {
		if notified[id] {
			return
		}
		notified[id] = true
		s.events.Notify(notify.Cluster, id)
	}
	for _, id := range stop {
		notifyOnce(id)
	}
	for _, id := range start {
		notifyOnce(id)
	}
	return nil
}

func (s *supervisor) stopLocked(id int64) {
	rc := s.running[id]
	rc.cancel()
	<-rc.done
	s.pool.RemoveCluster(id)
	delete(s.running, id)
	s.log.Info("watcher stopped", "cluster", id)
}

func (s *supervisor) start(ctx context.Context, id int64, spec clusterSpec) *runningCluster {
	skew := k8s.NewSkew(clock.Real{}, s.log.With("cluster", id))
	w := k8s.New(k8s.Config{ClusterID: id, ContextName: spec.contextName, Namespaces: spec.namespaces,
		ReadyChanged: func(bool) { s.events.Notify(notify.Cluster, id) }},
		k8s.KubeconfigClient(s.cfg.Kubeconfig, spec.contextName, skew), s.proc, s.log, s.counters)
	s.pool.AddCluster(id, capture.ClientLogs(w.Client))
	cctx, cancel := context.WithCancel(ctx)
	rc := &runningCluster{id: id, spec: spec, w: w, skew: skew, cancel: cancel, done: make(chan struct{})}
	s.log.Info("watching", "cluster", id, "context", spec.contextName, "namespaces", spec.namespaces)
	go func() {
		defer close(rc.done)
		if err := w.Run(cctx); err != nil && !errors.Is(err, context.Canceled) {
			s.log.Error("watcher stopped", "cluster", id, "err", err)
		}
	}()
	return rc
}

// run loads the tables now and every reloadInterval until ctx ends, then
// stops every watcher. The first load must succeed; later failures are
// logged and the running set is kept.
func (s *supervisor) run(ctx context.Context) error {
	if err := s.reload(ctx); err != nil {
		return err
	}
	if len(s.list()) == 0 {
		s.log.Warn("no clusters configured; nothing to watch")
	}
	t := time.NewTicker(reloadInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for id := range s.running {
				s.stopLocked(id)
			}
			s.mu.Unlock()
			return ctx.Err()
		case <-t.C:
			if err := s.reload(ctx); err != nil {
				s.log.Error("reload clusters", "err", err)
			}
		}
	}
}

// list returns the running clusters in id order.
func (s *supervisor) list() []*runningCluster {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*runningCluster, 0, len(s.running))
	for _, rc := range s.running {
		out = append(out, rc)
	}
	slices.SortFunc(out, func(a, b *runningCluster) int { return cmp.Compare(a.id, b.id) })
	return out
}
