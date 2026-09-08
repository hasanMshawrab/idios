# m10 step D - explore: the workloads tree, the runs, and the pod page by subtraction

Goal: after this plan a person looks around. The workloads tree says how
many of each kind there are and never loses its collapse state to a
filter; the pods no controller owns are one row per name; every row has
a context menu. A CronJob opens on its runs with a strip that draws one
cell per run, oldest first, coloured by outcome and dashed where the
sweeper cut the record; the Runs table opens on the failures, names a
run by its suffix, links it to its pod and its incident and folds the
consecutive runs that failed the same way. The pod page loses what it
repeats and gains one answer at the top: a verdict block built from the
fields the page already receives, one primary Acknowledge with its key,
a scrubber over one log body instead of a grid of chips, a breadcrumb
that opens Workloads, a Forward beside Back, a folded timeline and a
header tag that stops flickering. No proto change: every field the
verdict names is already decoded.

Architecture: what can be decided without a view is decided in
`IdiosModel` as a pure function with a table test - the run cells and
their outcomes, the hour-by-minute matrix, the table's fold, the merge
of the bare pods, the tree's captions, the verdict's sentences, the
timeline's fold, the readiness count behind the header tag and the one
plural helper. The application draws them: `WorkloadsScreen` gains a
pane header, plural captions, merged bare-pod rows and a context menu
backed by one new write path on `WorkloadsStore`; `WorkloadDetailView`
gains the strip, the defaults, the links and the chart's axis;
`PodPageScreen` and its panes lose their repeats and gain the verdict,
the actions, the scrubber, the breadcrumb and Forward, which is a
`path` concern of `IncidentsScreen` plus one serial on `Navigator`.
Views never import `IdiosAPI`; stores own every client call.

Tech stack: Swift 6 package `macos/Sources/IdiosModel` with tests in
`macos/Tests/IdiosModelTests` (`make app-test`); the Xcode application
under `macos/idios` (`make app`; a file under `macos/idios/` joins the
target by existing). SwiftUI for every view: `DisclosureGroup` for the
tree, `.contextMenu` for the row menus, a `Canvas`-free `HStack` of
`RoundedRectangle`s for the strip with `DragGesture` for the selection,
`.help` for every hover, `Menu` for Actions, `.onKeyPress` for the
single letters and `CommandMenu("Go")` for Cmd-]. The daemon is
unchanged; `go build ./... && go test ./...` runs only to prove that.

Spec: `docs/design/presentation.md` section 9.3, the Workloads row
whole, and the Pod page row from "The header leads with a verdict
block" to "the message wraps"; 7.4 (the four treatments, and "a card's
provenance -- the table and predicate its values trace to -- is the card
title's hover, never visible text"); 9.4 (the Go menu holds Cmd-[ Back
and Cmd-] Forward; single letters go through `.onKeyPress`); 9.5 (the
colour rule, which names the run strip and the pod header tag); 4.5
(`GET /v1/jobs` and its `total` and `failed_total`). Roadmap decisions
7, 8 and 10 of `docs/plans/m10-reading/roadmap.md`. Visual reference:
page 5 (frames 5a, 5b, 5c) and page 2 (frame 2a) of
`docs/mockups/idios-ui.html`.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Swift follows the Go rules: one-line doc comment per
public type and function, comments say why, tests trace to a spec
statement with whole-value assertions, variants are table rows. Views
never import `IdiosAPI`; stores own every client call; a cancelled call
maps to `APIError.cancelled` and every store ignores it. The only
hard-coded colours live in `BadgeStyle.swift`: the run strip's cells
take the tones already there - `.green` for a Complete run, `.red` for
a failed run with an unacknowledged open incident, `.orange` for one
whose open incidents are all acknowledged, `.grey` for a failed run
with nothing open, `.neutral` for a run still going and `.gap` (the
dashed border) for a run whose record the sweeper removed. Nothing new
is added to that file but the one mapping function. Every count is a
count of incidents; the run strip's cells count runs and say so in
words. A bare-letter key goes through `.onKeyPress` on the focused
view; Cmd-] is a menu key equivalent in the Go menu beside Back,
because a menu is where a person finds it. No proto change, so no
`make generate` and no `make generate-check`; nothing under
`internal/apigen`, `api/openapi` or `api/proto` is touched.

Nothing in a test, comment, doc or fixture names a real organisation,
cluster, namespace, workload, image or node. The invented names this
plan and its tests use are `checkout-api`, `report`, `nightly`,
`worker`, `idios-smoke`, `api` and `node-a`.

The application takes the daemon as a launch argument: the Debug build
is started with `nohup macos/DerivedData/Build/Products/Debug/idios.app
/Contents/MacOS/idios -daemon 127.0.0.1:7771 &` after `make app`
(`IDIOS_DAEMON` is read only by `hack/macos/screenshot.sh`). That build
shares its bundle id and its `UserDefaults` with the installed
application: never `defaults write` that domain; override a preference
for one run with an argument-domain flag (`-grouping workload
-density comfortable`, `-search <query>`). The production daemon on
127.0.0.1:7770 is never touched, read or screenshotted; every check
runs against a private daemon on 7771. Before any screenshot run
`screencapture -x /tmp/idios-probe.png` and stop if it fails; capture
through a scratch script that starts the application with
`-screenshot <png> -route <route> -daemon 127.0.0.1:7771`, waits for
"idios-window <n>" on stdout, sleeps 2.5 s, runs `screencapture -x -o
-l <n> <png>` and kills the application.

The mock daemon ignores every list filter and answers with the whole
fixture set, and its job incident carries no job uid, so run folds and
the run strip show only against the smoke store: `.storage/smoke`,
refilled with `make smoke PORT=7771`, its incidents reopened with
`KUBECONFIG=./kube/config kubectl -n idios-smoke apply -f hack/smoke/`.
Its failing CronJob has hundreds of runs (456 closed incidents were
seen there), which is what the strip's 200-run cap and the
hour-by-minute matrix are checked against.

`SwiftUI` `List(selection:)` on macOS does not redraw from a `Set`
binding whose getter disagrees with a click, which is why the sidebar
draws its own accent per row; the run strip's drag selection follows
that lesson and lights each selected cell itself rather than through a
selection binding.

Checkpoint before every commit: `go build ./... && go test ./... &&
make ascii && make app-test && make app`. Implementers never commit;
nothing is committed without the user's review of the diff. The
decisions are made; where a frame and a sentence differ, the sentence
of 9.3 wins and the doubt goes in the self-review.

