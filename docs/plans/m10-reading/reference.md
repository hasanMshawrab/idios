# m10 step A - reference and spec

Goal: after this plan the two references the later steps build against
say what m10 builds. `docs/mockups/idios-ui.html` pages 1, 5, 7 and C
are redrawn in place and page 2 is amended: page 1 is the list grouped
by problem with the source-list sidebar, the palette as a second frame
and the legend popover as a third; page 5 is the workloads tree with the
CronJob detail on its run strip, the Deployment overview with the window
switcher, the Pods tab on Live and the Rollouts tab; page 7 is the menu
bar extra counting open; page C's C1 is the colour rule with the state
hues, the neutral category chips and the container and capture
vocabularies under it, and C2's kinds lose their colour; page 2 gains
the verdict block, the one primary action, the scrubber, the breadcrumb
identity line, the Forward chevron and the trimmed rail without changing
its structure. `docs/design/presentation.md` states the same things in
the rows and sections the roadmap names, and gains the tooltip texts of
decision 3 as a table. Nothing under `macos/`, `internal/`, `cmd/` or
`api/` changes.

Architecture: the mockup is one HTML file with one stylesheet and one
`<section class="page">` per page; frames are `.opt` blocks holding a
`.win` window drawn with shared classes. The redrawn frames keep the
mockup's window chrome (`.win.light`, `.titlebar`, `.lights`) and take
their content classes from the review artifact's drawn windows, ported
under one wrapper class `.rd` so they cannot collide with the classes
pages 3, 4, 6, 8 and 9 still use (`.row` there is the page's flex row,
in the artifact it is a sidebar row). The badge vocabulary classes
(`.c-*`, `.s-*`, `.st-*`, `.gap`) are shared by every page and are
redefined once to the colour rule, so the pages that are not redrawn
follow it without being touched; the coloured dots those pages set
inline (`var(--r)`, `var(--b)`, `var(--o)`) stay as they are, because
those pages are not this step's deliverable and page 3's dots are
timeline dots that step D recolours in the application. The
presentation doc is edited in place, row by row; section 9.5 becomes
the colour rule and a new section 9.6 holds the vocabulary table, so
the tooltip texts have one home the application and the legend read
from.

Tech stack: HTML and CSS, hand-written, ASCII only (entities for every
glyph: `&#9662;` disclosure, `&middot;`, `&#8984;` Command, `&#8679;`
Shift, `&#8997;` Option, `&lsaquo;` `&rsaquo;` chevrons). Markdown for
the design doc. Checks: `hack/ascii-check` on both files, a browser
render of the mockup, and `grep` for the words the redraw retires.

