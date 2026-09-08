# m10 step B - application: the list, the sidebar, the colour

Goal: after this plan the Incidents screen reads as problems. A CronJob
that fails every two minutes is one group with a plain-language header
and five run rows, not 56 job rows and a rollup; a Deployment's crash
loop is one header over one rollup; every row is one line with real
columns and the age never wraps. The sidebar is a source list with the
closed states under a disclosure, every count kept, the empty categories
folded into one dimmed line, and a "?" that opens the legend, whose
Attention paragraph reads the window from the daemon's status. One
colour rule holds: state owns hue, a category is a neutral chip, a kind
has no colour, and `BadgeStyle.swift` is still the one file that knows a
colour. `manual` reads Resolved everywhere. Every state and category
carries its one-sentence tooltip, and an empty view teaches what would
fill it. Nothing in `api/`, `internal/` or `cmd/` changes.

Architecture: the reading decisions become pure functions in
`IdiosModel` with table tests, and the views draw what the functions
return. `Run.swift` folds the rows of one Job (`job_uid`) into a
`RunFold` and `GroupEntry` gains a `run` case; `groupEntries` decides per
row whether it belongs to a run (a CronJob's or Job's row carrying a job
uid) or to the m8 rollup and pod fold that stay for every other kind.
`GroupSummary.swift` builds the header's sentence and its one state
badge from the rows plus the optional facts a workload row or a jobs
page adds (`live_pods`, `total`, `failed_total`), and the list's summary
line. `Sidebar.swift` holds the closed count, the category fold and the
duration in human units; `Vocabulary.swift` holds the reader's word for
every state and category (Resolved for `manual`), the keys, and the
tooltip sentences the presentation doc fixes. In the application,
`BadgeStyle.swift` is remapped once; `IncidentsSidebar` becomes a `List`
whose selection is a set derived from the screen, the view and the
category, so the accent lands on the active row; a `GroupFactsStore`
loads the workload rows of the scope and one jobs page per CronJob or
Job group so the header can say "62 of 63 runs"; `IncidentsList` draws
a column header, group headers, run rows, rollup rows and one-line
incident rows on one set of column widths; the expansion state moves
above the list into `IncidentsScreen` so it outlives a grouping change;
the sidebar stays under a pushed pod page with its Screens section
alone, replacing the m9 column collapse. Views never import `IdiosAPI`;
stores own every call.

Tech stack: Swift 6 package `macos/Sources/IdiosModel` with tests in
`macos/Tests/IdiosModelTests` (`make app-test`); the Xcode application
(`make app`; a file under `macos/idios/` joins the target by existing).
No proto change. SwiftUI `List` with a `Set` selection for the sidebar,
`List` with custom rows for the incidents, `Popover` for the legend,
`CommandGroup` for the View menu items. Screenshots through
`hack/macos/screenshot.sh` against a private daemon on 7771.

Spec: `docs/design/presentation.md` section 9.3 Incidents row (the
sidebar, grouping, the run fold, the rollup kept, expansion state, the
row anatomy, Resolved, the legend, the empty views, the summary line,
the keys), the Status row's "human units" clause (the legend's window),
9.4 (the keyboard layer: single letters through `.onKeyPress`, the View
menu's density and group mode), 9.5 (the colour rule), 9.6 (the
vocabulary table). Roadmap decisions 1 to 5 and 10 of
`docs/plans/m10-reading/roadmap.md`. Visual reference: frames 1a and 1c
of page 1 and C1 of page C in `docs/mockups/idios-ui.html`.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Swift follows the Go rules: one-line doc comment per
public type and function, comments say why, tests trace to a spec
statement with whole-value assertions, variants are table rows. Views
never import `IdiosAPI`; stores own every client call and stream; a
cancelled call maps to `APIError.cancelled` and every store ignores it.
The only hard-coded colours live in `BadgeStyle.swift`. Every count is a
count of incidents; no view sums `occurrences`. A bare-letter key goes
through `.onKeyPress` on the focused view, never a bare
`keyboardShortcut`. `ForEach` repeats one element when two share an id,
so every list row has a distinct tag. Never
`.fixedSize(horizontal: false, vertical: true)` on a concatenated
multi-font `Text` in a header. Nothing in a test, comment, doc or
fixture names a real organisation, cluster, namespace, workload, image
or node; the invented names are the fixtures' (`checkout-api`, `report`,
`idios-smoke`, `api`, `node-a`) and the ones this plan adds
(`nightly`, `worker`). Checkpoint before every commit: `go build ./... &&
go test ./... && make ascii && make app-test && make app`. Implementers
never commit; nothing is committed without the user's review of the
diff. The decisions are made; where a frame and a sentence differ the
sentence of section 9.3 wins and the doubt goes in the self-review.

## File structure

    macos/Sources/IdiosModel/Vocabulary.swift          IncidentState.title, .label (Resolved), .key, .tooltip; CloseReason.label; Category.tooltip; restartsTooltip, jobContainerTooltip (task 1)
    macos/Sources/IdiosModel/Sidebar.swift             closedStates, closedCount, CategoryRows, categoryRows, humanDuration (task 1)
    macos/Sources/IdiosModel/Display.swift             scopeLabel says "job" and "pod"; clockTime moves in (task 1)
    macos/Sources/IdiosModel/Enums.swift               IncidentState.label and CloseReason.label leave for Vocabulary.swift (task 1)
    macos/Tests/IdiosModelTests/SidebarTests.swift     the closed count, the category fold, the labels, the human duration (task 1)
    macos/Tests/IdiosModelTests/DisplayTests.swift     scopeLabel rows updated (task 1)
    macos/Sources/IdiosModel/Run.swift                 RunFold, runSuffix, runFolds, shownRuns (task 2)
    macos/Sources/IdiosModel/Rollup.swift              GroupEntry.run; groupEntries routes a job's rows to runFolds (task 2)
    macos/Sources/IdiosModel/GroupSummary.swift        GroupFacts, GroupBadge, StateTone, groupBadge, runCadence, groupSummary, ListSummary, listSummary (task 2)
    macos/Tests/IdiosModelTests/RunTests.swift         the run fold, the suffix, the five shown (task 2)
    macos/Tests/IdiosModelTests/GroupSummaryTests.swift the header sentence, the badge, the cadence, the summary line (task 2)
    macos/idios/Views/Components/BadgeStyle.swift      remapped to the colour rule; yellow and purple deleted (task 3)
    macos/idios/Views/Components/Badges.swift          CategoryBadge as an outlined chip with a glyph; StateBadge reads the title and the tooltip; fixed widths (task 3)
    macos/idios/Views/Incidents/IncidentGrouping.swift squareStyle leaves; kind carries no colour (task 3)
    macos/idios/App/Preferences.swift                  Density; Preferences.density (task 4)
    macos/idios/App/IdiosApp.swift                     the View menu: Group by and Density (task 4)
    macos/idios/Views/Incidents/IncidentsSidebar.swift the source list, SidebarItem, the Closed disclosure, the category fold, the "?" (task 4)
    macos/idios/Views/Incidents/LegendPopover.swift    the lifecycle, the states with keys, Attention with the window, the Status link (task 4)
    macos/idios/Views/Incidents/IncidentsScreen.swift  sidebar under a pushed page, chips gone, View menu button, "/" (task 4); expansion state, facts store, openWorkloadRuns, summary actions (task 5)
    macos/idios/Store/IncidentsStore.swift             IncidentFilter.stateTitle leaves for IncidentState.title (task 4)
    macos/idios/Store/GroupFactsStore.swift            live pods per workload, runs total and failed per CronJob or Job (task 5)
    macos/idios/Views/Incidents/ListColumns.swift      the column widths, the column header row (task 5)
    macos/idios/Views/Incidents/GroupHeaderView.swift  the header shape (task 5)
    macos/idios/Views/Incidents/RunRowView.swift       one run (task 5)
    macos/idios/Views/Incidents/IncidentRowView.swift  one line with columns, the hover and Comfortable second line (task 5)
    macos/idios/Views/Incidents/RollupRowView.swift    on the columns (task 5)
    macos/idios/Views/Incidents/IncidentsList.swift    entries, the more-runs line, the summary line, the empty views, the keys (task 5)
    macos/idios/Views/Incidents/IncidentGrouping.swift IncidentGroup carries its summary, badge and facts key (task 5)
    docs/plans/m10-reading/roadmap.md                  step B's status line (task 6)

