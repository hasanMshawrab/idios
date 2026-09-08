// Package incident turns ingest changes into incident operations: open,
// attach, reopen, close. It is pure; the processor executes the operations.
package incident

import "github.com/hasanMshawrab/idios/internal/store"

// ContainerCategory classifies a container state by its reasons and by
// whether its pod is terminating. A running container is never a problem.
// CrashLoopBackOff after an OOMKilled termination is oom, not crash: the loop
// is the symptom, the kill the cause. An Error termination on a terminating
// pod is unclean_exit, not crash: the container was told to stop and died
// badly doing so, whether it exited non-zero on SIGTERM or outlived its grace
// and was killed, and the exit code stays on the history row for the person
// telling the two apart. OOMKilled on a terminating pod is still oom, because
// the limit is still the limit.
func ContainerCategory(state, reason, lastTerminatedReason string, terminating bool) string {
	switch state {
	case store.StateWaiting:
		switch reason {
		case "CrashLoopBackOff":
			if lastTerminatedReason == "OOMKilled" {
				return store.CategoryOOM
			}
			return store.CategoryCrash
		case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
			return store.CategoryImagePull
		case "CreateContainerConfigError", "CreateContainerError":
			return store.CategoryConfig
		}
	case store.StateTerminated:
		switch reason {
		case "OOMKilled":
			return store.CategoryOOM
		case "Error":
			if terminating {
				return store.CategoryUncleanExit
			}
			return store.CategoryCrash
		case "ContainerCannotRun", "StartError":
			return store.CategoryConfig
		}
	}
	return ""
}

// ConditionCategory classifies a pod condition. SchedulingGated is
// intentional and opens nothing. TerminationByKubelet also covers graceful
// node shutdown; the two differ only in message, so they share a category.
// EvictionByEvictionAPI opens nothing: it is the autoscaler, a drain, or
// anything else that respects a PodDisruptionBudget, the API cannot say
// whether the operator drained for consolidation or because the node was
// sick, and the eviction was never the actionable fact; a pod that cannot be
// placed afterwards, comes up broken or dies badly on the way out opens its
// own incident.
func ConditionCategory(condType, status, reason string) string {
	switch {
	case condType == "PodScheduled" && status == "False" && reason == "Unschedulable":
		return store.CategoryScheduling
	case condType == "DisruptionTarget" && status == "True":
		switch reason {
		case "TerminationByKubelet":
			return store.CategoryNodePressure
		case "PreemptionByScheduler", "DeletionByTaintManager", "DeletionByPodGC":
			return store.CategoryRescheduled
		}
	}
	return ""
}

// PodReasonCategory classifies pods.status_reason.
func PodReasonCategory(reason string) string {
	if reason == "Evicted" {
		return store.CategoryNodePressure
	}
	return ""
}

// componentKubelet is how the kubelet names itself as an event source.
const componentKubelet = "kubelet"

// EventCategory classifies an event reason and, for Evicted alone, the
// event's source component; nil is "no category". BackOff is nil on
// purpose: image back-off and restart back-off share the reason and differ
// only in message. Evicted is the kubelet reclaiming a node under pressure
// or another component calling the eviction API, which is a rescheduling;
// an empty component is an older event stream, where pressure is the
// fallback meaning.
func EventCategory(reason, sourceComponent string) *string {
	var c string
	switch reason {
	case "FailedScheduling":
		c = store.CategoryScheduling
	case "Failed", "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
		c = store.CategoryImagePull
	case "CreateContainerConfigError", "CreateContainerError":
		c = store.CategoryConfig
	case "Unhealthy":
		c = store.CategoryProbe
	case "OOMKilling":
		c = store.CategoryOOM
	case "Evicted":
		c = store.CategoryNodePressure
		if sourceComponent != "" && sourceComponent != componentKubelet {
			c = store.CategoryRescheduled
		}
	default:
		return nil
	}
	return &c
}
