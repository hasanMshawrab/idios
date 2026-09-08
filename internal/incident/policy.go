package incident

import "time"

// Policy is the timing the pure decisions compare against.
type Policy struct {
	SchedulingGrace     time.Duration
	ProbeGrace          time.Duration
	StabilizationWindow time.Duration
}
