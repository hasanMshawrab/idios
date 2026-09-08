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
	imgWeb142  = "registry.example.com/web:1.4.2"
	imgWeb143  = "registry.example.com/web:1.4.3"
	imgWorker  = "registry.example.com/worker@sha256:7777"
	deployment = "Deployment"
	replicaSet = "ReplicaSet"
)

// webWorkload is the Deployment both crash-loop and restart-jump belong to:
// three incidents on two pods, one of them swept away by the deletion.
func webWorkload() WorkloadRow {
	return WorkloadRow{
		ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: deployment, WorkloadName: "web",
		IncidentsByCategory: map[string]int64{store.CategoryCrash: 2, store.CategoryProbe: 1},
		OpenIncidents:       2, Occurrences: 4,
		ImageTags: []TagCount{{Tag: "1.4.2", Count: 3}}, LivePods: 1, DeletedPods: 1,
	}
}

// seededWorkloads is every workload querytest.Seed holds, in the order the
// list returns them. web-1c2d3 is the sidecar pod's ReplicaSet: it has pods
// and no incidents, and the tree must still show it. CronJob report is the
// other half of the union, an incident whose subject is a job and not a pod.
func seededWorkloads() []WorkloadRow {
	return []WorkloadRow{
		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: "CronJob", WorkloadName: "report",
			IncidentsByCategory: map[string]int64{store.CategoryJobFailed: 1}, Occurrences: 1},

		webWorkload(),

		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "mem-hog-5f6d7",
			IncidentsByCategory: map[string]int64{store.CategoryOOM: 1}, OpenIncidents: 1, Occurrences: 1,
			ImageTags: []TagCount{{Tag: "2.0.0", Count: 1}}, LivePods: 1},

		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-1c2d3",
			IncidentsByCategory: map[string]int64{}, LivePods: 1},

		// An unschedulable pod has no image on the incident, so it names no tag.
		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-2b3c4",
			IncidentsByCategory: map[string]int64{store.CategoryScheduling: 1}, Occurrences: 1, LivePods: 1},

		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-4e5f6",
			IncidentsByCategory: map[string]int64{store.CategoryCrash: 1}, OpenIncidents: 1, Occurrences: 1,
			ImageTags: []TagCount{{Tag: "1.4.2", Count: 1}}, LivePods: 1},

		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-66c9d",
			IncidentsByCategory: map[string]int64{store.CategoryImagePull: 1}, Occurrences: 2,
			ImageTags: []TagCount{{Tag: "does-not-exist", Count: 1}}, LivePods: 1},

		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-9a8b7",
			IncidentsByCategory: map[string]int64{store.CategoryCrash: 1}, Occurrences: 1,
			ImageTags: []TagCount{{Tag: "3.1.0", Count: 1}}, LivePods: 1},

		{ClusterID: querytest.ClusterStaging, Namespace: seedNS, WorkloadKind: deployment, WorkloadName: "web",
			IncidentsByCategory: map[string]int64{store.CategoryCrash: 1, store.CategoryNodePressure: 1},
			OpenIncidents:       1, Occurrences: 2,
			ImageTags: []TagCount{{Tag: "1.4.2", Count: 1}}, LivePods: 1},

		{ClusterID: querytest.ClusterStaging, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "web-5b8c7",
			IncidentsByCategory: map[string]int64{store.CategoryConfig: 1}, OpenIncidents: 1, Occurrences: 1,
			ImageTags: []TagCount{{Tag: "1.4.2", Count: 1}}, LivePods: 1},
	}
}

func wantWorkloads(indexes ...int) []WorkloadRow {
	all := seededWorkloads()
	var out []WorkloadRow
	for _, i := range indexes {
		out = append(out, all[i])
	}
	return out
}

