package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	logFileName    = "idios.log"
	logRotateBytes = 10 << 20
)

// rotatingFile appends to path and, when a write would take it past max,
// renames it to path+".1" and starts over. One generation is kept: the log
// exists for a look at the last stop, the database is the record.
type rotatingFile struct {
	path string
	max  int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

func openRotatingFile(path string, max int64) (*rotatingFile, error) {
	r := &rotatingFile{path: path, max: max}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, fi.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		if err := r.f.Close(); err != nil {
			// Whatever Close reported, the descriptor is spent; keeping it
			// would fail every later write instead of just this one.
			r.f = nil
			return 0, err
		}
		if err := os.Rename(r.path, r.path+".1"); err != nil {
			// A log that dies without a signal is worse than an oversized
			// one: keep appending to the original file instead of losing it.
			if openErr := r.open(); openErr != nil {
				r.f = nil
				return 0, openErr
			}
			return 0, err
		}
		if err := r.open(); err != nil {
			r.f = nil
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	return r.f.Close()
}

// teeHandler gives every record to each handler that wants its level.
type teeHandler []slog.Handler

func (t teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range t {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range t {
		if h.Enabled(ctx, r.Level) {
			errs = append(errs, h.Handle(ctx, r.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (t teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(teeHandler, len(t))
	for i, h := range t {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (t teeHandler) WithGroup(name string) slog.Handler {
	out := make(teeHandler, len(t))
	for i, h := range t {
		out[i] = h.WithGroup(name)
	}
	return out
}

// newLogger writes JSON to the log file under dataDir and, when stderr is
// a terminal, text to stderr as well. The returned Closer closes the file.
func newLogger(dataDir string, stderr *os.File) (*slog.Logger, io.Closer, error) {
	f, err := openRotatingFile(filepath.Join(dataDir, logFileName), logRotateBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	hs := teeHandler{slog.NewJSONHandler(f, nil)}
	if fi, err := stderr.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		hs = append(hs, slog.NewTextHandler(stderr, nil))
	}
	return slog.New(hs), f, nil
}