## File structure

    macos/Sources/IdiosModel/RunStrip.swift            RunOutcome, RunCell, runStripLimit, runCells, RunMatrixRow, RunMatrix, runMatrix, RunTableRow, runTableRows (task 1)
    macos/Sources/IdiosModel/Tree.swift                BarePodRow, barePodRows, kindCaption (task 1)
    macos/Tests/IdiosModelTests/RunStripTests.swift    the outcomes, the order, the matrix, the table fold (task 1)
    macos/Tests/IdiosModelTests/TreeTests.swift        the bare-pod merge, the captions (task 1)
    macos/Sources/IdiosModel/Verdict.swift             verdictSentences (task 2)
    macos/Sources/IdiosModel/TimelineFold.swift        TimelineFold, timelineFolds (task 2)
    macos/Sources/IdiosModel/PodPage.swift             podStateTag gains flips; readinessFlips (task 2)
    macos/Sources/IdiosModel/Display.swift             plural (task 2)
    macos/Tests/IdiosModelTests/VerdictTests.swift     the four sentences and what drops them (task 2)
    macos/Tests/IdiosModelTests/TimelineTests.swift    the cycle fold, the spine (task 2)
    macos/Tests/IdiosModelTests/PodPageTests.swift     the looping tag, the readiness count (task 2)
    macos/Tests/IdiosModelTests/DisplayTests.swift     plural (task 2)
    macos/idios/Views/Components/BadgeStyle.swift      runCellStyle (task 3)
    macos/idios/Views/Workloads/WorkloadsScreen.swift  the pane header, the captions, the bare-pod rows, the chooser, the context menu (task 3)
    macos/idios/Store/WorkloadsStore.swift             writes, openIncidents(of:), acknowledgeOpen (task 3)
    macos/idios/Views/Workloads/RunStripView.swift     RunStripView, RunMatrixView, the drag selection (task 4)
    macos/idios/Views/Workloads/WorkloadDetailView.swift  the Runs default and links, the Pods default, the window, the chart (task 4)
    macos/idios/Views/IncidentDetail/VerdictBlock.swift   VerdictBlock (task 5)
    macos/idios/Views/PodPage/ContainerPane.swift      PaneActionsRow rebuilt, the segmented rule, the verdict (task 5)
    macos/idios/Views/PodPage/PodCardPane.swift        the segmented rule, the verdict (task 5)
    macos/idios/Views/IncidentDetail/KubeletCard.swift the footer to the title's hover, the note removed (task 5)
    macos/idios/Views/IncidentDetail/JobCard.swift     the footer to the title's hover (task 5)
    macos/idios/Views/IncidentDetail/CapturedLogsCard.swift  the scrubber (task 6)
    macos/idios/Views/IncidentDetail/TimelineView.swift      the fold, the one time (task 6)
    macos/idios/Views/IncidentDetail/EventsCard.swift        the time column (task 6)
    macos/idios/Views/PodPage/PodPageHeader.swift      the breadcrumb, the debounced tag (task 7)
    macos/idios/Views/PodPage/PodPageRail.swift        CONTEXT loses its repeats (task 7)
    macos/idios/App/Route.swift                        Navigator.forwardSerial, goForward (task 7)
    macos/idios/App/IdiosApp.swift                     the Go menu's Forward, Cmd-] (task 7)
    macos/idios/Views/Incidents/IncidentsScreen.swift  the forward stack, the breadcrumb opener (task 7)
    docs/plans/m10-reading/roadmap.md                  step D's status line (task 8)

## Task 1 - the runs and the bare pods in IdiosModel

`macos/Sources/IdiosModel/RunStrip.swift`.

    /// RunOutcome is what one cell of the run strip says happened to a run.
    public enum RunOutcome: Hashable, Sendable {
        /// complete is a run whose condition is Complete.
        case complete
        /// failed carries the tone of the incidents the run opened.
        case failed(StateTone)
        /// running is a run with no condition yet.
        case running
        /// swept is a run known only from an incident's job uid, because the
        /// sweeper removed the jobs row.
        case swept
    }

    /// RunCell is one run as the strip and the table both read it: what it did,
    /// what a hover says, what a click opens and what a drag acknowledges.
    public struct RunCell: Identifiable, Hashable, Sendable {
        public let id: String
        public let jobUID: String
        /// name is the run's suffix after "<cronjob>-", or the whole job name
        /// when it does not carry that prefix.
        public let name: String
        public let outcome: RunOutcome
        public let startedAt: Timestamp?
        public let finishedAt: Timestamp?
        public let condition: String
        public let reason: String?
        public let exitCode: Int32?
        public let attempts: Int32
        public let backoffLimit: Int32
        public let incidentIDs: [String]
        public let podUID: String?
        public let podName: String?
    }

    extension RunCell {
        /// openIncidentIDs is what a drag over this cell acknowledges.
        public var openIncidentIDs: [String]
        /// hover is the one line the strip shows on a cell: the run, its
        /// condition, how long it ran and how it exited.
        public func hover(now: Date) -> String
    }

    /// runStripLimit is how many runs the strip draws one cell each before it
    /// becomes an hour-by-minute matrix.
    public let runStripLimit = 200

    /// runCells reads a page of runs and the incidents of the same workload into
    /// one cell per run, oldest first, adding a cell for every run that only an
    /// incident's job uid still names.
    public func runCells(jobs: [Job], incidents: [Incident], cronjobName: String) -> [RunCell]

    /// RunMatrixRow is one hour of the matrix: sixty minutes, each holding the
    /// run that started in it.
    public struct RunMatrixRow: Identifiable, Hashable, Sendable {
        public let id: String
        public let hour: Timestamp
        public let minutes: [RunCell?]
    }

    /// RunMatrix is the strip past its limit: one row per hour of the window,
    /// oldest first, with no hour left out.
    public struct RunMatrix: Hashable, Sendable {
        public let rows: [RunMatrixRow]
    }

    /// runMatrix buckets the cells by the hour and minute they started, because
    /// past two hundred runs a line of cells is thinner than a hair.
    public func runMatrix(_ cells: [RunCell]) -> RunMatrix

    /// RunTableRow is one line of the Runs table: a run, and the consecutive
    /// runs after it that failed for the same reason, which it stands for.
    public struct RunTableRow: Identifiable, Hashable, Sendable {
        public let id: String
        public let run: RunCell
        public let folded: [RunCell]
    }

    /// runTableRows reads the cells newest first and folds each stretch of
    /// consecutive runs that share a reason into the first of them, because
    /// fifty rows of one sentence answer nothing the first row did not.
    public func runTableRows(_ cells: [RunCell]) -> [RunTableRow]

Rules.

`runCells` gathers the incidents by `jobUID` (rows without one are
ignored: a run is a Job). A `Job` row yields a cell keyed on its uid; a
`jobUID` among the incidents with no `Job` row yields a cell with
`outcome .swept`, `condition "not recorded"`, `name` the run's suffix
read from a pod name through `runSuffix(podName:workloadName:
workloadKind:)` with kind `"CronJob"` and the cronjob's name, else the
uid middle-elided to 12. `name` for a `Job` row is the part of
`job.name` after `cronjobName + "-"` when it carries that prefix, else
`job.name` whole. `outcome` for a `Job` row: `.complete` when
`job.complete` and no incident names the run; `.running` when
`job.conditionType` is nil and no incident names it; otherwise
`.failed(groupBadge(rows).tone)`, which is the one place the tone is
decided and is the same function the list's headers use. `condition` is
`"Complete"`, `"Failed"`, `"running"` or `"not recorded"`; `reason` is
`job.conditionReason`; `exitCode` is the first exit code among the
run's incidents; `attempts` is `job.failed` and `backoffLimit` is
`job.backoffLimit`; `podUID` and `podName` come from the newest
incident of the run that has a pod (`min(by: foldOrder)` over the rows
with a `podUID`), so a click opens the pod that failed. `incidentIDs`
is every row's id in served order. Order is by `startedAt.raw`
ascending, oldest first, a cell with no `startedAt` last, ties by
`jobUID`, so the strip reads left to right as time.

`hover(now:)` is `"<name> - <condition>[ (<reason>)] - ran
<durationText> - exit <code>"`, each part dropped when it is not known,
with `"still going"` in place of the duration when `finishedAt` is nil
and the outcome is `.running`.

`runMatrix` reads the hour of each cell from the first thirteen
characters of `startedAt.raw` (the fixed-width UTC layout the daemon
writes, so the slice is the hour) and the minute from the two after the
colon. Every hour between the first and the last is present, empty ones
included, because a gap in the schedule is the point of the picture;
two runs in one minute keep the later one and the earlier one is
dropped from the matrix alone, never from the table. Cells with no
`startedAt` are not in the matrix.

`runTableRows` reverses the cells to newest first and walks them: a
cell whose `reason` equals the previous row's `reason` and whose
`outcome` is also a `.failed` joins that row's `folded`; anything else
opens a new row. A `nil` reason folds only into another `nil` reason of
the same outcome. `id` is the lead run's `jobUID`.

