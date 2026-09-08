# m8 step D - application: the workload rollup

Goal: after this plan twenty pods of one workload with one problem read
as one line of the incidents list: inside a group, the rows of two or
more pods that share a container and a category fold under one rollup
row that names the count of pods, carries the reasons and exit codes of
what is folded, and expands to the pod folds step C shaped; `a` on that
row acknowledges every incident under it, `d` and `r` ask first naming
the count and the workload, Command-Delete is refused on it. Page 1 of
the mockup shows the list as the design settled it, Attention as the
default pill, and the smoke cluster reproduces the shape with the crash
loop at three replicas. The daemon is untouched.

Architecture: one file joins `macos/Sources/IdiosModel`, `Rollup.swift`:
the rollup, the group entry that is either a rollup or a pod fold, and
the pure function that divides a group's rows into entries over the
`podFolds` and `buckets` of `Fold.swift`. The application target keeps
the SwiftUI: `IncidentGroup` carries entries instead of folds, a new
`RollupRowView` draws the rollup line, `IncidentsList` expands it, keys
its expansion on the rollup id, and routes the keys over it. The pieces
the two row views share (the disclosure, the container chip, the count
pill, the column label) become small views in `IncidentRowView.swift`
first, so the rollup row repeats none of them.

Tech stack: Swift 6 package `macos/Sources/IdiosModel` with tests in
`macos/Tests/IdiosModelTests` (`make app-test`); the Xcode application
(`make app`). No proto change: `rm -rf macos/.build` is not needed.

Spec: `docs/design/presentation.md` section 9.3, Incidents row of the
screens table, as amended by task 1 of this plan. Roadmap decisions 7, 9
and 10 of `docs/plans/m8-noise/roadmap.md`; the design canvas frames
"Main" and "Rollup" (sources in the `idios-design-canvas` directory
beside this repository) for the mockup of task 5.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Swift follows the Go rules (`CLAUDE.md`): one-line doc
comment per public type and function, comments say why, tests trace to a
spec statement with whole-value assertions, variants are table rows.
Views never import `IdiosAPI`; stores own every client call. The only
hard-coded colours stay in `BadgeStyle.swift`. Every count stays a count
of incidents; no view sums occurrences. Nothing in a test, comment, doc
or fixture names a real organisation, cluster, namespace, workload,
image or node; the invented names are the ones the existing tests and
the canvas use (`checkout-api`, `ledger-worker`, `consumer-default`,
`registry.example.com`, `node-a`, `worker-1`). Checkpoint before every
commit: `go build ./... && go test ./... && make ascii && make app-test
&& make app`. Implementers never commit; nothing is committed without
the user's review of the diff.

## File structure

    macos/Sources/IdiosModel/Rollup.swift                Rollup, GroupEntry, groupEntries(_:groupID:) (task 1)
    macos/Tests/IdiosModelTests/RollupTests.swift        the division into entries; the line's reasons and exit codes (task 1)
    macos/Tests/IdiosModelTests/FoldTests.swift          the row(...) builder widens and opens to the module (task 1)
    docs/design/presentation.md                          9.3 Incidents row, the rollup clause (task 1)
    macos/idios/Views/Incidents/IncidentRowView.swift    FoldDisclosure, ContainerChip, CountPill, ColumnLabel (task 2); indented (task 3)
    macos/idios/Views/Incidents/RollupRowView.swift      the rollup line (task 3)
    macos/idios/Views/Incidents/IncidentGrouping.swift   IncidentGroup.entries replaces folds (task 3)
    macos/idios/Views/Incidents/IncidentsList.swift      entries drawn, expandedRollups, Return and double click on a rollup (task 3); a/d/r over a rollup, the confirmation, Command-Delete refused (task 4)
    docs/mockups/idios-ui.html                           page 1 redrawn, its note rewritten, the pod-row classes (task 5)
    hack/smoke/crash-loop.yaml                           replicas: 3 and the comment (task 6)

## Task 1 - decision 9: the rollup in IdiosModel

Spec first, `docs/design/presentation.md` section 9.3, Incidents row.
After the fold clause, which ends "never changing a count, which stays a
count of incidents;", insert:

    replica rollup inside a group, where the pod rows of one workload that
    share `container_name` and `category` across two or more pods fold under
    one rollup row (named by the count of pods, carrying the shared
    container and category, every `last_reason` and exit code found among
    them once each, and a pill counting the incidents; `last_reason` is not
    in the key because a crash loop alternates its reasons on every
    occurrence and a rollup that formed and dissolved as loops went in and
    out of phase would be worse than none; a key held by one pod is a plain
    pod fold; a bare pod and a job-subject incident never roll up; the
    rollup never crosses a group boundary and changes no count), Return or
    a double click expanding it to the pod folds above, `a` acknowledging
    every incident under it in one write per incident, `d` and `r` asking
    first with the count and the workload named, and Command-Delete refused
    on it;

