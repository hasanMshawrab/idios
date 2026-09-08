# m10 step G - the explanation: every screen says what it is

Goal: after this plan a person who has never seen idios can stand on any
screen and find out what they are looking at without leaving it. A small
round [?] sits at the bottom right of every screen; pressing it, Cmd-/ or
"?" dims the screen under a scrim and lights every part of it that has
something to say: an accent outline with a numbered pill at its top-left,
and, under the pointer, a note carrying the region's name, the stored
field, table or endpoint it draws, and one or two sentences on what it
means and why to look there. Over that, a second layer draws the reading
order - three to five larger pills on the regions worth reading in order,
a footer line naming each, and a "Reading flow only" toggle that dims
every outline off the path. Esc or a click on the scrim closes it. The
words are the vocabulary the application already uses: where a region's
title is a state, a category or a kind, its sentence is the tooltip that
word already carries, so a person who reads the overlay and a person who
hovers a badge are told the same thing.

Revision (2026-09-04), after the user reviewed the built overlay: the
coach-mark overlay is replaced by help marks. The scrim, the numbered
pills, the reading path with its footer and its "Reading flow only"
toggle, and the `chrome` and `sidebar` regions are gone, because the
outlined boxes overlapped one another, outlines were drawn over content
that had scrolled away, and lighting every part of a screen at once lit
the whole screen. The [?] now toggles help mode, in which each explained
region carries its own small round "?" - inside its top right corner, or
just outside its trailing edge where the region is too short to hold one
- and clicking a mark outlines that region and opens its note as a
popover; Esc closes help mode. Every table is cut to the major parts of
its screen, which is what section 9.7 now records. Tasks 3 to 8 below
stand as written for the record; the code is the truth.

The texts are data, not view code. `IdiosModel` gains one file holding an
`ExplainedRegion` (id, title, tags, sentence) and one table per screen,
plus the reading path of each; the tables are fixed in
`docs/design/presentation.md` as a new section 9.7, one table per screen,
and a test compares the two where they overlap so they cannot drift. In
the application a `.explained(...)` modifier publishes a view's bounds
through an `anchorPreference` and one overlay at the screen's root reads
every anchor, so a region and its explanation are declared in the same
place and nothing keeps a separate registry of rectangles.

Coverage is enforced, not hoped for. In a DEBUG build the overlay
compares the anchors a screen registered against the ids that screen
declares; a difference either way trips an assertion and prints one line
on stderr, so a region added without an explanation, or an explanation
whose region was deleted, fails the screenshot run rather than shipping
as a hole. `hack/macos/screenshot.sh <route> <png> --explain` photographs
any screen with its overlay up, which is how the texts are reviewed and
how the verification task walks every route.

No proto change, no daemon change, no endpoint. Nothing here reads the
API: every sentence is about data the screen already has on it.

No colour is added. The outline is the accent, which the appearance rule
allows because the overlay is a selection of regions and nothing else;
the scrim is a system material, the pills and the notes are system
materials and the primary text colour, and `BadgeStyle.swift` gains
nothing.

Architecture: what a screen says is decided in `IdiosModel` as data with
a table test - `RegionID`, `ExplainedRegion`, `ReadingStep`,
`ExplainedScreen` and the two lookups over them. The application draws
them: one new file in `macos/idios/Views/Components` holds the preference
key, the `.explained(_:)` and `.explainable(_:)` modifiers, the
`HelpButton` and the `ExplainState` that says whether an overlay is up;
a second holds `ExplainOverlay`, which is the only view that reads
anchors. Every screen then does two things and no more: it hangs
`.explainable(<its screen>)` on its root and `.explained(<id>)` on each
region. Views never import `IdiosAPI`; nothing in this step touches a
store.

Two facts about the window shape the design. A SwiftUI overlay covers the
content view and not the window's toolbar, so the cluster scope menu,
Back and Forward, the title and Copy uid cannot be cut out of the scrim;
they are one region, `chrome`, anchored to a one-point strip the screen
draws at the top of its content, whose note says what sits above the
line. And a sheet and a popover are their own presentations: an overlay
on the screen behind them does not reach them, so each sheet and the menu
bar popover install their own.

Tech stack: Swift 6 package `macos/Sources/IdiosModel` with tests in
`macos/Tests/IdiosModelTests` (`make app-test`); the Xcode application
under `macos/idios` (`make app`; a file under `macos/idios/` joins the
target by existing, so the two new files need no project edit). SwiftUI
only: `anchorPreference`, `overlayPreferenceValue`, `GeometryProxy`,
`Canvas`-free masking through `blendMode(.destinationOut)` inside a
`compositingGroup`, `.onKeyPress` for the bare key and a Help menu item
for the Cmd key. `hack/macos/screenshot.sh` is `/bin/sh`.

Spec: `docs/design/presentation.md` section 9.3 - every screen row gains
the [?] and the overlay, and the new 9.7 holds one explanation table per
screen; 9.4 (the keyboard layer: single letters go through `.onKeyPress`
on the focused screen and Cmd keys are menu items, so "?" is a key press
and Cmd-/ is a Help menu item); 9.5 (the colour rule, and the accent as
selection only); 9.6 (the state, category and kind tooltips, which 9.7
reuses word for word rather than restating); 7.4 (a card's provenance is
its title's hover, which is the same fact the overlay's tag chips draw
where a region is not a card); 7.6 (what idios does not know is said, not
hidden, which is what the capture and sibling regions explain). Roadmap
decision 12 of `docs/plans/m10-reading/roadmap.md`. Visual reference: the
overlay's shape is the approved blueprint of the pod page - twenty-three
regions grouped as window chrome, pod header, left column, middle pane
and right rail, with a five-step reading path - and 9.7's pod page table
is those regions written out.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Swift follows the Go rules: one-line doc comment per
public type and function, comments say why, tests trace to a spec
statement with whole-value assertions, variants are table rows. The only
hard-coded colours live in `BadgeStyle.swift` and this step adds none:
the scrim is `.ultraThinMaterial`, the outline is `Color.accentColor`,
the pills are `.regularMaterial` with `.primary` text and the dimming of
an off-path outline is an opacity. Every time in the application is UTC,
as stored, and no region's sentence claims otherwise.

Nothing in a test, comment, doc or fixture names a real organisation,
cluster, namespace, workload, image or node. The invented names this plan
and its tests use are `checkout-api`, `report`, `nightly`, `worker`,
`idios-smoke`, `api` and `node-a`; the smoke fixtures it checks against
are `smoke-cron-fail`, `smoke-cronjob-retry`, `smoke-crash`, `smoke-oom`,
`smoke-bare`, `smoke-bad-image` and `smoke-missing-config` in namespace
`idios-smoke`.

The application takes the daemon as a launch argument: the Debug build is
started with `nohup macos/DerivedData/Build/Products/Debug/idios.app
/Contents/MacOS/idios -daemon 127.0.0.1:7771 &` after `make app`
(`IDIOS_DAEMON` is read only by `hack/macos/screenshot.sh`). That build
shares its bundle id and its `UserDefaults` with the installed
application: never `defaults write` that domain; override a preference
for one run with an argument-domain flag.

The production daemon on 127.0.0.1:7770 is never touched, read or
screenshotted; every check and every screenshot in this step runs against
a private daemon on 127.0.0.1:7771 and the application is always started
with `-daemon 127.0.0.1:7771`. `make smoke PORT=7771` fills
`.storage/smoke` and `KUBECONFIG=./kube/config kubectl -n idios-smoke
apply -f hack/smoke/` reopens its incidents; then
`./bin/idios -data-dir .storage/smoke -kubeconfig ./kube/config -listen
127.0.0.1:7771 run` serves them. Before any screenshot run
`screencapture -x /tmp/idios-probe.png` and stop if it fails: the
terminal needs Screen Recording permission.

