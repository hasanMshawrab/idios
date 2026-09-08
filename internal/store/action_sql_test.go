package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestHumanActionsAreIdempotentAndReportExistence(t *testing.T) {
	const (
		setupAt = "2026-08-27T11:00:00.000000Z"
		first   = "2026-08-27T12:05:00.000000Z"
		second  = "2026-08-27T12:10:00.000000Z"
	)
	ctx := context.Background()

	call := func(fn func(context.Context, *sql.Tx, int64, string) (bool, error), s *Store, id int64, val string) bool {
		var ok bool
		inTx(t, s, func(tx *sql.Tx) error {
			var err error
			ok, err = fn(ctx, tx, id, val)
			return err
		})
		return ok
	}
	unacknowledge := func(ctx context.Context, tx *sql.Tx, id int64, _ string) (bool, error) {
		return UnacknowledgeIncident(ctx, tx, id)
	}
	undismiss := func(ctx context.Context, tx *sql.Tx, id int64, _ string) (bool, error) {
		return UndismissIncident(ctx, tx, id)
	}
	unresolve := func(ctx context.Context, tx *sql.Tx, id int64, _ string) (bool, error) {
		return UnresolveIncident(ctx, tx, id)
	}

	cases := []struct {
		name  string
		setup func(t *testing.T, s *Store, id int64)
		fn    func(context.Context, *sql.Tx, int64, string) (bool, error)
		vals  [2]string
		want  func(seeded Incident) Incident
	}{
		{
			name: "resolve on a recovered incident keeps the close",
			setup: func(t *testing.T, s *Store, id int64) {
				inTx(t, s, func(tx *sql.Tx) error { return CloseIncident(ctx, tx, id, CloseRecovered, setupAt) })
			},
			fn:   ResolveIncident,
			vals: [2]string{first, second},
			want: func(seeded Incident) Incident {
				seeded.ClosedAt, seeded.CloseReason = ptr(setupAt), ptr(CloseRecovered)
				return seeded
			},
		},
		{
			name: "unresolve on a recovered incident stays closed but reports found",
			setup: func(t *testing.T, s *Store, id int64) {
				inTx(t, s, func(tx *sql.Tx) error { return CloseIncident(ctx, tx, id, CloseRecovered, setupAt) })
			},
			fn:   unresolve,
			vals: [2]string{"", ""},
			want: func(seeded Incident) Incident {
				seeded.ClosedAt, seeded.CloseReason = ptr(setupAt), ptr(CloseRecovered)
				return seeded
			},
		},
		{
			name: "unresolve after resolve reopens with acknowledged_at and note untouched",
			setup: func(t *testing.T, s *Store, id int64) {
				inTx(t, s, func(tx *sql.Tx) error { _, err := AcknowledgeIncident(ctx, tx, id, setupAt); return err })
				inTx(t, s, func(tx *sql.Tx) error { _, err := SetIncidentNote(ctx, tx, id, "manual note"); return err })
				inTx(t, s, func(tx *sql.Tx) error { _, err := ResolveIncident(ctx, tx, id, setupAt); return err })
			},
			fn:   unresolve,
			vals: [2]string{"", ""},
			want: func(seeded Incident) Incident {
				seeded.AcknowledgedAt = ptr(setupAt)
				seeded.Note = ptr("manual note")
				return seeded
			},
		},
		{
			name: "acknowledge",
			fn:   AcknowledgeIncident,
			vals: [2]string{first, second},
			want: func(seeded Incident) Incident {
				seeded.AcknowledgedAt = ptr(first)
				return seeded
			},
		},
		{
			name: "unacknowledge after acknowledge",
			setup: func(t *testing.T, s *Store, id int64) {
				inTx(t, s, func(tx *sql.Tx) error { _, err := AcknowledgeIncident(ctx, tx, id, setupAt); return err })
			},
			fn:   unacknowledge,
			vals: [2]string{"", ""},
			want: func(seeded Incident) Incident { return seeded },
		},
		{
			name: "resolve on open",
			fn:   ResolveIncident,
			vals: [2]string{first, second},
			want: func(seeded Incident) Incident {
				seeded.ClosedAt, seeded.CloseReason = ptr(first), ptr(CloseManual)
				return seeded
			},
		},
		{
			name: "dismiss",
			fn:   DismissIncident,
			vals: [2]string{first, second},
			want: func(seeded Incident) Incident {
				seeded.DismissedAt = ptr(first)
				return seeded
			},
		},
		{
			name: "undismiss after dismiss",
			setup: func(t *testing.T, s *Store, id int64) {
				inTx(t, s, func(tx *sql.Tx) error { _, err := DismissIncident(ctx, tx, id, setupAt); return err })
			},
			fn:   undismiss,
			vals: [2]string{"", ""},
			want: func(seeded Incident) Incident { return seeded },
		},
		{
			name: `note "fixed in PR 123"`,
			fn:   SetIncidentNote,
			vals: [2]string{"fixed in PR 123", "fixed in PR 123"},
			want: func(seeded Incident) Incident {
				seeded.Note = ptr("fixed in PR 123")
				return seeded
			},
		},
		{
			name: "empty note clears an existing note",
			setup: func(t *testing.T, s *Store, id int64) {
				inTx(t, s, func(tx *sql.Tx) error { _, err := SetIncidentNote(ctx, tx, id, "old note"); return err })
			},
			fn:   SetIncidentNote,
			vals: [2]string{"", ""},
			want: func(seeded Incident) Incident { return seeded },
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			cid := insertCluster(t, s, "c")
			insertPod(t, s, cid, "p1")
			id, err := insertPodIncident(t, s, cid, "p1", "api", CategoryCrash)
			if err != nil {
				t.Fatal(err)
			}
			seeded := loadAllIncidents(t, s)[0]
			if c.setup != nil {
				c.setup(t, s, id)
			}
			want := c.want(seeded)

			for _, val := range c.vals {
				if ok := call(c.fn, s, id, val); !ok {
					t.Fatalf("ok = false, want true")
				}
				if d := cmp.Diff([]Incident{want}, loadAllIncidents(t, s)); d != "" {
					t.Fatal(d)
				}
			}

			if ok := call(c.fn, s, 999, c.vals[1]); ok {
				t.Fatalf("ok = true for unknown id, want false")
			}
			if d := cmp.Diff([]Incident{want}, loadAllIncidents(t, s)); d != "" {
				t.Fatal(d)
			}
		})
	}
}
