package capture

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/processor"
	"github.com/hasanMshawrab/idios/internal/store"
)

const (
	testTail  = 2
	testBytes = int64(8)
)

type call struct {
	Namespace string
	Pod       string
	Opts      corev1.PodLogOptions
}

type fakeSource struct {
	mu      sync.Mutex
	calls   []call
	respond func(c call) (string, error)
}

func (f *fakeSource) Logs(_ context.Context, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	c := call{Namespace: namespace, Pod: pod, Opts: *opts}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	body, err := f.respond(c)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func (f *fakeSource) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// checkingWriter records whether the artifact file existed when the row
// was written.
type checkingWriter struct {
	w    *store.Writer
	root string

	mu           sync.Mutex
	fileAtRow    []bool
	pendingCheck string
	failFirstTx  bool
}

var errKeepCheck = errors.New("database is locked")

func (c *checkingWriter) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	c.mu.Lock()
	if c.failFirstTx {
		c.failFirstTx = false
		c.mu.Unlock()
		return errKeepCheck
	}
	if c.pendingCheck != "" {
		_, err := os.Stat(filepath.Join(c.root, c.pendingCheck))
		c.fileAtRow = append(c.fileAtRow, err == nil)
	}
	c.mu.Unlock()
	return c.w.Tx(ctx, fn)
}

