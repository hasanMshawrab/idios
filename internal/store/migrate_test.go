package store

import (
	"context"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func TestMigrateFromEmpty(t *testing.T) {
	s, _ := openMigratedStore(t)
	ctx := context.Background()

	rows, err := s.Reader.DB().QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	want := []string{
		"artifacts", "clusters", "container_state_history", "containers", "incidents", "jobs",
		"k8s_events", "pod_condition_history", "pods", "rollout_history", "schema_migrations",
		"sweep_runs", "watched_namespaces",
	}
	if d := cmp.Diff(want, tables); d != "" {
		t.Fatalf("tables mismatch:\n%s", d)
	}

	v, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", v)
	}
	var applied string
	if err := s.Reader.DB().QueryRowContext(ctx, "SELECT applied_at FROM schema_migrations WHERE version = 1").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if want := clock.Format(testEpoch); applied != want {
		t.Fatalf("applied_at = %q, want %q", applied, want)
	}
}

func TestMigrateTwiceIsNoop(t *testing.T) {
	s, clk := openMigratedStore(t)
	if err := s.Migrate(context.Background(), clk); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var n int
	if err := s.Reader.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", n)
	}
}

func TestMigrateRefusesNewerDatabase(t *testing.T) {
	s, clk := openMigratedStore(t)
	ctx := context.Background()
	if _, err := s.Writer.db.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", 99, clock.Format(testEpoch)); err != nil {
		t.Fatal(err)
	}
	err := s.Migrate(ctx, clk)
	if err == nil {
		t.Fatal("Migrate on a newer database returned no error")
	}
	want := "database schema version 99 is newer than this build understands (1); install a newer idios"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestAllTablesAreStrict(t *testing.T) {
	s, _ := openMigratedStore(t)
	rows, err := s.Reader.DB().QueryContext(context.Background(),
		"SELECT name, sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	strict := regexp.MustCompile(`(?i)\)\s*STRICT\s*$`)
	n := 0
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatal(err)
		}
		if !strict.MatchString(ddl) {
			t.Errorf("%s is not STRICT", name)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 13 {
		t.Fatalf("inspected %d tables, want 13", n)
	}
}
