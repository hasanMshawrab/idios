package k8s

import (
	"context"
	"fmt"
	"runtime/debug"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/hasanMshawrab/idios/internal/ingest"
)

// Handler receives every object the informers deliver, plus the connection
// and reconcile facts only the watcher knows. The processor implements it.
type Handler interface {
	ClusterConnected(ctx context.Context, clusterID int64, identity *string, apiServerURL string) error
	ClusterError(ctx context.Context, clusterID int64, msg string) error
	Pod(ctx context.Context, clusterID int64, pod *corev1.Pod, r ingest.OwnerResolver) error
	PodDeleted(ctx context.Context, uid, source string) error
	Job(ctx context.Context, clusterID int64, job *batchv1.Job) error
	JobDeleted(ctx context.Context, uid string) error
	ReplicaSet(ctx context.Context, clusterID int64, rs *appsv1.ReplicaSet) error
	ReplicaSetDeleted(ctx context.Context, uid string) error
	Event(ctx context.Context, clusterID int64, ev *corev1.Event) error
	Reconcile(ctx context.Context, clusterID int64, watched []string, live func(namespace, uid string) bool) error
}

// deletionSourceWatch is the pods.deletion_source value for a delete the
// informer itself delivered; the processor stores it verbatim.
const deletionSourceWatch = "watch"

// deleted returns the typed object of a notification, looking inside
// DeletedFinalStateUnknown, which the informer sends when the delete itself
// was missed during a watch gap.
func deleted[T any](obj any) (T, bool) {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = d.Obj
	}
	v, ok := obj.(T)
	return v, ok
}

// handlerFuncs adapts upsert and del to informer callbacks. Add and update
// are the same call: the store snapshot, not the informer, decides what
// changed.
func handlerFuncs[T metav1.Object](w *Watcher, kind, ns string, upsert, del func(T) error) cache.ResourceEventHandlerFuncs {
	on := func(obj any, fn func(T) error) {
		o, ok := deleted[T](obj)
		if !ok {
			w.log.Error("unexpected object", "cluster", w.cfg.ClusterID, "namespace", ns, "kind", kind, "type", fmt.Sprintf("%T", obj))
			return
		}
		w.counters.ObjectSeen(w.cfg.ClusterID)
		w.guard(kind, ns, o, func() error { return fn(o) })
	}
	f := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { on(obj, upsert) },
		UpdateFunc: func(_, obj any) { on(obj, upsert) },
	}
	if del != nil {
		f.DeleteFunc = func(obj any) { on(obj, del) }
	}
	return f
}

// guard runs one handler call so that neither an error nor a panic reaches
// client-go: an error there would be dropped silently, and a panic would
// end the informer's goroutine and with it every other object it serves.
func (w *Watcher) guard(kind, ns string, o metav1.Object, fn func() error) {
	defer func() {
		if r := recover(); r != nil {
			w.counters.HandlerPanic()
			w.log.Error("handler panicked", "cluster", w.cfg.ClusterID, "namespace", ns, "kind", kind, "uid", string(o.GetUID()), "name", o.GetName(), "panic", r, "stack", string(debug.Stack()))
		}
	}()
	if err := fn(); err != nil {
		w.counters.HandlerError()
		w.log.Error("handler failed", "cluster", w.cfg.ClusterID, "namespace", ns, "kind", kind, "uid", string(o.GetUID()), "name", o.GetName(), "err", err)
	}
}
