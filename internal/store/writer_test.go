package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func insertCluster(t *testing.T, s *Store, identity string) int64 {
	t.Helper()
	var id int64
	err := s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		res, err := tx.ExecContext(context.Background(), `
INSERT INTO clusters (identity, name, context_name, api_server_url, first_seen_at)
VALUES (?, 'c', 'ctx', 'https://127.0.0.1:26443', ?)`, identity, clock.Format(testEpoch))
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatalf("insertCluster: %v", err)
	}
	return id
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.Reader.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTxCommitsOrRollsBack(t *testing.T) {
	sentinel := errors.New("handler error")
	insert := func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `
INSERT INTO clusters (identity, name, context_name, api_server_url, first_seen_at)
VALUES ('x', 'c', 'ctx', 'u', ?)`, clock.Format(testEpoch))
		return err
	}
	cases := []struct {
		name      string
		fn        func(*sql.Tx) error
		wantErr   error
		wantPanic bool
		wantRows  int
	}{
		{"nil commits", insert, nil, false, 1},
		{"error rolls back", func(tx *sql.Tx) error { _ = insert(tx); return sentinel }, sentinel, false, 0},
		{"panic rolls back and propagates", func(tx *sql.Tx) error { _ = insert(tx); panic("bug") }, nil, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			var err error
			panicked := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicked = true
					}
				}()
				err = s.Writer.Tx(context.Background(), c.fn)
			}()
			if panicked != c.wantPanic {
				t.Fatalf("panicked = %v, want %v", panicked, c.wantPanic)
			}
			if !c.wantPanic && !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if got := countRows(t, s, "clusters"); got != c.wantRows {
				t.Fatalf("rows = %d, want %d", got, c.wantRows)
			}
		})
	}
}

func TestReaderIsNotBlockedByOpenTx(t *testing.T) {
	s, _ := openMigratedStore(t)
	insertCluster(t, s, "committed")
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
			_, _ = tx.ExecContext(context.Background(), `
INSERT INTO clusters (identity, name, context_name, api_server_url, first_seen_at)
VALUES ('uncommitted', 'c', 'ctx', 'u', ?)`, clock.Format(testEpoch))
			<-release
			return nil
		})
	}()
	got := make(chan int, 1)
	go func() { got <- countRows(t, s, "clusters") }()
	select {
	case n := <-got:
		if n != 1 {
			t.Errorf("reader saw %d rows, want 1 (only committed)", n)
		}
	case <-time.After(2 * time.Second):
		t.Error("reader blocked behind open write transaction")
	}
	close(release)
	<-done
}

type recordingObserver struct {
	calls []string
}

func (r *recordingObserver) ObserveTx(d time.Duration, err error) {
	outcome := "ok"
	if err != nil {
		outcome = "err"
	}
	if d < 0 {
		outcome += " negative"
	}
	r.calls = append(r.calls, outcome)
}

func TestObserverSeesEveryTransactionOutcome(t *testing.T) {
	cases := []struct {
		name        string
		fn          func(*sql.Tx) error
		wantOutcome string
		wantPanic   bool
	}{
		{"commit", func(*sql.Tx) error { return nil }, "ok", false},
		{"fn error rolls back", func(*sql.Tx) error { return errors.New("boom") }, "err", false},
		{
			"driver error rolls back",
			func(tx *sql.Tx) error {
				ctx := context.Background()
				_, err := tx.ExecContext(ctx, "INSERT INTO clusters (context_name) VALUES ('missing not null columns')")
				return err
			},
			"err",
			false,
		},
		{"panic is observed then re-raised", func(*sql.Tx) error { panic("bug") }, "err", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			obs := &recordingObserver{}
			s.Writer.Observer = obs
			ctx := context.Background()

			panicked := false
			func() {
				defer func() {
					if recover() != nil {
						panicked = true
					}
				}()
				_ = s.Writer.Tx(ctx, c.fn)
			}()
			if panicked != c.wantPanic {
				t.Fatalf("panicked = %v, want %v", panicked, c.wantPanic)
			}
			if d := cmp.Diff([]string{c.wantOutcome}, obs.calls); d != "" {
				t.Fatal(d)
			}
		})
	}
}