// openIncident writes one incident directly, for the shapes the ingest
// fixtures do not produce.
func openIncident(t *testing.T, st *store.Store, inc store.Incident) {
	t.Helper()
	ctx := context.Background()
	if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		_, err := store.OpenIncident(ctx, tx, inc)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// digestPinned adds an incident on an image pinned by digest, which carries
// no tag, to a workload that already failed on tag 1.4.2.
func digestPinned(t *testing.T, st *store.Store) {
	t.Helper()
	at := "2026-08-27T12:01:00.000000Z"
	openIncident(t, st, store.Incident{
		ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod,
		PodUID: sp(querytest.CrashPodUID), ContainerName: "api",
		WorkloadKind: deployment, WorkloadName: "web", Category: store.CategoryOther,
		FirstReason: "Error", LastReason: "Error",
		Image:       sp("registry.example.com/web@sha256:1111"),
		Occurrences: 1, OpenedAt: at, LastSeenAt: at,
	})
}

// A workload is the identity a service keeps while its pods rotate, so the
// list groups on it and unions the two tables that name one.
func TestListWorkloadsGroupsByIdentity(t *testing.T) {
	digestWeb := webWorkload()
	digestWeb.IncidentsByCategory[store.CategoryOther] = 1
	digestWeb.OpenIncidents, digestWeb.Occurrences = 3, 5
	digestWeb.ImageTags = []TagCount{{Tag: "1.4.2", Count: 3}, {Count: 1}}
	digestRows := seededWorkloads()
	digestRows[1] = digestWeb

	cases := []struct {
		name          string
		setup         func(t *testing.T, st *store.Store)
		filter        WorkloadFilter
		page          Page
		want          []WorkloadRow
		wantTruncated bool
	}{
		{name: "an image pinned by digest has no tag", setup: digestPinned, want: digestRows},
		{name: "no namespace matches", filter: WorkloadFilter{Namespace: "staging-web"}},
		{name: "cluster scope to none", filter: WorkloadFilter{ClusterIDs: []int64{99}}},
		{name: "every cluster", want: wantWorkloads(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)},
		{name: "one cluster", filter: WorkloadFilter{ClusterIDs: []int64{querytest.ClusterStaging}},
			want: wantWorkloads(8, 9)},
		{name: "namespace", filter: WorkloadFilter{Namespace: seedNS},
			want: wantWorkloads(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)},
		{name: "limit below the count", page: Page{Limit: 3}, want: wantWorkloads(0, 1, 2), wantTruncated: true},
		{name: "limit equal to the count", page: Page{Limit: 10}, want: wantWorkloads(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)},
		{name: "limit above the count", page: Page{Limit: 11}, want: wantWorkloads(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := querytest.Seed(t)
			if c.setup != nil {
				c.setup(t, st)
			}
			got, truncated, err := ListWorkloads(context.Background(), st.Reader.DB(), c.filter, c.page)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("rows (-want +got):\n%s", diff)
			}
			if truncated != c.wantTruncated {
				t.Errorf("truncated = %v, want %v", truncated, c.wantTruncated)
			}
		})
	}
}

// uids of the controller-less pods TestControllerLessPodsAreOneRowEach adds.
// noneSweptUID is deleted: a pruned pod's row is marked deleted, never
// removed, so incidents.pod_uid's foreign key still resolves, and an
// incident stays open on it.
const (
	noneQuietUID = "pod-none-quiet"
	noneCrashUID = "pod-none-crash"
	noneSweptUID = "pod-none-swept"
)

// seedControllerLessPods adds three controller-less pods, one with an open
// incident and one deleted with an incident still open on it, and returns
// the three workload rows a caller should get back for them, in the
// identity-chain order the list returns.
func seedControllerLessPods(t *testing.T, st *store.Store) []WorkloadRow {
	t.Helper()
	ctx := context.Background()
	pods := []store.Pod{
		{UID: noneCrashUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "one-off-migrate",
			Phase: "Running", WorkloadKind: "none", CreatedAt: podCreatedAt,
			FirstSeenAt: "2026-08-27T13:10:00.000000Z", LastSeenAt: "2026-08-27T13:10:00.000000Z"},
		{UID: noneQuietUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "debug-shell",
			Phase: "Running", WorkloadKind: "none", CreatedAt: podCreatedAt,
			FirstSeenAt: "2026-08-27T13:10:01.000000Z", LastSeenAt: "2026-08-27T13:10:01.000000Z"},
		{UID: noneSweptUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "one-off-cleanup",
			Phase: "Failed", WorkloadKind: "none", CreatedAt: podCreatedAt,
			FirstSeenAt: "2026-08-27T13:10:02.000000Z", LastSeenAt: "2026-08-27T13:10:02.000000Z",
			DeletedAt: sp("2026-08-27T13:10:02.000000Z"), DeletionSource: sp(store.DeletionSourceWatch),
			DeletionReason: sp(store.DeletionReasonUnknown)},
	}
	if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		for _, p := range pods {
			if err := store.UpsertPod(ctx, tx, p); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	at := "2026-08-27T13:10:03.000000Z"
	openIncident(t, st, store.Incident{
		ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod,
		PodUID: sp(noneCrashUID), WorkloadKind: "none", Category: store.CategoryCrash,
		FirstReason: "Error", LastReason: "Error", Occurrences: 1, OpenedAt: at, LastSeenAt: at,
	})
	openIncident(t, st, store.Incident{
		ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod,
		PodUID: sp(noneSweptUID), WorkloadKind: "none", Category: store.CategoryOOM,
		FirstReason: "OOMKilled", LastReason: "OOMKilled", Occurrences: 1, OpenedAt: at, LastSeenAt: at,
	})
	// Split rows tie-break on the pod name, so name order and not insertion
	// order is what the list returns.
	return []WorkloadRow{
		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: "none",
			PodUID: noneQuietUID, PodName: "debug-shell",
			IncidentsByCategory: map[string]int64{}, LivePods: 1},
		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: "none",
			PodUID: noneSweptUID, PodName: "one-off-cleanup",
			IncidentsByCategory: map[string]int64{store.CategoryOOM: 1}, OpenIncidents: 1, Occurrences: 1, DeletedPods: 1},
		{ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: "none",
			PodUID: noneCrashUID, PodName: "one-off-migrate",
			IncidentsByCategory: map[string]int64{store.CategoryCrash: 1}, OpenIncidents: 1, Occurrences: 1, LivePods: 1},
	}
}

