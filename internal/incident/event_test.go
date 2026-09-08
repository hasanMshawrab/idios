package incident

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

func TestFieldPathContainer(t *testing.T) {
	cases := []struct{ in, want string }{
		{"spec.containers{api}", "api"},
		{"spec.initContainers{init-db}", "init-db"},
		{"spec.ephemeralContainers{debug}", "debug"},
		{"", ""},
		{"spec.containers", ""},
		{"metadata.labels", ""},
	}
	for _, c := range cases {
		if got := FieldPathContainer(c.in); got != c.want {
			t.Errorf("FieldPathContainer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEventMatchesAttachRule(t *testing.T) {
	podInc := store.Incident{SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryCrash}
	podLevel := store.Incident{SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "", Category: store.CategoryNodePressure}
	jobInc := store.Incident{SubjectKind: store.SubjectJob, JobUID: ptr("job-1"), Category: store.CategoryJobFailed}
	ev := func(uid, fieldPath string, cat *string) store.K8sEvent {
		return store.K8sEvent{InvolvedUID: uid, FieldPath: fieldPath, Category: cat}
	}
	cases := []struct {
		name string
		ev   store.K8sEvent
		inc  store.Incident
		want bool
	}{
		{"same pod, container and category", ev("pod-crash", "spec.containers{api}", ptr(store.CategoryCrash)), podInc, true},
		{"uncategorised event on the container", ev("pod-crash", "spec.containers{api}", nil), podInc, true},
		{"different category", ev("pod-crash", "spec.containers{api}", ptr(store.CategoryImagePull)), podInc, false},
		{"different container", ev("pod-crash", "spec.containers{worker}", nil), podInc, false},
		{"pod-level event on a container incident", ev("pod-crash", "", nil), podInc, false},
		{"pod-level incident takes container events", ev("pod-crash", "spec.containers{api}", nil), podLevel, true},
		{"pod-level incident takes pod events of its category", ev("pod-crash", "", ptr(store.CategoryNodePressure)), podLevel, true},
		{"different pod", ev("pod-other", "spec.containers{api}", nil), podInc, false},
		{"job incident by job uid", ev("job-1", "", nil), jobInc, true},
		{"job incident, other uid", ev("pod-crash", "", nil), jobInc, false},
	}
	for _, c := range cases {
		if got := EventMatches(c.ev, c.inc); got != c.want {
			t.Errorf("%s: EventMatches = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestApplyEventProbeGate(t *testing.T) {
	now := clock.Format(testNow)
	pod := &store.Pod{UID: "pod-crash", ClusterID: 1, Namespace: "idios-smoke", WorkloadKind: "Deployment", WorkloadName: "web"}
	deletedPod := &store.Pod{UID: "pod-crash", ClusterID: 1, Namespace: "idios-smoke", WorkloadKind: "Deployment", WorkloadName: "web", DeletedAt: ptr("2026-08-27T11:58:30.000000Z")}
	terminatingPod := &store.Pod{UID: "pod-crash", ClusterID: 1, Namespace: "idios-smoke", WorkloadKind: "Deployment", WorkloadName: "web", DeletionRequestedAt: ptr("2026-08-27T11:59:20.000000Z")}
	notReady := &store.Container{PodUID: "pod-crash", Name: "api", Kind: store.ContainerKindApp, Image: "registry.example.com/web:1.4.2", ImageTag: ptr("1.4.2"), ImageID: ptr("sha"), State: store.StateRunning, Ready: false}
	ready := &store.Container{PodUID: "pod-crash", Name: "api", Kind: store.ContainerKindApp, Image: "registry.example.com/web:1.4.2", State: store.StateRunning, Ready: true}
	startedFresh := &store.Container{PodUID: "pod-crash", Name: "api", Kind: store.ContainerKindApp, Image: "registry.example.com/web:1.4.2", ImageTag: ptr("1.4.2"), ImageID: ptr("sha"), State: store.StateRunning, Ready: false, RunningSince: ptr("2026-08-27T11:59:30.000000Z")}
	startedOld := &store.Container{PodUID: "pod-crash", Name: "api", Kind: store.ContainerKindApp, Image: "registry.example.com/web:1.4.2", ImageTag: ptr("1.4.2"), ImageID: ptr("sha"), State: store.StateRunning, Ready: false, RunningSince: ptr("2026-08-27T11:50:00.000000Z")}
	graced := Policy{ProbeGrace: time.Minute}
	unhealthy := store.K8sEvent{
		ClusterID: 1, EventUID: "ev-1", Namespace: "idios-smoke", Type: "Warning", InvolvedKind: "Pod", InvolvedUID: "pod-crash", FieldPath: "spec.containers{api}",
		Reason: "Unhealthy", Message: "Readiness probe failed: HTTP probe failed with statuscode: 503", Category: ptr(store.CategoryProbe),
		FirstTS: "2026-08-27T11:58:00.000000Z", LastTS: "2026-08-27T11:59:00.000000Z",
	}
	backoff := unhealthy
	backoff.EventUID, backoff.Reason, backoff.Message, backoff.Category = "ev-2", "BackOff", "Back-off restarting failed container", nil

	openProbe := store.Incident{ID: 21, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryProbe, LastSeenAt: "2026-08-27T11:50:00.000000Z"}
	openCrash := store.Incident{ID: 22, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryCrash, LastSeenAt: "2026-08-27T11:40:00.000000Z"}
	openOOM := store.Incident{ID: 23, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryOOM, LastSeenAt: "2026-08-27T11:45:00.000000Z"}
	closedCrash := store.Incident{ID: 24, SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api", Category: store.CategoryCrash, LastSeenAt: "2026-08-27T11:59:00.000000Z", ClosedAt: ptr("2026-08-27T11:59:30.000000Z"), CloseReason: ptr(store.CloseRecovered)}

	probeOpen := Open{Incident: store.Incident{
		ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api",
		WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryProbe, FirstReason: "Unhealthy", LastReason: "Unhealthy",
		LastMessage: ptr(unhealthy.Message), Image: ptr("registry.example.com/web:1.4.2"), ImageTag: ptr("1.4.2"), ImageID: ptr("sha"),
		Occurrences: 1, OpenedAt: "2026-08-27T11:59:00.000000Z", LastSeenAt: now,
	}}
	cases := []struct {
		name      string
		ev        store.K8sEvent
		container *store.Container
		incidents []store.Incident
		pod       *store.Pod
		pol       Policy
		wantOps   Ops
		wantRef   EventRef
	}{
		{"unhealthy on a not-ready container opens probe", unhealthy, notReady, nil, pod, Policy{}, Ops{Open: []Open{probeOpen}}, EventRef{OpenIndex: 0}},
		{"unhealthy on a ready container is noise", unhealthy, ready, nil, pod, Policy{}, Ops{}, EventRef{OpenIndex: -1}},
		{"unhealthy without a container row opens nothing", unhealthy, nil, nil, pod, Policy{}, Ops{}, EventRef{OpenIndex: -1}},
		{"unhealthy attaches to the open probe incident", unhealthy, notReady, []store.Incident{openProbe}, pod, Policy{},
			Ops{Attach: []Attach{{IncidentID: 21, LastReason: "Unhealthy", LastMessage: ptr(unhealthy.Message), LastSeenAt: now}}}, EventRef{OpenIndex: -1, IncidentID: ptr[int64](21)}},
		{"backoff attaches to the matching open incident without an operation", backoff, notReady, []store.Incident{openCrash}, pod, Policy{}, Ops{}, EventRef{OpenIndex: -1, IncidentID: ptr[int64](22)}},
		{"latest last_seen_at wins", backoff, notReady, []store.Incident{openCrash, openOOM}, pod, Policy{}, Ops{}, EventRef{OpenIndex: -1, IncidentID: ptr[int64](23)}},
		{"closed incidents do not take events here", backoff, notReady, []store.Incident{closedCrash}, pod, Policy{}, Ops{}, EventRef{OpenIndex: -1}},
		// A deleted pod's containers stay not ready forever, so a trailing
		// Unhealthy delivery would mint an incident nothing can ever close.
		{"unhealthy on a deleted pod opens nothing", unhealthy, notReady, nil, deletedPod, Policy{}, Ops{}, EventRef{OpenIndex: -1}},
		{"unhealthy on a deleted pod attaches without a bump", unhealthy, notReady, []store.Incident{openProbe}, deletedPod, Policy{}, Ops{}, EventRef{OpenIndex: -1, IncidentID: ptr[int64](21)}},
		// A terminating pod's container never reports ready again either, so
		// the same dead-end applies before the delete watch event arrives.
		{"unhealthy on a terminating pod opens nothing", unhealthy, notReady, nil, terminatingPod, Policy{}, Ops{}, EventRef{OpenIndex: -1}},
		{"unhealthy on a terminating pod attaches without a bump", unhealthy, notReady, []store.Incident{openProbe}, terminatingPod, Policy{}, Ops{}, EventRef{OpenIndex: -1, IncidentID: ptr[int64](21)}},
		// The kubelet starts probing the moment a container starts, so a
		// readiness miss within the grace of running_since is a routine
		// startup miss; one that persists repeats past the grace and opens
		// then, the way an unschedulable pod waits out SchedulingGrace.
		{"unhealthy past the probe grace opens", unhealthy, startedOld, nil, pod, graced, Ops{Open: []Open{probeOpen}}, EventRef{OpenIndex: 0}},
		{"unhealthy within the probe grace opens nothing", unhealthy, startedFresh, nil, pod, graced, Ops{}, EventRef{OpenIndex: -1}},
		{"unhealthy before the container reports running waits out the grace", unhealthy, notReady, nil, pod, graced, Ops{}, EventRef{OpenIndex: -1}},
		{"unhealthy within the probe grace attaches without a bump", unhealthy, startedFresh, []store.Incident{openProbe}, pod, graced, Ops{}, EventRef{OpenIndex: -1, IncidentID: ptr[int64](21)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotOps, gotRef := ApplyEvent(c.incidents, c.ev, c.pod, c.container, testNow, c.pol)
			if d := cmp.Diff(c.wantOps, gotOps, cmpopts.EquateEmpty()); d != "" {
				t.Error(d)
			}
			if d := cmp.Diff(c.wantRef, gotRef); d != "" {
				t.Error(d)
			}
		})
	}
}
