# Phase 11: recorder semantics and contract

Goal: the daemon stops opening incidents nobody should read, says the true
thing about the ones it opens, and hands Phases 12 and 13 a contract that
already carries the fields they draw. A scheduling wait that resolves itself
in thirty seconds records nothing; a Job failure that is history opens as
history; a rollout row says when its revision shipped; the two tables that
grow one row per schedule tick can be asked for a bounded page; a pod no
controller owns is a workload row of its own; the one human action with no
inverse gets one; the status numbers say what they count.

Architecture: unchanged. The decisions stay in `internal/incident`, which is
pure and already takes `now`; `internal/ingest` gains the current-condition
list the grace window compares against; `internal/processor` passes a policy
and executes one extra close; mutation SQL lands in `internal/store` in the
existing per-area files plus `job_sql.go` and `stuck_sql.go`; read-model SQL
lands in `internal/query`; `internal/api` maps. Every schema change edits
`internal/store/migrations/0001_init.sql` in place per milestone decision 9,
so the schema version stays 1 and every test database is recreated. Every
proto change is made by the task that
produces the value, so no field ever ships without a producer.

Tech stack: unchanged. Go 1.26, sebuf, `modernc.org/sqlite` v1.57.0,
`k8s.io/*` v0.37.0.

Spec: `data-storage.md` Sections 5.7 (incidents), 5.8 (jobs), 5.9
(rollout_history), 6.1 (categories), 6.2 (opening and attaching), 6.3
(closing), 6.4 (human actions), 8 (retention and sweep);
`presentation.md` Sections 3.1 (wire rules), 3.2 (additive within v1), 4.1,
4.2, 4.5, 4.6 (the endpoints), 8 (human actions).

Global constraints: every `.ai/*.md` rule. The m3 cross-cutting decisions
this phase touches: 1 (noise is a recorder concern, decided in
`internal/incident` as a comparison and not a scheduler), 3 (the reader's
words: this phase supplies the values, the words are Phase 12), 4 (history
that grew is collapsed with a count, so a list that can grow gets a bound
and a count), 5 (ordering is Phase 13's; this phase does not sort), 6 (a
controller-less pod is a row with an identity), 9 (the schema is
`0001_init.sql` until the first stable release: edit it in place, keep
version 1, drop every database, and a column for a value Kubernetes always
sends is `NOT NULL`), 10
(contract first: `make generate-check` clean in every task that touches
`api/proto`). The m1 and m2 decisions stay in force and are in `CLAUDE.md`:
every timestamp through `clock.Format` of the injected clock, every mutation
one `Writer.Tx`, notify after the commit, generated code never edited.

Model per task: Opus implements, Opus reviews the diff against the task and
the `.ai` rules before the next task starts.

## Decisions this plan makes

Written here because the specs left them open, or because this phase changes
what they say; Task 12 moves the durable ones into the design docs.

1. **The grace window belongs to a category, and only `scheduling` has
   one.** `incident.Policy` carries it as one duration and `Apply` compares
   it for that category by name; a map arrives the day a second category
   needs a window, and not before. A category with no window keeps
   today's behaviour exactly: it is decided from the conditions that
   changed, never from the conditions that persist. This keeps the change to
   one category and leaves `node_pressure` and `rescheduled` alone.
2. **A persisting condition can open an incident; it can never attach to
   one.** The informers run with resync 0
   (`informers.NewSharedInformerFactoryWithOptions(client, 0, ...)`), so
   nothing re-delivers a pod on a timer; what does arrive is a pod update on
   every scheduling attempt, because the scheduler rewrites the condition's
   message and `diffConditions` drops it (the key excludes the message).
   That event is what the window is evaluated on. Attaching on each would
   make `occurrences` count scheduler retries and `last_seen_at` a
   heartbeat, so a changed condition behaves as today, subject to the
   window, and an unchanged one is considered only when its category has no
   open incident on the pod.
3. **`PodScheduled` becoming `True` closes the open `scheduling` incident as
   `recovered`, at once.** `CloseStableIncidents` still closes it too
   through its `node_name IS NOT NULL` arm; that one catches a pod scheduled
   while the daemon was down, this one is the immediate path.
