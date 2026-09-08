package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hasanMshawrab/idios/internal/api"
	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/capture"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/status"
	"github.com/hasanMshawrab/idios/internal/store"
)

// snapshot assembles what only the running process knows; the status loop
// publishes it.
func snapshot(sup *supervisor, pool *capture.Pool, closer *incident.Closer, counters *status.Counters, now time.Time) status.Snapshot {
	snap := status.Snapshot{WrittenAt: clock.Format(now), PID: os.Getpid(), Version: version,
		Writer: counters.Writer(), Handlers: counters.Handlers()}
	for _, rc := range sup.list() {
		c := status.Cluster{ID: rc.id, Ready: rc.w.Ready(), SkewSeconds: rc.skew.Offset().Seconds()}
		if t, ok := counters.LastSeen(rc.id); ok {
			c.LastEventAt = clock.Format(t)
		}
		snap.Clusters = append(snap.Clusters, c)
	}
	ps := pool.Stats()
	snap.Capture = status.Capture{Queued: ps.Queued, Completed: ps.Completed, Dropped: ps.Dropped, Gaps: ps.Gaps}
	if at, r := closer.Last(); !at.IsZero() {
		t := closer.Totals()
		snap.Closer = status.Closer{LastTickAt: clock.Format(at), Closed: r.Closed, Attached: r.Attached, Opened: r.Opened,
			ClosedTotal: t.Closed, AttachedTotal: t.Attached, OpenedTotal: t.Opened}
	}
	return snap
}

// statusLoop writes the snapshot now and every status.Interval, and removes
// it when the process stops so a stale file is not mistaken for a live one.
func statusLoop(ctx context.Context, cfg config.Config, sup *supervisor, pool *capture.Pool, closer *incident.Closer, counters *status.Counters, log *slog.Logger) error {
	path := filepath.Join(cfg.DataDir, status.FileName)
	t := time.NewTicker(status.Interval)
	defer t.Stop()
	for {
		if err := status.WriteFile(path, snapshot(sup, pool, closer, counters, clock.Real{}.Now())); err != nil {
			log.Error("write status", "err", err)
		}
		select {
		case <-ctx.Done():
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Error("remove status", "err", err)
			}
			return ctx.Err()
		case <-t.C:
		}
	}
}

// report is everything idios status prints, gathered from the data
// directory, the snapshot and the database.
type report struct {
	DataDir string
	// APIAddr is set only when the report came from the daemon, which is how
	// an API with no recorder behind it is told from a dead data directory.
	APIAddr        string
	DBBytes        int64
	WALBytes       int64
	ArtifactFiles  int64
	ArtifactBytes  int64
	Snap           *status.Snapshot
	SnapAge        time.Duration
	Clusters       []store.Cluster
	OpenByCategory map[string]int64
	ClosedByReason map[string]int64
	Counts         store.RowCounts
	Gaps           map[string]int64
	Sweeps         []store.SweepRun
}

func collectReport(ctx context.Context, cfg config.Config, now time.Time) (report, error) {
	r := report{DataDir: cfg.DataDir}
	r.DBBytes = api.FileSize(cfg.DBPath())
	r.WALBytes = api.FileSize(cfg.DBPath() + "-wal")
	r.ArtifactFiles, r.ArtifactBytes = api.DirSize(cfg.ArtifactsRoot)
	snap, err := status.ReadFile(filepath.Join(cfg.DataDir, status.FileName))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return report{}, fmt.Errorf("read status file: %w", err)
	default:
		r.Snap = &snap
		if t, err := clock.Parse(snap.WrittenAt); err == nil {
			r.SnapAge = now.Sub(t)
		}
	}
	if _, err := os.Stat(cfg.DBPath()); errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return report{}, err
	}
	defer func() { _ = st.Close() }()
	db := st.Reader.DB()
	if r.Clusters, err = store.ListClusters(ctx, db); err != nil {
		return report{}, err
	}
	if r.OpenByCategory, err = store.OpenIncidentsByCategory(ctx, db); err != nil {
		return report{}, err
	}
	if r.ClosedByReason, err = store.ClosedIncidentsByReason(ctx, db); err != nil {
		return report{}, err
	}
	if r.Counts, err = store.CountRows(ctx, db); err != nil {
		return report{}, err
	}
	if r.Gaps, err = store.ArtifactsByOutcome(ctx, db); err != nil {
		return report{}, err
	}
	if r.Sweeps, err = store.LatestSweepRuns(ctx, db); err != nil {
		return report{}, err
	}
	return r, nil
}