`macos/Sources/IdiosModel/Tree.swift`.

    /// BarePodRow is one name among the pods no controller owns: the live pod's
    /// row and the kept rows of deleted pods of the same name, read as one line.
    public struct BarePodRow: Identifiable, Hashable, Sendable {
        public let id: String
        public let name: String
        public let rows: [Workload]
        public let openIncidents: Int32
    }

    extension BarePodRow {
        /// podCount is how many pods of this name idios still keeps.
        public var podCount: Int
        /// live is the row whose pod is still in the cluster, nil when every
        /// kept pod of the name is gone.
        public var live: Workload?
        /// needsChooser is true when the row names more than one kept pod, so
        /// opening it has to ask which.
        public var needsChooser: Bool
    }

    /// barePodRows merges the workload rows of kind none by pod name, so five
    /// rows of one name are one line that says how many pods it stands for.
    public func barePodRows(_ rows: [Workload]) -> [BarePodRow]

    /// kindCaption is the tree's heading over one kind: the kind in the plural
    /// and how many rows stand under it, because the caption names the kind and
    /// not the row.
    public func kindCaption(kind: String, count: Int) -> String

`barePodRows` keeps only rows whose `workloadKind` is `"none"`, buckets
them by `podName ?? ""` in served order, sums `openIncidents`, and
sorts each bucket's rows with the live one first (`livePods > 0`) then
by `podUID`. `id` is `"<clusterID>/<namespace>/none/<name>"`, the same
shape `Workload.id` has. A bucket whose name is empty keeps one row per
uid, because two pods idios cannot name are not one pod.

`kindCaption` is `"<plural> <count>"`: `"none"` gives `"Bare pods"`,
every other kind gives the kind with an `"s"` (`"Deployments"`,
`"CronJobs"`, `"StatefulSets"`). The plural stands whatever the count
is; the caption is uppercased by the view, not here.

Tests. `macos/Tests/IdiosModelTests/RunStripTests.swift` and
`TreeTests.swift`, Swift Testing, table-driven as `RunTests.swift` is,
with private fixture helpers `job(name:complete:condition:reason:
started:finished:failed:)` and `incident(id:jobUID:podUID:podName:
state:)` built through the memberwise initialisers with fixed
timestamps and every other field at its zero. Invented names only.

