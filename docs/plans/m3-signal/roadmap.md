# idios Implementation Roadmap: milestone 3, signal

Status: complete 2026-08-31. All three phases shipped; no deliverable was
cut. Open hand-check items and milestone 4 candidates are in
`13-navigation.md` under "Hands to the merge".

Goal: every row the application shows is worth reading, and says the true
thing. Milestone 2 made the recorder legible; running it against real
clusters showed that legible is not useful. Twelve of fourteen incidents
on a two-cluster run were scheduling waits of 4 to 33 seconds that
resolved themselves, sixty-five of eighty-one pods were finished Job pods
painted in the danger colour, the column that would say when a revision
shipped held the daemon's connect time, and the two incidents that
mattered were titled after a pod that no longer existed. This milestone
fixes what is recorded, then what is said about it, then where it is said.

Specs: `docs/design/data-storage.md` for what is stored and why an
incident opens or closes, `docs/design/presentation.md` for the API and
the screens. Both are updated in place by the phase that changes the
system; `docs/mockups/idios-ui.html` is amended, not replaced, by the phase
that changes a screen, except where a decision deletes something, which is
struck from the mockup when the decision is taken.

## Where the work lands

Every commit of this milestone, from Phase 11's first task to the last
line of Phase 13, lives on the branch `m3-signal`. Nothing reaches `main`
until Phase 13 is finished and the user has agreed to the merge in that
session: no merge, no fast-forward, no cherry-pick, no push to `main`.
Phase checkpoints commit to the branch as usual, so the history stays one
commit per task and the branch is what a review reads.

The branch is checked out in place, not in a worktree. Everything the
phases test against is gitignored and lives beside the repository --
`.storage` and `.storage/smoke`, `kube/config`, `bin/` -- and a worktree
would start without any of it. Working in place keeps `make smoke`, the
mock server and the application screenshots pointing at the data that is
already there.

## Cross-cutting decisions this milestone adds

Every phase plan repeats the ones it touches in its Global Constraints.
The m1 and m2 decisions stay in force and are in `CLAUDE.md`.

1. **Noise is a recorder concern, never a presentation one.** If something
   should not be read, the daemon does not open it, or opens it closed.
   The application never hides a row to make a list calmer. The decision
   is made in `internal/incident`, which is pure and already takes `now`,
   so a grace window or a repeat threshold is a comparison, not a
   scheduler.
2. **Red means broken.** A container that exited 0 with reason `Completed`
   is the success state of a Job or an init container. The badge
   vocabulary in `BadgeStyle.swift` routes it to the neutral it already has
   and stays the only place a colour is chosen; the stray
   `.red`/`.green`/`.orange` in sidebar, menu bar, sheets and incident
   detail route through it.
3. **The reader's words on the surface, the store's words on hover.** A
   table name, a column name or a SQL predicate is a tooltip, never a
   visible label; `null` becomes "no limit" or "never terminated"; a chip
   is named after what it selects. The m2 provenance rule is kept: every
   value still traces to a column, and hovering says which.
4. **History that grew is collapsed with a count, never silently cut.** A
   rollout, run or pod list shows what changed and folds the rest behind a
   disclosure that says how many are folded and why. A section that can
   grow without a bound of its own becomes a tab, so the bound belongs to
   something.
5. **Long-lived kinds first, transient kinds last.** Wherever workloads or
   pods of different kinds share one list, the order is fixed:
   Deployments, StatefulSets, DaemonSets, then any other kind in
   alphabetical order (a real cluster has operator kinds such as a
   CloudNativePG `Cluster`, and the order must have a place for a kind it
   has never heard of), CronJobs, Jobs, then the pods no controller owns;
   empty kinds are not shown. A Deployment's pod is the
   same pod all week, a CronJob's pod lives for seconds, and the eye should
   find the stable things where it left them. Inside one kind, a pod list
   is live before finished before deleted, newest first. The order is
   never a user setting.
