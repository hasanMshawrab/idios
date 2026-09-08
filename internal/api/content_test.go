package api

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// The seeded artifacts that name a file, and what those files hold once a
// test writes them.
const (
	podJSONPath = "prod/pod-crash/pod.json"
	podJSONBody = `{"kind":"Pod","metadata":{"name":"web-7d9f8c6b5-abcde"}}`
	logPath     = "prod/pod-crash/api-0.log"
	logBody     = "panic: runtime error: invalid memory address\n"
)

// writeArtifactFile puts body where the seeded row says the capture left it.
func writeArtifactFile(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The daemon serves the captured file as stored, typed by the artifact's
// kind and sized from the file itself, so the application never opens the
// artifacts directory.
func TestArtifactContentTypesAndLength(t *testing.T) {
	stack := newTestStack(t)
	writeArtifactFile(t, stack.artifactsRoot, podJSONPath, podJSONBody)
	writeArtifactFile(t, stack.artifactsRoot, logPath, logBody)
	cases := []struct {
		name            string
		id              int64
		wantContentType string
		wantBody        string
	}{
		{"a captured pod object", 2, "application/json", podJSONBody},
		{"a captured log", 3, "text/plain; charset=utf-8", logBody},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, header, body := get(t, stack.url, contentPath(c.id))
			if code != http.StatusOK {
				t.Fatalf("status %d, want %d: %s", code, http.StatusOK, body)
			}
			if got := header.Get("Content-Type"); got != c.wantContentType {
				t.Errorf("content type %q, want %q", got, c.wantContentType)
			}
			if got, want := header.Get("Content-Length"), strconv.Itoa(len(c.wantBody)); got != want {
				t.Errorf("content length %q, want %q", got, want)
			}
			if got := string(body); got != c.wantBody {
				t.Errorf("body %q, want %q", got, c.wantBody)
			}
		})
	}
}

// A capture that left no file is a 404 carrying the gap and the note, so the
// reader is told why the pane is empty instead of being shown nothing.
func TestArtifactContentGapIs404WithGapBody(t *testing.T) {
	stack := newTestStack(t)
	cases := []struct {
		name string
		id   int64
		want string
	}{
		{"a gap with a note", 4, `{"message":"no log: no_output: container produced no output"}`},
		{"a gap with no note", 1, `{"message":"no log: unobservable"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, header, body := get(t, stack.url, contentPath(c.id))
			if code != http.StatusNotFound {
				t.Fatalf("status %d, want %d: %s", code, http.StatusNotFound, body)
			}
			if got := header.Get("Content-Type"); got != "application/json" {
				t.Errorf("content type %q, want application/json", got)
			}
			if got := string(compact(t, body)); got != c.want {
				t.Errorf("body %s, want %s", got, c.want)
			}
		})
	}
}

// The directory is the daemon's: a stored path that climbs out of the
// artifacts root is refused rather than followed, whatever it points at.
func TestArtifactContentRefusesPathOutsideRoot(t *testing.T) {
	stack := newTestStack(t)
	writeArtifactFile(t, stack.artifactsRoot, podJSONPath, podJSONBody)
	cases := []struct {
		name     string
		filePath string
	}{
		{"a path that climbs out", "../idios.db"},
		{"an absolute path", "/etc/hosts"},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The unique index is (pod_uid, container_name, kind,
			// restart_count), so each row needs an instance of its own.
			id := insertArtifact(t, stack, store.Artifact{
				PodUID: querytest.CrashPodUID, ContainerName: "api", Kind: store.ArtifactLogPrevious,
				RestartCount: int64(90 + i), FilePath: sp(c.filePath), SizeBytes: 1,
				CapturedAt: querytest.ArtifactLogAt,
			})
			code, _, body := get(t, stack.url, contentPath(id))
			if code != http.StatusNotFound {
				t.Fatalf("status %d, want %d: %s", code, http.StatusNotFound, body)
			}
			want := `{"message":"artifact ` + strconv.FormatInt(id, 10) + ` not found"}`
			if got := string(compact(t, body)); got != want {
				t.Errorf("body %s, want %s", got, want)
			}
		})
	}
}

// Every other way of having no bytes to send says which one it was: an id
// nobody stored, a row whose file the sweeper already removed, and an id that
// is not a number at all.
func TestArtifactContentWithoutBytesSaysWhy(t *testing.T) {
	stack := newTestStack(t)
	// An empty file path passes the schema check, which only ties a null path
	// to a gap, and joins to the root directory itself.
	directory := insertArtifact(t, stack, store.Artifact{
		PodUID: querytest.CrashPodUID, ContainerName: "api", Kind: store.ArtifactLogPrevious,
		RestartCount: 92, FilePath: sp(""), CapturedAt: querytest.ArtifactLogAt,
	})
	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"an unknown id", contentPath(999999), http.StatusNotFound, `{"message":"artifact 999999 not found"}`},
		{"a row whose file is gone", contentPath(3), http.StatusNotFound, `{"message":"artifact file missing"}`},
		{
			"a path that is not a file", contentPath(directory), http.StatusNotFound,
			`{"message":"artifact file missing"}`,
		},
		{
			"an id that is not a number", "/v1/artifacts/abc/content", http.StatusBadRequest,
			`{"violations":[{"field":"id","description":"artifact id must be a number, got abc"}]}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, body := get(t, stack.url, c.path)
			if code != c.wantStatus {
				t.Fatalf("status %d, want %d: %s", code, c.wantStatus, body)
			}
			if got := string(compact(t, body)); got != c.wantBody {
				t.Errorf("body %s, want %s", got, c.wantBody)
			}
		})
	}
}

// The row endpoint answers the artifact the application shows beside the
// file, and a 404 for an id the database does not hold.
func TestGetArtifactReturnsTheRow(t *testing.T) {
	stack := newTestStack(t)
	client := idiosv1.NewIdiosServiceClient(stack.url)
	want := crashArtifactsWire()[1]
	got, err := client.GetArtifact(context.Background(), &idiosv1.GetArtifactRequest{Id: want.GetId()})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
	code, _, body := get(t, stack.url, "/v1/artifacts/999999")
	if code != http.StatusNotFound {
		t.Fatalf("status %d, want %d: %s", code, http.StatusNotFound, body)
	}
	if got, want := string(compact(t, body)), `{"message":"artifact 999999 not found"}`; got != want {
		t.Errorf("body %s, want %s", got, want)
	}
}

func contentPath(id int64) string {
	return "/v1/artifacts/" + strconv.FormatInt(id, 10) + "/content"
}

// insertArtifact writes a row the seed does not hold and returns the id the
// database gave it, which the insert does not report.
func insertArtifact(t *testing.T, stack testStack, a store.Artifact) int64 {
	t.Helper()
	ctx := context.Background()
	if err := stack.store.Writer.Tx(ctx, func(tx *sql.Tx) error {
		return store.UpsertArtifact(ctx, tx, a)
	}); err != nil {
		t.Fatal(err)
	}
	for id := int64(1); id <= 20; id++ {
		got, err := query.GetArtifact(ctx, stack.store.Reader.DB(), id)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil && got.PodUID == a.PodUID && got.Kind == a.Kind && got.RestartCount == a.RestartCount {
			return id
		}
	}
	t.Fatalf("no artifact for instance %d of %s", a.RestartCount, a.Kind)
	return 0
}
