package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func loadArtifacts(t *testing.T, s *Store) []Artifact {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), `
SELECT id, pod_uid, incident_id, container_name, kind, restart_count, file_path, size_bytes, truncated, captured_early, capture_gap, capture_note, captured_at
FROM artifacts ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.PodUID, &a.IncidentID, &a.ContainerName, &a.Kind, &a.RestartCount, &a.FilePath, &a.SizeBytes, &a.Truncated, &a.CapturedEarly, &a.CaptureGap, &a.CaptureNote, &a.CapturedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestArtifactUpsertReplacesFileRowsAndKeepsFileOverGap(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	ts := clock.Format(testEpoch)
	key := Artifact{PodUID: "p1", ContainerName: "api", Kind: ArtifactLogPrevious, RestartCount: 1}
	gap := key
	gap.CaptureGap, gap.CaptureNote, gap.CapturedAt = ptr(GapNoOutput), nil, ts
	file := key
	file.FilePath, file.SizeBytes, file.Truncated, file.CapturedAt = ptr("1/idios-smoke/p1/api/restart_001.log"), 120, true, "2026-08-27T12:01:00.000000Z"
	bigger := file
	bigger.SizeBytes, bigger.Truncated, bigger.CapturedEarly, bigger.CapturedAt = 300, false, true, "2026-08-27T12:02:00.000000Z"
	laterGap := gap
	laterGap.CaptureGap, laterGap.CaptureNote, laterGap.CapturedAt = ptr(GapPodDeleted), ptr(`pods "pod-p1" not found`), "2026-08-27T12:03:00.000000Z"

	steps := []struct {
		name string
		in   Artifact
		want Artifact
	}{
		{"gap row is written", gap, gap},
		{"file replaces gap", file, file},
		{"newer file replaces file", bigger, bigger},
		{"later gap leaves the file row", laterGap, bigger},
	}
	for _, st := range steps {
		inTx(t, s, func(tx *sql.Tx) error { return UpsertArtifact(context.Background(), tx, st.in) })
		got := loadArtifacts(t, s)
		if len(got) != 1 {
			t.Fatalf("%s: %d rows, want 1", st.name, len(got))
		}
		got[0].ID = 0
		if d := cmp.Diff(st.want, got[0]); d != "" {
			t.Fatalf("%s: %s", st.name, d)
		}
	}
}