The rest of the row is unchanged.

Tests first. `macos/Tests/IdiosModelTests/FoldTests.swift`: the `row`
builder drops `private` so `RollupTests.swift` in the same module can
call it, and gains the fields the rollup key and line read, each
defaulting to `crashIncident`'s value so every existing call is
unchanged:

    func row(
        _ id: String, pod: String?, category: Category, closed: Bool = false,
        lastSeen: String, occurrences: Int32 = 1,
        container: String? = crashIncident.containerName,
        workload: String = crashIncident.workloadName,
        lastReason: String = crashIncident.lastReason,
        exitCode: Int32? = crashIncident.exitCode
    ) -> Incident

with `containerName: container`, `workloadName: workload`, `lastReason:
lastReason`, `exitCode: exitCode` in the initialiser. Its doc comment
says the builder serves the fold and the rollup tests.

`macos/Tests/IdiosModelTests/RollupTests.swift`, one table test,
`rowsOfTwoOrMorePodsSharingAContainerAndACategoryRollUp`, whole
`[GroupEntry]` assertions, `groupEntries(rows, groupID: "g")`, rows
served newest first; a local `let t1 = "2026-08-27T14:39:00.000000Z"`,
`t2 = "...14:37:00..."`, `t3 = "...14:35:00..."` keep the rows short.
Rows of the table, edge cases first:

- no rows: `[]`;
- three pods, `api` `crash`, all open (`1` on `p1` at t1, `2` on `p2` at
  t2, `3` on `p3` at t3): one rollup, `id: "g/rollup/1/idios-smoke/
  Deployment/checkout-api/api/crash"`, `containerName: "api"`,
  `category: .crash`, `folds` the three single-row folds in served order;
- two pods on `api` `crash` (`1` on `p1` at t1, `3` on `p3` at t3) with a
  third pod's `api` `oom` (`2` on `p2` at t2) served between them: the
  rollup first, in the place of its first served row, then `p2`'s plain
  fold;
- one pod with two `api` `crash` rows (`1` closed at t1, `2` open at t2)
  and nothing else: a plain fold led by `2` with `1` its sibling; a key
  held by one pod never rolls up;
- two pods, same category, different containers (`1` on `p1` `api`
  `crash`, `2` on `p2` `sidecar` `crash`): two plain folds;
- two pods, same container, different categories (`1` on `p1` `api`
  `crash`, `2` on `p2` `api` `oom`): two plain folds;
- a job-subject row (`2`, pod nil, `jobFailed`, `container: nil`) served
  between `1` on `p1` and `3` on `p2`, both `api` `crash`: the rollup of
  `p1` and `p2`, then the job row's own fold;
- two bare pods (`workload: ""`, `1` on `p1`, `2` on `p2`, both `app`
  `oom`): two plain folds; a bare pod is not replica fan-out;
- a pod contributing two rows to the key (`1` on `p1` open at t1, `2` on
  `p2` open at t2, `3` on `p1` closed at t3, all `api` `crash`): the
  rollup's fold for `p1` is led by `1` with `3` its sibling, the fold
  for `p2` is `2` alone;
- a rolled-up pod with a row outside the key (`1` on `p1` `api` `crash`
  at t1, `2` on `p1` `sidecar` `probe` at t2, `3` on `p2` `api` `crash`
  at t3): the rollup of `1` and `3`, then a plain fold of `2`;
- pod-level rows on two pods (`container: nil`, `.scheduling`, `1` on
  `p1`, `2` on `p2`): one rollup with `containerName: nil` and id
  `"g/rollup/1/idios-smoke/Deployment/checkout-api//scheduling"`.

Trace: the rollup clause of section 9.3 (the key, the count of pods, the
one-pod key, the bare pod, the job-subject row, the group boundary
which `groupEntries` cannot cross because it is called per group).

A second table test in the same file,
`theRollupLineNamesEveryReasonAndExitCodeOnce`, asserts the tuple
`(rollup.reasons, rollup.exitCodes, rollup.openCount)` for a `Rollup`
built directly with `id: "g"`, `containerName: "api"`, `category:
.crash`, `folds: [PodFold(id: "1", podUID: "p1", lead: <row 1>,
siblings: [<row 3>]), PodFold(id: "2", podUID: "p2", lead: <row 2>,
siblings: [])]`:

- every row `lastReason: "Error"`, `exitCode: 1`, all open:
  `(["Error"], [1], 3)`;
- row 1 `Error` exit 1 open, row 2 `CrashLoopBackOff` exit nil open, row
  3 `Error` exit 137 closed: `(["Error", "CrashLoopBackOff"], [1, 137],
  2)`: once each, first seen first, a nil exit code dropped.

Trace: "every `last_reason` and exit code found among them once each"
and the "N open" the row's badge prints.

