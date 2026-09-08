package sweep

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/hasanMshawrab/idios/internal/store"
)

// orphanGrace is how young a file with no row may be before it counts as an
// orphan. The pool writes the file first and the row in a later
// transaction, so a file this fresh may simply not have its row yet.
const orphanGrace = 10 * time.Minute

// sweepOrphanFiles deletes every file under the root that no artifacts row
// owns, temp files included, except those inside the grace period.
func (s *Sweeper) sweepOrphanFiles(ctx context.Context, _ string) result {
	var r result
	owned := map[string]bool{}
	err := s.w.Tx(ctx, func(tx *sql.Tx) error {
		files, err := store.ListArtifactFiles(ctx, tx)
		if err != nil {
			return err
		}
		for _, f := range files {
			owned[f.FilePath] = true
		}
		return nil
	})
	if err != nil {
		r.errs = append(r.errs, err)
		return r
	}
	edge := s.clk.Now().Add(-orphanGrace)
	err = filepath.WalkDir(s.cfg.Root, func(p string, d fs.DirEntry, err error) error {
		if d == nil {
			// Only the root itself reaches here without an entry; a root that
			// does not exist yet means nothing was ever captured.
			if errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		if err != nil {
			r.errs = append(r.errs, err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.cfg.Root, p)
		if err != nil {
			r.errs = append(r.errs, err)
			return nil
		}
		if owned[filepath.ToSlash(rel)] {
			return nil
		}
		fi, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			r.errs = append(r.errs, err)
			return nil
		}
		if fi.ModTime().After(edge) {
			return nil
		}
		if err := os.Remove(p); err != nil {
			r.errs = append(r.errs, err)
			return nil
		}
		r.files++
		r.bytes += fi.Size()
		return nil
	})
	if err != nil {
		r.errs = append(r.errs, err)
	}
	return r
}

// sweepOrphanRows deletes artifact rows whose file is gone, so the table
// never claims a file the disk does not have.
func (s *Sweeper) sweepOrphanRows(ctx context.Context, _ string) result {
	var r result
	err := s.w.Tx(ctx, func(tx *sql.Tx) error {
		files, err := store.ListArtifactFiles(ctx, tx)
		if err != nil {
			return err
		}
		var gone []int64
		for _, f := range files {
			_, err := os.Stat(filepath.Join(s.cfg.Root, filepath.FromSlash(f.FilePath)))
			if errors.Is(err, fs.ErrNotExist) {
				gone = append(gone, f.ID)
				continue
			}
			if err != nil {
				return err
			}
		}
		r.rows, err = store.DeleteArtifactRows(ctx, tx, gone)
		return err
	})
	if err != nil {
		r.errs = append(r.errs, err)
	}
	return r
}

// checkpoint moves the WAL into the main database file, the last step of
// every pass so it always runs after any deletes that made the file grow.
func (s *Sweeper) checkpoint(ctx context.Context, _ string) result {
	var r result
	if err := s.w.Checkpoint(ctx); err != nil {
		r.errs = append(r.errs, err)
	}
	return r
}
