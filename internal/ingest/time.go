package ingest

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func k8sTime(t metav1.Time) string {
	return clock.Format(t.Time)
}

func k8sTimePtr(t *metav1.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := clock.Format(t.Time)
	return &s
}
