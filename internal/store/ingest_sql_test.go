package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/clock"
)

func inTx(t *testing.T, s *Store, fn func(tx *sql.Tx) error) {
	t.Helper()
	if err := s.Writer.Tx(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

func TestLatestConditionPerType(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	ts := clock.Format(testEpoch)
	rows := []PodCondition{
		{PodUID: "p1", Type: "PodScheduled", Status: "False", Reason: "Unschedulable", Message: ptr("0/3 nodes"), ObservedAt: ts},
		{PodUID: "p1", Type: "Ready", Status: "False", Reason: "", ObservedAt: ts},
		{PodUID: "p1", Type: "PodScheduled", Status: "True", Reason: "", ObservedAt: ts},
	}
	ctx := context.Background()
	inTx(t, s, func(tx *sql.Tx) error { return InsertConditions(ctx, tx, rows) })
	var got []PodCondition
	inTx(t, s, func(tx *sql.Tx) (err error) { got, err = LoadLatestConditions(ctx, tx, "p1"); return err })
	want := []PodCondition{
		{ID: 2, PodUID: "p1", Type: "Ready", Status: "False", ObservedAt: ts},
		{ID: 3, PodUID: "p1", Type: "PodScheduled", Status: "True", ObservedAt: ts},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}

func TestUpsertOverwritesStatusColumns(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ctx := context.Background()
	first := Pod{UID: "p1", ClusterID: cid, Namespace: "idios-smoke", Name: "web-1", Phase: "Pending", ControllerKind: "none", WorkloadKind: "none",
		CreatedAt: "2026-08-27T11:45:00.000000Z", FirstSeenAt: "2026-08-27T11:50:00.000000Z", LastSeenAt: "2026-08-27T11:50:00.000000Z"}
	second := first
	second.Phase, second.NodeName, second.LastSeenAt = "Running", ptr("node-a"), "2026-08-27T11:51:00.000000Z"
	c1 := Container{PodUID: "p1", Name: "api", Kind: ContainerKindApp, Image: "web:1", State: StateWaiting, UpdatedAt: first.LastSeenAt}
	c2 := c1
	c2.State, c2.Ready, c2.RestartCount, c2.ExitCode, c2.UpdatedAt = StateRunning, true, 2, ptr[int64](1), second.LastSeenAt
	c2.Message = ptr("panic: config missing")
	inTx(t, s, func(tx *sql.Tx) error {
		if err := UpsertPod(ctx, tx, first); err != nil {
			return err
		}
		return UpsertContainers(ctx, tx, []Container{c1})
	})
	inTx(t, s, func(tx *sql.Tx) error {
		if err := UpsertPod(ctx, tx, second); err != nil {
			return err
		}
		return UpsertContainers(ctx, tx, []Container{c2})
	})
	var gotPod *Pod
	var gotContainers []Container
	inTx(t, s, func(tx *sql.Tx) (err error) {
		if gotPod, err = LoadPod(ctx, tx, "p1"); err != nil {
			return err
		}
		gotContainers, err = LoadContainers(ctx, tx, "p1")
		return err
	})
	c2.ID = 1
	if d := cmp.Diff(&second, gotPod); d != "" {
		t.Error(d)
	}
	if d := cmp.Diff([]Container{c2}, gotContainers); d != "" {
		t.Error(d)
	}
	if n := countRows(t, s, "containers"); n != 1 {
		t.Errorf("containers rows = %d, want 1", n)
	}
	var missing *Pod
	inTx(t, s, func(tx *sql.Tx) (err error) { missing, err = LoadPod(ctx, tx, "nope"); return err })
	if missing != nil {
		t.Errorf("LoadPod(unknown) = %+v, want nil", missing)
	}
}

func podIncident(podUID, container, category string) Incident {
	ts := clock.Format(testEpoch)
	return Incident{ClusterID: 1, Namespace: "idios-smoke", SubjectKind: SubjectPod, PodUID: ptr(podUID), ContainerName: container,
		WorkloadKind: "Deployment", WorkloadName: "web", Category: category, FirstReason: "r", LastReason: "r",
		Occurrences: 1, OpenedAt: ts, LastSeenAt: ts}
}

func loadIncidents(t *testing.T, s *Store, uid string) []Incident {
	t.Helper()
	var out []Incident
	inTx(t, s, func(tx *sql.Tx) (err error) {
		out, err = LoadIncidentsForSubject(context.Background(), tx, uid)
		return err
	})
	return out
}

// The Go constants and the category CHECK are two spellings of one
// vocabulary; a category added to one and not the other fails every insert
// that uses it, at runtime, on the machine that has the problem.
func TestIncidentCategoryVocabulary(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	cases := []struct {
		category string
		want     bool
	}{
		{"weird", false},
		{CategoryOOM, true}, {CategoryCrash, true}, {CategoryImagePull, true},
		{CategoryConfig, true}, {CategoryProbe, true}, {CategoryScheduling, true},
		{CategoryNodePressure, true}, {CategoryRescheduled, true}, {CategoryJobFailed, true}, {CategoryUncleanExit, true},
		{CategoryStuck, true}, {CategoryOther, true},
	}
	for _, c := range cases {
		_, err := insertPodIncident(t, s, cid, "p1", c.category, c.category)
		if got := err == nil; got != c.want {
			t.Errorf("category %q accepted = %v, want %v (%v)", c.category, got, c.want, err)
		}
	}
}

func TestOpenIncidentIsIdempotentUnderReplay(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	insertJob(t, s, cid, "j1")
	ctx := context.Background()
	pod := podIncident("p1", "api", CategoryCrash)
	pod.ClusterID = cid
	job := Incident{ClusterID: cid, Namespace: "idios-smoke", SubjectKind: SubjectJob, JobUID: ptr("j1"), WorkloadKind: "Job", WorkloadName: "j",
		Category: CategoryJobFailed, FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded", Occurrences: 1, OpenedAt: pod.OpenedAt, LastSeenAt: pod.LastSeenAt}
	cases := []struct {
		name string
		inc  Incident
		uid  string
	}{
		{"pod key", pod, "p1"},
		{"job key", job, "j1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var first, second int64
			replay := c.inc
			replay.LastReason, replay.LastSeenAt = "again", "2026-08-27T12:01:00.000000Z"
			inTx(t, s, func(tx *sql.Tx) (err error) {
				if first, err = OpenIncident(ctx, tx, c.inc); err != nil {
					return err
				}
				second, err = OpenIncident(ctx, tx, replay)
				return err
			})
			if first != second {
				t.Fatalf("replay opened a second incident: %d then %d", first, second)
			}
			got := loadIncidents(t, s, c.uid)
			want := c.inc
			want.ID, want.Occurrences, want.LastReason, want.LastSeenAt = first, 2, "again", replay.LastSeenAt
			if d := cmp.Diff([]Incident{want}, got); d != "" {
				t.Fatal(d)
			}
			inTx(t, s, func(tx *sql.Tx) error {
				return CloseIncident(ctx, tx, first, CloseRecovered, "2026-08-27T12:02:00.000000Z")
			})
			var third int64
			inTx(t, s, func(tx *sql.Tx) (err error) { third, err = OpenIncident(ctx, tx, c.inc); return err })
			if third == first {
				t.Fatal("open after close reused the closed row instead of inserting")
			}
		})
	}
}

// A pod incident of a Job carries the job uid so the two can be listed
// together, and that uid is not its subject: the pod incident answers for its
// pod, the job incident for its Job, and neither is loaded or closed under the
// other's uid.
func TestSubjectIsTheKindNotTheUID(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	insertJob(t, s, cid, "j1")
	ctx := context.Background()
	pod := podIncident("p1", "api", CategoryCrash)
	pod.ClusterID, pod.JobUID = cid, ptr("j1")
	job := Incident{ClusterID: cid, Namespace: "idios-smoke", SubjectKind: SubjectJob, JobUID: ptr("j1"), WorkloadKind: "Job", WorkloadName: "j",
		Category: CategoryJobFailed, FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded",
		Occurrences: 1, OpenedAt: pod.OpenedAt, LastSeenAt: pod.LastSeenAt}
	var podID, jobID int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		if podID, err = OpenIncident(ctx, tx, pod); err != nil {
			return err
		}
		jobID, err = OpenIncident(ctx, tx, job)
		return err
	})
	pod.ID, job.ID = podID, jobID

	loads := []struct {
		name string
		uid  string
		want []Incident
	}{
		{"the pod uid loads the pod incident", "p1", []Incident{pod}},
		{"the job uid loads the job incident alone", "j1", []Incident{job}},
	}
	for _, c := range loads {
		if d := cmp.Diff(c.want, loadIncidents(t, s, c.uid)); d != "" {
			t.Errorf("%s: %s", c.name, d)
		}
	}

	closedAt := "2026-08-27T12:30:00.000000Z"
	var closed []int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		closed, err = CloseOpenIncidents(ctx, tx, "j1", CloseJobFinished, closedAt)
		return err
	})
	if d := cmp.Diff([]int64{jobID}, closed); d != "" {
		t.Errorf("closing the job closed more than the job incident: %s", d)
	}
	if d := cmp.Diff([]Incident{pod}, loadIncidents(t, s, "p1")); d != "" {
		t.Error(d)
	}
}