## Task 1 - vocabulary and the sidebar's numbers

`macos/Sources/IdiosModel/Vocabulary.swift`. The `label` extensions of
`IncidentState` and `CloseReason` move out of `Enums.swift` into this
file (the other `label` extensions stay), because `manual` is the one
value whose reader's word is not its raw value with the underscores
replaced:

    extension IncidentState {
        /// title is the reader's word for this state as a heading: the sidebar
        /// row, the list title, the empty view.
        public var title: String {
            switch self {
            case .open: "Open"
            case .acknowledged: "Acknowledged"
            case .recovered: "Recovered"
            case .podDeleted: "Pod deleted"
            case .jobFinished: "Job finished"
            case .manual: "Resolved"
            case .dismissed: "Dismissed"
            case .attention: "Attention"
            }
        }

        /// label is the reader's word for this state in a badge; the raw value
        /// stays on hover and on copy.
        public var label: String { title.lowercased() }

        /// key is the letter that puts an incident into this state, when one does.
        public var key: String? {
            switch self {
            case .acknowledged: "a"
            case .manual: "r"
            case .dismissed: "d"
            default: nil
            }
        }

        /// tooltip is the one sentence every mention of this state carries.
        public var tooltip: String { ... }
    }

    extension CloseReason {
        /// label is the reader's word for this reason; a manual close reads
        /// resolved, as its state does, with the raw value on hover.
        public var label: String { self == .manual ? "resolved" : rawValue.replacingOccurrences(of: "_", with: " ") }
    }

    extension Category {
        /// tooltip is the one sentence every mention of this category carries.
        public var tooltip: String { ... }
    }

    /// restartsTooltip explains the Restarts column.
    public let restartsTooltip = "..."

    /// jobContainerTooltip explains the container column reading job.
    public let jobContainerTooltip = "..."

The tooltip bodies are the table of section 9.6, word for word, with
the wire value in parentheses where the label differs (`Resolved
(manual)`, `image pull (image_pull)`); Attention's sentence keeps its
"N hours" wording because the legend, not the tooltip, fills the
number. They are constants and get no test.

`macos/Sources/IdiosModel/Sidebar.swift`:

    extension IncidentState {
        /// closedStates is what the sidebar's Closed disclosure holds, in its order.
        public static let closedStates: [IncidentState] = [.recovered, .podDeleted, .jobFinished, .manual, .dismissed]
    }

    /// closedCount is the Closed disclosure's own number: every state a row
    /// leaves the open list by, dismissed included because it outranks the rest.
    public func closedCount(_ counts: IncidentCounts) -> Int32 {
        IncidentState.closedStates.reduce(0) { $0 + (counts.byState[$1] ?? 0) }
    }

    /// CategoryRows is the Category section: the categories drawn as rows and
    /// how many are folded into the "more with none" line.
    public struct CategoryRows: Hashable, Sendable {
        public let shown: [Category]
        public let hidden: Int
    }

    /// categoryRows keeps a category as a row while it counts anything or is the
    /// active filter, so the row a person clicked never vanishes under them;
    /// with no counts yet, every category is a row.
    public func categoryRows(counts: IncidentCounts?, active: Category?) -> CategoryRows

The order is `Category.ranked + [.other]`. `humanDuration` renders the
daemon's seconds the way the legend and the status screen say them:
whole hours as "24 h", whole minutes as "10 min", anything else as
"90 s"; a value that is whole hours is never said in minutes.

    /// humanDuration renders a configured number of seconds in the coarsest
    /// unit that divides it.
    public func humanDuration(seconds: Int32) -> String

`macos/Sources/IdiosModel/Display.swift`: `scopeLabel` returns `"job"`
for a job-subject row and `"pod"` for a pod-level one (its doc comment
loses "job-level"); `clockTime(_:seconds:)` moves here from
`macos/idios/Views/Components/TextTreatments.swift`, public, unchanged,
because the group summary of task 2 prints a clock time. Every view
that called it already imports `IdiosModel`.

Tests first, `macos/Tests/IdiosModelTests/SidebarTests.swift`, whole
values, one table per behaviour:

- `closedIsTheSumOfEveryClosingState`, traced to the Incidents row's
  "a Closed disclosure holding Recovered, Pod deleted, Job finished,
  Resolved, Dismissed" with "every row carrying its count": empty
  counts -> 0; `[.open: 16]` -> 0; `[.recovered: 1, .podDeleted: 121,
  .jobFinished: 55, .manual: 2, .dismissed: 3, .open: 16, .attention:
  20]` -> 182.
- `categoriesWithNoIncidentFoldIntoOneRow`, traced to "the categories
  with no incident folded into one dimmed 'N more with none' row" and
  the active-row sentence: `(nil, nil)` -> shown all twelve in ranked
  order then `.other`, hidden 0; every category zero, no active ->
  shown `[]`, hidden 12; `[.crash: 10, .jobFailed: 3, .oom: 1]`, no
  active -> shown `[.crash, .oom, .jobFailed]`, hidden 9; the same
  counts with active `.probe` -> shown `[.crash, .oom, .probe,
  .jobFailed]`, hidden 8; `[.other: 2]` -> shown `[.other]`, hidden 11.
- `stateWordsReadResolvedForManual`, traced to "`manual` is labelled
  Resolved everywhere": one row per `IncidentState` with `(title,
  label, key)` expected whole, the manual row `("Resolved", "resolved",
  "r")`, the attention row `("Attention", "attention", nil)`; and a
  second table over `CloseReason.label` with `.manual` -> `"resolved"`
  and `.podDeleted` -> `"pod deleted"`.
- `configuredSecondsReadInTheCoarsestWholeUnit`, traced to section 9.6's
  "its window read from `Status.attention_window_seconds`" and the
  Status row's "human units (24 h, 10 min)": 0 -> `"0 s"`; 90 ->
  `"90 s"`; 600 -> `"10 min"`; 5400 -> `"90 min"`; 3600 -> `"1 h"`;
  86400 -> `"24 h"`.

`DisplayTests.swift`: the `scopeLabel` table's rows change to `"job"`
and `"pod"`, traced to the Incidents row's "container (reading 'job'
for a job-subject row)". If `clockTime` has no test, none is added:
the code moves.

Run `make app-test`: compile failure on the missing names. Implement,
run again; `make app` proves every caller of `label` and `clockTime`
still compiles (the sidebar and the menu bar read `label` today).

Checkpoint: `go build ./... && go test ./... && make ascii && make
app-test && make app`. Commit: `model: add the vocabulary and the
sidebar's counts`.

