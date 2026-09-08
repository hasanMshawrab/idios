package ingest

import "github.com/hasanMshawrab/idios/internal/store"

// DeletionReason infers why a pod went away from rows that are local at
// delete time. Revision, not first_seen_at, decides a rollout: a rollback
// reuses an old ReplicaSet and bumps its revision. Sibling count is not an
// input; on a 3-replica Deployment every deletion leaves two siblings. A pod
// told to leave by the eviction API or the kubelet was not replaced by a
// rollout or pruned as a finished Job run even if one of those also holds,
// so disrupted is checked first and nothing else is consulted once it does.
func DeletionReason(controllerKind, createdAt string, ownRevision, newestRevision *int64, siblingCreatedAt []string, disrupted bool) string {
	if disrupted {
		return store.DeletionReasonEvicted
	}
	if controllerKind == "Job" {
		return store.DeletionReasonJobPruned
	}
	if ownRevision != nil && newestRevision != nil && *newestRevision > *ownRevision {
		return store.DeletionReasonRollout
	}
	if len(siblingCreatedAt) == 0 {
		return store.DeletionReasonUnknown
	}
	for _, s := range siblingCreatedAt {
		if s > createdAt {
			return store.DeletionReasonReplaced
		}
	}
	return store.DeletionReasonScaledDown
}