// The union used to key every controller-less pod of a namespace on the same
// empty (workload_kind, workload_name), collapsing them into one row with
// nothing to read and nothing to open. Edge cases first: a controller-less
// pod with no incidents at all still gets a row, and a deleted pod keeps its
// own row too, keyed on the uid nothing else in the namespace shares.
func TestControllerLessPodsAreOneRowEach(t *testing.T) {
	st, _ := querytest.Seed(t)
	none := seedControllerLessPods(t, st)
	// none sorts after every real kind alphabetically, and cluster 1 is where
	// it was seeded, so it lands between the ReplicaSet rows and cluster 2.
	want := append(append(wantWorkloads(0, 1, 2, 3, 4, 5, 6, 7), none...), wantWorkloads(8, 9)...)
	got, truncated, err := ListWorkloads(context.Background(), st.Reader.DB(), WorkloadFilter{}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rows (-want +got):\n%s", diff)
	}
	if truncated {
		t.Errorf("truncated = true, want false")
	}
}

// webDetail is the Deployment screen as querytest.Seed leaves it: the newest
// revision first, one restart hour, and the workload's two pods.
//
// The one restart is pod-jump's: its counter goes 3 -> 4 between the row the
// gap reconstructed and the row that observed it running, which also marks
// the hour as partly inferred. pod-crash has a single history row, and a
// container's first row witnesses no restart.
func webDetail(t *testing.T) *WorkloadDetail {
	t.Helper()
	return &WorkloadDetail{
		Workload: webWorkload(),
		Rollouts: []Rollout{
			{ReplicaSetUID: "rs-web-2", ReplicaSetName: "web-8e0a9d7c6", Revision: ip(8),
				Images:    []string{imgWeb143, imgWorker},
				CreatedAt: "2026-08-27T11:58:00.000000Z", Replicas: ip(3), ReadyReplicas: ip(2), AvailableReplicas: ip(2),
				FirstSeenAt: "2026-08-27T12:00:26.000000Z", LastSeenAt: "2026-08-27T12:00:26.000000Z", Incidents: 1},
			{ReplicaSetUID: "rs-web-1", ReplicaSetName: "web-7d9f8c6b5", Revision: ip(7),
				Images:    []string{imgWeb142, imgWorker},
				CreatedAt: "2026-08-27T11:44:00.000000Z", Replicas: ip(3),
				FirstSeenAt: "2026-08-27T12:00:25.000000Z", LastSeenAt: "2026-08-27T12:00:25.000000Z", Incidents: 3},
		},
		RestartsByHour: []HourBucket{{Hour: "2026-08-27T12:00:00.000000Z", Restarts: 1, Reconstructed: true}},
		Pods:           wantPodRows(t, querytest.JumpPodUID, querytest.CrashPodUID),
	}
}