6. **A group heading says the category, a row says an identity.** "No
   controller" is a heading, never a row: it carries the phrase that
   removes the ambiguity of "pods", while the rows under it carry pod
   names, one per pod, each opening that pod.
7. **One highlight, one place you are.** The sidebar selection is the
   screen. State and category are filters: shown as filters, composing
   with each other, and visible in the toolbar while they are in force.
8. **A screen that duplicates another is removed, not left dangling.**
   When a capability moves, its route, its store and its tests move or go
   with it.
9. **Until the first stable release, the schema is `0001_init.sql`.** Every
   milestone up to that release is a testing phase and every database in
   existence is test data the user is willing to drop, so a schema change
   edits `0001_init.sql` in place, keeps the schema version at 1, and the
   phase that makes it says in its checkpoint that `.storage`,
   `.storage/smoke` and the user's own data directory must be recreated. No
   migration file is added and no column is made nullable to spare rows
   that predate it; a column for a value Kubernetes always sends is
   `NOT NULL`. The migration mechanism stays as it is for the day the
   first stable schema needs a second file.
10. **Contract first.** Anything that changes `api/proto` lands in Phase 11
   with a clean `make generate-check`, so Phases 12 and 13 build against
   fields that already exist.

## Phases

```
Phase 11  recorder semantics and contract    first; owns every proto and schema change
Phase 12  presentation truth                 after 11 (needs the new fields)
Phase 13  navigation and information         after 11; overlaps 12 only in the sidebar
```

### Phase 11: recorder semantics and contract (plan: `11-semantics.md`)

Status: complete 2026-08-30.

Delivers, in this order:

1. A grace window before a `scheduling` incident opens, and a close on the
   `PodScheduled=True` transition so recovery no longer waits for the
   closer's stabilization window. Today the first `Unschedulable`
   condition opens an incident with no grace, nothing closes on
   scheduling success, and a pod pruned inside the window closes as
   `pod_deleted`, which reads as "never recovered".
2. Job incidents that mean something. A `Failed` condition seen for the
   first time and older than the stabilization window opens already
   closed, as history; an open one closes when a later Job of the same
   CronJob completes; and the row carries the Job's own facts (name,
   condition reason, failed count, backoff limit, started, finished) plus
   the last pod name the recorder saw, so it survives pod pruning.
3. `rollout_history.created_at` from the ReplicaSet's creation timestamp,
   with the replica counts that say which revision is live, and the sweep
   fixed to delete rollout rows by `deleted_at` instead of `last_seen_at`
   (with resync 0 a stable ReplicaSet's history is currently deleted while
   it still exists).
4. Bounded lists for the two tables that grow one row per schedule tick: a
   live and a failed-only filter for jobs, together with the key and the
   counts that tie a Job to its workload, because the workload row carries
   no uid today and the application pulls a namespace page and filters it
   by name; and a limit and a live filter for a workload's pods. Capture
   skips the log of a container that exited 0 with no restarts and no
   incident, which on a two-minute CronJob is one log read per container
   per run, forever, for pods nothing asks about.
5. One workload row per pod for the pods no controller owns, per decision
   6: the list keys on the pod, not on an empty `workload_name`, and the
   row carries the pod's name and uid so the application can open it.
   Today every such pod in a namespace collapses into one row that has
   nothing to read.
6. An inverse for the one human action that has none, and an end to the
   silent reopening of a manual close.
7. Honest counters and watch state in status: closer numbers that say
   whether they are cumulative or per tick, and a cluster that stops being
   reachable stops being ready. Today only a Forbidden list/watch error
   reaches the cluster row; an expired SSO token fails every relist for a
   day while `ready` stays true and `last_error` stays empty, and the
   application shows a green dot over a cluster it has not heard from.
   Cleared by a positive signal (an identity probe on the reconnect
   backoff), never by an age threshold: with resync 0 a healthy watcher on
   an idle namespace legitimately hears nothing for hours, which a staleness
   rule cannot tell apart from a credential that stopped working.
