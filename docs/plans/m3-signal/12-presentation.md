# Phase 12: presentation truth

Goal: every screen says the true thing about what the recorder kept. A
container that finished its work stops being painted as a failure; the
explanation of an incident sits under its title instead of a box that says
`null`; a job incident shows the Job, not a kubelet card full of unknowns;
a closed incident looks closed; the lists that grow live behind tabs that
own their bound and say how many rows they stand for; the reader's words
are on the surface and the store's words are on hover; and one rule colours
every cluster dot. Phase 11 put every needed field on the wire; this phase
makes the application read them and say them.

Architecture: unchanged. Model types are built only through `init(wire:)`
in `IdiosModel`; stores own every client call and stream; views read stores
and never import `IdiosAPI`; the only hard-coded colours are the badge
vocabulary in `BadgeStyle.swift`, which stays the one place a colour is
chosen. The daemon changes not at all: the Go edits in this phase are
confined to `internal/api/wire_test.go`'s fixture builder and the fixtures
it writes, so the Swift tests can decode the Phase 11 fields from the same
bytes the daemon serves.

Tech stack: unchanged. Swift, SwiftUI, deployment target macOS 15, the
generated OpenAPI client, `swift test --package-path macos` and
`xcodebuild` via `make app-test` and `make app`. No new dependency.

Spec: `presentation.md` Sections 4.2, 4.5, 4.6 (what the endpoints now
carry), 7.3, 7.4, 7.5, 7.6 (identity, treatments, images, what is not
known), 9.3 (the screens), 9.5 (appearance); `data-storage.md` 5.8 (Job
success comes from the condition), 6.3 (close reasons), 6.4 (human
actions). The visual reference is `docs/mockups/idios-ui.html`, amended in
place by the task that changes a screen.

Global constraints: every `.ai/*.md` rule, applied to Swift as CLAUDE.md
says (ASCII, comments say why, one-line doc comment per type, tests trace
to a spec statement with whole-value assertions). The m3 cross-cutting
decisions this phase touches:

- 2 (red means broken: exited 0 with reason `Completed` routes to the
  neutral the vocabulary already has; the strays in sidebar, menu bar,
  sheets and incident detail route through `BadgeStyle`),
- 3 (the reader's words on the surface, the store's words on hover; every
  value still traces to a column and hovering says which),
- 4 (history that grew is collapsed with a count, never silently cut; a
  section that can grow without a bound of its own becomes a tab),
