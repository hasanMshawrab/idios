# m9 - One page per pod

Status: complete 2026-09-03. Steps A to C shipped (`siblings.md`,
`page.md`). Depends on m8 step C (the fold order the left column reuses,
and the `IdiosModel` move it starts) and step B (the `unclean_exit`
category the badges show).

Goal: a pod has one page in the application, reached from anywhere, and
it is where a person reads what is going on inside that pod. Today the
incident detail and the pod screen are two screens over the same rows:
`GetIncident` already returns the pod, its containers, its latest
conditions, its siblings, every captured file and the pod's whole event
stream, so two incidents of one pod render the same page under a
different header, and the pod screen renders most of it a third time
under its own tabs. This milestone replaces both, and the separate
timeline screen, with one master-detail page: the pod and its containers
on the left, whatever is selected in the middle, the rail on the right.
Pod data appears once, container data once, and incident data behind a
control whose whole purpose is switching. The design was drawn first,
six frames on a canvas, and step A puts them into the repository's
mockup in place of the pages they retire, so the page and its reference
live together. The daemon changes are the sibling read model of decision
6 and the two MCP tool descriptions it makes true.

## Decisions (made, do not relitigate)

1. One screen, keyed by pod uid, replaces `IncidentDetailScreen`,
   `PodScreen` and the timeline screen. Its state is `(podUID,
   selection)`, where the selection is the Pod card or one container,
   plus for a container the id of the incident whose segment is lit and
   the pane's tab. Routes: `incident/<id>` fetches the incident, then
   opens the page on its pod with that container selected and that
   incident lit on Overview; `timeline/<id>` does the same with the
   Timeline tab selected; `pod/<uid>` opens the Pod card on Events;
   `pod/<uid>/<tab>` keeps every `PodTab` raw value as a route and maps it
   onto the Pod card: `containers`, `incidents` and `events` open Events
   (the left column is the container and incident list), `conditions`
   opens Conditions, `logs` opens Files, `podjson` opens pod.json. The
   back chevron returns to wherever the page was opened from.
   `hack/macos/screenshot.sh` keeps every route name and gains none.
2. The pod-and-containers column is the leading pane of the page itself,
   inside the pushed screen, at the sidebar's width so the rhythm of the
   window does not change. Pushing a pod page collapses the application
   sidebar through the split view's own column visibility and Back
   restores it; a sidebar the user had already collapsed stays collapsed
   and the pane is drawn regardless, because it belongs to the page and
   not to the sidebar's slot. State and category filters are about the
   list, which is why the sidebar steps aside here as it does nowhere
   else; Cmd-1/2/3 still switch screens and bring it back. The column is
   the Pod as a card, then the containers as cards indented under it with
   a connector, each carrying its state line and one badge per open
   incident category with its occurrences. The selected card fills with
   the accent.
3. Which call owns which pane. `GetPod` is authoritative for the page:
   the pod row, the containers, the latest conditions, the incident list
   (open and closed) that puts the badges on the cards, the artifacts
   grouped by container, and the siblings of decision 6. Selecting a
   container shows, above its tabs, the middle pane's own header (the lit
   incident's category and state tags, its sentence, its actions, its
   note); then Overview (kubelet facts from `GetPod`'s container row, the
   container's files, and its events), Timeline (the lit incident's
   window) and Logs. The container's events are the pod's events whose
   `field_path` names it plus every pod-level event (no `field_path`:
   `Scheduled`, `Killing` at pod scope, `Evicted`, `Preempted`), the
   latter marked as the pod's, because the event that explains a death
   is usually the pod's and a pane that shows the death must show its
   cause. When the container has incidents, a segmented control above the
   pane header has one segment per incident in the fold order (open
   before closed, worst category first, ties to the newest); the lit
   segment's `GetIncident` supplies the header, the note and the Timeline
   tab, and switching segments changes those and nothing else: the
   kubelet facts, the files and the events are the container's. The store
   keeps the previous `GetIncident` response until the next one arrives,
   so a switch never blanks the header. Selecting the Pod card shows the
   pod's tabs: Events (every container, a container column, time order,
   from `PodEvents`), Conditions (latest per type and the history from
   `PodHistory`), Files (every container's files together and
   `pod.json`), pod.json, and Related incidents only when the pod belongs
   to a Job (the `job_failed` row and the retries that ran in other pods,
   the one set of related rows the page cannot show anywhere else,
   fetched as today through `ListIncidents` by `job_uid`). Calls: opening
   `incident/<id>` is `GetIncident`, then `GetPod` and `PodEvents`;
   opening `pod/<uid>` is `GetPod` and `PodEvents`; a segment switch is
   one `GetIncident`; a live update is one `GetPod`.
