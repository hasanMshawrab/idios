package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

var testEpoch = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func openMigratedStore(t *testing.T) (*Store, *clock.Fake) {
	t.Helper()
	s := openTestStore(t)
	clk := clock.NewFake(testEpoch)
	if err := s.Migrate(context.Background(), clk); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s, clk
}
