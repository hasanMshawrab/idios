package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"

	sebufhttp "github.com/SebastienMelki/sebuf/http"

	"github.com/hasanMshawrab/idios/internal/prompt"
	"github.com/hasanMshawrab/idios/internal/query"
	"github.com/hasanMshawrab/idios/internal/sanitize"
	"github.com/hasanMshawrab/idios/internal/store"
)

// promptPattern is outside the proto contract for the reason the artifact
// content endpoint is: the body is text a person pastes into an agent, not a
// proto message.
const promptPattern = "GET /v1/incidents/{id}/prompt"

// incidentPrompt renders one incident as the text an AI agent is handed. The
// two modes differ in what travels with the prompt, never in what is asked of
// the answer.
func (s *Server) incidentPrompt(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, &sebufhttp.ValidationError{
			Violations: []*sebufhttp.FieldViolation{
				{Field: "id", Description: "incident id must be a number, got " + raw},
			},
		})
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode != prompt.ModeMCP && mode != prompt.ModeSnapshot {
		writeJSON(w, http.StatusBadRequest, &sebufhttp.ValidationError{
			Violations: []*sebufhttp.FieldViolation{
				{Field: "mode", Description: "mode must be " + prompt.ModeMCP + " or " + prompt.ModeSnapshot +
					", got " + strconv.Quote(mode)},
			},
		})
		return
	}
	d, err := query.GetIncident(r.Context(), s.db, id, query.Page{Limit: s.limit(0)})
	if err != nil {
		s.log.Error("incident prompt failed", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, &sebufhttp.Error{Message: "internal error"})
		return
	}
	if d == nil {
		writeJSON(w, http.StatusNotFound, &sebufhttp.Error{
			Message: (&notFoundError{what: "incident", id: raw}).Error()})
		return
	}
	in := prompt.Input{Detail: *d}
	if in.ClusterName, err = s.clusterName(r.Context(), d.Incident.ClusterID); err != nil {
		s.log.Error("incident prompt failed", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, &sebufhttp.Error{Message: "internal error"})
		return
	}
	text := prompt.MCP(in)
	if mode == prompt.ModeSnapshot {
		if err := s.snapshotEvidence(r.Context(), &in); err != nil {
			s.log.Error("incident prompt failed", "id", id, "err", err)
			writeJSON(w, http.StatusInternalServerError, &sebufhttp.Error{Message: "internal error"})
			return
		}
		text = prompt.Snapshot(in)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// The application says how large the prompt is before a person copies or
	// saves it, and it must not have to read the body to find out.
	w.Header().Set("Content-Length", strconv.Itoa(len(text)))
	if _, err := io.WriteString(w, text); err != nil {
		s.log.Error("incident prompt failed", "id", id, "err", err)
	}
}

// snapshotEvidence fills in everything the self-contained prompt carries
// beyond the detail. A file the row names and the tree no longer holds is a
// note in its own section, not a failed request: the rest of the evidence
// still answers the question.
func (s *Server) snapshotEvidence(ctx context.Context, in *prompt.Input) error {
	d := &in.Detail
	entries, err := query.IncidentTimeline(ctx, s.db, d.Incident.ID)
	if err != nil {
		return err
	}
	in.Timeline = entries
	// The detail already lists every capture of the pod, whichever of its
	// incidents each one attached to, so the snapshot reads the files from
	// there and composes no set of its own.
	for _, a := range d.Artifacts {
		if a.Kind == store.ArtifactPodJSON {
			in.PodJSON = s.podJSON(a)
			continue
		}
		content, missing := s.artifactBody(a)
		in.Logs = append(in.Logs, prompt.Log{Artifact: a, Content: content, Missing: missing})
	}
	return nil
}

// clusterName resolves the cluster an incident lives in to the name the MCP
// tools take; the numeric id the row carries means nothing to them.
func (s *Server) clusterName(ctx context.Context, id int64) (string, error) {
	clusters, err := query.ListClusters(ctx, s.db)
	if err != nil {
		return "", err
	}
	for _, c := range clusters {
		if c.ID == id {
			return c.Name, nil
		}
	}
	return "", nil
}

// podJSON is the captured pod object with its secret material stripped, or
// the reason the section has none. A capture that cannot be sanitized is
// withheld: serving it raw is exactly what the stripping exists to prevent.
func (s *Server) podJSON(a store.Artifact) string {
	content, missing := s.artifactBody(a)
	if missing != "" {
		return missing
	}
	clean, err := sanitize.PodJSON([]byte(content))
	if err != nil {
		s.log.Error("pod.json could not be sanitized", "artifact", a.ID, "err", err)
		return "The captured pod object is not readable as JSON, so it is withheld: " +
			"it cannot be stripped of its environment values."
	}
	return string(clean)
}

// artifactBody reads what a capture left, answering with the content or with
// the reason there is none.
func (s *Server) artifactBody(a store.Artifact) (content, missing string) {
	if a.FilePath == nil {
		return "", gapMessage(a)
	}
	path, ok := underRoot(s.cfg.ArtifactsRoot, *a.FilePath)
	if !ok {
		s.log.Error("artifact path escapes the artifacts root", "id", a.ID, "file_path", *a.FilePath)
		return "", "The captured file is not where the row says it is."
	}
	body, err := os.ReadFile(path)
	if err != nil {
		s.log.Error("artifact file unreadable", "id", a.ID, "path", path, "err", err)
		return "", "The captured file is gone: the row outlived it."
	}
	return string(body), ""
}