- 5 (long-lived kinds first, transient kinds last -- this phase takes only
  the filter order it implies; list ordering itself is Phase 13's).

Everything lands on `m3-signal`; nothing merges to `main`. The sidebar is
touched twice here (the cluster dot in Task 3, the folder titles in Task
4); both changes are local to rows that Phase 13's rebuild keeps, and the
handoff names them so Phase 13 coordinates on the same file.

Model per task: a fresh implementer per task -- Sonnet where the work is
mechanical SwiftUI (Tasks 3, 4, 8), Opus where it is structural or
version-specific (Tasks 1, 2, 5, 6, 7). Opus reviews every diff against
the task and the `.ai` rules before the next task starts. Every screen
task ends in `hack/macos/screenshot.sh` output that a person reads against
the spec before the next task begins.

Verification data: `make smoke` fills `.storage/smoke`, then
`./bin/idios -data-dir .storage/smoke -kubeconfig ./kube/config run`
serves it on 7770 (never together with `idios mock`). The smoke pods are
deleted when the run ends, so the incidents close as `pod_deleted`;
`kubectl -n idios-smoke apply -f hack/smoke/` reopens them while the
daemon is up. Task 2 adds the two CronJobs the later screenshots need:
today nothing in `hack/smoke/` produces a job incident or a container
that exited 0.

## Decisions this plan makes

Written here because the specs left them open; Task 9 moves the durable
ones into `presentation.md`.

1. **Success is decided at the drawing site from state and exit code, and
   the vocabulary stays in `BadgeStyle`.** `ContainerState.badge` cannot
   say "terminated well" because it does not see the exit code, so the
   mapping becomes a function of both: terminated with a known exit 0 is
   the neutral style, terminated otherwise is red, running and waiting are
   unchanged. A terminated container with no exit code keeps red: an
   unknown final state is not evidence of success. A pod-level row (no
   container in hand) reads `phase == "Succeeded"` for the same neutral;
   this is display only, and the reason-only rule binds incident
   categories, which are decided in the daemon and untouched here.
2. **One cluster-health rule, owned by `BadgeStyle`.** `last_error` set is
   red whatever `ready` says (an error outlives the connection that
   produced it, and Phase 11 made every list/watch failure record one);
   ready with no error is green; otherwise grey. The sidebar, the menu bar
   icon and rows, the workloads tree and the Status screen all call the one
   helper; the sidebar's inline red error text becomes hover text like the
   spec says ("red with last_error on hover"), and the menu bar's orange
   error line takes the red vocabulary. Sheets' and the incident detail's
   stray `.red` route through `BadgeStyle.red.text`.
3. **The reader's words are derived, not enumerated.** Every closed
   vocabulary shown on the surface (`Category`, `IncidentState`,
   `CloseReason`, `DeletionReason`, `DeletionSource`, `CaptureGap`) gains
   one derived `label`: the raw value with underscores as spaces
   (`image_pull` -> `image pull`, `pod_deleted` -> `pod deleted`). The raw
   stored value moves to the hover and stays what copy produces. A
   per-case table of prose names is not built: the stored words are
   already the reader's words once the store's spelling is removed, and a
   table would be a second vocabulary to keep true. `DetailCard`'s `meta`
   string (today visible tertiary text) becomes a tooltip on the card
   title, which is the m2 provenance rule kept and the visible SQL
   removed. Visible `null` is replaced per field by the fact it stands
   for ("never terminated", "no limit", "none attached"), named task by
   task.
4. **The workload detail's tabs are fixed per kind, and Overview holds
   only what cannot grow.** Overview is the four stat cards and the
   restarts chart. A CronJob shows Overview, Runs, Pods, Incidents with
   Runs selected on open: its runs are the signal and its pods are
   transient. Every other kind shows Overview, Pods, Rollouts, Incidents
   with Overview selected; the Rollouts tab is dropped when the daemon
   returned no rollout row and the kind is not Deployment, because only a
   Deployment's ReplicaSets fill `rollout_history`. Bounds: the Pods tab
   asks `pods_limit=50` and says "showing the newest 50 of N" from
   `pods_truncated` and the row's own counts; the Runs tab asks
   `limit=50` and heads itself with `total` and `failed_total` ("22 runs,
   3 failed"); the Rollouts tab shows the five newest rows and folds the
   rest behind a disclosure that says how many ("6 older revisions").
5. **The Pods tab's filters are chips in the order of decision 5: Live,
   Not running, Open incident.** Live maps to the request's
   `pods_live=true`. Not running and Open incident are decided over the
   returned page (`worstState != running`, `openIncidents > 0`): the
   daemon has no such filters and this phase adds no contract. When the
   page is truncated the filter text says so ("of the newest 50"), per
   decision 4's never-silently-cut.
6. **The settling tag reads the pod's phase.** An open incident whose
   `detail.pod.phase` is `Succeeded` shows a yellow `settling` tag beside
   the state tag, with the hover saying the pod already succeeded and the
   closer confirms before closing. Display only, detail screen only: the
   list row's wire message does not carry the pod's phase, and adding a
   field for a tag would be contract growth Phase 11 did not make.
7. **The newest previous log is the default selection; the chip order
   stays oldest first.** The chips read left to right as the story of the
   restarts, and that stays; what changes is which chip starts selected:
   the previous log with the highest restart count when one exists (the
   instance that died closest to the incident), else `current.log`. One
   rule, used by the incident screen and the pod screen.
8. **A closed incident's header tag is passive and its actions fold
   away.** The header shows one neutral tag, `closed - <reason label>`,
   with `closed_at` raw on hover; the coloured state vocabulary stays for
   list rows, where colour is scanned across many rows -- on the detail
   the incident is the whole screen and the colour has nothing left to
   say. While open, the action buttons stay flat as today; once closed
   they move into one overflow menu (Note, Dismiss/Undismiss,
   Acknowledge/Unacknowledge, Delete), joined by Unresolve exactly when
   `close_reason == manual`, the one close a person can take back
   (`DELETE /v1/incidents/{id}/resolve`).
9. **The pod's Incidents tab title counts what it lists.** `"N incidents,
   M open"` (singular "1 incident"), where open is `closedAt == nil`,
   the same fact the daemon's open list keys on. Two closed rows head
   themselves "2 incidents, 0 open", never "Incidents 0 open".
10. **The `stuck` folder stays beside `scheduling`.** The sidebar's
    category order already places it seventh, between `scheduling` and
    `node_pressure`; it stays there because `stuck` is the time-driven
    sibling of `scheduling` (Phase 11's handoff: a pod that never gets a
    `scheduling` incident but does get a `stuck` one is a real path), and
    Phase 13's sidebar rebuild keeps the placement. Recorded so the
    question is settled, not rediscovered.
11. **The Browser screen gets no words pass.** Phase 13 removes it with
    its store and route (roadmap deliverable); polishing copy on a screen
    with one phase to live is work decision 8 exists to prevent. Its two
    stray accent colours are left alone for the same reason.

## File structure

```
internal/api/wire_test.go          fixture builder fills the phase 11
                                   fields; a job-subject detail case
api/testdata/
  workload_detail.json             rollouts gain created_at and the counts;
                                   podsTruncated
  workloads.json                   a kind-none row with pod_uid, pod_name
  jobs.json                        total, failed_total
  incident_detail_job.json         new: a job-subject IncidentDetail with
                                   job and last_pod_name
  pods.json                        a second deleted row, reason evicted
  status.json                      closer totals and opened; stuck_after
cmd/idios/status_test.go           golden line gains the totals
macos/Sources/IdiosModel/
  Enums.swift                      DeletionReason + evicted; label
  Workload.swift                   Rollout + createdAt, replicas,
                                   readyReplicas, availableReplicas;
                                   Workload + podUid, podName;
                                   WorkloadDetail + podsTruncated;
                                   Page<Job> + total, failedTotal
  Incident.swift                   IncidentDetail + job, lastPodName
  Status.swift                     CloserStats + closedTotal,
                                   attachedTotal, opened, openedTotal;
                                   DaemonStatus + schedulingGraceSeconds,
                                   stuckAfterSeconds
macos/Tests/IdiosModelTests/       the new fields in the existing tests;
                                   the job detail and evicted rows
macos/idios/Views/Components/
  BadgeStyle.swift                 the exit-0 route; the cluster rule
  Badges.swift                     ContainerStateBadge takes the exit code
macos/idios/Views/                 per screen: IncidentDetail/*, Pod/*,
                                   Workloads/*, Status/*, MenuBar/*,
                                   Incidents/IncidentsSidebar.swift
macos/idios/Store/
  WorkloadsStore.swift             bounds, filters, counts, the workload
                                   incidents call
  IncidentDetailStore.swift        unresolve
hack/smoke/
  cronjob-fail.yaml                new: a CronJob whose runs fail
  cronjob-ok.yaml                  new: a CronJob whose runs succeed
docs/mockups/idios-ui.html         amended by each screen task
docs/design/presentation.md        Task 9
docs/plans/m3-signal/roadmap.md    Task 9
```

## Task 1: the model reads the Phase 11 contract

Spec: `presentation.md` 4.2 (`job`, `last_pod_name`), 4.5 (rollout
columns, `pods_truncated`, `total`, `failed_total`, `pod_uid`,
`pod_name`), 4.6 (closer totals, the two new intervals), 9.2 (the model
layer unwraps what is guaranteed), 11 item 3 (both sides tested on
identical bytes). Phase 11's handoff lists exactly these fields as "on the
wire, not yet read". Implementer: Opus (two languages, and the fixture
edits ripple into a Go golden test).

Today the generated Swift types carry every field (the vendored
`openapi.yaml` is byte-identical to `api/openapi/IdiosService.openapi.yaml`)
and `IdiosModel` reads none of them; `api/testdata` leaves them all unset,
so there are no bytes for a Swift test to decode.

Go, `internal/api/wire_test.go` only (no daemon code changes):

- The fixture builder fills, with values distinct from every neighbour:
  `Rollout.CreatedAt` plus the three replica counts on both rollout rows
  of `workload_detail.json` (one row with all three counts, one with them
  absent, so the optional path is covered); `WorkloadDetail.PodsTruncated:
  true`; a third `WorkloadRow` with `workload_kind` none carrying
  `PodUid`/`PodName`; `JobsResponse.Total` and `FailedTotal`;
  `CloserStats.ClosedTotal`, `AttachedTotal`, `Opened`, `OpenedTotal` and
  `Status.StuckAfterSeconds`; a second deleted pod row in `pods.json`
  with `DeletionReason: evicted`.
- A new case writes `incident_detail_job.json`: a job-subject
  `IncidentDetail` (subject_kind `job`, `job_uid` set, no pod, no
  containers) whose `Job` is a `JobRow` with a `Failed` condition and
  whose `LastPodName` is set. The fixture server's `detail` field is
  swapped for that case the way the existing cases fix their message.
- `go test ./internal/api -run TestWire -update` rewrites the fixtures;
  `cmd/idios/status_test.go`'s golden line changes to carry the totals
  (today it reads `closed 5 (0 total), attached 3 (0 total)` because the
  fixture left them unset).

Swift, `macos/Sources/IdiosModel/`:

- `Workload.swift`: `Rollout` gains `createdAt: Timestamp` (required: the
  column is `NOT NULL`, so `init(wire:)` throws when absent) and
  `replicas`, `readyReplicas`, `availableReplicas: Int32?`; `Workload`
  gains `podUID: String?` and `podName: String?`; `WorkloadDetail` gains
  `podsTruncated: Bool` defaulting false; `Page<Job>` gains `total:
  Int32` and `failedTotal: Int32` (proto3 drops a zero, so absent takes
  zero as every other absent scalar does).
- `Incident.swift`: `IncidentDetail` gains `job: Job?` (the existing
  `Job` model, same module) and `lastPodName: String?`.
- `Status.swift`: `CloserStats` gains `closedTotal`, `attachedTotal`,
  `opened`, `openedTotal: Int32`; `DaemonStatus` gains
  `schedulingGraceSeconds: Int32` and `stuckAfterSeconds: Int32`.
- `Enums.swift`: `DeletionReason` gains `case evicted`. `modelEnum` is
  lossy by design, so today an evicted row decodes as nil and three
  screens print "unknown"; after this task the word is `evicted`.

Tests (`macos/Tests/IdiosModelTests/`, whole-value assertions as the
existing ones):

- `WorkloadTests`: the `workload_detail.json` assertion gains the rollout
  fields on both rows and `podsTruncated`; the `workloads.json` assertion
  gains the kind-none row whole; the `jobs.json` assertion gains the two
  counts. Trace: 4.5's rows.
- `IncidentTests`: a new case decodes `incident_detail_job.json` and
  asserts the whole `IncidentDetail`, `job` and `lastPodName` included.
  Trace: 4.2's detail row.
- `PodTests`: the `pods.json` assertion gains the evicted row whole, so
  the enum case is proven against real bytes. Trace: the storage doc's
  deletion reasons via 4.3.
- `StatusTests`: the `status.json` assertion gains the six new values.
  Trace: 4.6.

Checkpoint: `go build ./... && go test ./... && make ascii && make
app-test && make app`. Commit `macos: read the phase 11 wire fields`.

Consumes: the Phase 11 contract, `modelEnum`, `require`, `Timestamp`.
Produces: every field the later tasks draw; `incident_detail_job.json`;
the evicted vocabulary end to end.

## Task 2: a clean exit is not a failure

Spec: roadmap decision 2 (red means broken); `presentation.md` 9.5 (the
badge vocabulary is the only hard-coded colour); 4.3 (the ready meaning is
implied by kind). Roadmap deliverable "the badge semantics of decision 2
wherever a container state is drawn, with the ready column saying 'exited
0' instead of a red `false`". Implementer: Opus (the vocabulary is
semantic, and every drawing site must be found, not guessed).