Consumes: `IncidentState`, `CloseReason`, `Category`, `Category.ranked`,
`IncidentCounts` of `Incident.swift`, `scopeLabel` of `Display.swift`,
`clockTime` of `TextTreatments.swift`.
Produces: `IncidentState.title`, `.label`, `.key`, `.tooltip`,
`IncidentState.closedStates`, `CloseReason.label`, `Category.tooltip`,
`restartsTooltip`, `jobContainerTooltip`, `closedCount`,
`CategoryRows`, `categoryRows`, `humanDuration`, `clockTime` (in
`IdiosModel`), `scopeLabel` saying `"job"` / `"pod"`.

## Task 2 - the run fold and the header's sentence

`macos/Sources/IdiosModel/Run.swift`:

    /// RunFold is one Job's incidents inside a CronJob's or Job's group: the
    /// job_failed row and the pod rows of that run, read as one line.
    public struct RunFold: Identifiable, Hashable, Sendable {
        public let id: String
        public let jobUID: String
        /// suffix is the run's name after "<cronjob>-", or nil when no pod row
        /// is kept to read it from or the workload is the Job itself.
        public let suffix: String?
        public let lead: Incident
        public let rows: [Incident]
    }

    extension RunFold {
        /// podCount is how many pods the run's rows name.
        public var podCount: Int
        /// categories is every category among the rows, worst first, once each.
        public var categories: [Category]
        /// reasons is every last reason among the rows, once each, first seen first.
        public var reasons: [String]
        /// exitCodes is every exit code among the rows, once each, first seen first.
        public var exitCodes: [Int32]
        /// openCount is how many of the rows are still open.
        public var openCount: Int
        /// podSuffixes is each pod's name after the run's name, in served order,
        /// for a Job whose run is the Job itself.
        public var podSuffixes: [String]
    }

    /// runSuffix is the part of a pod name that names its run: a CronJob's pod
    /// is "<cronjob>-<run>-<hash>", so the run is the segment between the
    /// workload prefix and the pod's own hash; a Job's pod has no run segment
    /// because the Job is the run.
    public func runSuffix(podName: String, workloadName: String, workloadKind: String) -> String?

    /// runFolds gathers rows by job uid, the run with the newest row first, and
    /// leads each with the pod row the list would have led with, or the job row
    /// when no pod row is kept, so opening a run opens the pod that failed.
    public func runFolds(_ rows: [Incident]) -> [RunFold]

    /// shownRuns cuts a group's runs to the newest few and counts the rest,
    /// which the "+ N more runs" line names.
    public func shownRuns(_ entries: [GroupEntry], limit: Int) -> (shown: [GroupEntry], more: Int)

`lead` is `rows.filter { $0.podUID != nil }.min(by: foldOrder)`, else
the job row. `id` is `"run/\(jobUID)"`. `suffix` is `runSuffix` of the
lead's pod name when the lead has one, else the first pod row's, else
nil. `podSuffixes` is `podNameSuffix(name:workloadName:)` per pod row
for a Job, and the segment after `"<workload>-<run>-"` for a CronJob.
`shownRuns` leaves non-run entries alone: it cuts only the `.run`
entries and keeps every `.fold` or `.rollup` in place.

`macos/Sources/IdiosModel/Rollup.swift`: `GroupEntry` gains
`case run(RunFold)` with its id; `groupEntries` first sets aside every
row whose `workloadKind` is `"CronJob"` or `"Job"` and whose `jobUID`
is set, folds them with `runFolds`, and puts the run entries where the
first of their rows stood in served order, ahead of the remaining rows'
rollup and fold logic, which is unchanged. The doc comment gains the
sentence "a job-subject incident folds into its run". `RollupTests`
and `FoldTests` pass unchanged.

`macos/Sources/IdiosModel/GroupSummary.swift`:

    /// GroupFacts is what the incident rows cannot say about their workload: the
    /// pods it runs and the runs it has had, read from the workload and jobs
    /// endpoints when the store has them.
    public struct GroupFacts: Hashable, Sendable {
        public let livePods: Int32?
        public let runsTotal: Int32?
        public let runsFailed: Int32?
        public init(livePods: Int32?, runsTotal: Int32?, runsFailed: Int32?)
    }

    /// StateTone is which of the three hues a badge that summarises several
    /// rows takes.
    public enum StateTone: Hashable, Sendable { case open, acknowledged, closed }

    /// GroupBadge is the one state badge of a header: its text and its tone.
    public struct GroupBadge: Hashable, Sendable {
        public let text: String
        public let tone: StateTone
    }

    /// groupBadge counts the open rows; the tone is red while one of them is
    /// unacknowledged, amber when every open row is acknowledged, grey when
    /// none is open.
    public func groupBadge(_ rows: [Incident]) -> GroupBadge

    /// runCadence is how often a run has been opening: the median gap between
    /// consecutive runs' openings, or nil under three runs, where a gap is not
    /// yet a rhythm.
    public func runCadence(openedAt: [Timestamp]) -> String?

    /// groupSummary is the header's plain-language line: for a CronJob or Job
    /// how often it runs, how many runs failed of how many, and since when; for
    /// every other kind how many pods have the problem of how many live, what
    /// the problem is, and which image tags are involved.
    public func groupSummary(kind: String, rows: [Incident], facts: GroupFacts?) -> String

    /// ListSummary is the line above the list.
    public struct ListSummary: Hashable, Sendable {
        public let problems: Int
        public let openIncidents: Int
        public let workloads: Int
        public let barePods: Int
        public let newest: Timestamp?
    }

    /// listSummary counts the groups as problems, the rows still open, the
    /// groups that have a workload and the ones that are a bare pod, and finds
    /// the newest last seen.
    public func listSummary(groups: [[Incident]]) -> ListSummary

`groupSummary` rules. Empty rows: `""`. CronJob or Job: the parts,
joined by ", ": `"every <cadence>"` when `runCadence` over the earliest
`openedAt` of each run is non-nil; `"<failed> of <total> runs failed"`
when both facts are set, else `"<runs> runs failed"` with the count of
runs among the rows (singular "1 run failed"); `"since <clockTime of
the oldest openedAt>"`. Other kinds: pods = distinct `podUID` among
the open rows; when zero, `"<distinct pods among all rows> pods,
nothing open"`; else `"<pods> of <livePods> pods <verb>"` when
`livePods` is set, else `"<pods> pods <verb>"`, the verb by the worst
open category: crash "looping", oom "out of memory", unclean exit
"exiting badly", image pull "cannot pull the image", config "cannot
start", probe "failing probes", scheduling "unschedulable", stuck
"stuck", node pressure "evicted", rescheduled "rescheduled", job
failed "failed", other "failing"; then `"tag <t>"` for one distinct
image tag, `"tags <a>, <b>"` for several, nothing for none. Plurals
agree ("1 pod"). `runCadence` uses `durationText` on the median gap,
with a trailing " 0m" dropped so an hourly CronJob reads "every 1h".

Tests first. `macos/Tests/IdiosModelTests/RunTests.swift`, with `row(...)`
of `FoldTests.swift` widened by `job: String? = crashIncident.jobUID`,
`podName: String? = crashIncident.podName`, `kind: String =
crashIncident.workloadKind` parameters (defaults keep every existing
call), and `t1 > t2 > t3` constants as `RollupTests` spells them:

