package status

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func TestWriterCountersReportP99OverTheRing(t *testing.T) {
	cases := []struct {
		name      string
		durations []time.Duration
		errAt     []int
		want      Writer
	}{
		{"no transactions", nil, nil, Writer{}},
		{"one transaction is its own p99", []time.Duration{3 * time.Millisecond}, nil, Writer{Transactions: 1, P99Ms: 3}},
		{"errors count and time", []time.Duration{time.Millisecond, 5 * time.Millisecond}, []int{1}, Writer{Transactions: 2, Errors: 1, P99Ms: 5}},
		{"p99 of a hundred is the second largest", ramp(100), nil, Writer{Transactions: 100, P99Ms: 99}},
		{"the ring forgets the oldest", ramp(2 * ringSize), nil, Writer{Transactions: 2 * ringSize, P99Ms: float64(2*ringSize - 10)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cs := New(clock.NewFake(testNow))
			for i, d := range c.durations {
				var err error
				for _, at := range c.errAt {
					if at == i {
						err = os.ErrInvalid
					}
				}
				cs.ObserveTx(d, err)
			}
			if d := cmp.Diff(c.want, cs.Writer()); d != "" {
				t.Fatal(d)
			}
		})
	}
}

// ramp returns 1ms, 2ms, ... n ms.
func ramp(n int) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = time.Duration(i+1) * time.Millisecond
	}
	return out
}

func TestLastSeenIsPerClusterFromTheClock(t *testing.T) {
	clk := clock.NewFake(testNow)
	cs := New(clk)
	cs.ObjectSeen(1)
	clk.Advance(time.Minute)
	cs.ObjectSeen(2)
	cs.HandlerError()
	cs.HandlerPanic()
	cs.HandlerPanic()
	if got, ok := cs.LastSeen(1); !ok || !got.Equal(testNow) {
		t.Errorf("cluster 1 = %v, %v", got, ok)
	}
	if got, ok := cs.LastSeen(2); !ok || !got.Equal(testNow.Add(time.Minute)) {
		t.Errorf("cluster 2 = %v, %v", got, ok)
	}
	if _, ok := cs.LastSeen(3); ok {
		t.Error("cluster 3 was never seen")
	}
	if d := cmp.Diff(Handlers{Errors: 1, Panics: 2}, cs.Handlers()); d != "" {
		t.Fatal(d)
	}
}

func TestSnapshotFileRoundTripsAndIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	want := Snapshot{
		WrittenAt: clock.Format(testNow), PID: 42, Version: "0.0.1-dev",
		Clusters: []Cluster{{ID: 1, Ready: true, LastEventAt: clock.Format(testNow), SkewSeconds: 1.5}},
		Writer:   Writer{Transactions: 10, Errors: 1, P99Ms: 2.5},
		Handlers: Handlers{Errors: 1},
		Capture:  Capture{Queued: 3, Completed: 2, Dropped: 1, Gaps: map[string]uint64{"file": 1, "no_output": 1}},
		Closer:   Closer{LastTickAt: clock.Format(testNow), Closed: 1, Attached: 2},
	}
	if err := WriteFile(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", fi.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the snapshot", len(entries))
	}
}
