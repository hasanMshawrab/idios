package processor

import (
	"context"
	"testing"

	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/store"
)

type recorder struct{ events []notify.Event }

func (r *recorder) Notify(k notify.Kind, id int64) {
	r.events = append(r.events, notify.Event{Kind: k, ID: id})
}

func (r *recorder) take() []notify.Event {
	out := r.events
	r.events = nil
	return out
}

func incidents(ids ...int64) []notify.Event {
	out := make([]notify.Event, len(ids))
	for i, id := range ids {
		out[i] = notify.Event{Kind: notify.Incident, ID: id}
	}
	return out
}

func TestPodChangesNotifyEveryTouchedIncident(t *testing.T) {
	cases := []struct {
		name      string
		seedClose *string
		steps     []step
		deleteUID string
		want      [][]notify.Event
	}{
		{"an open and an attach are each reported, a healthy step is not", nil,
			steps("image-pull/s1.json", "image-pull/s2.json", "image-pull/s3.json"), "",
			[][]notify.Event{incidents(1), incidents(1), nil}},
		{"two incidents opened in one transaction are both reported", nil,
			steps("evicted/before.json", "evicted/after.json"), "",
			[][]notify.Event{nil, incidents(1, 2)}},
		{"a reopen is reported like any other change", ptr(store.CloseRecovered),
			steps("crash-loop/before.json", "crash-loop/after.json"), "",
			[][]notify.Event{nil, incidents(1)}},
		{"deleting the pod reports the incidents it closed", nil,
			steps("crash-loop/before.json", "crash-loop/after.json"), "pod-crash",
			[][]notify.Event{nil, incidents(1), incidents(1)}},
		{"a fresh unschedulable message on the open incident is a change of its own, and so is scheduling", nil,
			steps("unschedulable/s1.json", "unschedulable/s2.json", "unschedulable/s3.json"), "",
			[][]notify.Event{incidents(1), incidents(1), incidents(1)}},
		{"correcting the workload of an open incident is a change of its own", nil,
			[]step{{"image-pull/s1.json", noOwners{}}, {"image-pull/s1.json", deployOwners{}}}, "",
			[][]notify.Event{incidents(1), incidents(1)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			rec := &recorder{}
			h.p.SetNotifier(rec)
			for i, st := range c.steps {
				if err := h.p.Pod(context.Background(), 1, loadPod(t, st.file), st.r); err != nil {
					t.Fatalf("%s: %v", st.file, err)
				}
				if i == 0 && c.seedClose != nil {
					h.seedIncident(t, "pod-crash", "api", store.CategoryCrash, c.seedClose)
				}
				diff(t, c.want[i], rec.take())
			}
			if c.deleteUID == "" {
				return
			}
			if err := h.p.PodDeleted(context.Background(), c.deleteUID, store.DeletionSourceWatch); err != nil {
				t.Fatal(err)
			}
			diff(t, c.want[len(c.steps)], rec.take())
		})
	}
}

func TestEventJobAndClusterChangesAreNotified(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		prepare func(t *testing.T, h *harness)
		act     func(t *testing.T, h *harness)
		want    []notify.Event
	}{
		{"an event that only links itself leaves the incident row alone",
			func(t *testing.T, h *harness) { h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json")) },
			func(t *testing.T, h *harness) { h.feedEvent(t, "event-series/event.json") },
			nil},
		{"an event opening an incident names the new one",
			func(t *testing.T, h *harness) { h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json")) },
			func(t *testing.T, h *harness) { h.feedEvent(t, "event-unhealthy/event.json") },
			incidents(2)},
		{"an event reopening a closed incident names it",
			func(t *testing.T, h *harness) {
				h.feed(t, steps("crash-loop/before.json", "crash-loop/after.json"))
				h.seedIncident(t, "pod-crash", "api", store.CategoryProbe, ptr(store.CloseRecovered))
			},
			func(t *testing.T, h *harness) { h.feedEvent(t, "event-unhealthy/event.json") },
			incidents(2)},
		{"a failed job opens an incident",
			func(t *testing.T, h *harness) { h.feedJobs(t, "job-failed/before.json") },
			func(t *testing.T, h *harness) { h.feedJobs(t, "job-failed/after.json") },
			incidents(1)},
		{"deleting the job closes it",
			func(t *testing.T, h *harness) { h.feedJobs(t, "job-failed/before.json", "job-failed/after.json") },
			func(t *testing.T, h *harness) {
				if err := h.p.JobDeleted(ctx, "job-report-1"); err != nil {
					t.Fatal(err)
				}
			},
			incidents(1)},
		{"a connection changes the cluster row",
			func(*testing.T, *harness) {},
			func(t *testing.T, h *harness) {
				if err := h.p.ClusterConnected(ctx, 1, ptr("c"), "https://127.0.0.1:26443"); err != nil {
					t.Fatal(err)
				}
			},
			[]notify.Event{{Kind: notify.Cluster, ID: 1}}},
		{"a failure changes the cluster row",
			func(*testing.T, *harness) {},
			func(t *testing.T, h *harness) {
				if err := h.p.ClusterError(ctx, 1, "connection refused"); err != nil {
					t.Fatal(err)
				}
			},
			[]notify.Event{{Kind: notify.Cluster, ID: 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			rec := &recorder{}
			h.p.SetNotifier(rec)
			c.prepare(t, h)
			rec.take()
			c.act(t, h)
			diff(t, c.want, rec.take())
		})
	}
}