Implementation, `macos/Sources/IdiosModel/Rollup.swift`:

    /// Rollup is the rows of two or more pods of one workload that share a
    /// container and a category inside one group: replica fan-out of one
    /// problem, read as one line.
    public struct Rollup: Identifiable, Hashable, Sendable {
        public let id: String
        public let containerName: String?
        public let category: Category
        public let folds: [PodFold]

        public init(id: String, containerName: String?, category: Category, folds: [PodFold]) {
            self.id = id
            self.containerName = containerName
            self.category = category
            self.folds = folds
        }
    }

    extension Rollup {
        /// rows is every incident folded here, each pod's lead before its
        /// siblings, pods in served order.
        public var rows: [Incident] { folds.flatMap { [$0.lead] + $0.siblings } }

        /// openCount is how many of the rows are still open.
        public var openCount: Int { rows.filter { $0.closedAt == nil }.count }

        /// reasons is every last reason among the rows, once each, first seen
        /// first.
        public var reasons: [String] { distinct(rows.map(\.lastReason)) }

        /// exitCodes is every exit code among the rows, once each, first seen
        /// first.
        public var exitCodes: [Int32] { distinct(rows.compactMap(\.exitCode)) }

        /// imageTags is every image tag among the rows, once each, first seen
        /// first.
        public var imageTags: [String] { distinct(rows.compactMap(\.imageTag)) }
    }

    /// GroupEntry is one line of a group: one pod's fold, or the rollup of
    /// several pods.
    public enum GroupEntry: Identifiable, Hashable, Sendable {
        case fold(PodFold)
        case rollup(Rollup)

        public var id: String {
            switch self {
            case .fold(let fold): fold.id
            case .rollup(let rollup): rollup.id
            }
        }
    }

    /// groupEntries divides the rows of one group into rollups and pod folds:
    /// the rows of two or more pods of one workload sharing a container and a
    /// category roll up, every other row folds by pod.
    public func groupEntries(_ rows: [Incident], groupID: String) -> [GroupEntry] {
        // A job-subject row has no pod and a bare pod has no replicas, so
        // neither is ever fan-out.
        let candidates = rows.filter { $0.podUID != nil && !$0.workloadName.isEmpty }
        let rolled = Set(
            buckets(candidates) { rollupKey($0, groupID: groupID) }
                .filter { Set($0.rows.compactMap(\.podUID)).count >= 2 }
                .map(\.key))
        return buckets(rows) { row in
            let key = rollupKey(row, groupID: groupID)
            if rolled.contains(key) { return key }
            return row.podUID.map { "pod/\($0)" } ?? "incident/\(row.id)"
        }
        .map { bucket in
            guard rolled.contains(bucket.key) else { return .fold(podFolds(bucket.rows)[0]) }
            let first = bucket.rows[0]
            return .rollup(
                Rollup(
                    id: bucket.key, containerName: first.containerName, category: first.category,
                    folds: podFolds(bucket.rows)))
        }
    }

    // last_reason is not in the key: a crash loop alternates Error and
    // CrashLoopBackOff on every occurrence, and a rollup that formed and
    // dissolved as the loops went in and out of phase would be worse than
    // none. The group id keeps two workloads' rollups apart when the list is
    // grouped by something other than the workload.
    private func rollupKey(_ row: Incident, groupID: String) -> String {
        "\(groupID)/rollup/\(row.clusterID)/\(row.namespace)/\(row.workloadKind)/"
            + "\(row.workloadName)/\(row.containerName ?? "")/\(row.category.rawValue)"
    }

    private func distinct<T: Hashable>(_ values: [T]) -> [T] {
        var seen: Set<T> = []
        return values.filter { seen.insert($0).inserted }
    }

A rolled-up bucket is bucketed by the rollup key, so a pod's rows
outside the key fall to its own `pod/<uid>` bucket and become a plain
fold; a non-rolled bucket holds one pod or one job row, so `podFolds`
returns exactly one fold for it. The `rows.filter` for candidates
excludes a job row (`podUID == nil`) and a bare pod (empty
`workloadName`) before the pod count is taken, so neither can make a key
reach two pods. `Rollup` needs the explicit `public init` because the
test builds one directly and the memberwise initialiser of a public
struct is internal.

Checkpoint: `go build ./... && go test ./... && make ascii && make
app-test && make app`. Commit: `app: roll replica fan-out up in
IdiosModel`.

Consumes: `PodFold`, `podFolds(_:)`, `buckets(_:key:)` of `Fold.swift`;
`Incident`, `Category` of `IdiosModel`.
Produces: `Rollup` (id, containerName, category, folds; rows, openCount,
reasons, exitCodes, imageTags), `GroupEntry` (`.fold`, `.rollup`, id),
`groupEntries(_:groupID:)`.

## Task 2 - the pieces two row views share

No behaviour change; the application builds and looks the same. This
task exists so task 3's `RollupRowView` repeats nothing.

