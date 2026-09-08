package capture

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/processor"
	"github.com/hasanMshawrab/idios/internal/store"
)

const tmpDir = "tmp"

// LimitBytes makes the server bound the body, so the local read bound only
// takes effect for a source that does not honor it. Kept above one kubelet
// error line so such a source cannot cut the body down to a fragment that
// the recognizer misses and the fragment gets stored as output; 4096 is
// comfortably above the longest of those lines, a containerd id plus a pod
// log path.
const minReadBytes = 4096

// The API server answers 200 with one of these lines as the whole body when
// the kubelet cannot serve the log; stored as is it would read as output.
var kubeletErrorPrefixes = []string{
	"unable to retrieve container logs for",
	"failed to try resolving symlinks",
}

// fetched is the outcome of one log read: a body, or the gap and note that
// explain its absence.
type fetched struct {
	body      []byte
	truncated bool
	gap       *string
	note      *string
}

func (p *Pool) process(ctx context.Context, src LogSource, r processor.CaptureRequest) {
	switch {
	case r.Kind == store.ArtifactPodJSON:
		p.capturePodJSON(ctx, r)
	case strings.HasPrefix(r.Trigger, processor.TriggerEarlyPrefix):
		p.captureEarly(ctx, src, r)
	default:
		p.captureLog(ctx, src, r)
	}
}

// fetch asks for one line and one byte more than the caps so that hitting a
// cap is known exactly rather than inferred from a count that equals it.
func (p *Pool) fetch(ctx context.Context, src LogSource, r processor.CaptureRequest) fetched {
	opts := &corev1.PodLogOptions{Container: r.Container, Previous: r.Previous, TailLines: ptr(int64(p.cfg.TailLines) + 1), LimitBytes: ptr(p.cfg.MaxBytes + 1)}
	rc, err := src.Logs(ctx, r.Namespace, r.PodName, opts)
	if err != nil {
		return gapFor(err, r.Previous)
	}
	defer func() { _ = rc.Close() }()
	limit := p.cfg.MaxBytes + 1
	if limit < minReadBytes {
		limit = minReadBytes
	}
	body, err := io.ReadAll(io.LimitReader(rc, limit))
	if err != nil {
		return fetched{gap: ptr(store.GapUnknown), note: ptr(err.Error())}
	}
	return trim(body, p.cfg.TailLines, p.cfg.MaxBytes)
}

// gapFor maps a GetLogs error. A 400 on previous=true means no dead
// instance exists; a 400 on the current instance (a waiting container has
// none) has no name of its own.
func gapFor(err error, previous bool) fetched {
	gap := store.GapUnknown
	switch {
	case apierrors.IsNotFound(err):
		gap = store.GapPodDeleted
	case apierrors.IsBadRequest(err) && previous:
		gap = store.GapNoPreviousRun
	case apierrors.IsForbidden(err):
		gap = store.GapForbidden
	}
	return fetched{gap: ptr(gap), note: ptr(err.Error())}
}

// trim applies the line cap before the byte cap, so that whole lines are
// kept where they fit: TailLines keeps the newest lines, so the extra line
// is the first one; LimitBytes stops the stream, so anything still over the
// byte cap is cut from the end.
func trim(body []byte, tail int, maxBytes int64) fetched {
	if len(body) == 0 {
		return fetched{gap: ptr(store.GapNoOutput)}
	}
	if line, ok := kubeletError(body); ok {
		return fetched{gap: ptr(store.GapKubeletError), note: ptr(line)}
	}
	var truncated bool
	if lines(body) > tail {
		body, truncated = body[bytes.IndexByte(body, '\n')+1:], true
	}
	if int64(len(body)) > maxBytes {
		body, truncated = body[:maxBytes], true
	}
	return fetched{body: body, truncated: truncated}
}

func lines(b []byte) int {
	n := bytes.Count(b, []byte{'\n'})
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n
}

func kubeletError(body []byte) (string, bool) {
	line := strings.TrimSuffix(string(body), "\n")
	if strings.Contains(line, "\n") {
		return "", false
	}
	for _, prefix := range kubeletErrorPrefixes {
		if strings.HasPrefix(line, prefix) {
			return line, true
		}
	}
	return "", false
}

// relPath is the file's path under the root, with forward slashes so the
// stored value reads the same on every platform.
func relPath(r processor.CaptureRequest) string {
	base := path.Join(strconv.FormatInt(r.ClusterID, 10), r.Namespace, r.PodUID)
	switch r.Kind {
	case store.ArtifactPodJSON:
		return path.Join(base, "pod.json")
	case store.ArtifactLogCurrent:
		return path.Join(base, r.Container, "current.log")
	default:
		return path.Join(base, r.Container, fmt.Sprintf("restart_%03d.log", r.RestartCount))
	}
}

