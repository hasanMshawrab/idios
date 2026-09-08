package k8s

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/processor"
	"github.com/hasanMshawrab/idios/internal/status"
	"github.com/hasanMshawrab/idios/internal/store"
)

const (
	ns     = "idios-smoke"
	web    = "registry.example.com/web:1.4.2"
	webID  = "registry.example.com/web@sha256:1111"
	webTag = "1.4.2"
)

type dropSink struct{}

func (dropSink) Enqueue(processor.CaptureRequest) {}

type run struct {
	t      *testing.T
	s      *store.Store
	client *fake.Clientset
	w      *Watcher
	cancel context.CancelFunc
	done   chan error

	mu     sync.Mutex
	delays []time.Duration
}

func fixture(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "ingest", "testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func kubeSystem() *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "ks-uid"}}
}

// newRun opens a temp store with cluster row 1, builds the real processor
// and a Watcher over a fake clientset. Backoff sleeps are recorded, not
// slept; a lifecycle test that hits one has failed.
func newRun(t *testing.T, client ClientFunc, objs ...runtime.Object) *run {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clk := clock.NewFake(testNow)
	if err := s.Migrate(context.Background(), clk); err != nil {
		t.Fatal(err)
	}
	r := &run{t: t, s: s, done: make(chan error, 1)}
	r.exec(`INSERT INTO clusters (id, name, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'orbstack', '', ?)`, clock.Format(testNow))
	r.client = fake.NewClientset(objs...)
	if client == nil {
		client = func() (kubernetes.Interface, string, error) { return r.client, "https://fake.invalid", nil }
	}
	proc := processor.New(s.Writer, clk, dropSink{}, incident.Policy{SchedulingGrace: time.Minute, StabilizationWindow: 10 * time.Minute})
	r.w = New(Config{ClusterID: 1, ContextName: "orbstack", Namespaces: []string{ns}}, client, proc, slog.New(slog.NewTextHandler(io.Discard, nil)), status.New(clk))
	r.w.syncTimeout = 5 * time.Second
	r.w.wait = func(ctx context.Context, d time.Duration) error {
		r.mu.Lock()
		r.delays = append(r.delays, d)
		r.mu.Unlock()
		return ctx.Err()
	}
	return r
}

func (r *run) start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() { r.done <- r.w.Run(ctx) }()
	r.t.Cleanup(func() {
		cancel()
		<-r.done
	})
}

func (r *run) exec(query string, args ...any) {
	r.t.Helper()
	err := r.s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), query, args...)
		return err
	})
	if err != nil {
		r.t.Fatalf("%v\n%s", err, query)
	}
}

func (r *run) cluster() store.Cluster {
	r.t.Helper()
	var out []store.Cluster
	err := r.s.Writer.Tx(context.Background(), func(tx *sql.Tx) (err error) { out, err = store.ListClusters(context.Background(), tx); return err })
	if err != nil || len(out) != 1 {
		r.t.Fatalf("clusters: %v %+v", err, out)
	}
	return out[0]
}

func (r *run) pod(uid string) *store.Pod {
	r.t.Helper()
	var out *store.Pod
	err := r.s.Writer.Tx(context.Background(), func(tx *sql.Tx) (err error) { out, err = store.LoadPod(context.Background(), tx, uid); return err })
	if err != nil {
		r.t.Fatal(err)
	}
	return out
}