`macos/idios/Views/Incidents/IncidentRowView.swift` gains four small
views, internal to the target, and its own body switches to them:

    /// FoldDisclosure is the chevron that opens and closes a folded line.
    struct FoldDisclosure: View {
        let expanded: Bool
        let help: String
        let toggle: () -> Void

        var body: some View {
            Button(action: toggle) {
                Image(systemName: expanded ? "chevron.down" : "chevron.right")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 12, height: 14)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, 2)
            .help(help)
        }
    }

    /// ContainerChip names the container a line is about, or says that the
    /// problem is the pod itself.
    struct ContainerChip: View {
        let name: String?
        let containerCount: Int32?

        var body: some View {
            Text(name ?? "pod-level")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(name == nil ? .tertiary : .secondary)
                .padding(.horizontal, 5)
                .padding(.vertical, 1)
                .background(RoundedRectangle(cornerRadius: 4).fill(.quaternary.opacity(0.5)))
                .help(help)
        }

        private var help: String {
            guard let name else { return "container_name is empty: the problem is the pod itself" }
            guard let containerCount else { return "container \(name)" }
            return "container \(name) of \(containerCount)"
        }
    }

    /// CountPill is the small capsule that counts folded rows.
    struct CountPill: View {
        let text: String
        let help: String

        var body: some View {
            Text(text)
                .font(.system(size: 10.5, weight: .semibold))
                .foregroundStyle(.secondary)
                .padding(.horizontal, 7)
                .padding(.vertical, 1)
                .background(Capsule().fill(.quaternary.opacity(0.5)))
                .help(help)
        }
    }

    /// ColumnLabel is the right-aligned "open 38m" / "closed 19m ago" cell, with
    /// the column it reads on hover.
    struct ColumnLabel: View {
        let prefix: String
        let timestamp: Timestamp
        let suffix: String
        let field: String
        let now: Date

        var body: some View {
            let elapsed = timestamp.date.map { durationText(from: $0, to: now) } ?? "-"
            return (Text("\(prefix) ") + Text(elapsed).fontWeight(.semibold) + Text(suffix))
                .font(.system(size: 11.5))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: 96, alignment: .trailing)
                .help("\(field) \(clockTime(timestamp, seconds: true)) UTC")
        }
    }

In `IncidentRowView`: `disclosure` becomes `FoldDisclosure(expanded:
expanded, help: expanded ? "fold this pod's incidents" : "show this
pod's other incidents", toggle: { toggleFold?() })`; `containerChip`
becomes `ContainerChip(name: incident.containerName, containerCount:
incident.containerCount)`; `siblingPill` becomes `CountPill(text: "+\(
siblings) on this pod", help: "\(siblings) more incidents share this
pod")`; `label(prefix:timestamp:suffix:field:)` is deleted and
`duration` calls `ColumnLabel(prefix: "closed", timestamp: closedAt,
suffix: " ago", field: "closed_at", now: now)` and `ColumnLabel(prefix:
"open", timestamp: incident.openedAt, suffix: "", field: "opened_at",
now: now)`. The sibling title's `.help` keeps its own text (it is a
title, not a chip). Nothing else in the file moves; the comments that
carry reasons stay where they are.

Checkpoint as task 1 (`make app-test` is unchanged; `make app` is the
evidence). Commit: `app: share the incident row's chip, pill, disclosure
and label`.

Consumes: `IncidentRowView` as step C left it.
Produces: `FoldDisclosure(expanded:help:toggle:)`,
`ContainerChip(name:containerCount:)`, `CountPill(text:help:)`,
`ColumnLabel(prefix:timestamp:suffix:field:now:)`.

## Task 3 - the rollup row and its expansion

`macos/idios/Views/Incidents/IncidentGrouping.swift`: `IncidentGroup`
loses `folds: [PodFold]` and gains `entries: [GroupEntry]`; `group()`
sets `entries: groupEntries(rows, groupID: id)`. Nothing else changes:
`worstCategory`, `openCount` and `rows` are computed as today.