func insertHistory(t *testing.T, st *store.Store, rows ...store.ContainerStateHistory) {
	t.Helper()
	ctx := context.Background()
	err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		for _, h := range rows {
			if err := store.InsertHistory(ctx, tx, h); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The rollout list answers "did failures start after a deploy", and the hour
// buckets answer "is this flapping"; an hour holding a transition inferred
// from a counter jump is marked so the chart can draw it hollow.
func TestGetWorkloadRolloutsAndRestartsPerHour(t *testing.T) {
	webKey := WorkloadKey{ClusterID: querytest.ClusterProd, Namespace: seedNS, Kind: deployment, Name: "web"}
	cases := []struct {
		name  string
		setup func(t *testing.T, st *store.Store)
		key   WorkloadKey
		want  func(t *testing.T) *WorkloadDetail
	}{
		{
			name: "unknown workload",
			key:  WorkloadKey{ClusterID: querytest.ClusterProd, Namespace: seedNS, Kind: deployment, Name: "nothing"},
			want: func(*testing.T) *WorkloadDetail { return nil },
		},
		{
			name: "workload in another cluster",
			key:  WorkloadKey{ClusterID: querytest.ClusterStaging, Namespace: seedNS, Kind: replicaSet, Name: "web-1c2d3"},
			want: func(*testing.T) *WorkloadDetail { return nil },
		},
		{
			name: "deployment",
			key:  webKey,
			want: webDetail,
		},
		{
			// pod-crash's counter stands at 1; the first two rows carry it to 3
			// and then to 5, so their hours hold two restarts each and the
			// second is partly inferred. The third row leaves the counter
			// where it was, so its hour saw no restart and is no bucket.
			name: "increments across hours, one hour reconstructed",
			setup: func(t *testing.T, st *store.Store) {
				insertHistory(t, st,
					store.ContainerStateHistory{PodUID: querytest.CrashPodUID, ContainerName: "api", Image: imgWeb142,
						State: store.StateWaiting, RestartCount: 3, ObservedAt: "2026-08-27T13:30:00.000000Z"},
					store.ContainerStateHistory{PodUID: querytest.CrashPodUID, ContainerName: "api", Image: imgWeb142,
						State: store.StateWaiting, RestartCount: 5, ObservedAt: "2026-08-27T14:15:00.000000Z",
						GapReconstructed: true},
					store.ContainerStateHistory{PodUID: querytest.CrashPodUID, ContainerName: "api", Image: imgWeb142,
						State: store.StateRunning, RestartCount: 5, ObservedAt: "2026-08-27T15:05:00.000000Z",
						GapReconstructed: true})
			},
			key: webKey,
			want: func(t *testing.T) *WorkloadDetail {
				d := webDetail(t)
				d.RestartsByHour = append(d.RestartsByHour,
					HourBucket{Hour: "2026-08-27T13:00:00.000000Z", Restarts: 2},
					HourBucket{Hour: "2026-08-27T14:00:00.000000Z", Restarts: 2, Reconstructed: true})
				return d
			},
		},
		{
			// A ReplicaSet the annotation never numbered has no place in the
			// order, so it goes after every revision that has one.
			name: "a rollout without a revision sorts last",
			setup: func(t *testing.T, st *store.Store) {
				t.Helper()
				ctx := context.Background()
				if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
					return store.UpsertRolloutHistory(ctx, tx, store.RolloutHistory{
						ClusterID: querytest.ClusterProd, Namespace: seedNS, DeploymentName: "web",
						DeploymentUID: "dep-web", ReplicaSetUID: "rs-web-0", ReplicaSetName: "web-000000000",
						ContainerName: "api", Image: imgWeb142, ImageTag: sp("1.4.2"),
						FirstSeenAt: "2026-08-27T12:00:27.000000Z", LastSeenAt: "2026-08-27T12:00:27.000000Z",
					})
				}); err != nil {
					t.Fatal(err)
				}
			},
			key: webKey,
			want: func(t *testing.T) *WorkloadDetail {
				d := webDetail(t)
				d.Rollouts = append(d.Rollouts, Rollout{
					ReplicaSetUID: "rs-web-0", ReplicaSetName: "web-000000000", Images: []string{imgWeb142},
					FirstSeenAt: "2026-08-27T12:00:27.000000Z", LastSeenAt: "2026-08-27T12:00:27.000000Z",
				})
				return d
			},
		},
		{
			// The sidecar pod's ReplicaSet has never failed; the tree must
			// still open it and show what is running.
			name: "workload with pods and no incidents",
			key:  WorkloadKey{ClusterID: querytest.ClusterProd, Namespace: seedNS, Kind: replicaSet, Name: "web-1c2d3"},
			want: func(t *testing.T) *WorkloadDetail {
				return &WorkloadDetail{Workload: seededWorkloads()[3], Pods: wantPodRows(t, sidePodUID)}
			},
		},
		{
			name: "workload that is not a deployment",
			key:  WorkloadKey{ClusterID: querytest.ClusterProd, Namespace: seedNS, Kind: replicaSet, Name: "mem-hog-5f6d7"},
			want: func(t *testing.T) *WorkloadDetail {
				return &WorkloadDetail{Workload: seededWorkloads()[2], Pods: wantPodRows(t, querytest.OOMPodUID)}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := querytest.Seed(t)
			if c.setup != nil {
				c.setup(t, st)
			}
			got, err := GetWorkload(context.Background(), st.Reader.DB(), c.key, nil, Page{})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want(t), got); diff != "" {
				t.Errorf("detail (-want +got):\n%s", diff)
			}
		})
	}
}