func bytesString(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

func countList[V int64 | uint64](m map[string]V) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(parts, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func renderReport(w io.Writer, r report) {
	fmt.Fprintf(w, "data_dir   %s\n", r.DataDir)
	fmt.Fprintf(w, "database   %s, wal %s\n", bytesString(r.DBBytes), bytesString(r.WALBytes))
	fmt.Fprintf(w, "artifacts  %d files, %s\n", r.ArtifactFiles, bytesString(r.ArtifactBytes))
	switch {
	case r.Snap == nil && r.APIAddr != "":
		fmt.Fprintf(w, "daemon     api at %s answered, recorder not running\n", r.APIAddr)
	case r.Snap == nil:
		fmt.Fprintln(w, "daemon     not running (no status file)")
	case r.SnapAge > status.Stale:
		fmt.Fprintf(w, "daemon     stale, pid %d, snapshot %s old\n", r.Snap.PID, r.SnapAge.Round(time.Second))
	default:
		fmt.Fprintf(w, "daemon     running, pid %d, snapshot %s old\n", r.Snap.PID, r.SnapAge.Round(time.Second))
	}

	fmt.Fprintln(w, "\nclusters")
	if len(r.Clusters) == 0 {
		fmt.Fprintln(w, "  none")
	} else {
		live := map[int64]status.Cluster{}
		if r.Snap != nil {
			for _, c := range r.Snap.Clusters {
				live[c.ID] = c
			}
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  id\tname\tcontext\tready\tlast_event\tskew\tlast_error")
		for _, c := range r.Clusters {
			ready, lastEvent, skew := "-", "-", "-"
			if s, ok := live[c.ID]; ok {
				ready = "no"
				if s.Ready {
					ready = "yes"
				}
				lastEvent = orDash(s.LastEventAt)
				skew = (time.Duration(s.SkewSeconds * float64(time.Second))).Round(time.Second).String()
			}
			lastErr := "-"
			if c.LastError != nil {
				lastErr = *c.LastError
				if c.LastErrorAt != nil {
					lastErr += " (" + *c.LastErrorAt + ")"
				}
			}
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%s\t%s\n", c.ID, c.Name, c.ContextName, ready, lastEvent, skew, lastErr)
		}
		_ = tw.Flush()
	}

	fmt.Fprintln(w, "\nincidents")
	fmt.Fprintf(w, "  open    %s\n", countList(r.OpenByCategory))
	fmt.Fprintf(w, "  closed  %s\n", countList(r.ClosedByReason))
	fmt.Fprintln(w, "rows")
	fmt.Fprintf(w, "  pods %d (%d live), transitions %d, events %d\n", r.Counts.Pods, r.Counts.LivePods, r.Counts.Transitions, r.Counts.Events)
	fmt.Fprintf(w, "  artifacts: %s\n", countList(r.Gaps))

	if s := r.Snap; s != nil {
		fmt.Fprintf(w, "\nwriter    tx %d, errors %d, p99 %.1fms\n", s.Writer.Transactions, s.Writer.Errors, s.Writer.P99Ms)
		fmt.Fprintf(w, "handlers  errors %d, panics %d\n", s.Handlers.Errors, s.Handlers.Panics)
		fmt.Fprintf(w, "capture   queued %d, completed %d, dropped %d\n", s.Capture.Queued, s.Capture.Completed, s.Capture.Dropped)
		fmt.Fprintf(w, "closer    last tick %s, opened %d (%d total), closed %d (%d total), attached %d (%d total)\n",
			orDash(s.Closer.LastTickAt), s.Closer.Opened, s.Closer.OpenedTotal,
			s.Closer.Closed, s.Closer.ClosedTotal, s.Closer.Attached, s.Closer.AttachedTotal)
	}
	fmt.Fprintln(w, "sweep")
	if len(r.Sweeps) == 0 {
		fmt.Fprintln(w, "  none")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  table\tran_at\trows\tfiles\tbytes\terror")
	for _, sr := range r.Sweeps {
		e := "-"
		if sr.Error != nil {
			e = *sr.Error
		}
		fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t%d\t%s\n", sr.TableName, sr.RanAt, sr.RowsRemoved, sr.FilesRemoved, sr.BytesRemoved, e)
	}
	_ = tw.Flush()
}

// statusTimeout bounds the ask to the daemon. "The daemon is down, what is
// in the database" is a question status must still answer, so a listener
// that does not reply in time counts as down.
const statusTimeout = time.Second

// runStatus prints the report for the configured data directory, from the
// daemon when it answers and from the database when it does not.
func runStatus(ctx context.Context, cfg config.Config, stdout io.Writer) error {
	r, ok, err := reportFromDaemon(ctx, cfg, clock.Real{}.Now())
	if err != nil {
		return err
	}
	if ok {
		renderReport(stdout, r)
		return nil
	}
	_, _ = fmt.Fprintf(stdout, "daemon not reachable at %s; reading the database directly\n", cfg.APIListen)
	r, err = collectReport(ctx, cfg, clock.Real{}.Now())
	if err != nil {
		return err
	}
	renderReport(stdout, r)
	return nil
}

// daemonClient probes the daemon with GetStatus under statusTimeout and
// returns a client of it and the status it answered when something did.
// The third result is false when nothing did, which is not an error: it is
// the case the database path exists for.
func daemonClient(ctx context.Context, cfg config.Config) (idiosv1.IdiosServiceClient, *idiosv1.Status, bool) {
	c := idiosv1.NewIdiosServiceClient("http://" + cfg.APIListen)
	statusCtx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	s, err := c.GetStatus(statusCtx, &idiosv1.GetStatusRequest{})
	if err != nil {
		return nil, nil, false
	}
	return c, s, true
}

// reportFromDaemon asks the running daemon for what it prints. The second
// result is false when nothing answered, which is not an error: it is the
// case the database path exists for. A daemon that answered once and then
// failed is not that case, and its failure is returned.
func reportFromDaemon(ctx context.Context, cfg config.Config, now time.Time) (report, bool, error) {
	c, s, ok := daemonClient(ctx, cfg)
	if !ok {
		return report{}, false, nil
	}
	// The status message keys each watcher's runtime state by cluster id; the
	// names, contexts and last connection errors the table prints are stored
	// rows, and the cluster list is the endpoint that carries them.
	clustersCtx, cancelClusters := context.WithTimeout(ctx, statusTimeout)
	defer cancelClusters()
	list, err := c.ListClusters(clustersCtx, &idiosv1.ListClustersRequest{})
	if err != nil {
		return report{}, false, fmt.Errorf("daemon at %s answered status but not clusters: %w", cfg.APIListen, err)
	}
	return reportFromStatus(cfg, s, list.GetClusters(), now), true, nil
}

// reportFromStatus builds the printed report from the daemon's answer. It
// fills what renderReport prints and nothing else.
func reportFromStatus(cfg config.Config, s *idiosv1.Status, clusters []*idiosv1.Cluster, now time.Time) report {
	counts := s.GetRowCounts()
	r := report{
		DataDir: cfg.DataDir, APIAddr: cfg.APIListen, DBBytes: s.GetDbBytes(), WALBytes: s.GetWalBytes(),
		ArtifactFiles: int64(s.GetArtifactFiles()), ArtifactBytes: s.GetArtifactBytes(),
		OpenByCategory: map[string]int64{}, ClosedByReason: map[string]int64{}, Gaps: map[string]int64{},
		Counts: store.RowCounts{
			Pods: int64(counts.GetPods()), LivePods: int64(counts.GetLivePods()),
			Transitions: int64(counts.GetTransitions()), Events: int64(counts.GetEvents()),
		},
	}
	for _, c := range clusters {
		r.Clusters = append(r.Clusters, store.Cluster{
			ID: c.GetId(), Name: c.GetName(), ContextName: c.GetContextName(),
			LastError: c.LastError, LastErrorAt: c.LastErrorAt,
		})
	}
	for _, c := range s.GetOpenByCategory() {
		r.OpenByCategory[enumLabel(c.GetCategory())] = int64(c.GetCount())
	}
	for _, c := range s.GetClosedByReason() {
		r.ClosedByReason[enumLabel(c.GetCloseReason())] = int64(c.GetCount())
	}
	for _, c := range s.GetArtifactsByOutcome() {
		r.Gaps[c.GetOutcome()] = int64(c.GetCount())
	}
	for _, sr := range s.GetLatestSweepRuns() {
		r.Sweeps = append(r.Sweeps, store.SweepRun{
			ID: sr.GetId(), RanAt: sr.GetRanAt(), Cutoff: sr.GetCutoff(), TableName: sr.GetTableName(),
			RowsRemoved: int64(sr.GetRowsRemoved()), FilesRemoved: int64(sr.GetFilesRemoved()),
			BytesRemoved: sr.GetBytesRemoved(), DurationMs: int64(sr.GetDurationMs()), Error: sr.Error,
		})
	}
	if !s.GetDaemonRunning() {
		return r
	}
	snap := status.Snapshot{
		WrittenAt: s.GetWrittenAt(), PID: int(s.GetPid()), Version: s.GetVersion(),
		Writer: status.Writer{
			Transactions: uint64(s.GetWriter().GetTransactions()),
			Errors:       uint64(s.GetWriter().GetErrors()), P99Ms: s.GetWriter().GetP99Ms(),
		},
		Handlers: status.Handlers{
			Errors: uint64(s.GetHandlers().GetErrors()), Panics: uint64(s.GetHandlers().GetPanics()),
		},
		Capture: status.Capture{
			Queued: uint64(s.GetCapture().GetQueued()), Completed: uint64(s.GetCapture().GetCompleted()),
			Dropped: uint64(s.GetCapture().GetDropped()),
		},
		Closer: status.Closer{
			LastTickAt: s.GetCloser().GetLastTickAt(),
			Closed:     int64(s.GetCloser().GetClosed()), Attached: int64(s.GetCloser().GetAttached()),
			Opened:        int64(s.GetCloser().GetOpened()),
			ClosedTotal:   int64(s.GetCloser().GetClosedTotal()),
			AttachedTotal: int64(s.GetCloser().GetAttachedTotal()),
			OpenedTotal:   int64(s.GetCloser().GetOpenedTotal()),
		},
	}
	for _, c := range s.GetClusters() {
		snap.Clusters = append(snap.Clusters, status.Cluster{
			ID: c.GetId(), Ready: c.GetReady(), LastEventAt: c.GetLastEventAt(), SkewSeconds: c.GetSkewSeconds(),
		})
	}
	r.Snap = &snap
	if t, err := clock.Parse(snap.WrittenAt); err == nil {
		r.SnapAge = now.Sub(t)
	}
	return r
}

// enumLabel is the stored string of a wire enum. The generated marshaler
// holds the only mapping back to the vocabulary the database and the report
// use, and it is not exported as a table.
func enumLabel(v json.Marshaler) string {
	raw, err := v.MarshalJSON()
	if err != nil {
		return ""
	}
	label, err := strconv.Unquote(string(raw))
	if err != nil {
		return ""
	}
	return label
}