`macos/idios/Views/Incidents/RollupRowView.swift`:

    import IdiosModel
    import SwiftUI

    /// RollupRowView is one line for the pods of a workload that share one
    /// problem: what decides whether to open it up.
    struct RollupRowView: View {
        let rollup: Rollup
        let clusterName: String
        let selectedClusters: Int
        let now: Date
        let expanded: Bool
        let toggle: () -> Void

        private var rows: [Incident] { rollup.rows }
        private var open: Int { rollup.openCount }

        var body: some View {
            HStack(alignment: .top, spacing: 12) {
                FoldDisclosure(
                    expanded: expanded,
                    help: expanded ? "fold these pods into one line" : "show each pod",
                    toggle: toggle)
                CategoryBadge(category: rollup.category, width: 86)
                    .padding(.top, 1)
                VStack(alignment: .leading, spacing: 3) {
                    firstLine
                    secondLine
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                // The column beside counts one row's occurrences; a rollup has no
                // number of its own to put there, and the pill on the first line
                // is where its incidents are counted.
                Color.clear.frame(width: 72, height: 1)
                duration
                Badge(
                    text: open > 0 ? "\(open) open" : "\(rows.count) closed",
                    style: open > 0 ? IncidentState.open.badge : .neutral)
                    .padding(.top, 1)
            }
            .padding(.vertical, 5)
            .opacity(open == 0 ? 0.7 : 1)
        }

        private var firstLine: some View {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text("\(rollup.folds.count) pods")
                    .font(.system(size: 12.5, weight: .semibold))
                ContainerChip(name: rollup.containerName, containerCount: nil)
                Text(rollup.reasons.joined(separator: ", "))
                    .font(.system(size: 11.5, design: .monospaced))
                    .foregroundStyle(.secondary)
                CountPill(
                    text: "\(rows.count) incidents",
                    help: "\(rows.count) incidents on \(rollup.folds.count) pods share this "
                        + "container and category")
            }
        }

        private var secondLine: some View {
            Flow(spacing: 5) {
                Text(
                    scopePrefix(
                        clusterName: clusterName, namespace: rows[0].namespace,
                        selectedClusterCount: selectedClusters))
                if !rollup.exitCodes.isEmpty {
                    separator
                    Text("exit " + rollup.exitCodes.map(String.init).joined(separator: ", "))
                }
                if !rollup.imageTags.isEmpty {
                    separator
                    Text((rollup.imageTags.count == 1 ? "tag " : "tags ")
                        + rollup.imageTags.joined(separator: ", "))
                }
            }
            .font(.system(size: 11, design: .monospaced))
            .foregroundStyle(.secondary)
        }

        private var separator: some View {
            Text("-").foregroundStyle(.quaternary)
        }

        // The problem has been going on since its oldest open row opened; once
        // every row is closed, since the newest of them closed. The stored
        // layout is fixed-width UTC, so string order is time order.
        @ViewBuilder private var duration: some View {
            let openRows = rows.filter { $0.closedAt == nil }
            if let oldest = openRows.map(\.openedAt).min(by: { $0.raw < $1.raw }) {
                ColumnLabel(
                    prefix: "open", timestamp: oldest, suffix: "", field: "oldest opened_at",
                    now: now)
            } else if let newest = rows.compactMap(\.closedAt).max(by: { $0.raw < $1.raw }) {
                ColumnLabel(
                    prefix: "closed", timestamp: newest, suffix: " ago", field: "newest closed_at",
                    now: now)
            }
        }
    }

`rows[0]` is safe: a rollup exists only with two or more pods. The exit
line reads "exit 1" for one code and "exit 1, 137" for two; the pill
counts rows, as the "+N on this pod" pill does; no occurrences are
summed anywhere.

