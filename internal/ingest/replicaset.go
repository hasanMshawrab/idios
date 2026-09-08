package ingest

import (
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// MapReplicaSet returns one rollout_history row per template container.
// Revision is read on every event because a rollback reuses the old
// ReplicaSet and bumps its annotation. The status counts are plain integers
// on the object, so a ReplicaSet its controller has never observed reports
// zeros that mean nothing; observedGeneration is the only sign it has been
// looked at, and the counts stay null until then.
func MapReplicaSet(rs *appsv1.ReplicaSet, clusterID int64, now time.Time) []store.RolloutHistory {
	nowS := clock.Format(now)
	base := store.RolloutHistory{
		ClusterID: clusterID, Namespace: rs.Namespace, ReplicaSetUID: string(rs.UID), ReplicaSetName: rs.Name,
		CreatedAt: k8sTime(rs.CreationTimestamp), FirstSeenAt: nowS, LastSeenAt: nowS,
	}
	if rs.Spec.Replicas != nil {
		base.Replicas = ptrInt64(int64(*rs.Spec.Replicas))
	}
	if rs.Status.ObservedGeneration != 0 {
		base.ReadyReplicas = ptrInt64(int64(rs.Status.ReadyReplicas))
		base.AvailableReplicas = ptrInt64(int64(rs.Status.AvailableReplicas))
	}
	if ctrl := metav1.GetControllerOf(rs); ctrl != nil && ctrl.Kind == "Deployment" {
		base.DeploymentName, base.DeploymentUID = ctrl.Name, string(ctrl.UID)
	}
	if v, err := strconv.ParseInt(rs.Annotations["deployment.kubernetes.io/revision"], 10, 64); err == nil {
		base.Revision = ptrInt64(v)
	}
	var rows []store.RolloutHistory
	for _, c := range rs.Spec.Template.Spec.Containers {
		row := base
		row.ContainerName, row.Image, row.ImageTag = c.Name, c.Image, imageTag(c.Image)
		rows = append(rows, row)
	}
	return rows
}
