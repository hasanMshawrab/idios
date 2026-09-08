package k8s

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/status"
)

func quietWatcher() *Watcher {
	return &Watcher{log: slog.New(slog.NewTextHandler(io.Discard, nil)), counters: status.New(clock.NewFake(time.Time{}))}
}

func TestDeleteUnwrapsFinalStateUnknown(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "idios-smoke", UID: "pod-1"}}
	cases := []struct {
		name string
		obj  any
		want []string
	}{
		{"plain object", pod, []string{"pod-1"}},
		{"final state unknown", cache.DeletedFinalStateUnknown{Key: "idios-smoke/web-1", Obj: pod}, []string{"pod-1"}},
		{"final state unknown without object", cache.DeletedFinalStateUnknown{Key: "idios-smoke/web-1"}, nil},
		{"wrong type", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{UID: "job-1"}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			h := handlerFuncs(quietWatcher(), "Pod", "idios-smoke",
				func(*corev1.Pod) error { t.Fatal("upsert called on delete"); return nil },
				func(p *corev1.Pod) error { got = append(got, string(p.UID)); return errors.New("logged, not returned") })
			h.OnDelete(c.obj)
			if d := cmp.Diff(c.want, got); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestHandlerBoundaryCountsErrorsAndRecoversPanics(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "idios-smoke", UID: "pod-1"}}
	cases := []struct {
		name string
		fn   func(*corev1.Pod) error
		want status.Handlers
	}{
		{"success counts nothing", func(*corev1.Pod) error { return nil }, status.Handlers{}},
		{"error is counted", func(*corev1.Pod) error { return errors.New("tx failed") }, status.Handlers{Errors: 1}},
		{"panic is recovered and counted", func(*corev1.Pod) error { panic("nil map") }, status.Handlers{Panics: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := quietWatcher()
			calls := 0
			h := handlerFuncs(w, "Pod", "idios-smoke", func(p *corev1.Pod) error { calls++; return c.fn(p) }, nil)
			h.OnAdd(pod, false)
			h.OnAdd(pod, false)
			if calls != 2 {
				t.Fatalf("handler ran %d times, want 2: the informer must keep delivering", calls)
			}
			want := status.Handlers{Errors: c.want.Errors * 2, Panics: c.want.Panics * 2}
			if d := cmp.Diff(want, w.counters.Handlers()); d != "" {
				t.Fatal(d)
			}
			if _, ok := w.counters.LastSeen(0); !ok {
				t.Fatal("delivered objects were not stamped on the cluster")
			}
		})
	}
}