- `aJobsIncidentsFoldIntoOneRun`, traced to the Incidents row's "the
  `job_failed` row and the pod rows of one Job (`job_uid`) fold into
  one run row naming the run's suffix, its pod count, the categories
  found among them, the exit code, the reason and the open count".
  Rows use workload `report`, kind `CronJob`: empty -> `[]`; one job row
  (`pod: nil`, job `j1`, category `.jobFailed`, no pod name) -> one run
  `suffix nil`, lead the job row, podCount 0, categories `[.jobFailed]`;
  a job row plus two pod rows of `j1` named `report-29807159-5dtpb` and
  `report-29807159-s966s`, one `.crash` open with exit 1 and one
  `.crash` closed -> one run, suffix `"29807159"`, lead the open pod
  row, podCount 2, categories `[.crash, .jobFailed]`, exitCodes `[1]`,
  openCount 2 (the job row and the open pod row); two runs `j1` newer
  and `j2` older in served order -> `[j1, j2]`; the two runs served
  interleaved (`j2`'s row first) -> `[j2, j1]`, the order of first
  appearance.
- `theRunSuffixIsTheSegmentBetweenWorkloadAndHash`, traced to the same
  sentence and the Workloads row's "names runs by the suffix after
  `<cronjob>-`": `("report-29807159-5dtpb", "report", "CronJob")` ->
  `"29807159"`; `("report-5dtpb", "report", "CronJob")` -> nil;
  `("nightly-29807159-5dtpb", "report", "CronJob")` -> nil;
  `("retry-bmrcw", "retry", "Job")` -> nil; `("", "report", "CronJob")`
  -> nil.
- `aJobsRowsBecomeRunsAndTheRestKeepTheirFold`, traced to "a bare pod
  never rolls up; a job-subject incident folds into its run" and "the
  replica rollup keeps its key ... for the other kinds": a CronJob job
  row plus its pod row, then a Deployment pod row (kind `Deployment`,
  job nil) -> `[.run(...), .fold(...)]` whole; a CronJob pod row with
  `job: nil` -> `.fold`, not a run; three Deployment pods sharing the
  key -> one `.rollup` as `RollupTests` already expects (one row copied
  from there, so the two files agree).
- `theNewestFiveRunsShowAndTheRestAreCounted`, traced to "the newest
  five runs are shown and a '+ N more runs' line": runs built from
  `j1`..`jN` with `limit: 5`: 0 -> `([], 0)`; 3 -> all, 0; 5 -> all, 0;
  6 -> first five, 1; 8 with a `.fold` entry after the runs -> first
  five runs then the fold, 3.

`macos/Tests/IdiosModelTests/GroupSummaryTests.swift`:

- `aHeaderSaysTheProblemInOneSentence`, traced to the Incidents row's
  summary examples ("every 2m, 62 of 63 runs failed, since 07:32" for a
  CronJob; "3 of 3 pods looping, tag 1.36" for a Deployment): empty ->
  `""`; CronJob, three runs opened at 07:32, 07:34, 07:36, facts
  `(nil, 63, 62)` -> `"every 2m, 62 of 63 runs failed, since 07:32"`;
  CronJob, two runs, no facts -> `"2 runs failed, since 07:32"`;
  CronJob, one run, no facts -> `"1 run failed, since 07:32"`;
  Deployment, three open crash pods tagged `1.36`, facts `(3, nil,
  nil)` -> `"3 of 3 pods looping, tag 1.36"`; Deployment, two open oom
  pods tagged `1.36` and `1.37`, no facts -> `"2 pods out of memory,
  tags 1.36, 1.37"`; Deployment, one open image pull pod, no tag ->
  `"1 pod cannot pull the image"`; Deployment, three closed pods ->
  `"3 pods, nothing open"`.
- `theHeaderBadgeCountsOpenRowsAndTakesTheWorstTone`, traced to "one
  state badge with the open count": empty -> `("0 closed", .closed)`;
  two open one closed -> `("2 open", .open)`; two open both
  acknowledged (built with `acknowledgedAt` set; widen `row` by
  `acknowledged: Bool = false`) -> `("2 open", .acknowledged)`; one
  open acknowledged and one open not -> `("2 open", .open)`; three
  closed -> `("3 closed", .closed)`.
- `aRhythmNeedsThreeRunsAndReadsTheMedianGap`, traced to the "every 2m"
  clause: `[]` -> nil; two times -> nil; 07:32, 07:34, 07:36 ->
  `"2m"`; 07:00, 08:00, 09:00 -> `"1h"`; 07:00, 07:02, 07:30, 07:32
  (gaps 2m, 28m, 2m; median 2m) -> `"2m"`; unsorted input sorts first.
- `theSummaryLineCountsProblemsOpenRowsAndWorkloads`, traced to "the
  summary line above the list says the problems, the open incidents,
  the workloads and the newest time": `[]` -> all zero, nil; `[[cron
  job row open, cron pod row closed], [bare pod row open (workload
  "")]]` -> problems 2, openIncidents 2, workloads 1, barePods 1,
  newest the max `lastSeenAt`.

Checkpoint as task 1. Commit: `model: fold a job's incidents into runs
and word the group header`.

Consumes: `Incident`, `Timestamp`, `Category.rank`, `foldOrder`,
`podFolds`, `buckets`, `PodFold`, `Rollup`, `GroupEntry`,
`podNameSuffix`, `durationText`, `clockTime` (task 1).
Produces: `RunFold` and its derived properties, `runSuffix`,
`runFolds`, `shownRuns`, `GroupEntry.run`, `GroupFacts`, `StateTone`,
`GroupBadge`, `groupBadge`, `runCadence`, `groupSummary`,
`ListSummary`, `listSummary`.

## Task 3 - the colour rule in BadgeStyle

`macos/idios/Views/Components/BadgeStyle.swift`, one pass, no new
colour value: `red` stays and is open; `orange` stays and is
acknowledged and a degraded container; `grey` stays and is closed;
`green` stays and is live; `blue` stays only because the pod page's
owner chain still draws it (step D removes that); `yellow` and
`purple` are deleted with their last callers. The mappings:

    extension IdiosModel.Category {
        /// badge is the chip every category shares: a category is a noun, and
        /// the state beside it carries the colour.
        var badge: BadgeStyle { .neutral }
    }

    extension IncidentState {
        var badge: BadgeStyle {
            switch self {
            case .open, .attention: .red
            case .acknowledged: .orange
            case .recovered, .podDeleted, .jobFinished, .manual, .dismissed: .grey
            }
        }
    }

    extension StateTone {
        /// badge is the hue a header's badge takes for its tone.
        var badge: BadgeStyle { open -> .red, acknowledged -> .orange, closed -> .grey }
    }

`ContainerState.badge(exitCode:)`: running green, waiting orange,
terminated orange unless the exit code is a known zero, which is
neutral; the comment about a missing exit code stays ("is not evidence
of success, so only a known zero leaves the degraded colour").
`podBadge` follows through it. `TimelineEntry.badge`: container
transition through the container rule; condition True green else
orange; event Warning orange else neutral; capture neutral; rollout
neutral; lifecycle closed grey, opened red; cut neutral. `clusterDot`
is unchanged (it is already the rule). `.neutral` gains a `border` of
the same colour as its text at low alpha so the category chip draws as
an outline; `Badge` already draws a dashed border when one is set, so
`BadgeStyle` gains `borderDash: [CGFloat]?` (`[2, 2]` for `gap`, nil
for an outline) and `Badge` reads it.

`macos/idios/Views/Components/Badges.swift`: `CategoryBadge` draws the
outlined chip with a small square glyph (`RoundedRectangle` 6 by 6,
stroked in the text colour) before the label, `.help(category.tooltip)`
instead of the raw value; the raw value is what the tooltip's
parenthesis already says. `StateBadge` reads `state.label` (task 1's,
so `manual` reads resolved) and `.help(state.tooltip)`, and takes a
`width` so every state pill in a column has one width. `Badge` gains
nothing else.

`macos/idios/Views/Incidents/IncidentGrouping.swift`: `IncidentGroup`
loses `squareStyle`; `workloadGroups` stops choosing blue or grey for
a kind (the header of task 5 draws no kind square). `IncidentsList`'s
`disclosureHeader` stops reading it (task 5 replaces the header; here
the two lines are deleted so the build is green). Every other caller
of `.badge` compiles unchanged and takes the new colours:
`WorkloadDetailView`'s rollout chip and category chips, `StatusScreen`'s
category chips, the menu bar's category dot, `PodColumn`, `PodPageHeader`,
`IncidentHeader`, `Rail`, `EventsCard`, `TimelineView`, `KubeletCard`.

No model test: the colours are the one place the rule is written and a
test would compare a constant with itself. Check: `grep -n 'yellow\|purple'
macos/idios -r` finds nothing; `make app` is green.

Checkpoint as task 1. Commit: `app: colour by state and draw categories
as neutral chips`.

Consumes: `BadgeStyle` and its statics, `Badge`, `CategoryBadge`,
`StateBadge`, `IncidentState.label` and `.tooltip`, `Category.tooltip`
(task 1), `StateTone` (task 2).
Produces: the remapped `Category.badge`, `IncidentState.badge`,
`StateTone.badge`, `ContainerState.badge(exitCode:)`,
`TimelineEntry.badge`, `BadgeStyle.borderDash`, `CategoryBadge` as a
chip, `StateBadge(state:width:)`.

## Task 4 - the sidebar as a source list, the legend, the View menu

`macos/idios/App/Preferences.swift`:

    /// Density is how much a list row says without a hover: Compact is one
    /// line, Comfortable keeps the second line open.
    enum Density: String, CaseIterable, Sendable {
        case compact, comfortable
        var title: String
    }

and `Preferences.density: Density` stored under `Key.density`, default
`.compact`. `Grouping` stays where it is.

`macos/idios/App/IdiosApp.swift`: the `.commands` block gains

    CommandGroup(after: .sidebar) {
        Picker("Group by", selection: grouping) { ... Grouping.allCases ... }
        Picker("Density", selection: density) { ... Density.allCases ... }
        Divider()
    }

which lands in the system View menu; the bindings read and write
`preferences`. The Go menu is unchanged in this step (Cmd-K and Cmd-]
arrive in steps C and D).

`macos/idios/Store/IncidentsStore.swift`: `IncidentFilter.stateTitle`
is deleted; `title` reads `IncidentState.title`. The `(nil, nil)` case
stays for a route that names no state, and nothing in the sidebar sets
it any more.

`macos/idios/Views/Incidents/IncidentsSidebar.swift`, rewritten:

    /// SidebarItem is one selectable row of the source list.
    enum SidebarItem: Hashable {
        case screen(RootScreen)
        case view(IncidentState)
        case category(IdiosModel.Category)
    }

    /// IncidentsSidebar is the source list: the screens on every screen, and
    /// the incident views and categories while the list is what the window
    /// shows.
    struct IncidentsSidebar: View {
        let counts: IncidentCounts?
        let attentionWindowSeconds: Int32?
        let showFilters: Bool
        @Binding var filter: IncidentFilter
        @Binding var screen: RootScreen
        let openStatus: () -> Void

        /// views is the open half of the lifecycle in triage order; Attention
        /// leads because it is the default and it contains Open.
        static let views: [IncidentState] = [.attention, .open, .acknowledged]
        ...
    }

The body is `List(selection: selectionSet)` in `.sidebar` style. The
selection is a `Set<SidebarItem>` derived from state: the screen, plus
on the Incidents screen the active view and the active category. Its
setter finds `new.subtracting(old).first` and applies it: a screen sets
`screen`; a view sets `filter.state` and `screen = .incidents`; a
category sets `filter.category` and `screen = .incidents`. A change
that inserts nothing is ignored, so a click on the row already
selected, a cmd-click or an empty set leaves the state alone and the
derived set reasserts itself. This is the one way a List row can take
the accent while three rows are lit at once.

Sections: "Screens" with `Label(item.title, systemImage:)` tagged
`.screen`. When `showFilters`: "View", whose header is an `HStack` of the
title and a "?" button (`questionmark.circle`, `.borderless`) that
toggles `@State showLegend` and presents `LegendPopover` in a
`.popover`; rows for `views` tagged `.view(state)`, each a small circle
in `state.badge.text` (open red for Attention and Open, amber for
Acknowledged), the title, and the count trailing, then
`DisclosureGroup(isExpanded: $closedExpanded)` whose label is "Closed"
with `closedCount(counts)` trailing and whose rows are
`IncidentState.closedStates` with grey circles, tagged `.view(state)`;
the disclosure starts collapsed and opens itself when `filter.state`
is a closed state (`.onChange` and `.onAppear`). "Category": rows for
`categoryRows(counts: counts, active: filter.category).shown`, a small
neutral square, the label, the count trailing (zero drawn as "0"), the
active row's count replaced by an `xmark` glyph whose tap clears
`filter.category` (a List row cannot deselect itself); then, when
`hidden > 0`, one non-selectable row "N more with none" in `.tertiary`
with `.selectionDisabled()`. Every view and category row carries
`.help(tooltip)`. Counts read `counts?.byState[state]` and
`counts?.byCategory[category]`; a nil count draws nothing while the
first load is in flight.

`macos/idios/Views/Incidents/LegendPopover.swift`:

    /// LegendPopover is what the "?" opens: the lifecycle, the states with
    /// their tooltips and keys, and Attention with the daemon's window.
    struct LegendPopover: View {
        let attentionWindowSeconds: Int32?
        let openStatus: () -> Void
    }

Width 420. The lifecycle row is state badges joined by chevrons: open,
acknowledged, then "closed, by" with recovered, pod deleted, job
finished, resolved (`StateBadge`, so the colours come from the rule).
Under it a two-column grid: for each of open, acknowledged, recovered,
podDeleted, jobFinished, manual, dismissed the circle in its colour, the
title with `(key)` where `key` is set, and `tooltip`. Then Attention:
`IncidentState.attention.tooltip` with the "N hours" replaced by
`humanDuration(seconds:)` when the store has the status and "the
daemon's attention window (read from Status)" when it does not, and a
"Status..." button calling `openStatus`. The keys `a`, `r`, `d`
appear beside their states; nothing else in the popover is
interactive.

`macos/idios/Views/Incidents/IncidentsScreen.swift`: the sidebar call
becomes

    IncidentsSidebar(
        counts: incidents.counts, attentionWindowSeconds: status.status?.attentionWindowSeconds,
        showFilters: screen == .incidents && path.isEmpty,
        filter: sidebarFilter, screen: sidebarScreen, openStatus: { show(.status) })

with `@Environment(StatusStore.self) private var status` (the app
already injects it). `columnVisibility`, `visibilityBeforePage`, the
`onChange(of: path)` block, the `isPageRoute` extension and the init's
collapse lines are deleted: the sidebar stays and shows Screens alone
under a pushed page. `filterChips` and `chip` are deleted (the active
sidebar row is where the filter shows), and the toolbar becomes the
cluster menu, the Group menu, a View menu button (density, same Picker
as the menu bar item) and the filter field. The hidden `Button("Filter")
.keyboardShortcut("k")` goes; "/" reaches the filter through task 5's
`onKeyPress`, so `IncidentsList` gains a `focusFilter: () -> Void`
parameter here that sets `filterFocused = true`.

No model test: the sidebar's logic is task 1's functions. Check: run
the application against the fixture daemon; the sidebar shows every
count including zero, "N more with none", the accent on Attention;
pick Pod deleted under Closed and the disclosure stays open; open a
pod page and only Screens remains; the "?" opens the legend saying
"24 h"; the View menu holds Group by and Density.

Checkpoint as task 1. Commit: `app: make the sidebar a source list with
a legend`.

Consumes: `RootScreen`, `IncidentFilter`, `IncidentCounts`,
`StatusStore.status`, `DaemonStatus.attentionWindowSeconds`,
`Preferences`, `Grouping`, `Navigator`; task 1's `IncidentState.title`,
`.key`, `.tooltip`, `closedStates`, `closedCount`, `categoryRows`,
`humanDuration`, `Category.tooltip`; task 3's `StateBadge`, `.badge`.
Produces: `SidebarItem`, `IncidentsSidebar(counts:attentionWindowSeconds:
showFilters:filter:screen:openStatus:)`, `LegendPopover`, `Density`,
`Preferences.density`, the View menu items, `IncidentsScreen` without
the column collapse and the chips.

## Task 5 - the list: groups, runs, one-line rows, the summary line, the keys

`macos/idios/Store/GroupFactsStore.swift`:

    /// GroupFactsStore holds what a group header cannot read off its rows: the
    /// live pods of every workload in scope, and the run totals of every
    /// CronJob or Job that has a group on screen.
    @Observable @MainActor
    final class GroupFactsStore {
        private(set) var livePods: [WorkloadKey: Int32] = [:]
        private(set) var runs: [WorkloadKey: (total: Int32, failed: Int32)] = [:]

        /// facts is the header's view of one workload, nil when nothing is known yet.
        func facts(for key: WorkloadKey) -> GroupFacts?

        /// loadWorkloads reads the workload rows of the scope once per watch.
        func loadWorkloads(connection: DaemonConnection, scope: ClusterScope) async

        /// loadRuns reads one jobs page per key, limit 1, for the totals it carries.
        func loadRuns(keys: Set<WorkloadKey>, connection: DaemonConnection) async
    }

`loadWorkloads` is the `ListWorkloads` call `WorkloadsStore.load`
makes, keyed by `(clusterID, namespace, workloadKind, workloadName)`
(`WorkloadKey.cluster` holds the cluster id here, as `WorkloadsStore`
uses it). `loadRuns` calls `ListJobs(cluster_ids: [key.cluster],
namespace:, limit: 1, cronjob_name: key.name)` for a CronJob key and
`ListJobs(... name: key.name)` for a Job key if the query has a job
name filter, else skips Job keys (a Job group has one run and its
header, when drawn, says "1 run failed"); it keeps `total` and
`failedTotal` of the page. Errors go through the same `report` shape
every store has; a cancelled call is ignored. `IncidentsScreen` owns
one, runs `loadWorkloads` in the incidents watch task, and runs
`loadRuns` in a `.task(id:)` keyed by the set of CronJob and Job keys
among `incidents.visibleRows` plus `incidents.rows.count`, so a
streamed run refreshes its total.

`macos/idios/Views/Incidents/IncidentGrouping.swift`: `IncidentGroup`
gains `let factsKey: WorkloadKey?` (set for workload-mode groups with a
workload name), `let summary: String` and `let badge: GroupBadge`, and
`incidentGroups` takes `facts: (WorkloadKey) -> GroupFacts?`; `group(...)`
computes `summary` with `groupSummary(kind: first.workloadKind, rows:,
facts:)` for workload-mode groups and `"<N> incidents, <open> open"`
for the namespace, category, cluster and time groups, and `badge` with
`groupBadge(rows)`. `headerless` is a computed property: true when the
group is a workload-mode group whose `entries.count == 1` and whose
kind is not `"CronJob"`, so a bare pod, a Job with one run and a
single-pod workload draw their one row without a header, as frame 1a
does, while a CronJob always has a header because its "+ N more runs"
line belongs under one. The `workloadTitle` fallback for a bare pod
stays.

`macos/idios/Views/Incidents/ListColumns.swift`:

    /// ListColumns is the one set of widths the column header, the group
    /// headers and every row share, so the columns line up.
    enum ListColumns {
        static let disclosure: CGFloat = 14
        static let dot: CGFloat = 10
        static let category: CGFloat = 96
        static let container: CGFloat = 84
        static let reason: CGFloat = 200
        static let restarts: CGFloat = 56
        static let age: CGFloat = 64
        static let state: CGFloat = 100
        static let gap: CGFloat = 10
    }

    /// ColumnHeaderRow names the columns once above the list.
    struct ColumnHeaderRow: View

The subject column is the flexible one. Every row below is an `HStack`
of these widths with `.frame(width:alignment:)` per cell; Restarts is
right-aligned and monospaced digits, Age is monospaced digits and never
wraps (`lineLimit(1)`, fixed width), the state badge takes
`ListColumns.state`.

`macos/idios/Views/Incidents/GroupHeaderView.swift`:

    /// GroupHeaderView is the one header shape: chevron, state dot, worst open
    /// category chip, kind and name with the summary, the containers, the
    /// reasons, "-" for restarts, the age, one state badge.
    struct GroupHeaderView: View {
        let group: IncidentGroup
        let collapsed: Bool
        let now: Date
        let toggle: () -> Void
    }

The dot takes `group.badge.tone.badge.text`; the chip is
`CategoryBadge(category: group.worstCategory)` when set; kind and name
are `Text(kind).semibold + Text(name)` with `group.summary` after them
in `.secondary` (an `HStack`, never a concatenated multi-font Text
with `fixedSize`); the container cell lists the distinct
`scopeLabel(containerName:subjectKind:)` values joined by " + "; the
reason cell the distinct `lastReason` values joined by ", "; Restarts
is "-" with `.help(restartsTooltip)` because a header spans rows and
no view sums occurrences; Age is `durationText` from the oldest open
`openedAt`, or from the newest `closedAt` when nothing is open, with
the raw timestamp on hover; the badge is `Badge(text: group.badge.text,
style: group.badge.tone.badge, width: ListColumns.state)`. The
background is a faint tint (`.quaternary.opacity(0.35)`) with the
list's inset; the header is a selectable row tagged `group.id`, so the
arrow keys reach it and the keys act on every incident under it. The
chevron toggles on click; Space and Return on the selected header
toggle too (task 5's key handlers).

`macos/idios/Views/Incidents/RunRowView.swift`:

    /// RunRowView is one run of a CronJob or Job: the job_failed row and the
    /// pod rows of one Job as one line.
    struct RunRowView: View {
        let run: RunFold
        let kind: String
        let workloadName: String
        let now: Date
        let expanded: Bool
        let density: Density
        let toggle: () -> Void
    }

Cells: chevron (`FoldDisclosure`, since a run expands to its rows),
the dot by `groupBadge(run.rows).tone`, `CategoryBadge` of
`run.categories.first`, the subject: under a CronJob `Text("run")
.secondary` then the suffix in monospace (or `middleElided(run.jobUID,
keeping: 12)` with the uid on hover when `suffix` is nil) then
"- N pods" (singular agrees; omitted at zero); for a Job `Text("Job")`,
the name, then "- pod <suffix>" for one pod or "- N pods"; the container
cell the distinct scope labels joined by ", " ("job, app"); the reason
cell `run.reasons` joined by ", " plus " - exit N" for `run.exitCodes`;
Restarts is the lead pod row's `occurrences` or "-" when the lead is the
job row; Age from the oldest open `openedAt` or the newest close; the
badge `groupBadge(run.rows)`. The second line (Comfortable or hover):
the namespace, every category label when more than one, the pod names'
suffixes, the tags, a deletion. Closed runs (`openCount == 0`) draw at
opacity 0.6. Expanded, the run's rows follow as `IncidentRowView`s with
`indented: true` in fold order, the job row last.

`macos/idios/Views/Incidents/IncidentRowView.swift`, rewritten on the
columns: chevron slot (the pod fold's `FoldDisclosure` when the row
leads siblings, else empty), the dot in `incident.state.badge.text`,
`CategoryBadge`, the subject (`Text(kind) .secondary` then the name for
a lead row: a bare pod reads "Pod <name>"; a pod under a workload
reads the workload name then the pod suffix in monospace after a
middle dot; a job-subject row reads "Job" and the workload name; a
sibling row reads its container in place of the subject as today), the
container cell (`ContainerChip` with `.help(jobContainerTooltip)` for a
job-subject row), the reason cell (`lastReason` + `exitText`), Restarts
(`"\(occurrences)"`, or "-" for a job subject, `.help(restartsTooltip)`),
Age (`durationText` from `closedAt` when closed, else from `openedAt`,
the raw timestamp and field on hover), `StateBadge(state:, width:
ListColumns.state)`. The second line is `leadLine` as it exists
(namespace, pod, exit, deletion, tag, acknowledged) and shows when
`density == .comfortable` or the row is hovered (`@State hovering` with
`.onHover`); a sibling's line is `siblingLine` under the same rule. The
"+N on this pod" pill stays in the subject cell. Closed rows draw at
opacity 0.6; the `acknowledged` weight rule stays. `ColumnLabel` is
deleted: the word open or closed lives in the badge now.

`macos/idios/Views/Incidents/RollupRowView.swift`: the same columns:
chevron, dot by `groupBadge(rollup.rows).tone`, `CategoryBadge`, the
subject "N pods of <workload>" with the incidents pill, the container
chip, the reasons plus exit codes, "-" for Restarts, the age as today,
the badge `groupBadge(rollup.rows)` at the column width; the second
line under the density and hover rule.

`macos/idios/Views/Incidents/IncidentsList.swift`:

    /// ListExpansion is what a person opened or folded, by id, for as long as
    /// the window lives and across a grouping change.
    struct ListExpansion: Hashable {
        var collapsedGroups: Set<String> = []
        var expandedPods: Set<String> = []
        var expandedRollups: Set<String> = []
        var expandedRuns: Set<String> = []
    }

lives in `IncidentsScreen` as `@State private var expansion` and reaches
the list as a `Binding`; the list's own `expandedPods` and
`expandedRollups` states are deleted. `IncidentsList` gains `summary:
ListSummary`, `density: Density`, `focusFilter: () -> Void`, `note:
([String], String) -> Void`, `acknowledgeAll: () -> Void`, `openRuns:
(Incident) -> Void`, and `acknowledge`, `dismiss`, `resolve` keep their
per-id shape.

Above the `List`, the summary line: `"<problems> problems"` bold, then
`"<open> open incidents"`, `"<workloads> workloads, <bare> bare pods"`,
`"newest <clockTime>"` in `.secondary`, and at the right two buttons:
"Collapse all" (which reads "Expand all" when every group with a header
is collapsed) and "Acknowledge all" (disabled when no visible row is
open and unacknowledged). Acknowledge all asks first, "Acknowledge N
incidents?", naming the count, because it acts on the whole view;
confirmed, it calls `acknowledgeAll`, which `IncidentsScreen` runs over
`incidents.visibleRows.filter { $0.closedAt == nil && $0.acknowledgedAt
== nil }`. Under the summary line, `ColumnHeaderRow`.

The `List` body: for each cluster bucket, a cluster header only while
`selectedClusters > 1`, drawn with `GroupHeaderView` over a synthetic
group (kind "" and the cluster name as title, `"<N> incidents, <open>
open"` as summary), and namespace groups the same way; for each group,
`GroupHeaderView` unless `group.headerless`, then, unless collapsed, the
entries: `shownRuns(group.entries, limit: 5)` for the run cut, each
`.run` a `RunRowView` tagged `run.id` with its rows under it while
expanded, each `.rollup` a `RollupRowView` as today, each `.fold` the
`foldRows` as today; after the entries, when `more > 0`, one
non-selectable line "+ N more runs - open the run history in Workloads"
as a `Button(.plain)` in the accent colour calling `openRuns(lead row)`.
Pinned `Section` headers go: the header is a row in the list, so
scrolling never pins a header over rows of another group. Every row
tag is distinct: group ids, run ids (`run/<job uid>`), rollup ids and
incident ids never collide.