4. The page header is the pod's and never moves: state tag (`RUNNING,
   READY`, `RUNNING, NOT READY`, `DELETED`, ...), the counts line
   (containers, incidents open, node), the title `Pod <name>`, and the
   identity line: cluster / namespace / the owner chain as it exists
   (`Deployment` + `ReplicaSet`, or `StatefulSet`, or `CronJob` + `Job`,
   or nothing for a bare pod), stopping before the pod because the title
   is the pod. The incident's own sentence and actions live in the middle
   pane's header (decision 3), never up here. A deleted pod shows the grey
   banner under the page header (`deleted_at`, source, inferred reason,
   termination requested, kept until) and dims the live-only fields; a
   closed incident's actions fold into one menu as today.
5. The rail is the pod's rail, top to bottom: owner chain in the dotted
   form with the container as a fourth dot when one is selected; context;
   times of what is selected (the lit incident's opened, last seen, open
   for, reopened, will close; or the pod's created, started, first and
   last seen, deletion requested, deleted); siblings.
6. Siblings are capped, ordered, and carry their category. `SiblingPod`
   (`incidents.proto`) gains the sibling's worst open category;
   `PodDetail` (`pods.proto`, which already imports `incidents.proto`)
   gains `repeated SiblingPod` and a total; `IncidentDetail.siblings` is
   deleted. The read model orders pods with an open incident first (worst
   category first), then live pods, then deleted ones, newest created
   first inside each group, returns five, and counts all pods of the same
   `controller_uid`. The rail shows the five with a category badge where
   there is one, a state where there is none, and a "+ N more of this
   ReplicaSet" line that opens Workloads > that workload > Pods, whose
   list is the Deployment's pods across ReplicaSets and may be longer;
   the label says which set it counts.
7. Nothing is repeated, and the tool surface says what is true.
   `RelatedIncidentsCard` (pod-or-job scope) is deleted: this pod's
   incidents are the left column, this ReplicaSet's pods are Siblings,
   the same problem across pods is the list's rollup; only the Job case
   survives as the tab of decision 3. The inline Pod tab of the old
   detail is gone with the screen. The incident stream is one
   subscription for the life of the page, filtered on this pod's
   incidents (`Incident` carries `podUID`); the lit segment is a local
   comparison and never a subscription boundary, because the stream has
   no replay and a reconnect gap loses events. An update to any incident
   of the pod re-reads `GetPod` so the badges stay true. The MCP
   `get_incident` description stops promising "the sibling pods" and the
   `get_pod` description gains them with their total; the prompt in
   `internal/prompt` changes if any line sends the agent to
   `get_incident` for siblings. The snapshot prompt reads
   `RelatedIncidents`, not `Siblings`, and is untouched.
8. The pod with no incident is the same page: no badges, the Pod card
   selected on arrival, no segmented control and no action buttons when a
   container is selected, no incident times in the rail, Files holding
   `pod.json` alone. A job-subject incident keeps today's borrowed shape:
   its page is the Job's last pod (`GetIncident` already returns it
   whole, so the page keys on that pod's uid), the `job_failed` row is
   the lit segment on the failing container, and Related incidents lists
   the retries.
9. `Route` and `PodTab` move into `IdiosModel` with the fold logic m8
   moved, so the route mapping is a pure function of a path string that
   the package tests can see; resolving `incident/<id>` to its pod is a
   store concern, not part of that function.
10. Nothing in fixtures, tests, docs, comments, commits or this plan
   names a real organisation, cluster, namespace, workload, image or
   node.

## Steps

### A. Mockup (`docs/mockups/idios-ui.html`)

Pages 2 (incident detail), 3 (incident timeline) and 4 (pod screen)
describe screens that will not exist after this milestone, so they are
replaced, not joined: page 2 becomes the pod page with a container
selected and an incident lit, and the same with the Pod card selected;
page 3 becomes the container pane on its Timeline tab; page 4 becomes
the pod with no incident reached from Workloads, and the deleted pod.
Page 1 already carries the list and the rollup from m8. The frames are
copied from the canvas the design was settled on; the page notes state
decisions 1 to 8 in the mockup's voice, present tense. Reviewed before B
starts.

### B. Daemon - siblings and tool surface (`siblings.md`, written when B starts)

Decision 6: the read model with its order, limit and total, the two
proto files, `make generate`, the wire fixtures regenerated so
`idios mock` serves a pod with more than five siblings. Decision 7's MCP
descriptions and prompt line. Spec statements land in
`docs/design/presentation.md` in the same step.

### C. Application - the page (`page.md`, written when C starts)

Decision 9's move first. Then the store of decisions 3 and 7 (one store
replacing `IncidentDetailStore`, `PodStore` and `TimelineStore`), the
screen of decisions 2, 4, 5 and 8, `Route` mapping every existing route
name onto it (decision 1), the three old screens and
`RelatedIncidentsCard` deleted, the screenshot script's routes verified
with a screenshot per frame of step A. Package tests in `IdiosModel`
cover the path-to-route mapping, the segment order and the sibling
order as pure functions. `docs/design/presentation.md`'s screen table is
rewritten for the one screen and the places that name the pod screen,
the timeline screen or the inline Pod tab change with it; `CLAUDE.md`'s
screenshot route list is amended.

## Order and status

A, then B, then C. `make generate-check` after B's commit; `rm -rf
macos/.build` before `make app-test` after the proto change; `make
app-test && make app` for C. Nothing is committed without the user's
review of the diff.

- Step A: complete 2026-09-02
- Step B: complete 2026-09-02
- Step C: complete 2026-09-03
- Follow-up 2026-09-03: decision 8's borrowed shape needs a pod to
  borrow, and a Job whose pods were pruned before idios first saw the
  cluster has none, so `incident/<id>` on such a row dead-ended. The
  page now has a pod-less shape for a Job's incident, and the list's
  chip says `job-level` for it. Stated in `docs/design/presentation.md`,
  Pod page row; no route, proto or daemon change.