4. **A Job failure that is already history opens closed.** First sight of a
   `Failed` condition whose `finished_at` is older than
   `stabilization_window` is a run that ended before the daemon looked. It
   is recorded as a closed incident (`close_reason = job_finished`,
   `closed_at` the Job's own finish time) so it appears in history and in
   the counts without ever entering the open list. Nothing else in the
   recorder opens a closed row, so `store.OpenIncident` grows two columns
   rather than a second function.
5. **A later successful run of the same CronJob closes the earlier run's
   incident, with `close_reason = job_finished`.** No new close reason,
   although the list is one edit away: `job_finished` already means "the
   Job is done with", which Job finished is on the row, and a second value
   would make every reader of the vocabulary distinguish two closes that
   are read the same way.
6. **A manual close is final.** `incident.resolve` and `resolveJob` stop
   treating `manual` as a reopen candidate, so a recurrence opens a new
   incident instead of quietly reviving the one a person said they were done
   with. The inverse of resolve is explicit:
   `DELETE /v1/incidents/{id}/resolve`, which clears the close only when it
   was `manual`. This changes `data-storage.md` 6.3 and `presentation.md`
   Section 8; both are updated in Task 12.
7. **Ready is a claim about now, and every list/watch failure is a cluster
   error.** The watch error handler in `internal/k8s/sync.go` records only a
   Forbidden error on the cluster row; a credential failure (an expired SSO
   token makes the `aws` exec plugin exit 255) or a network failure is a
   `WARN` line and nothing else, `Ready()` stays true because the initial
   list once synced, and `last_error` stays empty. On the real two-cluster
   run every relist failed for a day while `status.json` said `ready: true`
   and the application showed two green dots. So: every list/watch error is
   recorded as `last_error` once per informer key until it clears; a cluster
   with an outstanding error is not ready; and the error clears through a
   positive signal, a probe on backoff with the same identity call the
   connect path uses, because client-go reports a reflector's failure and
   never its recovery. No age threshold: with resync 0 a healthy watcher on
   an idle namespace legitimately delivers nothing for hours, and the
   earlier "stale" rule (`last_event_at` older than `last_connected_at`)
   was false on the very run it was written for, since events had arrived
   for hours before the credentials expired.
8. **`JobRow` moves to its own proto file.** The incident detail of a job
   subject needs `JobRow`, and `incidents.proto` cannot import
   `workloads.proto`, which imports `pods.proto`, which imports
   `incidents.proto`. `api/proto/idios/v1/jobs.proto` holds `JobRow`,
   `JobsResponse` and `ListJobsRequest`; the generated Go names and the
   OpenAPI schema names do not change, so it is a file move and not a
   contract change.
9. **A bounded list reports a total, not only `truncated`.** Milestone
   decision 4 wants "how many are folded" and `truncated` is a bool. The job
   list answers `total` and `failed_total` over the filter that selected it;
   the workload's pods answer `pods_truncated` beside the row's existing
   `live_pods` and `deleted_pods`. What ties a Job to its workload is the
   CronJob's name and not a uid: `query.WorkloadKey` is a name chain
   (cluster, namespace, kind, name) and a uid on the workload row would be a
   second identity for the same thing, true only for the kinds that have
   one.
10. **The last pod name of a job incident needs no column.** The `pods` row
    of a pruned CronJob pod survives pruning: it is marked deleted, not
    removed. The read model answers it with one
    `ORDER BY created_at DESC LIMIT 1` over `controller_uid`, and the row
    lives exactly as long as the incident does, both being swept on the same
    schedule.

## File structure

```
api/proto/idios/v1/
  jobs.proto                  new: JobRow, JobsResponse, ListJobsRequest
                              (moved out of workloads.proto), + filters
  workloads.proto             - the three messages; Rollout + created_at,
                              replicas, ready_replicas, available_replicas;
                              WorkloadRow + pod_uid, pod_name;
                              GetWorkloadRequest + pods_live, pods_limit;
                              WorkloadDetail + pods_truncated
  incidents.proto             IncidentDetail + job, last_pod_name;
                              + UnresolveIncidentRequest
  common.proto                Category + CATEGORY_STUCK (Task 11)
  status.proto                Status + scheduling_grace_seconds (Task 1);
                              CloserStats + closed_total, attached_total
                              (Task 9); + stuck_after_seconds, opened
                              (Task 11)
  service.proto               + UnresolveIncident
api/openapi/, internal/apigen/ regenerated (make generate)
internal/store/
  migrations/0001_init.sql    rollout_history + four columns (Task 3);
                              pods.deletion_reason + 'evicted' (Task 10);
                              incidents.category + 'stuck' (Task 11)
  ingest_sql.go               OpenIncident + closed_at, close_reason;
                              UpsertRolloutHistory + four columns
  action_sql.go               + UnresolveIncident
  job_sql.go                  new: CloseCronJobIncidentsBefore
  stuck_sql.go                new (Task 11): StuckCandidate,
                              ListStuckCandidates
  sweep_sql.go                DeleteExpiredRollouts by deleted_at only
  rows.go                     RolloutHistory + CreatedAt, Replicas,
                              ReadyReplicas, AvailableReplicas;
                              + CategoryStuck (Task 11)
internal/ingest/
  types.go                    PodChanges + CurrentConditions
  pod.go                      mapConditions split out of diffConditions
  replicaset.go               MapReplicaSet fills the four new columns
internal/k8s/
  sync.go                     failedSet records every list/watch error
  watcher.go                  ready false on a failure, the recovery probe
internal/incident/
  policy.go                   new: Policy
  apply.go                    conditions from CurrentConditions, the window,
                              the PodScheduled close, resolve skips manual
  job.go                      ApplyJob takes Policy, opens closed history,
                              resolveJob skips manual
  closer.go                   cumulative totals; the stuck pass (Task 11)
  stuck.go                    new (Task 11): StuckOps
internal/processor/
  processor.go                New takes incident.Policy
  pod.go, job.go              pass the policy; the CronJob close
  capture.go                  podRequests skips a clean single run
internal/query/
  workloads.go                rollout columns; job filters and counts;
                              GetWorkload bounds; the controller-less key
  detail.go                   IncidentDetail + Job, LastPodName
internal/api/
  workloads.go, pods.go       filters, bounds, counts, the new fields
  detail.go                   the job card
  actions.go                  UnresolveIncident
  status.go                   the settings, the totals
  enums.go                    + CategoryStuck (Task 11)
  mock/mock.go                + UnresolveIncident
internal/status/status.go     Closer + ClosedTotal, AttachedTotal, Opened
internal/config/config.go     + SchedulingGrace, StuckAfter
cmd/idios/
  run.go                      processor.New with the policy;
                              NewCloser with stuck_after (Task 11)
  status.go                   closer totals on the printed line
docs/design/data-storage.md   Sections 5.9, 6.1, 6.2, 6.3, 6.4, 8
docs/design/presentation.md   Sections 4.1, 4.2, 4.5, 4.6, 8, 13
CLAUDE.md                     durable facts
docs/plans/m3-signal/roadmap.md   Phase 11 status
```

## Task 1: a grace window before a scheduling incident, and a close when it schedules

Spec: `data-storage.md` 6.1 (`scheduling` opens on PodScheduled `False` /
`Unschedulable`), 6.3 (the close table's row "pod becomes scheduled").
Roadmap deliverable 1; decisions 1, 2 and 3.

Today `incident.Apply` gathers condition problems from `changes.Conditions`,
which `ingest.diffConditions` fills only when `(type, status, reason)`
changed, so the first `Unschedulable` condition opens an incident with no
grace at all; nothing closes on scheduling success, so recovery waits for
`CloseStableIncidents`; and a pod pruned inside that window is closed by
`PodDeleted` as `pod_deleted`, which reads as "never recovered".

Config: `SchedulingGrace time.Duration` (`scheduling_grace`), default
`60 * time.Second`. Sixty seconds because on an autoscaled cluster a
`FailedScheduling` is normally followed within a minute by a node claim.
`Validate` rejects `< 0` and not `<= 0` as the other durations do: zero is a
meaningful setting here, the behaviour this task replaces, and someone who
wants an incident the instant scheduling fails must be able to ask for it.

`internal/ingest`: `PodChanges` gains `CurrentConditions
[]store.PodCondition`. `pod.go` splits `mapConditions(pod, nowS)
[]store.PodCondition` (every condition, in the object's order) out of
`diffConditions(prev, all)`, which now filters that slice instead of reading
the object again. `DiffPod` sets both fields from it.

`internal/incident/policy.go`:

```go
// Policy is the timing the pure decisions compare against.
type Policy struct {
    SchedulingGrace     time.Duration
    StabilizationWindow time.Duration
}
```

`Apply(incidents []store.Incident, changes ingest.PodChanges, now time.Time,
pol Policy) Ops`. Its condition loop reads `changes.CurrentConditions`, with
`changed` a set of the types in `changes.Conditions`:

- category `scheduling`: skipped while `now.Sub(transition) <
  pol.SchedulingGrace`, where transition is `cond.K8sTransitionAt` or `now`
  when absent, so a condition with no Kubernetes time waits out the window
  from first sight; past the window it is taken when the condition changed,
  and when it did not, only if no open incident of that category exists on
  the pod. That second path opens directly and does not go through
  `resolve`: `resolve` answers a closed-but-reopenable incident with
  `reopen` set, which would turn a condition that merely persisted into an
  `Attach` and reopen a story that had ended. A condition that changed keeps
  the `resolve` path it has today.
- every other category: taken only when the condition changed, as today.
- `PodScheduled` with status `True` and an open `scheduling` incident on the
  pod appends `Close{IncidentID: inc.ID, Reason: store.CloseRecovered,
  ClosedAt: nowS}`.

`internal/processor`: `New(w, clk, sink, pol incident.Policy)` replaces the
`stabilizationWindow` parameter; `p.window` becomes
`p.pol.StabilizationWindow` at its uses in `ops.go` and `delete.go`;
`pod.go` passes `p.pol` to `Apply`. `cmd/idios/run.go` builds
`incident.Policy{SchedulingGrace: cfg.SchedulingGrace, StabilizationWindow:
cfg.StabilizationWindow}`.

Contract: `status.proto`'s `Status` gains `int32
scheduling_grace_seconds = 25`, because Section 4.6 says the status answer
carries the intervals in force; `internal/api/status.go` fills it with
`seconds(s.cfg.SchedulingGrace)`.

Tests:

- `internal/incident/apply_test.go`: `TestApplyScenarios` gains a `now`
  field defaulting to `testNow` and these rows, edge cases first. The
  fixtures exist: `unschedulable/s1.json` carries the condition at
  `11:45:01`, `s2.json` changes only the message, `s3.json` is
  `PodScheduled=True` at `11:58:00`, and `testNow` is `12:00:00`.
  - `s1` at `now = 11:45:31`: `Ops{}` (inside the window).
  - `s1` at `testNow`: one `Open`, `OpenedAt
    2026-08-27T11:45:01.000000Z`, the whole `store.Incident` compared.
  - `s1, s2` at `testNow` with no incidents: one `Open` (an unchanged
    condition past the window opens).
  - `s1, s2` at `testNow` with an open scheduling incident: `Ops{}` (no
    attach on a condition that only repeated).
  - `s1, s2` at `testNow` with a *closed* scheduling incident on the pod:
    one `Open` and no `Attach`, the closed row untouched. This is the row
    decision 2 argues about: the persisting-condition path must not reach
    `resolve`, which would reopen a story that had ended.
  - `s1, s3` with an open scheduling incident: one `Close` with
    `recovered`.
  - `s1, s3` with no incidents: `Ops{}`.
  Trace: 6.1's scheduling row, 6.3's "pod becomes scheduled".
- `internal/processor/pod_test.go`, `TestSchedulingWaitsOutTheGrace`: drive
  `Pod` with `s1` at a clock inside the window and assert no incident row;
  advance the fake clock past the window, drive `s2`, assert one open
  incident row whole; drive `s3`, assert the row closed `recovered`. That
  the condition rows are written whatever the incident decision is belongs
  to `TestUnschedulableConditionRows`, which already covers these three
  fixtures and is not repeated here. Trace: the same, plus the roadmap's "a
  pod pruned inside the window closes as pod_deleted".

The signature changes ripple: every existing caller of `incident.Apply` and
`processor.New` in the two packages' tests moves with them, and
`processor_test.go`'s constructor helper is where the policy is set once.

Checkpoint (with `make generate-check`), commit `incident: wait out a grace
window before a scheduling incident`.

Consumes: `ingest.DiffPod`, `diffConditions`, `incident.Apply`,
`ConditionCategory`, `Ops.Close`, `execOps`, `store.CloseRecovered`,
`api.seconds`.
Produces: `incident.Policy`, `ingest.PodChanges.CurrentConditions`,
`ingest.mapConditions`, `config.Config.SchedulingGrace`,
`processor.New(..., incident.Policy)`, `scheduling_grace_seconds`.

## Task 2: job incidents that mean something

Spec: `data-storage.md` 5.8 (success and failure come from the condition,
never the counters), 6.1 (`job_failed`), 6.3 (`job_finished`);
`presentation.md` 4.2 (`GET /v1/incidents/{id}`). Roadmap deliverable 2;
decisions 4, 5, 8 and 10.

Today `ApplyJob` opens a `job_failed` incident on any first-seen `Failed`
condition, including one that failed hours before the daemon started, and
closes it only when that same Job completes (which a failed Job never does)
or when it is deleted. The detail page of a job incident carries no Job at
all: `query.GetIncident` returns early when `PodUID` is nil, so Phase 12
would draw a kubelet card full of unknowns.

Contract: `api/proto/idios/v1/jobs.proto` is new and holds `JobRow`,
`JobsResponse` and `ListJobsRequest` moved verbatim out of
`workloads.proto`; `workloads.proto` and `service.proto` import it.
`incidents.proto` imports it too and `IncidentDetail` gains:

```proto
  // The Job this incident is about; absent for a pod incident.
  JobRow job = 8;
  // The newest pod the recorder saw for that Job, which may since have been
  // pruned from the cluster.
  optional string last_pod_name = 9;
```

`store.OpenIncident` inserts `closed_at` and `close_reason`, nil on every
existing caller. The partial unique indexes cover open rows only, so a
closed insert cannot conflict and the `ON CONFLICT` target stays as it is.

`incident.ApplyJob(incidents, changes, now, pol Policy) Ops`:

- `changes.Failed && changes.FirstSight` and the Job's `FinishedAt` before
  `now - pol.StabilizationWindow`: the `Open` carries `ClosedAt:
  job.FinishedAt` and `CloseReason: ptr(store.CloseJobFinished)`; the rest of
  the row is unchanged.
- otherwise as today.

`internal/store/job_sql.go`:

```go
// CloseCronJobIncidentsBefore closes the open incidents of the other Jobs of
// one CronJob that started no later than this one and returns their ids. A
// later run that worked is the answer to an earlier run that did not.
func CloseCronJobIncidentsBefore(ctx context.Context, tx *sql.Tx, cronJobUID, exceptJobUID string, startedAt *string, closedAt string) ([]int64, error)
```

one `queryIDs` over `UPDATE incidents SET closed_at = ?, close_reason =
'job_finished' WHERE closed_at IS NULL AND subject_kind = 'job' AND job_uid
IN (SELECT uid FROM jobs WHERE cronjob_uid = ? AND uid <> ? AND (? IS NULL
OR started_at IS NULL OR started_at <= ?)) RETURNING id`. The arguments go
in the statement's order and not the signature's: `closedAt, cronJobUID,
exceptJobUID, startedAt, startedAt`.

`processor.Job`: after `execOps`, when `changes.Completed` and
`changes.Job.CronJobUID` is set, call it and append the ids to `touched`.

`internal/query/detail.go`: `IncidentDetail` gains `Job *JobRow` and
`LastPodName *string`; `GetIncident` fills them when `incident.JobUID != nil`,
before the `PodUID == nil` return, the first with `collect(..., scanJobRow,
jobSelect+" WHERE uid = ?")` and the second with `SELECT name FROM pods
WHERE controller_uid = ? ORDER BY created_at DESC, uid LIMIT 1`.
`internal/api/detail.go` maps both through the existing `jobRow`.

Tests:

- `internal/incident/job_test.go`: `TestApplyJob` gains rows, edge first:
  first sight of `job-failed/after.json` with `now` one window past its
  finish opens a row whose `ClosedAt` is the Job's finish and whose
  `CloseReason` is `job_finished`; the same at `testNow`, inside the window,
  opens it open as today; a `Failed` condition that is not first sight opens
  open whatever the age. Trace: 6.1's `job_failed` row; decision 4.
- `internal/processor/job_test.go`,
  `TestALaterRunClosesAnEarlierRunsIncident`: two Jobs of one CronJob, an
  incident opened on the older through `Job` with the failed fixture, then
  the newer driven to `Complete`; the whole incident row set is compared:
  the older closed `job_finished` at the newer's observation time, the newer
  with no incident, a third Job of another CronJob untouched. Trace:
  deliverable 2; decision 5.
- `internal/query/detail_test.go`,
  `TestJobIncidentDetailCarriesTheJobAndItsLastPod`: over `querytest.Seed`
  extended with a job incident, its Job row and two pods of it, the newer
  deleted with `deletion_reason job_pruned`: the whole `IncidentDetail`
  equals the expected struct, `Job` the seeded `JobRow` and `LastPodName`
  the newer pod. Trace: 4.2's detail row; decision 10.
- `internal/api/detail_test.go`,
  `TestJobIncidentDetailCarriesTheJobCard`: one case asserting the whole
  wire `IncidentDetail` of that incident with `protocmp.Transform()`, so
  the mapper is covered as well as the query. Trace: 4.2's detail row.

Checkpoint (`go build ./... && go test ./... && make ascii && make
generate-check && make app-test`; the last one proves the generated Swift
client survived the proto file move), commit `incident: record job failures
as the history they are`.

Consumes: `ingest.DiffJob`, `JobChanges`, `resolveJob`, `store.OpenIncident`,
`queryIDs`, `query.scanJobRow`, `jobSelect`, `querytest.Seed`, `api.jobRow`.
Produces: `jobs.proto`, `store.CloseCronJobIncidentsBefore`,
`query.IncidentDetail.Job`, `query.IncidentDetail.LastPodName`.

## Task 3: a rollout row that says when its revision shipped

Spec: `data-storage.md` 5.9 (`rollout_history`), 8 (retention and sweep);
`presentation.md` 4.5 (the workload detail's rollouts). Roadmap deliverable
3; milestone decision 9.

Today `first_seen_at` is when the daemon connected, so eleven rows of a
Deployment share one instant (the default `revisionHistoryLimit` of ten plus
the live ReplicaSet, all arriving on the initial list); nothing says which
revision is live; and `DeleteExpiredRollouts` deletes by `last_seen_at`, so
with resync 0 a stable ReplicaSet's history is swept while the ReplicaSet
still exists.

`0001_init.sql`, `rollout_history`, after `revision`:

```sql
    created_at         TEXT NOT NULL,
    replicas           INTEGER,
    ready_replicas     INTEGER,
    available_replicas INTEGER,
```

`created_at` is `NOT NULL` because every ReplicaSet carries a
`creationTimestamp`; the three counts are nullable because `spec.replicas`
is optional on the object and the status counts are absent on a ReplicaSet
that has never been observed by its controller. The schema version stays 1
and `migrate_test.go` is untouched; the checkpoint says that every database
built before this task is recreated.

`store.RolloutHistory` gains `CreatedAt string` and `Replicas`,
`ReadyReplicas`, `AvailableReplicas *int64`; `UpsertRolloutHistory` writes
them on insert and on update, because a scaling ReplicaSet changes its
counts on every event and `created_at` is the object's own constant.
`ingest.MapReplicaSet` fills `CreatedAt` from `rs.CreationTimestamp`,
`Replicas` from `rs.Spec.Replicas` and the other two from `rs.Status`.

`store.DeleteExpiredRollouts` becomes `DELETE FROM rollout_history WHERE
deleted_at < ?`; the second placeholder goes and the signature stays
`(ctx, tx, cutoff)` so `sweep/steps.go` is untouched. The doc comment
carries the reason: a live ReplicaSet is not stale, it is quiet.

`query.Rollout` gains the four fields and `rolloutSelect` selects them;
`Rollout` on the wire gains `string created_at = 9` and three
`optional int32` counts, mapped with the existing `int32Ptr`. The order of
`rolloutSelect` stays `revision DESC`: ordering is Phase 13's.

Tests:

- `internal/ingest/replicaset_test.go`: the existing whole-row table gains
  the four values over `replicaset/deploy-rev8.json` and `bare.json`.
  Trace: 5.9's column list.
- `internal/store/sweep_sql_test.go`,
  `TestRolloutSweepKeepsALiveReplicaSet`: three rows, one deleted before the
  cutoff, one deleted after it, one never deleted whose `last_seen_at` is
  far before it; only the first goes. Trace: the roadmap bug.
- `internal/query/workloads_test.go`: the whole-row rollout assertion in
  `TestGetWorkloadRolloutsAndRestartsPerHour` gains the four values, so the
  columns are covered from the object to the read model. Trace: 4.5's
  rollouts row.

Checkpoint (with `make generate-check`; `rm -rf .storage .storage/smoke`
first, and the commit body says the user's data directory must be recreated
too), commit `store: record when a ReplicaSet was created and how many
replicas it has`.

Consumes: `MapReplicaSet`, `UpsertRolloutHistory`, `DeleteExpiredRollouts`,
`rolloutSelect`, `scanRolloutRow`, `api.rollout`, `int32Ptr`.
Produces: the four columns end to end.

## Task 4: the job list gains its bounds and its key

Spec: `presentation.md` 4.5 (`GET /v1/jobs`), 3.1 (wire rules);
`data-storage.md` 5.8. Roadmap deliverable 4, first half; milestone
decision 4 and this plan's decision 9.

Today `JobFilter` filters on namespace and `cronjob_uid`, the workload row
carries no uid, and `WorkloadsStore.loadJobs` in the application pulls a
whole namespace page and filters it by `cronjobName` in Swift. A two-minute
CronJob produces twenty-two rows in forty minutes.

`query.JobFilter` gains `CronJobName string`, `Live *bool` (on `deleted_at`)
and `Failed bool` (`condition_type = 'Failed'`). `JobCounts{Total, Failed,
Live int64}` and `CountJobs(ctx, db, f JobFilter) (JobCounts, error)` count
over the filter with `Live` and `Failed` ignored, so a bounded page can say
how many rows it stands for.

`jobs.proto`: `ListJobsRequest` gains `string cronjob_name = 5`, `string
live = 6` and `string failed = 7`, all `(sebuf.http.query)`; `JobsResponse`
gains `int32 total = 3` and `int32 failed_total = 4`. The two flags travel
as strings for the reason `validateListPods` already carries: sebuf applies
the enum spelling to JSON bodies only, never to query parameters. That
sentence moves onto `boolFilter` below, which is where the parsing ends up.

`internal/api`: the bool parsing inside `validateListPods` becomes
`boolFilter(field, value string, violations *[]*sebufhttp.FieldViolation)
*bool` in `pods.go`, used by both handlers so the violation is worded once.
`ListJobs` validates `live`, `failed` and `limit` together and answers
`Total` and `FailedTotal` from `CountJobs`.

Tests:

- `internal/query/workloads_test.go`, `TestListJobsFiltersAndCounts`: table
  over a seed holding one CronJob with a complete, a failed, a running and a
  deleted run plus a standalone Job. Edge rows first: a filter matching
  nothing; `failed=true` on a CronJob whose only failure is a pod count
  rather than a condition, which must not be read (5.8). Then
  `cronjob_name`, `live=true`, `live=false`, `failed=true` with
  `cronjob_name`, and `Page{Limit: 2}` truncating. Each row asserts the
  whole `[]JobRow` and the whole `JobCounts`. Trace: 4.5's `GET /v1/jobs`
  row; 5.8.
- `internal/api/workloads_test.go`, `TestListJobsRejectsUnknownFlagValues`:
  `live=yes&failed=1` is one 400 carrying two violations. Trace: 3.1's error
  rules.

Checkpoint (with `make generate-check`), commit `api: bound and filter the
job list`.

Consumes: `query.ListJobs`, `JobFilter`, `limitClause`, `truncate`,
`validateListPods`, `validateLimit`, `Server.limit`.
Produces: `JobFilter.CronJobName/Live/Failed`, `query.CountJobs`,
`query.JobCounts`, `api.boolFilter`, the three request fields and the two
response counts.

## Task 5: a workload's pods gain a bound and a live filter

Spec: `presentation.md` 4.5 (`GET /v1/workloads/{cluster_id}/{namespace}/
{kind}/{name}`). Roadmap deliverable 4, second half; milestone decision 4.

Today `query.GetWorkload` calls `ListPods` with `Page{}` and no `Live`, so a
CronJob answers every pod it ever had, unbounded, the pruned ones mixed in.

`GetWorkload(ctx, db, k WorkloadKey, live *bool, p Page)
(*WorkloadDetail, error)`; `WorkloadDetail` gains `PodsTruncated bool` from
the `ListPods` second result. `GetWorkloadRequest` gains `string
pods_live = 5` and `int32 pods_limit = 6`, both `(sebuf.http.query)`;
`WorkloadDetail` gains `bool pods_truncated = 5`. `api.GetWorkload` parses
`pods_live` with Task 4's `boolFilter` and passes
`Page{Limit: s.limit(req.GetPodsLimit())}`.

Tests:

- `internal/query/workloads_test.go`,
  `TestWorkloadPodsAreBoundedAndFilterable`: over a workload with two live
  and three deleted pods: a nil `live` and no limit is all five; `live=true`
  is the two; `Page{Limit: 2}` over all five gives two rows and
  `PodsTruncated true`; and the workload row's `LivePods` and `DeletedPods`
  stay 2 and 3 whatever the pod filter is, because the row counts the
  workload and the list is a page of it. Trace: 4.5's row; milestone
  decision 4.
- `internal/api/workloads_test.go`,
  `TestGetWorkloadReturnsTheMappedDetail` gains a case asserting the whole
  wire `WorkloadDetail` for `pods_live=true&pods_limit=1`, with
  `PodsTruncated` set. Trace: 4.5's workload detail row.

Checkpoint (with `make generate-check`), commit `api: bound a workload's pod
list`.

Consumes: `query.GetWorkload`, `ListPods`, `PodFilter.Live`,
`api.boolFilter`, `Server.limit`.
Produces: `GetWorkload`'s two parameters, `WorkloadDetail.PodsTruncated`,
`pods_live`, `pods_limit`, `pods_truncated`.

## Task 6: capture skips a successful run's log

Spec: `data-storage.md` 6.2 step 5 (capture `log_previous` when a dead
instance appears), 8 (retention). Roadmap deliverable 4, last sentence.

Today `ingest.deadInstances` reports a container that reached `terminated`
and `processor.podRequests` turns every report into a `log_previous`
request. A CronJob container that exits 0 on a two-minute schedule is one
log read per container per run, forever, for pods nothing will ever ask
about; previous-log artifacts for such pods were almost half of all
artifacts on one run.

`processor.podRequests` skips a dead instance when all of these hold. The
container is `changes.Containers` looked up by `DeadInstance.Container`,
because a `DeadInstance` carries an index and not an exit code:

- its termination exit code is 0: `c.ExitCode` while `c.State ==
  store.StateTerminated`, else `c.LastTerminatedExitCode`;
- `c.RestartCount` is 0;
- no incident names the container. Two sources, both needed:
  `byContainer` from `touchedByContainer` holds only what this event opened
  or attached to, so the `incidents` slice is scanned as well, for any row
  whose `ContainerName` is the container or is empty, a pod-level incident
  naming every container.

The reason goes inline in one sentence: a container that ran once and
exited 0 is a success, and its log is not evidence of anything.

Nothing else changes. The `pod_json` request still fires on
`len(changes.History) > 0`, because it costs no API call, and a container
that exits 0 after a restart still has its log taken, because something went
wrong earlier.

Tests:

- `internal/processor/pod_test.go`: `TestPodScenariosEnqueueCaptures` gains
  rows, edge first: `init-then-success` (an init container exiting 0 with no
  restarts) enqueues only the `pod_json` request; `oom-exit-zero` (exit 0
  with an incident on the container) still enqueues its log; a clean exit
  with `restart_count 1` still enqueues its log. The assertions stay
  whole-slice comparisons of the requests. Trace: 6.2 step 5 and the
  roadmap's two-minute CronJob fact.

Checkpoint, commit `processor: stop capturing the log of a clean single run`.

Consumes: `podRequests`, `touchedByContainer`, `changes.DeadInstances`,
`changes.Containers`.
Produces: nothing new; behaviour only.

## Task 7: one workload row per controller-less pod

Spec: `presentation.md` 4.5 (`GET /v1/workloads`), 7.3 (identity chain);
`data-storage.md` 5.3 (`workload_kind` is `none` for a pod with no
controller). Roadmap deliverable 5; milestone decision 6.

Today `workloadSelect` keys on `(cluster_id, namespace, workload_kind,
workload_name)` over the union of `incidents` and `pods`, so every
controller-less pod of a namespace collapses into one row of kind `none`
with an empty name, which has nothing to read and nothing to open.

`query.WorkloadKey` gains `PodUID string`, empty for every real workload;
`WorkloadRow` gains `PodUID` and `PodName string`. The union in
`workloadSelect` carries the extra column. **The alias is not optional**: a
compound SELECT takes its column names from its first arm, which here is
`incidents`, and an unaliased `CASE` expression gets an implicit name that
nothing can reference, so the outer query fails to prepare with `no such
column: k.pod_uid`.

```sql
FROM (SELECT cluster_id, namespace, workload_kind, workload_name,
             CASE WHEN workload_kind = 'none' THEN COALESCE(pod_uid, '') ELSE '' END AS pod_uid
      FROM incidents
      UNION
      SELECT cluster_id, namespace, workload_kind, workload_name,
             CASE WHEN workload_kind = 'none' THEN uid ELSE '' END
      FROM pods) k
```

The outer projection gains `k.pod_uid` and, after it, `(SELECT name FROM
pods WHERE uid = k.pod_uid)` for the name, both before the four counted
subqueries so the scan order stays readable; `scanWorkloadRow` scans them in
that position, into `w.PodUID` and `w.PodName`. `incidentOfWorkload` and
`podOfWorkload` gain the matching equality and `scanWorkloadCount` carries
the column.

`keyPredicates` is where this is easy to get wrong. It aliases the table it
narrows, and `incidents.pod_uid` holds a real uid on every pod incident, so
a plain `i.pod_uid = ?` with an empty `k.PodUID` matches nothing and every
real workload would lose its categories and its tags. The predicate is the
same expression the union uses, `CASE WHEN i.workload_kind = 'none' THEN
COALESCE(i.pod_uid, '') ELSE '' END = ?`, and its `pods` twin for the alias
`p`.

`WorkloadRow` on the wire gains `string pod_uid = 11` and `string
pod_name = 12`, documented as set only for kind `none`. The detail endpoint
is unchanged: it still requires a name, and the contract already says a kind
`none` workload has no detail. The list order stays `cluster_id, namespace,
workload_kind, workload_name` plus the pod name for the split rows; the kind
order of milestone decision 5 is Phase 13's tree, not this query's.

Tests:

- `internal/query/workloads_test.go`,
  `TestControllerLessPodsAreOneRowEach`: a namespace with two
  controller-less pods, one carrying an open incident, plus a Deployment
  with two pods. Edge rows first: a controller-less pod with no incidents at
  all still has a row; an incident whose pod row was swept keeps its own row
  keyed on the uid. The whole `[]WorkloadRow` is compared: the two `none`
  rows carry their own `PodUID`, `PodName`, `LivePods 1` and their own
  incident counts, and the Deployment row is unchanged by the split. Trace:
  4.5's list row; milestone decision 6.
- `internal/api/workloads_test.go`, `TestListWorkloadsReturnsMappedRows`
  gains the wire row of one of them, `pod_uid` and `pod_name` set and the
  Deployment row's two fields empty. Trace: 4.5's list row.

Checkpoint (with `make generate-check`), commit `query: give every
controller-less pod its own workload row`.

Consumes: `workloadSelect`, `incidentOfWorkload`, `podOfWorkload`,
`attachIncidentFacts`, `keyPredicates`, `filterPredicates`,
`scanWorkloadRow`.
Produces: `WorkloadKey.PodUID`, `WorkloadRow.PodUID/PodName`, the two wire
fields.

## Task 8: unresolve, and a manual close that stays closed

Spec: `presentation.md` Section 8 (the human actions); `data-storage.md` 6.3
(reopen) and 6.4 (mark resolved). Roadmap deliverable 6; decision 6.

Today acknowledge, dismiss and note all have an inverse and resolve does
not. And `incident.resolve` treats any close but `pod_deleted` as a reopen
candidate, so a recurrence silently revives an incident a person closed by
hand, keeping its `occurrences` and its `opened_at` and losing the fact that
someone had finished with it.

`internal/store/action_sql.go`:

```go
// UnresolveIncident reopens an incident a person closed by hand and reports
// whether the incident exists. A system close is not a human action and is
// left alone.
func UnresolveIncident(ctx context.Context, tx *sql.Tx, id int64) (bool, error)
```

one `execCount` over `UPDATE incidents SET closed_at = CASE WHEN
close_reason = 'manual' THEN NULL ELSE closed_at END, close_reason = CASE
WHEN close_reason = 'manual' THEN NULL ELSE close_reason END WHERE id = ?`,
so the matched-row count is still the existence check, as with the other six
helpers.

`incidents.proto` gains `UnresolveIncidentRequest { int64 id = 1; }`;
`service.proto` gains `UnresolveIncident` on `DELETE
/incidents/{id}/resolve` returning `IncidentRow`; `internal/api/actions.go`
adds the handler through the existing `incidentAction`;
`internal/api/mock/mock.go` answers the `incident_row` fixture.

`incident.resolve` skips a candidate closed `manual` as well as one closed
`pod_deleted`, and `incident.resolveJob` skips one closed `manual` as well
as one closed `job_finished`, which is the reason it skips today, with the
reason inline: a person said this one is done, so a recurrence is a new
story rather than the old one quietly coming back.

Tests:

- `internal/store/action_sql_test.go`: the existing table gains rows, edge
  first: unresolve on an unknown id is `(false, nil)`; unresolve on a
  `recovered` row leaves it closed and still reports `(true, nil)`;
  unresolve after resolve reopens the row with `acknowledged_at` and `note`
  untouched; unresolve twice changes nothing. Trace: Section 8; decision 6.
- `internal/incident/apply_test.go`: one case, a manually closed incident on
  the key of a recurring crash produces an `Open` and not an `Attach`, and
  the closed row keeps its close; `internal/incident/job_test.go` gets the
  same for `resolveJob`. Trace: 6.3's reopen paragraph as this phase
  rewrites it.
- `internal/api/actions_test.go`:
  `TestIncidentActionsWriteOnceAndAnswerTheRow` gains the unresolve rows,
  and the unknown-id and stream tests gain the operation, so the new action
  is covered exactly as the other six are. Trace: Section 8's table and
  Section 5's "a human action changed it".

Checkpoint (with `make generate-check`), commit `api: add the unresolve
action and keep a manual close closed`.

Consumes: `store.ResolveIncident`, `execCount`, `incidentAction`,
`incident.resolve`, `resolveJob`, `store.CloseManual`, `notFoundError`.
Produces: `store.UnresolveIncident`, the `UnresolveIncident` operation.

## Task 9: honest counters and a warning that means something

Spec: `presentation.md` 4.6 (`GET /v1/status`), 4.1 (the cluster row's
runtime state). Roadmap deliverable 7; decision 7.

Today `CloserStats.closed` and `attached` are the last tick's numbers and
nothing says so, so a status line reading `closed 0` after an hour of work
looks like a closer that has done nothing. And a cluster whose credentials
expired a day ago is reported ready with no error: `sync.go`'s watch error
handler records a Forbidden error and logs everything else, `Ready()` was
set once when the initial list synced and only the reconnect loop clears
it, and that loop never runs because the informers are still alive,
failing their relists on a timer.

`incident.Closer` keeps `total TickResult` beside `last`, adds to it in
`Tick` under the same mutex, and gains `Totals() TickResult`.
`status.Closer` gains `ClosedTotal` and `AttachedTotal int64`;
`cmd/idios/status.go`'s `snapshot` fills them and `renderReport`'s closer
line becomes `closer    last tick %s, closed %d (%d total), attached %d (%d
total)`.

`internal/k8s/sync.go`: the watch error handler records every error, not
only a Forbidden one. The `forbiddenSet` becomes a `failedSet` keyed the
same way, so one informer failing on a timer writes the row once, and the
recorded message is the error's own text prefixed with the resource and
namespace (`list pods in namespace payments: getting credentials: exec:
executable aws failed with exit code 255`), which is the sentence a person
acts on. The Forbidden case keeps its wording and its role in `waitSync`,
where a forbidden informer is excluded from the sync wait and a merely
failing one is not: the first is a permanent fact about the Role, the
second is expected to come back.

`internal/k8s/watcher.go`: the first recorded failure of a connected watcher
sets ready false through `setReady`, so `ReadyChanged` fires, the cluster
stream carries the transition and the application's dot turns at once. A
probe goroutine, started by `runOnce` beside the informers and stopped with
them, then calls `clusterIdentity` on the same one-second-to-one-minute
backoff the reconnect loop uses, until it succeeds; success calls
`w.h.ClusterConnected`, which already clears `last_error` on the row, empties
the failed set and sets ready true again. The reflectors need nothing: they
relist on their own once the credentials work. This is a probe and not a
timer on events because with resync 0 an idle namespace delivers nothing for
hours on a healthy watcher.

`status.Cluster`, `StatusCluster` and the cluster row gain no field: `ready`
already exists on both wire messages, `last_error` and `last_error_at` are
already on `GET /v1/clusters`, and the application already draws a cluster
with `last_error` red in the menu bar and on the Status screen. What changes
is that the daemon now tells the truth in them. Phase 12 makes the sidebar
dot and the Status screen say "connected before, failing since
`last_error_at`" from these three fields.

`status.proto`: `CloserStats` gains `int32 closed_total = 4` and `int32
attached_total = 5`, with doc comments saying `closed` and `attached` are
the last tick alone.

Tests:

- `internal/incident/closer_test.go`, `TestTotalsAccumulateAcrossTicks`: two
  ticks closing one incident each; `Last()` reports the second tick alone
  and `Totals()` reports both. Trace: deliverable 7.
- `internal/k8s/watcher_test.go`, `TestARelistFailureIsAClusterError`: over
  the existing fake-client harness, a watcher that reached ready has one
  informer's list fail with a non-Forbidden error (the fake's reactor
  returning `errors.New("getting credentials: exec: executable aws failed
  with exit code 255")`): the cluster row's `LastError` and `LastErrorAt`
  are the prefixed message and `testNow`, whole pair compared; `Ready()` is
  false; `ReadyChanged` saw exactly `[true, false]`; a second failure of
  the same informer writes nothing more. Then the reactor is cleared and the
  probe's wait is advanced: `Ready()` is true, `LastError` is nil,
  `ReadyChanged` saw `[true, false, true]`. Edge row first: a Forbidden
  error still produces the existing `forbidden: list ...` wording and does
  not clear ready, because the existing tests
  `TestReadyDespiteForbiddenReplicaSets` and its pods twin say a forbidden
  resource must not block the rest. Trace: `presentation.md` 4.1 (the
  cluster row's runtime state), 4.6; decision 7.

Checkpoint (with `make generate-check`), commit `k8s: report a failing
relist as a cluster error and count closer work cumulatively`.

Consumes: `incident.Closer.Tick`, `Last`, `status.Snapshot`,
`k8s.register`, `forbiddenSet`, `waitSync`, `setReady`, `clusterIdentity`,
`Handler.ClusterError`, `Handler.ClusterConnected`, `store.SetClusterError`,
`store.MarkClusterConnected`, `closerStats`.
Produces: `Closer.Totals`, `status.Closer.ClosedTotal/AttachedTotal`,
`k8s.failedSet`, the probe, `closed_total`, `attached_total`.

## Task 10: an evicted pod says so

Spec: `data-storage.md` 5.3 (`deletion_reason`), 6.1 (`rescheduled`).
Roadmap deliverable 8.

Today `ingest.InferDeletionReason` reads the controller kind, the pod's
creation time, the rollout revisions and the newest live sibling's creation
time, so a StatefulSet pod evicted by the Eviction API during a node drain
is recorded `replaced (inferred)` while the `rescheduled` incident on the
same pod carries `EvictionByEvictionAPI`. On the real run the two sat one
above the other on one screen.

`0001_init.sql`: `pods.deletion_reason`'s list gains `'evicted'`. The
inference gains one input, whether the pod's newest `DisruptionTarget`
condition is `True`, which `processor.PodDeleted` reads from
`pod_condition_history` with the query the detail page already uses for the
latest condition per type; when it is, the reason is `evicted` and nothing
else is consulted, because a pod that was told to leave was not replaced by
a rollout even if a newer sibling exists. `store.DeletionReasonEvicted`
follows, and `api.deletionReasons` maps it.

Tests:

- `internal/ingest/deletion_test.go`: the existing table gains rows, edge
  first: a disrupted pod with a newer live sibling is `evicted`, not
  `replaced`; a disrupted Job pod is `evicted`, not `job_pruned`; a pod
  whose `DisruptionTarget` is `False` keeps today's answer. Trace: 5.3;
  deliverable 8.
- `internal/processor/delete_test.go`: one scenario over the
  `unschedulable`-style fixture set with a `DisruptionTarget=True` condition
  written before the delete; the whole pod row is compared with
  `deletion_reason evicted`. Trace: the same.

Checkpoint (`rm -rf .storage .storage/smoke` first; with
`make generate-check` if the enum is on the wire), commit `ingest: record an
evicted pod as evicted`.

Consumes: `InferDeletionReason`, `processor.PodDeleted`, the latest
condition query, `store.DeletionReason*`.
Produces: `store.DeletionReasonEvicted`, the `evicted` value.

## Task 11: the pod that is stuck rather than failing

Spec: `data-storage.md` 6.1 (categories), 6.3 (closing). Roadmap deliverable
9. Droppable if the phase runs long: nothing later depends on it, and Phase
12 draws it only if the category exists.

This is the one gap with no signal at all today: a pod waiting on a missing
PersistentVolumeClaim or a mistyped ConfigMap sits in `Pending` or
`ContainerCreating` forever and no reason ever appears in a container state,
so no incident opens. The Job half of the deliverable needs nothing: a Job
past its `activeDeadlineSeconds` is failed by Kubernetes itself with
condition `Failed`, reason `DeadlineExceeded`, which opens `job_failed`
today.

Config: `StuckAfter time.Duration` (`stuck_after`), default
`10 * time.Minute`. `Validate` rejects `<= 0`, unlike `scheduling_grace`:
zero would put the edge at `now` and open an incident for every pod that is
merely starting, which is the opposite of what this task is for. `Status`
gains `int32 stuck_after_seconds = 26`.

`0001_init.sql`: `incidents.category`'s list gains `'stuck'`. A note for
the day this becomes a migration: the vocabulary is a `CHECK` constraint,
SQLite cannot alter one, and a table rebuild with foreign keys on detaches
every `incident_id` child through `ON DELETE SET NULL`; the reason milestone
decision 9 exists is that none of that has to be solved before the schema is
stable.

`store.CategoryStuck`, `api.categories` and `CATEGORY_STUCK = 11` in
`common.proto` follow.

`internal/store/stuck_sql.go`:

```go
// StuckCandidate is a live pod that has been waiting since before the edge
// with no open incident, and the container holding it up.
type StuckCandidate struct {
    PodUID, Namespace, ContainerName, Reason string
    ClusterID                                int64
    WorkloadKind, WorkloadName               string
    Since                                    string
}

// ListStuckCandidates returns the live pods whose containers have all been
// waiting since before edge, with no incident of any category open on them.
func ListStuckCandidates(ctx context.Context, tx *sql.Tx, edge string) ([]StuckCandidate, error)
```

The predicate: `pods.deleted_at IS NULL`, `pods.created_at < edge`, every
app or sidecar container in `waiting` with `containers.updated_at < edge`,
and no row in `incidents` with `pod_uid = p.uid AND closed_at IS NULL`.
`Reason` is the container's waiting reason and `Since` is `pods.created_at`.

`internal/incident/stuck.go`: `StuckOps(candidates []store.StuckCandidate,
now time.Time) Ops` builds one `Open` per candidate. The row is filled
whole: `ClusterID`, `Namespace`, `SubjectKind: store.SubjectPod`, `PodUID`,
`WorkloadKind`, `WorkloadName` from the candidate, `Category:
store.CategoryStuck`, `FirstReason` and `LastReason` the candidate's reason
or `Pending` when the container reports none, `Occurrences: 1`, `OpenedAt`
the candidate's `Since` and `LastSeenAt` the formatted `now`. **`ContainerName`
is the candidate's container, never empty**: `CloseStableIncidents` closes a
non-scheduling incident only through `EXISTS (SELECT 1 FROM containers c
... AND c.name = incidents.container_name ...)`, so a pod-level row would
match no container and could only ever close as `pod_deleted`, which is the
"never recovered" reading this milestone exists to remove. `StuckOps` takes
no policy: the window was already applied by the edge the caller computed,
which keeps the comparison in one place.

`Closer.Tick` gains a third step in the same transaction, after the close
and the attach: `ListStuckCandidates(ctx, tx, clock.Format(now.Add(
-c.stuckAfter)))`, `StuckOps`, then `store.OpenIncident` per op, the ids
appended to the notified set. `TickResult` gains `Opened int64`, carried to
the printed line and to `CloserStats` as `int32 opened = 6` and `int32
opened_total = 7`. `NewCloser` takes `stuckAfter time.Duration`, and
`cmd/idios/run.go` passes `cfg.StuckAfter`.

The closer drives this and the decision still stays pure, which is milestone
decision 1 read exactly: `StuckOps` is a comparison over rows, in
`internal/incident`, and the tick is only what delivers them. Nothing else
can: with resync 0 no event arrives for a pod that is doing nothing, which
is the whole condition being detected.

Closing needs nothing new once `ContainerName` is set: the container runs
and is ready, and `CloseStableIncidents` closes the row `recovered` through
the arm that already covers every category but `scheduling`; a deleted pod
closes it `pod_deleted`.

Tests:

- `internal/store/stuck_sql_test.go`,
  `TestStuckCandidatesAreLiveWaitingPodsWithNoIncident`: table, edge cases
  first: a pod waiting since before the edge but with an open incident is
  not a candidate; one with only a closed incident is; a deleted pod is not;
  a pod waiting since after the edge is not; a running pod is not; a pod
  with one running and one waiting container is not. Whole-slice comparison
  of the candidates. Trace: deliverable 8.
- `internal/incident/closer_test.go`, `TestTickOpensStuckIncidents`: one
  tick over a seeded stuck pod opens one incident, the whole row compared
  including its `container_name`; a second tick opens nothing, because that
  incident is now open; then the container is made running and ready, its
  `last_seen_at` aged past the window, and the next tick closes the row
  `recovered`, which is the assertion that would have caught a pod-level
  row. Trace: 6.2's dedup rule and 6.3's stable-container row.
- `internal/incident/category_test.go`: the existing
  `TestCategoriesNeverReadPhaseExitCodeOrMessage` gains rows asserting no
  classifier returns `stuck`, because the category is opened by elapsed time
  and never by a reason. No new test function: that rule already has one.
  Trace: 6.1's opening sentence.
- `internal/store/ingest_sql_test.go`: the existing category-vocabulary
  row set gains `stuck` accepted, with a value outside the list still
  rejected. Trace: 6.1.

Checkpoint (`rm -rf .storage .storage/smoke` first; with `make
generate-check`), commit `incident: open an incident for a pod that is
stuck`.

Consumes: `Closer.Tick`, `TickResult`, `NewCloser`, `store.OpenIncident`,
`CloseStableIncidents`, `clock.Format`, `api.categories`.
Produces: `store.CategoryStuck`, `store.StuckCandidate`, `ListStuckCandidates`,
`incident.StuckOps`, `TickResult.Opened`, `config.StuckAfter`.

## Task 12: smoke, docs, roadmap, handoff

Run the phase against the OrbStack cluster, in `idios-smoke` only, before
touching the docs. `rm -rf .storage .storage/smoke`, `make smoke`, then `./bin/idios -data-dir .storage/smoke
-kubeconfig ./kube/config run`, then:

1. `kubectl -n idios-smoke apply -f hack/smoke/` plus a pod with a CPU
   request no node can meet: `GET /v1/incidents` holds no scheduling
   incident for the first minute and one after it; deleting that pod inside
   the minute leaves nothing behind at all.
2. A CronJob on a two-minute schedule, left for ten minutes:
   `GET /v1/jobs?cronjob_name=<name>&limit=5` answers five rows with `total`
   at the real count, and `artifacts_by_outcome` in `GET /v1/status` stops
   growing by one `file` per container per run.
3. `GET /v1/workloads?namespace=idios-smoke` holds one row per
   controller-less pod, each with its `pod_name`.
4. `curl -X POST .../incidents/<id>/resolve` then `curl -X DELETE
   .../incidents/<id>/resolve`: the row closes `manual` and opens again;
   letting the same problem recur after a resolve produces a second
   incident, not a revived one.
5. `idios status`: the closer line shows the last tick and the totals.
   Then break the cluster's credentials without stopping the daemon (point
   `kube/config` at a token that cannot refresh, or `orb stop`): within one
   relist `GET /v1/clusters` carries `ready false` and a `last_error` that
   names the failure, and the application's dot for that cluster turns;
   restore them and both clear without a restart.

Then:

- `data-storage.md`: Section 5.9 gains the four rollout columns; Section 6.1
  gains the `stuck` row and the note that it is the one category keyed on
  time rather than on a reason; Section 6.2 gains the grace window and the
  rule that a persisting condition opens but never attaches, and its step 5
  gains Task 6's exception, that a container which ran once and exited 0
  with no incident has no `log_previous` taken; Section 6.3
  gains the `PodScheduled=True` close, the row for "a later run of the same
  CronJob closes an earlier run", the sentence that a Job failure older than
  the window is recorded closed, and its reopen paragraph stops saying a
  manual close reopens; Section 6.4's resolve row names its inverse; Section
  8 says rollout rows expire on `deleted_at` alone, and why.
- `presentation.md`: Section 4.1's cluster row says `ready` is false while a
  relist is failing and that `last_error` then names why; Section 4.2's
  detail row names `job` and `last_pod_name`; Section
  4.5's `GET /v1/jobs` row names the three filters and the two counts, its
  `GET /v1/workloads` row names `pod_uid` and `pod_name` for kind `none`,
  and the workload detail row names `pods_live`, `pods_limit` and
  `pods_truncated`; Section 4.6 names the closer totals; Section 8 gains the
  `DELETE /v1/incidents/{id}/resolve` row and says a manual close is not
  reopened by recurrence; Section 13 gains `scheduling_grace` and
  `stuck_after`.
- `CLAUDE.md`, Conventions: the bullet "Schema changes are new files under
  `internal/store/migrations/`; `0001_init.sql` is never edited" becomes
  "Until the first stable release every schema change edits
  `0001_init.sql` in place and the databases are recreated; the migration
  mechanism waits for a stable schema"; a list/watch failure of any kind is
  a cluster error and clears ready until the identity probe succeeds; the
  grace window
  and the stuck threshold are comparisons in `internal/incident` against an
  injected `Policy`, never timers; a persisting condition opens but never
  attaches; an incident that must be able to close as `recovered` carries a
  `container_name`; `JobRow` lives in `jobs.proto` because `incidents.proto`
  cannot import `workloads.proto`.
- `docs/plans/m3-signal/roadmap.md`: Phase 11 status `complete <date>`.
  If Task 11 was dropped, its deliverable moves to
  "Deferred to milestone 4" with one line saying why.
- This plan gains its "Hands to the next phase" section: the field names
  Phases 12 and 13 build against, the store and query helpers they call, and
  whatever the phase found that contradicts the roadmap. Phase 12 can start
  once Task 10 has landed: every field it draws exists by then, and Task 11
  adds only the `stuck` category, which it draws if it is there.

Checkpoint (`make ascii`), commit `docs: close phase 11`.

## Self-review

Spec coverage. `data-storage.md` 6.1: Tasks 1 (scheduling stays
reason-keyed, only its timing changes) and 11 (the new category and the test
that says it is not reason-derived). 5.3: Task 10. 6.2: Tasks 1 and 2 (what opens), 6
(what is captured when it opens). 6.3: Tasks 1 (`PodScheduled=True`), 2 (the
CronJob close and the closed-at-open history row), 8 (manual is final). 6.4:
Task 8. 5.8: Task 2, with Task 4's edge row asserting the counters are still
never read. 5.9 and 8: Task 3. `presentation.md` 4.5: Tasks 4, 5, 7. 4.6 and
4.1: Task 9. Section 8: Task 8. Every roadmap deliverable has one task:
1 -> T1, 2 -> T2, 3 -> T3, 4 -> T4, T5, T6, 5 -> T7, 6 -> T8, 7 -> T9,
8 -> T10, 9 -> T11.

`.ai` rules. ASCII at every checkpoint (`make ascii`). Tests: each traces in
the prose to a design doc statement or to a roadmap bug; variants are table
rows; assertions compare whole rows, whole slices or whole wire messages;
edge cases are named first in every table (inside the window, the unknown
id, the pod with an open incident, the filter matching nothing, the
not-ready cluster). Comments: the reasons named in the tasks are the only
ones (why a persisting condition does not attach, why `job_finished` is
reused, why rollout rows expire on `deleted_at`, why the flags travel as
strings, why a clean single run is not evidence); no comment restates code
and none references this plan or any document. Scope: the only new files are
`policy.go`, `stuck.go`, `job_sql.go`, `stuck_sql.go` and `jobs.proto`, each
named by its task; the schema changes are three edits to `0001_init.sql`; no field ships without the task that
fills it; the sweeper's step list and signature are untouched; no ordering
is changed, because ordering is Phase 13's. Commits: one per task,
`area: imperative subject`, no body unless the why is not in the subject.

Type consistency. `incident.Policy` is built once in `cmd/idios/run.go` and
travels through `processor.New`; `Apply` and `ApplyJob` take it, and the
closer takes its own `stuckAfter`, because the closer is not the processor
and sharing the struct there would be a wrapper with one field.
`store.RolloutHistory`'s `CreatedAt` is a `string` on a `NOT NULL` column and
its three counts are `*int64` mapped to `optional` wire fields through the
existing `int32Ptr`.
`query.WorkloadKey` gains a field, so every map keyed by it in
`attachIncidentFacts` keeps working by construction. `query.JobRow` is what
both `ListJobs` and `IncidentDetail.Job` carry, so `api.jobRow` stays the
only mapper. `api.boolFilter` is the one parser for `live` and `failed` on
both list endpoints.

Risks named. Task 1 depends on pod updates continuing to arrive for an
unschedulable pod; they do, because the scheduler rewrites the condition
message on every attempt, but a cluster that stopped updating the pod
entirely would leave the incident unopened until Task 11's time-driven pass,
which is the task most likely to be dropped; if it is dropped, the roadmap
gets that sentence. Three tasks edit `0001_init.sql`, and each one's
checkpoint starts with `rm -rf .storage .storage/smoke`: a database built
by the previous commit fails the `CHECK` or the `NOT NULL` at the first
write, which is loud, so the risk is a wasted minute and not a silent
detachment. Task 9's probe runs beside live informers; the test asserts the
exact `ReadyChanged` sequence so a probe that flaps or never clears is
caught.
Task 2 moves three messages between proto files, and `make generate-check`
plus `make app-test` are what prove the generated Go and Swift names did not
move with them.

## Hands to the next phase

Fields Phase 12 draws on, all present on the wire today:

- `Rollout.created_at` (`string`, not optional), `replicas`,
  `ready_replicas`, `available_replicas` (`optional int32`).
- `WorkloadRow.pod_uid`, `pod_name` (set only for `workload_kind = none`).
- `GetWorkloadRequest.pods_live`, `pods_limit`; `WorkloadDetail.pods_truncated`.
- `JobsResponse.total`, `failed_total`; `ListJobsRequest.cronjob_name`,
  `live`, `failed`.
- `IncidentDetail.job` (a `JobRow`), `last_pod_name`.
- `CloserStats.closed_total`, `attached_total`, `opened`, `opened_total`.
- `Status.scheduling_grace_seconds`, `stuck_after_seconds`.
- `Category.stuck` (`CATEGORY_STUCK`); `DeletionReason.evicted`
  (`DELETION_REASON_EVICTED`).
- `UnresolveIncident` on `DELETE /v1/incidents/{id}/resolve`, returning
  `IncidentRow`.

Store and query helpers a later phase or a fix will call rather than
reinvent: `store.CloseCronJobIncidentsBefore`, `store.UnresolveIncident`,
`store.ListStuckCandidates`, `store.DeletionReasonEvicted`,
`store.CategoryStuck`, `query.CountJobs`/`JobCounts`, `query.GetWorkload`'s
`live *bool, p Page` parameters, `incident.Policy`, `incident.StuckOps`,
`api.boolFilter`.

The Swift model already carries `Category.stuck` with a yellow badge and a
sidebar folder (added ahead of this phase, so the enum-value rule in
`presentation.md` 3.2 never bit). It does not yet read
`Rollout.createdAt`/`replicas`/`readyReplicas`/`availableReplicas`,
`WorkloadRow.podUid`/`podName`, `WorkloadDetail.podsTruncated`,
`JobsResponse.total`/`failedTotal`, `IncidentDetail.job`/`lastPodName`,
`CloserStats.closedTotal`/`attachedTotal`/`opened`/`openedTotal`,
`Status.schedulingGraceSeconds`/`stuckAfterSeconds`, the `UnresolveIncident`
operation, nor the `evicted` deletion reason. `Pod.swift`'s `deletionReason`
decodes through `modelEnum`, which returns `nil` for a raw value the Swift
`DeletionReason` enum does not carry rather than throwing, so an `evicted`
row decodes safely today; it just shows nothing where a reason belongs
until Phase 12 adds the case. `api/testdata/status.json` leaves the closer
totals unset, so `cmd/idios/status_test.go`'s golden line reads
`closed 5 (0 total), attached 3 (0 total)`.

What the smoke run found that the roadmap did not predict: a pod's
`PodScheduled` condition does not always change again after the first
sighting. On the OrbStack test cluster, an unschedulable pod's scheduler
retried twice in the first 90 seconds and then stopped touching the
object; with the informers at resync 0, no further pod update ever arrived,
so the `scheduling` grace window (Task 1) had nothing to re-evaluate past
60 seconds and no `scheduling` incident ever opened for that pod. Ten
minutes later the `stuck` pass (Task 11) opened it instead, as category
`stuck` rather than `scheduling`, because a `Pending` pod with a waiting
container and no incident is exactly what it looks for. This is the risk
this plan's self-review named ("a cluster that stopped updating the pod
entirely would leave the incident unopened until Task 11's time-driven
pass"), observed on a real cluster rather than only reasoned about; Task 11
was not dropped, and it is what kept this pod from going unrecorded
forever. Phase 12 and 13 draw on this: a workload that never gets a
`scheduling` incident but does get a `stuck` one is a real path, not a bug
to route around.
