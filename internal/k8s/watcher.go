package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/hasanMshawrab/idios/internal/status"
)

// Config names the cluster row a Watcher serves and the namespaces it
// watches there. ReadyChanged, when set, is called on each transition of
// Ready, from the goroutine that made it.
type Config struct {
	ClusterID    int64
	ContextName  string
	Namespaces   []string
	ReadyChanged func(ready bool)
}

// Watcher runs the informers of one cluster and feeds a Handler until its
// context ends.
type Watcher struct {
	cfg      Config
	client   ClientFunc
	h        Handler
	log      *slog.Logger
	counters *status.Counters

	syncTimeout time.Duration
	wait        func(ctx context.Context, d time.Duration) error
	ready       atomic.Bool

	clientMu sync.Mutex
	current  kubernetes.Interface
}

// New returns a Watcher for cfg that counts handler outcomes in counters.
// Run starts it.
func New(cfg Config, client ClientFunc, h Handler, log *slog.Logger, counters *status.Counters) *Watcher {
	return &Watcher{cfg: cfg, client: client, h: h, log: log, counters: counters, syncTimeout: 30 * time.Second, wait: sleep}
}

// Ready reports whether every informer is synced and the reconcile pass ran.
func (w *Watcher) Ready() bool {
	return w.ready.Load()
}

// setReady records the state and reports whether it changed, so that the
// reconnect loop can reset its backoff and ReadyChanged sees one call per
// transition rather than one per attempt.
func (w *Watcher) setReady(ready bool) bool {
	if w.ready.Swap(ready) == ready {
		return false
	}
	if w.cfg.ReadyChanged != nil {
		w.cfg.ReadyChanged(ready)
	}
	return true
}

// Client returns the clientset of the current connection, nil before the
// first one succeeds. The capture pool reads logs through it so that a
// reconnect with an edited kubeconfig is picked up there too.
func (w *Watcher) Client() kubernetes.Interface {
	w.clientMu.Lock()
	defer w.clientMu.Unlock()
	return w.current
}

const maxBackoff = time.Minute