func TestReopenClearsDismissedKeepsAcknowledged(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	ctx := context.Background()
	inc := podIncident("p1", "api", CategoryCrash)
	inc.ClusterID = cid
	var id int64
	inTx(t, s, func(tx *sql.Tx) (err error) { id, err = OpenIncident(ctx, tx, inc); return err })
	mustExec(t, s, "UPDATE incidents SET closed_at = ?, close_reason = 'recovered', acknowledged_at = ?, dismissed_at = ? WHERE id = ?",
		"2026-08-27T12:05:00.000000Z", "2026-08-27T12:03:00.000000Z", "2026-08-27T12:04:00.000000Z", id)
	inTx(t, s, func(tx *sql.Tx) error {
		return AttachIncident(ctx, tx, id, true, "CrashLoopBackOff", ptr("back-off"), "2026-08-27T12:10:00.000000Z")
	})
	want := inc
	want.ID, want.Occurrences, want.LastReason, want.LastMessage, want.LastSeenAt = id, 2, "CrashLoopBackOff", ptr("back-off"), "2026-08-27T12:10:00.000000Z"
	want.AcknowledgedAt = ptr("2026-08-27T12:03:00.000000Z")
	if d := cmp.Diff([]Incident{want}, loadIncidents(t, s, "p1")); d != "" {
		t.Fatal(d)
	}
}