Checkpoint before every commit: `go build ./... && go test ./... &&
make ascii && make app-test && make app`. No task changes `api/proto`, so
`make generate-check` and `rm -rf macos/.build` are not part of this
step. Implementers never commit; nothing is committed without the user's
review of the diff. The decisions are made; where a sample and a sentence
of 9.7 differ, the sentence wins and the doubt goes in the self-review.

## File structure

    docs/design/presentation.md                          9.3's [?], 9.4's key, the new 9.7 (task 1)
    macos/Sources/IdiosModel/Explain.swift               RegionID, ExplainedRegion, ReadingStep, the tables (task 2)
    macos/Tests/IdiosModelTests/ExplainTests.swift       the audit, the pod table, the vocabulary agreement (task 2)
    macos/idios/Views/Components/Explain.swift           the preference key, the modifiers, HelpButton, ExplainState (task 3)
    macos/idios/Views/Components/ExplainOverlay.swift    the scrim, the outlines, the notes, the reading path (task 3)
    macos/idios/Views/PodPage/PodPageScreen.swift        explainable, the chrome strip (task 3)
    macos/idios/Views/PodPage/PodPageHeader.swift        the header's regions (task 3)
    macos/idios/Views/PodPage/PodColumn.swift            the pod and container cards (task 3)
    macos/idios/Views/PodPage/ContainerPane.swift        the middle pane's regions (task 3)
    macos/idios/Views/PodPage/PodCardPane.swift          the pod card's tabs (task 3)
    macos/idios/Views/PodPage/PodPageRail.swift          the rail's four groups (task 3)
    macos/idios/App/IdiosApp.swift                       the Help menu item, ExplainState in the environment (task 3)
    macos/idios/Views/Incidents/IncidentsScreen.swift    explainable on the split view (task 4)
    macos/idios/Views/Incidents/IncidentsSidebar.swift   the sections and the legend button (task 4)
    macos/idios/Views/Incidents/IncidentsList.swift      the summary, the rows, the empty state (task 4)
    macos/idios/Views/Incidents/ListColumns.swift        the column header (task 4)
    macos/idios/Views/Search/SearchPalette.swift         its own overlay (task 4)
    macos/idios/Views/Workloads/WorkloadsScreen.swift    the tree and the pane header (task 5)
    macos/idios/Views/Workloads/WorkloadDetailView.swift the tabs and their cards (task 5)
    macos/idios/Views/Workloads/RunStripView.swift       the strip (task 5)
    macos/idios/Views/RunPage/RunPageScreen.swift        explainable and the middle (task 6)
    macos/idios/Views/RunPage/RunPageHeader.swift        the tag, counts and breadcrumb (task 6)
    macos/idios/Views/RunPage/AttemptsCard.swift         the attempt lines (task 6)
    macos/idios/Views/RunPage/RunPageRail.swift          the rail (task 6)
    macos/idios/Views/Status/StatusScreen.swift          its cards (task 6)
    macos/idios/Views/MenuBar/MenuBarView.swift          the popover's own overlay (task 7)
    macos/idios/Views/Clusters/AddClusterSheet.swift     the sheet's own overlay (task 7)
    macos/idios/Views/Clusters/ClustersSheet.swift       the sheet's own overlay (task 7)
    macos/idios/Views/IncidentDetail/NoteSheet.swift     the sheet's own overlay (task 7)
    macos/idios/Views/IncidentDetail/AskAISheet.swift    the sheet's own overlay (task 7)
    macos/idios/App/Screenshot.swift                     -explain and the coverage line (task 8)
    hack/macos/screenshot.sh                             the --explain flag (task 8)
    CLAUDE.md                                            the screenshot usage line (task 8)
    docs/plans/m10-reading/roadmap.md                    step G's status line (task 9)

## Task 1 - the spec learns the explanation

`docs/design/presentation.md`. Section 9.3's preamble gains one sentence
before the table: "Every screen carries a round [?] at its bottom right -
in the menu bar popover's action row and beside a sheet's Cancel, where
there is no bottom right - which dims the screen and lights each part of
it that Section 9.7 explains; Cmd-/ opens it from the Help menu and "?"
opens it when no text field has focus."

Section 9.4's keyboard paragraph gains, after the sentence about "/"
focusing the filter field: ""?" opens the screen's explanation the same
way, and Cmd-/ is the Help menu's Explain This Screen, because a menu is
where a person discovers it."

Section 9.7 is new, after 9.6 and before Section 10, and is the whole
text of the feature:

"### 9.7 Explanations

The [?] on every screen opens one overlay: the screen dimmed under a
scrim, each explained region showing through at full brightness under an
accent outline with a numbered pill outside its top-left corner, and, for
the region under the pointer or pinned by a click, a note beside it
carrying the region's name, the stored fields, tables or endpoints it
draws as monospace chips, and one or two sentences on what it means and
why to look there. Over the outlines a reading path draws three to five
larger pills on the regions worth reading in order with a footer line
naming each, and a Reading flow only toggle dims every outline off the
path. Esc or a click on the scrim closes it. The outline is the accent,
which is selection only and this is a selection of regions; nothing else
in the overlay carries a colour. A screen whose content depends on what
is selected has one table per selection state, and the overlay draws the
state in view. Where a region's name is a state, a category or a kind,
its sentence is that word's tooltip from Section 9.6 unchanged, so the
overlay and the tooltip never say two different things. The window's
toolbar is drawn above the content and cannot be dimmed: the scope menu,
Back and Forward, the title and Copy uid are one region at the top of the
content whose note says what sits above the line."

Then one table per screen, `| Region | Draws | What the note says |`. The
pod page with a container selected is written in this task, because its
regions are the ones the approved blueprint fixed; the other tables are
written by the task that draws their screen, so no table describes a
region nothing registers. The pod page's table, in the order the pills
are numbered:

| Region | Draws | What the note says |
|---|---|---|
| Window chrome | `GET /clusters`, `Cmd-[`, `Cmd-]`, `pods.uid` | Above this line sit the cluster scope the lists are filtered by, kept across screens and launches and drawn red where a cluster has a stored connection error; Back, which returns to whatever opened this page, and Forward beside it; the pod's name, display only; and Copy uid, which puts the raw uid on the clipboard, because that is the value kubectl and an agent take and a name alone does not identify one pod over time. |
| Sidebar, Screens only | `GET /incidents/counts` | Screens stays while a pod page is pushed, so the list is one click away. The View and Category filters do not stay: they narrow a list, and this is one pod. |
| Pod state tag | `pods.phase`, `conditions[Ready]`, `deleted_at` | Look here first. The kubelet keeps reporting phase Running while every container fails, so the Ready condition is what says the pod is not serving. It reads LOOPING when readiness flipped more than three times in the window, and DELETED once the pod is gone. |
| Counts line | `containers`, `incidents`, `pods.node_name` | How many containers, how many incidents and how many are still open, then the node. One open incident out of six containers is a different morning from six out of six, and a node repeated across failing pods is the next thing to check. |
| Title | `pods.name` | The subject of the page, never elided and always selectable. Detail pages show names whole; only lists shorten them to the suffix after the workload prefix. |
| Identity breadcrumb | `cluster_id`, `namespace`, `workload_kind`, `controller_kind` | The stored identity chain, cluster to namespace to workload to controller, stopping before the pod. Each segment opens that level, so this is the way to ask whether the neighbours are failing too. |
| Pod card | `GET /pods/{uid}`, `artifacts` | Selecting it swaps the middle pane to the pod's own material: every container's events, the conditions and their history, the files, and the captured pod object. Its subline counts what idios actually kept, so you know before clicking whether there is anything to read. |
| Container cards | `containers.kind`, `category`, `occurrences` | One card per container, init then app then sidecar then ephemeral, the selected one filled with the accent. Each badge is one incident with its category and how many times it has happened, so this column says whether one container is failing or the whole pod is. |
| Action row | `POST /incidents/{id}/acknowledge`, `/incidents/{id}/prompt` | Acknowledge is the one primary button and shows its key, so the letter is learned where it is used; it says a person has seen this, not that it is fixed. Note writes a sentence that outlives you, Ask AI copies a prompt or a sanitized snapshot, and the rest sit under Actions. |
| Verdict block | `occurrences`, `mem_limit_bytes`, `capture_gap`, `image_id` | Read this second, and often stop here: sentences built from fields the page already holds. What happened and how often since when, the memory limit when the kill was an OOM, what was and was not captured, and whether the image digest changed since the incident opened. |
| Category, state, id and occurrences | `incidents.category`, `incidents.state`, `occurrences` | The category is what kind of failure this is; the state is whether anyone has dealt with it and for how long it has run. Occurrences is the restarts attached to this one incident: a crash loop is one incident with a climbing number, never forty incidents. |
| Incident sentence | `container_name`, `exit_code`, `last_reason` | The failure stated as a person would say it: which container of which owner, the exit code, and the reason the kubelet last gave. It is the line to paste into a message when you hand the problem on. |
| Explanation | `k8s_events`, `last_message` | The kubelet's or a controller's own wording for the same failure, kept because it often names the missing secret, the unreachable registry or the failing probe. Its provenance is on hover, so an observation is never mistaken for an inference. |
| Tab strip | `/incidents/{id}/timeline`, `artifacts` | Overview is the facts, the files and the container's events; Timeline is the incident's window with repeating cycles folded into one entry; Logs gives the capture card the whole pane and counts the files that exist. Switching the lit incident changes the header and the timeline, nothing else. |
| What the kubelet reports | `restart_count`, `last_terminated_reason`, `image_id`, `mem_limit_bytes` | Read this third: the evidence the verdict was built from, in the kubelet's own words. Restart count is the container's whole life while occurrences above is this incident's share of it, and the image and digest are the ones recorded when the incident opened, not the current ones. |
| Captured logs | `artifacts`, `capture_gap`, `capture_note` | Read this fourth. The scrubber walks one file per dead instance, oldest to newest, so you can see whether every attempt failed the same way. A missing file is never an empty pane: the gap is named and the API's own words are quoted, which separates a container that printed nothing from a log that could never have been fetched. |
| Owner chain | `workload_kind`, `controller_kind`, `pod_uid`, `container_name` | Read this last, when the verdict is not enough: four links from what decides how many copies run down to the process that failed, each with the uid that identifies it. It answers who will replace this pod, and whether the thing to change is this container or the workload above it. |
| Context | `node_name`, `qos_class`, `image_id`, `container_id` | Where it ran and which build it ran. The image id is the resolved digest, which is what settles same tag, different image; identifiers are middle-elided so both ends stay readable and the raw value is what gets copied. |
| Times | `opened_at`, `last_seen_at`, `acknowledged_at` | The times of whatever is selected: the lit incident's, or the pod's when nothing is lit. Each is labelled k8s or observed, which matters when a laptop slept, since one is when it happened and the other is when idios saw it. Every time is UTC. |
| Siblings | `controller_uid`, `category` | This pod first, then the others of the same controller, with a category badge where one is failing and a readiness word where none is. One red dot among green is this pod's problem; all red is the image, the config or the cluster. |

Under the table, the reading path as a sentence: "Reading path: tag and
counts, is the pod serving and how much is broken; verdict, the answer in
sentences; kubelet facts, the evidence behind it; captured logs, what the
process said; rail, owner, place, time and siblings."

Then the pod page with the Pod card selected, which is five regions -
Events, Conditions, Files, pod.json and Related incidents - each saying
what its tab holds and why it is the pod's and not a container's, with
Related incidents naming `job_uid` and saying it exists only for a Job's
pod.

Rules. Every sentence is written for someone who has not read this
document: no section number, no table name that is not also a chip, no
"see above". A region whose name is a state, a category or a kind carries
9.6's tooltip verbatim and the table says so rather than restating it.
The chips are the stored names with their underscores kept, because a
chip is the value a person greps for.

No code changes in this task, so no test. The check is the read: 9.7
describes an overlay nothing has built yet, which is what makes the next
tasks answerable, and every sentence in it is read against 9.5 (nothing
claims a colour), 9.6 (no word is redefined) and 7.6 (a region that
explains an absence says what is absent).

Check: `hack/ascii-check docs/design/presentation.md`.

Checkpoint: `go build ./... && go test ./... && make ascii &&
make app-test && make app`.
Commit: `docs: spec the per-screen explanation overlay`.

Consumes: 9.3, 9.4, 9.5, 9.6.
Produces: 9.7, the pod page's two tables and the reading path every later
task quotes.

## Task 2 - what each screen says

`macos/Sources/IdiosModel/Explain.swift`. `Explanation` is taken: it is
the incident's own sentence and its provenance, and this file must not
shadow it. The region type is named for what it is.

    /// RegionID names one explained region of one screen; a literal is
    /// enough because a screen's regions and its explanations are compared
    /// at run time, which catches a typo the compiler cannot.
    public struct RegionID: Hashable, Sendable, ExpressibleByStringLiteral,
        CustomStringConvertible
    {
        public let raw: String
        public init(_ raw: String)
        public init(stringLiteral value: String)
        public var description: String { raw }
    }

    /// ExplainedRegion is one part of a screen and what the overlay says
    /// about it: the name a person reads, the stored names it draws, and
    /// why to look there.
    public struct ExplainedRegion: Identifiable, Hashable, Sendable {
        public let id: RegionID
        public let title: String
        /// tags are the stored fields, tables and endpoints behind the
        /// region, drawn as monospace chips, because the name of the thing
        /// is what a person greps for afterwards.
        public let tags: [String]
        public let sentence: String

        public init(id: RegionID, title: String, tags: [String], sentence: String)
    }

    /// ReadingStep is one stop on a screen's reading path: the region to
    /// look at and the few words that say why it is that one.
    public struct ReadingStep: Identifiable, Hashable, Sendable {
        public let id: RegionID
        public let label: String
        public let gloss: String

        public init(id: RegionID, label: String, gloss: String)
    }

    /// ExplainedScreen is a screen as the overlay knows it: a screen whose
    /// regions depend on what is selected is one value per selection, so
    /// the set drawn and the set declared can be compared.
    public enum ExplainedScreen: Hashable, Sendable {
        case incidents(ListState)
        case palette
        case workloads(WorkloadsPart)
        case podCard(PodPane)
        case container(ContainerTab)
        case run(RunPart)
        case status
        case menuBar
        case sheet(SheetKind)
    }

    /// ListState is the incidents list with rows and the incidents list
    /// with none; an empty view explains itself and nothing else.
    public enum ListState: String, CaseIterable, Hashable, Sendable {
        case rows, empty
    }

    /// WorkloadsPart is which half of the Workloads screen is explained,
    /// and which tab of the detail, because the tabs share no region.
    public enum WorkloadsPart: String, CaseIterable, Hashable, Sendable {
        case tree, overview, pods, runs, rollouts, incidents
    }

    /// RunPart is the Run page's tab, which decides the middle pane's
    /// regions and nothing else.
    public enum RunPart: String, CaseIterable, Hashable, Sendable {
        case overview, timeline, logs, events
    }

    /// SheetKind is a sheet that carries its own overlay, because a sheet
    /// is its own presentation and the screen behind it cannot reach it.
    public enum SheetKind: String, CaseIterable, Hashable, Sendable {
        case addCluster, clusters, note, askAI
    }

    /// explainedScreens is every screen the overlay can be opened on, which
    /// is what the audit walks.
    public let explainedScreens: [ExplainedScreen]

    /// explainedRegions is what a screen says about itself, in the order
    /// the pills are numbered: the chrome, then the screen top to bottom
    /// and left to right.
    public func explainedRegions(for screen: ExplainedScreen) -> [ExplainedRegion]

    /// readingOrder is the path through a screen for someone who does not
    /// know where to start; empty for a screen with one thing on it.
    public func readingOrder(for screen: ExplainedScreen) -> [ReadingStep]

    /// ScreenAudit is what a screen's tables must satisfy: it says a set
    /// exists, that no id repeats, that every step names a region and that
    /// no sentence or title is missing.
    public struct ScreenAudit: Hashable, Sendable {
        public let regions: Int
        public let uniqueIDs: Bool
        public let stepsNameRegions: Bool
        public let everyRegionSpeaks: Bool
        public let steps: Int
    }

    /// audit reads a screen's tables into the values a test compares.
    public func audit(_ screen: ExplainedScreen) -> ScreenAudit