Keys, all through `.onKeyPress` on the list, none while the filter
field has focus: `.return` opens the selected incident, toggles a
selected group, run or rollup; `.space` toggles a selected group, run,
rollup or lead fold and is `.ignored` on a plain row; `"/"` calls
`focusFilter`; `"a"`, `"d"`, `"r"`, `"n"` act on the selected incident
or on every incident under the selected group, run or rollup:
`acknowledge` at once (reversible), `dismiss` and `resolve` behind the
confirmation `PendingRollupAction` already implements, generalised to
`PendingBulkAction(rows: [Incident], action:)` whose title names the
count and the workload ("Dismiss 12 incidents of report?"), and `n`
opening `NoteSheet(note:)` (prefilled with the single incident's note,
empty for a set) whose save calls `note(ids, text)`. Command-Delete
stays per incident. `IncidentsScreen` implements `note` with
`writes.setNote` (the `deletes` store renamed `writes`, since it now
writes more than a delete) over each id; the stream brings the rows
back. `openRuns` is `openWorkloadRuns(_ row: Incident)`, the twin of
`openWorkloadPods` with `workloadsTree.tab = .runs` and the row's
cluster, namespace, kind and workload name.

Empty views. `empty` teaches by filter: title `"No <state title
lowercased> incidents."` (`"Nothing needs attention."` for Attention;
with a category, `"No <category label> incidents in <state title>."`);
body `state.tooltip` (plus `category.tooltip` on its own line when a
category is set) and, when `state.key` is set, "Press <key> on a row to
put it here."; then `Button("Show Attention")` setting the filter to
`IncidentFilter(state: .attention)` unless the view is Attention. The
no-cluster and no-match texts stay as they are, and "Clear filter"
stays.