func testEvent(uid, fieldPath, reason string, cat *string, lastTS string) K8sEvent {
	return K8sEvent{ClusterID: 1, EventUID: "ev-" + reason + "-" + fieldPath + lastTS, Namespace: "idios-smoke", Type: "Warning", InvolvedKind: "Pod",
		InvolvedName: "web", InvolvedUID: uid, FieldPath: fieldPath, Reason: reason, Message: "m", SourceComponent: "kubelet",
		Count: 1, FirstTS: lastTS, LastTS: lastTS, Category: cat, RawJSON: "{}"}
}

func loadEvents(t *testing.T, s *Store) []K8sEvent {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), `
SELECT id, cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, field_path, reason, message,
       source_component, count, first_ts, last_ts, category, incident_id, raw_json FROM k8s_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []K8sEvent
	for rows.Next() {
		var e K8sEvent
		if err := rows.Scan(&e.ID, &e.ClusterID, &e.EventUID, &e.Namespace, &e.Type, &e.InvolvedKind, &e.InvolvedName, &e.InvolvedUID,
			&e.FieldPath, &e.Reason, &e.Message, &e.SourceComponent, &e.Count, &e.FirstTS, &e.LastTS, &e.Category, &e.IncidentID, &e.RawJSON); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func TestEventUpsertKeepsAttachAndBumpsCount(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ctx := context.Background()
	ev := testEvent("p1", "spec.containers{api}", "BackOff", nil, "2026-08-27T11:55:00.000000Z")
	ev.ClusterID = cid
	insertPod(t, s, cid, "p1")
	var incID int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		incID, err = OpenIncident(ctx, tx, podIncident("p1", "api", CategoryCrash))
		return err
	})
	ev.IncidentID = &incID
	inTx(t, s, func(tx *sql.Tx) error { return UpsertEvent(ctx, tx, ev) })
	bump := ev
	bump.Count, bump.LastTS, bump.Message, bump.IncidentID = 5, "2026-08-27T11:59:30.000000Z", "m5", nil
	inTx(t, s, func(tx *sql.Tx) error { return UpsertEvent(ctx, tx, bump) })
	want := bump
	want.ID, want.IncidentID = 1, &incID
	if d := cmp.Diff([]K8sEvent{want}, loadEvents(t, s)); d != "" {
		t.Fatal(d)
	}
}

func TestAttachEventsRule(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	insertPod(t, s, cid, "p2")
	ctx := context.Background()
	since := "2026-08-27T11:50:00.000000Z"
	events := []K8sEvent{
		testEvent("p1", "spec.containers{api}", "Failed", ptr(CategoryImagePull), "2026-08-27T11:55:00.000000Z"),
		testEvent("p1", "spec.containers{api}", "BackOff", nil, "2026-08-27T11:56:00.000000Z"),
		testEvent("p1", "spec.containers{api}", "Unhealthy", ptr(CategoryProbe), "2026-08-27T11:57:00.000000Z"),
		testEvent("p1", "spec.containers{worker}", "BackOff", nil, "2026-08-27T11:58:00.000000Z"),
		testEvent("p1", "", "Killing", nil, "2026-08-27T11:59:00.000000Z"),
		testEvent("p1", "spec.containers{api}", "BackOff", nil, "2026-08-27T11:49:59.000000Z"),
		testEvent("p2", "spec.containers{api}", "BackOff", nil, "2026-08-27T11:56:30.000000Z"),
		testEvent("p1", "spec.containers{api}", "Pulled", nil, "2026-08-27T11:56:45.000000Z"),
	}
	inTx(t, s, func(tx *sql.Tx) error {
		for i := range events {
			events[i].ClusterID = cid
			if err := UpsertEvent(ctx, tx, events[i]); err != nil {
				return err
			}
		}
		return nil
	})
	var earlier int64
	inTx(t, s, func(tx *sql.Tx) (err error) {
		earlier, err = OpenIncident(ctx, tx, podIncident("p1", "api", CategoryCrash))
		return err
	})
	mustExec(t, s, "UPDATE k8s_events SET incident_id = ? WHERE event_uid = ?", earlier, events[7].EventUID)

	cases := []struct {
		name      string
		container string
		category  string
		wantIdx   []int
	}{
		{"container incident", "api", CategoryImagePull, []int{0, 1}},
		{"pod-level incident", "", CategoryNodePressure, []int{1, 3, 4}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustExec(t, s, "UPDATE k8s_events SET incident_id = NULL WHERE incident_id <> ?", earlier)
			inc := podIncident("p1", c.container, c.category)
			var id, n int64
			inTx(t, s, func(tx *sql.Tx) (err error) {
				if id, err = OpenIncident(ctx, tx, inc); err != nil {
					return err
				}
				n, err = AttachEvents(ctx, tx, id, "p1", c.container, c.category, since)
				return err
			})
			var got []int
			for i, e := range loadEvents(t, s) {
				if e.IncidentID != nil && *e.IncidentID == id {
					got = append(got, i)
				}
			}
			if d := cmp.Diff(c.wantIdx, got); d != "" {
				t.Error(d)
			}
			if n != int64(len(c.wantIdx)) {
				t.Errorf("AttachEvents reported %d rows, want %d", n, len(c.wantIdx))
			}
			inTx(t, s, func(tx *sql.Tx) error { return CloseIncident(ctx, tx, id, CloseManual, since) })
		})
	}
}

func rollout(rsUID, rsName, container, depUID string, rev *int64, ts string) RolloutHistory {
	return RolloutHistory{ClusterID: 1, Namespace: "idios-smoke", DeploymentName: "web", DeploymentUID: depUID, ReplicaSetUID: rsUID,
		ReplicaSetName: rsName, ContainerName: container, Image: "web:1", ImageTag: ptr("1"), Revision: rev, FirstSeenAt: ts, LastSeenAt: ts}
}

func loadRollouts(t *testing.T, s *Store) []RolloutHistory {
	t.Helper()
	rows, err := s.Reader.DB().QueryContext(context.Background(), `