Today `ContainerState.badge` (`BadgeStyle.swift:112-118`) returns red for
`terminated` unconditionally; sixty-five of eighty-one pods on the real
run were finished Job pods painted in the danger colour. The exit code
never reaches a colour decision anywhere, although `Container`,
`Incident`, `TimelineEntry` and the transition rows all carry one.

- `BadgeStyle.swift`: `ContainerState.badge` becomes `badge(exitCode:
  Int32?) -> BadgeStyle`: `.terminated` with `exitCode == 0` is
  `.neutral`; everything else as today. A convenience keeps the
  no-argument form for `waiting`/`running` call sites that have no exit
  code by passing nil. Pod-level: one small helper in the same file,
  `podBadge(worstState:phase:)`, neutral when `phase == "Succeeded"`,
  else `worstState.badge(exitCode: nil)`; the file stays the only place a
  colour is chosen.
- `Badges.swift`: `ContainerStateBadge` gains `exitCode: Int32?` and
  passes it through; its text is unchanged (`terminated - Completed` is
  the true state; only the colour lied).
- Call sites, each passing what it holds: `PodContainers` state cell
  (`container.exitCode`), `KubeletCard` current-state colour
  (`container.exitCode`), `TimelineView` dots (`entry.exitCode`),
  `PodHistoryCards` transition rows if they badge a state, `SiblingsCard`
  if it badges one, `WorkloadDetailView.state(pod)` and
  `BrowserScreen.podColor` (both through `podBadge` with the row's
  `phase` -- `PodRow.phase` is required, so it is in hand),
  `BrowserScreen`'s container glyph (`container.exitCode`).