`macos/idios/Views/Incidents/IncidentsScreen.swift`: owns `expansion`,
`facts = GroupFactsStore()`, passes `incidentGroups(rows:grouping:
cluster:selectedClusters:facts: facts.facts(for:))`, `listSummary(groups:
...)` over the groups' rows, `preferences.density`, `focusFilter`,
`note`, `acknowledgeAll`, `openRuns`. `selectedIncident` is renamed
`selection` since a group, run or rollup id can sit in it.

Check against the fixture daemon and then the smoke store on 7771
(`make smoke PORT=7771`, then `kubectl -n idios-smoke apply -f
hack/smoke/` to reopen the incidents): the CronJob is one header with
"every 2m, N of M runs failed, since HH:MM", five run rows and a
"+ N more runs" line that opens Workloads on Runs; the Deployment is
one header over one rollup; the bare pods are single rows without a
header; every age is one line; the state pills share a width; a hover
shows the second line and Comfortable keeps it; Collapse all folds
every header; `a` on the CronJob header acknowledges every incident
under it and the badge turns amber; Acknowledged and Recovered show
their teaching text and the Show Attention button. Screenshots for the
user's review: `IDIOS_DAEMON=127.0.0.1:7771 hack/macos/screenshot.sh
incidents <png>` and `incidents/acknowledged <png>`, after
`screencapture -x /tmp/idios-probe.png` succeeds.

Two commits. First, after the grouping, the facts store, the header,
the run row, the incident row and the rollup row build and the list
draws them: `app: draw the list as groups, runs and one-line rows`.
Second, after the summary line, the empty views and the keys: `app:
add the summary line, the teaching empty views and the list keys`.
Checkpoint before each as task 1.

Consumes: `groupEntries`, `GroupEntry.run`, `RunFold`, `shownRuns`,
`groupSummary`, `groupBadge`, `GroupFacts`, `listSummary`, `ListSummary`
(task 2); `scopeLabel`, `clockTime`, `restartsTooltip`,
`jobContainerTooltip`, `IncidentState.tooltip`, `.key`, `.title`
(task 1); `CategoryBadge`, `StateBadge(state:width:)`, `StateTone.badge`
(task 3); `Density`, `IncidentsSidebar` (task 4); `WorkloadKey`,
`WorkloadsStore.load`'s `ListWorkloads` call shape, `ListJobs`'s query
of `WorkloadsStore.loadRuns`, `IncidentWriteStore.setNote`, `NoteSheet`,
`FoldDisclosure`, `ContainerChip`, `CountPill`, `RowSeparator`, `Flow`,
`MiddleElidedText`, `durationText`, `middleElided`, `WorkloadsTreeState.tab`.
Produces: `GroupFactsStore`, `ListColumns`, `ColumnHeaderRow`,
`GroupHeaderView`, `RunRowView`, `IncidentRowView` on the columns,
`RollupRowView` on the columns, `ListExpansion`, `PendingBulkAction`,
`IncidentsList` with the summary line, the empty views and the keys,
`IncidentGroup.summary`, `.badge`, `.factsKey`, `.headerless`,
`IncidentsScreen.openWorkloadRuns`.

## Task 6 - verification with the user and the status line

The user runs the application against the smoke store on 7771 and
reads the list, the sidebar, the legend, the empty views, the hover
line and the Comfortable density, the keys on a header, a run and a
row, and the colours in light and dark. Anything the review turns up
is fixed in the task that owns it and its commit amended only by a new
commit, never by rewriting one. Then `docs/plans/m10-reading/roadmap.md`'s
step B line becomes "complete <date>" and the step C line stays "not
started". Commit: `docs: close m10 step B`. The 7771 daemon is stopped.

## Hands to the next step

Step C (`search.md`) reads: `IncidentsScreen`'s toolbar, where the
filter field now sits without a Cmd-K equivalent and a Search button
with `Cmd-K` goes; `IncidentsSidebar.views` and
`IncidentState.closedStates`, which are the views the palette's
"Show <view>" commands name; `IncidentState.title` and `.key` for the
command list; `RunFold` and `runSuffix` for the Runs section's names;
`ListSummary` for nothing (the palette has its own footer);
`IncidentsScreen.openWorkloadRuns` and `openWorkloadPods` as the two
ways a palette result opens Workloads; `GroupFactsStore` as the store
the palette does not need (its matching runs over `IncidentsStore.rows`,
`WorkloadsStore.workloads` and the pods it lists itself). Step D reads
`StateTone` and the remapped `ContainerState.badge(exitCode:)` for the
run strip and the tree dots, `BadgeStyle.blue` as the one colour it
retires with the owner chain, and `humanDuration` for the Status screen.
Step E reads `groupEntries`, `groupSummary` and `groupBadge` for the
menu bar's rows.

## Self-review

Spec coverage. Decision 1: task 2 (`runFolds`, `GroupEntry.run`,
`shownRuns`, `groupSummary`, `groupBadge`, the rollup kept with the
sentence changed in `groupEntries`'s comment), task 5 (the header
shape, the run rows, the "+ N more runs" line opening Workloads on
Runs, the cluster and namespace headers as the same shape only while
more than one is in scope, `ListExpansion` above the list so it
survives a grouping change, every count a count of incidents).
Decision 2: task 3 (the one pass over `BadgeStyle.swift`, categories
neutral, kinds without a square in the list header, `TimelineEntry`
included because it lives in that file; the owner chain's blue stays
for step D as the roadmap's step D paragraph owns the rail). Decision
3: task 1 (`title`, `label`, `tooltip`, `key`), task 4 (the legend
reading `attentionWindowSeconds`, the "?" beside View, the tooltips on
every sidebar row), task 5 (Restarts and "-", the "job" container
value, the tooltips on badges and cells, the teaching empty views).
Decision 4: task 5 (`ListColumns`, one line, fixed Restarts, Age and
State, the hover and Comfortable second line, closed rows dimmed), task
4 (`Density` in the View menu). Decision 5: task 1 (`closedStates`,
`closedCount`, `categoryRows`), task 4 (the source list, the Closed
disclosure, zero counts kept, "N more with none", the accent through
the List selection, View and Category only on Incidents, Screens under
a pushed page). Decision 10: every name in this plan and its tests is a
fixture's or invented (`report`, `nightly`, `retry`, `checkout-api`).

Rulings this plan makes inside the decisions. A header is drawn only
when the group holds two or more entries or is a CronJob, as frame 1a
draws the bare pods and the Job without one; the roadmap's "every group
header has one shape" is about the shape, and a header over one row
would say the row twice. The header's Restarts cell is "-", because the
frame's 122 is a sum of occurrences and "every count is a count of
incidents; no view sums occurrences" is the rule the milestone keeps;
the summary line and the badge count incidents. The CronJob sentence
says "N runs failed" when the jobs page is not in hand and "62 of 63"
when it is: the second needs `GET /jobs`, which the application already
calls, and is why `GroupFactsStore` exists; a Job group says "1 run
failed" without a call. The cadence needs three runs and reads the
median gap, so one slow run does not turn "every 2m" into "every 15m".
"Acknowledge all" asks first with the count even though `a` on a rollup
acts at once, because it is the whole view, not one line. A run row's
category cell shows the worst category and the hover line lists the
others, where frame 1a leaves the cell empty; the sentence says the row
names "the categories found among them". The `n` key on a group writes
the same note to every incident under it, as the sentence gives `n` the
same scope as `a`. The Closed disclosure is a heading and not a view:
the wire has no "closed" filter and inventing one client-side would
make the count and the list disagree. The filter's `(nil, nil)` title
stays reachable only by a route.

`.ai` rules: ASCII throughout; comments say why (the derived-set
sidebar, the header rule, the median cadence, the "-" restarts, the
Closed heading each carry their reason once); code is the truth (no
task, section or plan is named in code; the vocabulary sentences are
the strings themselves); scope (no proto change, the Go menu untouched
until C and D, the pod page's rail untouched until D, `blue` kept for
it, the menu bar untouched until E; `clockTime` moves because task 2
needs it and nothing else is moved); tests (every test traces to a 9.3,
9.5 or 9.6 sentence, tables with whole-value rows, edge cases first:
empty rows, a job row with no pod, a pod without a job uid, a zero
count, nil counts, unsorted times; no test of a constant: the tooltips
and colours have none); commits (six subjects in the `area: imperative`
form, one logical change each, after the user's review).

Type consistency: `GroupEntry.run(RunFold)` is what `shownRuns` cuts
and `IncidentsList` switches on; `GroupFacts(livePods:runsTotal:
runsFailed:)` is what `GroupFactsStore.facts(for:)` returns and
`groupSummary(kind:rows:facts:)` takes; `GroupBadge.tone` is a
`StateTone` and `StateTone.badge` is a `BadgeStyle`; `IncidentState.title`
replaces `IncidentFilter.stateTitle` in the one caller that remains
(`IncidentFilter.title`); `scopeLabel` keeps its signature and changes
two return values; `clockTime(_:seconds:)` keeps its signature across
the move; `WorkloadKey.cluster` carries the cluster id as
`WorkloadsStore` already uses it.
