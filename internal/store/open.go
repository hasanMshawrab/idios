// Package store owns the SQLite file: opening, migrations, the single
// Writer, the read-only Reader, and the row types other packages speak in.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store holds the writer and reader handles onto one SQLite file.
type Store struct {
	Writer *Writer
	Reader *Reader
	path   string
}

// busy_timeout first: journal_mode fails on a briefly locked file.
var basePragmas = []string{
	"busy_timeout(5000)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"foreign_keys(1)",
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	for _, p := range basePragmas {
		q.Add("_pragma", p)
	}
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	}
	return "file:" + path + "?" + q.Encode()
}

// Open opens both handles, creating the directory (0700) and file (0600)
// if needed. It does not migrate.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	// Created here rather than by the driver so the mode is 0600 regardless of
	// umask; chmod'd too since a pre-existing file keeps whatever mode it had.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create db file: %w", err)
	}
	_ = f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("chmod db file: %w", err)
	}

	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	w.SetMaxOpenConns(1)
	if err := w.Ping(); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("ping writer: %w", err)
	}

	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	if err := r.Ping(); err != nil {
		_ = w.Close()
		_ = r.Close()
		return nil, fmt.Errorf("ping reader: %w", err)
	}

	return &Store{Writer: &Writer{db: w}, Reader: &Reader{db: r}, path: path}, nil
}

// Close closes both the writer and reader connections.
func (s *Store) Close() error {
	return errors.Join(s.Reader.db.Close(), s.Writer.db.Close())
}