- The READY column (`PodContainers.swift:48-60`): a container whose
  `ready` is false but whose state is terminated with exit 0 shows
  `Badge(text: "exited 0", style: .neutral)` instead of a red `false`;
  the kind-dependent helper text stays for the other cells. `ready ==
  true` stays the green `true`.
- The EXIT column's `alarming` expression is left as is: exit 0 is
  already non-alarming there.
- `hack/smoke/cronjob-fail.yaml` (schedule `*/1 * * * *`, a container
  that exits 1, `backoffLimit: 1`, tight history limits) and
  `cronjob-ok.yaml` (same schedule, exits 0) join the smoke set so a
  person can read a Completed container, a job incident and a Runs tab
  in every later screenshot. `hack/smoke/run.sh` already applies the
  whole directory.

No Swift unit test: the mapping lives in the view layer, which the suite
does not reach by design (`presentation.md` 11 item 6 gives the screens to
a person); the screenshot is the verification.

Screenshot review: after `make smoke` (with the CronJobs applied and at
least two ticks waited out) and the smoke daemon up:
`hack/macos/screenshot.sh pod/<ok-run pod uid>/containers <png>` -- the
reviewer checks the Completed container's state badge is neutral, the
READY cell reads `exited 0` in neutral, the EXIT cell is quiet, and a
crash pod's terminated badge is still red.

Checkpoint (`make app-test && make app` beside the Go three), commit
`macos: route a clean exit to the neutral badge`.

Consumes: `BadgeStyle`, `ContainerStateBadge`, `Container.exitCode`,
`PodRow.phase`, `TimelineEntry.exitCode`.
Produces: `ContainerState.badge(exitCode:)`, `podBadge`, the two smoke
CronJobs; every later screenshot has success to show.

## Task 3: one rule for every cluster dot

Spec: roadmap deliverable "the cluster dot in every list routed through
one rule (ready and no error is green; connected before and failing now is
red with last_error on hover)"; decision 2's strays; `presentation.md` 7.6
("a cluster with last_error is drawn red in every list that names it").
Implementer: Sonnet (mechanical once decision 2 of this plan is stated).

Today the same three-state decision is written four times --
`IncidentsSidebar.swift:185`, `MenuBarView.swift:164`,
`WorkloadsScreen.swift:269`, `StatusScreen.swift:165` -- two of them in
raw SwiftUI colours; the menu bar paints the error text orange where the
sidebar paints it red; and the sidebar shows `last_error` as inline text
where the spec wants it on hover.

- `BadgeStyle.swift` gains the one helper, `clusterDot(ready: Bool,
  hasError: Bool) -> Color`: error red, ready green, else
  `BadgeStyle.grey.text`. The four sites call it; the copies go.
- The menu bar icon (`MenuBarView.swift:15`) and its cluster error line
  (`:152`) take the red from the vocabulary; the popover header dot
  (`:50`) keeps meaning "open incidents exist" but takes
  `BadgeStyle.red.text`.
- The sidebar cluster row moves `last_error` to `.help` on the row and
  drops the inline red `WrapText` (`IncidentsSidebar.swift:135-139`); the
  dot plus hover is the spec's shape, and the Status screen remains where
  the full error text lives.
- Strays route through the vocabulary: the error texts in
  `AddClusterSheet` (:42, :70), `ClustersSheet` (:32, :106),
  `IncidentDetailScreen` action error (:111) and the Delete tint (:238)
  become `BadgeStyle.red.text`. The Browser screen is left alone per
  decision 11.

Screenshot review: `hack/macos/screenshot.sh menubar <png>` and
`... status <png>` against the smoke daemon -- the reviewer checks the
menu bar cluster line and the Status dot agree, and (by stopping `orb` or
pointing at a dead context if convenient) that an erroring cluster is red
in sidebar, menu bar and Status alike; at minimum, that the healthy state
is one green in all three.

Checkpoint, commit `macos: route every cluster dot through one rule`.