`macos/idios/Views/Incidents/IncidentRowView.swift`: `var indented:
Bool = false` after `isSibling`; the body's `HStack` starts with `if
indented { Spacer().frame(width: 22) }` before the disclosure, so a pod
fold under an expanded rollup and its own siblings step in by one
level. The property's comment: a fold drawn under a rollup steps in so
the eye reads it as part of that line.

`macos/idios/Views/Incidents/IncidentsList.swift`:

- `@State private var expandedRollups: Set<String> = []` beside
  `expandedPods`, with the comment that a rollup's key is its own string
  and not a pod uid, because one pod can sit both inside a rollup and in
  a plain fold of its own (a second problem on that pod), and the two
  lines open independently.
- `rows(_ group:)` walks `group.entries`:

        ForEach(group.entries) { entry in
            switch entry {
            case .fold(let fold):
                foldRows(fold, indented: false)
            case .rollup(let rollup):
                let expanded = expandedRollups.contains(rollup.id)
                RollupRowView(
                    rollup: rollup,
                    clusterName: cluster(rollup.rows[0].clusterID)?.name ?? rollup.rows[0].clusterID,
                    selectedClusters: selectedClusters, now: Date(), expanded: expanded,
                    toggle: { toggleRollup(rollup) })
                    .contentShape(Rectangle())
                    .tag(rollup.id)
                // The pods are out of the list while the rollup is folded, so
                // the arrow keys and the actions reach them only once it is open.
                if expanded {
                    ForEach(rollup.folds) { fold in foldRows(fold, indented: true) }
                }
            }
        }

  and `foldRows(_ fold: PodFold, indented: Bool)` is the lead-and-
  siblings block that `rows` holds today, moved out unchanged except for
  `indented: indented` passed to every `IncidentRowView` it builds.
- `toggleRollup(_ rollup: Rollup)` inserts or removes `rollup.id` in
  `expandedRollups`.
- `rollups: [String: Rollup]` is a computed property over
  `clusterGroups`, every `.rollup` entry keyed by its id; it is what the
  key handlers ask whether a selection is a rollup.
- Return and the double click: `primaryAction` and `.onKeyPress(.return)`
  call a new `openOrToggle(_ id: String)` that toggles the rollup when
  `rollups[id]` exists and otherwise calls `open(id)`; the comment says
  a rollup is not an incident and has no detail to push, so the gesture
  that opens a row opens the rollup up instead.

Checkpoint as task 1. The application is opened against the smoke data
(`IDIOS_DAEMON=127.0.0.1:7771`, the daemon serving `.storage/smoke`);
the CronJob `smoke-cron-fail` has six pods with `app` `crash` rows and
renders as one rollup line under its group, expandable to six indented
pod rows. Commit: `app: fold replica fan-out into one rollup row`.

Consumes: `Rollup`, `GroupEntry`, `groupEntries(_:groupID:)` (task 1);
`FoldDisclosure`, `ContainerChip`, `CountPill`, `ColumnLabel` (task 2);
`Badge`, `CategoryBadge`, `Flow`, `scopePrefix`, `clockTime`.
Produces: `IncidentGroup.entries`, `RollupRowView`,
`IncidentRowView.indented`, `IncidentsList.rollups`,
`IncidentsList.openOrToggle(_:)`.

## Task 4 - actions over a rollup row

`macos/idios/Views/Incidents/IncidentsList.swift`.

    /// RollupAction is the one of the two closing actions a rollup row asks
    /// about before it is carried out over every incident under the row.
    private enum RollupAction {
        case dismiss, resolve
    }

    /// PendingRollupAction is the confirmation a rollup row is waiting on.
    private struct PendingRollupAction {
        let rollup: Rollup
        let action: RollupAction

        var title: String {
            let count = rollup.rows.count
            let workload = workloadTitle(rollup.rows[0])
            switch action {
            case .dismiss: return "Dismiss \(count) incidents of \(workload)?"
            case .resolve: return "Mark \(count) incidents of \(workload) resolved?"
            }
        }

        var verb: String {
            switch action {
            case .dismiss: "Dismiss"
            case .resolve: "Mark resolved"
            }
        }
    }

`@State private var pendingRollup: PendingRollupAction?` beside
`pendingDelete`, with the comment: `a` is reversible and acts at once;
a dismiss or a manual close of twenty incidents in one keystroke
deserves the confirmation Command-Delete already has.

The `a`/`d`/`r` handler:

    .onKeyPress(keys: ["a", "d", "r"]) { press in
        guard press.modifiers.isEmpty, let selection else { return .ignored }
        if let rollup = rollups[selection] {
            switch press.key {
            case "a": rollup.rows.forEach { acknowledge($0.id) }
            case "d": pendingRollup = PendingRollupAction(rollup: rollup, action: .dismiss)
            default: pendingRollup = PendingRollupAction(rollup: rollup, action: .resolve)
            }
            return .handled
        }
        switch press.key {
        case "a": acknowledge(selection)
        case "d": dismiss(selection)
        default: resolve(selection)
        }
        return .handled
    }

Every write goes through the per-incident callbacks the screen already
passes (`acknowledge`, `dismiss`, `resolve`), one call per row, each
row independently; the store's `write` reports each result on its own
and the connection state is one value, so a failure is shown once and
the rows that went through stay written. The comment above the rollup
branch says so.

Command-Delete: the hidden button's action becomes

    if let selection, rollups[selection] == nil { pendingDelete = selection }

with the comment that a delete is per incident and a rollup row is not
one, so the chord does nothing on it. `act(_:)` loses its callers to the
explicit handlers above and is deleted if nothing else calls it.

The confirmation, beside the delete alert:

    // Return cancels here as it does for a delete: a manual close is final,
    // and twenty of them should not ride on the Return reflex.
    .alert(pendingRollup?.title ?? "", isPresented: confirmingRollup) {
        Button(pendingRollup?.verb ?? "") {
            if let pending = pendingRollup {
                let act = pending.action == .dismiss ? dismiss : resolve
                pending.rollup.rows.forEach { act($0.id) }
            }
            pendingRollup = nil
        }
        Button("Cancel", role: .cancel) { pendingRollup = nil }
            .keyboardShortcut(.defaultAction)
    } message: {
        Text(
            "Each incident is written on its own. One that fails is reported once and "
                + "leaves the others done.")
    }

    private var confirmingRollup: Binding<Bool> {
        Binding(get: { pendingRollup != nil }, set: { if !$0 { pendingRollup = nil } })
    }

Checkpoint as task 1. Against the smoke data: select the
`smoke-cron-fail` rollup, press `a`, and the six rows turn acknowledged
in one keystroke (the rollup's badge stays "6 closed" because they are
closed; the sidebar's Attention count drops by six); press `r` and the
question names "6 incidents of smoke-cron-fail"; Command-Delete on the
rollup does nothing. Commit: `app: act on every incident under a rollup
row`.

Consumes: `IncidentsList.rollups` (task 3); `acknowledge`, `dismiss`,
`resolve`, `delete` callbacks as `IncidentsScreen` passes them.
Produces: nothing a later task reads.

## Task 5 - page 1 of the mockup

`docs/mockups/idios-ui.html`, `section#s1`. The two options `1a` and
`1b` are replaced by the two frames of the design canvas, "Main" and
"Rollup" (`Main.dc.html`, `Rollup.dc.html` and `shared.css` in the
`idios-design-canvas` directory beside this repository), drawn in the
mockup's light appearance (`class="win light"`) with the mockup's own
classes where they exist (`.card`, `.card-h`, `.badge`, `.irow-num`,
`.pill`, `.side-item`, `.dot`, `.sq`, `.faint`, `.mono`, `.mid`) and
these classes added to the style block after the `.irow` rules, copied
from `shared.css` and given the `.light` prefix the neighbouring rules
use: `.prow`, `.prow:last-child`, `.prow.sel`, `.prow .disc`,
`.prow-main`, `.prow-1`, `.prow-pod`, `.prow-2`, `.cchip`, `.cchip .nm`,
`.cchip .cat`, `.cchip.quiet`, `.cchip .n`, `.prow-3`, `.prow-3 s`,
`.crow`, `.crow .badge`, `.rrow`. The frames change in these ways and
no other:

