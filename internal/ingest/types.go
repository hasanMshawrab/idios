// Package ingest turns Kubernetes objects plus the store's snapshot rows into
// the row values and history rows to write. It has no SQL and no network.
package ingest

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/store"
)

// PodSnapshot is what the store holds for a pod before the current event.
// Conditions holds the latest row per condition type.
type PodSnapshot struct {
	Pod        store.Pod
	Containers []store.Container
	Conditions []store.PodCondition
}

// PodChanges is everything DiffPod decided for one pod event. History rows
// carry nil Category and IncidentID; the incident package fills them.
// Conditions holds the ones whose (type, status, reason) changed and is what
// is written; CurrentConditions holds every condition on the object.
type PodChanges struct {
	FirstSight        bool
	Pod               store.Pod
	Containers        []store.Container
	History           []store.ContainerStateHistory
	Conditions        []store.PodCondition
	CurrentConditions []store.PodCondition
	PodReasonChanged  bool
	WorkloadChanged   bool
	DeadInstances     []DeadInstance
}

// DeadInstance is a container instance that died since the snapshot. Index
// is the dead-instance index; Previous says whether the kubelet holds it in
// lastState (true) or in state.terminated (false). Unobservable marks a
// middle instance of a restart_count jump that no log call can reach. The
// same instance is reported twice when a restart is observed as two events
// (terminated, then running), so consumers key captures on (pod, container,
// index) rather than counting reports.
type DeadInstance struct {
	Container    string
	Index        int64
	Previous     bool
	Unobservable bool
}

// Workload is the owner above a pod's controller as the store knows it. The
// zero value means the store has no answer.
type Workload struct {
	Kind, Name string
}

// OwnerResolver answers owner lookups from the informer stores. A nil result
// means the object is not in the local store or has no controller.
type OwnerResolver interface {
	ReplicaSetOwner(namespace, name string) *metav1.OwnerReference
	JobOwner(namespace, name string) *metav1.OwnerReference
}