Consumes: `BadgeStyle`, `Cluster.lastError`, `Cluster.ready`.
Produces: `clusterDot`, called by the four screens; no stray semantic
colour outside `BadgeStyle.swift` except Browser's two accents.

## Task 4: the reader's words on the shared surfaces

Spec: roadmap decision 3; `presentation.md` 7.4 (raw value on hover and on
copy). Roadmap deliverable "the reader's words of decision 3 on every
screen" -- this task builds the mechanism and applies it to the screens no
later task owns (sidebar, menu bar, incidents list); Tasks 5-8 apply it to
theirs. Implementer: Sonnet.

Today `Badge` text is `rawValue`, so `image_pull`, `node_pressure`,
`pod_deleted` and `job_finished` are visible labels; `DetailCard`'s
`meta` renders SQL predicates as on-screen text on eleven cards; and the
sidebar's category folders are snake_case.

- `IdiosModel/Enums.swift` (or `Display.swift`): a `label` extension on
  the six surface vocabularies (`Category`, `IncidentState`,
  `CloseReason`, `DeletionReason`, `DeletionSource`, `CaptureGap`),
  derived as underscores-to-spaces per decision 3 of this plan. No test:
  a one-line string derivation fails the deletion test.
- `CategoryBadge`, `StateBadge` and `GapBadge` show `label` and gain
  `.help(rawValue)`; the direct `rawValue` call sites follow
  (`IncidentsSidebar.swift:91` folder titles, `MenuBarView.swift:111-124`
  `incidentText`, `IncidentGrouping`, `WorkloadDetailView.swift:72` and
  `StatusScreen.swift:456,462` category and reason chips,
  `IncidentRowView` state/category badges, deletion-reason phrases in
  `IncidentRowView.swift:72-78`).
- `DetailCard` renders `meta` as `.help` on the title instead of visible
  tertiary text; `SweepCard`'s meta, which carries the run's `ran_at` and
  cutoff rather than provenance, moves that line into the card body in
  clock words and keeps `sweep_runs` as the hover.
- The stuck folder stays where it is per decision 10; this task changes
  folder titles only.

Screenshot review: `hack/macos/screenshot.sh incidents <png>` and
`... menubar <png>` -- the reviewer checks the sidebar folders read
`image pull`, `node pressure`, `job failed`, the list badges match, no
card shows a WHERE clause, and hovering is where the store words went
(the reviewer confirms one tooltip by hand; the screenshot cannot).

Checkpoint, commit `macos: put the reader's words on the surface`.

Consumes: `Badge`, `DetailCard`, the enums.
Produces: `label` on six vocabularies, used by every later task;
`DetailCard.meta` as hover.

## Task 5: the incident detail says what it knows

