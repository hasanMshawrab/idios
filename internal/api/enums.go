package api

import (
	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/query"
	"github.com/hasanMshawrab/idios/internal/store"
)

// The tables below are the only place a stored string becomes a wire enum.
// Each is keyed by the store constants, so a stored value the contract does
// not know maps to the enum's zero and the vocabulary test fails.

// categories maps incidents.category.
var categories = map[string]idiosv1.Category{
	store.CategoryOOM:          idiosv1.Category_CATEGORY_OOM,
	store.CategoryCrash:        idiosv1.Category_CATEGORY_CRASH,
	store.CategoryImagePull:    idiosv1.Category_CATEGORY_IMAGE_PULL,
	store.CategoryConfig:       idiosv1.Category_CATEGORY_CONFIG,
	store.CategoryProbe:        idiosv1.Category_CATEGORY_PROBE,
	store.CategoryScheduling:   idiosv1.Category_CATEGORY_SCHEDULING,
	store.CategoryNodePressure: idiosv1.Category_CATEGORY_NODE_PRESSURE,
	store.CategoryRescheduled:  idiosv1.Category_CATEGORY_RESCHEDULED,
	store.CategoryJobFailed:    idiosv1.Category_CATEGORY_JOB_FAILED,
	store.CategoryStuck:        idiosv1.Category_CATEGORY_STUCK,
	store.CategoryUncleanExit:  idiosv1.Category_CATEGORY_UNCLEAN_EXIT,
	store.CategoryOther:        idiosv1.Category_CATEGORY_OTHER,
}

// closeReasons maps incidents.close_reason.
var closeReasons = map[string]idiosv1.CloseReason{
	store.CloseRecovered:   idiosv1.CloseReason_CLOSE_REASON_RECOVERED,
	store.ClosePodDeleted:  idiosv1.CloseReason_CLOSE_REASON_POD_DELETED,
	store.CloseJobFinished: idiosv1.CloseReason_CLOSE_REASON_JOB_FINISHED,
	store.CloseManual:      idiosv1.CloseReason_CLOSE_REASON_MANUAL,
}

// incidentStates maps the derived incident state. Three of the eight exist
// only as the derivation's output, so they are spelled here; four are the
// close reasons a closed row reports; attention is a filter and a count that
// no row ever reports as its own.
var incidentStates = map[string]idiosv1.IncidentState{
	"open":                 idiosv1.IncidentState_INCIDENT_STATE_OPEN,
	"acknowledged":         idiosv1.IncidentState_INCIDENT_STATE_ACKNOWLEDGED,
	store.CloseRecovered:   idiosv1.IncidentState_INCIDENT_STATE_RECOVERED,
	store.ClosePodDeleted:  idiosv1.IncidentState_INCIDENT_STATE_POD_DELETED,
	store.CloseJobFinished: idiosv1.IncidentState_INCIDENT_STATE_JOB_FINISHED,
	store.CloseManual:      idiosv1.IncidentState_INCIDENT_STATE_MANUAL,
	"dismissed":            idiosv1.IncidentState_INCIDENT_STATE_DISMISSED,
	query.StateAttention:   idiosv1.IncidentState_INCIDENT_STATE_ATTENTION,
}

// captureGaps maps artifacts.capture_gap.
var captureGaps = map[string]idiosv1.CaptureGap{
	store.GapPodDeleted:    idiosv1.CaptureGap_CAPTURE_GAP_POD_DELETED,
	store.GapNoPreviousRun: idiosv1.CaptureGap_CAPTURE_GAP_NO_PREVIOUS_RUN,
	store.GapForbidden:     idiosv1.CaptureGap_CAPTURE_GAP_FORBIDDEN,
	store.GapNoOutput:      idiosv1.CaptureGap_CAPTURE_GAP_NO_OUTPUT,
	store.GapKubeletError:  idiosv1.CaptureGap_CAPTURE_GAP_KUBELET_ERROR,
	store.GapUnknown:       idiosv1.CaptureGap_CAPTURE_GAP_UNKNOWN,
	store.GapUnobservable:  idiosv1.CaptureGap_CAPTURE_GAP_UNOBSERVABLE,
}

// deletionSources maps pods.deletion_source.
var deletionSources = map[string]idiosv1.DeletionSource{
	store.DeletionSourceWatch:     idiosv1.DeletionSource_DELETION_SOURCE_WATCH,
	store.DeletionSourceReconcile: idiosv1.DeletionSource_DELETION_SOURCE_RECONCILE,
	store.DeletionSourceUnwatched: idiosv1.DeletionSource_DELETION_SOURCE_UNWATCHED,
}