// manyPodsKey names a workload querytest.Seed does not hold: this task's test
// adds its pods directly, opt-in, rather than growing the shared seed for one
// scenario.
var manyPodsKey = WorkloadKey{ClusterID: querytest.ClusterProd, Namespace: seedNS, Kind: replicaSet, Name: "many-pods"}

// seedManyPods inserts five pods of manyPodsKey, two live and three deleted,
// newest last_seen_at first, and returns the rows GetWorkload should answer
// for them in that order.
func seedManyPods(t *testing.T, st *store.Store) []PodRow {
	t.Helper()
	ctx := context.Background()
	rows := []PodRow{
		{Pod: store.Pod{UID: "pod-many-1", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "many-pods-1",
			Phase: "Running", ControllerKind: replicaSet, ControllerName: "many-pods", ControllerUID: "rs-many-pods",
			WorkloadKind: replicaSet, WorkloadName: "many-pods", CreatedAt: podCreatedAt,
			FirstSeenAt: "2026-08-27T13:00:00.000000Z", LastSeenAt: "2026-08-27T13:00:05.000000Z"}},
		{Pod: store.Pod{UID: "pod-many-2", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "many-pods-2",
			Phase: "Running", ControllerKind: replicaSet, ControllerName: "many-pods", ControllerUID: "rs-many-pods",
			WorkloadKind: replicaSet, WorkloadName: "many-pods", CreatedAt: podCreatedAt,
			FirstSeenAt: "2026-08-27T13:00:00.000000Z", LastSeenAt: "2026-08-27T13:00:04.000000Z",
			DeletedAt: sp("2026-08-27T13:00:04.000000Z"), DeletionSource: sp(store.DeletionSourceWatch),
			DeletionReason: sp(store.DeletionReasonReplaced)}},
		{Pod: store.Pod{UID: "pod-many-3", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "many-pods-3",
			Phase: "Running", ControllerKind: replicaSet, ControllerName: "many-pods", ControllerUID: "rs-many-pods",
			WorkloadKind: replicaSet, WorkloadName: "many-pods", CreatedAt: podCreatedAt,
			FirstSeenAt: "2026-08-27T13:00:00.000000Z", LastSeenAt: "2026-08-27T13:00:03.000000Z"}},
		{Pod: store.Pod{UID: "pod-many-4", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "many-pods-4",
			Phase: "Running", ControllerKind: replicaSet, ControllerName: "many-pods", ControllerUID: "rs-many-pods",
			WorkloadKind: replicaSet, WorkloadName: "many-pods", CreatedAt: podCreatedAt,
			FirstSeenAt: "2026-08-27T13:00:00.000000Z", LastSeenAt: "2026-08-27T13:00:02.000000Z",
			DeletedAt: sp("2026-08-27T13:00:02.000000Z"), DeletionSource: sp(store.DeletionSourceWatch),
			DeletionReason: sp(store.DeletionReasonReplaced)}},
		{Pod: store.Pod{UID: "pod-many-5", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "many-pods-5",
			Phase: "Running", ControllerKind: replicaSet, ControllerName: "many-pods", ControllerUID: "rs-many-pods",
			WorkloadKind: replicaSet, WorkloadName: "many-pods", CreatedAt: podCreatedAt,
			FirstSeenAt: "2026-08-27T13:00:00.000000Z", LastSeenAt: "2026-08-27T13:00:01.000000Z",
			DeletedAt: sp("2026-08-27T13:00:01.000000Z"), DeletionSource: sp(store.DeletionSourceWatch),
			DeletionReason: sp(store.DeletionReasonReplaced)}},
	}
	err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		for _, r := range rows {
			if err := store.UpsertPod(ctx, tx, r.Pod); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// manyPodsWorkload is manyPodsKey's row: no incidents, two live, three
// deleted, whatever the pod list's own filter narrows.
func manyPodsWorkload() WorkloadRow {
	return WorkloadRow{
		ClusterID: querytest.ClusterProd, Namespace: seedNS, WorkloadKind: replicaSet, WorkloadName: "many-pods",
		IncidentsByCategory: map[string]int64{}, LivePods: 2, DeletedPods: 3,
	}
}

func TestWorkloadPodsAreBoundedAndFilterable(t *testing.T) {
	live := func(b bool) *bool { return &b }
	cases := []struct {
		name string
		live *bool
		page Page
		pods func([]PodRow) []PodRow
		want bool // PodsTruncated
	}{
		{
			name: "no filter and no limit answers every pod",
			pods: func(all []PodRow) []PodRow { return all },
		},
		{
			name: "live true answers only the live pods",
			live: live(true),
			pods: func(all []PodRow) []PodRow { return []PodRow{all[0], all[2]} },
		},
		{
			name: "a limit truncates the unfiltered list",
			page: Page{Limit: 2},
			pods: func(all []PodRow) []PodRow { return all[:2] },
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := querytest.Seed(t)
			all := seedManyPods(t, st)
			got, err := GetWorkload(context.Background(), st.Reader.DB(), manyPodsKey, c.live, c.page)
			if err != nil {
				t.Fatal(err)
			}
			want := &WorkloadDetail{Workload: manyPodsWorkload(), Pods: c.pods(all), PodsTruncated: c.want}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("detail (-want +got):\n%s", diff)
			}
		})
	}
}

