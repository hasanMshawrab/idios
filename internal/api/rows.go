package api

import (
	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/store"
)

// The functions below map one stored row to its wire message. Every screen
// that shows the same row shows the same fields, so they are shared rather
// than repeated per endpoint.

// mapAll maps a row slice.
func mapAll[T, M any](rows []T, one func(T) M) []M {
	if len(rows) == 0 {
		return nil
	}
	out := make([]M, 0, len(rows))
	for _, r := range rows {
		out = append(out, one(r))
	}
	return out
}

// pod maps the stored pod snapshot, or nothing when the pod has been swept.
func pod(p *store.Pod) *idiosv1.Pod {
	if p == nil {
		return nil
	}
	return &idiosv1.Pod{
		Uid:                 p.UID,
		ClusterId:           p.ClusterID,
		Namespace:           p.Namespace,
		Name:                p.Name,
		NodeName:            p.NodeName,
		Phase:               p.Phase,
		StatusReason:        p.StatusReason,
		StatusMessage:       p.StatusMessage,
		DeletionRequestedAt: p.DeletionRequestedAt,
		QosClass:            p.QOSClass,
		ControllerKind:      p.ControllerKind,
		ControllerName:      p.ControllerName,
		ControllerUid:       p.ControllerUID,
		WorkloadKind:        p.WorkloadKind,
		WorkloadName:        p.WorkloadName,
		CreatedAt:           p.CreatedAt,
		StartedAt:           p.StartedAt,
		FirstSeenAt:         p.FirstSeenAt,
		LastSeenAt:          p.LastSeenAt,
		DeletedAt:           p.DeletedAt,
		DeletionSource:      enumPtr(deletionSources, p.DeletionSource),
		DeletionReason:      enumPtr(deletionReasons, p.DeletionReason),
	}
}

// container maps one container snapshot.
func container(c store.Container) *idiosv1.Container {
	return &idiosv1.Container{
		Id:                     c.ID,
		PodUid:                 c.PodUID,
		Name:                   c.Name,
		Kind:                   containerKinds[c.Kind],
		Image:                  c.Image,
		ImageTag:               c.ImageTag,
		ImageId:                c.ImageID,
		ContainerId:            c.ContainerID,
		CpuRequest:             c.CPURequest,
		CpuLimit:               c.CPULimit,
		MemRequest:             c.MemRequest,
		MemLimit:               c.MemLimit,
		CpuRequestMillis:       c.CPURequestMillis,
		CpuLimitMillis:         c.CPULimitMillis,
		MemRequestBytes:        c.MemRequestBytes,
		MemLimitBytes:          c.MemLimitBytes,
		State:                  containerStates[c.State],
		Reason:                 c.Reason,
		ExitCode:               int32Ptr(c.ExitCode),
		Signal:                 int32Ptr(c.Signal),
		Ready:                  c.Ready,
		RestartCount:           int32(c.RestartCount),
		RunningSince:           c.RunningSince,
		LastTerminatedReason:   c.LastTerminatedReason,
		LastTerminatedExitCode: int32Ptr(c.LastTerminatedExitCode),
		LastTerminatedSignal:   int32Ptr(c.LastTerminatedSignal),
		LastTerminatedAt:       c.LastTerminatedAt,
		UpdatedAt:              c.UpdatedAt,
	}
}

// condition maps one pod condition.
func condition(c store.PodCondition) *idiosv1.PodCondition {
	return &idiosv1.PodCondition{
		Id:              c.ID,
		PodUid:          c.PodUID,
		Type:            c.Type,
		Status:          c.Status,
		Reason:          c.Reason,
		Message:         c.Message,
		K8STransitionAt: c.K8sTransitionAt,
		ObservedAt:      c.ObservedAt,
	}
}

// event maps one Kubernetes event. The stored raw object has no field on the
// wire: no screen shows it.
func event(e store.K8sEvent) *idiosv1.K8SEvent {
	return &idiosv1.K8SEvent{
		Id:              e.ID,
		ClusterId:       e.ClusterID,
		EventUid:        e.EventUID,
		Namespace:       e.Namespace,
		Type:            e.Type,
		InvolvedKind:    e.InvolvedKind,
		InvolvedName:    e.InvolvedName,
		InvolvedUid:     e.InvolvedUID,
		FieldPath:       e.FieldPath,
		Reason:          e.Reason,
		Message:         e.Message,
		SourceComponent: e.SourceComponent,
		Count:           int32(e.Count),
		FirstTs:         e.FirstTS,
		LastTs:          e.LastTS,
		Category:        enumPtr(categories, e.Category),
		IncidentId:      e.IncidentID,
	}
}

// artifact maps one captured file or the gap that stands for it.
func artifact(a store.Artifact) *idiosv1.Artifact {
	return &idiosv1.Artifact{
		Id:            a.ID,
		PodUid:        a.PodUID,
		IncidentId:    a.IncidentID,
		ContainerName: a.ContainerName,
		Kind:          artifactKinds[a.Kind],
		RestartCount:  int32(a.RestartCount),
		FilePath:      a.FilePath,
		SizeBytes:     a.SizeBytes,
		Truncated:     a.Truncated,
		CapturedEarly: a.CapturedEarly,
		CaptureGap:    enumPtr(captureGaps, a.CaptureGap),
		CaptureNote:   a.CaptureNote,
		CapturedAt:    a.CapturedAt,
	}
}

// transition maps one container state change.
func transition(h store.ContainerStateHistory) *idiosv1.ContainerStateHistory {
	return &idiosv1.ContainerStateHistory{
		Id:               h.ID,
		PodUid:           h.PodUID,
		ContainerName:    h.ContainerName,
		IncidentId:       h.IncidentID,
		Image:            h.Image,
		ImageId:          h.ImageID,
		ContainerId:      h.ContainerID,
		State:            containerStates[h.State],
		Reason:           h.Reason,
		ExitCode:         int32Ptr(h.ExitCode),
		Signal:           int32Ptr(h.Signal),
		RestartCount:     int32(h.RestartCount),
		Category:         enumPtr(categories, h.Category),
		K8SStartedAt:     h.K8sStartedAt,
		K8SFinishedAt:    h.K8sFinishedAt,
		ObservedAt:       h.ObservedAt,
		GapReconstructed: h.GapReconstructed,
	}
}