func (r *run) incidents(uid string) []store.Incident {
	r.t.Helper()
	var out []store.Incident
	err := r.s.Writer.Tx(context.Background(), func(tx *sql.Tx) (err error) {
		out, err = store.LoadIncidentsForSubject(context.Background(), tx, uid)
		return err
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return out
}

func (r *run) historyCount() int {
	r.t.Helper()
	var n int
	if err := r.s.Reader.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM container_state_history").Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// eventually polls because informer handlers run on their own goroutines.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func diff(t *testing.T, want, got any) {
	t.Helper()
	if d := cmp.Diff(want, got, cmpopts.EquateEmpty()); d != "" {
		t.Error(d)
	}
}

func TestWatcherRecordsPodLifecycle(t *testing.T) {
	var rs appsv1.ReplicaSet
	var before, after corev1.Pod
	fixture(t, "replicaset/deploy.json", &rs)
	fixture(t, "crash-loop/before.json", &before)
	fixture(t, "crash-loop/after.json", &after)
	r := newRun(t, nil, kubeSystem(), &rs, &before)
	stale := "2026-08-27T11:00:00.000000Z"
	r.exec(`INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at) VALUES ('pod-stale', 1, ?, 'gone', 'Running', ?, ?, ?)`, ns, stale, stale, stale)
	now := clock.Format(testNow)
	r.start()
	eventually(t, "ready", r.w.Ready)

	diff(t, store.Cluster{ID: 1, Identity: ptr("ks-uid"), Name: "c", ContextName: "orbstack", APIServerURL: "https://fake.invalid", FirstSeenAt: now, LastConnectedAt: ptr(now)}, r.cluster())
	diff(t, &store.Pod{UID: "pod-crash", ClusterID: 1, Namespace: ns, Name: "web-7d9f8c6b5-abcde", NodeName: ptr("node-a"), Phase: "Running", QOSClass: ptr("BestEffort"),
		ControllerKind: "ReplicaSet", ControllerName: "web-7d9f8c6b5", ControllerUID: "rs-web-1", WorkloadKind: "Deployment", WorkloadName: "web",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: ptr("2026-08-27T11:45:05.000000Z"), FirstSeenAt: now, LastSeenAt: now}, r.pod("pod-crash"))
	if n := r.historyCount(); n != 0 {
		t.Errorf("first sight wrote %d history rows, want 0", n)
	}
	gone := r.pod("pod-stale")
	diff(t, [2]*string{ptr(now), ptr(store.DeletionSourceReconcile)}, [2]*string{gone.DeletedAt, gone.DeletionSource})

	if _, err := r.client.CoreV1().Pods(ns).Update(context.Background(), &after, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "crash incident", func() bool { return len(r.incidents("pod-crash")) == 1 })
	crash := store.Incident{ID: 1, ClusterID: 1, Namespace: ns, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api",
		WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash, FirstReason: "CrashLoopBackOff", LastReason: "CrashLoopBackOff",
		Image: ptr(web), ImageTag: ptr(webTag), ImageID: ptr(webID), NodeName: ptr("node-a"),
		Occurrences: 1, OpenedAt: "2026-08-27T11:55:00.000000Z", LastSeenAt: now}
	diff(t, []store.Incident{crash}, r.incidents("pod-crash"))
	if n := r.historyCount(); n != 1 {
		t.Errorf("transition wrote %d history rows, want 1", n)
	}

	if err := r.client.CoreV1().Pods(ns).Delete(context.Background(), after.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "pod deleted", func() bool { return r.pod("pod-crash").DeletedAt != nil })
	deleted := r.pod("pod-crash")
	diff(t, [3]*string{ptr(now), ptr(store.DeletionSourceWatch), ptr(store.DeletionReasonUnknown)}, [3]*string{deleted.DeletedAt, deleted.DeletionSource, deleted.DeletionReason})
	crash.ClosedAt, crash.CloseReason = ptr(now), ptr(store.ClosePodDeleted)
	diff(t, []store.Incident{crash}, r.incidents("pod-crash"))

	r.cancel()
	if err := <-r.done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run returned %v, want context.Canceled", err)
	}
	r.done <- nil
	if len(r.delays) != 0 {
		t.Errorf("backoff slept %v during a healthy run", r.delays)
	}
}

func TestReadyChangedReportsEachTransitionOnce(t *testing.T) {
	var attempts int
	var r *run
	r = newRun(t, func() (kubernetes.Interface, string, error) {
		attempts++
		if attempts == 1 {
			return nil, "", errors.New("dial tcp 127.0.0.1:26443: connection refused")
		}
		return r.client, "https://fake.invalid", nil
	}, kubeSystem())
	var mu sync.Mutex
	var calls []bool
	r.w.cfg.ReadyChanged = func(ready bool) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, ready)
	}
	r.w.wait = func(context.Context, time.Duration) error { return nil }
	r.start()
	eventually(t, "ready", r.w.Ready)
	r.cancel()
	<-r.done
	r.done <- nil

	mu.Lock()
	defer mu.Unlock()
	diff(t, []bool{true, false}, calls)
}