- The state pills read `All`, `Attention 20` (the `hot` pill),
  `Open 6`, `Acknowledged 2`, `Recovered 9`, `Pod deleted 14`,
  `Job finished 4`, `Marked resolved 1`, `Dismissed 3`; the toolbar
  title is `Incidents needing attention` and the chip is `attention x`
  with the hover text `open, or closed within attention_window and never
  acknowledged or dismissed`.
- The category list gains `unclean exit` (red dot, no count) after
  `oom`, so it is the ranked list in full.
- The `ledger-worker` rollup row's note reads `same container, same
  category` (the reason is not in the key), and its chip's count reads
  `ImagePullBackOff - 3 pods - 3 incidents`.
- The rollup frame's pod-row chips read `Error, CrashLoopBackOff - exit
  1 - 20 pods - 20 incidents` on the first card and `Error,
  CrashLoopBackOff - exit 1 - 20 pods` on the second; the container
  `linkerd-proxy` becomes `mesh-proxy`, an invented name.
- Every rollup row's badge counts incidents (`20 open`, `3 open`), as
  the canvas already has it.
- The 1a label reads `Triage list, light, grouped by workload: the pod
  is the row, its containers the second line`; the 1b label reads `The
  rollup: 20 pods, one problem, one row - collapsed above, expanded
  below`. The two `faint` sentences inside the Rollup frame stay.

The page note is rewritten in the present tense, one paragraph, in the
mockup's voice, saying: the home screen answers "what needs attention":
`attention` is open, or closed within `attention_window` and never
acknowledged or dismissed, so a row that closed an hour ago and nobody
has looked at is still in front of the person, and after the window it
leaves the default view on its own; Attention is the default pill and
Open stands beside it; cluster is a scope, not a grouping (the
titlebar menu, kept across screens, every count recomputed for it, the
`cluster / namespace` prefix while more than one cluster is selected);
state and category are filters that compose into one query and read as
removable chips, the default `state: attention` included; a cluster
whose `last_error` is set draws its dot red; Group by is one of five
modes (the same sentence as today about workload, namespace and time);
every group header folds and stays readable folded (identity, "N
incidents, M open", the worst open category as a dot, the newest
`last_seen_at`); the pod is the row: rows sharing a `pod_uid` fold under
one lead, the open row before a closed one, then the worst category, the
lead carrying a "+N on this pod" pill that counts rows while every row
shows its own `occurrences`; replica fan-out folds again: the rows of
one workload that share a container and a category across two or more
pods are one rollup row naming the count of pods, the reasons and the
exit codes found among them, and expanding to the pod rows; `last_reason`
is not in the key because a crash loop alternates its reasons; every
count is a count of incidents and nothing sums occurrences; a pod that
no longer exists keeps its incident and says so, with the inferred
`deletion_reason` labelled as inferred; the keyboard: the arrow keys walk
the rows, Return opens the selected incident or opens a rollup up, `a`,
`d` and `r` acknowledge, dismiss and resolve the selected row or every
incident under a rollup (`d` and `r` ask first, naming the count and the
workload), Command-Delete deletes one incident behind a confirmation and
does nothing on a rollup, and the Go menu holds Cmd-1 Incidents, Cmd-2
Workloads, Cmd-3 Status and Cmd-[ Back. No sentence says what changed.

Checkpoint: `make ascii` (the file is ASCII; `&#9662;` and `&gt;` are
entities and stay) and the page opened in a browser and read once
against the canvas. Commit: `docs: redraw the mockup's incidents page`.

