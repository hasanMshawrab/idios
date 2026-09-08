package ingest

import (
	"strings"
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"

	"github.com/hasanMshawrab/idios/internal/store"
)

// mapContainers zips the three spec lists with their status lists by name.
// Spec order is kept so rows are stable across events.
func mapContainers(pod *corev1.Pod, now string) []store.Container {
	statuses := statusIndex(pod)
	var out []store.Container
	for _, c := range pod.Spec.InitContainers {
		kind := store.ContainerKindInit
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			kind = store.ContainerKindSidecar
		}
		out = append(out, mapContainer(string(pod.UID), kind, c.Name, c.Image, c.Resources, statuses[c.Name], now))
	}
	for _, c := range pod.Spec.Containers {
		out = append(out, mapContainer(string(pod.UID), store.ContainerKindApp, c.Name, c.Image, c.Resources, statuses[c.Name], now))
	}
	for _, c := range pod.Spec.EphemeralContainers {
		out = append(out, mapContainer(string(pod.UID), store.ContainerKindEphemeral, c.Name, c.Image, c.Resources, statuses[c.Name], now))
	}
	return out
}

func mapContainer(podUID, kind, name, image string, res corev1.ResourceRequirements, st *corev1.ContainerStatus, now string) store.Container {
	c := store.Container{
		PodUID: podUID, Name: name, Kind: kind, Image: image, ImageTag: imageTag(image),
		State: store.StateWaiting, UpdatedAt: now,
	}
	c.CPURequest, c.CPURequestMillis = quantityMillis(res.Requests, corev1.ResourceCPU)
	c.CPULimit, c.CPULimitMillis = quantityMillis(res.Limits, corev1.ResourceCPU)
	c.MemRequest, c.MemRequestBytes = quantityValue(res.Requests, corev1.ResourceMemory)
	c.MemLimit, c.MemLimitBytes = quantityValue(res.Limits, corev1.ResourceMemory)
	if st == nil {
		return c
	}
	c.ImageID = nonEmpty(st.ImageID)
	c.ContainerID = nonEmpty(st.ContainerID)
	c.Ready = st.Ready
	c.RestartCount = int64(st.RestartCount)
	switch {
	case st.State.Running != nil:
		c.State = store.StateRunning
		c.RunningSince = k8sTimePtr(&st.State.Running.StartedAt)
	case st.State.Terminated != nil:
		t := st.State.Terminated
		c.State = store.StateTerminated
		c.Reason = nonEmpty(t.Reason)
		c.Message = cappedMessage(t.Message)
		c.ExitCode = ptrInt64(int64(t.ExitCode))
		c.Signal = ptrInt64(int64(t.Signal))
	case st.State.Waiting != nil:
		c.State = store.StateWaiting
		c.Reason = nonEmpty(st.State.Waiting.Reason)
	}
	if lt := st.LastTerminationState.Terminated; lt != nil {
		c.LastTerminatedReason = nonEmpty(lt.Reason)
		c.LastTerminatedExitCode = ptrInt64(int64(lt.ExitCode))
		c.LastTerminatedSignal = ptrInt64(int64(lt.Signal))
		c.LastTerminatedAt = k8sTimePtr(&lt.FinishedAt)
	}
	return c
}

// imageTag returns the tag part of an image reference. The registry may
// carry a port, so only a colon after the last slash counts, and a digest
// suffix is cut first because a tag may precede it.
func imageTag(image string) *string {
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= slash {
		return nil
	}
	tag := image[colon+1:]
	return &tag
}

func quantityMillis(list corev1.ResourceList, name corev1.ResourceName) (*string, *int64) {
	q, ok := list[name]
	if !ok {
		return nil, nil
	}
	return ptrString(q.String()), ptrInt64(q.MilliValue())
}

func quantityValue(list corev1.ResourceList, name corev1.ResourceName) (*string, *int64) {
	q, ok := list[name]
	if !ok {
		return nil, nil
	}
	return ptrString(q.String()), ptrInt64(q.Value())
}

// terminatedMessageBytes caps the stored termination message. The kubelet
// applies the same cap per container when it reads the termination log, but
// the value arrives from the API server and this text rides every incident
// list row, so a value that never honoured the cap must not bloat the row.
const terminatedMessageBytes = 4096

// cappedMessage is a termination message trimmed to the cap on a rune
// boundary, so a cut inside a multi-byte rune does not store half of it.
func cappedMessage(s string) *string {
	if s == "" {
		return nil
	}
	if len(s) > terminatedMessageBytes {
		s = s[:terminatedMessageBytes]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return &s
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrString(s string) *string { return &s }

func ptrInt64(v int64) *int64 { return &v }