// failedJob and completeJob are the two runs querytest.Seed drives through
// the processor, newest start first.
func completeJob() JobRow {
	return JobRow{Job: store.Job{
		UID: "job-report-2", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "report-28812350",
		Succeeded: 1, BackoffLimit: ip(2), Completions: ip(1), Parallelism: ip(1), RestartPolicy: "Never",
		ConditionType: sp("Complete"),
		CreatedAt:     "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:50:02.000000Z"),
		FinishedAt:  sp("2026-08-27T11:52:00.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:23.000000Z", LastSeenAt: "2026-08-27T12:00:24.000000Z",
	}, Complete: true}
}

func failedJob() JobRow {
	return JobRow{Job: store.Job{
		UID: querytest.FailedJobUID, ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "report-28812345",
		CronJobUID: sp("cj-report"), CronJobName: sp("report"), Failed: 3,
		BackoffLimit: ip(2), Completions: ip(1), Parallelism: ip(1),
		ActiveDeadlineSeconds: ip(900), RestartPolicy: "Never",
		ConditionType: sp("Failed"), ConditionReason: sp("BackoffLimitExceeded"),
		ConditionMessage: sp("Job has reached the specified backoff limit"),
		CreatedAt:        "2026-08-27T11:45:00.000000Z", StartedAt: sp("2026-08-27T11:45:02.000000Z"),
		FinishedAt:  sp("2026-08-27T11:57:00.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:21.000000Z", LastSeenAt: "2026-08-27T12:00:22.000000Z",
	}}
}

// runningJob is a run that has not started and carries no condition, the case
// counter arithmetic gets wrong and the NULL comparison would drop.
func runningJob() JobRow {
	return JobRow{Job: store.Job{
		UID: "job-report-3", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "report-28812355",
		CronJobUID: sp("cj-report"), CronJobName: sp("report"), Active: 1, RestartPolicy: "Never",
		CreatedAt:   "2026-08-27T11:59:00.000000Z",
		FirstSeenAt: "2026-08-27T12:00:30.000000Z", LastSeenAt: "2026-08-27T12:00:30.000000Z",
	}}
}

// A job's outcome is read from the condition and never from the counters:
// failed can exceed the backoff limit and says nothing for an OnFailure job.
func TestListJobsCompleteFromCondition(t *testing.T) {
	insertRunning := func(t *testing.T, st *store.Store) {
		t.Helper()
		ctx := context.Background()
		if err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
			return store.UpsertJob(ctx, tx, runningJob().Job)
		}); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name          string
		setup         func(t *testing.T, st *store.Store)
		filter        JobFilter
		page          Page
		want          []JobRow
		wantTruncated bool
	}{
		{name: "unknown cronjob", filter: JobFilter{CronJobUID: "cj-nothing"}},
		{name: "cluster scope to none", filter: JobFilter{ClusterIDs: []int64{querytest.ClusterStaging}}},
		{name: "every job", want: []JobRow{completeJob(), failedJob()}},
		{name: "cronjob", filter: JobFilter{CronJobUID: "cj-report"}, want: []JobRow{failedJob()}},
		// A Job workload's row names the run itself, so the list must be
		// askable by that name and not only by the CronJob above it.
		{name: "job by name", filter: JobFilter{Name: "report-28812345"}, want: []JobRow{failedJob()}},
		{name: "a job name nothing carries", filter: JobFilter{Name: "report-28899999"}},
		{name: "namespace", filter: JobFilter{Namespace: seedNS}, want: []JobRow{completeJob(), failedJob()}},
		{name: "a job that never started sorts last", setup: insertRunning,
			want: []JobRow{completeJob(), failedJob(), runningJob()}},
		{name: "limit below the count", page: Page{Limit: 1}, want: []JobRow{completeJob()}, wantTruncated: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := querytest.Seed(t)
			if c.setup != nil {
				c.setup(t, st)
			}
			got, truncated, err := ListJobs(context.Background(), st.Reader.DB(), c.filter, c.page)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("rows (-want +got):\n%s", diff)
			}
			if truncated != c.wantTruncated {
				t.Errorf("truncated = %v, want %v", truncated, c.wantTruncated)
			}
		})
	}
}

