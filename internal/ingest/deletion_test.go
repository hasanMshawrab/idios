package ingest

import (
	"testing"

	"github.com/hasanMshawrab/idios/internal/store"
)

func TestDeletionReasonInference(t *testing.T) {
	created := "2026-08-27T11:45:00.000000Z"
	older := "2026-08-27T11:00:00.000000Z"
	newer := "2026-08-27T11:50:00.000000Z"
	seven, eight := int64(7), int64(8)
	cases := []struct {
		name        string
		kind        string
		own, newest *int64
		siblings    []string
		disrupted   bool
		want        string
	}{
		{"disrupted with a newer live sibling is evicted, not replaced", "ReplicaSet", &eight, &eight, []string{older, newer}, true, store.DeletionReasonEvicted},
		{"disrupted job pod is evicted, not job_pruned", "Job", nil, nil, []string{newer}, true, store.DeletionReasonEvicted},
		{"disrupted with no other signal at all is still evicted", "none", nil, nil, nil, true, store.DeletionReasonEvicted},
		{"disruption target false keeps today's answer", "ReplicaSet", &eight, &eight, []string{older, newer}, false, store.DeletionReasonReplaced},
		{"job pod", "Job", nil, nil, []string{newer}, false, store.DeletionReasonJobPruned},
		{"newer revision of the same deployment", "ReplicaSet", &seven, &eight, []string{newer}, false, store.DeletionReasonRollout},
		{"same revision, newer sibling", "ReplicaSet", &eight, &eight, []string{older, newer}, false, store.DeletionReasonReplaced},
		{"same revision, siblings all older", "ReplicaSet", &eight, &eight, []string{older, older}, false, store.DeletionReasonScaledDown},
		{"sibling created in the same second is not newer", "ReplicaSet", &eight, &eight, []string{created}, false, store.DeletionReasonScaledDown},
		{"no revision known, newer sibling", "ReplicaSet", nil, nil, []string{newer}, false, store.DeletionReasonReplaced},
		{"no siblings", "ReplicaSet", &eight, &eight, nil, false, store.DeletionReasonUnknown},
		{"bare pod", "none", nil, nil, nil, false, store.DeletionReasonUnknown},
		{"statefulset with newer sibling", "StatefulSet", nil, nil, []string{newer}, false, store.DeletionReasonReplaced},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeletionReason(c.kind, created, c.own, c.newest, c.siblings, c.disrupted); got != c.want {
				t.Fatalf("DeletionReason = %q, want %q", got, c.want)
			}
		})
	}
}