type harness struct {
	p    *Pool
	s    *store.Store
	src  *fakeSource
	cw   *checkingWriter
	clk  *clock.Fake
	root string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "idios.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clk := clock.NewFake(testNow)
	if err := s.Migrate(context.Background(), clk); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "artifacts")
	h := &harness{s: s, clk: clk, root: root}
	h.src = &fakeSource{respond: func(call) (string, error) { return "", nil }}
	h.cw = &checkingWriter{w: s.Writer, root: root}
	cfg := Config{Root: root, TailLines: testTail, MaxBytes: testBytes, Workers: 1, QueueSize: 4, EarlyDebounce: time.Minute, StabilizationWindow: 10 * time.Minute}
	h.p = New(cfg, h.cw, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.exec(t, `INSERT INTO clusters (id, identity, name, context_name, api_server_url, first_seen_at) VALUES (1, 'c', 'c', 'orbstack', 'https://127.0.0.1:26443', ?)`, clock.Format(testNow))
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

func (h *harness) seedPod(t *testing.T, uid string, exitCode int64) {
	t.Helper()
	ts := clock.Format(testNow)
	h.exec(t, `INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at) VALUES (?, 1, 'idios-smoke', ?, 'Running', ?, ?, ?)`, uid, "pod-"+uid, ts, ts, ts)
	h.exec(t, `INSERT INTO containers (pod_uid, name, kind, image, state, exit_code, restart_count, updated_at) VALUES (?, 'api', 'app', 'img', 'terminated', ?, 1, ?)`, uid, exitCode, ts)
}

func (h *harness) seedIncident(t *testing.T, uid string) int64 {
	t.Helper()
	var id int64
	err := h.s.Writer.Tx(context.Background(), func(tx *sql.Tx) (err error) {
		id, err = store.OpenIncident(context.Background(), tx, store.Incident{
			ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: &uid, ContainerName: "api",
			WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash, FirstReason: "Error", LastReason: "Error",
			Occurrences: 1, OpenedAt: clock.Format(testNow), LastSeenAt: clock.Format(testNow),
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (h *harness) artifacts(t *testing.T) []store.Artifact {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT pod_uid, incident_id, container_name, kind, restart_count, file_path, size_bytes, truncated, captured_early, capture_gap, capture_note, captured_at
FROM artifacts ORDER BY pod_uid, container_name, kind, restart_count`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.Artifact
	for rows.Next() {
		var a store.Artifact
		if err := rows.Scan(&a.PodUID, &a.IncidentID, &a.ContainerName, &a.Kind, &a.RestartCount, &a.FilePath, &a.SizeBytes, &a.Truncated, &a.CapturedEarly, &a.CaptureGap, &a.CaptureNote, &a.CapturedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (h *harness) file(t *testing.T, rel string) *string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.root, rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	return &s
}

func previousRequest(uid string, previous bool) processor.CaptureRequest {
	return processor.CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: uid, PodName: "pod-" + uid, Container: "api",
		Kind: store.ArtifactLogPrevious, RestartCount: 1, Previous: previous, Trigger: processor.TriggerRestart}
}

func diff(t *testing.T, want, got any) {
	t.Helper()
	if d := cmp.Diff(want, got, cmpopts.EquateEmpty()); d != "" {
		t.Fatal(d)
	}
}

func TestLogCaptureWritesFileThenRow(t *testing.T) {
	pods := schema.GroupResource{Resource: "pods"}
	notFound := apierrors.NewNotFound(pods, "pod-p1")
	badReq := apierrors.NewBadRequest("previous terminated container \"api\" in pod \"pod-p1\" not found")
	forbidden := apierrors.NewForbidden(pods, "pod-p1", errors.New("no"))
	kubelet := "unable to retrieve container logs for containerd://abc\n"
	rel := "1/idios-smoke/p1/api/restart_001.log"
	cases := []struct {
		name      string
		previous  bool
		body      string
		err       error
		wantFile  *string
		wantRow   store.Artifact
		fileAtRow bool
	}{
		{"content", true, "a\nb\n", nil, ptr("a\nb\n"), store.Artifact{FilePath: &rel, SizeBytes: 4}, true},
		{"tail plus one line drops the oldest", true, "l1\nl2\nl3\n", nil, ptr("l2\nl3\n"), store.Artifact{FilePath: &rel, SizeBytes: 6, Truncated: true}, true},
		{"max plus one byte drops the last", true, "123456789", nil, ptr("12345678"), store.Artifact{FilePath: &rel, SizeBytes: 8, Truncated: true}, true},
		{"no trailing newline counts as a line", true, "l1\nl2\nl3", nil, ptr("l2\nl3"), store.Artifact{FilePath: &rel, SizeBytes: 5, Truncated: true}, true},
		{"empty body", true, "", nil, nil, store.Artifact{CaptureGap: ptr(store.GapNoOutput)}, false},
		{"kubelet error with status 200", true, kubelet, nil, nil, store.Artifact{CaptureGap: ptr(store.GapKubeletError), CaptureNote: ptr(strings.TrimSuffix(kubelet, "\n"))}, false},
		{"not found", true, "", notFound, nil, store.Artifact{CaptureGap: ptr(store.GapPodDeleted), CaptureNote: ptr(notFound.Error())}, false},
		{"bad request on previous", true, "", badReq, nil, store.Artifact{CaptureGap: ptr(store.GapNoPreviousRun), CaptureNote: ptr(badReq.Error())}, false},
		{"bad request on current", false, "", badReq, nil, store.Artifact{CaptureGap: ptr(store.GapUnknown), CaptureNote: ptr(badReq.Error())}, false},
		{"forbidden", true, "", forbidden, nil, store.Artifact{CaptureGap: ptr(store.GapForbidden), CaptureNote: ptr(forbidden.Error())}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.seedPod(t, "p1", 1)
			h.src.respond = func(call) (string, error) { return c.body, c.err }
			h.cw.pendingCheck = rel
			r := previousRequest("p1", c.previous)

			h.p.process(context.Background(), h.src, r)

			diff(t, []call{{Namespace: "idios-smoke", Pod: "pod-p1", Opts: corev1.PodLogOptions{Container: "api", Previous: c.previous, TailLines: ptr(int64(testTail + 1)), LimitBytes: ptr(testBytes + 1)}}}, h.src.calls)
			diff(t, c.wantFile, h.file(t, rel))
			want := c.wantRow
			want.PodUID, want.ContainerName, want.Kind, want.RestartCount, want.CapturedAt = "p1", "api", store.ArtifactLogPrevious, 1, clock.Format(testNow)
			diff(t, []store.Artifact{want}, h.artifacts(t))
			diff(t, []bool{c.fileAtRow}, h.cw.fileAtRow)
		})
	}
}

func TestPodJSONCarriesTypeMetaAndIsOverwritten(t *testing.T) {
	h := newHarness(t)
	h.seedPod(t, "p1", 0)
	rel := "1/idios-smoke/p1/pod.json"
	req := func(pod *corev1.Pod) processor.CaptureRequest {
		return processor.CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: "p1", PodName: "pod-p1",
			Kind: store.ArtifactPodJSON, RestartCount: store.NoRestartIndex, Trigger: processor.TriggerRestart, Pod: pod}
	}
	first := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-p1", Namespace: "idios-smoke", UID: "p1"}}
	second := first.DeepCopy()
	second.Labels = map[string]string{"app": "web"}

	h.p.process(context.Background(), h.src, req(first))
	h.p.process(context.Background(), h.src, req(second))
	h.p.process(context.Background(), h.src, req(nil))

	var got corev1.Pod
	data := h.file(t, rel)
	if data == nil {
		t.Fatal("pod.json missing")
	}
	if err := json.Unmarshal([]byte(*data), &got); err != nil {
		t.Fatal(err)
	}
	want := second.DeepCopy()
	want.TypeMeta = metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}
	diff(t, want, &got)
	diff(t, []store.Artifact{{PodUID: "p1", Kind: store.ArtifactPodJSON, RestartCount: store.NoRestartIndex, FilePath: &rel, SizeBytes: int64(len(*data)), CapturedAt: clock.Format(testNow)}}, h.artifacts(t))
	if h.src.count() != 0 {
		t.Errorf("pod_json made %d log calls", h.src.count())
	}
}

func TestDeleteAndRestartUseEarlyCopyOnlyForPodsWorthKeeping(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "pod-p1")
	cases := []struct {
		name          string
		exitCode      int64
		incident      bool
		trigger       string
		kind          string
		fetch         string
		fetchErr      error
		failKeepCheck bool
		wantFile      *string
		wantRow       store.Artifact
		wantHeld      bool
	}{
		{"a failed keep check keeps the copy for the next request", 1, true, processor.TriggerDelete, store.ArtifactLogCurrent, "", notFound, true,
			nil, store.Artifact{CaptureGap: ptr(store.GapPodDeleted), CaptureNote: ptr(notFound.Error())}, true},
		{"delete of a pod with an incident writes the copy", 0, true, processor.TriggerDelete, store.ArtifactLogCurrent, "", notFound, false,
			ptr("early\n"), store.Artifact{CapturedEarly: true, SizeBytes: 6}, false},
		{"delete of a healthy pod drops the copy", 0, false, processor.TriggerDelete, store.ArtifactLogCurrent, "", notFound, false,
			nil, store.Artifact{CaptureGap: ptr(store.GapPodDeleted), CaptureNote: ptr(notFound.Error())}, false},
		{"restart with a failed exit writes the copy", 1, false, processor.TriggerRestart, store.ArtifactLogPrevious, "", nil, false,
			ptr("early\n"), store.Artifact{CapturedEarly: true, SizeBytes: 6}, false},
		{"restart whose own fetch worked leaves the copy", 1, false, processor.TriggerRestart, store.ArtifactLogPrevious, "live\n", nil, false,
			ptr("live\n"), store.Artifact{SizeBytes: 5}, true},
		{"incident open never consults the cache", 1, true, processor.TriggerIncidentOpen, store.ArtifactLogCurrent, "", notFound, false,
			nil, store.Artifact{CaptureGap: ptr(store.GapPodDeleted), CaptureNote: ptr(notFound.Error())}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.seedPod(t, "p1", c.exitCode)
			var incidentID *int64
			if c.incident {
				incidentID = ptr(h.seedIncident(t, "p1"))
			}
			h.p.cache.put("p1", "api", "Killing", []byte("early\n"), false, testNow)
			h.cw.failFirstTx = c.failKeepCheck
			h.src.respond = func(call) (string, error) { return c.fetch, c.fetchErr }
			r := processor.CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: "p1", PodName: "pod-p1", Container: "api",
				Kind: c.kind, RestartCount: store.NoRestartIndex, Trigger: c.trigger, IncidentID: incidentID}
			rel := "1/idios-smoke/p1/api/current.log"
			if c.kind == store.ArtifactLogPrevious {
				r.RestartCount, r.Previous = 1, true
				rel = "1/idios-smoke/p1/api/restart_001.log"
			}

			h.p.process(context.Background(), h.src, r)

			diff(t, c.wantFile, h.file(t, rel))
			want := c.wantRow
			want.PodUID, want.ContainerName, want.Kind, want.RestartCount, want.IncidentID, want.CapturedAt = "p1", "api", c.kind, r.RestartCount, incidentID, clock.Format(testNow)
			if want.CaptureGap == nil {
				want.FilePath = &rel
			}
			diff(t, []store.Artifact{want}, h.artifacts(t))
			if _, held := h.p.cache.take("p1", "api"); held != c.wantHeld {
				t.Errorf("copy still held = %v, want %v", held, c.wantHeld)
			}
		})
	}
}

// captured_early states that the early copy is what the file holds, so a
// write that never landed must not carry the mark.
func TestAFailedWriteOfTheEarlyCopyIsNotMarkedEarly(t *testing.T) {
	h := newHarness(t)
	h.seedPod(t, "p1", 1)
	// A regular file where the root's parent belongs makes every mkdir
	// beneath it fail, which is the first step of the write.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h.p.cfg.Root = filepath.Join(blocker, "artifacts")
	wantErr := os.MkdirAll(filepath.Join(h.p.cfg.Root, "1", "idios-smoke", "p1", "api"), 0o700)
	if wantErr == nil {
		t.Fatal("mkdir under a regular file succeeded")
	}
	h.p.cache.put("p1", "api", "Killing", []byte("early\n"), false, testNow)
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "pod-p1")
	h.src.respond = func(call) (string, error) { return "", notFound }
	r := processor.CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: "p1", PodName: "pod-p1", Container: "api",
		Kind: store.ArtifactLogCurrent, RestartCount: store.NoRestartIndex, Trigger: processor.TriggerDelete}

	h.p.process(context.Background(), h.src, r)

	diff(t, []store.Artifact{{PodUID: "p1", ContainerName: "api", Kind: store.ArtifactLogCurrent, RestartCount: store.NoRestartIndex,
		CaptureGap: ptr(store.GapUnknown), CaptureNote: ptr(wantErr.Error()), CapturedAt: clock.Format(testNow)}}, h.artifacts(t))
}

func TestEarlyRequestFillsCacheWithoutARow(t *testing.T) {
	h := newHarness(t)
	h.seedPod(t, "p1", 0)
	early := processor.CaptureRequest{ClusterID: 1, Namespace: "idios-smoke", PodUID: "p1", PodName: "pod-p1", Container: "api",
		Kind: store.ArtifactLogCurrent, RestartCount: store.NoRestartIndex, Trigger: processor.TriggerEarlyPrefix + "Killing"}

	h.src.respond = func(call) (string, error) { return "", apierrors.NewBadRequest("container api is waiting to start") }
	h.p.process(context.Background(), h.src, early)
	if _, ok := h.p.cache.take("p1", "api"); ok {
		t.Fatal("a failed early fetch left a copy")
	}

	h.src.respond = func(call) (string, error) { return "l1\nl2\nl3\n", nil }
	h.p.process(context.Background(), h.src, early)

	diff(t, []call{
		{Namespace: "idios-smoke", Pod: "pod-p1", Opts: corev1.PodLogOptions{Container: "api", TailLines: ptr(int64(testTail + 1)), LimitBytes: ptr(testBytes + 1)}},
		{Namespace: "idios-smoke", Pod: "pod-p1", Opts: corev1.PodLogOptions{Container: "api", TailLines: ptr(int64(testTail + 1)), LimitBytes: ptr(testBytes + 1)}},
	}, h.src.calls)
	got, ok := h.p.cache.take("p1", "api")
	if !ok {
		t.Fatal("early copy missing")
	}
	if d := cmp.Diff(held{body: []byte("l2\nl3\n"), truncated: true, at: testNow, final: true}, got, cmp.AllowUnexported(held{})); d != "" {
		t.Fatal(d)
	}
	diff(t, []store.Artifact(nil), h.artifacts(t))
	if h.file(t, "1/idios-smoke/p1/api/current.log") != nil {
		t.Fatal("an early capture wrote a file")
	}
}