8. An `evicted` deletion reason. A pod evicted through the Eviction API or
   the taint manager is recorded `replaced (inferred)` today, because the
   inference reads only sibling creation times, while the `rescheduled`
   incident on the same pod says what really happened; the banner and the
   incident should agree.
9. A time-driven category for the pod that is stuck rather than failing
   (Pending or ContainerCreating past a threshold, a Job past its
   deadline). This is the one gap with no signal at all today: a pod
   waiting on a missing volume produces no incident, because no reason
   ever appears in a container state. Keyed on time, not on a message, so
   the reason-only rule holds. Last task; droppable if the phase runs
   long.

Depends on: nothing new. Owns the proto change, the schema change and the
regeneration, so Phases 12 and 13 can start as soon as its contract tasks
land.

### Phase 12: presentation truth (plan: `12-presentation.md`)

Status: complete. No deliverable was cut. One find beside Task 7: the
workload screenshot route had never matched a row (the route spells the
cluster by name, the keys carry the id), fixed in three small commits;
the loose ends no task owned are listed in the plan's handoff.

Delivers: the badge semantics of decision 2 wherever a container state is
drawn, with the ready column saying "exited 0" instead of a red `false`;
the newest warning message as the headline under the title, in place of a
box that reads `null` where the explanation belongs; a Job card for a
job-subject incident instead of a kubelet card full of unknowns; a
"settling" tag while an incident is open but its pod already succeeded;
the newest previous log selected by default for a crash or an OOM, not the
oldest; a tabbed workload detail, where Overview holds only what cannot
grow (the stat cards and the restarts chart) and Pods, Rollouts, Runs and
Incidents each own their bound per decision 4, the history tab leading for
a CronJob because its runs are the signal and its pods are transient, the pods carrying the live,
not-running and open-incident filters in the order of decision 5, and the
rollouts column saying when a revision shipped, five newest first, one row
per ReplicaSet; the reader's words of decision 3 on every screen; a closed
incident
that looks closed, with one passive state label and its remaining actions
in an overflow menu; a rail that wraps the image and elides the digests
instead of the reverse; a container selection that does not paint over the
badges it is meant to explain; a pod row that navigates from anywhere in
the row; a pod's Incidents tab that counts what it lists ("2 incidents, 0
open" rather than "Incidents 0 open" above two closed rows); the cluster
dot in every list routed through one rule (ready and no error is green;
connected before and failing now is red with `last_error` on hover, the
menu bar and the Status screen already do this and the sidebar does not);
and a Status screen that answers "am I connected, am I missing anything,
how old is this" before it shows a sweep table.

Verification as in Phase 9: `swift test` and `xcodebuild` are the gate,
each screen is one task, and a person reads it from a screenshot against
the spec before the next begins.

Depends on: Phase 11's fields for the job card and the rollout column;
everything else only on m2.

### Phase 13: navigation and information (plan: `13-navigation.md`)

Status: complete 2026-08-31. No deliverable was cut. Deviations from the
plan as written: the flat grouping mode was absorbed into time rather
than kept beside it; a single click on an incident row now selects and
Return or a double click opens, which the keyboard deliverable implies;
and Task 5's screenshot caught a ForEach id collision that had always
collapsed the bare-pod tree rows into one, fixed by keying a
controller-less row on its pod uid. The detail screen's `a`/`d`/`r` keys
are in but compile-proven only; the plan's handoff lists that and the
other hand checks still open.

Delivers: the sidebar of decision 7, where the selection is the screen,
state and category are check-marked filters that compose, an all-states
row exists, and the active filter shows as removable chips in the toolbar
with the group control labelled again; a workloads tree that groups a
namespace by kind in the order of decision 5, with roll-up counts and no
extra indentation level, where a pod no controller owns is a row of its
own under the last heading and opens the pod, not a workload, instead of
collapsing into one row that sorts first; incidents grouped by cluster
above workload when more than one cluster is in scope, with cluster and
time as explicit modes and a group header that says enough to stay
collapsed; a menu bar popover that closes after it navigates, names the
cluster on each row, and says how many more there are; an empty cluster
scope that means empty and says so; a namespace picker that scrolls,
searches and selects all; the first keyboard layer (row selection with
arrows and Return, one key each for acknowledge, dismiss and resolve,
back, and the screens); and the Browser screen removed with its store
and its route, unwired from the sidebar and the screen switcher. Its
capability is not rebuilt elsewhere: a
controller-less pod is now a tree row under the last kind, a workload's
pods are its Pods tab, and a pod worth opening has an incident, which the
Incidents screen finds faster than any tree. A namespace-wide pod list
would be dominated by pruned CronJob pods, which is the noise this
milestone exists to remove.

