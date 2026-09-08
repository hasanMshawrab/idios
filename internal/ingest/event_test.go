package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/store"
)

func loadEvent(t *testing.T, path string) *corev1.Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	var ev corev1.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &ev
}

func TestMapEventTimestampPrecedence(t *testing.T) {
	cases := []struct {
		name string
		file string
		want store.K8sEvent
	}{
		{"eventTime and series win", "event-series/event.json", store.K8sEvent{
			ClusterID: 1, EventUID: "ev-series-1", Namespace: "idios-smoke", Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUID: "pod-crash", FieldPath: "spec.containers{api}",
			Reason: "BackOff", Message: "Back-off restarting failed container api in pod web-7d9f8c6b5-abcde_idios-smoke(pod-crash)",
			SourceComponent: "kubelet", Count: 5,
			FirstTS: "2026-08-27T11:55:00.123456Z", LastTS: "2026-08-27T11:59:30.654321Z",
		}},
		{"legacy timestamps and count", "event-legacy/event.json", store.K8sEvent{
			ClusterID: 1, EventUID: "ev-legacy-1", Namespace: "idios-smoke", Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-66c9d-pull1", InvolvedUID: "pod-pull", FieldPath: "spec.containers{api}",
			Reason: "Failed", Message: "Failed to pull image \"registry.example.com/web:does-not-exist\": not found",
			SourceComponent: "kubelet", Count: 3,
			FirstTS: "2026-08-27T11:45:10.000000Z", LastTS: "2026-08-27T11:49:40.000000Z",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := loadEvent(t, c.file)
			raw, err := json.Marshal(ev)
			if err != nil {
				t.Fatal(err)
			}
			c.want.RawJSON = string(raw)
			got, err := MapEvent(ev, 1)
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(c.want, got); d != "" {
				t.Fatal(d)
			}
		})
	}

	t.Run("creationTimestamp is the last resort", func(t *testing.T) {
		ev := loadEvent(t, "event-legacy/event.json")
		ev.FirstTimestamp, ev.LastTimestamp = metav1.Time{}, metav1.Time{}
		ev.Count = 0
		got, err := MapEvent(ev, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got.FirstTS != "2026-08-27T11:45:10.000000Z" || got.LastTS != "2026-08-27T11:45:10.000000Z" || got.Count != 1 {
			t.Fatalf("first=%s last=%s count=%d", got.FirstTS, got.LastTS, got.Count)
		}
	})
}

func TestEarlyCaptureReasonIgnoresType(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
	}{
		{"Killing", true}, {"Preempting", true}, {"Preempted", true}, {"Evicted", true}, {"Unhealthy", true},
		{"BackOff", false}, {"Pulled", false}, {"Scheduled", false}, {"", false},
	}
	for _, c := range cases {
		if got := EarlyCaptureReason(c.reason); got != c.want {
			t.Errorf("EarlyCaptureReason(%q) = %v, want %v", c.reason, got, c.want)
		}
	}
}
