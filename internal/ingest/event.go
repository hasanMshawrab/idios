package ingest

import (
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// MapEvent maps a core/v1 Event to its row. Category is left nil; the
// caller classifies. Newer components fill eventTime and series with
// microseconds and leave the whole-second pair empty, so the first non-zero
// source wins in that order.
func MapEvent(ev *corev1.Event, clusterID int64) (store.K8sEvent, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return store.K8sEvent{}, fmt.Errorf("marshal event %s: %w", ev.UID, err)
	}
	row := store.K8sEvent{
		ClusterID: clusterID, EventUID: string(ev.UID), Namespace: ev.Namespace, Type: ev.Type,
		InvolvedKind: ev.InvolvedObject.Kind, InvolvedName: ev.InvolvedObject.Name, InvolvedUID: string(ev.InvolvedObject.UID),
		FieldPath: ev.InvolvedObject.FieldPath, Reason: ev.Reason, Message: ev.Message,
		SourceComponent: ev.Source.Component, Count: 1, RawJSON: string(raw),
	}
	if row.SourceComponent == "" {
		row.SourceComponent = ev.ReportingController
	}
	switch {
	case ev.Series != nil && ev.Series.Count > 0:
		row.Count = int64(ev.Series.Count)
	case ev.Count > 0:
		row.Count = int64(ev.Count)
	}
	created := ev.CreationTimestamp.Time
	row.FirstTS = clock.Format(firstNonZero(ev.EventTime.Time, ev.FirstTimestamp.Time, created))
	last := []time.Time{ev.EventTime.Time, ev.LastTimestamp.Time, ev.FirstTimestamp.Time, created}
	if ev.Series != nil {
		last = append([]time.Time{ev.Series.LastObservedTime.Time}, last...)
	}
	row.LastTS = clock.Format(firstNonZero(last...))
	return row, nil
}

func firstNonZero(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// EarlyCaptureReason says whether an event announces a pod's imminent
// removal or a probe failure. It matches the reason only: Killing and
// Preempting are Normal events, and a type filter would drop them.
func EarlyCaptureReason(reason string) bool {
	switch reason {
	case "Killing", "Preempted", "Preempting", "Evicted", "Unhealthy":
		return true
	}
	return false
}
