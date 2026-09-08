# m10 - Reading: closing report

Status: complete 2026-09-05. Steps A to G shipped on branch `m10-reading`;
the plans in this directory are frozen and this report is what a later
milestone reads first. The code is the truth; where this report and the
code disagree, the code is right and this report is old.

## What the twelve decisions cost and left

Every decision is in the code, two of them not in the shape the roadmap
drew.

1. Problems, not incidents: `incidentGroups`, `groupEntries`,
   `groupSummary`, `groupBadge` and the run fold, drawn by the list and
   the menu bar. A header is drawn only over two or more entries or a
   CronJob (a header over one row would say the row twice); the header's
   Restarts cell is "-" because no view sums occurrences.
2. One colour rule: `BadgeStyle.swift` is the one place colour lives.
   The recorded fallback (one muted hue per category family) was never
   tried, because the list did not read flat after step B; it stays a
   recorded fallback and not a plan.
3. One vocabulary: `IncidentState.title/label/tooltip/key`,
   `Category.tooltip`, `kindWords`, `ownerChains`, the legend popover
   reading the attention window from Status.
4. Row anatomy: `ListColumns`, one line by default, the second line on
   hover and in Comfortable density.
5. The source-list sidebar; Screens stays under a pushed page.
6. Search: `SearchQuery` and `searchResults` client-side over the rows
   the stores hold, plus the exact `pod_name` filter on the daemon.
7. Workloads: the tree, `runCells`, `runTableRows`, the run strip and
   the matrix past 200 runs, the context menu.
8. The pod page by subtraction: `verdictSentences`, the scrubber, the
   breadcrumb, Forward, `timelineFolds`, the debounced tag.
9. The glance: the menu bar counting open, the Dock badge, Status in the
   system font and human units, `related_incidents` decoded.
10. No real name anywhere in fixtures, tests, docs or plans.
11. The run as a screen: `Route.run`, `incidentRoute`, `runTitle`,
    `runStateTag`, `runCountsLine`, `runAttempts`, `runVerdictSentences`,
    `RunPageStore`, the `job_uid` filter on `GET /jobs`; the list's
    children drawn as children through `GroupChild`; the legend's Kinds.
    The bordered-box fallback for the list's nesting was never tried:
    the guide rail and the tint were enough on the smoke data.
12. The explanation, in a different shape from the one decided. The
    coach-mark overlay (scrim, numbered pills, notes, a reading path
    with a "Reading flow only" toggle, a `chrome` region for the
    toolbar) was built for the pod page, the list, Workloads, the Run
    page and Status, reviewed on real data, and replaced: the outlined
    boxes overlapped, outlines were drawn over content that had scrolled
    away, and lighting every part of a screen lit the whole screen. What
    shipped is help mode: the [?] toggles it, each explained region
    draws its own small round "?" mark (inside its top-right corner, or
    beside it where the region is under thirty points tall), clicking a
    mark outlines the region and opens its note as a popover, Esc closes.
    The tables were cut to the major parts of each screen, and the menu
    bar popover and the four sheets carry no marks at all: a 360-point
    popover and a form have no room for them and their controls carry
    tooltips. The texts are unchanged and are the part worth keeping.
    Section 9.7 of the presentation doc and the "Revision" and "Task 7"
    notes in `explain.md` record it.

## Seams that outlive the milestone

`groupEntries`, `groupSummary`, `groupBadge` (one fold, two screens);
`incidentGroups` (one grouping function, two callers); `runCells`,
`runTableRows`, `RunStripView` with `marked`; `verdictSentences`,
`timelineFolds`; `SearchQuery`, `searchResults`; `humanDuration` (the
one place seconds become words); `BadgeStyle` (the one place colour
lives); `incidentRoute` (the one place an incident's page is chosen);
`runAttempts` and `RunAttempt` (the run's row shape, now one row per pod
with an `AttemptOutcome`); `RunPageStore.attemptLimit`; `kindWords` and
`ownerChains`; `GroupChild` and `ListColumns.childIndent`; `EventColumns`
in `EventsCard`; `Navigator.open(_:)` and `GroupTarget`;
`IncidentDetail.relatedIncidents`; `plural(_:_:)`. From step G:
`ExplainedRegion`, `ExplainedScreen` and `explainedRegions(for:)` as the
one place the application says what a screen is for (a screen added
later is a case there and a table in 9.7), the `.explained(...)`
modifiers that cost a view one preference entry, `explainable(_:)` and
`ExplainState` at the window's root, and `Screenshot.reportCoverage`
with the `--explain` screenshot flag as the gate that keeps a region and
its explanation from drifting apart. `readingOrder` and `ReadingStep`
were removed with the reading path.

## What a client outside this repository would notice

- An exact `pod_name` filter on `GET /incidents` and `GET /pods` (step
  C), served by the existing `pods (cluster_id, namespace, name)` index.
- A `job_uid` filter on `GET /jobs` (step F), because a Job can be read
  by uid no other way.
- A `job_uid` filter on `GET /pods` (the close of step G), the
  controller uid of the owning Job, with a new index
  `pods_controller ON pods (controller_uid)`. That is a schema change:
  `0001_init.sql` was edited in place and the databases recreated.

## Deviations, as reviewed and accepted

Step E: resourceLine returns nil only when a pair has neither a written
nor a parsed value; only ContainerPane builds KubeletCard; the pod route
has no Related tab segment; the popover's rows are the first page of
attention rows, so a problem whose rows fall off the page is not shown;
the popover uses "- " as its summary separator; the group title carries
layoutPriority; the menu bar store zeroes the badge inside its error
report when the daemon is unreachable; the dialog's message text was
dropped; cab49bb fixed a step B bug (a headerless group drawn collapsed).

Step F: the events table's column budget is a measured width, not
min/ideal/max frames (a wrapping message's ideal width is its whole text,
so ranged columns pushed the pod page past the window); floors are 60,
62, 44 and message 140; the older 9.3 sentence about a job-subject
incident joining its failing container's segments was removed;
runVerdictSentences takes now but no sentence uses it; runTitle with no
job name under a CronJob falls back to the elided uid; PaneActionsRow
takes a writes store, an apply closure and an optional bulk acknowledge,
with an extension init for the pod page; the rail's Schedule row is never
drawn because nothing in the API carries a schedule; the Run page opens
incidents and sibling runs through Navigator; the generated ListJobs
query spells limit before job_uid; JobsCard.runRow is a tap gesture, not
a Button; RunStripView and RunMatrixView lost openPod and openIncident as
unused; a job-subject incident with neither job uid nor pod reports a
not-found error; the Job's own row is not an attempt; the counts line
reads "N failed attempts" and a Complete run's verdict names the
succeeded pods from the Job's counter, with a footer in the Attempts
card; the list's guide rail is drawn per row (hairline gaps at row
boundaries) and the 0.12 tint is faint; the legend gained a "Kinds"
heading that 9.6 names and the plan's code block did not.

