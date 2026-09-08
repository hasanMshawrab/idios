package ingest

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// DiffPod compares pod with the store's snapshot and returns what to write.
// A nil snap is first sight: rows are recorded, no transition is inferred.
func DiffPod(snap *PodSnapshot, pod *corev1.Pod, clusterID int64, r OwnerResolver, stored Workload, now time.Time) PodChanges {
	nowS := clock.Format(now)
	ch := PodChanges{
		FirstSight: snap == nil,
		Pod:        mapPod(pod, clusterID, r, stored, nowS),
		Containers: mapContainers(pod, nowS),
	}
	var prevConditions []store.PodCondition
	if snap != nil {
		prevConditions = snap.Conditions
	}
	ch.CurrentConditions = mapConditions(pod, nowS)
	ch.Conditions = diffConditions(prevConditions, ch.CurrentConditions)
	if snap == nil {
		return ch
	}
	ch.Pod.FirstSeenAt = snap.Pod.FirstSeenAt
	ch.Pod.DeletedAt, ch.Pod.DeletionSource, ch.Pod.DeletionReason = snap.Pod.DeletedAt, snap.Pod.DeletionSource, snap.Pod.DeletionReason
	// The API server rewrites deletionTimestamp when a later delete arrives
	// with a shorter grace period, and the kubelet's final delete does exactly
	// that, so the last object seen would carry the moment of removal and not
	// the moment termination began.
	if snap.Pod.DeletionRequestedAt != nil {
		ch.Pod.DeletionRequestedAt = snap.Pod.DeletionRequestedAt
	}
	// A controller that has been pruned is no longer in the informer store and
	// no longer has a row, so a late event on its pod resolves only as far as
	// the controller itself. The pod belongs to the same owner it did before.
	if ch.Pod.ControllerUID == snap.Pod.ControllerUID &&
		ch.Pod.WorkloadKind == ch.Pod.ControllerKind && ch.Pod.WorkloadName == ch.Pod.ControllerName {
		ch.Pod.WorkloadKind, ch.Pod.WorkloadName = snap.Pod.WorkloadKind, snap.Pod.WorkloadName
	}
	ch.WorkloadChanged = ch.Pod.WorkloadKind != snap.Pod.WorkloadKind || ch.Pod.WorkloadName != snap.Pod.WorkloadName
	ch.PodReasonChanged = ch.Pod.Phase != snap.Pod.Phase || !equalPtr(ch.Pod.StatusReason, snap.Pod.StatusReason)

	prev := map[string]store.Container{}
	for _, c := range snap.Containers {
		prev[c.Name] = c
	}
	statuses := statusIndex(pod)
	for _, c := range ch.Containers {
		old, ok := prev[c.Name]
		if !ok {
			continue
		}
		ch.History = append(ch.History, diffContainer(old, c, statuses[c.Name], nowS)...)
		ch.DeadInstances = append(ch.DeadInstances, deadInstances(old, c)...)
	}
	return ch
}

func mapPod(pod *corev1.Pod, clusterID int64, r OwnerResolver, stored Workload, nowS string) store.Pod {
	p := store.Pod{
		UID: string(pod.UID), ClusterID: clusterID, Namespace: pod.Namespace, Name: pod.Name,
		NodeName: nonEmpty(pod.Spec.NodeName), Phase: string(pod.Status.Phase),
		StatusReason: nonEmpty(pod.Status.Reason), StatusMessage: nonEmpty(pod.Status.Message),
		DeletionRequestedAt: k8sTimePtr(pod.DeletionTimestamp), QOSClass: nonEmpty(string(pod.Status.QOSClass)),
		ControllerKind: "none", WorkloadKind: "none",
		CreatedAt: k8sTime(pod.CreationTimestamp), StartedAt: k8sTimePtr(pod.Status.StartTime),
		FirstSeenAt: nowS, LastSeenAt: nowS,
	}
	if ctrl := metav1.GetControllerOf(pod); ctrl != nil {
		p.ControllerKind, p.ControllerName, p.ControllerUID = ctrl.Kind, ctrl.Name, string(ctrl.UID)
		p.WorkloadKind, p.WorkloadName = resolveWorkload(ctrl, pod.Namespace, r, stored)
	}
	return p
}

// resolveWorkload walks one owner step through the informer stores. The
// informer is the live answer; stored covers a controller that has been
// pruned out of it, and the controller itself is what is left when neither
// knows the owner.
func resolveWorkload(ctrl *metav1.OwnerReference, ns string, r OwnerResolver, stored Workload) (kind, name string) {
	var owner *metav1.OwnerReference
	switch ctrl.Kind {
	case "ReplicaSet":
		owner = r.ReplicaSetOwner(ns, ctrl.Name)
	case "Job":
		owner = r.JobOwner(ns, ctrl.Name)
	}
	switch {
	case owner != nil:
		return owner.Kind, owner.Name
	case stored != Workload{}:
		return stored.Kind, stored.Name
	}
	return ctrl.Kind, ctrl.Name
}

