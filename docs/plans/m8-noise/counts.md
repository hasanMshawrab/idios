# m8 step C - application: counts and attention

Goal: after this plan the incidents list's pure logic lives in
`IdiosModel` where `make app-test` reaches it; a folded pod's lead row
shows its own count and the "+N on this pod" pill is the only place the
siblings are counted; the lead of a fold is the open row before the
closed one, then the worst category; the header's explanation fallback
reads only the Warnings attached to the incident it explains; an
`unclean_exit` arrives with a sentence built from its own row; the
sidebar's default pill is Attention with Open kept beside it; the menu
bar counts and lists the attention set; the status screen shows the
attention window. The daemon is untouched.

Architecture: three files join `macos/Sources/IdiosModel`: `Fold.swift`
(the ranked category order and the pod fold), `Explanation.swift` (the
sentence under the header title and its provenance), and one field on
`Status.swift`. The application target keeps the SwiftUI: the grouping
that builds sections, the row views, the sidebar, the header, the menu
bar. The views call the model's functions and own nothing pure any more.
`IncidentState.attention` already exists (step B added it because
`IncidentCounts(wire:)` throws on an unknown state); this step gives it
its places.

Tech stack: Swift 6 package `macos/Sources/IdiosModel` with tests in
`macos/Tests/IdiosModelTests` (`swift test --package-path macos`, run as
`make app-test`); the Xcode application (`make app`); generated client
`IdiosAPI` (`make generate` has run in step B, `rm -rf macos/.build`
before the first `make app-test`).

Spec: `docs/design/presentation.md` section 4.2 (the `attention`
definition), 4.6 (`attention_window_seconds` among the intervals), 4.7
and the screens table of section 12 as amended by tasks 2 and 3 of this
plan. Roadmap decisions 4, 7, 8 and 10 of `docs/plans/m8-noise/roadmap.md`.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Swift follows the Go rules (`CLAUDE.md`): one-line doc
comment per public type and function, comments say why, tests trace to a
spec statement with whole-value assertions. Views never import
`IdiosAPI`; stores own every client call. The only hard-coded colours
stay in `BadgeStyle.swift`. Nothing in a test, comment or doc names a
real organisation, cluster, namespace, workload, image or node; the test
fixtures are the invented `checkout-api`, `registry.example.com` and
`node-a` the existing tests use. Every count stays a count of incidents.
Checkpoint before every commit: `go build ./... && go test ./... && make
ascii && make app-test && make app`. Nothing is committed without the
user's review of the diff; implementers never commit.

## File structure

    macos/Sources/IdiosModel/Fold.swift                 Category.ranked, Category.rank, PodFold, podFolds (task 1); the lead order (task 2)
    macos/Sources/IdiosModel/Explanation.swift          Explanation, explanation(for:events:pod:) (task 1); the incident_id filter and the unclean_exit sentence (task 2)
    macos/Sources/IdiosModel/Status.swift               DaemonStatus.attentionWindowSeconds (task 3)
    macos/Tests/IdiosModelTests/FoldTests.swift         the fold as it is (task 1); the lead order and the dropped sum (task 2)
    macos/Tests/IdiosModelTests/ExplanationTests.swift  the choice as it is (task 1); the filter and the sentence (task 2)
    macos/Tests/IdiosModelTests/StatusTests.swift       the window decoded (task 3)
    macos/idios/Views/Incidents/IncidentGrouping.swift  PodFold, podFolds, categoryRank leave; @MainActor dropped (task 1); PodFold.occurrences gone (task 2)
    macos/idios/Views/Incidents/IncidentsSidebar.swift  categories reads Category.ranked (task 1); states gains .attention first (task 3)
    macos/idios/Views/IncidentDetail/IncidentHeader.swift  explanation comes from the model (task 1)
    macos/idios/Views/Incidents/IncidentsList.swift     foldedOccurrences no longer passed (task 2)
    macos/idios/Views/Incidents/IncidentRowView.swift   foldedOccurrences gone, the count is the row's (task 2)
    macos/idios/Views/Incidents/IncidentsScreen.swift   the default filter is .attention (task 3)
    macos/idios/Store/IncidentsStore.swift              title for .attention; matches() knows attention (task 3)
    macos/idios/Store/MenuBarStore.swift                attentionCount; the list asks state=attention (task 3)
    macos/idios/Views/MenuBar/MenuBarView.swift         the words say what is counted (task 3)
    macos/idios/Views/Status/StatusScreen.swift         the attention window fact (task 3)
    docs/design/presentation.md                         12 fold sentence (task 2); 4.7, 12 sidebar and menu bar rows (task 3)

