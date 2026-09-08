// Package processor turns one informer callback into one write transaction:
// load the snapshot rows, diff, decide incidents, execute, and after commit
// hand capture requests to the sink. Diffing and lifecycle decisions live in
// ingest and incident; this package only sequences them.
package processor

import (
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/store"
)

// Capture triggers name why a request was made. Early triggers carry the
// event reason after the prefix.
const (
	TriggerRestart      = "restart"
	TriggerIncidentOpen = "incident_open"
	TriggerDelete       = "delete"
	TriggerEarlyPrefix  = "early:"
)

// CaptureRequest asks the capture pool for one artifact. RestartCount is the
// dead-instance index for log_previous and store.NoRestartIndex otherwise;
// Previous says the dead instance is in lastState. Pod is set for pod_json
// only.
type CaptureRequest struct {
	ClusterID    int64
	Namespace    string
	PodUID       string
	PodName      string
	Container    string
	Kind         string
	RestartCount int64
	Previous     bool
	Trigger      string
	IncidentID   *int64
	Pod          *corev1.Pod
}

// CaptureSink receives requests after the transaction that produced them
// committed. The capture pool implements it.
type CaptureSink interface {
	Enqueue(CaptureRequest)
}

// Notifier is told the id of every row a committed transaction changed.
type Notifier interface {
	Notify(notify.Kind, int64)
}

// Processor handles every watched object kind through one Writer.
type Processor struct {
	w      *store.Writer
	clk    clock.Clock
	sink   CaptureSink
	pol    incident.Policy
	events Notifier
}

// New returns a Processor. pol.StabilizationWindow bounds how far back a
// newly opened incident adopts earlier events.
func New(w *store.Writer, clk clock.Clock, sink CaptureSink, pol incident.Policy) *Processor {
	return &Processor{w: w, clk: clk, sink: sink, pol: pol}
}

// SetNotifier makes the processor report the rows it changed. Without one
// it records silently. Call it before the processor runs: the field is not
// synchronized.
func (p *Processor) SetNotifier(n Notifier) { p.events = n }

// notifyIncidents reports the incidents one committed transaction touched.
// A reader loads the row on receipt, so this must run after the commit or
// it would send the reader back to the row as it was.
func (p *Processor) notifyIncidents(ids []int64) {
	if p.events == nil {
		return
	}
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		p.events.Notify(notify.Incident, id)
	}
}

func (p *Processor) notifyCluster(id int64) {
	if p.events == nil {
		return
	}
	p.events.Notify(notify.Cluster, id)
}

func ptr[T any](v T) *T { return &v }