Rules. Only the two pod page screens carry their whole tables in this
task; every other case answers an empty array until the task that draws
it fills it in, and `explainedScreens` lists it from the start so the
audit test fails loudly for a screen nobody has written yet. That is the
one place in this step where a task ships a red-looking table on purpose,
so the audit test is written in this task to accept an empty set only for
a screen with no reading path, and the later tasks tighten nothing: they
add rows.

`explainedRegions` is a `switch` over a set of `private let` arrays, one
per screen, so the tables read as tables. A region whose title is a
state, a category or a kind takes its sentence from `IncidentState`,
`Category` or `kindWords` rather than repeating the string, which is what
makes the vocabulary test able to compare them at all.

`readingOrder` is at most five steps, because a path a person has to
count is not a path; a screen with one card has none.

Tests, Swift Testing, table-driven, whole-value, edge cases first, in
`macos/Tests/IdiosModelTests/ExplainTests.swift`.

- `everyScreenExplainsItselfAndNamesItsReadingPath` (traces to 9.7's
  "The [?] on every screen opens one overlay" and "a reading path draws
  three to five larger pills"), a `@Test(arguments: explainedScreens)`
  comparing the whole `ScreenAudit` against the expected value for that
  screen. Edge cases lead the argument list: a screen with an empty
  table, a screen with one region and no path, a screen whose path is
  five steps. The audit is compared whole rather than field by field,
  because a table that gains a duplicated id and loses a region would
  pass two of four separate assertions.
- `thePodPageSaysTheSameWordsTheOverlayWasApprovedWith` (traces to 9.7's
  pod page table): the whole `[ExplainedRegion]` of
  `.container(.overview)` against the twenty expected values, and the
  whole array of `.podCard(.events)` against its five. This is the test
  that keeps the doc and the code from disagreeing about the text, and
  it earns its place for the same reason the kind vocabulary's does:
  nothing else compares them.
- `noExplanationRewritesAWordTheApplicationAlreadyDefines` (traces to
  9.7's "its sentence is that word's tooltip from Section 9.6
  unchanged"): over every region of every screen, the whole set of
  regions whose title matches a state title, a category label or a kind
  word and whose sentence is not that word's tooltip, compared against
  the empty set. Edge cases: a title that matches a kind word in a
  different case, a title that is a category's wire value rather than its
  label, and a region titled with a word that is deliberately not a
  vocabulary word (Verdict block), which must not be swept in.

Check: `make app-test` green. No view reads these yet, so `make app`
proves only that the package still builds into the application.

Checkpoint as task 1.
Commit: `model: add the per-screen explanation tables`.

Consumes: `IncidentState.tooltip`, `Category.tooltip`, `kindWords`,
`PodPane`, `ContainerTab`.
Produces: `RegionID`, `ExplainedRegion`, `ReadingStep`,
`ExplainedScreen`, `ListState`, `WorkloadsPart`, `RunPart`, `SheetKind`,
`explainedScreens`, `explainedRegions`, `readingOrder`, `ScreenAudit`,
`audit`.

## Task 3 - the overlay, and the pod page as its first screen

`macos/idios/Views/Components/Explain.swift`:

    /// ExplainAnchors carries every explained region's bounds up to the
    /// screen's root, which is the only view that sees all of them.
    struct ExplainAnchors: PreferenceKey {
        static let defaultValue: [RegionID: Anchor<CGRect>] = [:]
        static func reduce(
            value: inout [RegionID: Anchor<CGRect>],
            nextValue: () -> [RegionID: Anchor<CGRect>])
    }

    /// HelpButtonPlacement is where the [?] sits: its own corner on a
    /// screen, and a screen's button row where a popover or a sheet has no
    /// free bottom right.
    enum HelpButtonPlacement { case bottomTrailing, inline }

    /// HelpButton is the round [?] that opens the screen's explanation.
    struct HelpButton: View {
        let action: () -> Void
    }

    /// ExplainState is whether an explanation is up and which region is
    /// pinned; one per window, because a sheet's overlay and the overlay
    /// of the screen behind it must never be up together.
    @Observable @MainActor
    final class ExplainState {
        private(set) var screen: ExplainedScreen?
        var pinned: RegionID?
        var flowOnly = false

        /// openAtLaunch is the -explain argument, which opens whichever
        /// screen the -route argument landed on, so a screenshot run needs
        /// no keystroke.
        let openAtLaunch: Bool

        init(arguments: [String] = CommandLine.arguments)

        func open(_ screen: ExplainedScreen)
        func toggle(_ screen: ExplainedScreen)
        func close()
    }

    extension View {
        /// explained claims this view as a region the screen's overlay
        /// lights up; the id must name one of that screen's explanations,
        /// which is checked when the overlay opens.
        func explained(_ id: RegionID) -> some View

        /// explainable installs a screen's [?] and its overlay at the
        /// screen's root, and is the only place an anchor is read.
        func explainable(
            _ screen: ExplainedScreen,
            placement: HelpButtonPlacement = .bottomTrailing) -> some View
    }

Rules. `explained` is
`.anchorPreference(key: ExplainAnchors.self, value: .bounds) { [id: $0] }`
and nothing else: it draws no border, changes no layout, and costs a
laid-out view one preference entry whether or not an overlay is ever
opened. `reduce` merges by keeping the later value, because a region
drawn twice is a bug the coverage line names rather than a crash.

`explainable` wraps the content in
`.overlayPreferenceValue(ExplainAnchors.self) { anchors in GeometryReader
{ proxy in ... } }`, draws the `HelpButton` when `state.screen == nil`,
and draws `ExplainOverlay` when `state.screen == screen`. The [?] is
hidden while an overlay is up, because a control a scrim covers is a
control that looks broken. `.bottomTrailing` puts the button in a
`.overlay(alignment: .bottomTrailing)` with 12 points of padding;
`.inline` draws nothing and leaves the caller to place a `HelpButton` in
its own row.

The keys. `.onKeyPress(keys: ["?"])` on the same focused view that
already takes `a`, `d` and `r`, because a bare letter must not fire while
a text field has focus; Esc is `.onKeyPress(.escape)` on the overlay,
which is focused when it appears so the key reaches it before the screen
under it. Cmd-/ is a menu item, added in `IdiosApp`'s commands as
`CommandGroup(replacing: .help) { Button("Explain This Screen") { ... }
.keyboardShortcut("/", modifiers: .command) }`, which is where a person
looks for it; the button sets a serial on `ExplainState` that the
frontmost `explainable` reads, so the menu opens whichever screen is in
front without the menu knowing which that is.

`macos/idios/Views/Components/ExplainOverlay.swift`:

    /// ExplainOverlay is one screen's explanation drawn over it: the scrim
    /// with a hole per region, the outlines and their pills, the note
    /// beside the region under the pointer, and the reading path.
    struct ExplainOverlay: View {
        let screen: ExplainedScreen
        let anchors: [RegionID: Anchor<CGRect>]
        let proxy: GeometryProxy
    }

Rules. The scrim is a `Rectangle().fill(.ultraThinMaterial)` with one
`RoundedRectangle(cornerRadius: 5)` per region drawn over it in
`.blendMode(.destinationOut)`, the whole thing in a `.compositingGroup()`
so the holes cut rather than lighten; a click on it calls `close`. Each
region draws a `RoundedRectangle(cornerRadius: 5).strokeBorder(
Color.accentColor, lineWidth: 1.5)` at its rect and a pill at the rect's
top-left corner, offset outside it, carrying the region's number in
`.regularMaterial` with `.primary` text. Hover sets a local
`hovered: RegionID?`; a click sets `state.pinned`, and a second click on
the same region clears it, so a trackpad-less run can still read a note.

The note is a 270-point `VStack` on `.regularMaterial` with a 10-point
corner radius, holding the title, a wrapping row of tag chips in
11-point monospaced on `.quaternary`, and the sentence in 12.5-point
`.secondary`. It is placed on whichever side of the region has room:
right when `rect.maxX + 14 + 270 <= proxy.size.width`, left otherwise,
and clamped so it never leaves the content.

The reading path is drawn above the outlines: a 26-point pill per
`ReadingStep` at its region's top-left, and a footer bar along the
bottom, above the [?]'s corner, holding one `Text` per step - the number
in a pill, the label in `.primary` semibold, the gloss in `.secondary`.
A `Toggle("Reading flow only", isOn: $state.flowOnly)` sits at the
footer's leading edge; while it is on, every outline and pill whose id is
not on the path drops to `.opacity(0.28)` and returns to full on hover,
which is how a person checks one of them without leaving the path.

A region the screen registered no anchor for is not drawn, and a region
drawn with no explanation gets an outline with no pill and no note: the
overlay stays usable while the coverage line, which task 8 adds, says
what is wrong. Task 8 is where that becomes an assertion; this task draws
it and no more.

The pod page. `PodPageScreen.loaded(_:)` hangs
`.explainable(explainScreen)` on its outer `VStack`, where

    /// explainScreen is which table the overlay draws: the pod page has
    /// one set of regions per selection, because a container's pane and
    /// the pod card's share nothing but the header.
    private var explainScreen: ExplainedScreen

reads `selection`, answering `.container(tab)` for a container and
`.podCard(pane)` for the pod card. A one-point
`Color.clear.frame(height: 1).explained("chrome")` is the first child of
that `VStack`, so the toolbar's note has somewhere to hang. Then
`.explained(...)` goes on: `PodPageHeader`'s tag badge, counts line,
title and identity line; `PodColumn`'s pod card and its container card
stack; `ContainerPane`'s `PaneActionsRow`, verdict block, tag row,
`IncidentHeader` sentence, explanation, tab strip, `KubeletCard` and
`CapturedLogsCard`; `PodCardPane`'s five tabs; `PodPageRail`'s four
groups. The sidebar's region is task 4's, because the sidebar is
`IncidentsScreen`'s and is shared.

No test in this task: the decisions are the tables of task 2, and
`.ai/tests.md` keeps a test off a view and off a layout budget.

Check against the smoke store on 7771, the application started with
`-daemon 127.0.0.1:7771`. Open `smoke-crash`'s pod page and press "?":
the page dims, twenty regions light up, the pills number down the page,
hovering the verdict block shows its note on the left because the block
is wide, hovering the rail's Times shows its note on the left and never
off the window, and the five reading pills sit on the tag, the verdict,
the kubelet card, the logs and the owner chain with the footer naming
each. Toggle Reading flow only: the other fifteen dim and come back on
hover. Esc closes; the [?] reopens; a click on the scrim closes. Select
the Pod card: the overlay's regions change to the five tabs. Press "?"
while the filter field has focus on the list behind: nothing opens, which
is the rule the bare letter exists for. Do the same on `smoke-oom`'s pod
page, where the verdict names a memory limit, and on `smoke-bare`, whose
owner chain is one link. Screenshot `pod/<uid>` with the overlay up for
the diff review.

Checkpoint as task 1.
Commit: `app: explain the pod page under a coach-mark overlay`.

Consumes: `ExplainedScreen`, `explainedRegions`, `readingOrder`,
`PodPageScreen`, `PodPageHeader`, `PodColumn`, `ContainerPane`,
`PodCardPane`, `PodPageRail`, `IdiosApp`.
Produces: `ExplainAnchors`, `explained(_:)`, `explainable(_:placement:)`,
`HelpButton`, `HelpButtonPlacement`, `ExplainState`, `ExplainOverlay`,
the Help menu item, and the pod page explained.

## Task 4 - the list and the palette

`macos/Sources/IdiosModel/Explain.swift` gains the tables for
`.incidents(.rows)`, `.incidents(.empty)` and `.palette`;
`docs/design/presentation.md` 9.7 gains the same three tables. Every
sentence is written once, in the model, and copied to the doc; the
vocabulary test is what keeps a state's or a category's sentence from
being written a second way.

`.incidents(.rows)`, twelve regions: the chrome strip (the scope menu,
the group-by menu, the View menu and the filter field, which live in the
toolbar the overlay cannot dim); the sidebar's Screens section; its View
section with the counts; the legend [?] beside the View heading, whose
note says it defines the words and this one explains the screen; the
Category section and its folded "N more with none" row; the summary line
with Collapse all and Acknowledge all; the column header; a group header;
a run row; a rollup row; a pod row; and the "+ N more runs" line. The
reading path is five steps: the View section, the summary line, a group
header, a row, the state badge on it.

`.incidents(.empty)`, three regions: the empty view's title, its sentence
naming the state's definition and the key that produces it, and the
button back to Attention. No reading path: there is one thing on the
screen.

`.palette`, six regions: the query field with the prefixes `ns:`,
`node:`, `tag:` and `reason:`; the section headings Pods, Runs,
Workloads, Incidents and Commands; a hit row and what its trailing text
draws; the footer's keys, Return, Cmd-Return and Shift-Return; the recent
pages an empty query lists; and the command list that makes the keyboard
layer visible. The reading path is four steps: field, sections, hit,
keys.

`macos/idios/Views/Incidents/IncidentsScreen.swift`. `.explainable(...)`
goes on the `NavigationSplitView` and not on the detail column, because
the sidebar's anchors have to reach it; the screen it names is
`.incidents(rows.isEmpty ? .empty : .rows)` while `path.isEmpty` and
`screen == .incidents`, and nothing while a pod page, a run page,
Workloads or Status is up, since each of those installs its own. The
one-point chrome strip is the first child of `content`.

`IncidentsSidebar.swift` marks its three sections and the legend button.
`IncidentsList.swift` marks the summary line and, inside the row
builders, the first group header, the first run row, the first rollup row
and the first pod row it draws, plus the "+ N more runs" line and the
empty view's three parts. Marking the first of each kind is deliberate
and is a rule, not a shortcut: a `List` builds rows lazily and recycles
them, so marking every row would make the overlay's region set depend on
the scroll position, and the note is about the shape of a row and not
about one incident. The overlay therefore explains the first row of each
kind that is on screen, and the coverage check of task 8 treats a row
kind that is not on screen at all - a list with no rollup in it - as
absent rather than missing, which is the one exemption in that check.

`ListColumns.swift` marks `ColumnHeaderRow`.

`macos/idios/Views/Search/SearchPalette.swift` installs its own
`.explainable(.palette, placement: .inline)` on its panel, with the [?]
in the footer beside the keys, because the palette is itself an overlay
and a second one anchored to the screen behind it would be dimmed by its
own scrim.

No test: the tables are data and are covered by task 2's audit and
vocabulary tests, which run over every screen and pick these up as they
are filled in.

Check against the smoke store on 7771. On the list, "?" lights twelve
regions; the sidebar's regions are outlined although the sidebar is a
different column of the split view; the note for the group header sits to
its right and the note for the state badge to its left; the reading path
runs down the left and across a row. Filter to a state with nothing in it
and press "?": three regions, no path. Cmd-K, then "?" inside the
palette: the palette's own six regions light and the list behind stays
dim under one scrim, not two. Type into the palette's field and press
"?": the character is typed, not swallowed. Screenshot `incidents` and
`incidents/dismissed` with the overlay up for the diff review.

Checkpoint as task 1.
Commit: `app: explain the incidents list and the palette`.

Consumes: `explainable`, `explained`, `IncidentsScreen`,
`IncidentsSidebar`, `IncidentsList`, `ListColumns`, `SearchPalette`.
Produces: the three tables and the two screens explained.

## Task 5 - Workloads

`macos/Sources/IdiosModel/Explain.swift` and 9.7 gain six tables, one per
`WorkloadsPart`.

`.tree`, seven regions: the chrome strip; the cluster and namespace
levels; a kind caption with its count and the fixed order, long-lived
first; a workload row with its trailing open-incident count; the bare
pods caption and the one-row-per-name rule with "N pods" where a deleted
pod of the same name is kept; the row's context menu, whose note says
what it holds since a menu cannot be outlined; and the pane header with
Collapse all and Expand all. Path: three steps.

`.overview`, five regions: the window control, the stat cards, the
restarts chart with its y-axis and its dashed hours, the rollouts card
and the incidents count. `.pods`, four: the Live / not-running /
open-incident chips, the table, the truncation line and the row's link
into a pod page. `.runs`, six: the run strip and what a dashed cell
means, the Failed default, the suffix naming, the folded consecutive
runs, the Condition badge taking the incident's state, and the link into
a run. `.rollouts`, three. `.incidents`, two.

`macos/idios/Views/Workloads/WorkloadsScreen.swift` installs
`.explainable(...)` once, on the screen's `HStack`, naming `.tree` while
no workload is selected and the selected tab's part otherwise, so a
person on the Runs tab is told about runs. `WorkloadDetailView.swift`
marks its tab strip and each tab's cards; `RunStripView.swift` marks the
strip itself, once, on the strip's container rather than on a cell.

No test, for the reason task 4 gives.

Check against the smoke store on 7771: on Workloads with nothing
selected, "?" lights the tree's seven regions and the kind captions read
with their counts; select `smoke-cron-fail` and the overlay changes to
the Runs tab's six, with the strip outlined whole and not cell by cell;
switch to Overview and the regions change again; the context menu's note
is readable although no menu is open. Screenshot `workloads` and
`workload/<cluster>/idios-smoke/CronJob/smoke-cron-fail` with the overlay
up for the diff review.

Checkpoint as task 1.
Commit: `app: explain the workloads tree and detail`.

Consumes: `WorkloadsScreen`, `WorkloadDetailView`, `RunStripView`,
`WorkloadTab`.
Produces: the six tables and Workloads explained.

## Task 6 - the Run page and Status

`macos/Sources/IdiosModel/Explain.swift` and 9.7 gain five tables.

`.run(.overview)`, eleven regions: the chrome strip; the state tag as the
Job's condition and never its counters; the counts line and why attempts
and incidents are separate words; the breadcrumb; the actions row and
that Acknowledge acts on every incident of the run; the verdict; the
Attempts card and its footer about pods the Job counted and idios never
saw; the Job card; the run strip with this run marked; the rail's owner
chain; and its other runs. `.run(.timeline)`, `.run(.logs)` and
`.run(.events)` are the same header regions with the tab's own content
region in place of the Overview cards, which is three tables of six.
Path: five steps, tag and counts, verdict, attempts, job card, rail.

`.status`, eight regions: the header and what the daemon is; the clusters
health card and the one rule that colours a cluster dot; the process
cards and what a queue depth means; the artifact outcomes; storage; the
sweep card and that retention is why a row goes away; the incident
totals; and the configuration values shown in human units. Path: four
steps.

`macos/idios/Views/RunPage/RunPageScreen.swift` installs
`.explainable(.run(pane))` on its outer `VStack` with the one-point
chrome strip first, and marks the middle's regions;
`RunPageHeader.swift`, `AttemptsCard.swift` and `RunPageRail.swift` mark
theirs. `macos/idios/Views/Status/StatusScreen.swift` installs
`.explainable(.status)` and marks its six cards and its header.

No test, for the reason task 4 gives.

Check against the smoke store on 7771: open a run of `smoke-cron-fail`,
press "?", and the eleven regions light with the strip's marked cell
inside the strip's outline; switch to Logs and the Attempts, Job and
strip regions give way to one Logs region while the header's stay;
`smoke-cronjob-retry`, the standalone Job, lights the same set and its
tag note still reads as written, because the note is about the tag and
not about this Job. On Status, "?" lights eight regions and the note for
the sweep card names retention. Screenshots `run/<uid>` and `status` with
the overlay up for the diff review.

Checkpoint as task 1.
Commit: `app: explain the run page and status`.

Consumes: `RunPageScreen`, `RunPageHeader`, `AttemptsCard`,
`RunPageRail`, `StatusScreen`, `RunPane`.
Produces: the five tables and the two screens explained.

## Task 7 - the menu bar and the sheets

Dropped on 2026-09-04, on the user's decision during the build: the menu
bar popover and the four sheets get no explanation overlay. A 360-point
popover and a form have no room for a dimmed screen and a 270-point note,
and their controls already carry tooltips. `.menuBar` and `.sheet` are
not cases of `ExplainedScreen`, 9.3's sentence names the window's screens
and the palette only, and task 8's route list loses `menubar`,
`addcluster` and `clusters`. The text below is kept as written for the
record and is not to be built.

`macos/Sources/IdiosModel/Explain.swift` and 9.7 gain five tables.

`.menuBar`, five regions: the headline as the open count and that the
Dock badge follows it; the subline as the attention count and what
attention means; a workload row and that it is the same fold the list
draws; the closed line; and the action rows. Path: three steps.

`.sheet(.addCluster)`, four: the context list read from the daemon's
kubeconfig, the friendly name, the namespace picker and its free-text
fallback when the Role cannot list, and that picking a namespace starts
watching it at once. `.sheet(.clusters)`, five: the rail and the red dot
rule, the Cluster section, Watched namespaces, the Grafana label builder
and its preview, and Remove behind a confirmation.
`.sheet(.note)`, two: the field as one free-text value and not a thread,
and that an empty note clears it. `.sheet(.askAI)`, three: what the
prompt is, what the snapshot is and that it goes through the sanitizer,
and that neither leaves the machine on its own. No path on a sheet with
four regions or fewer; `.sheet(.clusters)` gets three steps.

Every one of the five installs `.explainable(..., placement: .inline)` on
its own root and puts a `HelpButton` in its button row, to the leading
side of Cancel, because a sheet has no free bottom right and a floating
[?] over a form reads as a field. The menu bar popover's button sits in
the action row beside "Status...", for the same reason. Esc is the
sheet's own dismissal: while an overlay is up the overlay takes Esc
first and the second Esc closes the sheet, which is stated here because
it is the one place two Esc handlers stack.

`MenuBarView.swift` also has to survive the screenshot route: the
`-route menubar` window draws `MenuBarView` as a plain view rather than
in a popover, and `.explainable` behaves the same either way, which is
what makes `--explain` able to photograph it.

No test, for the reason task 4 gives.

Check against the smoke store on 7771: open the menu bar extra, press its
[?], and five regions light inside the 360-point popover with the notes
clamped inside it rather than spilling over the screen; the row note
names the fold. Open Clusters and namespaces, press its [?], and five
regions light over the sheet while the window behind stays as it is;
press Esc once and the overlay closes, twice and the sheet does. The same
for Add cluster, for the note sheet from a pod page and for Ask AI.
Screenshots `menubar`, `addcluster` and `clusters` with the overlay up
for the diff review; each captures the sheet, because the screenshot
helper photographs the key window.

Checkpoint as task 1.
Commit: `app: explain the menu bar and the sheets`.

Consumes: `MenuBarView`, `AddClusterSheet`, `ClustersSheet`, `NoteSheet`,
`AskAISheet`, `HelpButtonPlacement`.
Produces: the five tables and the five presentations explained.

## Task 8 - the screenshot flag and the coverage check

`macos/idios/App/Screenshot.swift`:

    /// explainRequested is the -explain argument: the screenshot run opens
    /// the overlay of whatever screen -route landed on, so the texts can be
    /// reviewed as a picture rather than by reading a table.
    static func explainRequested() -> Bool

    /// reportCoverage says on stderr when a screen's regions and its
    /// explanations are not the same set, and fails a debug build; a region
    /// with no explanation and an explanation with no region are both a
    /// hole in what a person is told.
    @MainActor static func reportCoverage(
        screen: ExplainedScreen, registered: Set<RegionID>)

Rules. `explainable` calls `reportCoverage` every time an overlay opens.
The line is one per screen and names both differences:
`idios-explain <screen>: unexplained <ids>; undrawn <ids>`, printed to
`FileHandle.standardError`, followed in a DEBUG build by
`assertionFailure` with the same text. A release build prints nothing and
asserts nothing: the check is for the person building the application,
not for the person running it. The one exemption is the incidents list's
row kinds, which task 4 states: a screen may declare a region it does not
always draw, and those ids are listed in one `private let` set in this
file rather than being a flag on `ExplainedRegion`, because four ids do
not earn a field on every row of every table.

`ExplainState.openAtLaunch` is already read in task 3; this task wires it
to `Screenshot.explainRequested()` and makes `Screenshot.captureIfRequested`
wait for the overlay as well as for the first store load, so the picture
is never taken half-drawn.

`hack/macos/screenshot.sh` takes its flags in a loop rather than by
position, so `--explain` and `--allow-disconnected` can be given in
either order and either alone:

    # Usage: hack/macos/screenshot.sh <route> <png> [--allow-disconnected] [--explain]

`--explain` adds `-explain` to the application's argument list. The
`idios-explain` lines the application prints go to the run's temp file
with everything else and are copied to stderr when the run fails, and the
script exits non-zero when any `idios-explain` line appeared, so a
screenshot run is the coverage gate the goal asks for.

`CLAUDE.md`'s screenshot paragraph gains the flag: "`--explain` opens the
route's help overlay before the capture, and the run fails when a
screen's regions and its explanations disagree."

No test. A shell flag and a stderr line are neither a spec scenario nor a
bug, and `.ai/tests.md` rule 1 keeps a test off both; the check is the
run, and the run is what every later screenshot in this repository now
does.

Check against the smoke store on 7771:
`IDIOS_DAEMON=127.0.0.1:7771 hack/macos/screenshot.sh pod/<uid>
/tmp/explain-pod.png --explain` writes a picture of the overlay and exits
zero. Delete one `.explained(...)` from `PodPageRail` and run it again:
the run exits non-zero and the line names the undrawn id. Put it back and
add a `.explained("nonsense")` somewhere: the run exits non-zero and the
line names the unexplained id. Put the file back as it was and run the
whole set - `incidents`, `incidents/dismissed`, `workloads`,
`workload/<cluster>/idios-smoke/CronJob/smoke-cron-fail`, `pod/<uid>`,
`pod/<uid>/events`, `run/<uid>`, `status`, `menubar`, `addcluster`,
`clusters` - and every one exits zero.

Checkpoint as task 1.
Commit: `app: screenshot the overlay and check its coverage`.

Consumes: `Screenshot`, `ExplainState`, `explainable`,
`hack/macos/screenshot.sh`.
Produces: `-explain`, `--explain`, `reportCoverage`, and a screenshot run
that fails on a hole.

## Task 9 - verification with the user and the status line

The user runs the application against the smoke store on 7771 and reads
the overlay on every screen: the pod page on a container and on the Pod
card, the incidents list with rows and with none, the palette, the
Workloads tree and each of its tabs, the Run page on each tab for both
`smoke-cron-fail` and `smoke-cronjob-retry`, Status, the menu bar
popover, and the four sheets. On each: the [?] is where the eye expects
it, "?" opens the overlay and does not fire while a text field has focus,
Cmd-/ opens it from the Help menu, Esc and a scrim click close it, the
notes stay inside the window, the reading path is the path a person would
actually take, and no sentence says something the screen does not show.
The words are read against 9.6: a state, a category or a kind reads the
same in the overlay as in its tooltip. Both appearances and both
densities, on a small window and on a full-screen one. Anything the
review turns up is fixed in the task that owns it and committed as a new
commit, never by rewriting one.

Then `docs/plans/m10-reading/roadmap.md`'s step G line becomes "complete
<date>". The milestone's own status line and the `docs/plans/README.md`
row for `m10-reading/` are the user's call, not this task's: closing the
milestone is a separate step with its own report.
Commit: `docs: close m10 step G`. The 7771 daemon is stopped, the Debug
build is quit, and `.storage/smoke` is left as it is for the next run.

## Hands to the next step

G is the last step of m10 as the roadmap stands. This step adds to what
step F handed on:

- `explainedRegions` and `readingOrder` are the one place the application
  says what a screen is for. A screen added later is a case there and a
  table in 9.7, and the screenshot run refuses it until both exist.
- `.explained(...)` costs a view one preference entry and no layout, so a
  later screen marks its regions as it is written rather than
  afterwards; the coverage line is what makes that habit hold.
- The `chrome` region is the recorded answer to a toolbar an overlay
  cannot dim. If the application ever draws its own title bar, that
  region becomes four and the note goes away.
- The row-kind exemption in `reportCoverage` is the one hole in the
  check, and it exists because a `List` recycles rows. A later step that
  moves the list to a `LazyVStack` in a `ScrollView` could close it.
- The one daemon change m10 decided not to make, so the next milestone
  starts from it rather than rediscovering it: the Run page draws only
  the attempts that opened an incident, because `GET /incidents?job_uid=`
  is the only way to reach a Job's pods and a pod that succeeded has no
  incident. The daemon holds every pod of a watched namespace (the
  processor upserts each one the informer delivers, with no incident
  gate) and `pods.controller_uid` is the join, unindexed and unfiltered:
  `ListPodsRequest` has no controller predicate and `query.PodFilter` no
  field for it. The change is a `job_uid` (controller uid) query field on
  `GET /pods`, the predicate in `query.PodFilter`, an index on
  `pods.controller_uid` in `0001_init.sql` (a schema change, so the
  databases are recreated), and an Attempts card that draws a pod row
  with no incident. Until then the page says it from the Job's own
  counters: the counts line reads "N failed attempts", a Complete run's
  verdict reads "Complete after N failed attempts and M that succeeded",
  and the Attempts card's footer names the succeeded pods idios did not
  record.
- The doubts below.

## Self-review

Spec coverage. 9.7 is written by task 1 for the pod page and extended by
tasks 4 to 7 for every other screen, each in the task that draws it, so
no table describes a region nothing registers. 9.3's screen rows gain the
[?] sentence in task 1. 9.4's keyboard paragraph gains "?" and Cmd-/ in
task 1 and both are built in task 3, the bare letter through `.onKeyPress`
on the focused screen and the Cmd key as a menu item, which is exactly
what that section requires. 9.5: task 3 adds no colour - the scrim and
the pills are materials, the outline is the accent and the accent is
selection only, which an overlay that selects regions is;
`BadgeStyle.swift` is untouched in every task. 9.6: task 2's third test
is the mechanism that keeps a state, a category or a kind from being
explained twice in two ways. 7.4: the tag chips draw the same provenance
a card title's hover draws, for regions that are not cards. 7.6: the
capture, sibling and Attempts regions each explain an absence rather than
hiding it. Decision 12: every clause of the roadmap paragraph is a task
here, including the [?]'s placement where there is no bottom right (task
7), the launch argument (task 8) and the coverage assertion (task 8).
Decision 10: every name in this plan, its tables and its checks is
invented or a smoke fixture's.

Split. Nine tasks, seven of them code. Task 1 is the spec before the
code, as step A was for the milestone and task 2 of step F was for the
run: 9.7's pod page table is what task 3 is answerable against. Task 2 is
the data alone, with its three tests, because the tables are the part of
this feature that outlives any view. Task 3 is the machinery and one
screen together, deliberately: an overlay with no screen on it cannot be
looked at, and the pod page is the screen whose regions were already
approved, so the first commit that draws anything draws the thing the
design was agreed on. Tasks 4 to 7 are one commit per area rather than
one per screen, because a screen's marking is a handful of one-line
modifiers and eleven commits of that shape would be eleven reviews of the
same diff; they are four rather than one because each carries its own
tables into 9.7 and its own check. Task 8 is last among the code because
it turns a warning into a gate, and a gate added before the screens are
marked would fail every run in between.

Doubts. The anchors of rows inside the incidents list are the largest
one. `IncidentsList` is a `List`, which builds and recycles its rows, so
a row scrolled out of view stops publishing its anchor and the overlay's
region set changes with the scroll position. Task 4 answers it by marking
the first row of each kind and by exempting those ids from the coverage
check, which is honest but means the list is the one screen whose
explanation can be incomplete without anything saying so, and it means
scrolling while the overlay is up moves an outline out from under its
pill. Opening the overlay could pin the scroll position; that was not
specified and is not built. -- The overlay above a `NavigationSplitView`
is the second. `.explainable` is hung on the split view so the sidebar's
anchors reach it, but a split view manages its own columns and a sidebar
that is collapsed, being animated, or in the overlay presentation style
on a narrow window may report a bounds the scrim then cuts a hole in the
wrong place. The check in task 4 looks at it on a wide window only; a
narrow one is not walked until task 9. -- The sheets are the third. Each
installs its own overlay, which is correct, but a sheet is a separate
window and its scrim therefore dims only the sheet while the screen
behind stays bright, so the picture a person sees is a lit window with a
dimmed card on it rather than the single dimmed screen every other route
gives. That may read as a different feature rather than the same one. The
alternative, one overlay in the window that covers the sheet, is not
possible. -- The palette is the fourth. It is already an overlay with its
own 0.25 dim, and putting a second scrim inside it means two dims stack
on the list behind. Task 4 says one scrim, not two, which requires the
palette's own dim to be dropped while its overlay is up; that is one
line, and it is the kind of one line that gets missed. -- The `chrome`
region is a compromise and reads as one: four different controls, one
outline that is a strip of nothing, and a note that describes things
outside the outline. It keeps the approved vocabulary and it is
buildable, but a person hovering a one-point strip to learn about Copy
uid is not being taught the way the rest of the overlay teaches. -- The
`tags` field is plural where decision 12 says tag. The approved sample
draws two to four chips on almost every region and one chip cannot carry
`restart_count`, `last_terminated_reason`, `image_id` and
`mem_limit_bytes`; the plural is the sample's shape and the deviation is
recorded here rather than silently taken. -- The vocabulary test compares
a region's sentence to a tooltip only when the region's title matches a
vocabulary word exactly. A region titled "Category, state, id and
occurrences" matches nothing and is free to say whatever it likes about a
category, which is precisely the drift the test exists to stop. Matching
on substrings would sweep in every region whose sentence mentions a
state; nothing better was found. -- `ExplainState` is one per window and
the menu item finds the frontmost screen through a serial. Two windows of
the same application, which `WindowGroup` allows, would share the menu
item and the serial; the second window is not a case this plan handles
and the application has never been driven with two.

`.ai` rules. ascii-only: no symbol in any code block, string or table
above, the arrows are `->`, and `hack/ascii-check
docs/plans/m10-reading/explain.md docs/plans/m10-reading/roadmap.md` is
run before these files are finished. tests: three tests, all in task 2,
each naming the sentence it traces to, each table-driven or whole-set,
each comparing a whole value - a whole `ScreenAudit`, a whole
`[ExplainedRegion]`, a whole set of offenders against the empty set - and
each leading with its edge cases: the screen with an empty table, the
screen with no reading path, the title that matches a kind word in a
different case, the title that is a wire value, the title that is
deliberately not a vocabulary word. Tasks 1 and 3 to 8 write no test,
because a document, a view, a layout, a set of data rows already covered
by task 2's audit, a stderr line and a shell flag are none of the three
sources rule 1 allows; the audit and vocabulary tests of task 2 run over
every screen and pick up each later task's rows without a new function,
which is rule 2. comments: every comment in a code block says why - the
literal id being checked at run time rather than by the compiler, the
tags being what a person greps for, one state per window because two
overlays must not stack, the [?] hidden under its own scrim, the first
row of each kind because a `List` recycles. code-is-truth: no code block,
comment, test name or stderr string mentions a document, a section or
this plan; the overlay's own text is about the screen and never about the
spec, and task 1 changes the doc in the same step the code changes.
scope: no new colour, no change to `BadgeStyle.swift`, no proto change,
no daemon change, no store change, no endpoint, no new screen, no
persistence of whether a person has seen an overlay, no first-run tour,
no per-region deep link, and no README screenshot retaken.
commits: nine subjects under 72 characters, one `model:`, six `app:`,
two `docs:`; no `make generate` and no `make generate-check`, because
`api/proto` is not touched.

Type consistency. `RegionID` (task 2) is the one key everywhere: the
literal in `.explained(...)`, the key of `ExplainAnchors`, the id of
`ExplainedRegion`, the id of `ReadingStep`, the element of the set
`reportCoverage` compares and the value `ExplainState.pinned` holds, so
no view converts between two spellings of a region's name.
`ExplainedScreen` (task 2) is built from `PodPane` and `ContainerTab`,
which are the same values `PodSelection` already carries, so the pod page
computes its screen from the selection it already has rather than
tracking a second one; `WorkloadsPart`, `RunPart`, `SheetKind` and
`ListState` are new because the application's own tab enums live in view
files and a model that imported them would invert the layering.
`ExplainedRegion` (task 2) is read by `ExplainOverlay` and by
`reportCoverage` and by nothing else, so the text and the check cannot
disagree about which regions exist. `Anchor<CGRect>` (task 3) is resolved
only through the `GeometryProxy` of the view that installed the overlay,
so every rect the scrim, the outlines, the pills and the notes use is in
one coordinate space. `HelpButtonPlacement` (task 3) is the one thing
tasks 4 and 7 vary, so a sheet and a screen share the whole of the rest.