## Task 1 - decision 8: the move

Nothing changes in behaviour; the application builds and looks the same.
The tests written here pin the behaviour that exists so task 2 can
change it under test.

`macos/Sources/IdiosModel/Fold.swift`:

    extension Category {
        /// ranked is the category vocabulary, worst first: the sidebar's
        /// order, the fold's lead choice and a group's worst category.
        public static let ranked: [Category] = [
            .crash, .oom, .uncleanExit, .imagePull, .config, .probe, .scheduling, .stuck,
            .nodePressure, .rescheduled, .jobFailed,
        ]

        /// rank is this category's place in ranked; a category the list does
        /// not rank sorts behind every one it does.
        public var rank: Int { Category.ranked.firstIndex(of: self) ?? Category.ranked.count }
    }

    /// PodFold is one pod's incidents inside a group: the row that leads them
    /// and the siblings that fold under it.
    public struct PodFold: Identifiable, Hashable, Sendable {
        public let id: String
        public let podUID: String?
        public let lead: Incident
        public let siblings: [Incident]
        public let occurrences: Int32
    }

    /// podFolds gathers the rows of one group by pod, so one pod's containers
    /// and categories read as the single event they are.
    public func podFolds(_ rows: [Incident]) -> [PodFold]

The body is the one in `IncidentGrouping.swift` today (bucket by
`podUID`, a job-subject row alone under `incident/<id>`, the lead by
`rank` with `min` so a tie falls to the first served, the siblings in
served order, `occurrences` still summed; task 2 removes the sum). The
private `buckets` helper moves with it; `IncidentGrouping.swift` keeps a
copy for its own sections, or the model exports one `buckets` and the
app calls it. The implementer picks the smaller diff and says which.

`macos/Sources/IdiosModel/Explanation.swift`:

    /// Explanation is the sentence under the header title and the column it
    /// came from.
    public struct Explanation: Hashable, Sendable {
        public let message: String
        public let provenance: String
    }

    /// explanation picks the sentence that says why the incident exists: the
    /// recorder's own last_message, else the newest Warning among the pod's
    /// events.
    public func explanation(for incident: Incident, events: [Event], pod: Pod?) -> Explanation?

Today's body: `lastMessage` with provenance `incidents.last_message`;
else the newest `Warning` by `lastTS ?? firstTS` over every event, with
provenance `k8s_events.message WHERE incident_id - <reason>`; else nil.
The `pod` parameter is unused until task 2 and is declared here so the
signature does not change under the header twice; the doc comment says
nothing about it until task 2 uses it.

`IncidentGrouping.swift`: `PodFold`, `podFolds`, `categoryRank` are
deleted; `group()` computes `worstCategory` as
`open.map(\.category).min { $0.rank < $1.rank }` and `folds:
podFolds(rows)`; every `@MainActor` in the file goes (they existed for
`IncidentsSidebar.categories`). `IncidentsSidebar.categories` is
deleted and the `ForEach` reads `Category.ranked`; if the constant has
another reader, it reads `Category.ranked` too. `IncidentHeader.swift`:
the private `Explanation`, `explanation`, `newestWarning` and `at` go;
`explanation` becomes `explanation(for: incident, events: detail.events,
pod: detail.pod)`.

Tests first, `macos/Tests/IdiosModelTests/FoldTests.swift`. A local
helper builds rows from `crashIncident` (`IncidentTests.swift`) with the
fields the fold reads:

    func row(_ id: String, pod: String?, category: Category, closed: Bool = false,
             lastSeen: String, occurrences: Int32 = 1) -> Incident

built with the memberwise initialiser (the test target imports
`@testable`), `subjectKind` `.pod` when `pod` is set and `.job` when it
is nil, `closedAt`/`closeReason` `.podDeleted`/`state` `.podDeleted` when
closed, `state` `.open` otherwise, every other field copied from
`crashIncident`. One table test, `rowsOfOnePodFoldUnderTheWorstCategory`,
whole `[PodFold]` assertions, rows served newest first:

- two categories on one pod: `probe` newer, `oom` older -> lead `oom`,
  sibling `probe`, `occurrences` the sum;
- two rows of the same category on one pod: the lead is the first
  served (newest);
- rows of two pods: two folds in served order;
- a job-subject row (pod nil) between two rows of one pod: it stands
  alone and does not join them;
