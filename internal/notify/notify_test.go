package notify

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestSlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	const sent = 300
	n := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slow := n.Subscribe(ctx)
	live := n.Subscribe(ctx)

	done := make(chan []Event, 1)
	go func() {
		var got []Event
		for i := range sent {
			n.Notify(Incident, int64(i))
			got = append(got, <-live)
		}
		done <- got
	}()
	var liveGot []Event
	select {
	case liveGot = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Notify blocked on a subscriber that never reads")
	}
	want := make([]Event, sent)
	for i := range want {
		want[i] = Event{Kind: Incident, ID: int64(i)}
	}
	if d := cmp.Diff(want, liveGot); d != "" {
		t.Error(d)
	}

	var slowGot []Event
	for ev := range slow {
		slowGot = append(slowGot, ev)
	}
	if len(slowGot) != buffer {
		t.Errorf("the dropped subscriber read %d events before its channel closed, want the %d it had buffered", len(slowGot), buffer)
	}
}

func TestSubscribeClosesWhenContextEnds(t *testing.T) {
	n := New()
	ctx, cancel := context.WithCancel(context.Background())
	ch := n.Subscribe(ctx)
	n.Notify(Cluster, 7)
	cancel()

	var got []Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			got = append(got, ev)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the channel was not closed when the context ended")
	}
	if d := cmp.Diff([]Event{{Kind: Cluster, ID: 7}}, got); d != "" {
		t.Error(d)
	}
}
