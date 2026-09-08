package k8s

import (
	"context"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

type namedInformer struct {
	namespace string
	resource  string
	inf       cache.SharedIndexInformer
	h         cache.ResourceEventHandler
	synced    cache.InformerSynced
}

func (ni namedInformer) key() string {
	return ni.namespace + "/" + ni.resource
}

// failedSet remembers the informers whose list or watch failed, so that one
// informer failing on a timer writes the cluster row once, and so that the
// sync wait stops expecting the resources the Role refuses.
type failedSet struct {
	mu sync.Mutex
	// The value tells the Role's permanent refusal from a failure that is
	// expected to come back.
	keys map[string]bool
}

func newFailedSet() *failedSet {
	return &failedSet{keys: map[string]bool{}}
}

// add records one informer's failure and reports whether it is the first.
func (f *failedSet) add(key string, forbidden bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.keys[key]; ok {
		return false
	}
	f.keys[key] = forbidden
	return true
}

// forbidden reports whether the Role refused this informer's resource.
func (f *failedSet) forbidden(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys[key]
}

// clearFailures drops every informer that is merely failing and keeps the
// ones the Role refused: those are facts about the Role, not an outage.
func (f *failedSet) clearFailures() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, forbidden := range f.keys {
		if !forbidden {
			delete(f.keys, key)
		}
	}
}

// register attaches the handler and a list/watch error handler; both must
// precede the informer's start. startProbe is called on the first failure
// that is expected to recover, and asks the cluster whether it has.
func (w *Watcher) register(ctx context.Context, ni *namedInformer, failed *failedSet, startProbe func()) error {
	reg, err := ni.inf.AddEventHandler(ni.h)
	if err != nil {
		return err
	}
	ni.synced = reg.HasSynced
	return ni.inf.SetWatchErrorHandlerWithContext(func(_ context.Context, _ *cache.Reflector, err error) {
		forbidden := apierrors.IsForbidden(err)
		// A connection that never completes is already the sync wait's
		// failure; only a watcher that claimed to be watching contradicts
		// itself by failing.
		if !forbidden && !w.Ready() {
			w.log.Warn("list/watch failed", "cluster", w.cfg.ClusterID, "namespace", ni.namespace, "resource", ni.resource, "err", err)
			return
		}
		if !failed.add(ni.key(), forbidden) {
			return
		}
		msg := fmt.Sprintf("forbidden: list %s in namespace %s", ni.resource, ni.namespace)
		if !forbidden {
			msg = fmt.Sprintf("list %s in namespace %s: %v", ni.resource, ni.namespace, err)
			w.setReady(false)
		}
		if err := w.h.ClusterError(ctx, w.cfg.ClusterID, msg); err != nil {
			w.log.Error("record cluster error", "cluster", w.cfg.ClusterID, "err", err)
		}
		if !forbidden {
			startProbe()
		}
	})
}

// waitSync returns once every informer has synced or has been refused by
// the Role; a namespace missing one resource must not stall the rest. An
// informer that is merely failing is waited for, because it is expected to
// come back. The registration's HasSynced is used rather than the
// informer's: the latter flips when the initial list leaves the queue,
// before the handler has been given those objects.
func (w *Watcher) waitSync(ctx context.Context, infs []namedInformer, failed *failedSet) error {
	deadline := time.NewTimer(w.syncTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		pending := 0
		for _, ni := range infs {
			if !ni.synced() && !failed.forbidden(ni.key()) {
				pending++
			}
		}
		if pending == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("cache sync timed out after %s", w.syncTimeout)
		case <-tick.C:
		}
	}
}
