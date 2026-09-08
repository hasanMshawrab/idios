// Package querytest builds the seeded database the query and api tests read.
// It is not a _test.go file so both packages can import it.
package querytest

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/processor"
	"github.com/hasanMshawrab/idios/internal/store"
)

// Cluster ids of the two seeded clusters.
const (
	ClusterProd    int64 = 1
	ClusterStaging int64 = 2
)

// Pod uids of the seeded scenarios, as the ingest fixtures spell them.
const (
	CrashPodUID  = "pod-crash"
	OOMPodUID    = "pod-oom"
	PullPodUID   = "pod-pull"
	UnschedPod   = "pod-unsched"
	MultiPodUID  = "pod-multi"
	JumpPodUID   = "pod-jump"
	InitPodUID   = "pod-init"
	ConfigPodUID = "pod-cfg"
	EvictPodUID  = "pod-evict"
)

// FailedJobUID is the job whose incident the seed holds.
const FailedJobUID = "job-report-1"

// CrashIncidentID is the incident the seeded artifacts belong to.
const CrashIncidentID int64 = 1

// Capture times of the seeded artifacts, inside the crash incident's span.
const (
	ArtifactPodJSONAt = "2026-08-27T11:55:01.000000Z"
	ArtifactLogAt     = "2026-08-27T11:55:02.000000Z"
	ArtifactGapAt     = "2026-08-27T11:55:03.000000Z"
)

// base is the fake clock's start; every call below advances it by step.
var base = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

const step = time.Second

// policy matches the defaults the processor runs with.
var policy = incident.Policy{SchedulingGrace: time.Minute, StabilizationWindow: 10 * time.Minute}

type sink struct{}

func (sink) Enqueue(processor.CaptureRequest) {}

type noOwners struct{}

func (noOwners) ReplicaSetOwner(string, string) *metav1.OwnerReference { return nil }
func (noOwners) JobOwner(string, string) *metav1.OwnerReference        { return nil }

type deployOwners struct{}

func (deployOwners) ReplicaSetOwner(string, string) *metav1.OwnerReference {
	return &metav1.OwnerReference{Kind: "Deployment", Name: "web", UID: "dep-web"}
}
func (deployOwners) JobOwner(string, string) *metav1.OwnerReference { return nil }

type seeder struct {
	t   testing.TB
	st  *store.Store
	clk *clock.Fake
	p   *processor.Processor
}

// Seed returns a migrated temp-directory store holding one deterministic
// snapshot of the ingest fixtures, and the fake clock that produced it.
//
// Clusters: 1 prod (context prod) and 2 staging, two watched namespaces each.
//
// Incidents, by id:
//
//	 1  cluster 1  pod-crash    api      crash          open, newest activity
//	 2  cluster 1  pod-oom      api      oom            acknowledged
//	 3  cluster 1  pod-pull     api      image_pull     dismissed while open
//	 4  cluster 1  pod-unsched  --       scheduling     recovered
//	 5  cluster 1  pod-multi    worker   crash          open
//	 6  cluster 1  pod-jump     api      crash          pod_deleted
//	 7  cluster 1  pod-init     init-db  crash          dismissed after close
//	 8  cluster 2  pod-cfg      api      config         open
//	 9  cluster 2  pod-evict    api      crash          open
//	10  cluster 2  pod-evict    --       node_pressure  manual
//	11  cluster 1  job-report-1 --       job_failed     job_finished
//	12  cluster 1  pod-crash    api      probe          open
//
// Also seeded: pod-side, a healthy pod with no incident; job-report-2, a
// complete job; both revisions of the web Deployment in rollout_history; the
// Unhealthy, Killing and Evicted events on pod-crash; one sweep_runs row whose
// cutoff falls three days before every incident span; and three artifacts of
// incident 1 (a pod_json file, a log_previous file, a log_current gap).
//
// Fixture order is append only: ids come from insertion order and the fake
// clock stamps last_seen_at one second apart, so inserting a scenario in the
// middle renumbers and restamps every row after it.
func Seed(t testing.TB) (*store.Store, *clock.Fake) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := clock.NewFake(base)
	if err := st.Migrate(context.Background(), clk); err != nil {
		t.Fatal(err)
	}
	s := &seeder{t: t, st: st, clk: clk, p: processor.New(st.Writer, clk, sink{}, policy)}
	s.clusters()
	s.pods()
	s.jobs()
	s.rollouts()
	s.events()
	// The deletion reason is read from the rollout revisions and the live
	// siblings, so the ReplicaSets must be in before the pod goes.
	s.deletions()
	s.humanActions()
	s.sweepRun()
	s.artifacts()
	return st, clk
}