Spec: `docs/plans/m10-reading/roadmap.md` decisions 1 to 10, which this
step turns into the reference; `docs/plans/m10-reading/review.md` for
the evidence each frame answers; the review artifact's drawn windows
(proposals 1 to 6) as the source of the redraw; `docs/design/
presentation.md` sections 4.2, 4.3, 4.7, 7.4 (the hover rule the pod
page's method text moves under), 9.3, 9.4 and 9.5 as they are before
this step.

Global constraints: `.ai/ascii-only.md`, `.ai/comments.md` (the
mockup's CSS comments say why a rule exists, never what it does),
`.ai/code-is-truth.md` rule 6 (the mockup notes and the doc state what
is, never what changed: no "previously", no "now", no "replaces"),
`.ai/scope.md` (pages 3, 4, 6, 8 and 9 are not redrawn; no Swift, Go or
proto change), `.ai/commits.md`. Roadmap decision 10: every name in a
frame is an invented one; the smoke fixtures' names (`smoke-crash`,
`smoke-cron-fail`, `smoke-oom`, `idios-smoke`, `orbstack`) and the
mockup's existing invented names (`checkout-api`, `shop`, `worker-2`)
are the vocabulary. Page notes are written in the mockup's voice,
present tense, and state the decisions, not the review. The decisions
are made; a frame that seems to want a different answer is drawn to the
decision and the doubt goes in the self-review. Every count drawn is a
count of incidents. Nothing is committed without the user's review of
the diff; this step ends with the user's review of the mockup before
step B starts.

## File structure

```
docs/mockups/idios-ui.html      the stylesheet gains the state tokens, the
                                redefined badge vocabulary and the .rd
                                classes; pages 1, 5, 7, C redrawn, page 2
                                amended; pages 3, 4, 6, 8, 9 untouched
docs/design/presentation.md     4.2, 4.3, 4.7, 9.3 (Incidents, Pod page,
                                Workloads, Menu bar rows), 9.4, 9.5, new 9.6
docs/plans/m10-reading/roadmap.md   step A's status line
```

No test file: this step produces no code. Each task's check is named
in the task and is the thing a reviewer runs.

## Task 1 - the stylesheet: tokens, vocabulary, `.rd` classes

Check before: `grep -c 's-manual\|c-oom{' docs/mockups/idios-ui.html`
finds the coloured category classes and the `manual` state; after: the
category classes share one neutral rule and `.s-manual` is labelled
Resolved wherever it is drawn.

1. Add the state tokens to `:root`: `--open` (red), `--ack` (amber),
   `--closed` (grey), `--live` (green) with their `-soft` backgrounds,
   in light and dark values, beside the existing `--r --o --y --g --b
   --p --grey`, which the pages not redrawn keep using.
2. Redefine the vocabulary once, with a comment saying why the
   categories carry no hue: every `.c-*` class becomes the same neutral
   outlined chip with a small square glyph (the glyph is `::before`),
   `.s-open` red, `.s-ack` amber, `.s-recovered`, `.s-pod_deleted`,
   `.s-job_finished`, `.s-manual` and a new `.s-dismissed` grey,
   `.st-running` green, `.st-waiting` and `.st-terminated` amber (a
   degraded container state), `.st-neutral` grey, `.gap` a dashed grey
   outline. Light and dark values for each.
3. Port the artifact's drawn-window classes under `.rd`: the toolbar
   search with its `kbd`, the source list (`.sh` with `.q`, `.srow`,
   `.srow.sub`, `.srow.dim`, `.cnt`), the summary line (`.sum`), the
   column header (`.hdrrow`), the group header (`.grp`) and the row
   (`.r`) on one nine-column grid, `.st` state badges, `.cat` chips,
   `.more`, `.foot`; the palette (`.scrim`, `.pal`); the popover
   (`.pop`); the workloads tree (`.tree`, `.cap`, `.tr`, `.det`, `.dh`,
   `.tabs`, `.strip`, `.cells`, `.legend`, `.filters`, `.runs`); the pod
   pane pieces (`.kicker`, `.vd`, `.acts`, `.scrub`); the menu bar
   popover (`.mbx`). Grid columns are named in one place and reused by
   `.hdrrow`, `.grp` and `.r` so the columns line up.

Consumes: the mockup's existing `.win`, `.titlebar`, `.lights`,
`.clusterbtn`, `.btn`, `.card`, `.th`, `.tr`, `.kv`, `.rail-h`, `.tl`,
`.badge`, `.tag`, `.chip`, `.log`, `.spark`, `.mb`, `.mb-bar`.
Produces: the tokens and classes tasks 2 to 6 draw with.

## Task 2 - page 1: the list, the palette, the legend

Check: the page renders three frames; `grep -c 'pill hot\|Marked
resolved' docs/mockups/idios-ui.html` is zero.

Frame 1a, "Attention, grouped by workload": the source-list sidebar
(Screens; View with the "?" and Attention, Open, Acknowledged, then a
Closed disclosure holding Recovered, Pod deleted, Job finished,
Resolved, Dismissed, every row with its count including zero; Category
with the five categories that have rows and one dimmed "7 more with
none"); the toolbar with the cluster button, the title, the Group menu
and the search field showing its Cmd-K; the summary line ("5 problems,
16 open incidents, 3 workloads and 3 bare pods, newest 09:59" with
Collapse all and Acknowledge all); the column header (state, category,
workload / pod, container, reason, restarts, age, state); the CronJob
group header with chevron, summary line, `job + app`, the reason, "-"
for restarts, the age and "9 open", then five run rows (`run 29807159 -
2 pods`, `job, app`, `exit 1`, restarts 0, age, state) with the two
oldest closed and grey, then "+ 58 more runs - open the run history in
Workloads"; the Deployment group header collapsed ("3 of 3 pods looping
- tag 1.36", restarts 122, "3 open"); three bare pod rows (oom, image
pull, config) with restarts and ages; one Job row (`Job
smoke-cronjob-retry - pod bmrcw`); the foot with the keys. Five
annotation markers and a callout list stating decisions 1, 4 and 5 in
the mockup's voice.

Frame 1b, "Cmd-K over the list": the scrim and the palette with the
query `29807154`, the sections Pods (two, one selected with `return`),
Runs, Incidents (`#187`, `#186`), Commands ("Show CronJob
smoke-cron-fail in Workloads" with `Cmd-2`), the footer with `return
open`, `Cmd-return open in Workloads`, `Shift-return copy uid`, "type #
for an incident id", and a line under the frame naming the prefixes
`ns:`, `node:`, `tag:`, `reason:` and the empty query's recent pages
and command list.

Frame 1c, "The legend and an empty view": the popover with the
lifecycle flow (open, acknowledged, closed by recovered / pod deleted /
job finished / resolved), the seven states with one line each and their
keys, the Attention paragraph with "24 h" marked as read from the
daemon's status; beside it the Acknowledged empty view ("No acknowledged
incidents." with the definition, the key, and a Show Attention button)
and one tooltip example on a `job finished` badge.

The page note states decisions 1, 3, 4, 5 and 6: grouping and the run
fold, the header shape, the row anatomy, the source list and where the
View and Category sections show, the palette and the filter, the
vocabulary. It keeps the sentences of today's note that still hold
(attention's definition, cluster as scope, the five group modes, the
replica rollup with its sentence changed to "a bare pod never rolls up;
a job-subject incident folds into its run", the keys) and drops the
pod-sibling fold paragraph only where the run fold replaces it.

Consumes: task 1. Produces: the visual reference for step B (`list.md`)
and step C's palette.

## Task 3 - page 2: the pod page by subtraction

Check: 2a renders with the verdict block above the actions; `grep -c
'probe or not running\|phase is not used' docs/mockups/idios-ui.html`
is zero.

In 2a: the titlebar gains a Forward chevron beside Back; the identity
line's segments (`orbstack`, `shop`, `Deployment checkout-api`,
`ReplicaSet checkout-api-7d9f8b6c4`) are drawn as links; the segmented
control stays (container `api` has two incidents); the pane header
keeps its tags and sentence and gains the verdict block under it ("Exit
1 (Error) 8 times since 14:03, last 14:40. Memory limit 128Mi, request
64Mi. 6 logs captured, restart 6 missing (no output). Same image digest
since open."); the actions become Acknowledge with `a` as the primary,
Note with `n`, an Actions menu, Ask AI at the right; the kubelet card's
Ready value reads `false` alone and the card title carries the method
on hover (`title`); Captured logs becomes the scrubber ("Restart 7 of 8
- 14:40:58", previous and next, the bar with one cell per file, the
missing one dashed) over one log body; the events card's method meta
moves to the title's hover. In the rail: OWNER CHAIN dots are neutral
(kinds have no colour); CONTEXT keeps Node, QoS class, uid and gains
Image id and Container id, and loses Cluster and Namespace; TIMES is
unchanged; SIBLINGS dots follow the rule (open red, live green, deleted
grey). In 2b: the same titlebar, identity line, rail changes; the tabs
and the events card are unchanged, the events card's meta moves to
hover.

The page note gains the sentences of decision 8: the verdict block and
its fields, the one primary action, the control only when a container
has two or more incidents, the scrubber and "Logs N" counting files
that exist, the breadcrumb and Forward (Cmd-]), the rail's contents,
the Timeline's fold of repeating cycles with one time per entry, the
debounced header tag, the events table's time column. Structure
sentences stay as they are.

Consumes: task 1. Produces: the visual reference for step D's pod page.

## Task 4 - page 5: the tree and the CronJob detail

Check: the page renders four frames; `grep -c 'NO CONTROLLER'
docs/mockups/idios-ui.html` is zero.

Frame 5a, "Workloads, a CronJob on Runs": the tree pane with its header
("open incidents", Collapse all, Expand all), the cluster and namespace
rows, plural captions with counts (DEPLOYMENTS 1, CRONJOBS 2, JOBS 1,
BARE PODS 5), neutral kind squares, the selected CronJob, the bare pod
rows carrying "2 pods" where a deleted pod of the same name is kept,
zero counts dimmed; a context menu drawn beside one row (Show incidents,
Show pods, Copy name, Acknowledge all open); the detail header with the
breadcrumb (`CronJob - orbstack / idios-smoke - schedule */2 * * * * -
backoff limit 1`); the tabs (Runs 63, 62 failed; Pods 7 live; Incidents
9 open; Overview); the run strip (label "Last 63 runs, oldest first -
07:32 to 09:59", 63 cells red with one green and one dashed and the
newest outlined, the legend with the hover, click and drag verbs); the
filters (All, Failed 62 selected, Live 0, "newest first"); the Runs
table (Run, Condition, Started, Ran, Reason, links) with three open
failed runs in red, one closed in grey with "same reason - pods
pruned", and "+ 58 older runs, same reason - show". Four markers and a
callout list.

Frame 5b, "A Deployment's Overview": today's 5a main pane redrawn: the
3d / 24h / 6h control, the four cards with neutral category chips and
the image tag chips neutral, the restarts chart with a y-axis (0, 5,
10), hover value on one bar, true hourly bars and dashed bars for hours
containing reconstructed rows.

Frame 5c, "Pods tab, Live by default": today's 5b with Live selected,
the badges on the rule (`CrashLoopBackOff` amber, `Running - ready`
green, `deleted` grey), the explanatory paragraph kept.

Frame 5d, "Rollouts tab": today's 5c with the incident count chips
neutral.

Today's 5d (the hourly Runs table) is retired; 5a holds the Runs tab.
The page note states decision 7 and keeps the sentences that still
hold (grouping by kind and name, success read from the condition, the
three-day window).

Consumes: task 1. Produces: the visual reference for step D's tree and
CronJob detail.

## Task 5 - page 7: the glance

Check: the frame's headline is the open count and the subline the
attention count.

Frame 7a: the menu bar strip with the glyph and `16`; the popover with
"16 open" and "205 need attention" and "1 cluster"; one row per
workload group (CronJob `smoke-cron-fail` "9 open runs, failing every
2m"; Deployment `smoke-crash` "3 pods looping, 122 restarts"; `smoke-oom`
"oom - 31 restarts"; `smoke-bad-image` "image pull";
`smoke-missing-config` "config") each with its newest time, a grey "2
more problems closed in the last 24 h" row, the cluster row green
"synced <1m ago", then Open idios (`Cmd-Shift-I`), Acknowledge
everything shown (`Option-A`), Status..., Clusters and namespaces....
The page note states decision 9: the headline, the subline, the Dock
badge, the fold, a row opening the group in the list.

Consumes: task 1. Produces: the visual reference for step E.

## Task 6 - page C: the colour rule

Check: C1 shows five state swatches and neutral category chips in both
appearances; `grep -c 's-manual">manual' docs/mockups/idios-ui.html` is
zero.

C1 "Colour rule and badge vocabulary": the five swatches (red open,
amber acknowledged, grey closed by any reason, green live and healthy,
accent selection) with one sentence each; then the rows: `incidents.
category` as twelve neutral chips (the eleven categories and `other`);
the lifecycle as `open`, `acknowledged`, `recovered`, `pod deleted`,
`job finished`, `resolved` (with `manual` on hover), `dismissed`;
container states running green, waiting and terminated-with-error
amber, `terminated - Completed` neutral; capture gaps dashed grey; the
dark strip repeating a sample. The frame's caption names the recorded
fallback of decision 2 in one sentence as not tried before step B.
C2: the kind squares neutral, the text unchanged. C3: the phase bar on
the rule (Running green, Pending amber, Succeeded grey, Failed amber),
the pod-deleted block unchanged. C4 unchanged. The page note gains the
one-colour-rule sentence.

Consumes: task 1. Produces: the vocabulary `BadgeStyle.swift` is
remapped to in step B.

## Task 7 - the presentation doc

Check: `hack/ascii-check docs/design/presentation.md`; `grep -n
'Marked resolved\|job-level\|state=attention&limit=5'
docs/design/presentation.md` finds nothing outside section 8's
endpoint table.

1. 4.2: `GET /v1/incidents` gains `pod_name` (an exact match on the
   pod's name, so a palette that holds the name from a truncated list
   still reaches the row; the join to `pods` uses the `(cluster_id,
   namespace, name)` index the table already has). 4.3: `GET /v1/pods`
   gains `pod_name` the same way.
2. 4.7: the menu bar counts open for its headline and attention for its
   subline, with the reasoning kept for the subline; its rows are the
   list's workload groups from `GET /incidents?state=attention` with
   the list limit, folded client-side; the Dock badge follows the
   headline.
3. 9.3 Incidents row: rewritten around decisions 1, 3, 4, 5 and 6: the
   source-list sidebar and where its sections show; grouping by
   workload by default with the header shape and the cluster or
   namespace header only while more than one is in scope; the run fold
   inside a CronJob's or Job's group and the "+ N more runs" line; the
   replica rollup kept for the other kinds with the changed sentence;
   expansion state per group id; the row anatomy with the fixed
   widths, the hover second line and the Comfortable density; closed
   rows dimmed; the label Resolved; the Restarts column and the "job"
   container value; the legend popover and the teaching empty views;
   the palette and the "/" filter; the keys. Every count a count of
   incidents.
4. 9.3 Pod page row: the sentences decision 8 changes: the segmented
   control only when a container has two or more incidents; the header
   led by the verdict block and its five fields; Acknowledge primary
   with its key, Note, the Actions menu; the scrubber and "Logs N";
   method text on card-title hover; the breadcrumb and Forward; the
   rail's contents; the Timeline's fold with the entries that never
   fold and one time per entry; the debounced tag; the events table's
   time column; plurals. The `job-level` chip sentence becomes the
   "job" container value.
5. 9.3 Workloads row: decision 7 in full: captions, pane header,
   collapse state, the bare-pod merge and the chooser, the context
   menu, the CronJob detail's run strip and its matrix past 200 runs,
   the Runs table's default, naming, links and fold, Pods on Live, the
   Overview's window and chart.
6. 9.3 Menu bar row: decision 9's headline, subline, rows, badge, and
   Status's header and human units.
7. 9.4 keyboard paragraph: Cmd-K palette (a menu key equivalent in the
   Go menu), "/" focusing the filter, Cmd-] Forward beside Cmd-[, the
   View menu's density and Group by, the palette's empty query as the
   discoverable command list.
8. 9.5: the colour rule replaces the C1 sentence: state owns hue, the
   four hues and the accent, categories as neutral chips, kinds without
   colour, where the rule applies, `BadgeStyle.swift` as the one place,
   the two existing rules kept (exit 0 neutral, the cluster dot).
9. New 9.6 "Vocabulary": one table, word by word, of the tooltip text
   for every state (open, acknowledged, recovered, pod deleted, job
   finished, resolved, dismissed, attention) and category (the eleven
   and other) as the application shows it, plus Restarts and the "job"
   container value; a paragraph on the legend popover reading the
   attention window from `Status.attention_window_seconds`.

Consumes: tasks 2 to 6 (the doc describes what the frames draw).
Produces: the spec statements steps B to E trace their tests to
(section 9.3 rows, 9.4, 9.5, 9.6, 4.2, 4.3, 4.7).

## Task 8 - status and review

`hack/ascii-check docs/mockups/idios-ui.html docs/design/presentation.md
docs/plans/m10-reading/reference.md`; open the mockup in a browser and
read pages 1, 2, 5, 7 and C in light and dark; the roadmap's step A
status line becomes "in progress" until the user's review and
"complete <date>" after it. One commit, "docs: redraw the mockup and
spec for m10", after the user's review of the diff.

## Hands to the next step

Step B (`list.md`) reads: the section 9.3 Incidents row, 9.4, 9.5 and
9.6 of `docs/design/presentation.md`; page 1 frames 1a and 1c and page
C of the mockup. Step C reads 4.2 and 4.3 (`pod_name`) and frame 1b.
Step D reads the Pod page and Workloads rows and pages 2 and 5. Step E
reads 4.7, the Menu bar row and page 7. No names are handed over: this
step produces no code.

## Self-review

Spec coverage: decision 1 (frame 1a, Incidents row), 2 (page C, 9.5),
3 (frame 1c, 9.6, the labels on every redrawn frame), 4 (frame 1a's
rows, Incidents row), 5 (frame 1a's sidebar, Incidents row), 6 (frame
1b, 4.2, 4.3, 9.4), 7 (page 5, Workloads row), 8 (page 2, Pod page
row), 9 (page 7, 4.7, Menu bar row), 10 (every name invented; the
smoke names are fixtures). Decision 3's legend has a frame (1c) though
the roadmap's step A paragraph lists page 1 as the list and the
palette only: step B builds the popover and needs a drawing to build
to, so the frame is the smaller risk. The run strip's cells are
coloured by outcome (failed red, Complete green) as the artifact drew
them; the doc says so and says the Runs table's Condition badge takes
its incident's state, which is where the rule and the outcome differ.
Pages 3 and 4 keep inline coloured dots that the rule will recolour in
the application; they are not this step's pages and are left as they
are. `.ai` rules: ASCII (entities for every glyph), comments say why
(the stylesheet's two comments), code is the truth rule 6 (notes state
what is), scope (five pages untouched, no code), commits (one commit
after review). Type consistency: no types; the grid columns of
`.hdrrow`, `.grp` and `.r` are one declaration.
