package store

import "database/sql"

// Deletion sources record which path observed a pod's removal.
const (
	DeletionSourceWatch     = "watch"
	DeletionSourceReconcile = "reconcile"
	DeletionSourceUnwatched = "unwatched"
)

// Deletion reasons classify why a pod was removed.
const (
	DeletionReasonRollout    = "rollout"
	DeletionReasonReplaced   = "replaced"
	DeletionReasonScaledDown = "scaled_down"
	DeletionReasonJobPruned  = "job_pruned"
	DeletionReasonUnknown    = "unknown"
	DeletionReasonEvicted    = "evicted"
)

// Container kinds classify a container's role within a pod spec.
const (
	ContainerKindInit      = "init"
	ContainerKindSidecar   = "sidecar"
	ContainerKindApp       = "app"
	ContainerKindEphemeral = "ephemeral"
)

// States record a container's current lifecycle state.
const (
	StateWaiting    = "waiting"
	StateRunning    = "running"
	StateTerminated = "terminated"
)

// Subjects distinguish which kind of workload an incident is about.
const (
	SubjectPod = "pod"
	SubjectJob = "job"
)

// Categories classify the kind of incident observed.
const (
	CategoryOOM          = "oom"
	CategoryCrash        = "crash"
	CategoryImagePull    = "image_pull"
	CategoryConfig       = "config"
	CategoryProbe        = "probe"
	CategoryScheduling   = "scheduling"
	CategoryNodePressure = "node_pressure"
	CategoryRescheduled  = "rescheduled"
	CategoryJobFailed    = "job_failed"
	CategoryStuck        = "stuck"
	CategoryUncleanExit  = "unclean_exit"
	CategoryOther        = "other"
)

// Close reasons record why an incident was closed.
const (
	CloseRecovered   = "recovered"
	ClosePodDeleted  = "pod_deleted"
	CloseJobFinished = "job_finished"
	CloseManual      = "manual"
)

// Artifact kinds classify what an artifact file captured.
const (
	ArtifactLogPrevious = "log_previous"
	ArtifactLogCurrent  = "log_current"
	ArtifactPodJSON     = "pod_json"
)

// Gap reasons are artifacts.capture_gap values explaining why no log file
// was captured.
const (
	GapPodDeleted    = "pod_deleted"
	GapNoPreviousRun = "no_previous_run"
	GapForbidden     = "forbidden"
	GapNoOutput      = "no_output"
	GapKubeletError  = "kubelet_error"
	GapUnknown       = "unknown"
	GapUnobservable  = "unobservable"
)

// NoRestartIndex is artifacts.restart_count for pod_json and log_current,
// so the unique index never sees a NULL.
const NoRestartIndex = -1

// Cluster is a row of the clusters table.
type Cluster struct {
	ID                int64
	Identity          *string
	Name              string
	ContextName       string
	APIServerURL      string
	FirstSeenAt       string
	LastConnectedAt   *string
	LastError         *string
	LastErrorAt       *string
	GrafanaURL        string
	LokiDatasourceUID string
	LogSelector       string
}

// WatchedNamespace is a row of the watched_namespaces table.
type WatchedNamespace struct {
	ID        int64
	ClusterID int64
	Name      string
	AddedAt   string
}

// Pod is a row of the pods table.
type Pod struct {
	UID                 string
	ClusterID           int64
	Namespace           string
	Name                string
	NodeName            *string
	Phase               string
	StatusReason        *string
	StatusMessage       *string
	DeletionRequestedAt *string
	QOSClass            *string
	ControllerKind      string
	ControllerName      string
	ControllerUID       string
	WorkloadKind        string
	WorkloadName        string
	CreatedAt           string
	StartedAt           *string
	FirstSeenAt         string
	LastSeenAt          string
	DeletedAt           *string
	DeletionSource      *string
	DeletionReason      *string
}

// PodCondition is a row of the pod_condition_history table.
type PodCondition struct {
	ID              int64
	PodUID          string
	Type            string
	Status          string
	Reason          string
	Message         *string
	K8sTransitionAt *string
	ObservedAt      string
}

// Container is a row of the containers table.
type Container struct {
	ID                     int64
	PodUID                 string
	Name                   string
	Kind                   string
	Image                  string
	ImageTag               *string
	ImageID                *string
	ContainerID            *string
	CPURequest             *string
	CPULimit               *string
	MemRequest             *string
	MemLimit               *string
	CPURequestMillis       *int64
	CPULimitMillis         *int64
	MemRequestBytes        *int64
	MemLimitBytes          *int64
	State                  string
	Reason                 *string
	Message                *string
	ExitCode               *int64
	Signal                 *int64
	Ready                  bool
	RestartCount           int64
	RunningSince           *string
	LastTerminatedReason   *string
	LastTerminatedExitCode *int64
	LastTerminatedSignal   *int64
	LastTerminatedAt       *string
	UpdatedAt              string
}