Step G, beyond the redesign above: `tags` is plural where decision 12
says tag, because one chip cannot carry four field names; the pod page's
Pod card tables are one per pane, each carrying the tag, the container
cards, the rail's two and only that pane's tab region, because only the
visible tab registers; the container's Timeline and Logs tabs are their
own tables for the same reason; the vocabulary test matches a title to a
state, category or kind word exactly, so a region titled otherwise is
free to speak of a state in its own words; the coverage line's
"unexplained" set is an id no table anywhere names rather than an id the
current table lacks, because a pushed page keeps the screen it covers in
the view tree; the coverage report runs 600 ms after help mode opens so
a table passed through at launch never reports; the incidents list marks
the first row of each kind and those ids, with every other region that
depends on data (a namespace's kind caption and bare pods, a runs
table's condition column and fold, a rollouts table and its caveat, a
standalone Job's other runs, a container with no incident's verdict), are
exempt from the "undrawn" half of the check; the `blendMode
(.destinationOut)` hole the plan specified does not cut a material, an
even-odd mask did, and both are gone with the scrim. The close of step G
also drew a run's succeeded attempts (the daemon change above), read
"Every failed attempt exits N" once an attempt succeeded, made the
list's cluster and namespace headers section titles without a category
chip (an `IncidentGroup.scope` flag, because nothing else told a
namespace header from a category or time header), and fixed the Workloads pane header clipping "Expand all" at the
tree's width.

## Left as decided, and why

Rulings the steps made inside their decisions, so nobody relitigates
them blind: "Show incidents" in the tree's context menu opens the
workload's Incidents tab, because the list has no workload filter and no
endpoint was to be added; the breadcrumb's cluster and namespace
segments reveal that node in the tree rather than navigate, because
`Route` has no case for them; the Closed disclosure is a heading and not
a view, because the wire has no closed filter and a client-side one
would make the count and the list disagree; acknowledge on a run acts on
every incident of the run at once, as `a` on a group header does; a
palette lookup on the daemon runs only for a query with a dash and three
characters; the Overview's widest window is labelled "3d" and means the
daemon's whole window whatever retention is; Back's Forward returns to a
pushed page and carries no history of screen switches, which Cmd-1/2/3
and the palette's recent pages cover; the menu bar's reveal sets the view
to Attention and the grouping to workload, and widens a narrowed cluster
scope, because a row that led nowhere would be worse; the headline is
`open` and excludes acknowledged rows, as 4.7 says; `humanDuration`
answers in the coarsest unit that divides exactly (5400 s is "90 min");
the Related tab of a Job's pod is kept beside the breadcrumb's Job
segment, because it is the one place a pod page shows the run's other
pods without leaving.

Gaps that stay, each a decision for a later milestone:

- The menu bar's CronJob row reads "9 runs failed" rather than "62 of 63
  runs failed", because the stronger sentence needs `/workloads` and
  `/jobs` on every open of a popover that is up for seconds.
- The menu bar popover shows the first page of attention rows, so a
  problem whose rows fall off the page is not shown.
- `ExplainState` is one per window and the Help menu item finds the
  frontmost screen through a serial; two windows of the application
  would share both, and the application has never been driven with two.
- Help mode on a narrow window, where the split view's sidebar becomes
  an overlay column, was not walked.
- The list's cluster header needs two clusters in scope and neither the
  fixtures nor the smoke store have two; it shares its treatment with the
  namespace header, which was photographed in the Namespace grouping.
- The list's child tint against its group header in dark appearance was
  not checked: the `-AppleInterfaceStyle Dark` launch argument does not
  switch the application's appearance, and no dark screenshot exists.
- The screenshot script exec'd from a shell without a GUI session never
  gets a window; the review captures of this step were taken by
  launching the Debug application with `open -n ... --args` instead.

## The presentation doc against the code

Step A wrote the spec before the code and every later step changed it
in the same commit as the code. The mismatches found at the close and
fixed: 9.3's Run page row said an attempt that succeeded is counted and
not drawn (it is drawn now, with its outcome), and its rail listed a
schedule in the run's context that nothing in the API carries; 9.7 and
9.3's [?] sentence described the coach-mark overlay and now describe
help mode; the `GET /v1/pods` row gained `job_uid`. Nothing else known
to differ; the tables of 9.7 are compared to the code by
`thePodPageSaysTheSameWordsTheOverlayWasApprovedWith` for the pod page
and by the read for the rest.
