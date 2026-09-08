package processor

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/store"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

// policy is set once here; grace is one minute so a scenario can sit inside
// or past the window by moving the fake clock.
var policy = incident.Policy{SchedulingGrace: time.Minute, StabilizationWindow: 10 * time.Minute}

type fakeSink struct{ reqs []CaptureRequest }

func (f *fakeSink) Enqueue(r CaptureRequest) { f.reqs = append(f.reqs, r) }

type noOwners struct{}

func (noOwners) ReplicaSetOwner(string, string) *metav1.OwnerReference { return nil }
func (noOwners) JobOwner(string, string) *metav1.OwnerReference        { return nil }

type deployOwners struct{}

func (deployOwners) ReplicaSetOwner(string, string) *metav1.OwnerReference {
	return &metav1.OwnerReference{Kind: "Deployment", Name: "web", UID: "dep-web"}
}
func (deployOwners) JobOwner(string, string) *metav1.OwnerReference { return nil }

type harness struct {
	p    *Processor
	s    *store.Store
	sink *fakeSink
	clk  *clock.Fake
}

func newHarness(t *testing.T) *harness {
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
	h := &harness{s: s, sink: &fakeSink{}, clk: clk}
	h.exec(t, `INSERT INTO clusters (id, identity, name, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'c', 'orbstack', 'https://127.0.0.1:26443', ?)`, clock.Format(testNow))
	h.p = New(s.Writer, clk, h.sink, policy)
	return h
}

func (h *harness) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	err := h.s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), query, args...)
		return err
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, query)
	}
}

func (h *harness) tx(t *testing.T, fn func(tx *sql.Tx) error) {
	t.Helper()
	if err := h.s.Writer.Tx(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
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

func loadPod(t *testing.T, path string) *corev1.Pod {
	t.Helper()
	var pod corev1.Pod
	fixture(t, path, &pod)
	return &pod
}

func (h *harness) incidents(t *testing.T, uid string) []store.Incident {
	t.Helper()
	var out []store.Incident
	h.tx(t, func(tx *sql.Tx) (err error) {
		out, err = store.LoadIncidentsForSubject(context.Background(), tx, uid)
		return err
	})
	return out
}

func (h *harness) pod(t *testing.T, uid string) *store.Pod {
	t.Helper()
	var out *store.Pod
	h.tx(t, func(tx *sql.Tx) (err error) { out, err = store.LoadPod(context.Background(), tx, uid); return err })
	return out
}

func (h *harness) history(t *testing.T) []store.ContainerStateHistory {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT id, pod_uid, container_name, incident_id, image, image_id, container_id, state, reason, message, exit_code, signal, restart_count,
       category, k8s_started_at, k8s_finished_at, observed_at, gap_reconstructed FROM container_state_history ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.ContainerStateHistory
	for rows.Next() {
		var r store.ContainerStateHistory
		var gap int64
		if err := rows.Scan(&r.ID, &r.PodUID, &r.ContainerName, &r.IncidentID, &r.Image, &r.ImageID, &r.ContainerID, &r.State, &r.Reason,
			&r.Message, &r.ExitCode, &r.Signal, &r.RestartCount, &r.Category, &r.K8sStartedAt, &r.K8sFinishedAt, &r.ObservedAt, &gap); err != nil {
			t.Fatal(err)
		}
		r.GapReconstructed = gap == 1
		out = append(out, r)
	}
	return out
}

func (h *harness) artifacts(t *testing.T) []store.Artifact {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT id, pod_uid, incident_id, container_name, kind, restart_count, file_path, size_bytes, truncated, captured_early, capture_gap, capture_note, captured_at
FROM artifacts ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.Artifact
	for rows.Next() {
		var a store.Artifact
		var trunc, early int64
		if err := rows.Scan(&a.ID, &a.PodUID, &a.IncidentID, &a.ContainerName, &a.Kind, &a.RestartCount, &a.FilePath, &a.SizeBytes, &trunc, &early,
			&a.CaptureGap, &a.CaptureNote, &a.CapturedAt); err != nil {
			t.Fatal(err)
		}
		a.Truncated, a.CapturedEarly = trunc == 1, early == 1
		out = append(out, a)
	}
	return out
}

func (h *harness) conditions(t *testing.T, uid string) []store.PodCondition {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(),
		"SELECT id, pod_uid, type, status, reason, message, k8s_transition_at, observed_at FROM pod_condition_history WHERE pod_uid = ? ORDER BY id", uid)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.PodCondition
	for rows.Next() {
		var c store.PodCondition
		if err := rows.Scan(&c.ID, &c.PodUID, &c.Type, &c.Status, &c.Reason, &c.Message, &c.K8sTransitionAt, &c.ObservedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// seedIncident opens an incident with the given close state so reopen rules
// can be exercised without a fixture sequence long enough to produce one.
func (h *harness) seedIncident(t *testing.T, podUID, container, category string, closeReason *string) int64 {
	t.Helper()
	var id int64
	h.tx(t, func(tx *sql.Tx) (err error) {
		id, err = store.OpenIncident(context.Background(), tx, store.Incident{
			ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr(podUID), ContainerName: container,
			WorkloadKind: "ReplicaSet", WorkloadName: "web-7d9f8c6b5", Category: category, FirstReason: "Error", LastReason: "Error",
			Occurrences: 3, OpenedAt: "2026-08-27T10:00:00.000000Z", LastSeenAt: "2026-08-27T10:30:00.000000Z",
		})
		if err != nil || closeReason == nil {
			return err
		}
		return store.CloseIncident(context.Background(), tx, id, *closeReason, "2026-08-27T10:45:00.000000Z")
	})
	return id
}

func diff(t *testing.T, want, got any) {
	t.Helper()
	if d := cmp.Diff(want, got, cmpopts.EquateEmpty()); d != "" {
		t.Error(d)
	}
}