// deletionReasons maps pods.deletion_reason.
var deletionReasons = map[string]idiosv1.DeletionReason{
	store.DeletionReasonRollout:    idiosv1.DeletionReason_DELETION_REASON_ROLLOUT,
	store.DeletionReasonReplaced:   idiosv1.DeletionReason_DELETION_REASON_REPLACED,
	store.DeletionReasonScaledDown: idiosv1.DeletionReason_DELETION_REASON_SCALED_DOWN,
	store.DeletionReasonJobPruned:  idiosv1.DeletionReason_DELETION_REASON_JOB_PRUNED,
	store.DeletionReasonUnknown:    idiosv1.DeletionReason_DELETION_REASON_UNKNOWN,
	store.DeletionReasonEvicted:    idiosv1.DeletionReason_DELETION_REASON_EVICTED,
}

// containerKinds maps containers.kind.
var containerKinds = map[string]idiosv1.ContainerKind{
	store.ContainerKindInit:      idiosv1.ContainerKind_CONTAINER_KIND_INIT,
	store.ContainerKindSidecar:   idiosv1.ContainerKind_CONTAINER_KIND_SIDECAR,
	store.ContainerKindApp:       idiosv1.ContainerKind_CONTAINER_KIND_APP,
	store.ContainerKindEphemeral: idiosv1.ContainerKind_CONTAINER_KIND_EPHEMERAL,
}

// containerStates maps containers.state.
var containerStates = map[string]idiosv1.ContainerState{
	store.StateWaiting:    idiosv1.ContainerState_CONTAINER_STATE_WAITING,
	store.StateRunning:    idiosv1.ContainerState_CONTAINER_STATE_RUNNING,
	store.StateTerminated: idiosv1.ContainerState_CONTAINER_STATE_TERMINATED,
}

// artifactKinds maps artifacts.kind.
var artifactKinds = map[string]idiosv1.ArtifactKind{
	store.ArtifactLogPrevious: idiosv1.ArtifactKind_ARTIFACT_KIND_LOG_PREVIOUS,
	store.ArtifactLogCurrent:  idiosv1.ArtifactKind_ARTIFACT_KIND_LOG_CURRENT,
	store.ArtifactPodJSON:     idiosv1.ArtifactKind_ARTIFACT_KIND_POD_JSON,
}

// subjectKinds maps incidents.subject_kind.
var subjectKinds = map[string]idiosv1.SubjectKind{
	store.SubjectPod: idiosv1.SubjectKind_SUBJECT_KIND_POD,
	store.SubjectJob: idiosv1.SubjectKind_SUBJECT_KIND_JOB,
}

// timelineKinds maps the kind of a timeline entry. The values are the read
// model's, not a stored column: the timeline is merged from six tables.
var timelineKinds = map[string]idiosv1.TimelineKind{
	query.TimelineContainerTransition: idiosv1.TimelineKind_TIMELINE_KIND_CONTAINER_TRANSITION,
	query.TimelineCondition:           idiosv1.TimelineKind_TIMELINE_KIND_CONDITION,
	query.TimelineEvent:               idiosv1.TimelineKind_TIMELINE_KIND_EVENT,
	query.TimelineCapture:             idiosv1.TimelineKind_TIMELINE_KIND_CAPTURE,
	query.TimelineRollout:             idiosv1.TimelineKind_TIMELINE_KIND_ROLLOUT,
	query.TimelineLifecycle:           idiosv1.TimelineKind_TIMELINE_KIND_LIFECYCLE,
	query.TimelineCut:                 idiosv1.TimelineKind_TIMELINE_KIND_CUT,
}

// lifecycleSteps maps the step a lifecycle entry reports.
var lifecycleSteps = map[string]idiosv1.LifecycleStep{
	query.LifecycleOpened: idiosv1.LifecycleStep_LIFECYCLE_STEP_OPENED,
	query.LifecycleClosed: idiosv1.LifecycleStep_LIFECYCLE_STEP_CLOSED,
}

// enumPtr maps an optional stored value, keeping absent absent. A value the
// table does not hold becomes the enum's zero, which the wire spells
// UNSPECIFIED, because a newer database must not break an older reader.
func enumPtr[T ~int32](table map[string]T, s *string) *T {
	if s == nil {
		return nil
	}
	v := table[*s]
	return &v
}

// int32Ptr narrows an optional stored count to the wire's int32, which
// proto3 JSON writes as a number where int64 would be a string.
func int32Ptr(v *int64) *int32 {
	if v == nil {
		return nil
	}
	n := int32(*v)
	return &n
}
