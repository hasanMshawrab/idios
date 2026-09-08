# m9 step C - application: the pod page

Goal: after this plan a pod has one page in the application, reached
from anywhere, and it is where a person reads what is going on inside
that pod: the pod and its containers as cards on the left, whatever is
selected in the middle, the rail on the right. `incident/<id>`,
`timeline/<id>`, `pod/<uid>` and `pod/<uid>/<tab>` all open it; the
incident detail screen, the pod screen and the timeline screen are gone,
with their three stores and the cards only they drew. The application
sidebar steps aside while the page is up and comes back with Back or a
Go menu item. Pod data appears once, container data once, incident data
behind a segmented control whose whole purpose is switching. The
presentation doc's screen table says so, and `CLAUDE.md` names the routes
the way they now resolve.

Architecture: `Route` and `PodTab` move into `IdiosModel`, where the
path-to-route mapping is a pure function the package tests can see;
`Navigator` and `Route.launched` stay in the application (they read
`CommandLine` and `Observation`). A second `IdiosModel` file,
`PodPage.swift`, holds the pure functions the page draws from: the
selection type, the pane a `PodTab` maps onto, the segment order (the
fold order of `Fold.swift`, extracted as one comparator), the container
order, which events belong to a container, the pod's state tag, the
container's state line, the rail's sibling rows and the "+ N more" count.
The application gains two stores: `IncidentWriteStore` (the nine incident
writes `IncidentDetailStore` held, so the list's delete and the page's
actions share one client) and `PodPageStore` (`GetPod` and `PodEvents` for
the page, `GetIncident` for the lit segment kept until the next one
arrives, the timeline of the lit segment, the history for Conditions, the
Job's related incidents, the artifact bytes, one incident stream filtered
on the pod's uid for the life of the page). The views live under
`macos/idios/Views/PodPage/`; the cards the page keeps (`KubeletCard`,
`EventsCard`, `CapturedLogsCard`, `JobCard`, `TimelineView`,
`CapturedFilesCard`, `PodIncidentsCard`, `PodHistoryCards`, the sheets)
stay where they are and take the parameters the page needs; the shared
pieces the old screens defined (`DetailCard`, `FactRow`, the artifact
order helpers, `RailHeading`, `RailRow`, `DeletedBanner`, `utcMinute`)
move to `Views/Components/`. `IncidentsScreen` routes the three route
families onto the page, collapses the split view's sidebar column while
one is pushed, and restores what the person had when it pops.

Tech stack: Swift 6 package `macos/Sources/IdiosModel` with tests in
`macos/Tests/IdiosModelTests` (`make app-test`); the Xcode application
(`make app`; a file under `macos/idios/` joins the target by existing,
a deleted file leaves it). No proto change. SwiftUI `NavigationSplitView`
with `columnVisibility`, `NavigationStack(path:)`. Screenshots through
`hack/macos/screenshot.sh`, which needs Screen Recording permission and
is run by the user.

Spec: `docs/design/presentation.md` section 9.3 (the screens table: the
Incident detail, Incident timeline and Pod rows become one Pod page row,
written by task 5 of this plan), section 9.4 (the keyboard layer, the
prose tables as custom rows), the "Sibling counts" sentence of section
8 and the Grafana "three places" sentence of section 4.8 as task 5
amends them. Roadmap decisions 1 to 5 and 7 to 10 of
`docs/plans/m9-pod-page/roadmap.md`; step B's handoff in
`docs/plans/m9-pod-page/siblings.md` (`SiblingPod.worstOpenCategory`,
`PodDetail.siblings`, `PodDetail.siblingTotal`). Visual reference: pages
2 to 4 of `docs/mockups/idios-ui.html`.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Swift follows the Go rules: one-line doc comment per
public type and function, comments say why, tests trace to a spec
statement with whole-value assertions, variants are table rows. Views
never import `IdiosAPI`; stores own every client call and stream; a
cancelled call maps to `APIError.cancelled` and every store ignores it.
The only hard-coded colours stay in `BadgeStyle.swift`. Every count is a
count of incidents; no view sums occurrences. A bare-letter key goes
through `.onKeyPress` on the focused view. Never
`.fixedSize(horizontal: false, vertical: true)` on a concatenated
multi-font `Text` in a header. Nothing in a test, comment, doc or
fixture names a real organisation, cluster, namespace, workload, image
or node; the invented names are the ones the tests use (`checkout-api`,
`idios-smoke`, `api`, `init-db`, `trace-agent`, `node-a`). Checkpoint
before every commit: `go build ./... && go test ./... && make ascii &&
make app-test && make app`. Implementers never commit; nothing is
committed without the user's review of the diff.

## File structure

    macos/Sources/IdiosModel/Route.swift                  Route, PodTab, Route(path:) (task 1)
    macos/Tests/IdiosModelTests/RouteTests.swift          the path mapping (task 1)
    macos/idios/App/Route.swift                           Route.launched and Navigator only (task 1)
    macos/Sources/IdiosModel/Fold.swift                   foldOrder extracted from podFolds (task 2)
    macos/Sources/IdiosModel/PodPage.swift                PodPane, ContainerTab, PodSelection, PodTab.pane, segmentOrder, containerOrder, eventContainer, containerEvents, podStateTag, containerStateLine, SiblingRow, siblingRows, moreSiblings (task 2)
    macos/Tests/IdiosModelTests/PodPageTests.swift        every function above (task 2)
    macos/idios/Store/IncidentWriteStore.swift            the nine writes and their row overloads (task 3)
    macos/idios/Store/PodPageStore.swift                  the page's reads, the lit segment, the stream (task 3)
    macos/idios/Views/Incidents/IncidentsScreen.swift     deletes becomes IncidentWriteStore (task 3); routing, sidebar collapse, openWorkloadPods (task 5)
    macos/idios/Views/Components/DetailCard.swift         DetailCard, FactRow (task 4)
    macos/idios/Views/Components/ArtifactOrder.swift      artifactOrder, defaultArtifact, artifactLabel (task 4)
    macos/idios/Views/Components/Rail.swift               RailHeading, RailRow, DeletedBanner, utcMinute (task 4)
    macos/idios/Views/IncidentDetail/KubeletCard.swift    takes a container, the phase and QoS class, an optional incident and job; the envelope row; ResourceEnvelopeCard deleted (task 4)
    macos/idios/Views/IncidentDetail/EventsCard.swift     container column, the pod marker (task 4)
    macos/idios/Views/IncidentDetail/IncidentHeader.swift the identity line leaves; headerSentence stays (task 4)
    macos/idios/Views/Pod/PodContainers.swift             ContainersTable deleted; CapturedFilesCard gains a container column and a title; PodIncidentsCard gains title and meta (task 4)
    macos/idios/Views/IncidentDetail/IncidentDetailScreen.swift   adapted to the new card signatures (task 4), deleted (task 5)
    macos/idios/Views/Pod/PodScreen.swift                 adapted (task 4), deleted (task 5)
    macos/idios/Views/PodPage/PodPageScreen.swift         the screen: state, routes, keys, sheets, actions (task 5)
    macos/idios/Views/PodPage/PodPageHeader.swift         the pod's header (task 5)
    macos/idios/Views/PodPage/PodColumn.swift             the Pod card and the container cards (task 5)
    macos/idios/Views/PodPage/ContainerPane.swift         segments, the lit incident's header, Overview, Timeline, Logs (task 5)
    macos/idios/Views/PodPage/PodPane.swift               Events, Conditions, Files, pod.json, Related incidents (task 5)
    macos/idios/Views/PodPage/PodPageRail.swift           owner chain, context, times, siblings (task 5)
    macos/idios/Views/Workloads/WorkloadsScreen.swift     the tab moves into WorkloadsTreeState (task 5)
    macos/idios/Views/IncidentDetail/IncidentRail.swift, PodFactsView.swift, RelatedIncidentsCard.swift   deleted (task 5)
    macos/idios/Views/Pod/PodRail.swift                   deleted (task 5)
    macos/idios/Store/IncidentDetailStore.swift, PodStore.swift, TimelineStore.swift   deleted (task 5)
    docs/design/presentation.md                           the screens table row, the three sentences (task 5)
    CLAUDE.md                                             the screenshot route list names the page (task 5)

## Task 1 - decision 9: Route and PodTab in IdiosModel

`macos/Sources/IdiosModel/Route.swift` receives the `Route` enum and
`init?(path:)` from `macos/idios/App/Route.swift`, public, with
`IncidentState` already in the package. `PodTab` moves from
`PodScreen.swift` into the same file, its `title(detail:events:)` staying
behind in `PodScreen.swift` as a `PodTab` extension until task 5 deletes
that screen (the package must not know the view's titles):

    /// Route is one addressable screen, as the -route launch argument and the
    /// menu bar spell it.
    public enum Route: Hashable, Sendable {
        case incidents(IncidentState?)
        case incident(String)
        case timeline(String)
        case pod(String, PodTab)
        case workloads
        case workload(cluster: String, namespace: String, kind: String, name: String)
        case status
        /// addCluster opens the add-cluster sheet over whatever screen was
        /// showing, for the screenshot script.
        case addCluster
        /// clusters is the clusters-and-namespaces sheet, opened from the menu
        /// bar and from a sidebar cluster row's context menu.
        case clusters
        /// menubar draws the status item popover in a plain window; the window
        /// server cannot photograph the popover itself.
        case menubar
    }

    /// PodTab is a pane name a pod route carries; its raw value is what the
    /// route spells.
    public enum PodTab: String, CaseIterable, Hashable, Sendable {
        case containers
        case incidents
        case events
        case conditions
        case logs
        case podJSON = "podjson"
    }

`init?(path:)` is the existing body verbatim (a two-part `pod` route
takes `.containers`, as today). The application file keeps `Route.launched`
(as an extension on `IdiosModel.Route`) and `Navigator`; it imports
`IdiosModel` already. Every file that named `Route` or `PodTab` already
imports `IdiosModel` (`IncidentsScreen`, `WorkloadsScreen`,
`MenuBarView`, `IdiosApp`, `PodScreen`); if one does not, add the import.

Tests first, `macos/Tests/IdiosModelTests/RouteTests.swift`, one table
test `routePathsMapOntoTheScreensTheyName`, whole `Route?` assertions,
traced to the route sentence of the Pod page row of section 9.3 (task 5
writes it; the plan is read whole first) and to the existing routes of
`CLAUDE.md`'s screenshot list. Rows, invalid first:

| path | want |
|---|---|
| `""` | `nil` |
| `"bogus"` | `nil` |
| `"incidents/nonsense"` | `nil` |
| `"incidents/open/extra"` | `nil` |
| `"pod"` | `nil` |
| `"pod/u1/bogus"` | `nil` |
| `"workload/only/three/parts"` | `nil` |
| `"incidents"` | `.incidents(nil)` |
| `"incidents/attention"` | `.incidents(.attention)` |
| `"incidents/pod_deleted"` | `.incidents(.podDeleted)` |
| `"incident/412"` | `.incident("412")` |
| `"timeline/412"` | `.timeline("412")` |
| `"pod/u1"` | `.pod("u1", .containers)` |
| `"pod/u1/podjson"` | `.pod("u1", .podJSON)` |
| `"pod/u1/conditions"` | `.pod("u1", .conditions)` |
| `"workloads"` | `.workloads` |
| `"workload/orbstack/shop/Deployment/checkout-api"` | `.workload(cluster: "orbstack", namespace: "shop", kind: "Deployment", name: "checkout-api")` |
| `"status"` | `.status` |
| `"addcluster"` | `.addCluster` |
| `"clusters"` | `.clusters` |
| `"menubar"` | `.menubar` |

Run `make app-test`: the test fails to compile on the missing type. Move
the code, run again, then the checkpoint (`make app` proves every
application caller still compiles).

Checkpoint: `go build ./... && go test ./... && make ascii && make
app-test && make app`. Commit: `app: move Route and PodTab into
IdiosModel`.

Consumes: `IncidentState` of `Enums.swift`; the application's `Route`
and `PodTab` as they are.
Produces: `IdiosModel.Route` (the cases above, `init?(path:)`),
`IdiosModel.PodTab` (six cases, raw values).

## Task 2 - the page's pure functions

`macos/Sources/IdiosModel/Fold.swift`: the comparator inside `podFolds`
becomes a public function the segment order reuses, total so the order
never depends on the sort's stability:

    /// foldOrder ranks two rows of one pod the way the list's fold and the
    /// page's segments read them: an open row before a closed one, then the
    /// worst category, then the newest last seen, then the higher id.
    public func foldOrder(_ a: Incident, _ b: Incident) -> Bool {
        if (a.closedAt == nil) != (b.closedAt == nil) { return a.closedAt == nil }
        if a.category.rank != b.category.rank { return a.category.rank < b.category.rank }
        if a.lastSeenAt.raw != b.lastSeenAt.raw { return a.lastSeenAt.raw > b.lastSeenAt.raw }
        return a.id > b.id
    }

and `podFolds` picks `bucket.rows.min(by: foldOrder)`; its comment about
`min(by:)` and served order goes, because the order is total now. Run
`FoldTests` and `RollupTests` after the change: they must pass unchanged
(the rows they build already differ in last seen).

`macos/Sources/IdiosModel/PodPage.swift`:

    /// PodPane is one tab of the Pod card.
    public enum PodPane: String, CaseIterable, Hashable, Sendable {
        case events, conditions, files, podJSON, related
    }

    /// ContainerTab is one tab of a container's pane.
    public enum ContainerTab: String, CaseIterable, Hashable, Sendable {
        case overview, timeline, logs
    }

    /// PodSelection is what the middle pane of the pod page shows: the Pod card
    /// on one of its tabs, or one container with the incident whose segment is
    /// lit (nil when the container has none) and the pane's tab.
    public enum PodSelection: Hashable, Sendable {
        case pod(PodPane)
        case container(name: String, incidentID: String?, tab: ContainerTab)
    }

    extension PodTab {
        /// pane is the Pod card tab a pod route opens: the left column is the
        /// container and incident list, so the three tabs that listed them open
        /// Events.
        public var pane: PodPane {
            switch self {
            case .containers, .incidents, .events: .events
            case .conditions: .conditions
            case .logs: .files
            case .podJSON: .podJSON
            }
        }
    }

    /// segmentOrder is the order of a container's incidents in the segmented
    /// control: the fold order, so the segment that is lit on arrival is the
    /// row the list would have led with.
    public func segmentOrder(_ rows: [Incident]) -> [Incident] { rows.sorted(by: foldOrder) }

    /// containerOrder puts the containers the way the pod runs them: init
    /// containers first, then the app, then the sidecars, ephemeral last, by
    /// name inside a kind.
    public func containerOrder(_ containers: [Container]) -> [Container] {
        containers.sorted { a, b in
            if a.kind != b.kind { return kindRank(a.kind) < kindRank(b.kind) }
            return a.name < b.name
        }
    }

    private func kindRank(_ kind: ContainerKind) -> Int {
        switch kind {
        case .`init`: 0
        case .app: 1
        case .sidecar: 2
        case .ephemeral: 3
        }
    }

    /// eventContainer reads the container an event's field_path names, or nil
    /// for a pod-level event; the kubelet writes spec.containers{name},
    /// spec.initContainers{name} and spec.ephemeralContainers{name}.
    public func eventContainer(fieldPath: String?) -> String? {
        guard let fieldPath, let open = fieldPath.firstIndex(of: "{"),
            let close = fieldPath.lastIndex(of: "}"), open < close
        else { return nil }
        let name = fieldPath[fieldPath.index(after: open)..<close]
        return name.isEmpty ? nil : String(name)
    }

    /// containerEvents keeps the pod's events that name the container plus
    /// every pod-level event, in served order, because the event that explains
    /// a death is usually the pod's (Scheduled, Killing, Evicted, Preempted).
    public func containerEvents(_ events: [Event], container: String) -> [Event] {
        events.filter { event in
            let named = eventContainer(fieldPath: event.fieldPath)
            return named == nil || named == container
        }
    }

    /// podStateTag is the page header's tag: DELETED for a pod that is gone,
    /// else the phase and, when a Ready condition was read, READY or NOT READY.
    public func podStateTag(phase: String, deleted: Bool, ready: Bool?) -> String {
        if deleted { return "DELETED" }
        switch ready {
        case .some(true): return "\(phase.uppercased()), READY"
        case .some(false): return "\(phase.uppercased()), NOT READY"
        case .none: return phase.uppercased()
        }
    }

    /// containerStateLine is the one line a container card says under its name:
    /// the state, its reason or readiness, and what the kubelet counted.
    public func containerStateLine(_ container: Container) -> String {
        var parts = [container.state.rawValue]
        switch container.state {
        case .running: parts.append(container.ready ? "ready" : "not ready")
        case .waiting, .terminated: if let reason = container.reason { parts.append(reason) }
        }
        if container.state == .terminated, let exit = container.exitCode {
            parts.append("exit \(exit)")
        } else {
            parts.append("\(container.restartCount) restarts")
        }
        return parts.joined(separator: " - ")
    }

    /// SiblingRow is one line of the rail's siblings: the page's own pod first,
    /// then the daemon's five.
    public struct SiblingRow: Hashable, Sendable {
        public let uid: String
        public let name: String
        public let isThisPod: Bool
        public let deleted: Bool
        public let ready: Bool
        public let category: Category?
    }

    /// siblingRows puts the page's pod at the top of the daemon's siblings,
    /// so the rail always shows where this pod stands among them.
    public func siblingRows(pod: PodRow, ready: Bool?, siblings: [SiblingPod]) -> [SiblingRow] {
        [SiblingRow(
            uid: pod.uid, name: pod.name, isThisPod: true, deleted: pod.deletedAt != nil,
            ready: ready ?? false, category: nil)]
            + siblings.map {
                SiblingRow(
                    uid: $0.uid, name: $0.name, isThisPod: false, deleted: $0.deletedAt != nil,
                    ready: $0.ready, category: $0.worstOpenCategory)
            }
    }

    /// moreSiblings is how many pods of the controller the rail does not show:
    /// the total counts this pod, so it and the shown siblings come off.
    public func moreSiblings(total: Int32, shown: Int) -> Int {
        max(Int(total) - 1 - shown, 0)
    }

If `ContainerState` has cases beyond `running`, `waiting` and
`terminated`, cover them in the `switch` with no extra part. The page's
own pod row leaves `category` nil because its badges are the left
column; the rail says "this pod" there.

Tests first, `macos/Tests/IdiosModelTests/PodPageTests.swift`, whole-value
assertions, one table test per function, traced to the Pod page row of
section 9.3 (task 5) as named per test:

- `podRoutesOpenThePodCardOnThePaneTheyNamed`: every `PodTab` to its
  `PodPane` (six rows; the route sentence).
- `segmentsReadInTheFoldOrder`, using `row(...)` of `FoldTests.swift`
  and `t1 > t2 > t3` local constants: empty -> empty; a closed `crash`
  at t1 and an open `probe` at t3 -> probe then crash; two open rows
  `probe` at t1 and `crash` at t3 -> crash then probe; two open `crash`
  rows at t2 (id `"1"`) and t1 (id `"2"`) -> `"2"` then `"1"`; two open
  `crash` rows both at t1, ids `"1"` and `"2"` -> `"2"` then `"1"` (the
  segmented control sentence).
- `containersReadInitThenAppThenSidecar`, with a local
  `container(_ name: String, kind: ContainerKind)` builder copying
  `apiContainer` of `PodTests.swift` (make `apiContainer` file-internal
  rather than private if it is private, as `FoldTests` did for `row`):
  empty -> empty; `[sidecar "trace-agent", app "api", init "init-db"]`
  -> init-db, api, trace-agent; two sidecars `"b"`, `"a"` -> a, b (the
  left column sentence).
- `eventFieldPathsNameTheirContainer`: nil -> nil; `""` -> nil;
  `"spec.containers{api}"` -> `"api"`; `"spec.initContainers{init-db}"`
  -> `"init-db"`; `"spec.ephemeralContainers{debug}"` -> `"debug"`;
  `"spec.containers{}"` -> nil; `"status"` -> nil.
- `aContainerPaneShowsItsOwnEventsAndThePods`, with events built from
  the fixture event of `EventTests.swift` varying `fieldPath` only
  (`event(_ id: String, fieldPath: String?)`; open the builder up as
  above): empty -> empty; `[api-1 "spec.containers{api}", pod-1 nil,
  side-1 "spec.containers{trace-agent}"]` for `api` -> api-1, pod-1 in
  that order (the container events sentence).
- `theStateTagSaysDeletedOrThePhaseAndReadiness`: `("Running", true,
  true)` -> `"DELETED"`; `("Running", false, true)` -> `"RUNNING,
  READY"`; `("Running", false, false)` -> `"RUNNING, NOT READY"`;
  `("Pending", false, nil)` -> `"PENDING"`; `("Succeeded", false, false)`
  -> `"SUCCEEDED, NOT READY"` (the header sentence).
- `theContainerLineSaysStateReasonAndCount`: running ready 0 restarts
  -> `"running - ready - 0 restarts"`; running not ready 2 ->
  `"running - not ready - 2 restarts"`; waiting `CrashLoopBackOff` 8 ->
  `"waiting - CrashLoopBackOff - 8 restarts"`; waiting with nil reason 0
  -> `"waiting - 0 restarts"`; terminated `Completed` exit 0 ->
  `"terminated - Completed - exit 0"`; terminated `Error` exit 1 ->
  `"terminated - Error - exit 1"`; terminated with nil exit code and nil
  reason, 3 restarts -> `"terminated - 3 restarts"`.
- `theRailListsThisPodThenTheDaemonsSiblings`, with `crashPodRow` and
  the five `SiblingPod` values `PodTests.swift` already spells: `(ready:
  false, siblings: [])` -> one row `isThisPod: true, deleted: false,
  ready: false, category: nil`; `(ready: true, siblings: the five)` ->
  six rows, the first this pod, then the five in served order with
  `category` `.imagePull, nil, nil, nil, nil` and `deleted` false four
  times then true (the siblings sentence).
- `theMoreLineCountsWhatTheRailDoesNotShow`: `(0, 0)` -> 0; `(1, 0)` ->
  0; `(8, 5)` -> 2; `(3, 5)` -> 0; `(6, 5)` -> 0.

Checkpoint as task 1. Commit: `model: add the pod page's pure
functions`.

Consumes: `Incident`, `Container`, `ContainerKind`, `ContainerState`,
`Event`, `PodRow`, `SiblingPod`, `Category.rank`, `PodTab` of task 1.
Produces: `foldOrder`, `PodPane`, `ContainerTab`, `PodSelection`,
`PodTab.pane`, `segmentOrder`, `containerOrder`, `eventContainer`,
`containerEvents`, `podStateTag`, `containerStateLine`, `SiblingRow`,
`siblingRows`, `moreSiblings`, all public in `IdiosModel`.

## Task 3 - the stores

`macos/idios/Store/IncidentWriteStore.swift`: the nine writes of
`IncidentDetailStore` (`acknowledge`, `unacknowledge`, `resolve`,
`unresolve`, `dismiss`, `undismiss`, `setNote`, `delete`) and their
private `row(_:)` overloads move here verbatim, with one change of shape:
instead of updating a detail the store holds, each write answers the row
the daemon returned, so the caller decides what to replace:

    /// IncidentWriteStore is the incident writes of section 8, shared by the
    /// list's delete and the pod page's actions so there is one client for
    /// them. actionError is the last write's failure, kept apart from any
    /// load error so a button's failure shows without hiding the page.
    @Observable @MainActor
    final class IncidentWriteStore {
        private(set) var actionError: APIError?

        /// acknowledge stamps the incident acknowledged once; the daemon answers
        /// the current row either way.
        func acknowledge(id: String, connection: DaemonConnection) async -> Incident? {
            await write(connection: connection) {
                try Self.row(try await connection.client.AcknowledgeIncident(
                    path: .init(id: id), body: .json(.init(id: id))))
            }
        }
        // unacknowledge, resolve, unresolve, dismiss, undismiss, setNote follow.

        /// delete removes the row and its files now; true only on .ok.
        func delete(id: String, connection: DaemonConnection) async -> Bool { ... as today ... }

        private func write(
            connection: DaemonConnection, call: () async throws -> Components.Schemas.IncidentRow
        ) async -> Incident? {
            do {
                let incident = try Incident(wire: try await call())
                reportAction(nil, connection: connection)
                return incident
            } catch {
                reportAction(apiError(error), connection: connection)
                return nil
            }
        }
    }

`reportAction` and the seven `row` overloads are the existing code.
`IncidentsScreen.deletes` becomes `IncidentWriteStore()`; its one call
(`deletes.delete(id:connection:)`) keeps its shape. `IncidentDetailStore`
is left as it is until task 5 deletes it; the duplication lives for two
commits of this branch and no longer.

`macos/idios/Store/PodPageStore.swift`:

    /// PodPageStore holds one pod page: the pod as GetPod answers it, its whole
    /// event stream, the incident whose segment is lit, that incident's
    /// timeline, the history for Conditions, the Job's related rows, the
    /// bytes of the files a person opened, and the last error of each.
    @Observable @MainActor
    final class PodPageStore {
        private(set) var detail: PodDetail?
        /// events is nil until PodEvents answered, so the Pod card does not claim
        /// a count it has not seen.
        private(set) var events: [Event]?
        private(set) var eventsTruncated = false
        /// lit is the GetIncident of the lit segment, kept until the next one
        /// arrives so a segment switch never blanks the pane's header.
        private(set) var lit: IncidentDetail?
        private(set) var litError: APIError?
        private(set) var timeline: [TimelineEntry] = []
        private(set) var timelineTruncated = false
        private(set) var timelineError: APIError?
        private(set) var history: PodHistory?
        /// related is the Job's other incidents, for the Related tab of a Job's
        /// pod; a failure leaves the tab empty rather than hiding the pod.
        private(set) var related: [Incident] = []
        private(set) var isLoading = false
        private(set) var error: APIError?
        /// missing is the pod row having been swept while an incident that
        /// names it outlived it: nothing is broken, there is nothing to show.
        private(set) var missing = false

        let writes = IncidentWriteStore()
        private let artifacts = ArtifactContentStore()
        var contents: [String: Result<Data, APIError>] { artifacts.contents }

        /// resolve answers the pod uid and container an incident route opens
        /// on, and seeds lit with the detail it fetched; nil when the incident
        /// is unknown or names no pod (its error is in litError).
        func resolve(incidentID: String, connection: DaemonConnection) async
            -> (podUID: String, container: String?)?

        /// watch loads the pod and its events, then reloads the pod (and the lit
        /// incident when the row was its) on every stream event whose row is
        /// on this pod, until cancelled; it reloads before every resubscribe
        /// because the stream has no replay and a reconnect gap loses events.
        func watch(uid: String, connection: DaemonConnection) async

        /// reload fetches the pod and its events again, as the window becoming
        /// key asks for.
        func reload(uid: String, connection: DaemonConnection) async

        /// light fetches the incident whose segment is lit; the previous one
        /// stays until this one arrives.
        func light(incidentID: String, connection: DaemonConnection) async

        /// loadTimeline fetches the lit incident's timeline.
        func loadTimeline(incidentID: String, connection: DaemonConnection) async

        /// loadHistory fetches the transitions and condition history.
        func loadHistory(uid: String, connection: DaemonConnection) async

        /// loadRelated fetches the Job's incidents other than the lit one.
        func loadRelated(jobUID: String, exceptID: String?, connection: DaemonConnection) async

        /// content fetches a captured file's bytes once.
        func content(of artifact: Artifact, connection: DaemonConnection) async

        /// apply replaces the lit incident's row with what a write answered and
        /// re-reads the pod so the badges on the cards stay true.
        func apply(_ incident: Incident, uid: String, connection: DaemonConnection) async
    }

Bodies follow the stores they replace: `resolve` is
`IncidentDetailStore.load` answering `(detail.incident.podUID ??
detail.pod?.uid, detail.incident.containerName)`, setting `lit` on
success and `litError` otherwise, and returning nil when the uid is nil
(`litError` then carries `.notFound`-shaped text "incident N names no
pod"; build it the way `APIError` is built elsewhere in the stores, or
keep a plain `String?` `litMessage` if `APIError` has no such case, and
say which in the report). `watch` is `IncidentDetailStore.watch` with
`guard event.data?.podUid == uid` and, inside the loop, `await
loadPod(uid:)` and `await loadEvents(uid:)`; when `event.data?.id ==
lit?.incident.id` also `await light(...)`. `loadPod` is
`PodStore.load`, setting `missing = true` on `.notFound` the way
`IncidentDetailStore.notePod` did and `Screenshot.noteFirstLoad()` after
it. `loadEvents` is `PodStore.loadEvents`; `loadHistory` is
`PodStore.loadHistory`; `loadTimeline` is `TimelineStore.reload` into
the three timeline fields; `loadRelated` is
`IncidentDetailStore.loadRelated` taking the job uid directly; `light`
is `IncidentDetailStore.load` without the related call, writing `lit`
and `litError`; `apply` is `lit = lit?.replacingIncident(incident)` then
`loadPod`. Every error path goes through a `report` that ignores
`.cancelled`, as every store does.

No test: the stores are client glue the model tests cannot see, as the
project's other stores are; the checkpoint's `make app` is the gate.

Checkpoint as task 1. Commit: `app: add the pod page store and the
shared incident writes`.

Consumes: `IdiosModel.PodDetail` (with `siblings`, `siblingTotal`),
`IncidentDetail`, `Page`, `TimelineEntry`, `PodHistory`,
`ArtifactContentStore`, `DaemonConnection`, `apiError`, `APIError`,
`Screenshot.noteFirstLoad`.
Produces: `IncidentWriteStore` (the writes returning `Incident?`,
`delete` returning `Bool`, `actionError`); `PodPageStore` with the
members above.

## Task 4 - the shared pieces, moved and widened

Moves, verbatim, with their doc comments: `DetailCard` and `FactRow`
from `IncidentDetailScreen.swift` to `Views/Components/DetailCard.swift`;
`artifactOrder`, `defaultArtifact`, `artifactLabel` (both overloads) to
`Views/Components/ArtifactOrder.swift`; `RailHeading`, `RailRow`,
`DeletedBanner`, `utcMinute` from `IncidentRail.swift` to
`Views/Components/Rail.swift`. `DetailTab` stays in the screen (task 5
deletes both).

`KubeletCard` takes what the page has rather than an incident detail,
and absorbs the one fact `ResourceEnvelopeCard` added:

    /// KubeletCard is what the kubelet reported about one container, in the
    /// kubelet's own words, and what the incident recorded at open when one
    /// is lit.
    struct KubeletCard: View {
        let container: Container
        let phase: String
        let qosClass: String?
        let incident: Incident?
        let job: Job?

(`phase` and `qosClass` rather than a pod row, because the page has a
`PodRow` and the old screen a `Pod`, and the card needs two facts of
either.) The rows: Current state, Restart count, Last terminated, Last
finished at (as today); First reason, Last reason, Image at open, Image
id at open only `if let incident`; Ready, Pod phase (`phase`), Container
kind, QoS class (`qosClass`); a new `cpu / memory` row reading
`resources(container.cpuRequest, container.cpuLimit)` and the memory
pair the way `PodRail.resources` wrote them ("request 100m - limit
500m"), the two joined by " - ", or "no requests written" when all four
are nil; Run outcome `if let job`; and, `if let incident`, one trailing
`WrapText` carrying `ResourceEnvelopeCard`'s sentence ("idios records
requests and limits, never usage. The limit is context, not the cause;
the reason is <lastReason>."). The `podLevel` branch and
`ResourceEnvelopeCard` are deleted: a pod-level incident's facts are the
header's sentence and the left column's state lines. The old
`IncidentDetailScreen` adapts its two call sites to
`KubeletCard(container: subject, phase: detail.pod?.phase ?? "unknown",
qosClass: detail.pod?.qosClass, incident: detail.incident, job:
detail.job)` guarded by `if let subject`; the page passes
`detail.pod.phase` and `detail.pod.qosClass`.

`EventsCard` gains a container column and a pod marker:

        /// showsContainer adds the column the Pod card's tab needs; a
        /// container's own pane leaves it off.
        var showsContainer = false
        /// marksPodScope tags a row with no field_path as the pod's own event,
        /// for a pane that is about one container.
        var marksPodScope = false

The column (width 96, monospaced, `eventContainer(fieldPath:
event.fieldPath) ?? ""`) sits after REASON; the marker is a `Badge(text:
"pod", style: .neutral)` after the reason when `marksPodScope` and
`eventContainer(...) == nil`. Headings follow.

`IncidentHeader` loses `identity` and `identityLine` (the page header
owns the identity line); `headerSentence` stays.

`PodContainers.swift`: `ContainersTable` and `readyMeaning` are deleted
(the left column replaces the table; `KubeletCard` has its own
`readyMeaning`). `CapturedFilesCard` becomes

    struct CapturedFilesCard: View {
        var title = "Captured files"
        let artifacts: [Artifact]
        /// containerGrafanaURL is the fallback link of a gap row; the Pod
        /// card's Files tab passes the map so each row finds its container's.
        let grafanaURL: (Artifact) -> String?
        var showsContainer = false

with a CONTAINER column (width 96) when `showsContainer`, the title in a
plain `DetailCard(title:)`, and `GrafanaLinkButton(urlString:
grafanaURL(artifact))`. `PodIncidentsCard` gains `var title = "Incidents
on this pod"` and `var meta = "incidents WHERE pod_uid = ?"`.
`PodScreen` adapts its two call sites (`CapturedFilesCard(artifacts:
files(detail), grafanaURL: { _ in container(detail)?.grafanaURL })`, and
the table's removal leaves `.containers` showing the files card alone
for the two commits it survives).

Checkpoint as task 1 (both old screens still build). Commit: `app: widen
the cards the pod page keeps`.

Consumes: `eventContainer` of task 2; the cards as they are.
Produces: `DetailCard`, `FactRow`, `artifactOrder`, `defaultArtifact`,
`artifactLabel`, `RailHeading`, `RailRow`, `DeletedBanner`, `utcMinute`
in `Views/Components/`; `KubeletCard(container:phase:qosClass:incident:job:)`;
`EventsCard(events:title:meta:emptyText:incidentID:showsContainer:marksPodScope:)`;
`CapturedFilesCard(title:artifacts:grafanaURL:showsContainer:)`;
`PodIncidentsCard(incidents:clusterName:open:title:meta:)`;
`IncidentHeader(detail:clusterName:now:)` without the identity line;
`headerSentence`.

## Task 5 - the page, the routing, the deletions, the spec

Spec first, `docs/design/presentation.md` section 9.3. The three rows
"Incident detail", "Incident timeline" and "Pod" become one row:

    | Pod page: one screen keyed by pod uid, reached from every route the old screens had (`incident/<id>` fetches the incident, then opens its pod with that container selected and that incident lit on Overview; `timeline/<id>` the same on Timeline; `pod/<uid>` opens the Pod card on Events; `pod/<uid>/<tab>` keeps every old tab name: `containers`, `incidents` and `events` open Events, `conditions` opens Conditions, `logs` opens Files, `podjson` opens pod.json), the back chevron returning to wherever it was opened from. Three columns inside the pushed screen: the pod-and-containers column at the sidebar's width (the application sidebar collapses while the page is up and Back or a Go menu item restores what the person had, because state and category filters are about the list), the selection in the middle, the rail on the right. The column is the Pod as a card, then the containers as cards indented under it (init, app, sidecar, ephemeral, by name inside a kind), each with its state line and one badge per incident on it in the fold order (open first, worst category, newest last seen), a closed one dimmed; the selected card fills with the accent; a pod-level incident's badge sits on the Pod card. `GetPod` is authoritative: the pod row, the containers, the latest conditions, the incidents that put the badges on the cards, the artifacts, the siblings. Selecting a container shows a segmented control with one segment per incident on it in the fold order (a job-subject incident the page was opened from joins its failing container's segments), then the lit incident's header (category and state tags, its sentence, its explanation, its note, its actions: Note..., Dismiss/Undismiss, Mark resolved while open, Acknowledge/Unacknowledge, Delete behind a confirmation, a closed one folding them into one menu joined by Unresolve exactly when the close was manual, and [Ask AI] for the lit incident), then Overview (the kubelet facts of the container from `GetPod`, the container's files, its events: the pod's events whose `field_path` names it plus every pod-level event tagged as the pod's), Timeline (the lit incident's window) and Logs; switching segments changes the header, the note and the Timeline, and nothing else; the previous incident stays until the next arrives. A container with no incident shows no control and no actions. Selecting the Pod card shows the pod-level incidents' control and header when it has any, then Events (every container, a container column, served order), Conditions (the latest per type, then the history), Files (every container's files together and `pod.json`), pod.json, and Related incidents only for a Job's pod (the `job_failed` row and the retries in other pods, by `job_uid`). The page header is the pod's and never moves: the state tag (DELETED, or the phase with READY / NOT READY from the Ready condition), the counts line (containers, incidents and how many open, node), the title `Pod <name>`, and the identity line cluster / namespace / the owner chain as it exists, stopping before the pod. A deleted pod shows the banner under the header and dims the live-only fields. The rail is the pod's: the owner chain with the container as a fourth dot when one is selected; context; the times of what is selected (the lit incident's opened, last seen, open for, closed and close reason, acknowledged, dismissed; or the pod's created, started, first and last seen, deletion requested, deleted); siblings: this pod first, then the five the daemon serves with a category badge where one is open and a state where none is, and a "+ N more of this <controller kind>" line that opens Workloads > that workload > Pods. The keys `a`, `d`, `r` act on the lit incident. The incident stream is one subscription for the life of the page, filtered on this pod's rows; any of them re-reads `GetPod`; the lit segment is a local comparison. | `/pods/{uid}`, `/pods/{uid}/events`, `/pods/{uid}/history`, `/incidents/{id}`, `/incidents/{id}/timeline`, `/incidents?job_uid=`, `/artifacts/{id}/content`, `/incidents/{id}/prompt`, the incident writes of Section 8 |

Section 4.8's "three places" sentence: "the incident detail header" becomes
"the lit incident's header on the pod page". Section 6's `GET
/v1/incidents/{id}` row keeps "the detail page" wording as the endpoint's
name for itself; leave it. Section 8's sentence from step B stands. In
`CLAUDE.md`, the screenshot sentence's route list gains, after the
parenthesis, "; `incident/<id>`, `timeline/<id>` and `pod/<uid>[/<tab>]`
all open the pod page, on the incident's container, its Timeline tab, or
the Pod card".

The screen, `Views/PodPage/PodPageScreen.swift`:

    /// PodPageOpening is what a route asked the page to show first.
    enum PodPageOpening: Hashable {
        case incident(id: String, tab: ContainerTab)
        case pod(uid: String, pane: PodPane)
    }

    /// PodPageScreen is one pod: its containers on the left, the selection in
    /// the middle, the rail on the right.
    struct PodPageScreen: View {
        let opening: PodPageOpening
        let clusters: [Cluster]
        let openPod: (String) -> Void
        let openWorkloadPods: (PodRow) -> Void
        let deleted: (String) -> Void

        @State private var store = PodPageStore()
        @State private var podUID: String?
        @State private var selection: PodSelection
        @State private var selectedArtifactID: String?
        @State private var wrap = false
        @State private var showingNoteSheet = false
        @State private var showingDeleteAlert = false
        @State private var askAI: AskAIRequest?

`init` sets `podUID` and `selection` for a pod opening (`.pod(pane)`)
and leaves `podUID` nil with `selection = .pod(.events)` for an incident
opening. `body`:

- `.task(id: opening)`: for `.incident(id, tab)`, `await store.resolve`;
  on success set `podUID` and `selection = .container(name:
  container, incidentID: id, tab: tab)` when the incident names a
  container, else `.pod(.events)` with the incident lit on the Pod card
  (`PodSelection.pod` carries no incident id: the Pod card's lit incident
  is `store.lit` when its row is pod-level, see below).
- `.task(id: PodKey(generation, podUID))`: `guard let podUID` then
  `await store.watch(uid:)`.
- `.task(id: LitKey(generation, litID))`: `guard let litID` then `await
  store.light(incidentID:)` unless `store.lit?.incident.id == litID`
  (the resolve already fetched it).
- `.task(id: TimelineKey(generation, litID, showingTimeline))`,
  `.task(id: HistoryKey(generation, podUID, showingConditions))`,
  `.task(id: RelatedKey(generation, jobUID, showingRelated))`, each
  guarded like the old screens' tasks.
- `.task(id: paneArtifact?.id)` fetching content as the old screens did.
- `.onChange(of: activeState)` reloading the pod and, if lit, the lit
  incident and the timeline when showing.
- `.onKeyPress(keys: ["a", "d", "r"])` acting on `litID` through
  `store.writes` then `store.apply`.
- `.navigationTitle(store.detail?.pod.name ?? "pod")`, `.toolbar {
  askAIMenu; copyUID }` (the actions of the lit incident live in the
  pane header, decision 3), the two sheets and the delete alert of the
  old detail screen, with `deleted(id)` popping the page.

`litID` is `selection`'s `incidentID` for a container, and for the Pod
card the first row of `segmentOrder(podLevelIncidents)` unless a
`@State private var podLit: String?` was chosen through the Pod card's
control; `podLevelIncidents` are `detail.incidents.filter {
$0.containerName == nil }`. `containerIncidents(name)` are
`segmentOrder(detail.incidents.filter { $0.containerName == name } +
borrowed)` where `borrowed` is `[store.lit!.incident]` when it is a
job-subject incident (`subjectKind == .job`) and `name` is the failing
container the resolve chose (`resolve` answers `container` for a job
incident the way `IncidentDetailScreen.subject` chose it: the container
whose exit code or last terminated exit code is not 0, else the app
container, else the first).

The body is `PodPageHeader`, then (when `detail.pod.deletedAt != nil`)
`DeletedBanner`, then an `HStack(spacing: 0)` of `PodColumn` (width 240,
`.background(.quaternary.opacity(0.25))`), a `Divider`, the middle
(`ContainerPane` or `PodPane` in a `ScrollView`), a `Divider`, and the
rail in a `ScrollView` of width 310. Loading, error and `missing` states
mirror the old screens' (`missing` says "pod <uid> is no longer stored;
the sweep removed it.").

`PodPageHeader(detail: PodDetail, clusterName: String)`: the tag
`Badge(text: podStateTag(phase:deleted:ready:), style:)` with style
`IncidentState.podDeleted.badge` when deleted, `ContainerState.running.badge`
when ready is true, `ContainerState.waiting.badge` when false,
`.neutral` when nil (ready is the `Ready` condition's status, nil when
no such condition); the counts line "<n> containers - <m> incidents, <k>
open - node <name>" ("was on node" when deleted, "not scheduled" when
nil) in the tertiary monospaced style of the old header's id line; the
title `Text("Pod ") + mono(name)` at 20pt (see `headerSentence`'s
`mono`; make `mono` internal or repeat its one line); the identity line
"<cluster> / <namespace>[ / <workloadKind> <workloadName>][ / <controllerKind>
<controllerName>]" for a workload kind other than `"none"` and a
controller that exists, monospaced 12pt secondary, `lineLimit(nil)`.

`PodColumn(detail: PodDetail, eventCount: Int?, selection:
Binding<PodSelection>, podLevel: [Incident])`: the Pod card (title
"Pod", the name in the tertiary caps style, the sub line "events <n> -
conditions - files <f> - pod.json" with `n` from `eventCount` or "events"
alone while nil and `f` the count of artifacts with a `filePath`, the
pod-level incidents' badges), then `containerOrder(detail.containers)`
as cards indented 16pt with a 1pt connector drawn as a `Rectangle` on
the left, each with `Text(name)` monospaced semibold, the kind in caps
tertiary, `containerStateLine(container)` in 11pt monospaced secondary,
and the badges: for each incident in `segmentOrder(incidents of that
container)` a `Badge(text: "<category label> <occurrences>", style:
category.badge)` with `.opacity(0.7)` when closed. A tap selects
(`.pod(current pane or .events)` / `.container(name:, incidentID: first
segment's id or nil, tab: .overview)`); the selected card takes
`Color.accentColor` as background with white text (the badges keep their
own colours over a white-tinted capsule as the mockup draws them: use
`.colorMultiply` if it reads well, else leave the badge as is; say which
in the report). Every card is a `Button` with `.buttonStyle(.plain)` and
`.help(name)`.

`ContainerPane(detail:, container:, incidents: [Incident], lit:
IncidentDetail?, tab:, store:, ... callbacks)`: when `incidents` is not
empty, a `Picker` with `.segmented` style whose segments are
`incidents` labelled "<category label> - incident <id>" bound to the
selection's `incidentID`; beside it the actions (open: Note...,
Dismiss/Undismiss, Mark resolved, Acknowledge/Unacknowledge, Delete
incident tinted red; closed: one `Menu("Actions")` as the old screen's
`closedActions`), all acting on `lit.incident.id` through
`store.writes` and `store.apply`. Then `IncidentHeader(detail: lit,
clusterName:, now:)` when `lit?.incident.id == litID` (the old one
stays visible until the new arrives), `store.writes.actionError` in
red under it as today, then the tab strip (Overview, Timeline, "Logs N
files") using the old screen's `tabButton`, then:

- Overview: `JobCard` when `lit?.job != nil` and the lit row is a
  job-subject incident; `KubeletCard(container:phase:qosClass:incident:
  lit?.incident, job: lit?.job)`; `CapturedLogsCard` over the container's
  artifacts (kind != podJSON, `artifactOrder`) with `emptyText: "idios
  captured no log for this container."`; `EventsCard(events:
  containerEvents(store.events ?? [], container: name), title: "Events of
  container <name>", meta: "k8s_events WHERE involved_uid = pod AND
  (field_path names the container OR field_path IS NULL)", emptyText:
  "idios kept no event for this container.", incidentID: litID,
  marksPodScope: true)`.
- Timeline: `TimelineView(entries: store.timeline, truncated:,
  isLoading:, error:)`, or the text "Select an incident to see its
  timeline." when nothing is lit.
- Logs: `CapturedLogsCard` as Overview's.

`PodPane(detail:, pane:, store:, podLevel: [Incident], lit:, ...)`: the
pod-level control and header as `ContainerPane` draws them when
`podLevel` is not empty; then the tab strip (Events <n>, Conditions,
Files <f>, pod.json, Related incidents only when
`detail.pod.controllerKind == "Job"`); then:

- Events: `EventsCard(events: store.events ?? [], title: "Events on this
  pod", meta: "k8s_events WHERE involved_uid = pod", emptyText: "idios
  kept no event for this pod.", showsContainer: true)`, and the
  truncation line the old pod screen drew if it drew one.
- Conditions: a `DetailCard(title: "Conditions (latest per type)", meta:
  "pod_condition_history, latest per type")` of `FactRow(label:
  condition.type)` with the status and reason coloured as `PodRail`
  did, then `PodHistoryCards(history: store.history)`.
- Files: `CapturedFilesCard(title: "Captured files", artifacts:
  detail.artifacts.sorted(by: artifactOrder), grafanaURL: { artifact in
  detail.containers.first { $0.name == artifact.containerName }?.grafanaURL
  }, showsContainer: true)`.
- pod.json: the old pod screen's `podJSON(detail)`.
- Related incidents: `PodIncidentsCard(incidents: store.related,
  clusterName:, open: openIncident, title: "Related incidents", meta:
  "incidents WHERE job_uid = ?")` where `openIncident` pushes
  `.incident(id)` through a new `openIncident: (String) -> Void`
  parameter of the screen.

`PodPageRail(detail:, cluster:, container: Container?, lit:
IncidentDetail?, now:, openPod:, openWorkloadPods:)`: OWNER CHAIN as
`IncidentRail.ownerChain` built from `detail.pod` (workload from
`pod.workloadKind`/`pod.workloadName`, controller from
`pod.controllerKind`/`Name`/`UID`, the pod with its phase and
`deleted_at`, the container as a fourth red dot when selected, with
`IncidentRail.containerNote`); CONTEXT as `IncidentRail.context` from
the pod row (Cluster, Context, API server, Namespace, Node, QoS class,
and Image tag and Container id of the selected container when there is
one); TIMES: when `lit` is set for the selection, the lit incident's
rows of `IncidentRail.times` (opened, last seen, open for, closed and
close reason, acknowledged, dismissed) under the heading "TIMES incident
<id>", else the pod's (created, started, first seen, last seen, deletion
requested, deleted) under "TIMES pod" as `PodRail.identity` stamped
them; SIBLINGS: heading "SIBLINGS <total> pods of this <controllerKind>"
(or "SIBLINGS none" for a pod with no controller), one row per
`siblingRows(pod:ready:siblings:)`: a 7pt dot (red when `category` is
set, grey when deleted, green when ready, orange otherwise), the name's
suffix after "<workloadName>-" through `podNameSuffix`, and on the
right "this pod" for the page's pod, `CategoryBadge` for a category,
"deleted" or "ready" or "not ready" otherwise; a row that is not this
pod is a `Button` calling `openPod(uid)`; then, when
`moreSiblings(total:shown:) > 0`, a `Button("+ N more")` styled as a
link with the help "opens Workloads > <workloadName> > Pods" calling
`openWorkloadPods(detail.pod)`. The rail imports nothing the old rails
did not.

`IncidentsScreen`:

- `@State private var columnVisibility: NavigationSplitViewVisibility =
  .all` and `@State private var visibilityBeforePage:
  NavigationSplitViewVisibility?`; `NavigationSplitView(columnVisibility:
  $columnVisibility)`; `.onChange(of: path)`: when the new path holds a
  page route (`.incident`, `.timeline`, `.pod`) and the old one did not,
  `visibilityBeforePage = columnVisibility; columnVisibility =
  .detailOnly`; when the old path held one and the new does not,
  `columnVisibility = visibilityBeforePage ?? .all;
  visibilityBeforePage = nil`. A sidebar the person had collapsed is
  `.detailOnly` before and after, so it stays collapsed. `init` for a
  launch route that is a page route sets `columnVisibility` to
  `.detailOnly` and `visibilityBeforePage` to `.all`.
- `destination`: `.incident(id)` -> `PodPageScreen(opening:
  .incident(id: id, tab: .overview), ...)`; `.timeline(id)` -> the same
  with `.timeline`; `.pod(uid, tab)` -> `PodPageScreen(opening: .pod(uid:
  uid, pane: tab.pane), ...)`; the callbacks: `openPod: { path.append(.pod($0,
  .containers)) }`, `openIncident: openIncident`, `openWorkloadPods:`
  below, `deleted: deleteIncident`.
- `openWorkloadPods(_ pod: PodRow)`: `screen = .workloads;
  workloadsTree.selected = nil; workloadsTree.tab = .pods; workloadRoute =
  .workload(cluster: clusters.cluster(id: pod.clusterID)?.name ??
  pod.clusterID, namespace: pod.namespace, kind: pod.workloadKind, name:
  pod.workloadName); path = []`.
- `deletes` stays the `IncidentWriteStore` of task 3.

`WorkloadsScreen`: `@State private var tab: WorkloadTab?` becomes
`WorkloadsTreeState.tab` (`var tab: WorkloadTab?` on the struct, bound
as `$tree.tab` where `$tab` was); its `.onChange(of: selected)` resets
`tree.tab` (and the two filters) only when the old value was not nil:
a first adoption from a route keeps the tab the opener asked for, and a
person moving between workloads still drops the reading position. If
`WorkloadsTreeState` is built anywhere with a memberwise call, the new
property has a default and needs no change.

Deletions: `IncidentDetailScreen.swift` (with `DetailTab`),
`PodScreen.swift` (the `PodTab.title` extension goes with it),
`PodFactsView.swift`, `RelatedIncidentsCard.swift`, `IncidentRail.swift`,
`PodRail.swift`, `IncidentDetailStore.swift`, `PodStore.swift`,
`TimelineStore.swift`. Grep the application for every deleted name
(`IncidentDetailScreen`, `PodScreen`, `DetailTab`, `PodFactsView`,
`RelatedIncidentsCard`, `IncidentRail`, `PodRail`, `IncidentDetailStore`,
`PodStore`, `TimelineStore`, `ContainersTable`, `ResourceEnvelopeCard`)
and fix every hit; `IncidentHeader.swift` stays for the pane.

Checkpoint: `go build ./... && go test ./... && make ascii && make
app-test && make app`. Commit: `app: replace the incident and pod
screens with the pod page`, body: "The incident detail and the pod
screen were two screens over the same rows, and the timeline a third;
one page keyed by pod shows the pod once, each container once, and the
incidents behind a control whose purpose is switching."

Consumes: everything tasks 1 to 4 produce; `Badge`, `CategoryBadge`,
`WrapText`, `MiddleElidedText`, `GrafanaLinkButton`, `podNameSuffix`,
`durationText`, `explanation(for:events:pod:)`, `AskAISheet`,
`NoteSheet`, `TimelineView`, `JobCard`, `PodHistoryCards`.
Produces: `PodPageScreen(opening:clusters:openPod:openIncident:
openWorkloadPods:deleted:)`, `PodPageOpening`, `WorkloadsTreeState.tab`.

## Verification with the user

Screenshots need Screen Recording permission this session lacks. After
task 5's commit, the user runs, against the dev daemon on 7771 serving
`.storage/smoke` (`IDIOS_DAEMON=127.0.0.1:7771`), one screenshot per
frame of the mockup and checks them against pages 2 to 4:

    hack/macos/screenshot.sh incident/<crash id> /tmp/p2a.png
    hack/macos/screenshot.sh pod/<crash pod uid> /tmp/p2b.png
    hack/macos/screenshot.sh timeline/<crash id> /tmp/p3a.png
    hack/macos/screenshot.sh pod/<healthy pod uid>/events /tmp/p4a.png
    hack/macos/screenshot.sh incident/<pod_deleted id> /tmp/p4b.png

with ids and uids read from `curl -s 127.0.0.1:7771/v1/incidents`. The
README's `docs/screenshots/incident-detail.png` and `pod.png` show the
old screens; retaking them is the user's call and not part of this plan.

## Hands to the next step

m9 is the last planned milestone; what outlives it moves to `CLAUDE.md`
(the route list) and the presentation doc (the Pod page row). A later
milestone that adds a route adds a case to `IdiosModel.Route`, a row to
`RouteTests`, and a line to the screenshot list.

## Self-review

Spec coverage: decision 1 lands in task 1 (the routes as pure mapping),
task 2 (`PodTab.pane`) and task 5 (the destinations, the back chevron);
decision 2 in task 5 (the column, the sidebar collapse and restore,
`containerOrder` and the badges from task 2); decision 3 in task 3 (the
store's ownership of each call, the lit detail kept) and task 5 (the
panes, `containerEvents` and `segmentOrder` from task 2); decisions 4
and 5 in task 5 (`PodPageHeader`, `PodPageRail`, `podStateTag`,
`siblingRows`, `moreSiblings` from task 2); decision 7 in task 5
(`RelatedIncidentsCard` deleted, the Job tab, the one stream filtered
on the pod, the lit segment a local comparison); decision 8 in task 5
(no control and no actions for a container without incidents, the Pod
card selected on arrival for `pod/<uid>`, the job-subject incident
borrowed onto its failing container); decision 9 in task 1; decision 10
holds. The spec statements land in task 5, the task whose views they
describe; tasks 1 and 2's tests trace to the row task 5 writes, the plan
being read whole first.

Rulings this plan makes inside the decisions: a pod-level incident
(`container_name` empty: scheduling, node pressure, stuck) lights on the
Pod card, whose pane gets the same control and header a container's
does, because the decisions give every incident a segment and the pod is
the only subject such a row has; one badge per incident rather than per
category, because open incidents are unique per (pod, container,
category) and a closed duplicate is the case the dimming exists for;
`ResourceEnvelopeCard` folds into the kubelet card as one row, as the
mockup draws it, because the old card had one user and it is deleted;
the incident writes move to `IncidentWriteStore` so the list's delete
and the page share a client, rather than the page store carrying the
list's delete; `WorkloadsTreeState` carries the workload tab so the
"+ N more" line can open Pods, and the reset on a selection change
spares a first adoption; `foldOrder` is total (last seen, then id) so
the segment order never depends on the sort's stability; the rail's
"this pod" row is the page's own pod and the "+ N more" count comes off
the total including it, as step B's handoff says; a swept pod behind an
incident route shows the page's `missing` state, because the page keys
on the pod and the old inline Pod tab said the same sentence; the
README's two screenshots are left to the user.

`.ai` rules: ascii (checked per task); tests trace to the Pod page row
(tasks 1, 2) and are tables with whole-value assertions; no test of the
language (no test that a `PodSelection` holds what was put in it, no
test of `sorted`); comments carry the reason for the pod-level events,
the borrowed job incident, the total order, the lit detail kept, the
sidebar restore, the tab reset guard; code-is-truth: no doc named from
code, the presentation doc changes in task 5, `CLAUDE.md`'s route list
says what the routes now do; scope: no proto change, no new endpoint,
no new route name, no README retake, no change to the list's rows;
commits per task, none red, none without the user's review.

Type consistency: `PodSelection.container(name:incidentID:tab:)` carries
`String`, `String?`, `ContainerTab` in tasks 2 and 5; `PodTab.pane` is
`PodPane` in tasks 2 and 5 and `PodPageOpening.pod(uid:pane:)` takes it;
`siblingRows(pod: PodRow, ready: Bool?, siblings: [SiblingPod])` returns
`[SiblingRow]` in tasks 2 and 5; `moreSiblings(total: Int32, shown: Int)`
takes `detail.siblingTotal` (`Int32`) and `detail.siblings.count`;
`IncidentWriteStore`'s writes return `Incident?` and `PodPageStore.apply`
takes `Incident` in tasks 3 and 5; `KubeletCard(container:phase:qosClass:
incident:job:)` in tasks 4 and 5; `EventsCard.showsContainer` and
`marksPodScope` are `Bool` defaults in tasks 4 and 5;
`CapturedFilesCard.grafanaURL` is `(Artifact) -> String?` in tasks 4 and
5; `openWorkloadPods` takes `PodRow` in task 5's screen, rail and
`IncidentsScreen`.
