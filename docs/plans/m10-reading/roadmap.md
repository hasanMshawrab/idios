# m10 - Reading

Status: complete 2026-09-05, started 2026-09-03. Closing report: `closing.md`. Step plans are written when
each step starts (`reference.md`, `list.md`, `search.md`, `explore.md`,
`glance.md`). Evidence: `review.md` in this directory, the condensed UI
review this milestone answers.

Goal: the application makes incidents and workloads easy to read,
search, look around and act on. The daemon records a complete and honest
story of every failure; the application shows that story almost raw, and
a UI review over real smoke data traced most of what is hard about it to
three causes. Rows are incidents, not problems: a CronJob that fails
every two minutes is one problem and appears as 56 identical job-level
rows plus a separate pod rollup, because the rollup key requires a pod
uid and a job-subject incident has none. Colour and words each carry
several meanings: red is a category, an open state, a namespace dot, a
destructive button and an owner-chain dot; green marks a failed run
"job finished"; eight states and a dozen internal terms have no
definition anywhere in the UI. Nothing takes a person to a thing: the
two filter fields are client-side substring matches, identity is printed
five times a page and none of it is a link, and the keyboard layer is
invisible. This milestone fixes the causes, not the symptoms, screen by
screen: the list groups problems, one colour rule and one vocabulary
hold everywhere, a palette jumps to anything by name, and the workloads
tree, the pod page and the menu bar lose what they repeat. The pod page
is structurally right and is changed by subtraction only.

## Decisions (made, do not relitigate)

1. Problems, not incidents. The list groups by workload by default and
   every group header has one shape: the section header with a chevron,
   the same inset rule as its rows and a faint tint, carrying kind and
   name, a plain-language summary line ("every 2m, 62 of 63 runs failed,
   since 07:32" for a CronJob; "3 of 3 pods looping, tag 1.36" for a
   Deployment), the worst open category and one state badge with the
   open count. A cluster or namespace header appears only while more
   than one is in scope and is drawn as the same header shape; the plain
   non-folding cluster row is gone. Inside a CronJob's or Job's group the
   unit is the run: the `job_failed` row and the pod rows of one Job
   (`job_uid`) fold into one run row naming the run's suffix, its pod
   count, the categories found among them, the exit code, the reason and
   the open count; the newest five runs are shown and a "+ N more runs"
   line opens Workloads on that CronJob's Runs. The replica rollup of m8
   keeps its key (`container_name` and `category` across two or more pods
   of one workload) for the other kinds; the sentence "a bare pod and a
   job-subject incident never roll up" becomes "a bare pod never rolls
   up; a job-subject incident folds into its run". Expansion state of
   groups, runs and rollups persists per group id across the window's
   life and across a grouping change. Every count is still a count of
   incidents.
2. One colour rule. State owns hue: red is open and unacknowledged, amber
   is acknowledged (and a degraded container state), grey is closed by
   any reason including `job_finished`, green is live and healthy only
   (running and ready, a synced cluster, a Complete run) and never a
   closed failure, the accent is selection only. Categories are nouns:
   a neutral outlined chip with a small glyph, no hue. Kinds in the
   workloads tree have no colour. The rule applies in one pass to list
   badges, rail dots, tree dots, timeline dots, the run strip and the pod
   header tag; `BadgeStyle.swift` stays the one place colours live and
   the mockup's C1 page is redrawn to it first. Recorded fallback if the
   list reads too flat after step B: one muted hue per category family
   (resource, image and config, scheduling, disruption, job) and nothing
   else red; not to be tried before B ships.
3. One vocabulary. `manual` is labelled Resolved everywhere (the wire
   value does not change). Every state and category name carries a
   one-sentence tooltip whose text is fixed in the presentation doc, and
   a "?" beside the sidebar's View heading opens a legend popover: the
   lifecycle (open, acknowledged, closed by recovered / pod deleted / job
   finished / resolved; dismissed cutting across), the definition of
   Attention with its window read from `Status.attention_window_seconds`
   rather than hard-coded, and the keys. The column that shows
   `occurrences` for a pod incident is titled Restarts and shows "-" for
   a job-subject row. The container column reads "job" for a job-subject
   row and the `job-level` chip is gone. Card footers that explain method
   move to the card title's hover, as Section 7.4 already says.
   Attention keeps its 24-hour rule: with problems grouped, a recently
   closed problem is one grey group, not 56 rows. Empty views teach: the
   state's definition, the key that produces it, and a button back to
   Attention.
