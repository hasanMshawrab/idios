package processor

import (
	"context"
	"database/sql"

	corev1 "k8s.io/api/core/v1"

	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// podRequests derives what to capture: the newest dead instance per
// container unless its file already exists (a restart seen as two events
// reports it twice), the live log of every container an incident opened or
// reopened on, and one manifest refresh when anything moved.
func (p *Processor) podRequests(ctx context.Context, tx *sql.Tx, clusterID int64, pod *corev1.Pod, changes ingest.PodChanges, ops incident.Ops, res opsResult, incidents []store.Incident) ([]CaptureRequest, error) {
	base := CaptureRequest{ClusterID: clusterID, Namespace: pod.Namespace, PodUID: string(pod.UID), PodName: pod.Name, RestartCount: store.NoRestartIndex}
	byContainer, names := touchedByContainer(incidents, ops, res)
	containerByName := map[string]store.Container{}
	for _, c := range changes.Containers {
		containerByName[c.Name] = c
	}
	var reqs []CaptureRequest
	for _, d := range changes.DeadInstances {
		if d.Unobservable {
			continue
		}
		// A container that ran once and exited 0 is a success, and its log
		// is not evidence of anything.
		if cleanSingleRun(containerByName[d.Container], incidents, byContainer) {
			continue
		}
		done, err := store.HasArtifactFile(ctx, tx, base.PodUID, d.Container, store.ArtifactLogPrevious, d.Index)
		if err != nil {
			return nil, err
		}
		if done {
			continue
		}
		r := base
		r.Container, r.Kind, r.RestartCount, r.Previous, r.Trigger, r.IncidentID = d.Container, store.ArtifactLogPrevious, d.Index, d.Previous, TriggerRestart, byContainer[d.Container]
		reqs = append(reqs, r)
	}

	opened := map[int64]bool{}
	for _, id := range res.openIDs {
		opened[id] = true
	}
	for _, a := range ops.Attach {
		if a.Reopen {
			opened[a.IncidentID] = true
		}
	}
	seen := map[string]bool{}
	for _, inc := range incidentsInOrder(res.openIDs, ops.Attach) {
		if !opened[inc] {
			continue
		}
		containers := []string{names[inc]}
		if names[inc] == "" {
			containers = containers[:0]
			for _, c := range changes.Containers {
				containers = append(containers, c.Name)
			}
		}
		for _, name := range containers {
			if seen[name] {
				continue
			}
			seen[name] = true
			r := base
			r.Container, r.Kind, r.Trigger, r.IncidentID = name, store.ArtifactLogCurrent, TriggerIncidentOpen, ptr(inc)
			reqs = append(reqs, r)
		}
	}

	switch {
	case len(changes.History) > 0:
		r := base
		r.Kind, r.Trigger, r.Pod = store.ArtifactPodJSON, TriggerRestart, pod
		reqs = append(reqs, r)
	case len(opened) > 0:
		r := base
		r.Kind, r.Trigger, r.Pod = store.ArtifactPodJSON, TriggerIncidentOpen, pod
		reqs = append(reqs, r)
	}
	return reqs, nil
}

// cleanSingleRun reports whether a dead instance is a container that ran
// once, exited 0 and has no incident naming it: a container that only ever
// succeeded has no story for a log to tell.
func cleanSingleRun(c store.Container, incidents []store.Incident, byContainer map[string]*int64) bool {
	var exitCode *int64
	if c.State == store.StateTerminated {
		exitCode = c.ExitCode
	} else {
		exitCode = c.LastTerminatedExitCode
	}
	if exitCode == nil || *exitCode != 0 || c.RestartCount != 0 {
		return false
	}
	if byContainer[c.Name] != nil {
		return false
	}
	for _, inc := range incidents {
		if inc.ContainerName == c.Name || inc.ContainerName == "" {
			return false
		}
	}
	return true
}

// incidentsInOrder lists opened ids then attached ids, so requests come out
// in a stable order for the pool and the tests.
func incidentsInOrder(openIDs []int64, attaches []incident.Attach) []int64 {
	out := append([]int64(nil), openIDs...)
	for _, a := range attaches {
		out = append(out, a.IncidentID)
	}
	return out
}
