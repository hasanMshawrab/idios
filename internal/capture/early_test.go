package capture

import (
	"fmt"
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func TestEarlyCacheDebouncesUnhealthyAndTakesFinalReasonsOnce(t *testing.T) {
	c := newEarlyCache(60 * time.Second)
	steps := []struct {
		name      string
		at        time.Duration
		container string
		reason    string
		want      bool
	}{
		{"first unhealthy captures", 0, "api", "Unhealthy", true},
		{"unhealthy inside the window waits", 30 * time.Second, "api", "Unhealthy", false},
		{"sibling container is not blocked", 30 * time.Second, "sidecar", "Unhealthy", true},
		{"unhealthy at the window captures", 60 * time.Second, "api", "Unhealthy", true},
		{"killing bypasses the window", 61 * time.Second, "api", "Killing", true},
		{"second killing is ignored", 62 * time.Second, "api", "Killing", false},
		{"evicted after killing is ignored", 63 * time.Second, "api", "Evicted", false},
		{"unhealthy after killing is ignored", 200 * time.Second, "api", "Unhealthy", false},
	}
	for _, st := range steps {
		now := testNow.Add(st.at)
		got := c.reserve("p1", st.container, st.reason, now)
		if got != st.want {
			t.Errorf("%s: reserve = %v, want %v", st.name, got, st.want)
		}
		if got {
			c.put("p1", st.container, st.reason, []byte(st.name), false, now)
		}
	}
	h, ok := c.take("p1", "api")
	if !ok || string(h.body) != "killing bypasses the window" || !h.final {
		t.Fatalf("api copy = %+v, %v; want the Killing copy, final", h, ok)
	}
	if _, ok := c.take("p1", "api"); ok {
		t.Fatal("take returned the api copy twice")
	}
	if !c.reserve("p1", "api", "Unhealthy", testNow.Add(300*time.Second)) {
		t.Fatal("a taken container does not accept a new capture")
	}
}

func TestReserveAdmitsOneFetchAtATime(t *testing.T) {
	c := newEarlyCache(60 * time.Second)
	steps := []struct {
		name string
		do   func() bool
		want bool
	}{
		{"first request reserves", func() bool { return c.reserve("p1", "api", "Unhealthy", testNow) }, true},
		{"sibling request while in flight is refused", func() bool { return c.reserve("p1", "api", "Unhealthy", testNow) }, false},
		{"a final reason while in flight is refused too", func() bool { return c.reserve("p1", "api", "Killing", testNow) }, false},
		{"release after a failed fetch reopens", func() bool { c.release("p1", "api"); return c.reserve("p1", "api", "Unhealthy", testNow) }, true},
		{"put ends the reservation and starts the window", func() bool {
			c.put("p1", "api", "Unhealthy", []byte("one"), false, testNow)
			return c.reserve("p1", "api", "Unhealthy", testNow.Add(59*time.Second))
		}, false},
		{"at the window a new fetch reserves", func() bool { return c.reserve("p1", "api", "Unhealthy", testNow.Add(60*time.Second)) }, true},
		{"release keeps the earlier copy", func() bool {
			c.release("p1", "api")
			h, ok := c.take("p1", "api")
			return ok && string(h.body) == "one"
		}, true},
		{"a placeholder with no copy is not taken", func() bool { c.reserve("p1", "api", "Unhealthy", testNow); _, ok := c.take("p1", "api"); return ok }, false},
		{"a restored take does not carry the pending mark forward", func() bool {
			c.put("p1", "api", "Unhealthy", []byte("two"), false, testNow)
			c.reserve("p1", "api", "Unhealthy", testNow.Add(60*time.Second))
			h, _ := c.take("p1", "api")
			c.restore("p1", "api", h)
			return c.reserve("p1", "api", "Unhealthy", testNow.Add(120*time.Second))
		}, true},
	}
	for _, st := range steps {
		if got := st.do(); got != st.want {
			t.Errorf("%s: got %v, want %v", st.name, got, st.want)
		}
	}
}

func TestEarlyCacheExpiresAndBoundsPods(t *testing.T) {
	c := newEarlyCache(time.Minute)
	for i := 0; i < earlyMaxPods; i++ {
		c.put(fmt.Sprintf("p%03d", i), "api", "Killing", []byte("x"), false, testNow.Add(time.Duration(i)*time.Second))
	}
	c.put("newest", "api", "Killing", []byte("x"), false, testNow.Add(earlyMaxPods*time.Second))
	if got := c.len(); got != earlyMaxPods {
		t.Fatalf("pods held = %d, want %d", got, earlyMaxPods)
	}
	if _, ok := c.take("p000", "api"); ok {
		t.Fatal("the least recently written pod survived the bound")
	}
	if _, ok := c.take("p001", "api"); !ok {
		t.Fatal("the second oldest pod was evicted")
	}

	c.put("late", "api", "Killing", []byte("x"), false, testNow.Add(earlyExpiry+earlyMaxPods*time.Second))
	if got := c.len(); got != 2 {
		t.Fatalf("pods held after expiry = %d, want 2 (newest, late)", got)
	}
	if _, ok := c.take("newest", "api"); !ok {
		t.Fatal("a pod written inside the expiry window was dropped")
	}
}