4. Row anatomy. One line by default with real columns: state dot,
   category chip, subject (kind and name, a pod's suffix after the
   workload prefix), container, reason with exit code, restarts, age,
   state badge. The last three have fixed widths and the age never
   wraps: the word "open" or "closed" lives in the state badge. The
   second line of today (namespace, pod, tag, deletion) is the row's
   hover and a Comfortable density in the View menu; the elision and
   copy rules of Section 7.4 hold. Closed rows dim.
5. The sidebar is a macOS source list, not pills: Screens, then View
   (Attention, Open, Acknowledged, then a Closed disclosure holding
   Recovered, Pod deleted, Job finished, Resolved, Dismissed), then
   Category. Every row keeps its count, zero included; categories with
   no incident fold into one dimmed "N more with none" row. Active rows
   take the selection accent, never their state colour. The View and
   Category sections are shown only on the Incidents screen, since they
   filter nothing else; Screens stays on every screen, including under a
   pushed pod page, replacing m9's whole-sidebar collapse.
6. Search jumps; filter narrows. Cmd-K opens a palette (a menu key
   equivalent, discoverable in the Go menu) over the current screen; "/"
   focuses the list's filter field, which keeps narrowing served rows.
   The palette matches incident id (`#187`), pod name including its
   suffix, Job name, workload, namespace, container, reason, image tag
   and node, sectioned as Pods, Runs, Workloads, Incidents and Commands;
   field prefixes `ns:`, `node:`, `tag:`, `reason:` narrow; Return opens,
   Cmd-Return opens in Workloads, Shift-Return copies the uid; an empty
   query lists recent pages and every command with its key, which is how
   the keyboard layer becomes visible. Matching is client-side over the
   rows the stores already hold (`ListIncidents` up to the list limit,
   `ListWorkloads`, `ListPods` live, `ListJobs` for the workload in
   view) plus the detail endpoints for an id. One daemon change makes it
   exact at any size: an exact `pod_name` filter on `GET /incidents` and
   `GET /pods`, served by the existing `pods (cluster_id, namespace,
   name)` index. No grouped count endpoint: the tree's numbers already
   come from `/workloads`.
7. Workloads. Captions are plural with counts ("DEPLOYMENTS 1",
   "CRONJOBS 2", "BARE PODS 5"); the pane header names the trailing
   number ("open incidents") and holds Collapse all and Expand all; a
   filter no longer clears collapse state. Pods no controller owns are
   one row per name carrying "N pods" when a deleted pod of the same
   name is kept; the row opens the live pod or a chooser. Each row has a
   context menu: Show incidents, Show pods, Copy name, Acknowledge all
   open. The CronJob detail opens on Runs with a run strip above the
   table: one cell per run in the window, oldest first, coloured by
   outcome, dashed where a sweep removed the record, hover for
   condition, duration and exit code, click opening the run's pod, drag
   acknowledging the incidents under the selection; a strip up to 200
   runs, an hour-by-minute matrix beyond. The Runs table defaults to
   Failed when `failed_total > 0`, names runs by the suffix after
   `<cronjob>-`, links each run to its pod and its incident, and folds
   consecutive runs with the same reason. The Pods tab defaults to Live.
   The Overview gains the mockup's 3d / 24h / 6h window and the restarts
   chart a y-axis, hover values and true hourly bars, gaps dashed.
8. The pod page changes by subtraction. The middle pane leads with a
   verdict block built from fields the page already receives: what
   happened, how many times since when and last when, the memory limit
   (`mem_limit_bytes`), what was and was not captured (`capture_gap`),
   and whether the image digest changed since open (`image_id` at open
   against the container's). Acknowledge is the one primary button with
   its key shown, Note beside it, the rest in an Actions menu; the
   segmented control appears only when a container has two or more
   incidents. Captured logs are a scrubber over one file body (restart N
   of M, previous and next), replacing the chip grid; "Logs N" counts
   files that exist. Method text moves to card-title hovers; implementer
   notes ("phase is not used for category") go. The identity line is a
   breadcrumb whose segments open Workloads at that node; Back gains
   Forward (Cmd-]). The rail keeps the owner chain, Node, QoS, Image id
   and Container id, the times and the siblings; CONTEXT's repeats of
   cluster, namespace and image go. Repeating cycles in the Timeline
   fold into one entry ("x31, every ~4 min, first 07:32, last 09:52")
   with a disclosure; lifecycle, rollout and cut entries never fold; one
   time per entry, the exact pair on hover. The header tag is debounced
   and reads "RUNNING, LOOPING" when readiness flipped more than three
   times in the window. Plurals agree. The events table never wraps a
   timestamp: the time column widens and the message wraps.
