package query

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

func sp(v string) *string { return &v }
func ip(v int64) *int64   { return &v }

const (
	seedNS  = "idios-smoke"
	web     = "registry.example.com/web:1.4.2"
	webID   = "registry.example.com/web@sha256:1111"
	webTag  = "1.4.2"
	actedAt = "2026-08-27T12:00:31.000000Z"
	deleted = "2026-08-27T12:00:30.000000Z"
)

// seeded is every incident querytest.Seed holds, keyed by id, with the values
// the ingest fixtures carry. Order of the seeded list is by last_seen_at then
// id, both descending: 1, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2.
func seeded() map[int64]IncidentRow {
	rows := map[int64]IncidentRow{
		1: {Incident: store.Incident{
			ID: 1, ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.CrashPodUID),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash,
			FirstReason: "CrashLoopBackOff", LastReason: "CrashLoopBackOff", Image: sp(web), ImageTag: sp(webTag), ImageID: sp(webID),
			Occurrences: 1, OpenedAt: "2026-08-27T11:55:00.000000Z", LastSeenAt: actedAt,
		}, State: "open", PodName: sp("web-7d9f8c6b5-abcde"), ContainerCount: 1, ExitCode: ip(1), Signal: ip(0)},

		2: {Incident: store.Incident{
			ID: 2, ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.OOMPodUID),
			ContainerName: "api", WorkloadKind: "ReplicaSet", WorkloadName: "mem-hog-5f6d7", Category: store.CategoryOOM,
			FirstReason: "OOMKilled", LastReason: "OOMKilled", Image: sp("registry.example.com/mem-hog:2.0.0"), ImageTag: sp("2.0.0"),
			ImageID: sp("registry.example.com/mem-hog@sha256:2222"), Occurrences: 1,
			OpenedAt: "2026-08-27T11:52:30.000000Z", LastSeenAt: "2026-08-27T12:00:03.000000Z", AcknowledgedAt: sp(actedAt),
		}, State: "acknowledged", PodName: sp("mem-hog-5f6d7-qwert"), ContainerCount: 1, ExitCode: ip(137), Signal: ip(9)},

		3: {Incident: store.Incident{
			ID: 3, ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.PullPodUID),
			ContainerName: "api", WorkloadKind: "ReplicaSet", WorkloadName: "web-66c9d", Category: store.CategoryImagePull,
			FirstReason: "ErrImagePull", LastReason: "ImagePullBackOff", Image: sp("registry.example.com/web:does-not-exist"),
			ImageTag: sp("does-not-exist"), Occurrences: 2,
			OpenedAt: "2026-08-27T11:45:00.000000Z", LastSeenAt: "2026-08-27T12:00:05.000000Z", DismissedAt: sp(actedAt),
		}, State: "dismissed", PodName: sp("web-66c9d-pull1"), ContainerCount: 1},

		4: {Incident: store.Incident{
			ID: 4, ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.UnschedPod),
			WorkloadKind: "ReplicaSet", WorkloadName: "web-2b3c4", Category: store.CategoryScheduling,
			FirstReason: "Unschedulable", LastReason: "Unschedulable",
			LastMessage: sp("0/4 nodes are available: 4 Insufficient cpu. preemption: 0/4 nodes are available: 4 No preemption victims found for incoming pod."),
			Occurrences: 1, OpenedAt: "2026-08-27T11:45:01.000000Z", LastSeenAt: "2026-08-27T12:00:07.000000Z",
			ClosedAt: sp("2026-08-27T12:00:09.000000Z"), CloseReason: sp(store.CloseRecovered),
		}, State: "recovered", PodName: sp("web-2b3c4-pend1"), ContainerCount: 1},

		5: {Incident: store.Incident{
			ID: 5, ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.MultiPodUID),
			ContainerName: "worker", WorkloadKind: "ReplicaSet", WorkloadName: "web-4e5f6", Category: store.CategoryCrash,
			FirstReason: "CrashLoopBackOff", LastReason: "CrashLoopBackOff", Image: sp("registry.example.com/worker:1.4.2"),
			ImageTag: sp(webTag), ImageID: sp("registry.example.com/worker@sha256:7777"), Occurrences: 1,
			OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: "2026-08-27T12:00:11.000000Z",
		}, State: "open", PodName: sp("web-4e5f6-multi"), ContainerCount: 2, ExitCode: ip(2), Signal: ip(0)},

		6: {Incident: store.Incident{
			ID: 6, ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.JumpPodUID),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash,
			FirstReason: "CrashLoopBackOff", LastReason: "Error", LastMessage: sp("panic: read of a closed queue"),
			Image: sp(web), ImageTag: sp(webTag), ImageID: sp(webID),
			Occurrences: 2, OpenedAt: "2026-08-27T11:40:00.000000Z", LastSeenAt: "2026-08-27T12:00:13.000000Z",
			ClosedAt: sp(deleted), CloseReason: sp(store.ClosePodDeleted),
		}, State: "pod_deleted", PodName: sp("web-7d9f8c6b5-jump1"), PodDeletedAt: sp(deleted),
			PodDeletionReason: sp(store.DeletionReasonRollout), ContainerCount: 1, ExitCode: ip(1), Signal: ip(0)},

		7: {Incident: store.Incident{
			ID: 7, ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.InitPodUID),
			ContainerName: "init-db", WorkloadKind: "ReplicaSet", WorkloadName: "web-9a8b7", Category: store.CategoryCrash,
			FirstReason: "Error", LastReason: "Error", Image: sp("registry.example.com/migrate:3.1.0"), ImageTag: sp("3.1.0"),
			ImageID: sp("registry.example.com/migrate@sha256:5555"), Occurrences: 1,
			OpenedAt: "2026-08-27T11:45:00.000000Z", LastSeenAt: "2026-08-27T12:00:14.000000Z",
			ClosedAt: sp(actedAt), CloseReason: sp(store.CloseRecovered), DismissedAt: sp(actedAt),
		}, State: "dismissed", PodName: sp("web-9a8b7-init1"), ContainerCount: 2, ExitCode: ip(1), Signal: ip(0)},

		8: {Incident: store.Incident{
			ID: 8, ClusterID: querytest.ClusterStaging, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.ConfigPodUID),
			ContainerName: "api", WorkloadKind: "ReplicaSet", WorkloadName: "web-5b8c7", Category: store.CategoryConfig,
			FirstReason: "CreateContainerConfigError", LastReason: "CreateContainerConfigError", Image: sp(web), ImageTag: sp(webTag),
			Occurrences: 1, OpenedAt: "2026-08-27T12:00:18.000000Z", LastSeenAt: "2026-08-27T12:00:18.000000Z",
		}, State: "open", PodName: sp("web-5b8c7-cfg01"), ContainerCount: 1},

		9: {Incident: store.Incident{
			ID: 9, ClusterID: querytest.ClusterStaging, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.EvictPodUID),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryCrash,
			FirstReason: "Error", LastReason: "Error", Image: sp(web), ImageTag: sp(webTag), ImageID: sp(webID), Occurrences: 1,
			OpenedAt: "2026-08-27T11:57:30.000000Z", LastSeenAt: "2026-08-27T12:00:20.000000Z",
		}, State: "open", PodName: sp("web-7d9f8c6b5-evic1"), ContainerCount: 1, ExitCode: ip(137), Signal: ip(9)},

		10: {Incident: store.Incident{
			ID: 10, ClusterID: querytest.ClusterStaging, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.EvictPodUID),
			WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryNodePressure,
			FirstReason: "Evicted", LastReason: "Evicted",
			LastMessage: sp("The node was low on resource: memory. Threshold quantity: 100Mi, available: 52Mi. Container api was using 900Mi, request is 0, has larger consumption of memory."),
			Occurrences: 1, OpenedAt: "2026-08-27T12:00:20.000000Z", LastSeenAt: "2026-08-27T12:00:20.000000Z",
			ClosedAt: sp(actedAt), CloseReason: sp(store.CloseManual),
		}, State: "manual", PodName: sp("web-7d9f8c6b5-evic1"), ContainerCount: 1},

		11: {Incident: store.Incident{
			ID: 11, ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectJob, JobUID: sp(querytest.FailedJobUID),
			WorkloadKind: "CronJob", WorkloadName: "report", Category: store.CategoryJobFailed,
			FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded",
			LastMessage: sp("Job has reached the specified backoff limit"), Occurrences: 1,
			OpenedAt: "2026-08-27T11:57:00.000000Z", LastSeenAt: "2026-08-27T12:00:22.000000Z",
			ClosedAt: sp(actedAt), CloseReason: sp(store.CloseJobFinished),
		}, State: "job_finished"},

		12: {Incident: store.Incident{
			ID: 12, ClusterID: querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod, PodUID: sp(querytest.CrashPodUID),
			ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "web", Category: store.CategoryProbe,
			FirstReason: "Unhealthy", LastReason: "Unhealthy",
			LastMessage: sp("Readiness probe failed: HTTP probe failed with statuscode: 503"),
			Image:       sp(web), ImageTag: sp(webTag), ImageID: sp(webID), Occurrences: 1,
			OpenedAt: "2026-08-27T11:59:00.000000Z", LastSeenAt: "2026-08-27T12:00:27.000000Z",
		}, State: "open", PodName: sp("web-7d9f8c6b5-abcde"), ContainerCount: 1, ExitCode: ip(1), Signal: ip(0)},
	}
	// The node is denormalised onto the incident at open, and every seeded pod
	// is placed on node-a. Incident 4 opened while its pod was unschedulable and
	// 11 is a job incident with no pod at all, so neither names a node and
	// neither is back-filled with one.
	for id, r := range rows {
		if id == 4 || id == 11 {
			continue
		}
		r.NodeName = sp("node-a")
		rows[id] = r
	}
	return rows
}