Depends on: Phase 11 for the bounded pod list behind the Pods tab. Touches
the sidebar, so it lands after Phase 12's sidebar-adjacent work or
coordinates on the same file.

## Model choice per phase

Execution follows the m1 and m2 pattern: the orchestrator holds the plan
and pastes the `.ai/*.md` paths into every subagent prompt, a fresh
implementer runs each task, a reviewer checks the diff before the next.

| Phase | Orchestrator | Implementer | Reviewer | Why |
|---|---|---|---|---|
| 11 | Opus | Opus | Opus | Incident semantics decide what a person is woken for; a wrong grace window hides a real failure. |
| 12 | Opus | Opus | Opus | SwiftUI behaviour is version-specific and the implementer cannot see the result; every task ends in a screenshot review. |
| 13 | Opus | Opus | Opus | Removing a screen and rebuilding the selection model touches many files at once; the risk is in the seams. |

Plan writing for every phase: Opus, inline, after reading the design docs
and the code as it stands at that moment.

## Facts a phase plan needs that are not in the design docs

- Eleven rollout rows per Deployment is Kubernetes' default
  `revisionHistoryLimit` of ten plus the live ReplicaSet, all arriving on
  the initial list, so their `first_seen_at` is one instant per cluster
  connect. Two containers in the template make it twenty-two rows.
- On an autoscaled cluster a `FailedScheduling` is normally followed by a
  `Nominated` event naming a node claim, and the pod starts within a
  minute. That pairing is what a grace window sits above.
- Most pods in a real store are finished CronJob pods deleted by a history
  limit, recorded with deletion reason `job_pruned`; previous-log
  artifacts for them were almost half of all artifacts on one run.
- The two-minute CronJob is the worst case for list growth: twenty-two
  runs and twenty-two pods in forty minutes, all successful.

## Deferred to milestone 4

Kept here so the next milestone does not rediscover them, and so no phase
in this one quietly grows to include them.

- Polish that does not change what the tool means: a real restarts
  histogram with a scale, a newest-first timeline, log and pod.json panes
  that fill their tab, clock-time event columns, per-folder empty-state
  copy that teaches what a folder is, breadcrumb chips, a Dock badge, a
  command palette, and `idios://` deep links (the route parser exists).
- Incident fingerprinting: one problem per workload, container, category
  and reason, with a pod count, so twenty crash-looping replicas are one
  row and one acknowledgement. Repeat thresholds per category follow from
  it, as the general form of Phase 11's grace window.
- Suppression rules and quiet hours, for notifications only, never for
  what is stored; then notifications on a problem opening and an optional
  webhook, rate limited per fingerprint.
- The `rescheduled` category's coverage: on the real run 108
  `TaintManagerEviction` events arrived against one `rescheduled`
  incident, so the `DisruptionTarget` reasons the category keys on are
  checked against what the taint manager and the eviction API write.
- Deploy correlation now that a rollout has a real time: the revision
  that was live when an incident opened, and rollout markers on the chart.
- Copy as kubectl with the right context, open-in templates per cluster,
  an export bundle for one incident, and retention controls with a way to
  exempt pinned evidence from the sweep.

## Plan files

- `docs/plans/m3-signal/roadmap.md` (this file)
- `docs/plans/m3-signal/11-semantics.md` (Phase 11, written when it starts)
- `docs/plans/m3-signal/12-presentation.md` (Phase 12)
- `docs/plans/m3-signal/13-navigation.md` (Phase 13)