// Run connects, watches and reconciles until ctx ends. A failed attempt is
// written to the cluster row and retried after 1s, doubling to a minute;
// an attempt that reached ready resets the delay.
func (w *Watcher) Run(ctx context.Context) error {
	delay := time.Second
	for {
		err := w.runOnce(ctx)
		if w.setReady(false) {
			delay = time.Second
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.log.Error("watcher failed", "cluster", w.cfg.ClusterID, "context", w.cfg.ContextName, "err", err)
		if rerr := w.h.ClusterError(ctx, w.cfg.ClusterID, err.Error()); rerr != nil {
			w.log.Error("record cluster error", "cluster", w.cfg.ClusterID, "err", rerr)
		}
		if err := w.wait(ctx, delay); err != nil {
			return err
		}
		delay = min(delay*2, maxBackoff)
	}
}

func (w *Watcher) runOnce(ctx context.Context) error {
	client, host, err := w.client()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	identity, err := clusterIdentity(ctx, client)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	w.clientMu.Lock()
	w.current = client
	w.clientMu.Unlock()
	if err := w.h.ClusterConnected(ctx, w.cfg.ClusterID, identity, host); err != nil {
		return fmt.Errorf("record connection: %w", err)
	}

	stop := make(chan struct{})
	// The probe lives exactly as long as the informers it answers for.
	probeCtx, stopProbe := context.WithCancel(ctx)
	var probeMu sync.Mutex
	var probing, gone bool
	var probes sync.WaitGroup
	var factories []informers.SharedInformerFactory
	defer func() {
		stopProbe()
		probeMu.Lock()
		gone = true
		probeMu.Unlock()
		probes.Wait()
		close(stop)
		for _, f := range factories {
			f.Shutdown()
		}
	}()
	id := w.cfg.ClusterID
	failed := newFailedSet()
	startProbe := func() {
		probeMu.Lock()
		defer probeMu.Unlock()
		if probing || gone {
			return
		}
		probing = true
		probes.Add(1)
		go func() {
			defer func() {
				probeMu.Lock()
				probing = false
				probeMu.Unlock()
				probes.Done()
			}()
			w.probe(probeCtx, client, host, failed)
		}()
	}
	resolver := newListerResolver()
	var owners, workloads []namedInformer
	for _, ns := range w.cfg.Namespaces {
		f := informers.NewSharedInformerFactoryWithOptions(client, 0, informers.WithNamespace(ns))
		factories = append(factories, f)
		rs, jobs := f.Apps().V1().ReplicaSets(), f.Batch().V1().Jobs()
		resolver.add(ns, rs.Lister().ReplicaSets(ns), jobs.Lister().Jobs(ns))
		stage := []namedInformer{
			{namespace: ns, resource: "replicasets", inf: rs.Informer(), h: handlerFuncs(w, "ReplicaSet", ns,
				func(o *appsv1.ReplicaSet) error { return w.h.ReplicaSet(ctx, id, o) },
				func(o *appsv1.ReplicaSet) error { return w.h.ReplicaSetDeleted(ctx, string(o.UID)) })},
			{namespace: ns, resource: "jobs", inf: jobs.Informer(), h: handlerFuncs(w, "Job", ns,
				func(o *batchv1.Job) error { return w.h.Job(ctx, id, o) },
				func(o *batchv1.Job) error { return w.h.JobDeleted(ctx, string(o.UID)) })},
		}
		for i := range stage {
			if err := w.register(ctx, &stage[i], failed, startProbe); err != nil {
				return err
			}
		}
		owners = append(owners, stage...)
	}
	// Owner stores fill first; a pod handler that ran before its ReplicaSet
	// was listed would write the fallback workload for every pod on the
	// initial list.
	for _, f := range factories {
		f.Start(stop)
	}
	if err := w.waitSync(ctx, owners, failed); err != nil {
		return fmt.Errorf("sync owners: %w", err)
	}
	for i, ns := range w.cfg.Namespaces {
		f := factories[i]
		pods, events := f.Core().V1().Pods(), f.Core().V1().Events()
		stage := []namedInformer{
			{namespace: ns, resource: "pods", inf: pods.Informer(), h: handlerFuncs(w, "Pod", ns,
				func(o *corev1.Pod) error { return w.h.Pod(ctx, id, o, resolver) },
				func(o *corev1.Pod) error { return w.h.PodDeleted(ctx, string(o.UID), deletionSourceWatch) })},
			{namespace: ns, resource: "events", inf: events.Informer(), h: handlerFuncs[*corev1.Event](w, "Event", ns,
				func(o *corev1.Event) error { return w.h.Event(ctx, id, o) }, nil)},
		}
		for j := range stage {
			if err := w.register(ctx, &stage[j], failed, startProbe); err != nil {
				return err
			}
		}
		workloads = append(workloads, stage...)
	}
	for _, f := range factories {
		f.Start(stop)
	}
	if err := w.waitSync(ctx, workloads, failed); err != nil {
		return fmt.Errorf("sync pods: %w", err)
	}

	subjects := append(append([]namedInformer{}, owners...), workloads...)
	var once sync.Once
	live := map[string]bool{}
	unknown := map[string]bool{}
	// The stores are listed on the first lookup so that they are read after
	// Reconcile has loaded its rows: a pod added in between would otherwise
	// be in the rows but not in the list, and be declared gone forever.
	build := func() {
		for _, ni := range subjects {
			if ni.resource != "pods" && ni.resource != "jobs" {
				continue
			}
			// An informer the Role refused holds nothing, so its namespace's
			// rows are left as they are rather than declared gone.
			if failed.forbidden(ni.key()) {
				unknown[ni.namespace] = true
				continue
			}
			for _, obj := range ni.inf.GetStore().List() {
				if m, err := meta.Accessor(obj); err == nil {
					live[string(m.GetUID())] = true
				}
			}
		}
	}
	isLive := func(ns, uid string) bool {
		once.Do(build)
		return unknown[ns] || live[uid]
	}
	if err := w.h.Reconcile(ctx, id, w.cfg.Namespaces, isLive); err != nil {
		// The informers are healthy; a row that cannot be marked is not a
		// reason to reconnect.
		w.log.Error("reconcile", "cluster", id, "err", err)
	}
	w.setReady(true)
	<-ctx.Done()
	return ctx.Err()
}

// probe asks the cluster for its identity until it answers, on the
// reconnect loop's backoff, and then declares the watcher healthy again.
// client-go reports a reflector's failure and never its recovery, so a
// positive signal of our own is the only thing that can clear one.
func (w *Watcher) probe(ctx context.Context, client kubernetes.Interface, host string, failed *failedSet) {
	delay := time.Second
	for {
		if err := w.wait(ctx, delay); err != nil {
			return
		}
		delay = min(delay*2, maxBackoff)
		identity, err := clusterIdentity(ctx, client)
		if err != nil {
			w.log.Warn("cluster probe failed", "cluster", w.cfg.ClusterID, "context", w.cfg.ContextName, "err", err)
			continue
		}
		if err := w.h.ClusterConnected(ctx, w.cfg.ClusterID, identity, host); err != nil {
			w.log.Error("record connection", "cluster", w.cfg.ClusterID, "err", err)
			continue
		}
		// The reflectors need nothing: they relist on their own.
		failed.clearFailures()
		w.setReady(true)
		return
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
