package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/status"
)

// write puts content at path, creating the directories above it.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// statusRuntime is the process snapshot the status test merges in.
func statusRuntime() fakeRuntime {
	return fakeRuntime{snap: status.Snapshot{
		WrittenAt: "2026-08-27T15:00:00.000000Z", PID: 4242, Version: "test",
		Clusters: []status.Cluster{
			{ID: querytest.ClusterProd, Ready: true, LastEventAt: "2026-08-27T15:29:00.000000Z", SkewSeconds: 1.5},
			{ID: querytest.ClusterStaging},
		},
		Writer:   status.Writer{Transactions: 120, Errors: 2, P99Ms: 3.5},
		Handlers: status.Handlers{Errors: 1, Panics: 0},
		Capture: status.Capture{Queued: 9, Completed: 7, Dropped: 1,
			Gaps: map[string]uint64{"no_output": 2, "pod_deleted": 1}},
		Closer: status.Closer{LastTickAt: "2026-08-27T15:28:00.000000Z", Closed: 5, Attached: 3, Opened: 2,
			ClosedTotal: 41, AttachedTotal: 12, OpenedTotal: 7},
	}}
}

// seedRowCounts sizes the observation tables querytest.Seed leaves behind.
func seedRowCounts() *idiosv1.RowCounts {
	return &idiosv1.RowCounts{Pods: 10, LivePods: 9, Transitions: 12, Events: 3}
}

// seedSweepRuns is the one recorded pass of the seed.
func seedSweepRuns() []*idiosv1.SweepRun {
	return []*idiosv1.SweepRun{{
		Id: 1, RanAt: "2026-08-27T12:00:32.000000Z", Cutoff: "2026-08-24T12:00:32.000000Z",
		TableName: "container_state_history", RowsRemoved: 3, DurationMs: 4,
	}}
}

// idios status prints the running process's snapshot merged with the
// database aggregates and the settings in force, so every part of it is
// asserted at once. written_at is the moment the answer was assembled, not
// the moment the process last published a file.
func TestStatusAggregates(t *testing.T) {
	stack := newTestStack(t, withRuntime(statusRuntime()))
	write(t, stack.cfg.DBPath(), "0123456789")
	write(t, stack.cfg.DBPath()+"-wal", "wal")
	write(t, filepath.Join(stack.artifactsRoot, "prod", "pod-crash", "api-0.log"), "twelve bytes")
	write(t, filepath.Join(stack.artifactsRoot, "prod", "pod-crash", "pod.json"), "{}")

	client := idiosv1.NewIdiosServiceClient(stack.url)
	got, err := client.GetStatus(context.Background(), &idiosv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	want := &idiosv1.Status{
		DaemonRunning: true, WrittenAt: testNow, Pid: 4242, Version: "test",
		Clusters: []*idiosv1.StatusCluster{
			{Id: querytest.ClusterProd, Ready: true, LastEventAt: sp("2026-08-27T15:29:00.000000Z"), SkewSeconds: 1.5},
			{Id: querytest.ClusterStaging},
		},
		Writer:   &idiosv1.WriterStats{Transactions: 120, Errors: 2, P99Ms: 3.5},
		Handlers: &idiosv1.HandlerStats{Errors: 1},
		Capture: &idiosv1.CaptureStats{Queued: 9, Completed: 7, Dropped: 1, Gaps: []*idiosv1.GapCount{
			{Gap: idiosv1.CaptureGap_CAPTURE_GAP_POD_DELETED, Count: 1},
			{Gap: idiosv1.CaptureGap_CAPTURE_GAP_NO_OUTPUT, Count: 2},
		}},
		Closer: &idiosv1.CloserStats{LastTickAt: sp("2026-08-27T15:28:00.000000Z"), Closed: 5, Attached: 3, Opened: 2,
			ClosedTotal: 41, AttachedTotal: 12, OpenedTotal: 7},
		OpenByCategory: []*idiosv1.CategoryCount{
			categoryCount(idiosv1.Category_CATEGORY_OOM, 1),
			categoryCount(idiosv1.Category_CATEGORY_CRASH, 3),
			categoryCount(idiosv1.Category_CATEGORY_IMAGE_PULL, 1),
			categoryCount(idiosv1.Category_CATEGORY_CONFIG, 1),
			categoryCount(idiosv1.Category_CATEGORY_PROBE, 1),
		},
		ClosedByReason: []*idiosv1.CloseReasonCount{
			{CloseReason: idiosv1.CloseReason_CLOSE_REASON_RECOVERED, Count: 2},
			{CloseReason: idiosv1.CloseReason_CLOSE_REASON_POD_DELETED, Count: 1},
			{CloseReason: idiosv1.CloseReason_CLOSE_REASON_JOB_FINISHED, Count: 1},
			{CloseReason: idiosv1.CloseReason_CLOSE_REASON_MANUAL, Count: 1},
		},
		ArtifactsByOutcome: []*idiosv1.OutcomeCount{
			{Outcome: "file", Count: 2},
			{Outcome: "no_output", Count: 1},
			{Outcome: "unobservable", Count: 1},
		},
		RowCounts:       seedRowCounts(),
		LatestSweepRuns: seedSweepRuns(),
		DbBytes:         10, WalBytes: 3, ArtifactFiles: 2, ArtifactBytes: 14,
		RetentionDays: 3, SweepIntervalSeconds: 3600, StabilizationWindowSeconds: 600,
		StabilizationCheckIntervalSeconds: 30, EarlyCaptureDebounceSeconds: 60,
		ApiStreamThrottleSeconds: 1, SchedulingGraceSeconds: 60, ProbeGraceSeconds: 60,
		StuckAfterSeconds: 600, AttentionWindowSeconds: 86400,
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}

// A server with no process behind it answers from the database alone, which
// is what idios mock does, and says the daemon is not running rather than
// reporting a zero snapshot as a live one.
func TestStatusWithoutARuntimeSaysTheDaemonIsNotRunning(t *testing.T) {
	client := newTestServer(t)
	got, err := client.GetStatus(context.Background(), &idiosv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetDaemonRunning() {
		t.Error("daemon_running with no runtime")
	}
	if diff := cmp.Diff(seedRowCounts(), got.GetRowCounts(), protocmp.Transform()); diff != "" {
		t.Errorf("row counts (-want +got)\n%s", diff)
	}
	if len(got.GetClusters()) != 0 {
		t.Errorf("clusters %v with no runtime", got.GetClusters())
	}
}