9. The glance. The menu bar extra's headline is the open count, its
   subline the attention count, and the Dock badge follows the headline;
   Section 4.7's reasoning survives as the subline. Its rows are one per
   workload group (the same fold as decision 1), a closed group grey, and
   a row opens that group in the list. Status sets its header in the
   system font, shows configuration in human units (24 h, 10 min) and is
   linked from the legend popover. The fields the model decodes and no
   view draws are drawn where the mockups place them: the incident's
   `node_name`, `related_incidents` (decoded for the first time),
   `running_since`, the resource byte and milli values, `Event.first_ts`
   as a span, and the Job's `succeeded`, `completions` and `parallelism`.
10. Nothing in fixtures, tests, docs, comments, commits or this plan
   names a real organisation, cluster, namespace, workload, image or
   node. The screenshots in `review.md` describe the smoke fixtures.

11. The run is a thing you can open. One screen for one Job, keyed by the
   Job's uid (`run/<job uid>`), titled `Run <suffix> of <CronJob name>`
   when a CronJob created the Job and `Job <name>` when nothing did: the
   word run means a Job a CronJob created, and a standalone Job is never
   called one. It leads with the Job's condition as a state tag, a counts
   line of attempts, incidents and how long the run ran, the breadcrumb
   cluster / namespace / CronJob, and a verdict block built from the
   fields the page holds: how it ended and after how many attempts, the
   backoff limit against the failed counter, the exit codes, what the
   attempts captured, the image tag, and how many earlier runs failed the
   same way. Acknowledge acts on every incident of the run, with Note,
   Ask AI and an Actions menu beside it as the pod page has. Overview
   holds an Attempts card of one line per pod in order - number, suffix,
   category, exit and reason, time, incident and state, and a link into
   that pod's logs - then the Job card, then the run strip with this run
   marked; Timeline, Logs and Events are the other tabs. The rail is the
   owner chain, the context, the times and the other runs of the CronJob.
   Every run row, run strip cell, Runs table row, palette Runs hit, pod
   page breadcrumb Job segment and rail Job dot opens it, and so does
   every job-subject incident, with or without a pod kept; the pod-less
   job page is replaced by it, a pod row inside a run still opens the pod
   page, and a job-subject incident no longer joins a container's
   segments, so the pod page's counts line and segments are the pod's
   own. The decisions about the title, the tag, the counts, the verdict
   and the attempt rows are pure functions in `IdiosModel` with table
   tests. One daemon change makes the page reachable: a `job_uid` filter
   on `GET /jobs`, because a Job can be read by uid no other way and a
   completed run has no incident to carry it. The list draws its children
   as children in the same step: every row under a group header is
   indented, a neutral guide rail runs from the header to the last child
   and turns in, the children carry a faint tint that stops with them,
   and the next top-level row starts on a heavier rule. The legend gains
   a Kinds section - Pod, Container, Workload, Deployment, Job, CronJob,
   Run, Incident, and the three owner chains - whose texts are fixed in
   `docs/design/presentation.md` 9.6 as a table beside the state and
   category tooltips.

12. The explanation. Every screen answers "what am I looking at" without
   leaving it. A small round [?] sits at the bottom right of each screen
   - at the popover's foot in the menu bar extra and beside a sheet's
   Cancel, where there is no bottom right - and opens a coach-mark
   overlay over the screen a person is already on: the screen stays put,
   a scrim dims it, every explained region shows through at full
   brightness under an accent outline with a numbered pill at its
   top-left corner, and hovering or clicking a region shows a note beside
   it carrying the region's name, a monospace tag naming the stored
   field, table or endpoint it draws (`restart_count`, `k8s_events`,
   `GET /incidents/{id}`) and one or two sentences on what it means and
   why to look there. A second layer draws the reading order as larger
   numbered pills over the regions on the path, with a footer line naming
   each and a "Reading flow only" toggle that dims every other outline.
   Cmd-/ opens it, "?" opens it when no text field has focus, Esc or a
   click on the scrim closes it. No colour is added: the outline is the
   accent, which 9.5 allows because the overlay is a selection of
   regions, and the scrim, the pills and the notes are system materials
   and the primary text colour. The texts are data, not view code: an
   `ExplainedRegion` (id, title, tags, sentence) and per-screen tables in
   `IdiosModel`, fixed in `docs/design/presentation.md` 9.7 with one
   table per screen, agreeing word for word with 9.6 wherever a region's
   title is a state, a category or a kind. A `.explained(...)` modifier
   publishes a view's bounds through an `anchorPreference` and one
   overlay at each screen's root reads them, so a region and its
   explanation are declared in one place; a DEBUG assertion and a line on
   stderr fail the screenshot run when a screen's regions and its
   explanations differ, which is what keeps the two from drifting, and
   `hack/macos/screenshot.sh <route> <png> --explain` photographs any
   screen's overlay. Every screen is covered - the incidents list and its
   parts, the palette, Workloads, the pod page, the Run page, Status, the
   menu bar popover and the four sheets - and a screen whose content
   depends on what is selected declares one set per state, the registry
   reading the state in view.