// want returns the seeded rows for ids, in the order given.
func want(t *testing.T, ids ...int64) []IncidentRow {
	t.Helper()
	all := seeded()
	var out []IncidentRow
	for _, id := range ids {
		row, ok := all[id]
		if !ok {
			t.Fatalf("no seeded incident %d", id)
		}
		out = append(out, row)
	}
	return out
}

func list(t *testing.T, db store.Querier, f IncidentFilter, p Page) ([]IncidentRow, bool) {
	t.Helper()
	rows, truncated, err := ListIncidents(context.Background(), db, f, p)
	if err != nil {
		t.Fatal(err)
	}
	return rows, truncated
}

// A row's state is open until acknowledged, then its close_reason once
// closed, and dismissed whatever else holds.
func TestListIncidentsDerivesState(t *testing.T) {
	cases := []struct {
		name string
		// AttentionSince is empty for every ordinary state row; only the
		// attention rows below set it.
		state          string
		attentionSince string
		// mutate runs against the row's own freshly seeded store before the
		// list call, so the acknowledged-closed-row case cannot leak its
		// UPDATE into any other row's assertions.
		mutate func(t *testing.T, st *store.Store)
		ids    []int64
	}{
		{name: "open", state: "open", ids: []int64{1, 12, 9, 8, 5}},
		{name: "acknowledged", state: "acknowledged", ids: []int64{2}},
		{name: "recovered", state: "recovered", ids: []int64{4}},
		{name: "pod_deleted", state: "pod_deleted", ids: []int64{6}},
		{name: "manual", state: "manual", ids: []int64{10}},
		{name: "job_finished", state: "job_finished", ids: []int64{11}},
		{name: "dismissed", state: "dismissed", ids: []int64{7, 3}},
		// Attention is never a row's own state: the filter is open, or closed
		// within the window and never acknowledged or dismissed. A dismissed row
		// is excluded whether or not it ever closed (3 is open and dismissed; 7
		// is closed and dismissed), which is why the predicate gates dismissal
		// ahead of the open/closed split rather than only inside the closed leg.
		{name: "attention before every close", state: StateAttention,
			attentionSince: "2026-08-27T00:00:00.000000Z",
			ids:            []int64{1, 12, 11, 10, 9, 8, 6, 5, 4, 2}},
		// Incident 4 closed at 12:00:09.000000Z, incident 6 at 12:00:30.000000Z.
		{name: "attention cutoff between two closes drops the earlier one", state: StateAttention,
			attentionSince: "2026-08-27T12:00:10.000000Z",
			ids:            []int64{1, 12, 11, 10, 9, 8, 6, 5, 2}},
		{name: "acknowledging a closed row drops it from attention", state: StateAttention,
			attentionSince: "2026-08-27T00:00:00.000000Z",
			mutate: func(t *testing.T, st *store.Store) {
				const closeOf6 = "2026-08-27T12:00:30.000000Z" // incident 6's closed_at, seeded by querytest.Seed
				err := st.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
					_, err := tx.ExecContext(context.Background(),
						`UPDATE incidents SET acknowledged_at = ? WHERE id = 6`, closeOf6)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
			},
			ids: []int64{1, 12, 11, 10, 9, 8, 5, 4, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := querytest.Seed(t)
			if c.mutate != nil {
				c.mutate(t, st)
			}
			got, truncated := list(t, st.Reader.DB(),
				IncidentFilter{State: c.state, AttentionSince: c.attentionSince}, Page{})
			if truncated {
				t.Error("truncated with no limit")
			}
			if diff := cmp.Diff(want(t, c.ids...), got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// Every list is scoped to the selected clusters before anything else, then
// narrowed by the filters the triage list offers. The rows come back newest
// activity first, so id order and list order diverge.
func TestListIncidentsFilters(t *testing.T) {
	st, _ := querytest.Seed(t)
	all := []int64{1, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2}
	cases := []struct {
		name   string
		filter IncidentFilter
		ids    []int64
	}{
		{"no filter", IncidentFilter{}, all},
		{"category", IncidentFilter{Category: store.CategoryOOM}, []int64{2}},
		{"namespace", IncidentFilter{Namespace: seedNS}, all},
		{"namespace unwatched", IncidentFilter{Namespace: "default"}, nil},
		{"workload kind", IncidentFilter{WorkloadKind: "CronJob"}, []int64{11}},
		{"workload name", IncidentFilter{WorkloadName: "web-4e5f6"}, []int64{5}},
		{"pod uid", IncidentFilter{PodUID: querytest.CrashPodUID}, []int64{1, 12}},
		{"job uid", IncidentFilter{JobUID: querytest.FailedJobUID}, []int64{11}},
		// The node is stamped on the incident at open, so the filter answers
		// "what else broke on that machine" in one query. It finds placed pods
		// only: incident 4 opened while its pod was unschedulable and 11 is a
		// job incident with no pod, so neither names a node.
		{"node name", IncidentFilter{NodeName: "node-a"}, []int64{1, 12, 10, 9, 8, 7, 6, 5, 3, 2}},
		{"a node no incident opened on", IncidentFilter{NodeName: "node-b"}, nil},
		// The name is exact so that a client holding a name from a truncated
		// list still reaches its rows; a prefix is not that name.
		{"pod name", IncidentFilter{PodName: "web-7d9f8c6b5-abcde"}, []int64{1, 12}},
		{"pod name prefix misses", IncidentFilter{PodName: "web-7d9f8c6b5"}, nil},
		{"node name and category", IncidentFilter{NodeName: "node-a", Category: store.CategoryOOM}, []int64{2}},
		{"one cluster", IncidentFilter{ClusterIDs: []int64{querytest.ClusterProd}}, []int64{1, 12, 11, 7, 6, 5, 4, 3, 2}},
		{"both clusters", IncidentFilter{ClusterIDs: []int64{querytest.ClusterProd, querytest.ClusterStaging}}, all},
		{"unknown cluster", IncidentFilter{ClusterIDs: []int64{99}}, nil},
		{"cluster and category", IncidentFilter{ClusterIDs: []int64{querytest.ClusterStaging}, Category: store.CategoryCrash}, []int64{9}},
		{"one id", IncidentFilter{ID: 5}, []int64{5}},
		{"an id outside the cluster scope", IncidentFilter{ID: 5, ClusterIDs: []int64{querytest.ClusterStaging}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := list(t, st.Reader.DB(), c.filter, Page{})
			if diff := cmp.Diff(c.ids, ids(got)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// ids reduces rows to what a selection test is about.
func ids(rows []IncidentRow) []int64 {
	var out []int64
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// Each row carries the pod's name and deletion facts, the pod's container
// count, and the subject container's last exit code and signal.
func TestListIncidentsJoinsPodFacts(t *testing.T) {
	st, _ := querytest.Seed(t)
	cases := []struct {
		name   string
		filter IncidentFilter
		ids    []int64
	}{
		{"live pod", IncidentFilter{PodUID: querytest.CrashPodUID}, []int64{1, 12}},
		{"deleted pod", IncidentFilter{PodUID: querytest.JumpPodUID}, []int64{6}},
		{"pod level incident has no container", IncidentFilter{PodUID: querytest.UnschedPod}, []int64{4}},
		{"terminated container with no previous run", IncidentFilter{PodUID: querytest.EvictPodUID, Category: store.CategoryCrash}, []int64{9}},
		{"job incident has no pod", IncidentFilter{JobUID: querytest.FailedJobUID}, []int64{11}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := list(t, st.Reader.DB(), c.filter, Page{})
			if diff := cmp.Diff(want(t, c.ids...), got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// A list says when the limit was hit, and returns no more than the limit.
func TestListIncidentsTruncatesAtLimit(t *testing.T) {
	st, _ := querytest.Seed(t)
	cases := []struct {
		name      string
		limit     int
		ids       []int64
		truncated bool
	}{
		{"below the count", 1, []int64{1}, true},
		{"equal to the count", 2, []int64{1, 12}, false},
		{"above the count", 3, []int64{1, 12}, false},
		{"no limit", 0, []int64{1, 12}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, truncated := list(t, st.Reader.DB(), IncidentFilter{PodUID: querytest.CrashPodUID}, Page{Limit: c.limit})
			if truncated != c.truncated {
				t.Errorf("truncated = %v, want %v", truncated, c.truncated)
			}
			if diff := cmp.Diff(want(t, c.ids...), got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

// Every column of the incidents table reaches the row, so a column added to
// the schema cannot silently stop at the database.
func TestIncidentSelectReadsEveryIncidentColumn(t *testing.T) {
	st, _ := querytest.Seed(t)
	rows, err := st.Reader.DB().QueryContext(context.Background(),
		"SELECT name FROM pragma_table_info('incidents') ORDER BY cid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var schema []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		schema = append(schema, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(schema) == 0 {
		t.Fatal("pragma_table_info returned no columns")
	}
	var read []string
	for _, f := range strings.Split(incidentColumns, ",") {
		read = append(read, strings.TrimPrefix(strings.TrimSpace(f), "i."))
	}
	if diff := cmp.Diff(schema, read); diff != "" {
		t.Error(diff)
	}
}

// attentionCutoff falls before every seeded incident's closed_at, so the
// attention set the tests below check is the one TestListIncidentsDerivesState
// works out by hand: every row but the two that are dismissed (3, 7).
const attentionCutoff = "2026-08-27T00:00:00.000000Z"

// Each facet is counted over the other facet's selection and never over its
// own, so the number on a folder is the length of the list it opens.
func TestCountIncidentsFacetsEachAxis(t *testing.T) {
	st, _ := querytest.Seed(t)
	cases := []struct {
		name       string
		clusterIDs []int64
		state      string
		category   string
		want       IncidentCounts
	}{
		{"no filter", nil, "", "", IncidentCounts{
			ByState: map[string]int64{"open": 5, "acknowledged": 1, "recovered": 1, "pod_deleted": 1, "manual": 1, "job_finished": 1, "dismissed": 2, StateAttention: 10},
			ByCategory: map[string]int64{
				store.CategoryCrash: 5, store.CategoryProbe: 1, store.CategoryConfig: 1, store.CategoryOOM: 1,
				store.CategoryImagePull: 1, store.CategoryJobFailed: 1, store.CategoryNodePressure: 1, store.CategoryScheduling: 1,
			},
		}},
		{"state only", nil, "open", "", IncidentCounts{
			ByState:    map[string]int64{"open": 5, "acknowledged": 1, "recovered": 1, "pod_deleted": 1, "manual": 1, "job_finished": 1, "dismissed": 2, StateAttention: 10},
			ByCategory: map[string]int64{store.CategoryCrash: 3, store.CategoryProbe: 1, store.CategoryConfig: 1},
		}},
		{"category only", nil, "", store.CategoryCrash, IncidentCounts{
			ByState: map[string]int64{"open": 3, "pod_deleted": 1, "dismissed": 1, StateAttention: 4},
			ByCategory: map[string]int64{
				store.CategoryCrash: 5, store.CategoryProbe: 1, store.CategoryConfig: 1, store.CategoryOOM: 1,
				store.CategoryImagePull: 1, store.CategoryJobFailed: 1, store.CategoryNodePressure: 1, store.CategoryScheduling: 1,
			},
		}},
		{"both selected", nil, "open", store.CategoryCrash, IncidentCounts{
			ByState:    map[string]int64{"open": 3, "pod_deleted": 1, "dismissed": 1, StateAttention: 4},
			ByCategory: map[string]int64{store.CategoryCrash: 3, store.CategoryProbe: 1, store.CategoryConfig: 1},
		}},
		{"prod only", []int64{querytest.ClusterProd}, "", "", IncidentCounts{
			ByState: map[string]int64{"open": 3, "acknowledged": 1, "recovered": 1, "pod_deleted": 1, "job_finished": 1, "dismissed": 2, StateAttention: 7},
			ByCategory: map[string]int64{
				store.CategoryCrash: 4, store.CategoryProbe: 1, store.CategoryOOM: 1,
				store.CategoryImagePull: 1, store.CategoryJobFailed: 1, store.CategoryScheduling: 1,
			},
		}},
		{"state attention selects the attention set for by_category", nil, StateAttention, "", IncidentCounts{
			ByState: map[string]int64{"open": 5, "acknowledged": 1, "recovered": 1, "pod_deleted": 1, "manual": 1, "job_finished": 1, "dismissed": 2, StateAttention: 10},
			ByCategory: map[string]int64{
				store.CategoryCrash: 4, store.CategoryProbe: 1, store.CategoryConfig: 1, store.CategoryOOM: 1,
				store.CategoryJobFailed: 1, store.CategoryNodePressure: 1, store.CategoryScheduling: 1,
			},
		}},
		{"unknown cluster", []int64{99}, "", "", IncidentCounts{
			ByState:    map[string]int64{},
			ByCategory: map[string]int64{},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := CountIncidents(context.Background(), st.Reader.DB(), c.clusterIDs, c.state, c.category, attentionCutoff)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}
