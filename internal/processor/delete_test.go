package processor

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

func TestPodDeletedScenarios(t *testing.T) {
	now := clock.Format(testNow)
	cases := []struct {
		name        string
		prepare     func(t *testing.T, h *harness)
		uid, source string
		wantPod     *[3]string
		wantClosed  int
		wantReqs    []CaptureRequest
	}{
		{"watch delete closes incidents and keeps the logs", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
		}, "pod-crash", store.DeletionSourceWatch, &[3]string{now, store.DeletionSourceWatch, store.DeletionReasonUnknown}, 1,
			[]CaptureRequest{request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerDelete, ptr[int64](1))}},
		{"healthy pod under a newer revision is a rollout and is dropped", func(t *testing.T, h *harness) {
			h.feedReplicaSets(t, "replicaset/deploy.json", "replicaset/deploy-rev8.json")
			h.feed(t, steps("crash-loop/before.json"))
		}, "pod-crash", store.DeletionSourceWatch, &[3]string{now, store.DeletionSourceWatch, store.DeletionReasonRollout}, 0, nil},
		{"healthy pod with a newer live sibling was replaced", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json"))
			h.exec(t, `INSERT INTO pods (uid, cluster_id, namespace, name, phase, controller_kind, controller_uid, created_at, first_seen_at, last_seen_at)
VALUES ('pod-sib', 1, 'idios-smoke', 'web-7d9f8c6b5-zzzzz', 'Running', 'ReplicaSet', 'rs-web-1', '2026-08-27T11:50:00.000000Z', ?, ?)`, now, now)
		}, "pod-crash", store.DeletionSourceWatch, &[3]string{now, store.DeletionSourceWatch, store.DeletionReasonReplaced}, 0, nil},
		{"job pod is pruned and its failed log is kept", func(t *testing.T, h *harness) {
			h.feed(t, steps("job-never-error/before.json", "job-never-error/after.json"))
		}, "pod-jobfail", store.DeletionSourceWatch, &[3]string{now, store.DeletionSourceWatch, store.DeletionReasonJobPruned}, 1,
			[]CaptureRequest{request("pod-jobfail", "import-28812346-b7c3d", "worker", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerDelete, ptr[int64](1))}},
		{"terminating pod's unclean exit closes pod_deleted", func(t *testing.T, h *harness) {
			h.feed(t, steps("terminating-exit/before.json", "terminating-exit/after.json"))
		}, "pod-term", store.DeletionSourceWatch, &[3]string{now, store.DeletionSourceWatch, store.DeletionReasonUnknown}, 1,
			[]CaptureRequest{request("pod-term", "web-7d9f8c6b5-term1", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerDelete, ptr[int64](1))}},
		{"reconcile closes but enqueues nothing", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
		}, "pod-crash", store.DeletionSourceReconcile, &[3]string{now, store.DeletionSourceReconcile, store.DeletionReasonUnknown}, 1, nil},
		{"unknown pod is ignored", func(*testing.T, *harness) {}, "pod-never-seen", store.DeletionSourceWatch, nil, 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			c.prepare(t, h)
			h.sink.reqs = nil
			if err := h.p.PodDeleted(context.Background(), c.uid, c.source); err != nil {
				t.Fatal(err)
			}
			if err := h.p.PodDeleted(context.Background(), c.uid, c.source); err != nil {
				t.Fatal(err)
			}
			pod := h.pod(t, c.uid)
			if c.wantPod == nil {
				if pod != nil {
					t.Fatalf("pod row appeared: %+v", pod)
				}
			} else {
				if pod == nil || pod.DeletedAt == nil || pod.DeletionSource == nil || pod.DeletionReason == nil {
					t.Fatalf("deletion columns not set: %+v", pod)
				}
				diff(t, *c.wantPod, [3]string{*pod.DeletedAt, *pod.DeletionSource, *pod.DeletionReason})
			}
			closed := 0
			for _, inc := range h.incidents(t, c.uid) {
				if inc.ClosedAt == nil {
					t.Errorf("incident %d still open", inc.ID)
				}
				if inc.CloseReason != nil && *inc.CloseReason == store.ClosePodDeleted && *inc.ClosedAt == now {
					closed++
				}
			}
			if closed != c.wantClosed {
				t.Errorf("closed with pod_deleted: %d, want %d", closed, c.wantClosed)
			}
			if d := cmp.Diff(c.wantReqs, h.sink.reqs, cmpopts.EquateEmpty()); d != "" {
				t.Error(d)
			}
		})
	}
}

// A pod evicted by the kubelet carries a DisruptionTarget=True condition
// before it goes; that must win over the sibling-based inference, which
// would otherwise read the same ReplicaSet's live sibling as a replacement.
func TestEvictedPodIsRecordedAsEvicted(t *testing.T) {
	h := newHarness(t)
	h.feed(t, steps("disruption-kubelet/before.json", "disruption-kubelet/after.json"))
	if err := h.p.PodDeleted(context.Background(), "pod-press", store.DeletionSourceWatch); err != nil {
		t.Fatal(err)
	}
	now := clock.Format(testNow)
	want := &store.Pod{
		UID: "pod-press", ClusterID: 1, Namespace: "idios-smoke", Name: "web-7d9f8c6b5-press",
		NodeName: ptr("node-a"), Phase: "Failed", StatusReason: ptr("Evicted"), StatusMessage: ptr("The node was low on resource: memory."),
		QOSClass:       ptr("BestEffort"),
		ControllerKind: "ReplicaSet", ControllerName: "web-7d9f8c6b5", ControllerUID: "rs-web-1",
		WorkloadKind: "ReplicaSet", WorkloadName: "web-7d9f8c6b5",
		CreatedAt: "2026-08-27T11:45:00.000000Z", StartedAt: ptr("2026-08-27T11:45:05.000000Z"),
		FirstSeenAt: now, LastSeenAt: now,
		DeletedAt: ptr(now), DeletionSource: ptr(store.DeletionSourceWatch), DeletionReason: ptr(store.DeletionReasonEvicted),
	}
	diff(t, want, h.pod(t, "pod-press"))
}