Spec: `presentation.md` 4.2 (the detail carries `job` and
`last_pod_name`), 7.4 (wrap for messages), 7.6 (what is not known is
said), Section 8 (unresolve is the inverse of resolve, manual only);
`data-storage.md` 5.8 (the Job's own facts), 6.4. Roadmap deliverables:
the headline message, the Job card, the settling tag, the newest previous
log, the closed look with the overflow menu. Implementer: Opus.

Today the explanation of an incident sits at the bottom of the kubelet
card in a box that reads `null: no event message is attached to this
incident` when `last_message` is absent; a job-subject incident falls into
the pod-level card ("the pod itself; container_name is empty", phase
"unknown", QoS "unknown") because its pod is pruned and nothing reads
`detail.job`; the default log chip is the oldest previous log
(`chips.first` after `artifactOrder`, which sorts restart ascending); a
closed incident shows five flat toolbar buttons of which one is greyed
out; and `UnresolveIncident` exists on the client with no call site.

- Headline (`IncidentHeader.swift`): under the title sentence, the
  explanation wrapped at reading size: `incident.lastMessage` when set,
  else the newest attached Warning event's message from `detail.events`,
  else nothing -- the box in `KubeletCard.swift:76-93` is removed, and
  with it the visible `LAST MESSAGE (incidents.last_message)` label; the
  headline carries `.help("incidents.last_message")` or the event's
  provenance accordingly.
- Job card (`KubeletCard.swift` or a sibling `JobCard.swift`): when
  `incident.subjectKind == .job`, the kubelet card is replaced by the
  Job's own facts from `detail.job`: name, condition (type, reason label,
  message wrapped), "failed N of backoff limit M" (the counters are
  context, per the workload card's existing footer language), started,
  finished with duration, the CronJob's name, and `last_pod_name` with
  the note that the pod may since have been pruned. Fields the row does
  not carry are absent, not "unknown". The card's provenance hover is
  `jobs WHERE uid = incidents.job_uid`. `Incident.subjectKind` and
  `jobUID` are decoded today and unread; this is their reader.
- Settling tag (`IncidentHeader.swift`): per decision 6, a yellow
  `settling` tag beside the state tag while `closedAt == nil` and
  `detail.pod?.phase == "Succeeded"`.
- Default log selection: `artifactOrder` keeps its oldest-first reading
  order; the default in `IncidentDetailScreen.swift:242-246` and
  `PodScreen.swift:222-233` becomes decision 7's rule via one shared
  helper (newest previous log, else `current.log`).
- Closed look: the header state tag per decision 8 (neutral,
  `closed - <reason label>`, `closed_at` on hover). The toolbar: open
  incidents keep today's flat row; a closed incident shows the one
  passive tag and a single overflow `Menu` holding Note,
  Dismiss/Undismiss, Acknowledge/Unacknowledge, Delete, plus Unresolve
  when `closeReason == .manual`. `IncidentDetailStore` gains
  `unresolve()` beside `resolve()` with the same output unwrapping and
  the same reload-on-stream behaviour; a cancelled call maps to
  `APIError.cancelled` and is ignored as in every store.
- Reader's words on this screen: the rail's `RailRow` labels
  (`opened_at`, `last_seen_at`, `closed_at`, `close_reason`,
  `acknowledged_at`, `dismissed_at`, `first_seen_at`) become words
  ("opened", "last seen", ...) with the column name joining the raw
  timestamp in the existing hover; the kubelet card's `null` values
  become the fact ("never terminated" for last-terminated, "none
  recorded" where a value was never observed); the deleted banner
  wording keeps `(inferred)`.

No Swift unit test: the store method is exercised against the daemon and
the screen by a person; the model already proved `job` and `lastPodName`
decode in Task 1.

Screenshot review, against the smoke daemon with the CronJobs applied:
`hack/macos/screenshot.sh incident/<open crash id> <png>` (headline
present, settling absent, flat buttons), `incident/<job incident id>`
(the Job card, no "unknown", the last pod name), and after `resolve` on
one row, `incident/<that id>` (passive closed tag, one overflow control,
Unresolve inside it -- the reviewer opens the menu by hand). The
`scheduling`-free `stuck` path from Phase 11's handoff is real data here
if the cluster produced one; the reviewer treats it as such, not as a
bug.

Checkpoint, commit `macos: say what the incident detail knows`.

Consumes: Task 1's `job`, `lastPodName`; Task 4's labels; `Menu`.
Produces: the Job card, the settling tag, the shared default-chip rule,
`IncidentDetailStore.unresolve`.

## Task 6: the pod screen stops fighting its reader

Spec: `presentation.md` 7.4 (wrap for image references on a detail page,
middle elide for digests and ids), 4.3 (the pod detail), 9.4 (Table on
macOS 15 clips at ideal widths; prose tables are custom rows). Roadmap
deliverables: the rail treatments, the selection paint, the row click
target, the tab count. Implementer: Opus (the selection work is
version-specific).

Today `PodRail.selected` tail-truncates the image reference to one line
(`ExpandableText`) while wrapping `image_id` and `container_id` in full
over four lines each -- the reverse of useful, since a digest's
information is at its ends; the containers `Table`'s accent selection
paints under the translucent badge fills it is meant to explain; the
Incidents tab header reads `Incidents 0 open` above two closed rows; and
`PodIncidentsCard` rows take clicks only on their drawn glyphs
(`Button` + `.buttonStyle(.plain)` with no `contentShape`).

- Rail: the image reference becomes `WrapText` (a detail page never
  elides prose-like values); `image_id` and `container_id` become
  `MiddleElidedText` with the copy control they already have. The uid
  row is already middle-elided and stays.
- Selection: the containers table becomes custom rows (the repo's own
  rule for tables that need control on macOS 15), selection drawn as a
  low-opacity accent wash plus a leading accent bar, chosen so every
  badge keeps its own fill and text; selection still drives the rail and
  the browser binding as today.
- Tab title per decision 9: `"N incidents, M open"` with the singular.
- Click targets: `PodIncidentsCard` rows gain `.contentShape(Rectangle())`
  so the whole row navigates; the task greps
  `buttonStyle(.plain)` under `macos/idios/Views` and fixes any other
  row-shaped button missing one (the incident detail's siblings card is
  the known second).
- Reader's words on this screen: the containers card's visible
  `containers WHERE pod_uid = ?` and friends are hover now (Task 4's
  `DetailCard`); the column headings lose their column-name spelling
  (`CAPTURED_AT / GAP` -> `CAPTURED / GAP`); `PodRail`'s visible `null`s
  become the fact ("no requests written", "no limit", "never terminated",
  "still exists" keeps its sentence); the deleted banner says `evicted`
  now that the model carries it (Task 1), with the raw value on hover.

Screenshot review: `hack/macos/screenshot.sh pod/<crash pod uid> <png>`
and `pod/<uid>/incidents <png>` -- the reviewer checks the rail (image
wrapped whole, digests elided middle-out with copy), selects a container
by hand to see the badges stay legible, the tab reads `2 incidents, 1
open` in the shape of what it lists, and a click on a row's empty
trailing space navigates.

Checkpoint, commit `macos: fix the pod screen rail, selection and count`.

Consumes: `WrapText`, `MiddleElidedText`, Task 2's badges, Task 4's
labels.
Produces: the custom containers rows; the whole-row click rule.

## Task 7: the workload detail grows tabs that own their bounds

Spec: `presentation.md` 4.5 (both endpoints, the bounds and counts, the
rollout columns), 9.4 (prose tables as custom rows); roadmap decisions 4
and 5; this plan's decisions 4 and 5. Implementer: Opus (structural).

Today the detail is one scroll of unbounded sections: `PodsCard` renders
every pod the daemon ever saw, `JobsCard` renders a whole namespace of
jobs filtered in Swift by name (`WorkloadsStore.swift:128-134`),
`RolloutsCard` renders every rollout row with a `FIRST_SEEN` column that
holds the daemon's connect time, and there is no incidents list at all on
the screen that names the workload.

- `WorkloadsStore`: `GetWorkload` passes `pods_live` and
  `pods_limit: 50`; `ListJobs` passes `cronjob_name`, `limit: 50` and
  the `live`/`failed` flags, and reads `total`/`failedTotal`; the Swift
  name filter goes. A `loadIncidents(key:)` calls `ListIncidents` with
  `workload_kind`, `workload_name`, `namespace` and the cluster, owned
  here because the screen owns the call, not the incidents store.
- `WorkloadDetailView` becomes the tab shape of decision 4: Overview
  (the four summary cards and `RestartsCard`), Pods, Rollouts or Runs,
  Incidents; CronJob order and default per the decision. The tab titles
  count what they hold ("Pods 12", "Runs 22, 3 failed", "Rollouts 8",
  "Incidents 4, 1 open").
- Pods tab: the chips of decision 5 (Live, Not running, Open incident,
  in that order, single-select beside All); the truncation line from
  `podsTruncated` and the row counts ("showing the newest 50 of 61");
  the deletion column heading loses its column-name spelling; ordering
  stays as the daemon returns it (Phase 13 owns ordering).
- Rollouts tab: one row per ReplicaSet as today; the date column becomes
  the revision's own `createdAt` under a heading that says so
  ("SHIPPED"), with `rollout_history.created_at` on hover; a READY
  column says `ready/replicas` when the counts are present and nothing
  when they are absent; five newest first with the rest behind a
  disclosure that says how many are folded ("6 older revisions") --
  `rolloutSelect` orders `revision DESC`, so newest-first is the served
  order. The correlation footer stays.
- Runs tab: headed by `total` and `failedTotal` ("22 runs, 3 failed");
  chips Live and Failed mapping to the request flags; the condition
  badges keep Task 2's vocabulary; the counters-are-context footer
  stays.
- Incidents tab: the workload's incidents as `IncidentRowView` rows
  opening the detail, headed by what it lists (decision 9's shape).
- Reader's words on this screen: the four card metas are hover now; the
  visible `jobs WHERE cronjob_name = ...` goes with them.

Screenshot review: `hack/macos/screenshot.sh
workload/orbstack/idios-smoke/Deployment/smoke-crash <png>` (Overview
default, Pods and Rollouts tabs, SHIPPED holding a real creation time
distinct per revision once the deployment has rolled) and
`workload/orbstack/idios-smoke/CronJob/<fail cronjob> <png>` (Runs
selected on open, the counts line, a failed run red and a complete run
green-if-complete per the existing vocabulary). The reviewer flips the
Pods chips by hand.

Checkpoint, commit `macos: tab the workload detail and bound its lists`.

Consumes: Task 1's fields, Task 2's badges, Task 4's labels,
`IncidentRowView`.
Produces: the tabbed detail, `WorkloadsStore.loadIncidents`, the chips.

## Task 8: the status screen answers its three questions first

Spec: `presentation.md` 4.6 (the two closer numbers told apart, the two
new intervals); roadmap deliverable "a Status screen that answers 'am I
connected, am I missing anything, how old is this' before it shows a
sweep table". Implementer: Sonnet.

Today the header is one monospaced line with an absolute clock time, the
closer card shows only the last tick, the two new intervals are missing
from the settings card, and connectedness is only implicit (the screen
either renders or is replaced whole by `NotConnectedView`).

- Header block, three answers in order: connected -- the daemon address
  from preferences beside `pid`/`version` (or the mock line as today);
  missing anything -- one sentence built from the cluster rows: "2
  clusters watched, all ready" or "1 of 2 clusters not ready" with the
  red vocabulary when errors exist, so the answer precedes the table
  that details it; how old -- "snapshot 4 s ago, written every 10 s"
  (relative, raw timestamp on hover).
- The Clusters card stays first below the header; the closer card says
  both numbers each: "closed 5 this tick, 12 since start; late-attached
  3, 7; opened 1, 9" (shape at the implementer's eye, both told apart
  per 4.6).
- The settings card gains "scheduling grace" and "stuck after" beside
  the other intervals; its labels become words with the config key on
  hover (`retention_days` -> "retention", and the rest alike); the
  sweep card's `ran_at` line moved into the body in Task 4 stays.
- Capture `dropped` is already on the queue card and is part of answer
  two; no new counter is invented.

Screenshot review: `hack/macos/screenshot.sh status <png>` -- the
reviewer reads the three answers top to bottom before any table, sees
both closer numbers, and finds the two new intervals.

Checkpoint, commit `macos: answer connected, missing and age first`.

Consumes: Task 1's status fields, Task 3's dot rule, Task 4's labels.
Produces: the header block; the closer totals on screen.

## Task 9: docs and roadmap

- `docs/design/presentation.md`: 9.3's workload row gains the tab shape
  and bounds; the screens table's incident detail row gains the Job card
  and the overflow; a sentence in 7.4/7.6 territory records decision 3's
  derived labels and the meta-as-hover rule; 9.5 records the exit-0
  neutral and the one cluster rule as part of the vocabulary.
- `docs/mockups/idios-ui.html` was amended per screen task; this task
  only verifies nothing contradicts the doc.
- `CLAUDE.md`: no new durable facts expected; the badge-vocabulary
  sentence already says BadgeStyle is the only colour source. Add one
  only if a task uncovered a lasting gotcha (the custom containers rows
  may earn one).
- `docs/plans/m3-signal/roadmap.md`: Phase 12 status complete, with the
  one-line deviations if any task was cut down.
- This plan gains its "Hands to the next phase" section: the sidebar
  rows Phase 13 rebuilds (the dot helper and folder labels it must
  keep), the workload tabs its tree links into, the Browser screen left
  word-for-word for deletion, and whatever contradicted the roadmap.

Checkpoint (`make ascii`), commit `docs: close phase 12`.

## Self-review

Spec coverage. Every roadmap Phase 12 deliverable has one task: badge
semantics and the ready column -> T2; the headline message -> T5; the Job
card -> T5; the settling tag -> T5; the newest previous log -> T5; the
tabbed workload detail with its bounds, the CronJob's leading Runs, the
pods filters and the SHIPPED column -> T7; the reader's words -> T4
(mechanism and shared screens) plus T5, T6, T7, T8 (their screens), with
Browser excluded by decision 11; the closed incident -> T5; the rail
treatments -> T6; the selection paint -> T6; the whole-row click -> T6;
the tab count -> T6; the one cluster dot rule -> T3; the Status screen's
three questions -> T8. The handoff's model gap list is emptied by T1,
`evicted` included; `UnresolveIncident` gains its UI in T5; the stuck
folder placement is decision 10; the "2 incidents, 0 open" wording is
decision 9 in T6.

`.ai` rules. ASCII at every checkpoint. Tests: the only new tests are
T1's decode assertions, each tracing to a `presentation.md` section and
asserting whole values against the same bytes the daemon serves; no view
test is invented, because the screens are a person's check by the spec's
own testing order. Comments: the reasons named in the tasks (why an
unknown exit stays red, why the chip order stays oldest-first, why the
labels are derived, why Browser is skipped) are the only ones. Scope: the
daemon is untouched outside one test file's fixture builder; no contract
change; the new files are the Job card view, the two smoke CronJobs and
the fixture JSON; ordering is not changed anywhere, five-newest rollouts
being the served order consumed. Commits: one per task, `area: imperative
subject`. Code is truth: no view or comment references this plan; the
mockup is amended, not cited.

Type consistency. `Rollout.createdAt` is a required `Timestamp` because
the column is `NOT NULL`; the three counts are `Int32?` because the wire
is `optional int32`. `IncidentDetail.job` reuses the `Job` model that
`Page<Job>` already builds, so `jobRow`-shaped mapping exists once in
Swift as it does once in Go. `Page<Job>.total` takes zero when absent
because proto3 drops zero scalars, matching every other absent count in
the model layer.

Risks named. The containers-table rework (T6) is the version-specific
piece: `Table` selection cannot be restyled on macOS 15, so the task moves
to custom rows, which the repo has done twice and which loses
`TableColumnCustomization` for that one table -- accepted, the table has
five narrow columns. The fixture rewrite (T1) ripples into
`cmd/idios/status_test.go`'s golden line, which is loud, not silent. The
job-incident screenshots need the new smoke CronJobs and two schedule
ticks; the task says so instead of hoping. Screenshots need Screen
Recording permission for the terminal, as m2 established. If T7 runs
long, its Incidents tab is the one part that can be dropped without
leaving a dangling bound (the other three tabs own theirs), and the
roadmap would get that sentence.

## Hands to the next phase

What Phase 13's sidebar rebuild must keep: `clusterDot(ready:hasError:)`
in `BadgeStyle.swift` is the one cluster-dot rule (error red whatever
`ready` says, ready green, else grey) and sidebar, menu bar, workloads
tree and Status all call it; the category folder titles draw
`Category.label` with the raw value on hover; `last_error` is the sidebar
row's hover, and the Status screen is where the full text lives.

The workload detail Phase 13's tree links into: `WorkloadTab` in
`WorkloadDetailView.swift`, `defaultTab` opening a CronJob on Runs and
everything else on Overview; the Rollouts tab is dropped when the daemon
returned no rollout row and the kind is not Deployment. `WorkloadsStore`
owns `loadDetail(_:podsLive:)`, `loadRuns(key:live:failed:)` and
`loadIncidents(key:)`; a controller-less row is scoped by `pod_uid` in
both its pods and its incidents.

The screenshot route `workload/<cluster>/...` spells the cluster by name;
`adoptSelection` in `WorkloadsScreen.swift` resolves it against the
clusters list after the tree loads, and a unique namespace/kind/name
match stands in while that list is still empty. Phase 13's navigation
work coordinates on that file.

`defaultArtifact(_:)` in `IncidentDetailScreen.swift` is the one
default-log rule (the newest previous log, else `current.log`); the
incident and pod screens share it. The Browser screen is left word for
word for Phase 13's deletion: its own `readyColor` and two accent
colours are the only colour decisions outside `BadgeStyle.swift`.

The smoke set gained `cronjob-fail.yaml` and `cronjob-ok.yaml` (a run
every minute each); job screens need two schedule ticks of data before a
screenshot.

Loose ends found, none owned by a Phase 12 task:

- A job-subject incident's title sentence still reads
  "Pod <id> is <reason>"; the header's title builder predates the Job
  card and never learned the subject kind.
- The pod rail keeps its column-name labels (`created_at`, ...) where the
  incident rail moved to words; each matches its mockup today.
- The workload Overview stat cards carry no provenance hover; nothing on
  them names a column.
- The Status gap histogram and the workload pods tab's worst-state cell
  still draw `rawValue`.
- `IncidentGrouping`'s category header shows the label without a
  raw-value hover.
- `SiblingsCard` colours on `ready` alone; it never draws red, so
  decision 2 holds, but a Succeeded sibling is orange rather than
  neutral.
- The workload Incidents tab sends no `limit`, so the daemon's 500-row
  default is its only bound, and past it the heading would miscount.
- `idios mock` does not serve `incident_detail_job.json`; the fixture
  exists only as bytes for the decoding tests.