func mapConditions(pod *corev1.Pod, nowS string) []store.PodCondition {
	var rows []store.PodCondition
	for _, c := range pod.Status.Conditions {
		rows = append(rows, store.PodCondition{
			PodUID: string(pod.UID), Type: string(c.Type), Status: string(c.Status), Reason: c.Reason,
			Message: nonEmpty(c.Message), K8sTransitionAt: k8sTimePtr(&c.LastTransitionTime), ObservedAt: nowS,
		})
	}
	return rows
}

// diffConditions keys on (type, status, reason). The message is left out:
// the Unschedulable text changes with the node count and would flood the
// table; the latest wording lives on the incident instead.
func diffConditions(prev, all []store.PodCondition) []store.PodCondition {
	last := map[string]store.PodCondition{}
	for _, c := range prev {
		last[c.Type] = c
	}
	var rows []store.PodCondition
	for _, c := range all {
		if p, ok := last[c.Type]; ok && p.Status == c.Status && p.Reason == c.Reason {
			continue
		}
		rows = append(rows, c)
	}
	return rows
}

func statusIndex(pod *corev1.Pod) map[string]*corev1.ContainerStatus {
	out := map[string]*corev1.ContainerStatus{}
	for _, list := range [][]corev1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for i := range list {
			out[list[i].Name] = &list[i]
		}
	}
	return out
}

// diffContainer returns zero, one or two history rows: a reconstructed row
// for lastState when restart_count jumped by more than one, then the row for
// the current state when the transition key differs from the snapshot.
func diffContainer(old, c store.Container, st *corev1.ContainerStatus, nowS string) []store.ContainerStateHistory {
	var rows []store.ContainerStateHistory
	if st != nil && c.RestartCount-old.RestartCount > 1 && st.LastTerminationState.Terminated != nil {
		lt := st.LastTerminationState.Terminated
		rows = append(rows, store.ContainerStateHistory{
			PodUID: c.PodUID, ContainerName: c.Name, Image: c.Image, ImageID: c.ImageID, ContainerID: nonEmpty(lt.ContainerID),
			State: store.StateTerminated, Reason: nonEmpty(lt.Reason), Message: cappedMessage(lt.Message),
			ExitCode: ptrInt64(int64(lt.ExitCode)), Signal: ptrInt64(int64(lt.Signal)),
			RestartCount: deadIndex(c), K8sStartedAt: k8sTimePtr(&lt.StartedAt), K8sFinishedAt: k8sTimePtr(&lt.FinishedAt),
			ObservedAt: nowS, GapReconstructed: true,
		})
	}
	if sameTransitionKey(old, c) {
		return rows
	}
	row := store.ContainerStateHistory{
		PodUID: c.PodUID, ContainerName: c.Name, Image: c.Image, ImageID: c.ImageID, ContainerID: c.ContainerID,
		State: c.State, Reason: c.Reason, Message: c.Message, ExitCode: c.ExitCode, Signal: c.Signal,
		RestartCount: c.RestartCount, ObservedAt: nowS,
	}
	if st != nil {
		switch {
		case st.State.Terminated != nil:
			row.K8sStartedAt = k8sTimePtr(&st.State.Terminated.StartedAt)
			row.K8sFinishedAt = k8sTimePtr(&st.State.Terminated.FinishedAt)
		case st.State.Running != nil:
			row.K8sStartedAt = k8sTimePtr(&st.State.Running.StartedAt)
		}
	}
	return append(rows, row)
}

func sameTransitionKey(a, b store.Container) bool {
	return a.State == b.State && equalPtr(a.Reason, b.Reason) && equalPtr(a.ExitCode, b.ExitCode) &&
		a.RestartCount == b.RestartCount && equalPtr(a.ImageID, b.ImageID) && equalPtr(a.ContainerID, b.ContainerID)
}

// deadIndex is the index of the newest dead instance. restart_count labels
// the newest container; while that one is running, the dead one is the
// previous index.
func deadIndex(c store.Container) int64 {
	if c.State == store.StateRunning {
		return c.RestartCount - 1
	}
	return c.RestartCount
}

// deadInstances reports a new death when the counter rose, the last
// termination time moved, or the container itself terminated. In a crash
// loop the terminated state is usually never seen, so the counter and the
// lastState timestamp are the triggers that matter.
func deadInstances(old, c store.Container) []DeadInstance {
	rose := c.RestartCount > old.RestartCount
	lastMoved := c.LastTerminatedAt != nil && !equalPtr(c.LastTerminatedAt, old.LastTerminatedAt)
	terminated := c.State == store.StateTerminated && old.State != store.StateTerminated
	if !rose && !lastMoved && !terminated {
		return nil
	}
	idx := deadIndex(c)
	previous := c.State != store.StateTerminated
	var out []DeadInstance
	if c.RestartCount-old.RestartCount > 1 {
		known := int64(-1)
		if old.LastTerminatedAt != nil {
			known = deadIndex(old)
		}
		for i := known + 1; i < idx; i++ {
			out = append(out, DeadInstance{Container: c.Name, Index: i, Previous: true, Unobservable: true})
		}
	}
	return append(out, DeadInstance{Container: c.Name, Index: idx, Previous: previous})
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
