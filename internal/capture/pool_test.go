package capture

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/processor"
	"github.com/hasanMshawrab/idios/internal/store"
)

func podJSONRequest(cluster int64, uid string) processor.CaptureRequest {
	return processor.CaptureRequest{ClusterID: cluster, Namespace: "idios-smoke", PodUID: uid, PodName: "pod-" + uid,
		Kind: store.ArtifactPodJSON, RestartCount: store.NoRestartIndex, Trigger: processor.TriggerRestart,
		Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-" + uid, Namespace: "idios-smoke", UID: "p1"}}}
}

func TestFullQueueDropsAndCounts(t *testing.T) {
	h := newHarness(t)
	h.p.cfg.QueueSize = 2
	h.p.AddCluster(1, h.src)
	for i := 0; i < 3; i++ {
		h.p.Enqueue(podJSONRequest(1, "p1"))
	}
	h.p.Enqueue(podJSONRequest(9, "p1"))
	want := Stats{Queued: 2, Dropped: 2, Gaps: map[string]uint64{}}
	if d := cmp.Diff(want, h.p.Stats()); d != "" {
		t.Fatal(d)
	}
	if got := len(h.p.clusters[1].ch); got != 2 {
		t.Fatalf("queued = %d, want 2", got)
	}
}

func waitForCompleted(t *testing.T, h *harness, n uint64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.p.Stats().Completed < n {
		if time.Now().After(deadline) {
			t.Fatalf("fewer than %d completed within 5s", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestClusterAddedToARunningPoolGetsWorkersAndARemovedOneDrops(t *testing.T) {
	h := newHarness(t)
	h.seedPod(t, "p1", 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.p.Run(ctx) }()

	h.p.AddCluster(1, h.src)
	h.p.Enqueue(podJSONRequest(1, "p1"))
	waitForCompleted(t, h, 1)
	if got := len(h.artifacts(t)); got != 1 {
		t.Fatalf("artifact rows = %d, want 1", got)
	}

	h.p.RemoveCluster(1)
	h.p.Enqueue(podJSONRequest(1, "p1"))

	want := Stats{Queued: 1, Completed: 1, Dropped: 1, Gaps: map[string]uint64{"file": 1}}
	if d := cmp.Diff(want, h.p.Stats()); d != "" {
		t.Fatal(d)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
}