- one row: a fold with no siblings and its own occurrences.

Trace: presentation section 12, Incidents row, "rows sharing a `pod_uid`
fold under the sibling with the worst category (ties to the newest last
seen) ... never applying to an incident without a pod".

`macos/Tests/IdiosModelTests/ExplanationTests.swift`, one table test,
`theHeaderExplainsWithTheRecorderThenTheEvents`, whole `Explanation?`
assertions, built from `crashIncident`, `unhealthyEvent` and
`killingEvent` (`Fixtures.swift` / `EventTests.swift`; check their
`type`, `incidentID`, `lastTS` and use a copy with the fields the row
needs, adding a `warning(id:incidentID:reason:message:lastTS:)` helper
if the fixtures do not cover a case):

- a row with `lastMessage`: that message, provenance
  `incidents.last_message`, whatever the events say;
- no `lastMessage`, two Warnings: the newer one's message, provenance
  `k8s_events.message WHERE incident_id - <its reason>`;
- no `lastMessage`, a Normal event only: nil;
- no `lastMessage`, no events: nil.

Trace: the provenance label the header shows on hover
(`k8s_events.message WHERE incident_id`), which section 12's
"provenance ... is the card title's hover" makes a statement about what
is shown.

Checkpoint: `go build ./... && go test ./... && make ascii && rm -rf
macos/.build && make app-test && make app`. The application is opened
against the smoke data (`make smoke PORT=7771` if 7770 is held, then
`IDIOS_DAEMON=127.0.0.1:7771 hack/macos/screenshot.sh incidents
$CLAUDE_JOB_DIR/tmp/c1.png`) and the list reads as before. Commit:
`app: move the fold and the explanation into IdiosModel`.

Consumes: `Incident`, `Event`, `Pod`, `Category` of `IdiosModel`.
Produces: `Category.ranked`, `Category.rank`, `PodFold`, `podFolds(_:)`,
`Explanation`, `explanation(for:events:pod:)`.

## Task 2 - decision 7: a count is a count of the row it sits on

Spec first, `docs/design/presentation.md` section 12, Incidents row: the
fold clause becomes "pod-sibling folding inside a group, where the rows
sharing a `pod_uid` fold under one lead (an open row before a closed
one, then the worst category, ties to the newest last seen) which
carries a "+N on this pod" pill counting the folded rows, every row
showing its own occurrences whether folded or not, the siblings then
indenting beneath it ...", the rest of the sentence unchanged. The
"summed occurrences" clause is gone; the lead order is stated.

Tests first, added rows and one new test.

`FoldTests.swift`: `PodFold` loses `occurrences`, so every expected value
drops it and the single-row case goes with the count it asserted (the
two-pods row already covers a fold with no siblings); the test is
renamed `rowsOfOnePodFoldUnderTheOpenRowThenTheWorstCategory`; new rows
in the table:

- a closed `crash` (newer) and an open `probe` (older) on one pod: lead
  `probe` - open comes before the worst category;
- two closed rows, `probe` newer and `oom` older: lead `oom` - among
  closed rows the rank decides;
- two open rows of one category: the lead is the first served.

Trace: the amended fold clause.

`ExplanationTests.swift`, renamed
`theHeaderExplainsWithTheRecorderThenTheEventsThenTheRow`, rows added to
the table:

