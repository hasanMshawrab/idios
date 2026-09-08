package processor

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/ingest"
	"github.com/hasanMshawrab/idios/internal/store"
)

func loadEvent(t *testing.T, path string) *corev1.Event {
	t.Helper()
	var ev corev1.Event
	fixture(t, path, &ev)
	return &ev
}

func (h *harness) feedEvent(t *testing.T, file string) {
	t.Helper()
	if err := h.p.Event(context.Background(), 1, loadEvent(t, file)); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
}

func (h *harness) events(t *testing.T) []store.K8sEvent {
	t.Helper()
	rows, err := h.s.Reader.DB().QueryContext(context.Background(), `
SELECT id, cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, message,
       source_component, count, first_ts, last_ts, category, incident_id, raw_json FROM k8s_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.K8sEvent
	for rows.Next() {
		var e store.K8sEvent
		if err := rows.Scan(&e.ID, &e.ClusterID, &e.EventUID, &e.Namespace, &e.Type, &e.InvolvedKind, &e.InvolvedName, &e.InvolvedUID,
			&e.FieldPath, &e.Reason, &e.Message, &e.SourceComponent, &e.Count, &e.FirstTS, &e.LastTS, &e.Category, &e.IncidentID, &e.RawJSON); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// mapEventRow is the row MapEvent produces for an event, classified and
// attached as the case expects; the processor adds nothing else.
func mapEventRow(t *testing.T, id int64, ev *corev1.Event, incidentID *int64) store.K8sEvent {
	t.Helper()
	row, err := ingest.MapEvent(ev, 1)
	if err != nil {
		t.Fatal(err)
	}
	row.ID, row.Category, row.IncidentID = id, incident.EventCategory(row.Reason, row.SourceComponent), incidentID
	return row
}

func storedEvent(t *testing.T, id int64, file string, incidentID *int64) store.K8sEvent {
	t.Helper()
	return mapEventRow(t, id, loadEvent(t, file), incidentID)
}

func TestEventScenarios(t *testing.T) {
	probe := withImage(podIncident(2, "pod-crash", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryProbe, "Unhealthy", "Unhealthy", "2026-08-27T11:59:00.000000Z"), web, ptr(webTag), ptr(webID))
	probe.LastMessage = ptr("Readiness probe failed: HTTP probe failed with statuscode: 503")
	bumpedProbe := probe
	bumpedProbe.Occurrences = 2
	crash := withImage(podIncident(1, "pod-crash", "ReplicaSet", "web-7d9f8c6b5", "api", store.CategoryCrash, "CrashLoopBackOff", "CrashLoopBackOff", "2026-08-27T11:55:00.000000Z"), web, ptr(webTag), ptr(webID))
	pull := withImage(podIncident(1, "pod-pull", "ReplicaSet", "web-66c9d", "api", store.CategoryImagePull, "ErrImagePull", "ErrImagePull", "2026-08-27T11:45:00.000000Z"), "registry.example.com/web:does-not-exist", ptr("does-not-exist"), nil)
	reopenedProbe := store.Incident{
		ID: 2, ClusterID: 1, Namespace: "idios-smoke", SubjectKind: store.SubjectPod, PodUID: ptr("pod-crash"), ContainerName: "api",
		WorkloadKind: "ReplicaSet", WorkloadName: "web-7d9f8c6b5", Category: store.CategoryProbe, FirstReason: "Error", LastReason: "Unhealthy",
		LastMessage: ptr("Readiness probe failed: HTTP probe failed with statuscode: 503"),
		Occurrences: 4, OpenedAt: "2026-08-27T10:00:00.000000Z", LastSeenAt: clock.Format(testNow),
	}
	early := func(reason string) CaptureRequest {
		return request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerEarlyPrefix+reason, nil)
	}
	unhealthyOnTerm := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{UID: "ev-term-1", Namespace: "idios-smoke", CreationTimestamp: metav1.NewTime(time.Date(2026, 8, 27, 11, 59, 50, 0, time.UTC))},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "idios-smoke", Name: "web-7d9f8c6b5-term1", UID: "pod-term", FieldPath: "spec.containers{api}"},
		Reason:         "Unhealthy",
		Message:        "Readiness probe failed: HTTP probe failed with statuscode: 503",
		Type:           "Warning",
		FirstTimestamp: metav1.NewTime(time.Date(2026, 8, 27, 11, 59, 50, 0, time.UTC)),
		LastTimestamp:  metav1.NewTime(time.Date(2026, 8, 27, 11, 59, 50, 0, time.UTC)),
		Count:          1,
		Source:         corev1.EventSource{Component: "kubelet", Host: "node-a"},
	}
	cases := []struct {
		name          string
		run           func(t *testing.T, h *harness)
		uid           string
		wantEvents    []store.K8sEvent
		wantIncidents []store.Incident
		wantReqs      []CaptureRequest
	}{
		{"backoff attaches to the open crash incident", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-series/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-series/event.json", ptr[int64](1))}, []store.Incident{crash}, nil},
		{"failed pull event before the pod is adopted when the incident opens", func(t *testing.T, h *harness) {
			h.feedEvent(t, "event-legacy/event.json")
			h.feed(t, steps("image-pull/s1.json"))
			h.sink.reqs = nil
		}, "pod-pull", []store.K8sEvent{storedEvent(t, 1, "event-legacy/event.json", ptr[int64](1))}, []store.Incident{pull}, nil},
		{"unhealthy on a ready container is noise but early-captures", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-unhealthy/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-unhealthy/event.json", nil)}, nil, []CaptureRequest{early("Unhealthy")}},
		{"unhealthy on a not-ready container opens probe", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-unhealthy/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-unhealthy/event.json", ptr[int64](2))}, []store.Incident{crash, probe},
			[]CaptureRequest{request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, ptr[int64](2))}},
		// A relist replays every Event object unchanged; only the correlator's
		// counter and timestamp say something new happened.
		{"a re-delivered unchanged event bumps nothing", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-unhealthy/event.json")
			h.feedEvent(t, "event-unhealthy/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-unhealthy/event.json", ptr[int64](2))}, []store.Incident{crash, probe},
			[]CaptureRequest{request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, ptr[int64](2))}},
		{"a count advance on the same event bumps the open probe incident", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-unhealthy/event.json")
			h.feedEvent(t, "event-unhealthy/later.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-unhealthy/later.json", ptr[int64](2))}, []store.Incident{crash, bumpedProbe},
			[]CaptureRequest{request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, ptr[int64](2))}},
		{"unhealthy reopens the recovered probe incident and captures its current log", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.seedIncident(t, "pod-crash", "api", store.CategoryProbe, ptr(store.CloseRecovered))
			h.sink.reqs = nil
			h.feedEvent(t, "event-unhealthy/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-unhealthy/event.json", ptr[int64](2))}, []store.Incident{crash, reopenedProbe},
			[]CaptureRequest{request("pod-crash", "web-7d9f8c6b5-abcde", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerIncidentOpen, ptr[int64](2))}},
		{"pod-level evicted with no incident early-captures every container", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-evicted/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-evicted/event.json", nil)}, nil, []CaptureRequest{early("Evicted")}},
		{"pod-level evicted next to an open incident of another category skips the early capture", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-evicted/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-evicted/event.json", nil)}, []store.Incident{crash}, nil},
		{"killing with no incident early-captures", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-killing/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-killing/event.json", nil)}, nil, []CaptureRequest{early("Killing")}},
		{"killing on a container with an open incident attaches and skips early capture", func(t *testing.T, h *harness) {
			h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
			h.sink.reqs = nil
			h.feedEvent(t, "event-killing/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-killing/event.json", ptr[int64](1))}, []store.Incident{crash}, nil},
		// The probe gate keeps the container's long-running readiness miss
		// from opening a fresh incident, but Unhealthy still announces the
		// pod's removal on its own, so the early capture still fires.
		{"unhealthy on a terminating pod opens nothing but early-captures", func(t *testing.T, h *harness) {
			h.feed(t, steps("terminating-exit/before.json"))
			terminating := loadPod(t, "terminating-exit/before.json")
			terminating.DeletionTimestamp = ptr(metav1.NewTime(time.Date(2026, 8, 27, 11, 59, 20, 0, time.UTC)))
			terminating.Status.Conditions[0].Status = corev1.ConditionFalse
			terminating.Status.ContainerStatuses[0].Ready = false
			if err := h.p.Pod(context.Background(), 1, terminating, noOwners{}); err != nil {
				t.Fatal(err)
			}
			h.sink.reqs = nil
			if err := h.p.Event(context.Background(), 1, unhealthyOnTerm); err != nil {
				t.Fatal(err)
			}
		}, "pod-term", []store.K8sEvent{mapEventRow(t, 1, unhealthyOnTerm, nil)}, nil,
			[]CaptureRequest{request("pod-term", "web-7d9f8c6b5-term1", "api", store.ArtifactLogCurrent, store.NoRestartIndex, false, TriggerEarlyPrefix+"Unhealthy", nil)}},
		{"event for an unknown pod is stored unattached", func(t *testing.T, h *harness) {
			h.feedEvent(t, "event-killing/event.json")
		}, "pod-crash", []store.K8sEvent{storedEvent(t, 1, "event-killing/event.json", nil)}, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			c.run(t, h)
			diff(t, c.wantEvents, h.events(t))
			diff(t, c.wantIncidents, h.incidents(t, c.uid))
			if d := cmp.Diff(c.wantReqs, h.sink.reqs, cmpopts.EquateEmpty()); d != "" {
				t.Error(d)
			}
		})
	}
}