- `runCellsReadEveryRunsOutcomeOldestFirst` (traces to 9.3's "a run
  strip: one cell per run in the window, oldest first, coloured by
  outcome, dashed where a sweep removed the record"): one fixture set
  under CronJob `nightly` - a Complete run with no incident, a Failed
  run with one open unacknowledged incident, a Failed run whose only
  incident is acknowledged, a Failed run whose incidents are all
  closed, a run with no condition and no incident, and one incident
  whose `jobUID` no job row names - served newest first; the whole
  `[RunCell]` compared, which asserts the order, the names
  (`nightly-29807159` gives `29807159`), the outcomes and the
  `podUID` of each click.
- `runMatrixKeepsEveryHourAndTheMinuteEachRunStarted` (traces to "a
  strip up to 200 runs, an hour-by-minute matrix beyond"): cells at
  07:32, 07:59, 09:05 give three rows, the middle one empty, with the
  cells at minutes 32, 59 and 5; a cell with no `startedAt` is in no
  row; two cells in one minute leave the later one; the whole
  `RunMatrix` compared.
- `runTableRowsFoldConsecutiveRunsWithTheSameReason` (traces to "folds
  consecutive runs with the same reason"): six cells newest first with
  reasons A, A, A, B, A, A give three rows with `folded` counts 2, 0
  and 1; a Complete run between two failures never joins a fold; the
  whole `[RunTableRow]` compared.
- `barePodRowsMergeOneNameAndSayWhenItCannotOpenOne` (traces to 9.3's
  "Pods no controller owns are one row per name, carrying 'N pods'
  when a deleted pod of the same name is kept, and the row opens the
  live pod or a chooser when more than one shares the name"): rows for
  a name with one live pod, a name with a live and a deleted pod, a
  name with two deleted pods and a row the daemon could not name; the
  whole `[BarePodRow]` compared, with `podCount`, `live` and
  `needsChooser` asserted in the same value.
- `kindCaptionIsThePluralWithItsCount` (traces to "plural captions
  with counts (DEPLOYMENTS 1, CRONJOBS 2, BARE PODS 5)"): rows
  `("Deployment", 1) -> "Deployments 1"`, `("CronJob", 2) ->
  "CronJobs 2"`, `("none", 5) -> "Bare pods 5"`, `("StatefulSet", 0)
  -> "StatefulSets 0"`.

Check: `make app-test` green. No view changes yet, so the application
is unchanged and `make app` proves only that the package still builds
into it.

Checkpoint: `go build ./... && go test ./... && make ascii &&
make app-test && make app`.
Commit: `model: fold the runs and the bare pods for the tree`.

Consumes: `Job`, `Incident`, `Workload`, `Timestamp`, `StateTone`,
`groupBadge`, `foldOrder`, `buckets`, `runSuffix`, `durationText`,
`middleElided`, `distinct`.
Produces: `RunOutcome`, `RunCell` with `openIncidentIDs` and
`hover(now:)`, `runStripLimit`, `runCells`, `RunMatrixRow`,
`RunMatrix`, `runMatrix`, `RunTableRow`, `runTableRows`, `BarePodRow`,
`barePodRows`, `kindCaption(kind:count:)`.

## Task 2 - the verdict, the timeline fold and the steady tag

`macos/Sources/IdiosModel/Verdict.swift`.

    /// verdictSentences is what the middle pane leads with: what happened and
    /// how often, what the container was allowed, what was captured and whether
    /// the image moved, each sentence left out when its fields are not there.
    public func verdictSentences(
        incident: Incident, container: Container?, artifacts: [Artifact]
    ) -> [String]

Rules, in order, one sentence each.

1. What happened. `"Exit <code> (<lastReason>) <n> times since
   <clockTime openedAt>, last <clockTime lastSeenAt>."` with
   `"Exit <code> (<lastReason>)"` reduced to `"<lastReason>"` when
   `exitCode` is nil, and `"<n> times"` reduced to `"once"` when
   `occurrences` is 1. This sentence is always present.
2. What the container was allowed. `"Memory limit <byteCount
   memLimitBytes>, request <byteCount memRequestBytes>."`, either half
   dropped when its field is nil, the whole sentence dropped when the
   container is nil or neither is set. It is the limit that the spec
   names, and the request rides with it because a limit alone does not
   say how much was asked for.
3. What was and was not captured. Over the artifacts whose `kind` is
   not `podJSON`: `"<with> of <total> restarts captured a log"`, then
   `"; <label> wrote nothing (capture gap: <gap.label>)"` naming the
   newest artifact with no `filePath` through its `restartCount`
   (`"restart 6"`, or `"the running container"` for `logCurrent`), and
   the sentence closes with a full stop. With no artifact at all the
   sentence is `"Nothing was captured."`; with every artifact carrying
   a file it is `"Every restart captured a log."`.
4. Whether the image moved. `"Same image digest since open."` when
   `incident.imageID` equals `container?.imageID`, `"The image digest
   changed since this opened."` when both are set and differ, and no
   sentence when either is empty or the container is nil.

The plural rule of the whole file is `plural`, below; the counts here
are spelled through it.

`macos/Sources/IdiosModel/TimelineFold.swift`.

    /// TimelineFold is one line of the timeline: an entry, and the entries
    /// after it that repeat it, which its summary stands for.
    public struct TimelineFold: Identifiable, Hashable, Sendable {
        public let id: String
        public let lead: TimelineEntry
        public let repeats: [TimelineEntry]
        /// summary is the folded cycle's one line, nil when nothing folded.
        public var summary: String?
    }

    /// timelineFolds folds a repeating cycle into one line: consecutive entries
    /// whose kind, container, state and reason repeat with the same period at
    /// least three times. Lifecycle, rollout and cut entries never fold,
    /// because they are the spine of the story rather than one of its kinds.
    public func timelineFolds(_ entries: [TimelineEntry], minimumCycles: Int) -> [TimelineFold]

Rules. A fold key is the tuple `(kind, containerName, state, reason,
eventReason, conditionType, conditionStatus)`; every other field, the
times included, is outside it, because a cycle is the same thing
happening again. An entry whose kind is `lifecycle`, `rollout` or `cut`
has no key and ends any run in progress. The walk finds, at each
position, the smallest period `p` from 1 to 4 whose next `p` keys
repeat at least `minimumCycles` times consecutively, takes the longest
such stretch, and makes one `TimelineFold` whose `lead` is the first
entry of the stretch and whose `repeats` is every entry after it.
Everything else is a fold with an empty `repeats` and a nil `summary`.
`id` is the lead's `observedAt.raw` with its kind, which is unique
because the daemon orders by the observed time and never writes two
rows of one kind at one instant for one container.

`summary` is `"x<cycles>, every <runCadence over the cycle starts>,
first <clockTime of the lead>, last <clockTime of the last repeat>"`,
with the cadence part dropped when `runCadence` returns nil (under
three starts it is a gap, not yet a rhythm), and `cycles` the number of
whole periods.

`macos/Sources/IdiosModel/PodPage.swift`:

    /// readinessFlips counts the changes of the pod's Ready condition in the
    /// history, so a header tag that would otherwise flip every few seconds can
    /// say the pod is looping instead.
    public func readinessFlips(_ conditions: [PodCondition]) -> Int

    /// podStateTag is the page header's tag: DELETED for a pod that is gone,
    /// LOOPING for one whose readiness keeps flipping, else the phase and, when
    /// a Ready condition was read, READY or NOT READY.
    public func podStateTag(phase: String, deleted: Bool, ready: Bool?, flips: Int) -> String

`readinessFlips` keeps the conditions whose `type` is `"Ready"`, sorts
them by `observedAt.raw`, and counts the positions where `status`
differs from the one before. `podStateTag` keeps its three existing
answers and inserts one: with `deleted` false and `flips > 3` it is
`"<PHASE>, LOOPING"`. The existing `podStateTag(phase:deleted:ready:)`
is replaced, not kept beside the new one; every call site passes
`flips`.

`macos/Sources/IdiosModel/Display.swift`:

    /// plural writes a count with its noun under the one rule English keeps:
    /// one is singular and every other number, zero included, is not.
    public func plural(_ count: Int, _ noun: String) -> String

`plural` appends `"s"`; a noun with an irregular plural is not passed
to it. It returns `"<count> <noun>"`.

Tests, table-driven, whole-value.

- `verdictSentencesSayWhatHappenedAndWhatWasCaptured` (traces to 9.3's
  "The header leads with a verdict block built from the fields the page
  already receives: what happened, how many times since when and last
  when, the memory limit from mem_limit_bytes, what was and was not
  captured from capture_gap, and whether the image digest changed since
  the incident opened"): rows for a crash on container `api` of
  `checkout-api` with eight occurrences, a memory limit and request,
  eight artifacts of which one has no file and the same image id (the
  four sentences); the same incident with `occurrences` 1 (`"once"`);
  a container with no limits (three sentences); no artifact at all
  (`"Nothing was captured."`); an incident whose `imageID` differs from
  the container's (the changed sentence); an incident with no exit code
  (the reason alone); a job-subject incident with no container (two
  sentences). Whole `[String]` compared.
- `timelineFoldsCollapseARepeatingCycleAndNeverTheSpine` (traces to
  "Repeating cycles in the Timeline fold into one entry (x31, every ~4
  min, first 07:32, last 09:52) with a disclosure; lifecycle, rollout
  and cut entries never fold"): rows for eight entries alternating
  waiting and terminated on container `api` at four-minute steps (one
  fold, `summary` "x4, every 8m, first 07:32, last 08:00" written out
  literally); the same eight with a `lifecycle` entry in the middle
  (two folds and the lifecycle entry between them, each with its own
  summary or none); four entries of one key (one fold, cycles 4); two
  entries of one key (two folds, no summary, because two is not a
  cycle); a `cut` entry alone (one fold, no summary). Whole
  `[TimelineFold]` compared.
- `podStateTagReadsLoopingPastThreeReadinessFlips` (traces to "The
  header tag is debounced and reads RUNNING, LOOPING when readiness
  flipped more than three times in the window"): rows
  `(Running, false, true, 0) -> "RUNNING, READY"`, `(Running, false,
  false, 3) -> "RUNNING, NOT READY"`, `(Running, false, false, 4) ->
  "RUNNING, LOOPING"`, `(Running, false, true, 9) -> "RUNNING,
  LOOPING"`, `(Running, true, false, 9) -> "DELETED"`, `(Pending,
  false, nil, 0) -> "PENDING"`. The existing rows of
  `PodPageTests.swift` for this function gain the flips argument.
- `readinessFlipsCountsTheReadyConditionsChanges` (same sentence): no
  condition (0), one Ready row (0), three Ready rows True, True, False
  (1), four rows alternating (3), Ready rows out of served order (the
  sort decides), rows of another type ignored.
- `pluralAgreesWithItsCount` (traces to 9.3's "Plurals agree with their
  counts"): rows `(0, "restart") -> "0 restarts"`, `(1, "restart") ->
  "1 restart"`, `(2, "pod") -> "2 pods"`.

Check: `make app-test` green; nothing in the application draws these
yet except `podStateTag`, whose one call site in `PodPageHeader`
passes `flips: 0` in this task and is given the history in task 7.

Checkpoint as task 1.
Commit: `model: build the verdict, fold the timeline, steady the tag`.

Consumes: `Incident`, `Container`, `Artifact`, `CaptureGap.label`,
`PodCondition`, `TimelineEntry`, `TimelineKind`, `byteCount`,
`clockTime`, `durationText`, `runCadence`, `artifact` kinds.
Produces: `verdictSentences`, `TimelineFold`, `timelineFolds`,
`readinessFlips`, `podStateTag(phase:deleted:ready:flips:)`, `plural`.

## Task 3 - the workloads tree

`macos/idios/Views/Components/BadgeStyle.swift`:

    /// runCellStyle is the colour one run of the strip carries: the state tones
    /// the vocabulary already fixes, with the dashed style for the run whose
    /// record the sweeper removed.
    func runCellStyle(_ outcome: RunOutcome) -> BadgeStyle

`.complete` is `.green`, `.failed(let tone)` is `tone.badge` (red,
orange, grey), `.running` is `.neutral` and `.swept` is `.gap`. No new
colour is defined; the function is a mapping onto the statics that are
there.

`macos/idios/Views/Workloads/WorkloadsScreen.swift`.

The pane header goes above the filter field, at the tree's width:

    /// WorkloadsPaneHeader names what the tree's trailing number counts and
    /// carries the two controls that move every group at once.
    private struct WorkloadsPaneHeader: View {
        let collapseAll: () -> Void
        let expandAll: () -> Void
    }

One row, height 28: `Text("open incidents")` in `.secondary` at 11
points, `Spacer()`, `Button("Collapse all")` and `Button("Expand all")`
in `.link` style at 11 points, as `IncidentsList` draws its pair.
`collapseAll` inserts every cluster and namespace id into
`tree.collapsed`; `expandAll` empties it. The ids are the ones
`expansion(_:)` already uses (`"cluster/<id>"` and
`"<clusterID>/<namespace>"`), computed from `clusterGroups`.

The `.onChange(of: tree.filter)` that empties `tree.collapsed` is
deleted, with nothing in its place: a filter narrows the rows and a
group the filter hides comes back where it was.

`kindCaptionRow` draws `kindCaption(kind: kind.kind, count:
kind.rows.count).uppercased()` and keeps its type, kerning and
`.help`, whose text for `"none"` becomes "pods no controller owns; one
row per pod name". The file-scope `kindCaption(_ kind: String)` is
deleted; `IdiosModel.kindCaption(kind:count:)` replaces it. The count
of a bare-pod caption is the merged row count, not the workload rows.

Bare pods. `KindGroup` for kind `"none"` carries
`barePodRows(kind.rows)` instead of its rows, and `podRow` takes a
`BarePodRow`:

    /// barePodRow is one name among the pods no controller owns: the name, how
    /// many pods of it are kept, and its open incidents.
    private func barePodRow(_ row: BarePodRow) -> some View

The dot keeps its grey square; the name is monospaced, tertiary when
`row.live` is nil; `plural(row.podCount, "pod")` follows it in
`.secondary` when `podCount > 1`; the open count is drawn as today. A
tap opens `row.live?.podUID` when the row does not need a chooser, and
otherwise sets `@State private var chooser: BarePodRow?`, a popover
anchored on the row listing one button per kept pod: the name, "live"
or `"deleted <clockTime deletedAt>"`, opening that uid. A row whose
pods have no uid at all keeps its existing help and opens nothing.

The context menu is on every workload row and every bare-pod row:

    /// rowMenu is the four things a tree row can do without leaving the tree.
    @ViewBuilder private func rowMenu(_ key: WorkloadKey?, name: String, podUID: String?)
        -> some View

- "Show incidents" opens that node on its Incidents tab; "Show pods"
  opens it on Pods. Both go through the `route` binding and
  `tree.tab`, which is what `IncidentsScreen.openWorkload(_:tab:)`
  already sets from outside; inside the tree they set
  `tree.selected` and `tree.tab` directly. A bare-pod row has no
  workload node, so its two items open the pod page instead (its
  incidents and its pod are the same page).
- "Copy name" writes `name` to `NSPasteboard.general` with the two
  lines `clearContents()` and `setString(_:forType: .string)`.
- "Acknowledge all open" sets `@State private var
  pendingAcknowledge: (key: WorkloadKey?, podUID: String?, name:
  String, count: Int)?` from the counts the tree already holds
  (`Workload.openIncidents`, or the `BarePodRow.openIncidents`) and
  raises a `confirmationDialog` naming the count and the name, as the
  list's bulk acknowledge does; confirming calls the store.

`macos/idios/Store/WorkloadsStore.swift`:

    /// writes is the one write path the tree has: acknowledging every open
    /// incident of a row.
    let writes = IncidentWriteStore()

    /// acknowledgeOpen acknowledges every open incident of one workload or one
    /// pod and answers how many it changed, so the tree can say so.
    func acknowledgeOpen(key: WorkloadKey?, podUID: String?, connection: DaemonConnection)
        async -> Int

    /// acknowledge acknowledges the incidents a caller already holds the ids of,
    /// which is what a drag over the run strip has.
    func acknowledge(ids: [String], connection: DaemonConnection) async -> Int

`acknowledgeOpen` lists first, because the tree holds counts and not
rows: `ListIncidents(query: .init(cluster_ids: [key.cluster],
namespace: key.namespace, state: "open", workload_kind: key.kind,
workload_name: key.name))` for a workload and `.init(pod_uid: podUID,
state: "open")` for a bare pod; then hands the row ids to
`acknowledge(ids:connection:)`, which calls `writes.acknowledge(id:
connection:)` per id in order and counts the ones that answered with an
incident. Errors go through the existing `report(_:
connection:)`; `.cancelled` is ignored. Afterwards `load(connection:
scope:)` runs, so the counts in the tree are the daemon's again rather
than a local guess.

Check against the smoke store on 7771 (`./bin/idios -data-dir
.storage/smoke -kubeconfig ./kube/config -listen 127.0.0.1:7771 run`),
the application started with `-daemon 127.0.0.1:7771`: the captions read
"DEPLOYMENTS 1", "CRONJOBS 2", "JOBS 1", "BARE PODS 5" with the row
count after each; the pane header says "open incidents" and its two
buttons move every group; typing in the filter and clearing it leaves
the collapse state where it was; a bare pod whose deleted twin is kept
is one row saying "2 pods" and opens a chooser; the context menu on a
workload row shows its four items, "Copy name" pastes the name, and
"Acknowledge all open" asks first, then turns that row's count amber in
the list. Screenshot `workloads` through the scratch script for the
diff review.

Checkpoint as task 1.
Commit: `app: give the workloads tree captions, controls and a menu`.

Consumes: `kindCaption`, `barePodRows`, `BarePodRow`, `plural`,
`RunOutcome`, `StateTone.badge`, `WorkloadsTreeState`, `WorkloadKey`,
`IncidentWriteStore`, `DaemonConnection`, `ClusterScope`.
Produces: `runCellStyle`, `WorkloadsPaneHeader`, the merged bare-pod
row and its chooser, the row context menu,
`WorkloadsStore.acknowledgeOpen`, `WorkloadsStore.acknowledge(ids:)`
and `WorkloadsStore.writes`.

## Task 4 - the CronJob detail

`macos/idios/Views/Workloads/RunStripView.swift`:

    /// RunStripView is the line of runs above the Runs table: one cell per run,
    /// oldest first, with the hover, the click and the drag the runs answer to.
    struct RunStripView: View {
        let cells: [RunCell]
        let now: Date
        let openPod: (String) -> Void
        let openIncident: (String) -> Void
        let acknowledge: ([String]) -> Void
    }

    /// RunMatrixView is the strip past its limit: one row per hour, one cell
    /// per minute, so two thousand runs still fit a pane.
    struct RunMatrixView: View {
        let matrix: RunMatrix
        let now: Date
        let openPod: (String) -> Void
    }

The strip is a `VStack`: one line saying `"Last \(plural(cells.count,
"run")), oldest first - <clockTime first> to <clockTime last>"` and, at
the right, the outcome counts (`"62 failed - 1 complete - 1 not
recorded (swept)"`, each part dropped at zero); then the cells; then
the legend, one `Text` in `.secondary`: "hover: condition, duration and
exit code - click: the run's pod - drag: acknowledge those runs".

A cell is a `RoundedRectangle(cornerRadius: 1)` of width 6 and height
22 in `runCellStyle(cell.outcome).text`, drawn as a dashed
`strokeBorder` with that style's `borderDash` when the outcome is
`.swept`, with `.help(cell.hover(now: now))`. The newest cell carries a
one-point outline in `.primary`, as frame 5a draws it. A click opens
`cell.podUID` through `openPod`, or `cell.incidentIDs.first` through
`openIncident` when no pod is kept. Cells sit in an `HStack(spacing:
1)` inside a horizontal `ScrollView` so a 200-cell strip never squeezes
below its own width.

The drag is a `DragGesture(minimumDistance: 3)` on the strip: the
start and current x map to cell indexes through the fixed cell pitch
(7 points), and every cell in the range lights with an accent ring it
draws itself, never a `List` selection, because a selection binding
whose getter disagrees with the pointer does not redraw. On the
gesture's end the union of `openIncidentIDs` over the range goes to
`acknowledge`, which raises the same `confirmationDialog` the tree
uses, naming the run count and the incident count.

`RunMatrixView` draws one row per `RunMatrixRow`: the hour at the left
in monospaced digits, then sixty cells of width 4, an empty minute
drawn in `.quaternary`. The same hover and click apply.

`macos/idios/Views/Workloads/WorkloadDetailView.swift`.

- The Runs default. `RunsTab` gains `defaultFilter`, computed as
  `.failed` when `page.failedTotal > 0` and nil (All) otherwise, and
  `selectedFilter` reads `filter ?? defaultFilter`, the same shape
  `selectedTab` already has, so the default re-derives per workload
  rather than being written into the binding.
- The strip. `RunsTab` draws `RunStripView(cells:)` when `cells.count
  <= runStripLimit` and `RunMatrixView(matrix: runMatrix(cells))`
  otherwise, above the chips. `cells` is `runCells(jobs: page.rows,
  incidents: incidents ?? [], cronjobName: name)`. The strip is drawn
  over the whole page the store holds, never over the filtered chips:
  a strip that changes with a chip stops being a picture of the
  window. The runs page is asked for unfiltered rows for this reason:
  `RunsKey` keeps its `live` and `failed` for the table's chips, and
  the strip reads the rows of the unfiltered key, so `WorkloadsStore`
  keeps the unfiltered page beside the filtered one under
  `stripPage`/`stripKey` with the same key guard `runs(of:)` has, and
  `func strip(of key: WorkloadKey) -> Page<Job>?` answers it.
- The table. `JobsCard` takes `[RunTableRow]` from
  `runTableRows(cells)` instead of `[Job]`. The JOB column shows
  `row.run.name`, the suffix alone, with `.help` carrying the whole
  job name. Each row is a `Button { openPod(uid) }` when
  `row.run.podUID` is set, and the REASON column ends with the run's
  incident as a link, `Button("#\(id)") { openIncident(id) }` in
  `.link` style, for the first of `row.run.incidentIDs`; a run with
  no pod kept says `"pods pruned"` in `.secondary` where the pod link
  would be. A row with a non-empty `folded` draws under itself one
  line, `"+ \(plural(row.folded.count, "older run")), same reason"`
  with a "show" button that expands it into its rows, kept in a
  `@State private var expandedFolds: Set<String>` keyed on the row id.
  The Condition badge takes `runCellStyle(row.run.outcome)`, which is
  red while the run's incident is open and grey once it closed, and is
  the one place that colour is decided.
- The Pods default. `PodsTab` gains the same `defaultFilter` shape,
  `.live`, and `DetailKey.podsLive` follows `selectedFilter == .live`
  so the daemon serves the live pods for the default.
- The Overview window.

        /// WorkloadWindow is how far back the Overview's cards and chart look.
        enum WorkloadWindow: String, CaseIterable, Hashable, Sendable {
            case all, day, sixHours
            /// title is the control's label.
            var title: String
            /// hours is the cut, nil for the daemon's whole window.
            var hours: Int?
        }

  `title` is "3d", "24h" and "6h"; `hours` is nil, 24 and 6. The
  picker sits at the right of the Overview's first row and is kept in
  `@State private var window: WorkloadWindow = .all`. The chart's
  buckets are cut to `hours` before drawing. The four cards keep the
  daemon's aggregates under `.all` and are counted from the incident
  rows the store holds for the key (`incidents(of:)`) under the two
  narrower windows, each card's sub-line saying which window it
  stands for; the Incidents rows are loaded for the selected key
  whatever the tab is, one `.task(id:)` as today.
- The chart. `RestartsCard` gains a y-axis of three labels (the peak,
  half of it rounded down, and 0) in a 28-point leading column with a
  hairline rule; every hour between the first and the last bucket is
  drawn, an hour the daemon did not send as a zero bar, so the bars
  are true hours and a quiet stretch is visible; each bar carries
  `.help("<clockTime hour> - <plural(restarts, "restart")>")`, and an
  hour whose rows are reconstructed keeps its dashed hollow bar. The
  caption keeps its sentence about reconstructed rows.

Check against the smoke store on 7771: the failing CronJob opens on
Runs; the strip draws one cell per run oldest first with the complete
run green, the swept record dashed and the newest outlined; a hover
names the condition, the duration and the exit code; a click opens the
run's pod; a drag over a stretch asks before acknowledging and the
cells turn amber after it; the Runs table opens on Failed, names runs
by their suffix, links each to its pod and its `#id`, and folds the
long stretch behind "+ N older runs, same reason"; the CronJob with
hundreds of runs draws the matrix instead of the strip, one row per
hour; the Deployment's Pods tab opens on Live; its Overview's window
control moves the cards and the chart together and the chart has an
axis and a value on hover. Screenshots `workload/<cluster>/<ns>/
CronJob/<name>` and the Deployment's node for the diff review.

Checkpoint as task 1.
Commit: `app: open a CronJob on its runs with the run strip`.

Consumes: `runCells`, `runStripLimit`, `runMatrix`, `RunMatrix`,
`runTableRows`, `RunTableRow`, `RunCell`, `runCellStyle`, `plural`,
`clockTime`, `durationText`, `WorkloadsStore.runs(of:)` and
`.incidents(of:)`, `openPod`, `openIncident`,
`WorkloadsStore.acknowledge(ids:)`.
Produces: `RunStripView`, `RunMatrixView`, `WorkloadWindow`,
`WorkloadsStore.strip(of:)`, the Runs and Pods defaults, the run links
and the chart's axis.

## Task 5 - the pod page's verdict and its one primary action

`macos/idios/Views/IncidentDetail/VerdictBlock.swift`:

    /// VerdictBlock is what the middle pane leads with: the answer, in
    /// sentences, before the tags and the fields that support it.
    struct VerdictBlock: View {
        let incident: Incident
        let container: Container?
        let artifacts: [Artifact]
    }

One `Text` per sentence of `verdictSentences(incident:container:
artifacts:)`, wrapped (`lineLimit(nil)`, never `.fixedSize(horizontal:
false, vertical: true)`, which overflows the window on a concatenated
header text), the first sentence `.semibold` and the rest
`.secondary`, at 12 points with 3 points of spacing. No colour: the
tags under it carry the state. It draws above the category and state
tags in the pane header, so the first line of the pane is the answer.

`macos/idios/Views/PodPage/ContainerPane.swift` and `PodCardPane.swift`.

- `VerdictBlock` goes at the top of the pane header, with the lit
  incident, the selected `Container` (nil on the Pod card and on the
  Job-only page) and the artifacts of that container.
- The segmented control is drawn only when the row count is two or
  more: `if incidents.count >= 2` in `ContainerPane.controlRow` and
  `if podLevel.count >= 2` in `PodCardPane.controlRow`. With one
  incident the header alone says which it is, and a one-segment
  control painted as a button says nothing.
- `PaneActionsRow` is rebuilt to one shape for open and closed rows:

        /// PaneActionsRow is the lit incident's verbs: Acknowledge with its
        /// key, Note beside it, and everything else behind Actions.
        private struct PaneActionsRow: View

  `Button("Acknowledge")` in `.borderedProminent`, followed by a key
  cap - `Text("a")` in a 14-point rounded `.quaternary` square - and
  disabled with the help "acknowledged <clockTime acknowledgedAt>"
  when the row is already acknowledged; then `Button("Note...")`;
  then `Menu("Actions")` holding Dismiss or Undismiss, "Mark resolved"
  while the row is open, Unacknowledge while it is acknowledged,
  "Unresolve" exactly when `closeReason == .manual`, "Ask AI" for the
  lit incident (its three items become a submenu, moved out of the
  toolbar, where it sat beside a Copy uid that is not about the
  incident), a `Divider()` and "Delete incident" with its existing
  confirmation. `BadgeStyle.red` leaves this row: a destructive verb
  behind a menu and a confirmation does not also need the hue that
  means an open incident.
- The keys are unchanged (`a`, `d`, `r` on the focused screen through
  `.onKeyPress`); only the button that shows `a` is new.

Method text to the title's hover. `KubeletCard`'s footer ("idios
records requests and limits, never usage...") and `JobCard`'s footer
("Success and failure come from condition_type and condition_reason
...") become those cards' `DetailCard` `meta`, which is already the
title's hover, and the visible footers go. `EventsCard`'s two
paragraphs stay visible: they say what Kubernetes does, not where a
value came from, and 7.4's rule is about provenance.

Implementer notes go. `KubeletCard`'s `" (phase is not used for
category)"` is deleted. The deleted banner's "(inferred)" stays: 7.6
says what idios does not know is said, not hidden.

Plurals agree, through `plural`: `containerStateLine`'s
`"\(restartCount) restarts"`, `PodPageRail.containerNote`'s copy of
it, `PodPageHeader.countsLine`'s containers and incidents,
`PodPageRail`'s "SIBLINGS N pods of this <kind>",
`ContainerPane`'s "Logs N files", `CapturedLogsCard`'s "N lines" and
`TimelineView`'s "N of M entries". `containerStateLine` lives in
`IdiosModel`, so its `PodPageTests` row for a one-restart container is
updated in the same commit.

Check against the smoke store on 7771: the crash pod's pane opens with
four sentences above the tags, naming the exit code, the count since
and last, the memory limit, what the captures got and that the digest
has not moved; a container with one incident shows no segmented
control; Acknowledge is the only filled button, shows `a`, and greys
out with a hover after the key or the button is used; Note sits beside
it and the rest are under Actions, Ask AI included; the kubelet card's
footer is gone and its title's hover carries it; nothing on the page
says "1 restarts". Screenshots `incident/<id>` for the diff review.

Checkpoint as task 1.
Commit: `app: lead the pod page with a verdict and one action`.

Consumes: `verdictSentences`, `plural`, `Container`, `Artifact`,
`IncidentWriteStore`, `DetailCard.meta`, `segmentOrder`,
`containerStateLine`.
Produces: `VerdictBlock`, the rebuilt `PaneActionsRow`, the
two-or-more rule on both segmented controls, the cards' hovers.

## Task 6 - the log scrubber, the folded timeline, the events time column

`macos/idios/Views/IncidentDetail/CapturedLogsCard.swift`. The chip
grid goes; one file body stays with a scrubber over it:

    /// LogScrubber is how a person walks the captured files: one body at a
    /// time, restart N of M, with the previous and next restart a click away.
    private struct LogScrubber: View {
        let artifacts: [Artifact]
        let selected: Artifact
        let select: (String) -> Void
    }

One row: `Text("Restart")`, a chevron-left `Button` disabled at the
first file, `Text("\(index + 1) of \(artifacts.count)")` in
monospaced digits with the file's label on its hover, the capture time
through `clockTime(capturedAt, seconds: true)`, a chevron-right
`Button` disabled at the last, then the bar - one cell per artifact in
`artifactOrder`, four points wide, the selected one filled with
`Color.accentColor`, a cell whose `filePath` is nil drawn dashed in
`BadgeStyle.gap`, each clickable and carrying its label and, where
there is one, its capture gap on hover - and at the right one
`.secondary` line: "dashed: no output captured - last cell:
current.log". `ArtifactChips` is deleted; the "no file" case keeps the
badge, the `capture_gap` line and the `capture_note` it already draws
in place of the pane. The tab title becomes `"Logs \(plural(files,
"file"))"` where `files` counts the artifacts of the container whose
`filePath` is not nil, so a container with thirty-four gaps and no
file reads "Logs 0 files" over a scrubber that says so once, not
thirty-four times.

`macos/idios/Views/IncidentDetail/TimelineView.swift`. The rows come
from `timelineFolds(visible, minimumCycles: 3)` after the filter, so a
filter that hides a kind does not fold across the hole it leaves. A
fold with an empty `repeats` draws exactly the row it draws today. A
fold with repeats draws its lead, then `summary` in `.secondary` on
the same line, then a `DisclosureGroup` chevron that reveals the
repeats as ordinary rows. `TimelineRow` shows one time,
`clockTime(entry.k8sAt ?? entry.observedAt, seconds: true)`, with
`.help("k8s <k8sAt.raw or none> - observed <observedAt.raw>")`; the
"(observed)" line under the time goes, because the hover now carries
the pair and one entry printing its time three times is what the
review found.

`macos/idios/Views/IncidentDetail/EventsCard.swift`. The `LAST_TS
(k8s)` column widens from 150 to 190 points and its `Text` takes
`.lineLimit(1)`, so a 27-character stamp sits on one line; the
`MESSAGE` column keeps `WrapText` and grows the row instead. Nothing
else in the table changes.

Check against the smoke store on 7771: the crash container's Logs tab
shows one body with "Restart 8 of 8" and the two chevrons walking it,
the bar dashed where nothing was captured; the tab reads "Logs 8
files"; the timeline of the looping container shows one folded line
"x31, every ~4 min, first ..., last ..." with a chevron that opens the
31 rows, while the opened and closed entries stay their own rows; each
entry shows one time and its hover carries the pair; the events table
keeps every timestamp on one line and wraps the message. Screenshots
`pod/<uid>/logs` and `timeline/<id>` for the diff review.

Checkpoint as task 1.
Commit: `app: scrub the captured logs and fold the timeline`.

Consumes: `timelineFolds`, `TimelineFold`, `plural`, `artifactOrder`,
`artifactLabel`, `defaultArtifact`, `clockTime`, `BadgeStyle.gap`,
`ArtifactContentStore` through `PodPageStore.content(of:connection:)`.
Produces: `LogScrubber`, the folded timeline rows with their
disclosure, the one time per entry, the events time column.

## Task 7 - the breadcrumb, Forward and the rail's subtractions

`macos/idios/App/Route.swift`:

    /// forwardSerial changes on every Forward request, for the same reason
    /// serial does: the screen watches it rather than a value that could
    /// repeat.
    private(set) var forwardSerial = 0

    /// goForward asks the main window to return to the route Back left.
    func goForward() { forwardSerial += 1 }

`macos/idios/App/IdiosApp.swift`: in `CommandMenu("Go")`, after Back:

    Button("Forward") { navigator.goForward() }
        .keyboardShortcut("]", modifiers: .command)

`macos/idios/Views/Incidents/IncidentsScreen.swift`: `@State private
var forward: [Route] = []`. The `.onChange(of: navigator.backSerial)`
that pops `path` appends the popped route to `forward`; a new
`.onChange(of: navigator.forwardSerial)` pops `forward` and appends it
to `path`. Every other push (`perform`, `openIncident`, the `openPod`
closures) empties `forward` first, so a new direction discards the old
one, which is what a forward stack means; `show(_:)` and every
`path = []` empty it too.

The breadcrumb. `PodPageScreen` gains `let openWorkload: (Route,
WorkloadTab) -> Void` and `let revealNamespace: (String, String) ->
Void`, wired in `IncidentsScreen` to `openWorkload(_:tab:)` and to a
new private `revealNamespace(cluster:namespace:)` that sets `screen =
.workloads`, empties `path`, and removes `"cluster/<id>"` and
`"<id>/<namespace>"` from `workloadsTree.collapsed`, which are the
exact keys the tree's `expansion(_:)` uses. `PodPageHeader`'s identity
line becomes a row of `Button`s in `.link` style separated by
`Text(" / ")`: the cluster segment reveals its cluster, the namespace
segment reveals the namespace, and the workload and controller
segments open that node on Pods through `openWorkload`. A segment with
nothing to open (a bare pod's absent workload) stays plain text. The
back chevron in the toolbar gains a forward chevron beside it, both
`Button`s calling `navigator.goBack()` and `goForward()`, with their
key equivalents named in their help.

The debounced tag. `PodPageHeader` takes `flips: Int` and passes it to
`podStateTag`; `PodPageScreen` loads the pod's history on arrival
(`store.loadHistory(uid:connection:)`, which the Conditions tab
already calls) and computes `readinessFlips(store.history?.conditions
?? [])`. The tag itself is held: `@State private var shownTag: String`
and a `.task(id: tag)` that sleeps 5 seconds before adopting a new
value, so a readiness that flips every few seconds cannot make the
header flicker; the first value is adopted at once.

The rail. `PodPageRail`'s CONTEXT section keeps `Node`, `QoS class`,
`Image id` and `Container id` and nothing else: the `Cluster`,
`Context`, `API server`, `Namespace` and `Image` rows go, because the
header's identity line already carries the cluster and the namespace
and the image reference is on the Overview card. The owner chain, the
times and the siblings are untouched, and the siblings' "+ N more"
line keeps opening Workloads on that workload's Pods.

Check against the smoke store on 7771: Cmd-] returns to the page Back
left and is greyed nowhere it should act; the Go menu shows Forward
under Back with its symbol; clicking the workload segment of the
identity line opens Workloads on that node's Pods, the namespace
segment opens the tree with that namespace expanded; the looping pod's
header tag settles and reads "RUNNING, LOOPING" rather than flipping;
the rail's CONTEXT has four rows and repeats nothing above it.
Screenshots `pod/<uid>` and `workloads` for the diff review.

Checkpoint as task 1.
Commit: `app: link the breadcrumb and add Forward`.

Consumes: `Navigator`, `IncidentsScreen.openWorkload(_:tab:)` and
`resolvedWorkload(_:)`, `WorkloadsTreeState.collapsed`,
`podStateTag(phase:deleted:ready:flips:)`, `readinessFlips`,
`PodPageStore.loadHistory`, `PodHistory.conditions`.
Produces: `Navigator.forwardSerial` and `goForward`, the Go menu's
Forward, the forward stack, the breadcrumb, the held header tag, the
reduced CONTEXT.

## Task 8 - verification with the user and the status line

The user runs the application against the smoke store on 7771 and
walks both screens: the tree's captions, the pane header's two
controls, a filter and its cleared collapse state, a bare pod with a
deleted twin and its chooser, each of the four context-menu items; the
CronJob's strip, its hover, its click, its drag, the matrix on the
CronJob with hundreds of runs, the Runs table's default, names, links
and fold, the Pods tab's Live default, the Overview's window and its
chart; the pod page's verdict block on a crash, an OOM, an image-pull
and a job-subject incident, the one primary Acknowledge and its key,
the Actions menu, the scrubber, the folded timeline, the breadcrumb,
Cmd-] and the tag. Anything the review turns up is fixed in the task
that owns it and committed as a new commit, never by rewriting one.
Then `docs/plans/m10-reading/roadmap.md`'s step D line becomes
"complete <date>" and the step E line stays "not started".
Commit: `docs: close m10 step D`. The 7771 daemon is stopped.

## Hands to the next step

Step E (`glance.md`) reads: `plural`, which every count it writes goes
through; `runCellStyle` and `RunOutcome` for nothing (the menu bar has
no runs), but `groupEntries`, `groupSummary` and `groupBadge` of step B
for its rows; `WorkloadWindow` as the precedent for a client-side
window over served rows, which Status does not need; `VerdictBlock` for
nothing; and the three fields it must draw that this step leaves alone -
`related_incidents`, which is still not decoded on
`IdiosModel.IncidentDetail`, `Event.firstTS`, which the events table
still does not draw, and `Job.succeeded`, `completions` and
`parallelism`, which `JobCard` still does not draw. The rail's context
now has four rows, so the fields step E adds there go beside them, not
in place of the ones this step removed.

## Self-review

Spec coverage. The Workloads row of 9.3: task 1 (the captions, the
bare-pod merge, the cells, the matrix, the table fold), task 3 (the
pane header, Collapse and Expand all, the filter keeping collapse
state, the bare-pod row and its chooser, the four-item context menu),
task 4 (the strip with its hover, click and drag, the 200-run cap and
the matrix, the Runs default, the suffix names, the two links, the
Condition badge's tone, the Pods Live default, the Overview's window
and the chart's axis, hover, true hours and dashed gaps). The Pod page
row: task 2 (the verdict's four sentences, the timeline fold's rule,
the looping tag), task 5 (the verdict block, the one primary with its
key, Note beside it, the Actions menu, the two-or-more segmented rule,
method text to the title hover, the implementer note gone, plurals),
task 6 (the scrubber over one body, "Logs N" counting files that
exist, the folded timeline with its disclosure, one time per entry with
the pair on hover, the events time column), task 7 (the breadcrumb, the
Forward chevron and Cmd-], the rail's kept rows and CONTEXT's dropped
repeats). 7.4: task 5 (provenance is the title's hover) and task 6 (the
message wraps, the identifier keeps its middle elision). 9.4: task 7
(Cmd-] is a Go menu item beside Cmd-[; no bare letter is added). 9.5:
task 3's `runCellStyle`, which adds no colour and maps onto the tones
`BadgeStyle` already holds. Decision 10: every name in this plan, its
tests and its checks is invented or a fixture's.

Split. Eight tasks, seven of them code. The model is two commits rather
than one because the two halves share nothing - the runs and the tree
are read by `WorkloadsScreen`, the verdict, the fold and the tag by the
pod page - and a single commit would be one diff no reviewer could
read against one screen. The pod page is three commits rather than the
four the roadmap's sentence list suggests: the scrubber, the timeline
and the events column are all one pane's subtractions and share the
`plural` and `clockTime` edits, while the breadcrumb, Forward and the
rail are the page's frame. Tasks 3 and 4 are separate because the tree
and the detail have no shared type but `WorkloadKey`, and task 4's
strip cannot be reviewed before the tree's rows are what they will be.

Doubts. The roadmap's goal sentence for step B speaks of "one header
over one rollup", while `IncidentGroup.headerless` (step B, shipped)
draws a single-entry workload group without a header. The headerless
rule stands: decision 1's own text fixes the header shape and says
nothing that requires a header over a lone rollup, and nothing in
decisions 7 or 8 touches the list. If the user wants the header back it
is a step B deviation, recorded in that step's closing report, not a
change here. -- "Show incidents" in the tree's context menu opens the
node's Incidents tab rather than the Incidents screen: the incidents
list has no workload filter and decision 6 forbids adding an endpoint,
so the tab is the only place the rows for one workload exist. -- The
breadcrumb's cluster and namespace segments have no `Route` case to
open (`Route` carries `.workloads` and a whole `.workload`), so they
reveal that node in the tree by clearing its collapse keys instead of
navigating to it; the alternative, a new `Route` case, is a change no
decision asks for. -- The Overview's widest window is labelled "3d"
after the mockup, though retention is configurable
(`Status.retention_days`); it means the daemon's whole window whatever
that is, and reading the label from Status is a step E concern where
Status is already in hand. -- Back gains Forward, and Forward is a
`path` concern: it returns to a pushed page Back left. It does not
carry a history of screen switches, because `show(_:)` empties `path`
by design and no decision asks for one; a person returning to a screen
has Cmd-1/2/3 and the palette's recent pages, which step C records. If
screen-switch history is wanted it is a new decision, not a reading of
"Back gains Forward (Cmd-])". -- The verdict's memory sentence names
the request beside the limit, which decision 8 does not; a limit with
no request beside it does not say how much was asked for, and both
fields are on the container the page already has.

`.ai` rules. ascii-only: no symbol in any code block or string; the
chevrons are SF Symbols named in prose, the key caps are the letters,
and `hack/ascii-check` is run on this file. tests: every test above
names the sentence it traces to, is table-driven, compares whole
values, and leads with the edge cases (the swept run, the run with no
condition, the pod with no uid, the container with no limits, the
incident with no exit code, the two-entry stretch that is not a cycle,
zero and one in `plural`); no test asserts a constant, a title or a
store. comments: every comment in a code block says why. code-is-truth:
no code block or test name mentions a document, a section or this plan.
scope: no proto change, no new endpoint, no `related_incidents`
decoding (step E owns it), no screen-switch history, no window
parameter on `GetWorkload`, no colour added to `BadgeStyle`. commits:
eight subjects under 72 characters, two `model:`, five `app:`, one
`docs:`; no `make generate` anywhere, because nothing under
`api/proto` moves.

Type consistency. `RunCell` (task 1) is the one type the strip, the
matrix and the table row all carry, so `RunStripView`,
`RunMatrixView` and `JobsCard` (task 4) read one shape; `RunOutcome`
(task 1) is what `runCellStyle` (task 3) maps and what the Condition
badge (task 4) takes; `StateTone` and `groupBadge` (step B) decide a
failed run's tone, so a run's colour and its group header's colour
cannot disagree; `BarePodRow` (task 1) is what `barePodRow` and the
chooser (task 3) draw and what `kindCaption`'s count counts;
`verdictSentences` (task 2) takes `Incident`, `Container?` and
`[Artifact]`, which are exactly what `ContainerPane` and `PodCardPane`
hold from `PodDetail` (task 5); `TimelineFold` (task 2) is what
`TimelineView` (task 6) draws after its existing `TimelineFilter`;
`podStateTag(phase:deleted:ready:flips:)` (task 2) is what
`PodPageHeader` (task 7) calls with `readinessFlips` over
`PodHistory.conditions`; `plural` (task 2) is used in tasks 3 to 7 and
in `containerStateLine`, whose test row moves with it; `WorkloadTab`
(existing) is what the tree's context menu (task 3) and
`IncidentsScreen.openWorkload(_:tab:)` (step C) share; `Route` gains
nothing, and `Navigator`'s new `forwardSerial` (task 7) is the same
shape as `backSerial`.
