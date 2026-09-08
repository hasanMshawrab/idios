package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pragma(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var v string
	if err := db.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return strings.ToLower(v)
}

func TestConnectionsCarryPragmas(t *testing.T) {
	s := openTestStore(t)
	cases := []struct {
		db     *sql.DB
		pragma string
		want   string
	}{
		{s.Writer.db, "journal_mode", "wal"},
		{s.Writer.db, "synchronous", "1"},
		{s.Writer.db, "foreign_keys", "1"},
		{s.Writer.db, "busy_timeout", "5000"},
		{s.Writer.db, "query_only", "0"},
		{s.Reader.DB(), "foreign_keys", "1"},
		{s.Reader.DB(), "query_only", "1"},
	}
	for _, c := range cases {
		if got := pragma(t, c.db, c.pragma); got != c.want {
			t.Errorf("PRAGMA %s = %q, want %q", c.pragma, got, c.want)
		}
	}
	if _, err := s.Reader.DB().ExecContext(context.Background(), "CREATE TABLE t (x INTEGER) STRICT"); err == nil {
		t.Error("reader executed a write")
	}
}

func TestDBFileIsPrivate(t *testing.T) {
	cases := []struct {
		name  string
		setup func(path string)
	}{
		{name: "fresh file", setup: func(path string) {}},
		{name: "pre-existing 0644 file", setup: func(path string) {
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "idios.db")
			c.setup(path)
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			info, err := os.Stat(s.path)
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Fatalf("db file mode = %o, want 600", perm)
			}
		})
	}
}