SELECT id, cluster_id, namespace, deployment_name, deployment_uid, replicaset_uid, replicaset_name, container_name, image, image_tag,
       revision, first_seen_at, last_seen_at, deleted_at FROM rollout_history ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []RolloutHistory
	for rows.Next() {
		var r RolloutHistory
		if err := rows.Scan(&r.ID, &r.ClusterID, &r.Namespace, &r.DeploymentName, &r.DeploymentUID, &r.ReplicaSetUID, &r.ReplicaSetName,
			&r.ContainerName, &r.Image, &r.ImageTag, &r.Revision, &r.FirstSeenAt, &r.LastSeenAt, &r.DeletedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestRolloutUpsertKeepsFirstSeenUpdatesRevision(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ctx := context.Background()
	first := rollout("rs-1", "web-a", "api", "dep-web", ptr[int64](7), "2026-08-27T11:50:00.000000Z")
	first.ClusterID = cid
	second := first
	second.Revision, second.FirstSeenAt, second.LastSeenAt = ptr[int64](9), "2026-08-27T11:55:00.000000Z", "2026-08-27T11:55:00.000000Z"
	inTx(t, s, func(tx *sql.Tx) error { return UpsertRolloutHistory(ctx, tx, first) })
	inTx(t, s, func(tx *sql.Tx) error { return UpsertRolloutHistory(ctx, tx, second) })
	want := second
	want.ID, want.FirstSeenAt = 1, first.FirstSeenAt
	if d := cmp.Diff([]RolloutHistory{want}, loadRollouts(t, s)); d != "" {
		t.Fatal(d)
	}
}

func TestRolloutRevisionsForDeployment(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ctx := context.Background()
	ts := clock.Format(testEpoch)
	rows := []RolloutHistory{
		rollout("rs-1", "web-a", "api", "dep-web", ptr[int64](7), ts),
		rollout("rs-1", "web-a", "worker", "dep-web", ptr[int64](7), ts),
		rollout("rs-2", "web-b", "api", "dep-web", ptr[int64](8), ts),
		rollout("rs-3", "other-a", "api", "dep-other", ptr[int64](12), ts),
		rollout("rs-bare", "standalone", "api", "", nil, ts),
	}
	inTx(t, s, func(tx *sql.Tx) error {
		for _, r := range rows {
			r.ClusterID = cid
			if err := UpsertRolloutHistory(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
	cases := []struct {
		rs          string
		own, newest *int64
	}{
		{"rs-1", ptr[int64](7), ptr[int64](8)},
		{"rs-2", ptr[int64](8), ptr[int64](8)},
		{"rs-bare", nil, nil},
		{"rs-unknown", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.rs, func(t *testing.T) {
			var own, newest *int64
			inTx(t, s, func(tx *sql.Tx) (err error) { own, newest, err = LoadRolloutRevisions(ctx, tx, c.rs); return err })
			if d := cmp.Diff([]*int64{c.own, c.newest}, []*int64{own, newest}); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestCloseOpenIncidentsReturnsTheIDsItClosed(t *testing.T) {
	ctx := context.Background()
	ts := clock.Format(testEpoch)
	cases := []struct {
		name    string
		subject string
		repeat  bool
		want    []int64
	}{
		{"every open incident of the subject, ascending", "p1", false, []int64{1, 2}},
		{"a subject with nothing open", "p2", false, nil},
		{"a repeat closes nothing", "p1", true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			cid := insertCluster(t, s, "c")
			insertPod(t, s, cid, "p1")
			insertPod(t, s, cid, "p2")
			for _, in := range []struct{ container, category string }{
				{"api", CategoryCrash}, {"web", CategoryOOM}, {"db", CategoryImagePull},
			} {
				if _, err := insertPodIncident(t, s, cid, "p1", in.container, in.category); err != nil {
					t.Fatal(err)
				}
			}
			inTx(t, s, func(tx *sql.Tx) error { return CloseIncident(ctx, tx, 3, CloseRecovered, ts) })
			var got []int64
			inTx(t, s, func(tx *sql.Tx) (err error) {
				if got, err = CloseOpenIncidents(ctx, tx, c.subject, ClosePodDeleted, ts); err != nil || !c.repeat {
					return err
				}
				got, err = CloseOpenIncidents(ctx, tx, c.subject, ClosePodDeleted, ts)
				return err
			})
			if d := cmp.Diff(c.want, got); d != "" {
				t.Fatal(d)
			}
		})
	}
}

// A pruned Job or ReplicaSet leaves the informer store, so its pod's owner can
// only come from these rows; a deleted Job must still answer.
func TestLoadControllerWorkloadOutlivesTheController(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ctx := context.Background()
	ts := "2026-08-27T11:50:00.000000Z"
	job := func(uid string, cronjob *string, deletedAt *string) Job {
		return Job{UID: uid, ClusterID: cid, Namespace: "idios-smoke", Name: uid, CronJobName: cronjob,
			CreatedAt: ts, FirstSeenAt: ts, LastSeenAt: ts, DeletedAt: deletedAt}
	}
	rs := func(uid, deployment string) RolloutHistory {
		r := rollout(uid, uid+"-name", "api", "dep-web", nil, ts)
		r.ClusterID, r.DeploymentName = cid, deployment
		return r
	}
	inTx(t, s, func(tx *sql.Tx) error {
		for _, j := range []Job{
			job("job-cron", ptr("report"), nil),
			job("job-bare", nil, nil),
			job("job-gone", ptr("report"), ptr(ts)),
		} {
			if err := UpsertJob(ctx, tx, j); err != nil {
				return err
			}
		}
		for _, r := range []RolloutHistory{rs("rs-deploy", "web"), rs("rs-bare", "")} {
			if err := UpsertRolloutHistory(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})

	type result struct {
		Kind, Name string
		OK         bool
	}
	cases := []struct {
		name string
		kind string
		uid  string
		want result
	}{
		{"job of a cronjob", "Job", "job-cron", result{"CronJob", "report", true}},
		{"job without a cronjob", "Job", "job-bare", result{}},
		{"deleted job still names its cronjob", "Job", "job-gone", result{"CronJob", "report", true}},
		{"replicaset of a deployment", "ReplicaSet", "rs-deploy", result{"Deployment", "web", true}},
		{"replicaset without a deployment", "ReplicaSet", "rs-bare", result{}},
		{"unknown uid", "Job", "job-missing", result{}},
		{"unknown kind", "StatefulSet", "sts-1", result{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got result
			inTx(t, s, func(tx *sql.Tx) (err error) {
				got.Kind, got.Name, got.OK, err = LoadControllerWorkload(ctx, tx, c.kind, c.uid)
				return err
			})
			if d := cmp.Diff(c.want, got); d != "" {
				t.Error(d)
			}
		})
	}
}
