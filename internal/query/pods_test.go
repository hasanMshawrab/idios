package query

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

const (
	podCreatedAt = "2026-08-27T11:45:00.000000Z"
	podStartedAt = "2026-08-27T11:45:05.000000Z"
	migrate      = "registry.example.com/migrate:3.1.0"
	envoy        = "registry.example.com/envoy:1.30.0"
)

// seededPodRows is every pod querytest.Seed holds, keyed by uid, with the
// values the ingest fixtures carry. The fake clock stamps last_seen_at one
// second per fixture, so list order is the reverse of the seed's order.
func seededPodRows() map[string]PodRow {
	return map[string]PodRow{
		querytest.CrashPodUID: {Pod: store.Pod{
			UID: querytest.CrashPodUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-7d9f8c6b5-abcde",
			NodeName: sp("node-a"), Phase: "Running", QOSClass: sp("BestEffort"),
			ControllerKind: "ReplicaSet", ControllerName: "web-7d9f8c6b5", ControllerUID: "rs-web-1",
			WorkloadKind: "Deployment", WorkloadName: "web", CreatedAt: podCreatedAt, StartedAt: sp(podStartedAt),
			FirstSeenAt: "2026-08-27T12:00:00.000000Z", LastSeenAt: "2026-08-27T12:00:01.000000Z",
		}, OpenIncidents: 2, ContainerCount: 1, WorstState: store.StateWaiting},

		querytest.OOMPodUID: {Pod: store.Pod{
			UID: querytest.OOMPodUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "mem-hog-5f6d7-qwert",
			NodeName: sp("node-a"), Phase: "Running", QOSClass: sp("Burstable"),
			ControllerKind: "ReplicaSet", ControllerName: "mem-hog-5f6d7", ControllerUID: "rs-memhog-1",
			WorkloadKind: "ReplicaSet", WorkloadName: "mem-hog-5f6d7", CreatedAt: podCreatedAt, StartedAt: sp(podStartedAt),
			FirstSeenAt: "2026-08-27T12:00:02.000000Z", LastSeenAt: "2026-08-27T12:00:03.000000Z",
		}, OpenIncidents: 1, ContainerCount: 1, WorstState: store.StateWaiting},

		querytest.PullPodUID: {Pod: store.Pod{
			UID: querytest.PullPodUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-66c9d-pull1",
			NodeName: sp("node-a"), Phase: "Running", QOSClass: sp("BestEffort"),
			ControllerKind: "ReplicaSet", ControllerName: "web-66c9d", ControllerUID: "rs-web-2",
			WorkloadKind: "ReplicaSet", WorkloadName: "web-66c9d", CreatedAt: podCreatedAt, StartedAt: sp(podStartedAt),
			FirstSeenAt: "2026-08-27T12:00:04.000000Z", LastSeenAt: "2026-08-27T12:00:06.000000Z",
		}, ContainerCount: 1, WorstState: store.StateRunning},

		querytest.UnschedPod: {Pod: store.Pod{
			UID: querytest.UnschedPod, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-2b3c4-pend1",
			NodeName: sp("node-b"), Phase: "Running", QOSClass: sp("Burstable"),
			ControllerKind: "ReplicaSet", ControllerName: "web-2b3c4", ControllerUID: "rs-web-7",
			WorkloadKind: "ReplicaSet", WorkloadName: "web-2b3c4", CreatedAt: podCreatedAt,
			StartedAt:   sp("2026-08-27T11:58:00.000000Z"),
			FirstSeenAt: "2026-08-27T12:00:07.000000Z", LastSeenAt: "2026-08-27T12:00:09.000000Z",
		}, ContainerCount: 1, WorstState: store.StateRunning},

		querytest.MultiPodUID: {Pod: store.Pod{
			UID: querytest.MultiPodUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-4e5f6-multi",
			NodeName: sp("node-a"), Phase: "Running", QOSClass: sp("BestEffort"),
			ControllerKind: "ReplicaSet", ControllerName: "web-4e5f6", ControllerUID: "rs-web-6",
			WorkloadKind: "ReplicaSet", WorkloadName: "web-4e5f6", CreatedAt: podCreatedAt, StartedAt: sp(podStartedAt),
			FirstSeenAt: "2026-08-27T12:00:10.000000Z", LastSeenAt: "2026-08-27T12:00:11.000000Z",
		}, OpenIncidents: 1, ContainerCount: 2, WorstState: store.StateWaiting},

		querytest.JumpPodUID: {Pod: store.Pod{
			UID: querytest.JumpPodUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-7d9f8c6b5-jump1",
			NodeName: sp("node-a"), Phase: "Running", QOSClass: sp("BestEffort"),
			ControllerKind: "ReplicaSet", ControllerName: "web-7d9f8c6b5", ControllerUID: "rs-web-1",
			WorkloadKind: "Deployment", WorkloadName: "web", CreatedAt: podCreatedAt, StartedAt: sp(podStartedAt),
			FirstSeenAt: "2026-08-27T12:00:12.000000Z", LastSeenAt: "2026-08-27T12:00:13.000000Z",
			DeletedAt: sp(deleted), DeletionSource: sp(store.DeletionSourceWatch), DeletionReason: sp(store.DeletionReasonRollout),
		}, ContainerCount: 1, WorstState: store.StateRunning},

		querytest.InitPodUID: {Pod: store.Pod{
			UID: querytest.InitPodUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-9a8b7-init1",
			NodeName: sp("node-a"), Phase: "Running", QOSClass: sp("BestEffort"),
			ControllerKind: "ReplicaSet", ControllerName: "web-9a8b7", ControllerUID: "rs-web-4",
			WorkloadKind: "ReplicaSet", WorkloadName: "web-9a8b7", CreatedAt: podCreatedAt, StartedAt: sp(podStartedAt),
			FirstSeenAt: "2026-08-27T12:00:14.000000Z", LastSeenAt: "2026-08-27T12:00:15.000000Z",
		}, ContainerCount: 2, WorstState: store.StateRunning},

		sidePodUID: {Pod: store.Pod{
			UID: sidePodUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "web-1c2d3-side1",
			NodeName: sp("node-a"), Phase: "Running", QOSClass: sp("BestEffort"),
			ControllerKind: "ReplicaSet", ControllerName: "web-1c2d3", ControllerUID: "rs-web-5",
			WorkloadKind: "ReplicaSet", WorkloadName: "web-1c2d3", CreatedAt: podCreatedAt, StartedAt: sp(podStartedAt),
			FirstSeenAt: "2026-08-27T12:00:16.000000Z", LastSeenAt: "2026-08-27T12:00:16.000000Z",
		}, ContainerCount: 2, WorstState: store.StateRunning},

		querytest.ConfigPodUID: {Pod: store.Pod{
			UID: querytest.ConfigPodUID, ClusterID: querytest.ClusterStaging, Namespace: seedNS, Name: "web-5b8c7-cfg01",
			NodeName: sp("node-a"), Phase: "Pending", QOSClass: sp("BestEffort"),
			ControllerKind: "ReplicaSet", ControllerName: "web-5b8c7", ControllerUID: "rs-web-3",
			WorkloadKind: "ReplicaSet", WorkloadName: "web-5b8c7", CreatedAt: podCreatedAt, StartedAt: sp(podStartedAt),
			FirstSeenAt: "2026-08-27T12:00:17.000000Z", LastSeenAt: "2026-08-27T12:00:18.000000Z",
		}, OpenIncidents: 1, ContainerCount: 1, WorstState: store.StateWaiting},

		querytest.EvictPodUID: {Pod: store.Pod{
			UID: querytest.EvictPodUID, ClusterID: querytest.ClusterStaging, Namespace: seedNS, Name: "web-7d9f8c6b5-evic1",
			NodeName: sp("node-a"), Phase: "Failed", StatusReason: sp("Evicted"),
			StatusMessage: sp("The node was low on resource: memory. Threshold quantity: 100Mi, available: 52Mi. " +
				"Container api was using 900Mi, request is 0, has larger consumption of memory."),
			QOSClass:       sp("BestEffort"),
			ControllerKind: "ReplicaSet", ControllerName: "web-7d9f8c6b5", ControllerUID: "rs-web-1",
			WorkloadKind: "Deployment", WorkloadName: "web", CreatedAt: podCreatedAt, StartedAt: sp(podStartedAt),
			FirstSeenAt: "2026-08-27T12:00:19.000000Z", LastSeenAt: "2026-08-27T12:00:20.000000Z",
		}, OpenIncidents: 1, ContainerCount: 1, WorstState: store.StateTerminated},
	}
}