// writeFile lands data at rel through a temp file in the same tree, so a
// reader never sees a partial file and a crash leaves only an orphan.
func (p *Pool) writeFile(rel string, data []byte) error {
	dst := filepath.Join(p.cfg.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(p.cfg.Root, tmpDir)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(tmp, "capture-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// captureLog fetches the log, falls back to the early copy when the fetch
// got nothing and the pod is worth keeping, writes the file and then the
// row. A row is written for every attempt so that a miss is recorded.
func (p *Pool) captureLog(ctx context.Context, src LogSource, r processor.CaptureRequest) {
	f := p.fetch(ctx, src, r)
	early := false
	if f.gap != nil && (r.Trigger == processor.TriggerDelete || r.Kind == store.ArtifactLogPrevious) {
		if h, ok := p.cache.take(r.PodUID, r.Container); ok {
			keep, err := p.worthKeeping(ctx, r.PodUID)
			switch {
			case err != nil:
				// The copy may be the only one that will ever exist, so a
				// transient database error must not spend it; the next
				// request for this container can still use it.
				p.cache.restore(r.PodUID, r.Container, h)
				p.log.Error("early copy: keep check", "pod", r.PodName, "uid", r.PodUID, "err", err)
			case keep:
				f, early = fetched{body: h.body, truncated: h.truncated}, true
			}
		}
	}
	a := store.Artifact{PodUID: r.PodUID, IncidentID: r.IncidentID, ContainerName: r.Container, Kind: r.Kind, RestartCount: r.RestartCount,
		CapturedAt: clock.Format(p.clk.Now())}
	if f.gap != nil {
		a.CaptureGap, a.CaptureNote = f.gap, f.note
	} else {
		rel := relPath(r)
		if err := p.writeFile(rel, f.body); err != nil {
			a.CaptureGap, a.CaptureNote = ptr(store.GapUnknown), ptr(err.Error())
		} else {
			a.FilePath, a.SizeBytes, a.Truncated, a.CapturedEarly = &rel, int64(len(f.body)), f.truncated, early
		}
	}
	p.writeRow(ctx, a)
}

// capturePodJSON writes the pod the request carries. Informers hand out
// objects with an empty TypeMeta, so it is set before marshaling.
func (p *Pool) capturePodJSON(ctx context.Context, r processor.CaptureRequest) {
	if r.Pod == nil {
		p.log.Error("pod_json request without a pod", "pod", r.PodName, "uid", r.PodUID)
		return
	}
	pod := r.Pod.DeepCopy()
	pod.TypeMeta = metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}
	a := store.Artifact{PodUID: r.PodUID, IncidentID: r.IncidentID, Kind: store.ArtifactPodJSON, RestartCount: store.NoRestartIndex, CapturedAt: clock.Format(p.clk.Now())}
	data, err := json.Marshal(pod)
	if err == nil {
		rel := relPath(r)
		if err = p.writeFile(rel, data); err == nil {
			a.FilePath, a.SizeBytes = &rel, int64(len(data))
		}
	}
	if err != nil {
		a.CaptureGap, a.CaptureNote = ptr(store.GapUnknown), ptr(err.Error())
	}
	p.writeRow(ctx, a)
}

// captureEarly takes a live copy into the cache when the debounce and
// once-per-container rules allow it. Nothing is written to disk or to the
// database: the copy only becomes an artifact if a later capture needs it.
func (p *Pool) captureEarly(ctx context.Context, src LogSource, r processor.CaptureRequest) {
	reason := strings.TrimPrefix(r.Trigger, processor.TriggerEarlyPrefix)
	now := p.clk.Now()
	if !p.cache.reserve(r.PodUID, r.Container, reason, now) {
		return
	}
	r.Previous = false
	f := p.fetch(ctx, src, r)
	if f.gap != nil {
		p.cache.release(r.PodUID, r.Container)
		return
	}
	p.cache.put(r.PodUID, r.Container, reason, f.body, f.truncated, now)
}

func (p *Pool) writeRow(ctx context.Context, a store.Artifact) {
	err := p.w.Tx(ctx, func(tx *sql.Tx) error { return store.UpsertArtifact(ctx, tx, a) })
	if err != nil {
		p.log.Error("artifact row", "uid", a.PodUID, "container", a.ContainerName, "kind", a.Kind, "restart", a.RestartCount, "err", err)
		return
	}
	p.countCompleted(a)
}

// worthKeeping is the gate on early copies: an incident open or closed
// within the stabilization window, or a container that exited non-zero.
func (p *Pool) worthKeeping(ctx context.Context, podUID string) (bool, error) {
	var keep bool
	since := clock.Format(p.clk.Now().Add(-p.cfg.StabilizationWindow))
	err := p.w.Tx(ctx, func(tx *sql.Tx) (err error) {
		keep, err = store.HasRecentIncidentOrFailure(ctx, tx, podUID, since)
		return err
	})
	return keep, err
}
