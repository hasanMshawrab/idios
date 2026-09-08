package api

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	sebufhttp "github.com/SebastienMelki/sebuf/http"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/hasanMshawrab/idios/internal/query"
	"github.com/hasanMshawrab/idios/internal/store"
)

// artifactContentPattern is the one endpoint outside the proto contract: raw
// bytes cannot be a proto response, so the file is served by a plain handler
// on the same mux.
const artifactContentPattern = "GET /v1/artifacts/{id}/content"

// artifactContent serves the captured file of one artifact. Everything that
// leaves the reader without bytes is a 404 carrying the reason, because an
// empty pane is indistinguishable from a log that was empty.
func (s *Server) artifactContent(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, &sebufhttp.ValidationError{
			Violations: []*sebufhttp.FieldViolation{
				{Field: "id", Description: "artifact id must be a number, got " + raw},
			},
		})
		return
	}
	missing := "artifact " + strconv.FormatInt(id, 10) + " not found"
	a, err := query.GetArtifact(r.Context(), s.db, id)
	if err != nil {
		s.log.Error("artifact content failed", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, &sebufhttp.Error{Message: "internal error"})
		return
	}
	if a == nil {
		writeJSON(w, http.StatusNotFound, &sebufhttp.Error{Message: missing})
		return
	}
	if a.FilePath == nil {
		writeJSON(w, http.StatusNotFound, &sebufhttp.Error{Message: gapMessage(*a)})
		return
	}
	path, ok := underRoot(s.cfg.ArtifactsRoot, *a.FilePath)
	if !ok {
		// The row is the daemon's own; a path that climbs out of the root is
		// a corrupted row or a tampered database, and either way it is not
		// what the reader asked for.
		s.log.Error("artifact path escapes the artifacts root", "id", id, "file_path", *a.FilePath)
		writeJSON(w, http.StatusNotFound, &sebufhttp.Error{Message: missing})
		return
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// The sweeper and the row disagree, which the reader must see.
			s.log.Error("artifact file missing", "id", id, "path", path)
			writeJSON(w, http.StatusNotFound, &sebufhttp.Error{Message: "artifact file missing"})
			return
		}
		s.log.Error("artifact content failed", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, &sebufhttp.Error{Message: "internal error"})
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		s.log.Error("artifact content failed", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, &sebufhttp.Error{Message: "internal error"})
		return
	}
	if !info.Mode().IsRegular() {
		// A directory or a device opens without error and has no bytes to
		// send; the row and the tree disagree as surely as a removed file.
		s.log.Error("artifact path is not a file", "id", id, "path", path)
		writeJSON(w, http.StatusNotFound, &sebufhttp.Error{Message: "artifact file missing"})
		return
	}
	w.Header().Set("Content-Type", contentType(a.Kind))
	// The stored size_bytes is what was captured; the file on disk is what is
	// sent, so the length comes from the file.
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if _, err := io.Copy(w, f); err != nil {
		s.log.Error("artifact content failed", "id", id, "err", err)
	}
}

// gapMessage says why there is no file, quoting the note when the capture
// left one. A row without a file always names a gap: the schema requires one
// exactly when file_path is null.
func gapMessage(a store.Artifact) string {
	message := "no log: " + *a.CaptureGap
	if a.CaptureNote != nil {
		message += ": " + *a.CaptureNote
	}
	return message
}

// underRoot joins a stored file path to the artifacts root and reports
// whether it stays inside it. An absolute path is refused rather than
// resolved: Join would quietly reroot it and serve a file the daemon never
// captured.
func underRoot(root, path string) (string, bool) {
	if filepath.IsAbs(path) {
		return "", false
	}
	full := filepath.Join(root, path)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

// contentType is what the artifact's kind holds: a captured pod object is
// JSON, and every other kind is log text.
func contentType(kind string) string {
	if kind == store.ArtifactPodJSON {
		return "application/json"
	}
	return "text/plain; charset=utf-8"
}

// writeJSON answers with code and the JSON form of msg. This endpoint is
// outside the contract and negotiates nothing: the application fetches it
// with a plain request and reads JSON on every failure.
func writeJSON(w http.ResponseWriter, code int, msg proto.Message) {
	body, err := protojson.Marshal(msg)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}