func TestForbiddenInformerRecordsErrorAndKeepsOthers(t *testing.T) {
	var before corev1.Pod
	fixture(t, "crash-loop/before.json", &before)
	r := newRun(t, nil, kubeSystem(), &before)
	r.client.PrependReactor("list", "replicasets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "replicasets"}, "", errors.New("no list"))
	})
	r.start()
	eventually(t, "ready despite forbidden replicasets", r.w.Ready)
	eventually(t, "last_error", func() bool { return r.cluster().LastError != nil })
	c := r.cluster()
	diff(t, [2]*string{ptr("forbidden: list replicasets in namespace idios-smoke"), ptr(clock.Format(testNow))}, [2]*string{c.LastError, c.LastErrorAt})
	pod := r.pod("pod-crash")
	if pod == nil {
		t.Fatal("pods informer did not run")
	}
	diff(t, [2]string{"ReplicaSet", "web-7d9f8c6b5"}, [2]string{pod.WorkloadKind, pod.WorkloadName})
}

func TestForbiddenPodsInformerLeavesNamespaceRowsAlone(t *testing.T) {
	r := newRun(t, nil, kubeSystem())
	r.client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("no list"))
	})
	kept := "2026-08-27T11:00:00.000000Z"
	r.exec(`INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at) VALUES ('pod-keep', 1, ?, 'keep', 'Running', ?, ?, ?)`, ns, kept, kept, kept)
	r.start()
	eventually(t, "ready despite forbidden pods", r.w.Ready)
	eventually(t, "last_error", func() bool { return r.cluster().LastError != nil })
	diff(t, ptr("forbidden: list pods in namespace idios-smoke"), r.cluster().LastError)
	keep := r.pod("pod-keep")
	diff(t, [2]*string{nil, nil}, [2]*string{keep.DeletedAt, keep.DeletionSource})
}

func TestRunBacksOffDoublingToOneMinute(t *testing.T) {
	var attempts int
	r := newRun(t, func() (kubernetes.Interface, string, error) {
		attempts++
		return nil, "", errors.New("dial tcp 127.0.0.1:26443: connection refused")
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.w.wait = func(_ context.Context, d time.Duration) error {
		r.delays = append(r.delays, d)
		if len(r.delays) == 8 {
			cancel()
			return context.Canceled
		}
		return nil
	}
	if err := r.w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
	sec := func(n int) time.Duration { return time.Duration(n) * time.Second }
	diff(t, []time.Duration{sec(1), sec(2), sec(4), sec(8), sec(16), sec(32), sec(60), sec(60)}, r.delays)
	if attempts != 8 {
		t.Errorf("client built %d times, want 8", attempts)
	}
	c := r.cluster()
	diff(t, [2]*string{ptr("connect: dial tcp 127.0.0.1:26443: connection refused"), ptr(clock.Format(testNow))}, [2]*string{c.LastError, c.LastErrorAt})
	if r.w.Ready() {
		t.Error("watcher reported ready without a connection")
	}
	if r.w.Client() != nil {
		t.Error("watcher offered a clientset without a connection")
	}
}

func TestSyncTimeoutIsAFailure(t *testing.T) {
	r := newRun(t, nil, kubeSystem())
	r.client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewInternalError(errors.New("etcd unavailable"))
	})
	r.w.syncTimeout = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	r.w.wait = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}
	if err := r.w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
	c := r.cluster()
	diff(t, [2]*string{ptr("sync pods: cache sync timed out after 200ms"), ptr(clock.Format(testNow))}, [2]*string{c.LastError, c.LastErrorAt})
}

