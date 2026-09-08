// Package sweep applies the retention rules: anything that no longer exists
// in the cluster goes once it is older than the window, anything that still
// exists stays, and every step leaves a sweep_runs row saying what it did.
package sweep

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// Config sizes the sweeper. Root is the artifacts root every file_path is
// relative to; Retention is the window; Interval is the tick of Run.
type Config struct {
	Root      string
	Retention time.Duration
	Interval  time.Duration
}

// Sweeper runs the retention pass. It keeps no state between passes.
type Sweeper struct {
	cfg Config
	w   *store.Writer
	clk clock.Clock
	log *slog.Logger
}

// New returns a Sweeper; Sweep runs one pass, Run repeats it every Interval.
func New(cfg Config, w *store.Writer, clk clock.Clock, log *slog.Logger) *Sweeper {
	return &Sweeper{cfg: cfg, w: w, clk: clk, log: log}
}

type result struct {
	rows, files, bytes int64
	errs               []error
}

type step struct {
	table string
	run   func(ctx context.Context, cutoff string) result
}

// Sweep runs every step once against one cutoff and records each in
// sweep_runs. A step's failures are recorded on its row and the next step
// still runs; only a failure to record stops the pass.
func (s *Sweeper) Sweep(ctx context.Context) error {
	cutoff := clock.Format(s.clk.Now().Add(-s.cfg.Retention))
	for _, st := range s.steps() {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := s.clk.Now()
		r := st.run(ctx, cutoff)
		row := store.SweepRun{
			RanAt: clock.Format(start), Cutoff: cutoff, TableName: st.table,
			RowsRemoved: r.rows, FilesRemoved: r.files, BytesRemoved: r.bytes,
			DurationMs: s.clk.Now().Sub(start).Milliseconds(),
		}
		if err := errors.Join(r.errs...); err != nil {
			msg := err.Error()
			row.Error = &msg
			s.log.Warn("sweep step", "table", st.table, "err", err)
		}
		if err := s.w.Tx(ctx, func(tx *sql.Tx) error { return store.InsertSweepRun(ctx, tx, row) }); err != nil {
			return fmt.Errorf("record sweep of %s: %w", st.table, err)
		}
	}
	return nil
}

// Run sweeps every Interval until ctx is done and returns ctx.Err(). The
// startup pass is the caller's, so it can run before anything else starts.
func (s *Sweeper) Run(ctx context.Context) error {
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := s.Sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("sweep", "err", err)
			}
		}
	}
}

// removeFile deletes one artifact file and reports its size. A file that is
// already gone counts as nothing removed; its row is the orphan-rows step's.
func (s *Sweeper) removeFile(rel string) (int64, bool, error) {
	p := filepath.Join(s.cfg.Root, filepath.FromSlash(rel))
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if err := os.Remove(p); err != nil {
		return 0, false, err
	}
	return fi.Size(), true, nil
}

// removeFiles deletes the files of the given rows and stops at the first
// failure, so the caller can leave the rows in place and retry next pass.
func (s *Sweeper) removeFiles(files []store.ArtifactFile) (count, bytes int64, err error) {
	for _, f := range files {
		n, ok, err := s.removeFile(f.FilePath)
		if err != nil {
			return count, bytes, err
		}
		if ok {
			count++
			bytes += n
		}
	}
	return count, bytes, nil
}

// pruneDirs removes the container and pod directories of the given files
// once they are empty. It stops at the namespace directory, which is shared
// by every pod of that namespace and is not worth racing a capture for.
func (s *Sweeper) pruneDirs(files []store.ArtifactFile) {
	for _, f := range files {
		dir := path.Dir(f.FilePath)
		for strings.Count(dir, "/") >= 2 {
			if err := os.Remove(filepath.Join(s.cfg.Root, filepath.FromSlash(dir))); err != nil {
				break
			}
			dir = path.Dir(dir)
		}
	}
}
