package api

import (
	"cmp"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/status"
	"github.com/hasanMshawrab/idios/internal/store"
)

// FileSize is the size of the file at path, or zero when it is not there.
func FileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// DirSize is how many files live under root and how many bytes they hold. A
// directory that cannot be walked counts as empty: the number is a report,
// and a failure to read it must not fail the report.
func DirSize(root string) (files, bytes int64) {
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			files++
			bytes += fi.Size()
		}
		return nil
	})
	return files, bytes
}

// GetStatus answers what idios status prints.
func (s *Server) GetStatus(ctx context.Context, _ *idiosv1.GetStatusRequest) (*idiosv1.Status, error) {
	var snap status.Snapshot
	running := s.runtime != nil
	if running {
		snap = s.runtime.Snapshot()
	}
	files, bytes := DirSize(s.cfg.ArtifactsRoot)
	// The snapshot is assembled while answering, so the time it was written
	// and the time it describes are the same one. The status file's age, which
	// says whether the process behind it is still alive, has no equivalent
	// here: an answer means it is.
	out := &idiosv1.Status{
		DaemonRunning: running,
		WrittenAt:     clock.Format(s.clk.Now()),
		Pid:           int32(snap.PID),
		Version:       snap.Version,
		Clusters:      mapAll(snap.Clusters, statusCluster),
		Writer: &idiosv1.WriterStats{
			Transactions: int32(snap.Writer.Transactions), Errors: int32(snap.Writer.Errors), P99Ms: snap.Writer.P99Ms,
		},
		Handlers: &idiosv1.HandlerStats{Errors: int32(snap.Handlers.Errors), Panics: int32(snap.Handlers.Panics)},
		Capture: &idiosv1.CaptureStats{
			Queued: int32(snap.Capture.Queued), Completed: int32(snap.Capture.Completed),
			Dropped: int32(snap.Capture.Dropped), Gaps: gapCounts(snap.Capture.Gaps),
		},
		Closer:                            closerStats(snap.Closer),
		DbBytes:                           FileSize(s.cfg.DBPath()),
		WalBytes:                          FileSize(s.cfg.DBPath() + "-wal"),
		ArtifactFiles:                     int32(files),
		ArtifactBytes:                     bytes,
		RetentionDays:                     int32(s.cfg.RetentionDays),
		SweepIntervalSeconds:              seconds(s.cfg.SweepInterval),
		StabilizationWindowSeconds:        seconds(s.cfg.StabilizationWindow),
		StabilizationCheckIntervalSeconds: seconds(s.cfg.StabilizationCheckInterval),
		EarlyCaptureDebounceSeconds:       seconds(s.cfg.EarlyCaptureDebounce),
		ApiStreamThrottleSeconds:          seconds(s.cfg.APIStreamThrottle),
		SchedulingGraceSeconds:            seconds(s.cfg.SchedulingGrace),
		ProbeGraceSeconds:                 seconds(s.cfg.ProbeGrace),
		StuckAfterSeconds:                 seconds(s.cfg.StuckAfter),
		AttentionWindowSeconds:            seconds(s.cfg.AttentionWindow),
	}
	if err := s.statusAggregates(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// statusAggregates fills the counts the database holds.
func (s *Server) statusAggregates(ctx context.Context, out *idiosv1.Status) error {
	openByCategory, err := store.OpenIncidentsByCategory(ctx, s.db)
	if err != nil {
		return err
	}
	for category, n := range openByCategory {
		out.OpenByCategory = append(out.OpenByCategory,
			&idiosv1.CategoryCount{Category: categories[category], Count: int32(n)})
	}
	slices.SortFunc(out.OpenByCategory, func(a, b *idiosv1.CategoryCount) int {
		return cmp.Compare(a.GetCategory(), b.GetCategory())
	})
	closedByReason, err := store.ClosedIncidentsByReason(ctx, s.db)
	if err != nil {
		return err
	}
	for reason, n := range closedByReason {
		out.ClosedByReason = append(out.ClosedByReason,
			&idiosv1.CloseReasonCount{CloseReason: closeReasons[reason], Count: int32(n)})
	}
	slices.SortFunc(out.ClosedByReason, func(a, b *idiosv1.CloseReasonCount) int {
		return cmp.Compare(a.GetCloseReason(), b.GetCloseReason())
	})
	// The outcome of a capture is a gap name or "file", which is not a wire
	// enum, so the order is the name's.
	byOutcome, err := store.ArtifactsByOutcome(ctx, s.db)
	if err != nil {
		return err
	}
	for outcome, n := range byOutcome {
		out.ArtifactsByOutcome = append(out.ArtifactsByOutcome, &idiosv1.OutcomeCount{Outcome: outcome, Count: int32(n)})
	}
	slices.SortFunc(out.ArtifactsByOutcome, func(a, b *idiosv1.OutcomeCount) int {
		return cmp.Compare(a.GetOutcome(), b.GetOutcome())
	})
	counts, err := store.CountRows(ctx, s.db)
	if err != nil {
		return err
	}
	out.RowCounts = &idiosv1.RowCounts{
		Pods: int32(counts.Pods), LivePods: int32(counts.LivePods),
		Transitions: int32(counts.Transitions), Events: int32(counts.Events),
	}
	sweeps, err := store.LatestSweepRuns(ctx, s.db)
	if err != nil {
		return err
	}
	out.LatestSweepRuns = mapAll(sweeps, sweepRun)
	return nil
}

// seconds narrows a configured interval to the whole seconds the wire holds.
func seconds(d time.Duration) int32 {
	return int32(d / time.Second)
}

// statusCluster maps one watcher's runtime state.
func statusCluster(c status.Cluster) *idiosv1.StatusCluster {
	out := &idiosv1.StatusCluster{Id: c.ID, Ready: c.Ready, SkewSeconds: c.SkewSeconds}
	if c.LastEventAt != "" {
		out.LastEventAt = &c.LastEventAt
	}
	return out
}

// closerStats maps the last closer tick and the totals behind it; a closer
// that has not ticked yet reports no time.
func closerStats(c status.Closer) *idiosv1.CloserStats {
	out := &idiosv1.CloserStats{Closed: int32(c.Closed), Attached: int32(c.Attached), Opened: int32(c.Opened),
		ClosedTotal: int32(c.ClosedTotal), AttachedTotal: int32(c.AttachedTotal), OpenedTotal: int32(c.OpenedTotal)}
	if c.LastTickAt != "" {
		out.LastTickAt = &c.LastTickAt
	}
	return out
}

// gapCounts maps the capture outcomes the pool counted, in the enum's order.
func gapCounts(gaps map[string]uint64) []*idiosv1.GapCount {
	var out []*idiosv1.GapCount
	for gap, n := range gaps {
		out = append(out, &idiosv1.GapCount{Gap: captureGaps[gap], Count: int32(n)})
	}
	slices.SortFunc(out, func(a, b *idiosv1.GapCount) int { return cmp.Compare(a.GetGap(), b.GetGap()) })
	return out
}

// sweepRun maps one recorded sweep pass.
func sweepRun(r store.SweepRun) *idiosv1.SweepRun {
	return &idiosv1.SweepRun{
		Id: r.ID, RanAt: r.RanAt, Cutoff: r.Cutoff, TableName: r.TableName,
		RowsRemoved: int32(r.RowsRemoved), FilesRemoved: int32(r.FilesRemoved), BytesRemoved: r.BytesRemoved,
		DurationMs: int32(r.DurationMs), Error: r.Error,
	}
}
