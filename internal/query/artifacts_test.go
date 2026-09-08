package query

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// The artifact page reads one row by id, whether it names a captured file or
// only the reason there is none, and an id nobody stored is absence, not an
// error.
func TestGetArtifactReadsFileAndGapRows(t *testing.T) {
	st, _ := querytest.Seed(t)
	cases := []struct {
		name string
		id   int64
		want *store.Artifact
	}{
		{
			name: "a captured file",
			id:   3,
			want: &store.Artifact{
				ID: 3, PodUID: querytest.CrashPodUID, IncidentID: ip(querytest.CrashIncidentID),
				ContainerName: "api", Kind: store.ArtifactLogPrevious, RestartCount: 0,
				FilePath: sp("prod/pod-crash/api-0.log"), SizeBytes: 4096, Truncated: true,
				CapturedAt: querytest.ArtifactLogAt,
			},
		},
		{
			name: "a gap",
			id:   4,
			want: &store.Artifact{
				ID: 4, PodUID: querytest.CrashPodUID, IncidentID: ip(querytest.CrashIncidentID),
				ContainerName: "api", Kind: store.ArtifactLogCurrent, RestartCount: store.NoRestartIndex,
				CaptureGap: sp(store.GapNoOutput), CaptureNote: sp("container produced no output"),
				CapturedAt: querytest.ArtifactGapAt,
			},
		},
		{"an unknown id", 999999, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := GetArtifact(context.Background(), st.Reader.DB(), c.id)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("-want +got\n%s", diff)
			}
		})
	}
}
