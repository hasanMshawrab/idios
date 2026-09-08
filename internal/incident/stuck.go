package incident

import (
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// stuckReason is what a stuck incident says when the container reports no
// waiting reason at all, which is what a pod waiting on a volume or a
// scheduling decision looks like.
const stuckReason = "Pending"

// StuckOps opens one incident per candidate. It takes no policy: the
// threshold was already applied by the edge the caller selected the
// candidates with, so the comparison stays in one place.
func StuckOps(candidates []store.StuckCandidate, now time.Time) Ops {
	nowS := clock.Format(now)
	var ops Ops
	for _, c := range candidates {
		reason := c.Reason
		if reason == "" {
			reason = stuckReason
		}
		podUID := c.PodUID
		ops.Open = append(ops.Open, Open{Incident: store.Incident{
			ClusterID:   c.ClusterID,
			Namespace:   c.Namespace,
			SubjectKind: store.SubjectPod,
			PodUID:      &podUID,
			// A pod-level row matches no container, and the closer closes a
			// non-scheduling incident only through the container it names; a
			// stuck incident without one could never close as recovered.
			ContainerName: c.ContainerName,
			WorkloadKind:  c.WorkloadKind,
			WorkloadName:  c.WorkloadName,
			Category:      store.CategoryStuck,
			FirstReason:   reason,
			LastReason:    reason,
			Occurrences:   1,
			OpenedAt:      c.Since,
			LastSeenAt:    nowS,
		}})
	}
	return ops
}