// A relist that fails is the only signal that a working watcher stopped
// working: client-go reports a reflector's failure and never its recovery,
// so the failure is written once, the ready claim goes with it, and a probe
// of our own is what takes it back.
func TestARelistFailureIsAClusterError(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantError string
		wantReady bool
		wantCalls []bool
		recovers  bool
	}{
		{
			name:      "a forbidden resource is a fact about the Role, not an outage",
			err:       apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "replicasets"}, "", errors.New("no list")),
			wantError: "forbidden: list replicasets in namespace idios-smoke",
			wantReady: true,
			wantCalls: []bool{true},
		},
		{
			name:      "a credential that expired unreadies the cluster until a probe succeeds",
			err:       errors.New("getting credentials: exec: executable aws failed with exit code 255"),
			wantError: "list replicasets in namespace idios-smoke: getting credentials: exec: executable aws failed with exit code 255",
			wantReady: false,
			wantCalls: []bool{true, false},
			recovers:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRun(t, nil, kubeSystem())
			var mu sync.Mutex
			var failWith error
			var open *watch.FakeWatcher
			var failures int
			// The reflector re-runs its list and watch on a backoff, so an
			// answer of an error is an informer failing on a timer, which is
			// what an expired credential looks like. Stopping the watch it was
			// given is what makes it ask again.
			r.client.PrependWatchReactor("replicasets", func(k8stesting.Action) (bool, watch.Interface, error) {
				mu.Lock()
				defer mu.Unlock()
				if failWith != nil {
					failures++
					return true, nil, failWith
				}
				open = watch.NewFake()
				return true, open, nil
			})
			r.client.PrependReactor("get", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
				mu.Lock()
				defer mu.Unlock()
				if failWith == nil {
					return false, nil, nil
				}
				return true, nil, failWith
			})
			var callsMu sync.Mutex
			var calls []bool
			r.w.cfg.ReadyChanged = func(ready bool) {
				callsMu.Lock()
				defer callsMu.Unlock()
				calls = append(calls, ready)
			}
			readyCalls := func() []bool {
				callsMu.Lock()
				defer callsMu.Unlock()
				return append([]bool{}, calls...)
			}
			probe := make(chan struct{})
			r.w.wait = func(ctx context.Context, _ time.Duration) error {
				select {
				case <-probe:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			r.start()
			eventually(t, "ready", r.w.Ready)
			eventually(t, "the replicasets watch", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return open != nil
			})
			mu.Lock()
			failWith, w := tc.err, open
			mu.Unlock()
			w.Stop()

			eventually(t, "last_error", func() bool { return r.cluster().LastError != nil })
			c := r.cluster()
			diff(t, [2]*string{ptr(tc.wantError), ptr(clock.Format(testNow))}, [2]*string{c.LastError, c.LastErrorAt})
			if r.w.Ready() != tc.wantReady {
				t.Errorf("Ready() = %v after a failing relist, want %v", r.w.Ready(), tc.wantReady)
			}
			diff(t, tc.wantCalls, readyCalls())

			mu.Lock()
			failWith = errors.New("a second failure of the same informer")
			seen := failures
			mu.Unlock()
			eventually(t, "the informer to fail again", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return failures > seen
			})
			diff(t, ptr(tc.wantError), r.cluster().LastError)
			if !tc.recovers {
				return
			}

			mu.Lock()
			failWith = nil
			mu.Unlock()
			select {
			case probe <- struct{}{}:
			case <-time.After(5 * time.Second):
				t.Fatal("no probe asked to wait")
			}
			eventually(t, "ready again", r.w.Ready)
			c = r.cluster()
			diff(t, [2]*string{nil, nil}, [2]*string{c.LastError, c.LastErrorAt})
			diff(t, []bool{true, false, true}, readyCalls())
		})
	}
}