- no `lastMessage`, the newest Warning carries another incident's id
  (a sibling's probe), an older Warning carries this one: the older
  one's message - the provenance label says `WHERE incident_id` and the
  choice now does what it claims;
- no `lastMessage`, every Warning carries another id: nil for a `crash`;
- an `unclean_exit` row (`category: .uncleanExit`, `exitCode: 137`,
  `signal: 0`, no `lastMessage`, no Warning of its own) with a pod whose
  `deletionRequestedAt` is `2026-08-27T14:39:00.000000Z`:
  `Explanation(message: "exit 137, while the pod was terminating",
  provenance: "exit_code and signal of the container's last termination
  - pods.deletion_requested_at 2026-08-27T14:39:00.000000Z")`;
- the same with `signal: 9`: `"exit 137, signal 9, while the pod was
  terminating"`;
- the same with `exitCode: nil` (the termination was not observed): the
  message `"the container exited while the pod was terminating"`, the
  provenance the same;
- the same with `pod: nil` (the pod row was swept): the provenance
  ends after "termination" with no timestamp clause.

Trace: decision 7 ("built from the row ... from the history row's exit
code and signal and the pod's `deletion_requested_at`, with the
provenance saying so") and the smoke-cluster fact that a grace-period
kill arrives as exit 137 with signal 0 from containerd, so the signal is
printed only when it is non-zero, as `IncidentRowView.exitText` already
does for the list.

A `pod(deletionRequestedAt:)` helper in the test file copies `crashPod`
(`PodTests.swift`) with the one field changed.

Implementation.

`Fold.swift`: `PodFold` loses `occurrences`; `podFolds` picks the lead
with

    let lead = bucket.rows.min { a, b in
        if (a.closedAt == nil) != (b.closedAt == nil) { return a.closedAt == nil }
        return a.category.rank < b.category.rank
    } ?? bucket.rows[0]

with the one-sentence reason: a closed row is a finished fact and the
open one is what the pill still stands for. `min` is not a strict weak
ordering when both keys tie, and Swift's `min(by:)` returns the first of
equal elements, which is the newest served row, as today.

`Explanation.swift`: the Warning filter gains `$0.incidentID ==
incident.id`; after it, for `incident.category == .uncleanExit`:

    var parts: [String] = []
    if let exit = incident.exitCode {
        parts.append("exit \(exit)")
        if let signal = incident.signal, signal != 0 { parts.append("signal \(signal)") }
    }
    let message = parts.isEmpty
        ? "the container exited while the pod was terminating"
        : parts.joined(separator: ", ") + ", while the pod was terminating"
    var provenance = "exit_code and signal of the container's last termination"
    if let at = pod?.deletionRequestedAt { provenance += " - pods.deletion_requested_at \(at.raw)" }

The comment carries the kubelet fact: it writes no message for a
grace-period kill and `Killing` is a Normal event, so an `unclean_exit`
has nothing to quote and the row is the source; containerd reports
signal 0 for the kill, so "signal 0" would read as a signal that was
sent.

`IncidentGrouping.swift`: nothing (the struct is the model's).
`IncidentsList.swift`: the `foldedOccurrences:` argument goes.
`IncidentRowView.swift`: `foldedOccurrences` and the `leads && !expanded`
branch go; `occurrences` prints `incident.occurrences`; the comment above
it is replaced by the reason the pill exists alone: the pill counts
rows, the column counts this row's occurrences, and summing the
siblings' occurrences onto the lead invented a number no row carried.

Checkpoint as task 1. The screenshot of `incidents` against the smoke
data shows a lead row with its own count and the pill beside it. Commit:
`app: count the row it sits on and give unclean_exit a sentence`.

Consumes: task 1's names.
Produces: `PodFold` without `occurrences`; the lead order step D's
rollup reuses.

## Task 3 - decision 4: attention

Spec first, `docs/design/presentation.md`:

- 4.7: the menu bar extra uses `GET /v1/incidents?state=attention&limit=5`
  and `GET /v1/incidents/counts` for its number, which is the attention
  count; the sentence says why: a row that closed an hour ago and
  nobody has looked at is still the menu bar's business, and the count
  that says "open" would be false the moment it included one.
- Section 12, Incidents row: "state as a wrapping row of pills with an
  all pill, Attention first and the default (open, or closed within
  `attention_window` and never acknowledged or dismissed), Open beside
  it, ...".
- Section 12, Menu bar extra row: "the attention count, the newest five
  attention rows (a closed one naming its close reason), each leading
  with its cluster name while more than one cluster exists, then "N
  more need attention"", the endpoint column `/incidents?state=attention&limit=5`,
  `/incidents/counts`, `/clusters`.

Test first, `StatusTests.swift`: the whole-`DaemonStatus` expectation
gains `attentionWindowSeconds: 86400` (the fixture `api/testdata/status.json`
already carries it since step B). Trace: 4.6 "the configured intervals
include ... `attention_window_seconds`". `Status.swift`:
`public let attentionWindowSeconds: Int32`, decoded `wire.attentionWindowSeconds ?? 0`
after `stuckAfterSeconds`.

The rest is the application target, which `make app-test` does not
reach; `make app` and the screenshots are its evidence.

`IncidentsSidebar.states`: `[.attention, .open, .acknowledged,
.recovered, .podDeleted, .jobFinished, .manual, .dismissed]`; the doc
comment says Attention leads because it is the default and contains
Open.

`IncidentsScreen.swift`: the two `IncidentFilter(state: .open)` defaults
(the plain init and `.incidents(nil)` in both the route init and the
route handler) become `.attention`. `Route(path: "incidents/attention")`
already parses because the enum has the case.

`IncidentsStore.swift`: `IncidentFilter.title` gains `case (.attention,
nil): "Incidents needing attention"` before the generic state case.
`matches(_:)`:

    if let state {
        if state == .attention {
            // A row's own state is never attention, and the application
            // does not know the window. A closed row that changes is nearly
            // always a fresh close, so unread and undismissed decides; a
            // write on a row closed longer ago can show until the next
            // reload.
            guard incident.dismissedAt == nil,
                incident.closedAt == nil || incident.acknowledgedAt == nil
            else { return false }
        } else if incident.state != state {
            return false
        }
    }

Without this every streamed row would leave the default list, because no
row reports `attention` as its state. The one thing the missing window
costs: a note, undismiss or unacknowledge on a row closed longer than the
window ago streams back and sits in the list until the next reload; the
counts reload on every stream event and stay right.

`MenuBarStore.swift`: `openCount` becomes `attentionCount`, read from
`byState[.attention] ?? 0`; `loadIncidents` asks
`state: IncidentState.attention.rawValue`. `MenuBarView.swift`: the label
shows `attentionCount`; the header dot is red while it is above zero;
the header text is "N incidents need attention" ("1 incident needs
attention", "Nothing needs attention" at zero, which also replaces "No
open incidents" in the empty list); `incidentText` appends ", closed -
<close reason label>" for a row with `closedAt` set, before the
acknowledged clause (an attention row is never both); `moreRow` says "N
more need attention" ("1 more needs attention").

`StatusScreen.swift`: `fact("attention window",
seconds(status.attentionWindowSeconds), key: "attention_window")` after
`stuck after`.

Checkpoint as task 1. Screenshots against the smoke data: `incidents`
(Attention active, Open beside it, closed rows in the list with their
closed badge), `menubar` (the count and the words), `status` (the
window row). Commit: `app: make attention the default view`.

Consumes: `IncidentState.attention`, `IncidentCounts.byState`,
`Status.attentionWindowSeconds` on the wire.
Produces: `DaemonStatus.attentionWindowSeconds`; the default pill step
D's mockup redraw shows.

## Hands to the next step

Step D reads: `Category.ranked` and `Category.rank` (`Fold.swift`) for
the rollup's lead and worst category; `PodFold` (id, podUID, lead,
siblings; no count) and `podFolds(_:)`, whose lead order is open before
closed then rank; `IncidentRowView` takes `siblings:` and `expanded:`
and prints the row's own occurrences; `expandedPods` in `IncidentsList`
keys on the pod uid, so the rollup's expansion set is a second set keyed
on the rollup key string. The default pill is Attention; the mockup's
page 1 still shows Open as the hot pill and is D's to redraw.

## Self-review

Spec coverage: 4.2's attention definition reaches the application in
task 3 (the default filter, the stream match, the menu bar); 4.6 in
task 3 (`attentionWindowSeconds`); 4.7 rewritten in task 3; section 12
fold clause in task 2, sidebar and menu bar rows in task 3. Decisions: 7
in task 2 (the sum, the order, the filter, the sentence); 8 in task 1
(the move, `@MainActor` dropped, views still never import `IdiosAPI`); 4
in task 3; 10 everywhere (`checkout-api`, `registry.example.com`,
`node-a`, `idios-smoke`).

`.ai` rules: ascii (checked per task); tests trace to the section 12
fold clause, the header's provenance label, decision 7, 4.6 - variants
are table rows, assertions are whole `[PodFold]`, whole `Explanation?`,
whole `DaemonStatus`; no test of the language (no test that `ranked`
holds eleven values, that `rank` indexes it, or that a struct holds what
was put in it); comments carry the kubelet and containerd facts and the
stream reasoning, no what; code-is-truth: no doc named from code, the
presentation doc changes in the task that changes the behaviour; scope:
no rollup, no window in the application's match (a streamed row is in
the window), no `stateTitle` move (nothing tests it), no mockup redraw
(step D's); commits per task, none red, none without the user's review.

Type consistency: `Category.rank` is `Int` in task 1 and its two readers
(`podFolds`, `group()`); `PodFold` has `occurrences` in task 1 and not
from task 2 on, and every expected value in `FoldTests` follows;
`explanation(for:events:pod:)` takes `Pod?` from task 1 and reads it
only from task 2; `attentionWindowSeconds` is `Int32` like the other
windows; `attentionCount` is `Int32` like the `openCount` it replaces.