Consumes: the two canvas frames; the default pill and the fold of
step C.
Produces: page 1 as m9 step A expects to find it.

## Task 6 - the smoke cluster reproduces the rollup

`hack/smoke/crash-loop.yaml`: `replicas: 3`, and a comment at the top of
the file:

    # Three replicas of one crash loop: the smoke tables print three crash
    # incidents for this Deployment, one per pod, and the application's list
    # folds them into one rollup row.

Nothing else changes; `run.sh`'s queries print whatever is there.

Checkpoint: `make ascii`; then the smoke run. The dev daemon on 7771
serves `.storage/smoke` and `run.sh` recreates that directory, so it is
stopped first, `make smoke PORT=7771` runs, and the daemon is started
again on the new data: `./bin/idios -data-dir .storage/smoke -kubeconfig
./kube/config -listen 127.0.0.1:7771 run`. The `incidents` table of the
run prints three `crash` rows for `smoke-crash` (one per pod) and the
application shows them as one rollup line. The controller runs the
smoke, not the implementer, because it stops a process the user
started. Commit: `smoke: run the crash loop at three replicas`.

Consumes: nothing.
Produces: `.storage/smoke` carrying the rollup shape for the screenshots
the user takes.

## Hands to the next step

m9 step A finds page 1 of `docs/mockups/idios-ui.html` carrying the list
and the rollup as the canvas drew them, with Attention as the default
pill; it replaces pages 2 to 4 and leaves page 1 alone. m9 step C reads
`GroupEntry` and `Rollup` from `Rollup.swift` if the pod page's left
column ever needs the fan-out, and `IncidentRowView.indented` for a row
drawn under another. The rollup's expansion key (`Rollup.id`, a string
built from the group id and the shared identity) is separate from the
pod fold's `podUID`.

## Self-review

Spec coverage: the rollup clause of 9.3 lands in task 1 (the key, the
count of pods, the reasons and exit codes, the one-pod key, the bare
pod, the job row, the group boundary), task 3 (the line, the pill, the
expansion by Return and double click) and task 4 (`a` at once, `d` and
`r` behind the question, Command-Delete refused); decision 9's "in the
application only" holds (no daemon file is touched); decision 7 holds
(no view sums occurrences; the rollup's count column is empty and its
pill counts rows); decision 10 holds (`checkout-api`, `ledger-worker`,
`consumer-default`, `mesh-proxy`, `worker-1`, `node-a`,
`registry.example.internal`). The mockup redraw and the smoke fixture
are the roadmap's two remaining sentences for step D.

Rulings this plan makes inside the decisions: the rollup key carries
the workload identity so that a namespace, category, cluster or time
group never rolls two workloads together, and a bare pod never rolls up
(replica fan-out belongs to a controller); the pod incidents of a
CronJob's retries do roll up, because the decision excludes only the
job-subject row; the rollup row's count column is empty and the pill
counts its incidents; the rollup's badge reads "N open" or "N closed";
Return and a double click on a rollup open it up because it has no
detail to push; the shared row pieces are extracted in a task of their
own so the feature commit carries no refactor; the mockup's page 1
carries the canvas frames, whose pod row draws one chip per container,
while the application's pod fold keeps step C's lead row with the
"+N on this pod" pill, because the roadmap names the canvas frames for
this page and names only decision 9 for the application.

`.ai` rules: ascii (checked per task); tests trace to the 9.3 rollup
clause, variants are table rows, assertions are whole `[GroupEntry]`
and a whole tuple; no test of the language (no test that `GroupEntry.id`
returns what was put in, that `distinct` removes duplicates on its own);
comments carry the crash-loop fact behind the key, the reason the count
column is empty, the reason Return opens the rollup, the reason the
confirmation exists and the reason the two expansion sets are separate;
code-is-truth: no doc named from code, the presentation doc changes in
the task whose tests trace to it, the mockup states what is; scope: no
daemon change, no store method for batches (the per-incident calls are
the decision), no window in the application, no pod-row redraw of the
application's fold; commits per task, none red, none without the user's
review.

Type consistency: `groupEntries(_:groupID:)` takes `[Incident]` and
`String` and returns `[GroupEntry]` in tasks 1 and 3; `Rollup.folds` is
`[PodFold]` and `Rollup.rows` is `[Incident]` in tasks 1, 3 and 4;
`IncidentGroup.entries` is `[GroupEntry]` in task 3; `ContainerChip.
containerCount` is `Int32?` and `IncidentRowView` passes
`incident.containerCount` (`Int32`) in task 2 while `RollupRowView`
passes `nil` in task 3; `ColumnLabel.now` is `Date` like the rows'
`now`; `rollups` is `[String: Rollup]` in tasks 3 and 4;
`PendingRollupAction.rollup` is `Rollup`.
