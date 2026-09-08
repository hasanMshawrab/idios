// Package notify is the in-process broadcast of "this row changed". An
// event carries an id, never a row: the reader loads the current row, so a
// producer never holds a snapshot for a consumer.
package notify

import (
	"context"
	"sync"
)

// Kind names the row set an event belongs to.
type Kind int

// The row sets that are broadcast.
const (
	Incident Kind = iota
	Cluster
)

// Event names one row that changed.
type Event struct {
	Kind Kind
	ID   int64
}

// buffer bounds how far a subscriber may fall behind before it is dropped.
const buffer = 256

// Notifier broadcasts to whoever is subscribed now and keeps no history: a
// subscriber reloads the list it watches when it connects and after a
// drop, so the events it did not see change nothing it ends up showing.
type Notifier struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// New returns a Notifier with no subscribers.
func New() *Notifier {
	return &Notifier{subs: map[chan Event]struct{}{}}
}

// Notify broadcasts one changed row. A nil Notifier drops it, so a caller
// that runs without one needs no check of its own.
func (n *Notifier) Notify(k Kind, id int64) {
	if n == nil {
		return
	}
	ev := Event{Kind: k, ID: id}
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subs {
		select {
		case ch <- ev:
		default:
			// The producers call this while recording; waiting on a slow
			// reader would stall the recorder, and the reader loses nothing
			// a reload does not return.
			delete(n.subs, ch)
			close(ch)
		}
	}
}

// Subscribe returns a channel of events, closed when ctx ends or when the
// subscriber falls more than the buffer behind. A nil Notifier returns a
// closed channel, so a reader of one ends at once instead of hanging.
func (n *Notifier) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, buffer)
	if n == nil {
		close(ch)
		return ch
	}
	n.mu.Lock()
	n.subs[ch] = struct{}{}
	n.mu.Unlock()
	go func() {
		<-ctx.Done()
		n.drop(ch)
	}()
	return ch
}

func (n *Notifier) drop(ch chan Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.subs[ch]; !ok {
		return
	}
	delete(n.subs, ch)
	close(ch)
}