## Steps

### A. Reference and spec (`reference.md`)

The mockup is redrawn before the code moves, in place: page 1 becomes
the list of decisions 1, 4 and 5 with the palette of decision 6 as a
second frame; page 5 becomes the tree and the CronJob detail of decision
7; page 7 becomes the menu bar of decision 9; the C1 badge vocabulary
becomes the colour rule of decision 2; page 2 gains the verdict block,
the primary action and the scrubber of decision 8 without changing its
structure. The page notes state the decisions in the mockup's voice,
present tense. `docs/design/presentation.md` changes in the same step:
the Incidents row of 9.3 (grouping, the run fold, the row anatomy, the
sidebar, the palette), the Workloads row, the Pod page row's subtractions,
the Menu bar row, 4.7's count, 9.4's keyboard paragraph (Cmd-K, Cmd-],
"/", the View menu) and 9.5's colour rule, plus the tooltip texts of
decision 3 as a table. The `pod_name` filter of decision 6 is stated in
4.2 and 4.3. Reviewed before B starts.

### B. List, sidebar, colour (`list.md`)

Decisions 1 to 5 in the application: the run fold and the header shape
in `IdiosModel` as pure functions with table tests, the source-list
sidebar, the legend popover reading the attention window from the status
store, the row anatomy, the View menu density, `BadgeStyle` remapped,
the label Resolved, the tooltips, the teaching empty states. No proto
change.

### C. Search (`search.md`)

Decision 6: the `pod_name` filter in the daemon (query, proto, `make
generate`, the mock fixtures), then the palette and its store in the
application, the Go menu items, the command list.

### D. Workloads and pod page (`explore.md`)

Decisions 7 and 8. The run strip and the bare-pod merge as pure
functions in `IdiosModel`; the tree, the detail defaults and links, the
context menu; the verdict block, the scrubber, the breadcrumb, Forward,
the timeline fold, the debounced tag. No proto change.

### E. The glance (`glance.md`)

Decision 9: the menu bar extra, the Dock badge, Status, and the decoded
fields drawn. `related_incidents` decoded. `README.md` screenshots
retaken with `hack/macos/screenshot.sh` against `make smoke PORT=7771`
after the installed daemon holds 7770.

### F. The run (`run.md`)

Decision 11. The events table's columns first, so a narrow pane stops
wrapping one character per line and step E's `pod.png` is retaken over a
table that reads. Then the spec: 9.3 gains a Run page row and loses the
pod-less Job page, and 9.6 gains the kinds table and the owner chains.
Then the one daemon change, `job_uid` on `GET /jobs` (proto, query,
handler, `make generate`). Then the run route, `incidentRoute` and the
page's title, tag, counts, attempts and verdict as pure functions in
`IdiosModel`; then the Run page itself over its own store; then every
route into it, with `JobOnlyPage` deleted and the pod page's job-subject
segments gone. The list's nesting and the legend's Kinds section close
the step.

### G. The explanation (`explain.md`)

Decision 12. The spec first: 9.3's rows gain the [?] and the overlay,
9.4 gains the key, and a new 9.7 fixes one explanation table per screen.
Then the model - `ExplainedRegion`, `ExplainedScreen`,
`explainedRegions(for:)` and `readingOrder(for:)` with a table test that
every screen has a
non-empty set, every id is unique and every reading-order id names an
explanation. Then the registry and the overlay, with the pod page as the
first screen it draws, because its regions are the ones already written.
Then the rest of the screens in four tasks by area: the incidents list
and the palette; Workloads; the Run page and Status; the menu bar
popover and the four sheets. Then the `-explain` launch argument, the
screenshot flag and the DEBUG coverage assertion that fails a screenshot
run when a screen's regions and its explanations disagree. Verification
with the user walks every route's overlay.

## Order and status

A, then B, then C and D in either order, then E, then F, then G.
`make generate-check` after C's commit and after F's proto commit;
`rm -rf macos/.build` before `make app-test` after either proto change;
`make app-test && make app` for every application step.
Screenshots for review run against a private daemon on 7771 with
`IDIOS_DAEMON=127.0.0.1:7771`, never against the installed daemon on
7770. Nothing is committed without the user's review of the diff.

- Step A: complete 2026-09-03
- Step B: complete 2026-09-03
- Step C: complete 2026-09-03
- Step D: complete 2026-09-04
- Step E: complete 2026-09-04
- Step F: complete 2026-09-04
- Step G: complete 2026-09-05