// sidePodUID is the healthy sidecar pod, which carries no incident and so is
// not one of querytest's incident constants.
const sidePodUID = "pod-side"

// wantPodRows returns the seeded rows for uids, in the order given.
func wantPodRows(t *testing.T, uids ...string) []PodRow {
	t.Helper()
	all := seededPodRows()
	var out []PodRow
	for _, uid := range uids {
		row, ok := all[uid]
		if !ok {
			t.Fatalf("no seeded pod %s", uid)
		}
		out = append(out, row)
	}
	return out
}

func listPods(t *testing.T, db store.Querier, f PodFilter) []PodRow {
	t.Helper()
	rows, truncated, err := ListPods(context.Background(), db, f, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("truncated with no limit")
	}
	return rows
}

// The list is scoped to the selected clusters and the live filter before the
// screen's own filters, and each row carries the open incident count, the
// container count and the state its badge takes: the worst state over the
// containers that serve traffic, which is why a pod whose init container
// ended terminated still reads as running.
func TestListPodsFiltersAndCounts(t *testing.T) {
	st, _ := querytest.Seed(t)
	all := []string{querytest.EvictPodUID, querytest.ConfigPodUID, sidePodUID, querytest.InitPodUID, querytest.JumpPodUID,
		querytest.MultiPodUID, querytest.UnschedPod, querytest.PullPodUID, querytest.OOMPodUID, querytest.CrashPodUID}
	live := []string{querytest.EvictPodUID, querytest.ConfigPodUID, sidePodUID, querytest.InitPodUID,
		querytest.MultiPodUID, querytest.UnschedPod, querytest.PullPodUID, querytest.OOMPodUID, querytest.CrashPodUID}
	cases := []struct {
		name   string
		filter PodFilter
		uids   []string
	}{
		{"live only", PodFilter{Live: bp(true)}, live},
		{"deleted only", PodFilter{Live: bp(false)}, []string{querytest.JumpPodUID}},
		{"live and deleted", PodFilter{}, all},
		{"namespace", PodFilter{Namespace: seedNS}, all},
		{"namespace unwatched", PodFilter{Namespace: "default"}, nil},
		{"workload kind", PodFilter{WorkloadKind: "Deployment"},
			[]string{querytest.EvictPodUID, querytest.JumpPodUID, querytest.CrashPodUID}},
		{"workload name", PodFilter{WorkloadName: "web-4e5f6"}, []string{querytest.MultiPodUID}},
		// The name is exact so that a client holding a name from a truncated
		// list still reaches its row; a prefix is not that name.
		{"pod name", PodFilter{PodName: "web-7d9f8c6b5-abcde"}, []string{querytest.CrashPodUID}},
		{"pod name prefix misses", PodFilter{PodName: "web-7d9f8c6b5"}, nil},
		// No pod of the seed was started by a Job, so the controller uid of
		// the seed's failed Job matches nothing.
		{"job uid no pod carries", PodFilter{JobUID: querytest.FailedJobUID}, nil},
		{"one cluster", PodFilter{ClusterIDs: []int64{querytest.ClusterStaging}},
			[]string{querytest.EvictPodUID, querytest.ConfigPodUID}},
		{"unknown cluster", PodFilter{ClusterIDs: []int64{99}}, nil},
		{"cluster and live", PodFilter{ClusterIDs: []int64{querytest.ClusterProd}, Live: bp(false)},
			[]string{querytest.JumpPodUID}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if diff := cmp.Diff(wantPodRows(t, c.uids...), listPods(t, st.Reader.DB(), c.filter)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// A run's attempts are the pods of its Job, the pruned ones included: the
// filter is the pod's controller uid, so a page keyed by a Job uid reaches
// every attempt and not only the attempts that opened an incident.
func TestListPodsByJobUIDReturnsEveryAttempt(t *testing.T) {
	st, _ := querytest.Seed(t)
	pods := querytest.SeedJobPods(t, st)
	want := []PodRow{
		{Pod: pods[0]},
		{Pod: pods[1], ContainerCount: 1, WorstState: store.StateTerminated},
	}
	if diff := cmp.Diff(want, listPods(t, st.Reader.DB(), PodFilter{JobUID: querytest.FailedJobUID})); diff != "" {
		t.Error(diff)
	}
}

// The detail carries every container of the pod, whatever its kind, in the
// order the pod runs them, with ready as the kubelet meant it per kind: an
// init container that exited 0 is ready and terminated, which no app
// container ever is. Conditions are the latest row per type.
func TestGetPodReturnsContainersOfEveryKind(t *testing.T) {
	st, _ := querytest.Seed(t)
	cases := []struct {
		name string
		uid  string
		want *PodDetail
	}{
		{"no such pod", "pod-gone", nil},

		{"sidecar and app", sidePodUID, &PodDetail{
			Pod: wantPodRows(t, sidePodUID)[0],
			Containers: []store.Container{
				{ID: 10, PodUID: sidePodUID, Name: "proxy", Kind: store.ContainerKindSidecar, Image: envoy,
					ImageTag: sp("1.30.0"), ImageID: sp("registry.example.com/envoy@sha256:6666"), ContainerID: sp("containerd://sss"),
					State: store.StateRunning, Ready: true, RunningSince: sp("2026-08-27T11:45:06.000000Z"),
					UpdatedAt: "2026-08-27T12:00:16.000000Z"},
				{ID: 11, PodUID: sidePodUID, Name: "api", Kind: store.ContainerKindApp, Image: web,
					ImageTag: sp(webTag), ImageID: sp(webID), ContainerID: sp("containerd://aaa3"),
					State: store.StateRunning, Ready: true, RunningSince: sp("2026-08-27T11:45:08.000000Z"),
					UpdatedAt: "2026-08-27T12:00:16.000000Z"},
			},
			SiblingTotal: 1,
		}},

		{"init then app", querytest.InitPodUID, &PodDetail{
			Pod: wantPodRows(t, querytest.InitPodUID)[0],
			Containers: []store.Container{
				{ID: 8, PodUID: querytest.InitPodUID, Name: "init-db", Kind: store.ContainerKindInit, Image: migrate,
					ImageTag: sp("3.1.0"), ImageID: sp("registry.example.com/migrate@sha256:5555"), ContainerID: sp("containerd://iii1"),
					State: store.StateTerminated, Reason: sp("Completed"), ExitCode: ip(0), Signal: ip(0), Ready: true, RestartCount: 1,
					LastTerminatedReason: sp("Error"), LastTerminatedExitCode: ip(1), LastTerminatedSignal: ip(0),
					LastTerminatedAt: sp("2026-08-27T11:45:20.000000Z"), UpdatedAt: "2026-08-27T12:00:15.000000Z"},
				{ID: 9, PodUID: querytest.InitPodUID, Name: "api", Kind: store.ContainerKindApp, Image: web,
					ImageTag: sp(webTag), ImageID: sp(webID), ContainerID: sp("containerd://aaa2"),
					State: store.StateRunning, Ready: true, RunningSince: sp("2026-08-27T11:46:00.000000Z"),
					UpdatedAt: "2026-08-27T12:00:15.000000Z"},
			},
			Incidents:    want(t, 7),
			SiblingTotal: 1,
		}},

		{"captured files, by container then kind then instance", querytest.CrashPodUID, &PodDetail{
			Pod:        wantPodRows(t, querytest.CrashPodUID)[0],
			Containers: crashContainers(),
			Incidents:  want(t, 1, 12),
			Artifacts:  crashArtifacts(),
			Siblings: []SiblingPod{{UID: querytest.JumpPodUID, Name: "web-7d9f8c6b5-jump1", Phase: "Running",
				DeletedAt: sp(deleted), RestartCount: 4, Ready: true}},
			SiblingTotal: 2,
		}},

		{"latest condition per type", querytest.UnschedPod, &PodDetail{
			Pod: wantPodRows(t, querytest.UnschedPod)[0],
			Containers: []store.Container{
				{ID: 4, PodUID: querytest.UnschedPod, Name: "api", Kind: store.ContainerKindApp, Image: web,
					ImageTag: sp(webTag), ImageID: sp(webID), ContainerID: sp("containerd://uuu"),
					CPURequest: sp("8"), CPURequestMillis: ip(8000),
					State: store.StateRunning, Ready: true, RunningSince: sp("2026-08-27T11:58:05.000000Z"),
					UpdatedAt: "2026-08-27T12:00:09.000000Z"},
			},
			Conditions: []store.PodCondition{
				{ID: 2, PodUID: querytest.UnschedPod, Type: "PodScheduled", Status: "True",
					K8sTransitionAt: sp("2026-08-27T11:58:00.000000Z"), ObservedAt: "2026-08-27T12:00:09.000000Z"},
			},
			Incidents:    want(t, 4),
			SiblingTotal: 1,
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := GetPod(context.Background(), st.Reader.DB(), c.uid)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// sibPod builds a pod under the fresh controller rs-sib, in ClusterProd
// unless the caller overrides it, for TestGetPodSiblingsAreOrderedCappedAndCounted.
func sibPod(uid, name, createdAt string, deletedAt *string) store.Pod {
	return store.Pod{
		UID: uid, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: name,
		Phase:          "Running",
		ControllerKind: "ReplicaSet", ControllerName: "sib-5c88f", ControllerUID: "rs-sib",
		WorkloadKind: "Deployment", WorkloadName: "sib",
		CreatedAt: createdAt, FirstSeenAt: "2026-08-27T12:00:00.000000Z", LastSeenAt: "2026-08-27T12:00:00.000000Z",
		DeletedAt: deletedAt,
	}
}

// sibIncident opens category on podUID with the fields the sib fixtures
// share, and returns the incident id.
func sibIncident(t *testing.T, tx *sql.Tx, podUID, category string) int64 {
	t.Helper()
	const t0 = "2026-08-27T12:00:00.000000Z"
	id, err := store.OpenIncident(context.Background(), tx, store.Incident{
		ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod,
		PodUID: &podUID, ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "sib",
		Category: category, FirstReason: "Error", LastReason: "Error", Occurrences: 1,
		OpenedAt: t0, LastSeenAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// The rail orders a pod's siblings with an open incident first, worst
// category first, newest created first inside a category, then live pods
// newest first, then deleted ones, capped at siblingLimit; the total counts
// every pod of the controller regardless of the cap. The order is a property
// of the sibling set, not of which pod is asking, so the live subject and its
// deleted sibling see the same five.
func TestGetPodSiblingsAreOrderedCappedAndCounted(t *testing.T) {
	st, _ := querytest.Seed(t)
	const t0 = "2026-08-27T12:00:00.000000Z"

	ctx := context.Background()
	if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		for _, p := range []store.Pod{
			sibPod("pod-sib-self", "sib-5c88f-self1", "2026-08-27T11:35:00.000000Z", nil),
			sibPod("pod-sib-crash-old", "sib-5c88f-crold", "2026-08-27T11:40:00.000000Z", nil),
			sibPod("pod-sib-crash-new", "sib-5c88f-crnew", "2026-08-27T11:45:00.000000Z", nil),
			sibPod("pod-sib-probe", "sib-5c88f-probe", "2026-08-27T11:50:00.000000Z", nil),
			sibPod("pod-sib-dismissed", "sib-5c88f-dism1", "2026-08-27T11:57:00.000000Z", nil),
			sibPod("pod-sib-live-new", "sib-5c88f-lvnew", "2026-08-27T11:55:00.000000Z", nil),
			sibPod("pod-sib-live-old", "sib-5c88f-lvold", "2026-08-27T11:30:00.000000Z", nil),
			func() store.Pod {
				p := sibPod("pod-sib-gone", "sib-5c88f-gone1", "2026-08-27T11:56:00.000000Z", sp("2026-08-27T11:59:00.000000Z"))
				p.Phase = "Failed"
				return p
			}(),
			func() store.Pod {
				p := sibPod("pod-sib-away", "sib-5c88f-away1", "2026-08-27T11:58:00.000000Z", nil)
				p.ClusterID = querytest.ClusterStaging
				return p
			}(),
			func() store.Pod {
				p := sibPod("pod-sib-lone", "lone", "2026-08-27T11:36:00.000000Z", nil)
				p.ControllerUID, p.ControllerKind, p.WorkloadKind = "", "", "none"
				return p
			}(),
		} {
			if err := store.UpsertPod(ctx, tx, p); err != nil {
				return err
			}
		}
		sibIncident(t, tx, "pod-sib-crash-old", store.CategoryCrash)
		sibIncident(t, tx, "pod-sib-crash-new", store.CategoryCrash)
		sibIncident(t, tx, "pod-sib-probe", store.CategoryProbe)
		dismissed := sibIncident(t, tx, "pod-sib-dismissed", store.CategoryCrash)
		if _, err := store.DismissIncident(ctx, tx, dismissed, t0); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The five the rail shows for either pod under test: pod-sib-live-old and
	// pod-sib-gone fall past the cap, and pod-sib-away never belongs, whoever
	// is asking.
	fiveOrdered := []SiblingPod{
		{UID: "pod-sib-crash-new", Name: "sib-5c88f-crnew", Phase: "Running", WorstOpenCategory: store.CategoryCrash},
		{UID: "pod-sib-crash-old", Name: "sib-5c88f-crold", Phase: "Running", WorstOpenCategory: store.CategoryCrash},
		{UID: "pod-sib-probe", Name: "sib-5c88f-probe", Phase: "Running", WorstOpenCategory: store.CategoryProbe},
		{UID: "pod-sib-dismissed", Name: "sib-5c88f-dism1", Phase: "Running"},
		{UID: "pod-sib-live-new", Name: "sib-5c88f-lvnew", Phase: "Running"},
	}

	type got struct {
		Siblings []SiblingPod
		Total    int64
	}
	cases := []struct {
		name string
		uid  string
		want got
	}{
		{"no controller belongs to no set", "pod-sib-lone", got{nil, 0}},
		{"open worst newest first then live newest first, capped", "pod-sib-self", got{fiveOrdered, 8}},
		{"order is about the siblings, not the pod asking", "pod-sib-gone", got{fiveOrdered, 8}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := GetPod(context.Background(), st.Reader.DB(), c.uid)
			if err != nil {
				t.Fatal(err)
			}
			g := got{d.Siblings, d.SiblingTotal}
			if diff := cmp.Diff(c.want, g); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// The pod's events are every event about it, newest first, including the
// Evicted event that belongs to no incident of this pod: an event nobody
// attached is often the only trace of what happened.
func TestPodEventsIncludesUnattached(t *testing.T) {
	st, _ := querytest.Seed(t)
	want := []store.K8sEvent{
		{ID: 3, ClusterID: querytest.ClusterProd, EventUID: "ev-evicted-1", Namespace: seedNS, Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUID: querytest.CrashPodUID,
			Reason: "Evicted", Message: "The node was low on resource: memory.", SourceComponent: "kubelet", Count: 1,
			FirstTS: "2026-08-27T11:59:55.000000Z", LastTS: "2026-08-27T11:59:55.000000Z", Category: sp(store.CategoryNodePressure)},
		{ID: 2, ClusterID: querytest.ClusterProd, EventUID: "ev-killing-1", Namespace: seedNS, Type: "Normal",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUID: querytest.CrashPodUID,
			FieldPath: "spec.containers{api}", Reason: "Killing", Message: "Stopping container api",
			SourceComponent: "kubelet", Count: 1,
			FirstTS: "2026-08-27T11:59:50.500000Z", LastTS: "2026-08-27T11:59:50.500000Z", IncidentID: ip(12)},
		{ID: 1, ClusterID: querytest.ClusterProd, EventUID: "ev-unhealthy-1", Namespace: seedNS, Type: "Warning",
			InvolvedKind: "Pod", InvolvedName: "web-7d9f8c6b5-abcde", InvolvedUID: querytest.CrashPodUID,
			FieldPath: "spec.containers{api}", Reason: "Unhealthy",
			Message:         "Readiness probe failed: HTTP probe failed with statuscode: 503",
			SourceComponent: "kubelet", Count: 3,
			FirstTS: "2026-08-27T11:58:00.000000Z", LastTS: "2026-08-27T11:59:00.000000Z",
			Category: sp(store.CategoryProbe), IncidentID: ip(12)},
	}
	got, truncated, err := PodEvents(context.Background(), st.Reader.DB(), querytest.CrashPodUID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("truncated with no limit")
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error(diff)
	}
}

// History reads forwards: both lists come back oldest first, and two rows
// written in the same transaction keep the order they were written in, so
// the reconstructed instance of a restart jump precedes the live one.
func TestPodHistoryIsOldestFirst(t *testing.T) {
	st, _ := querytest.Seed(t)
	cases := []struct {
		name string
		uid  string
		want PodHistoryRows
	}{
		{"restart jump", querytest.JumpPodUID, PodHistoryRows{
			Transitions: []store.ContainerStateHistory{
				{ID: 7, PodUID: querytest.JumpPodUID, ContainerName: "api", IncidentID: ip(6), Image: web, ImageID: sp(webID),
					ContainerID: sp("containerd://j3"), State: store.StateTerminated, Reason: sp("Error"), Message: sp("panic: read of a closed queue"),
					ExitCode: ip(1), Signal: ip(0),
					RestartCount: 3, Category: sp(store.CategoryCrash), K8sStartedAt: sp("2026-08-27T11:57:00.000000Z"),
					K8sFinishedAt: sp("2026-08-27T11:58:00.000000Z"), ObservedAt: "2026-08-27T12:00:13.000000Z", GapReconstructed: true},
				{ID: 8, PodUID: querytest.JumpPodUID, ContainerName: "api", Image: web, ImageID: sp(webID),
					ContainerID: sp("containerd://j4"), State: store.StateRunning, RestartCount: 4,
					K8sStartedAt: sp("2026-08-27T11:59:00.000000Z"), ObservedAt: "2026-08-27T12:00:13.000000Z"},
			},
		}},

		{"scheduled at last", querytest.UnschedPod, PodHistoryRows{
			Transitions: []store.ContainerStateHistory{
				{ID: 5, PodUID: querytest.UnschedPod, ContainerName: "api", Image: web, ImageID: sp(webID),
					ContainerID: sp("containerd://uuu"), State: store.StateRunning,
					K8sStartedAt: sp("2026-08-27T11:58:05.000000Z"), ObservedAt: "2026-08-27T12:00:09.000000Z"},
			},
			Conditions: []store.PodCondition{
				{ID: 1, PodUID: querytest.UnschedPod, Type: "PodScheduled", Status: "False", Reason: "Unschedulable",
					Message:         sp("0/3 nodes are available: 3 Insufficient cpu. preemption: 0/3 nodes are available: 3 No preemption victims found for incoming pod."),
					K8sTransitionAt: sp("2026-08-27T11:45:01.000000Z"), ObservedAt: "2026-08-27T12:00:07.000000Z"},
				{ID: 2, PodUID: querytest.UnschedPod, Type: "PodScheduled", Status: "True",
					K8sTransitionAt: sp("2026-08-27T11:58:00.000000Z"), ObservedAt: "2026-08-27T12:00:09.000000Z"},
			},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := PodHistory(context.Background(), st.Reader.DB(), c.uid)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}