// ContainerStateHistory is a row of the container_state_history table.
type ContainerStateHistory struct {
	ID               int64
	PodUID           string
	ContainerName    string
	IncidentID       *int64
	Image            string
	ImageID          *string
	ContainerID      *string
	State            string
	Reason           *string
	Message          *string
	ExitCode         *int64
	Signal           *int64
	RestartCount     int64
	Category         *string
	K8sStartedAt     *string
	K8sFinishedAt    *string
	ObservedAt       string
	GapReconstructed bool
}

// Incident is a row of the incidents table.
type Incident struct {
	ID             int64
	ClusterID      int64
	Namespace      string
	SubjectKind    string
	PodUID         *string
	JobUID         *string
	ContainerName  string
	WorkloadKind   string
	WorkloadName   string
	Category       string
	FirstReason    string
	LastReason     string
	LastMessage    *string
	Image          *string
	ImageTag       *string
	ImageID        *string
	NodeName       *string
	Occurrences    int64
	OpenedAt       string
	LastSeenAt     string
	ClosedAt       *string
	CloseReason    *string
	AcknowledgedAt *string
	DismissedAt    *string
	Note           *string
}

// Job is a row of the jobs table.
type Job struct {
	UID          string
	ClusterID    int64
	Namespace    string
	Name         string
	CronJobUID   *string
	CronJobName  *string
	Active       int64
	Succeeded    int64
	Failed       int64
	BackoffLimit *int64
	Completions  *int64
	Parallelism  *int64
	// ActiveDeadlineSeconds is the Job's own spec.activeDeadlineSeconds; a
	// DeadlineExceeded failure is unreadable without the deadline it names.
	ActiveDeadlineSeconds *int64
	RestartPolicy         string
	ConditionType         *string
	ConditionReason       *string
	ConditionMessage      *string
	CreatedAt             string
	StartedAt             *string
	FinishedAt            *string
	FirstSeenAt           string
	LastSeenAt            string
	DeletedAt             *string
}

// RolloutHistory is a row of the rollout_history table.
type RolloutHistory struct {
	ID                int64
	ClusterID         int64
	Namespace         string
	DeploymentName    string
	DeploymentUID     string
	ReplicaSetUID     string
	ReplicaSetName    string
	ContainerName     string
	Image             string
	ImageTag          *string
	Revision          *int64
	CreatedAt         string
	Replicas          *int64
	ReadyReplicas     *int64
	AvailableReplicas *int64
	FirstSeenAt       string
	LastSeenAt        string
	DeletedAt         *string
}

// K8sEvent is a row of the k8s_events table.
type K8sEvent struct {
	ID              int64
	ClusterID       int64
	EventUID        string
	Namespace       string
	Type            string
	InvolvedKind    string
	InvolvedName    string
	InvolvedUID     string
	FieldPath       string
	Reason          string
	Message         string
	SourceComponent string
	Count           int64
	FirstTS         string
	LastTS          string
	Category        *string
	IncidentID      *int64
	RawJSON         string
}

// Artifact is a row of the artifacts table.
type Artifact struct {
	ID            int64
	PodUID        string
	IncidentID    *int64
	ContainerName string
	Kind          string
	RestartCount  int64
	FilePath      *string
	SizeBytes     int64
	Truncated     bool
	CapturedEarly bool
	CaptureGap    *string
	CaptureNote   *string
	CapturedAt    string
}

// SweepRun is a row of the sweep_runs table.
type SweepRun struct {
	ID           int64
	RanAt        string
	Cutoff       string
	TableName    string
	RowsRemoved  int64
	FilesRemoved int64
	BytesRemoved int64
	DurationMs   int64
	Error        *string
}

// podColumns lists the pods columns in the order scanPod expects.
const podColumns = `uid, cluster_id, namespace, name, node_name, phase, status_reason, status_message,
deletion_requested_at, qos_class, controller_kind, controller_name, controller_uid,
workload_kind, workload_name, created_at, started_at, first_seen_at, last_seen_at,
deleted_at, deletion_source, deletion_reason`

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

var (
	_ rowScanner = (*sql.Row)(nil)
	_ rowScanner = (*sql.Rows)(nil)
)

// scanPod scans a row selected with podColumns into a Pod.
func scanPod(r rowScanner) (Pod, error) {
	var p Pod
	err := r.Scan(
		&p.UID, &p.ClusterID, &p.Namespace, &p.Name, &p.NodeName, &p.Phase, &p.StatusReason, &p.StatusMessage,
		&p.DeletionRequestedAt, &p.QOSClass, &p.ControllerKind, &p.ControllerName, &p.ControllerUID,
		&p.WorkloadKind, &p.WorkloadName, &p.CreatedAt, &p.StartedAt, &p.FirstSeenAt, &p.LastSeenAt,
		&p.DeletedAt, &p.DeletionSource, &p.DeletionReason,
	)
	return p, err
}
