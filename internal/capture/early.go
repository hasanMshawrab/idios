package capture

import (
	"sync"
	"time"
)

const (
	earlyExpiry     = 30 * time.Minute
	earlyMaxPods    = 500
	reasonUnhealthy = "Unhealthy"
)

// held is one early copy of a container's live log and the time it was
// taken. final marks a copy taken on a death announcement; nothing later
// replaces it because nothing later exists. pending marks a fetch in
// flight, so a concurrent request for the same container waits for its
// result instead of fetching too.
type held struct {
	body      []byte
	truncated bool
	at        time.Time
	final     bool
	pending   bool
}

type podEntry struct {
	containers map[string]*held
	touched    time.Time
}

// earlyCache holds early copies keyed by pod uid then container. Debounce
// and "once" are per container because a pod-level event produces one
// request per container at the same instant; expiry and the size bound are
// per pod. Entries survive the pod's deletion: that is when they matter.
type earlyCache struct {
	debounce time.Duration

	mu   sync.Mutex
	pods map[string]*podEntry
}

func newEarlyCache(debounce time.Duration) *earlyCache {
	return &earlyCache{debounce: debounce, pods: map[string]*podEntry{}}
}

// reserve says whether an early request for the container should fetch now
// and, when it should, marks the fetch in flight. The caller ends it with
// put on success or release on failure.
func (c *earlyCache) reserve(podUID, container, reason string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		e = &podEntry{containers: map[string]*held{}, touched: now}
		c.pods[podUID] = e
	}
	h := e.containers[container]
	switch {
	case h == nil:
		e.containers[container] = &held{pending: true}
	case h.pending, h.final:
		return false
	case reason == reasonUnhealthy && now.Sub(h.at) < c.debounce:
		return false
	default:
		h.pending = true
	}
	return true
}

// release ends a reservation whose fetch produced nothing, leaving the
// earlier copy, if there was one, as it was.
func (c *earlyCache) release(podUID, container string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		return
	}
	h := e.containers[container]
	if h == nil {
		return
	}
	if h.body == nil {
		delete(e.containers, container)
		if len(e.containers) == 0 {
			delete(c.pods, podUID)
		}
		return
	}
	h.pending = false
}

// put stores a copy, then drops pods untouched for earlyExpiry and the
// least recently touched pods above earlyMaxPods.
func (c *earlyCache) put(podUID, container, reason string, body []byte, truncated bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		e = &podEntry{containers: map[string]*held{}}
		c.pods[podUID] = e
	}
	e.containers[container] = &held{body: body, truncated: truncated, at: now, final: reason != reasonUnhealthy}
	e.touched = now
	for uid, p := range c.pods {
		if now.Sub(p.touched) > earlyExpiry {
			delete(c.pods, uid)
		}
	}
	// The bound is small enough that a scan per insert costs less than
	// keeping an ordered list in step with every put and take.
	for len(c.pods) > earlyMaxPods {
		var oldest string
		for uid, p := range c.pods {
			if oldest == "" || p.touched.Before(c.pods[oldest].touched) {
				oldest = uid
			}
		}
		delete(c.pods, oldest)
	}
}

// take removes and returns the container's copy.
func (c *earlyCache) take(podUID, container string) (held, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		return held{}, false
	}
	h := e.containers[container]
	if h == nil || h.body == nil {
		return held{}, false
	}
	delete(e.containers, container)
	if len(e.containers) == 0 {
		delete(c.pods, podUID)
	}
	// A copy in the caller's hands is not a fetch in flight, so restoring
	// it must not block the next reservation.
	out := *h
	out.pending = false
	return out, true
}

// restore puts a taken copy back unchanged, keeping its age and final mark
// so a later request sees the same copy take would have seen. No expiry or
// size sweep runs: the entry was already counted when it was put.
func (c *earlyCache) restore(podUID, container string, h held) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.pods[podUID]
	if e == nil {
		e = &podEntry{containers: map[string]*held{}, touched: h.at}
		c.pods[podUID] = e
	}
	e.containers[container] = &h
}

func (c *earlyCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pods)
}