// The runs of CronJob report beyond the failed one the seed holds: a retry
// that then succeeded, a run that is already gone, and the running one.
// cleanupRun is a second CronJob whose only failure is a pod count. None
// come from a fixture, so the rows go in directly.
func reportCompleteRun() JobRow {
	return JobRow{Job: store.Job{
		UID: "job-report-4", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "report-28812360",
		CronJobUID: sp("cj-report"), CronJobName: sp("report"), Succeeded: 1, Failed: 1,
		BackoffLimit: ip(2), Completions: ip(1), Parallelism: ip(1), RestartPolicy: "Never",
		ConditionType: sp("Complete"),
		CreatedAt:     "2026-08-27T11:55:00.000000Z", StartedAt: sp("2026-08-27T11:55:02.000000Z"),
		FinishedAt:  sp("2026-08-27T11:58:00.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:31.000000Z", LastSeenAt: "2026-08-27T12:00:32.000000Z",
	}, Complete: true}
}

func reportDeletedRun() JobRow {
	return JobRow{Job: store.Job{
		UID: "job-report-0", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "report-28812340",
		CronJobUID: sp("cj-report"), CronJobName: sp("report"), Succeeded: 1,
		BackoffLimit: ip(2), Completions: ip(1), Parallelism: ip(1), RestartPolicy: "Never",
		ConditionType: sp("Complete"),
		CreatedAt:     "2026-08-27T11:30:00.000000Z", StartedAt: sp("2026-08-27T11:30:02.000000Z"),
		FinishedAt:  sp("2026-08-27T11:32:00.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:33.000000Z", LastSeenAt: "2026-08-27T12:00:34.000000Z",
		DeletedAt: sp("2026-08-27T12:00:35.000000Z"),
	}, Complete: true}
}

func cleanupRun() JobRow {
	return JobRow{Job: store.Job{
		UID: "job-cleanup-1", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "cleanup-28812345",
		CronJobUID: sp("cj-cleanup"), CronJobName: sp("cleanup"), Succeeded: 1, Failed: 1,
		BackoffLimit: ip(2), Completions: ip(1), Parallelism: ip(1), RestartPolicy: "Never",
		ConditionType: sp("Complete"),
		CreatedAt:     "2026-08-27T11:40:00.000000Z", StartedAt: sp("2026-08-27T11:40:02.000000Z"),
		FinishedAt:  sp("2026-08-27T11:42:00.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:36.000000Z", LastSeenAt: "2026-08-27T12:00:37.000000Z",
	}, Complete: true}
}

func seedJobRuns(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		for _, r := range []JobRow{reportCompleteRun(), reportDeletedRun(), runningJob(), cleanupRun()} {
			if err := store.UpsertJob(ctx, tx, r.Job); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The job list is filtered by the CronJob's name, by whether the run is
// still in the cluster and by the Failed condition, and the counts describe
// the scope a bounded page was cut from: the two flags never narrow them.
// A failed pod count is not a failed run.
func TestListJobsFiltersAndCounts(t *testing.T) {
	reportCounts := JobCounts{Total: 4, Failed: 1, Live: 3}
	allCounts := JobCounts{Total: 6, Failed: 1, Live: 5}
	cases := []struct {
		name          string
		filter        JobFilter
		page          Page
		want          []JobRow
		wantTruncated bool
		wantCounts    JobCounts
	}{
		{name: "unknown cronjob name", filter: JobFilter{CronJobName: "nothing"}},
		{name: "a failed pod count is not a failed run", filter: JobFilter{CronJobName: "cleanup", Failed: true},
			wantCounts: JobCounts{Total: 1, Live: 1}},
		{name: "cronjob name", filter: JobFilter{CronJobName: "report"},
			want:       []JobRow{reportCompleteRun(), failedJob(), reportDeletedRun(), runningJob()},
			wantCounts: reportCounts},
		{name: "live", filter: JobFilter{Live: bp(true)},
			want:       []JobRow{reportCompleteRun(), completeJob(), failedJob(), cleanupRun(), runningJob()},
			wantCounts: allCounts},
		{name: "deleted", filter: JobFilter{Live: bp(false)}, want: []JobRow{reportDeletedRun()}, wantCounts: allCounts},
		{name: "failed runs of one cronjob", filter: JobFilter{CronJobName: "report", Failed: true},
			want: []JobRow{failedJob()}, wantCounts: reportCounts},
		{name: "limit below the count", page: Page{Limit: 2},
			want: []JobRow{reportCompleteRun(), completeJob()}, wantTruncated: true, wantCounts: allCounts},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := querytest.Seed(t)
			seedJobRuns(t, st)
			got, truncated, err := ListJobs(context.Background(), st.Reader.DB(), c.filter, c.page)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("rows (-want +got):\n%s", diff)
			}
			if truncated != c.wantTruncated {
				t.Errorf("truncated = %v, want %v", truncated, c.wantTruncated)
			}
			counts, err := CountJobs(context.Background(), st.Reader.DB(), c.filter)
			if err != nil {
				t.Fatal(err)
			}
			if counts != c.wantCounts {
				t.Errorf("counts = %+v, want %+v", counts, c.wantCounts)
			}
		})
	}
}

// One screen for one Job is keyed by the Job's uid, so the uid is the only
// filter that names a single run. It ands with the rest and, left empty,
// applies nothing. The counts share jobScope, so a predicate added to the
// list and not to them is what these rows catch.
func TestListJobsByUIDAnswersOneRun(t *testing.T) {
	cases := []struct {
		name       string
		filter     JobFilter
		want       []JobRow
		wantCounts JobCounts
	}{
		{name: "a uid nothing carries", filter: JobFilter{UID: "job-report-9"}},
		{name: "a uid outside the namespace it asks", filter: JobFilter{UID: "job-report-4", Namespace: "api"}},
		{name: "empty uid leaves the filter unapplied",
			want: []JobRow{reportCompleteRun(), completeJob(), failedJob(), cleanupRun(), reportDeletedRun(),
				runningJob()},
			wantCounts: JobCounts{Total: 6, Failed: 1, Live: 5}},
		{name: "one run", filter: JobFilter{UID: "job-report-4"},
			want: []JobRow{reportCompleteRun()}, wantCounts: JobCounts{Total: 1, Live: 1}},
		// A run gone from the cluster is still the run its page names.
		{name: "a deleted run", filter: JobFilter{UID: "job-report-0"},
			want: []JobRow{reportDeletedRun()}, wantCounts: JobCounts{Total: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := querytest.Seed(t)
			seedJobRuns(t, st)
			got, truncated, err := ListJobs(context.Background(), st.Reader.DB(), c.filter, Page{})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("rows (-want +got):\n%s", diff)
			}
			if truncated {
				t.Error("truncated = true, want false")
			}
			counts, err := CountJobs(context.Background(), st.Reader.DB(), c.filter)
			if err != nil {
				t.Fatal(err)
			}
			if counts != c.wantCounts {
				t.Errorf("counts = %+v, want %+v", counts, c.wantCounts)
			}
		})
	}
}

// The runs answer "is this recurring" from the workload call itself. Only a
// CronJob and a Job have runs: a CronJob's are the Jobs it created, a Job
// workload is the one run its row names, and no other kind has any. The
// seed's complete run carries no CronJob, so it is nobody's run but its own.
func TestGetWorkloadRunsOfCronJobsAndJobs(t *testing.T) {
	st, _ := querytest.Seed(t)
	ctx := context.Background()
	// A Job workload exists once a pod names one; the seed's job pods hang off
	// the CronJob above the run.
	err := st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		return store.UpsertPod(ctx, tx, store.Pod{
			UID: "pod-run-1", ClusterID: querytest.ClusterProd, Namespace: seedNS, Name: "report-28812345-x1y2z",
			Phase: "Failed", ControllerKind: "Job", ControllerName: "report-28812345",
			ControllerUID: querytest.FailedJobUID, WorkloadKind: "Job", WorkloadName: "report-28812345",
			CreatedAt:   "2026-08-27T11:45:03.000000Z",
			FirstSeenAt: "2026-08-27T12:00:21.000000Z", LastSeenAt: "2026-08-27T12:00:22.000000Z",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		key  WorkloadKey
		want []JobRow
	}{
		{"cronjob", WorkloadKey{ClusterID: querytest.ClusterProd, Namespace: seedNS, Kind: "CronJob", Name: "report"},
			[]JobRow{failedJob()}},
		{"job", WorkloadKey{ClusterID: querytest.ClusterProd, Namespace: seedNS, Kind: "Job", Name: "report-28812345"},
			[]JobRow{failedJob()}},
		{"deployment", WorkloadKey{ClusterID: querytest.ClusterProd, Namespace: seedNS, Kind: deployment, Name: "web"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := GetWorkload(ctx, st.Reader.DB(), c.key, nil, Page{})
			if err != nil {
				t.Fatal(err)
			}
			if d == nil {
				t.Fatal("no workload detail")
			}
			if diff := cmp.Diff(c.want, d.Runs); diff != "" {
				t.Errorf("runs (-want +got):\n%s", diff)
			}
		})
	}
}