func (s *seeder) tx(fn func(ctx context.Context, tx *sql.Tx) error) {
	s.t.Helper()
	ctx := context.Background()
	if err := s.st.Writer.Tx(ctx, func(tx *sql.Tx) error { return fn(ctx, tx) }); err != nil {
		s.t.Fatal(err)
	}
}

func (s *seeder) exec(query string, args ...any) {
	s.t.Helper()
	s.tx(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	})
}

func (s *seeder) clusters() {
	at := clock.Format(base)
	s.tx(func(ctx context.Context, tx *sql.Tx) error {
		for _, c := range []struct {
			id         int64
			name, url  string
			namespaces []string
		}{
			{ClusterProd, "prod", "https://127.0.0.1:26443", []string{"idios-smoke", "default"}},
			{ClusterStaging, "staging", "https://127.0.0.1:26444", []string{"idios-smoke", "staging-web"}},
		} {
			id, err := store.InsertCluster(ctx, tx, c.name, c.name, at)
			if err != nil {
				return err
			}
			if id != c.id {
				s.t.Fatalf("cluster %s got id %d, want %d", c.name, id, c.id)
			}
			identity := "id-" + c.name
			if err := store.MarkClusterConnected(ctx, tx, id, &identity, c.url, at); err != nil {
				return err
			}
			for _, ns := range c.namespaces {
				if _, err := store.AddWatchedNamespace(ctx, tx, id, ns, at); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *seeder) pods() {
	s.pod(ClusterProd, deployOwners{}, "crash-loop/before.json", "crash-loop/after.json")
	s.pod(ClusterProd, noOwners{}, "oom/before.json", "oom/after.json")
	s.pod(ClusterProd, noOwners{}, "image-pull/s1.json", "image-pull/s2.json", "image-pull/s3.json")
	s.pod(ClusterProd, noOwners{}, "unschedulable/s1.json", "unschedulable/s2.json", "unschedulable/s3.json")
	s.pod(ClusterProd, noOwners{}, "multi-container/before.json", "multi-container/after.json")
	s.pod(ClusterProd, deployOwners{}, "restart-jump/before.json", "restart-jump/after.json")
	s.pod(ClusterProd, noOwners{}, "init-then-success/before.json", "init-then-success/after.json")
	s.pod(ClusterProd, noOwners{}, "sidecar/pod.json")
	s.pod(ClusterStaging, noOwners{}, "config/before.json", "config/after.json")
	s.pod(ClusterStaging, deployOwners{}, "evicted/before.json", "evicted/after.json")
}

func (s *seeder) jobs() {
	s.job("job-failed/before.json", "job-failed/after.json")
	s.job("job-complete/before.json", "job-complete/after.json")
}

// SeedJobPods adds the two retries of the failed job to a seeded store, the
// newer one pruned, and returns them oldest first. They are not part of Seed
// because every pod and workload list would count them; only the job's
// incident detail wants them. No fixture drives them, so the rows go in
// directly with the job's own times. The pruned retry carries a container,
// a captured log and an unattached event, because the job incident's page
// shows the newest pod's evidence whole.
func SeedJobPods(t testing.TB, st *store.Store) []store.Pod {
	t.Helper()
	pods := []store.Pod{
		{UID: "pod-report-1", Name: "report-28812345-a1b2c", Phase: "Failed", CreatedAt: "2026-08-27T11:45:03.000000Z",
			StartedAt: ptr("2026-08-27T11:45:05.000000Z")},
		{UID: "pod-report-2", Name: "report-28812345-d4e5f", Phase: "Failed", CreatedAt: "2026-08-27T11:51:00.000000Z",
			StartedAt: ptr("2026-08-27T11:51:02.000000Z"), DeletedAt: ptr("2026-08-27T11:57:30.000000Z"),
			DeletionSource: ptr(store.DeletionSourceWatch), DeletionReason: ptr(store.DeletionReasonJobPruned)},
	}
	for i := range pods {
		p := &pods[i]
		p.ClusterID, p.Namespace, p.NodeName, p.QOSClass = ClusterProd, "idios-smoke", ptr("node-a"), ptr("BestEffort")
		p.ControllerKind, p.ControllerName, p.ControllerUID = "Job", "report-28812345", FailedJobUID
		p.WorkloadKind, p.WorkloadName = "CronJob", "report"
		p.FirstSeenAt, p.LastSeenAt = "2026-08-27T12:00:21.000000Z", "2026-08-27T12:00:22.000000Z"
	}
	container := store.Container{
		PodUID: "pod-report-2", Name: "report", Kind: store.ContainerKindApp,
		Image: "registry.example.com/report:0.9.1", ImageTag: ptr("0.9.1"),
		State: store.StateTerminated, Reason: ptr("Error"), ExitCode: ptr[int64](1), Signal: ptr[int64](0),
		UpdatedAt: "2026-08-27T11:57:00.000000Z",
	}
	artifact := store.Artifact{
		PodUID: "pod-report-2", ContainerName: "report", Kind: store.ArtifactLogPrevious,
		FilePath: ptr("prod/pod-report-2/report-0.log"), SizeBytes: 1024,
		CapturedAt: "2026-08-27T11:57:01.000000Z",
	}
	event := store.K8sEvent{
		ClusterID: ClusterProd, EventUID: "ev-report-killing-1", Namespace: "idios-smoke", Type: "Normal",
		InvolvedKind: "Pod", InvolvedName: "report-28812345-d4e5f", InvolvedUID: "pod-report-2",
		FieldPath: "spec.containers{report}", Reason: "Killing", Message: "Stopping container report",
		SourceComponent: "kubelet", Count: 1,
		FirstTS: "2026-08-27T11:56:58.000000Z", LastTS: "2026-08-27T11:56:58.000000Z", RawJSON: "{}",
	}
	ctx := context.Background()
	err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		for _, p := range pods {
			if err := store.UpsertPod(ctx, tx, p); err != nil {
				return err
			}
		}
		if err := store.UpsertContainers(ctx, tx, []store.Container{container}); err != nil {
			return err
		}
		if err := store.UpsertArtifact(ctx, tx, artifact); err != nil {
			return err
		}
		return store.UpsertEvent(ctx, tx, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	return pods
}

func (s *seeder) rollouts() {
	for _, f := range []string{"replicaset/deploy.json", "replicaset/deploy-rev8.json"} {
		var rs appsv1.ReplicaSet
		s.fixture(f, &rs)
		if err := s.p.ReplicaSet(context.Background(), ClusterProd, &rs); err != nil {
			s.t.Fatalf("%s: %v", f, err)
		}
		s.clk.Advance(step)
	}
}

func (s *seeder) events() {
	for _, f := range []string{"event-unhealthy/event.json", "event-killing/event.json", "event-evicted/event.json"} {
		var ev corev1.Event
		s.fixture(f, &ev)
		if err := s.p.Event(context.Background(), ClusterProd, &ev); err != nil {
			s.t.Fatalf("%s: %v", f, err)
		}
		s.clk.Advance(step)
	}
}

func (s *seeder) deletions() {
	if err := s.p.PodDeleted(context.Background(), JumpPodUID, store.DeletionSourceWatch); err != nil {
		s.t.Fatal(err)
	}
	s.clk.Advance(step)
}

// humanActions applies what a person and the periodic closer do. No store
// function writes acknowledged_at, dismissed_at or a manual close yet, so
// these are inline updates.
func (s *seeder) humanActions() {
	at := clock.Format(s.clk.Now())
	s.exec(`UPDATE incidents SET acknowledged_at = ? WHERE pod_uid = ? AND closed_at IS NULL`, at, OOMPodUID)
	s.exec(`UPDATE incidents SET dismissed_at = ? WHERE pod_uid = ? AND closed_at IS NULL`, at, PullPodUID)
	s.exec(`UPDATE incidents SET closed_at = ?, close_reason = ?, dismissed_at = ? WHERE pod_uid = ? AND closed_at IS NULL`,
		at, store.CloseRecovered, at, InitPodUID)
	s.exec(`UPDATE incidents SET closed_at = ?, close_reason = ? WHERE pod_uid = ? AND category = ?`,
		at, store.CloseManual, EvictPodUID, store.CategoryNodePressure)
	s.exec(`UPDATE incidents SET closed_at = ?, close_reason = ? WHERE job_uid = ?`, at, store.CloseJobFinished, FailedJobUID)
	// The list is ordered by activity, not by id; without this the two would
	// agree row for row and a wrong ORDER BY would still pass.
	s.exec(`UPDATE incidents SET last_seen_at = ? WHERE pod_uid = ? AND category = ?`, at, CrashPodUID, store.CategoryCrash)
	s.clk.Advance(step)
}

func (s *seeder) sweepRun() {
	now := s.clk.Now()
	s.tx(func(ctx context.Context, tx *sql.Tx) error {
		return store.InsertSweepRun(ctx, tx, store.SweepRun{
			RanAt: clock.Format(now), Cutoff: clock.Format(now.Add(-72 * time.Hour)),
			TableName: "container_state_history", RowsRemoved: 3, DurationMs: 4,
		})
	})
	s.clk.Advance(step)
}

// artifacts writes what the capture pool would have written for the crash
// incident. The pool is not run by this seed, so the rows go in directly, and
// their times are the incident's, not the seeding clock's: a capture follows
// the transition that triggered it by seconds.
func (s *seeder) artifacts() {
	s.tx(func(ctx context.Context, tx *sql.Tx) error {
		for _, a := range []store.Artifact{
			{PodUID: CrashPodUID, IncidentID: ptr(CrashIncidentID), Kind: store.ArtifactPodJSON,
				RestartCount: store.NoRestartIndex, FilePath: ptr("prod/pod-crash/pod.json"), SizeBytes: 2048,
				CapturedAt: ArtifactPodJSONAt},
			{PodUID: CrashPodUID, IncidentID: ptr(CrashIncidentID), ContainerName: "api", Kind: store.ArtifactLogPrevious,
				RestartCount: 0, FilePath: ptr("prod/pod-crash/api-0.log"), SizeBytes: 4096, Truncated: true,
				CapturedAt: ArtifactLogAt},
			{PodUID: CrashPodUID, IncidentID: ptr(CrashIncidentID), ContainerName: "api", Kind: store.ArtifactLogCurrent,
				RestartCount: store.NoRestartIndex, CaptureGap: ptr(store.GapNoOutput),
				CaptureNote: ptr("container produced no output"), CapturedAt: ArtifactGapAt},
		} {
			if err := store.UpsertArtifact(ctx, tx, a); err != nil {
				return err
			}
		}
		return nil
	})
	s.clk.Advance(step)
}

func ptr[T any](v T) *T { return &v }

func (s *seeder) pod(clusterID int64, r ingest.OwnerResolver, files ...string) {
	s.t.Helper()
	for _, f := range files {
		var pod corev1.Pod
		s.fixture(f, &pod)
		if err := s.p.Pod(context.Background(), clusterID, &pod, r); err != nil {
			s.t.Fatalf("%s: %v", f, err)
		}
		s.clk.Advance(step)
	}
}

func (s *seeder) job(files ...string) {
	s.t.Helper()
	for _, f := range files {
		var job batchv1.Job
		s.fixture(f, &job)
		if err := s.p.Job(context.Background(), ClusterProd, &job); err != nil {
			s.t.Fatalf("%s: %v", f, err)
		}
		s.clk.Advance(step)
	}
}

func (s *seeder) fixture(path string, into any) {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureRoot(s.t), path))
	if err != nil {
		s.t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		s.t.Fatalf("%s: %v", path, err)
	}
}

// fixtureRoot locates the fixtures from this file's path, because the test
// binary's working directory is the importing package's, not this one's.
func fixtureRoot(t testing.TB) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller information")
	}
	return filepath.Join(filepath.Dir(self), "..", "..", "ingest", "testdata")
}
