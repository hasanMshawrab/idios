# Phase 9: macOS application, read-only

**Goal:** a person opens the application, points it at a running daemon
(default `127.0.0.1:7770`) and reads every screen of `presentation.md`
Section 9.3 with nothing lost between the database and the glass: the
identity chain, the four text treatments, the states idios does not know
said out loud, and the human-action controls drawn disabled with the
reason.

**Architecture:** `macos/` holds one Swift package and one Xcode project.
The package (`Package.swift` at `macos/`) has two library targets:
`IdiosAPI`, which is nothing but the `swift-openapi-generator` build plugin
run over `api/openapi/IdiosService.openapi.yaml`, and `IdiosModel`, the
model layer (one struct per screen concept, each with one initializer from
its wire type) plus the pure display derivations. `IdiosModelTests` decodes
`api/testdata/*.json` through the generated types into the models and
asserts exact values. The Xcode project `idios.xcodeproj` builds the
application target `idios` from the folder `macos/idios/` (`App/`,
`Store/`, `Views/`) and depends on the local package. Stores own the client
calls, the artifact content fetches and the two SSE subscriptions and
expose plain state; views read stores and never import `IdiosAPI`.

**Tech stack:** Xcode 26.2 (SDK macOS 26.2), Swift 6.2 in Swift 6 language
mode, deployment target macOS 15.0, SwiftUI with AppKit where the spec
names it (`NSTextView`), `swift-openapi-generator` 1.13.0 as a build
plugin, `swift-openapi-runtime` 1.12.0, `swift-openapi-urlsession` 1.3.1,
`swift-testing` (ships with the toolchain) for the model tests. No other
dependency.

**Spec:** `presentation.md` Sections 3.1 and 3.2 (what the wire carries
and what the app tolerates), 4 and 5 (which endpoint feeds which screen),
7 (scope, identity, text), 9 (the application), 11 item 5 (model tests)
and item 6 (screens checked by a person against the mockup), 13 (the
preferences). The mockup `docs/mockups/idios-ui.html` decides where the
spec does not.

**Global constraints:** every `.ai/*.md` rule, applied to Swift exactly as
to Go (roadmap decision 7). Roadmap decisions 5 (the application never
opens the database, artifact files or the kubeconfig; nothing imports
SQLite or reads `~/.kube`), 6 (macOS 15 minimum, current SDK, Swift 6
mode, not sandboxed, signed to run locally, builds with `xcodebuild` from
the command line), 8 (fixtures cross the boundary once: the Swift tests
read `api/testdata/*.json`, nobody hand-writes JSON). Vocabulary: endpoint,
client operation, never RPC. Every commit is one green task per
`.ai/commits.md`; the checkpoint for this phase is `make app-test && make
app && make ascii` (the Go checkpoint `go build ./... && go test ./...`
runs too whenever a Go or Makefile file changed).

Facts the tasks depend on that are not in the design docs:

- The generated Swift enums are closed (`@frozen enum: String`) and nested
  inside each message type (`Components.Schemas.IncidentRow.categoryPayload`),
  so the generator's `typeOverrides` cannot replace them and a value the
  app does not know fails decoding of the whole response. Section 3.2's
  "render the raw string with the `other` colour" is therefore not
  achievable with the generated types today; Task 11 writes that
  limitation into the design doc. The model layer still maps every wire
  enum to its own enum so views never see `Components.Schemas`.
- Non-optional zero scalars are absent from the JSON: missing
  `containerCount` is 0, missing `truncated`, `forbidden`, `complete`,
  `ready`, `daemonRunning`, `gapReconstructed`, `reconstructed`,
  `capturedEarly` are false, a missing repeated field is empty. The model
  initializer applies those defaults; nothing downstream sees an optional
  for them.
- `int64` fields arrive as `String`. Identifiers stay `String` in the
  model. Byte counts (`sizeBytes`, `dbBytes`, `walBytes`, `artifactBytes`,
  `bytesRemoved`, `*Millis`, `*Bytes` on containers) are parsed to `Int64`
  for display arithmetic only.
- Timestamps are the stored strings. Kubernetes timestamps are stored
  verbatim and have no fraction (`2026-08-27T14:38:00Z`); process
  timestamps have six fractional digits and a `Z`. The model parses both
  shapes for durations and keeps the raw string for display and copy.
- The two stream operations return `OpenAPIRuntime.HTTPBody`;
  `asDecodedServerSentEventsWithJSONData(of:)` from `OpenAPIRuntime` turns
  it into an `AsyncSequence` of decoded rows. The response head is
  flushed on connect and the first `data:` line arrives only when a row
  changes; a silent open stream is a quiet cluster, not a failure.
- `GET /v1/artifacts/{id}/content` is outside the OpenAPI document; the
  store fetches it with `URLSession` against the same base URL. A 404 body
  is sebuf's `Error` JSON; the typed `captureGap` and `captureNote` come
  from the `Artifact` row already held.
- Filter query parameters are strings in the stored vocabulary
  (`state=open`, `category=crash`, `live=true`). `limit` omitted means the
  daemon's `api_list_limit`.
- `xcodebuild` refuses to run a package build plugin non-interactively
  unless `-skipPackagePluginValidation` is passed. `swift build` and
  `swift test` need no flag.
- Xcode 26 project files support a synchronized root group
  (`PBXFileSystemSynchronizedRootGroup`, `objectVersion = 77`): every file
  under `macos/idios/` is a member of the target without being listed in
  the project file. The project file is hand-written and ASCII.
- Screenshots for review come from the application itself: launched with
  `-screenshot <png path> [-route <route>]` it opens the route, waits for
  layout, writes the window contents with `NSView.cacheDisplay` and quits.
  This needs no screen-recording permission. Real data for screenshots is
  `.storage/smoke` (populated by `make smoke`), served with
  `./bin/idios -data-dir .storage/smoke -kubeconfig ./kube/config run`;
  fixture data is `./bin/idios -data-dir .storage mock`. Both listen on
  `127.0.0.1:7770`; never run both at once.

## File structure

```
Makefile                                   app, app-test targets
.gitignore                                 macos/.build/, macos/DerivedData/, xcuserdata
macos/Package.swift                        IdiosAPI, IdiosModel, IdiosModelTests
macos/Package.resolved                     committed
macos/Sources/IdiosAPI/openapi.yaml        symlink -> ../../../api/openapi/IdiosService.openapi.yaml
macos/Sources/IdiosAPI/openapi-generator-config.yaml
macos/Sources/IdiosAPI/IdiosAPI.swift      module doc only (the plugin writes the rest)
macos/Sources/IdiosModel/Enums.swift       Category, IncidentState, CloseReason, CaptureGap, ...
macos/Sources/IdiosModel/Timestamp.swift   Timestamp (raw string, parsed date), Duration formatting
macos/Sources/IdiosModel/Incident.swift    Incident, IncidentCounts, IncidentDetail, SiblingPod
macos/Sources/IdiosModel/Pod.swift         Pod, PodRow, Container, PodCondition, ContainerTransition
macos/Sources/IdiosModel/Artifact.swift    Artifact
macos/Sources/IdiosModel/Event.swift       Event
macos/Sources/IdiosModel/Timeline.swift    TimelineEntry
macos/Sources/IdiosModel/Workload.swift    Workload, WorkloadDetail, Rollout, HourBucket, Job
macos/Sources/IdiosModel/Cluster.swift     Cluster, ClusterScope
macos/Sources/IdiosModel/Status.swift      DaemonStatus and its parts
macos/Sources/IdiosModel/Display.swift     podNameSuffix, middleElided, scopePrefix, byteCount
macos/Sources/IdiosModel/APIError.swift    APIError (badRequest, notFound, unreachable, ...)
macos/Tests/IdiosModelTests/Fixtures.swift fixture loader (api/testdata via #filePath)
macos/Tests/IdiosModelTests/*Tests.swift   one file per model source file
macos/idios.xcodeproj/project.pbxproj      hand-written, synchronized folder idios/
macos/idios.xcodeproj/xcshareddata/xcschemes/idios.xcscheme
macos/idios/App/IdiosApp.swift             scenes: main window, menu bar extra, settings
macos/idios/App/Connection.swift           DaemonConnection: base URL, client, reachability
macos/idios/App/Preferences.swift          daemon address, cluster scope, column widths
macos/idios/App/Route.swift                Route enum, -route parsing
macos/idios/App/Screenshot.swift           -screenshot launch argument
macos/idios/Store/*.swift                  one @Observable store per screen
macos/idios/Views/Components/*.swift       badges, text treatments, log pane, not-connected
macos/idios/Views/Incidents/*.swift        home screen
macos/idios/Views/IncidentDetail/*.swift   detail, logs, events, timeline tab
macos/idios/Views/Pod/*.swift
macos/idios/Views/Workloads/*.swift
macos/idios/Views/Status/*.swift
macos/idios/Views/MenuBar/*.swift
macos/idios/Views/Browser/*.swift          column browser
macos/idios/Assets.xcassets                app icon placeholder, accent colour unset
hack/macos/screenshot.sh                   build, run daemon check, capture one route
docs/design/presentation.md                Sections 3.2, 9.2 updated (Task 11)
CLAUDE.md                                  Swift conventions and commands (Task 11)
```

Every task below ends with the checkpoint and one commit. A screen task
is done when its screenshot has been reviewed against the mockup and the
spec by the reviewer, not when it compiles.

## Task 1: project skeleton

Section 9.1 (shape), 9.2 (layers), roadmap decision 6.

1. `macos/Package.swift`, tools version 6.0, platform `.macOS(.v15)`.
   Targets: `IdiosAPI` (dependencies `OpenAPIRuntime`, `OpenAPIURLSession`;
   plugin `OpenAPIGenerator`), `IdiosModel` (depends on `IdiosAPI`),
   `IdiosModelTests` (depends on `IdiosModel`). Products: the two
   libraries. `Sources/IdiosAPI/openapi.yaml` is a relative symlink to the
   committed document; `openapi-generator-config.yaml` generates `types`
   and `client` with `accessModifier: public`. `IdiosModel` holds one
   placeholder type this task, `IdiosModelTests` one test that decodes
   `api/testdata/error.json` into `Components.Schemas.Error` and asserts
   `message == "incident 999999 not found"`; it proves the plugin ran and
   the fixture path resolves (trace: Section 11 item 5, "decoding tests
   against api/testdata"). Task 2 replaces both.
2. `macos/idios.xcodeproj/project.pbxproj`, `objectVersion = 77`, one
   application target `idios`, one synchronized root group for `idios/`,
   a local package reference to `.` with product dependencies `IdiosModel`
   and `IdiosAPI`. Build settings: `MACOSX_DEPLOYMENT_TARGET = 15.0`,
   `SWIFT_VERSION = 6.0`, `SWIFT_STRICT_CONCURRENCY = complete`,
   `GENERATE_INFOPLIST_FILE = YES`, `PRODUCT_BUNDLE_IDENTIFIER =
   dev.idios.app`, `CODE_SIGN_STYLE = Manual`, `CODE_SIGN_IDENTITY = "-"`
   (ad hoc, "Sign to Run Locally"), no entitlements file, no
   `ENABLE_APP_SANDBOX`, `ENABLE_HARDENED_RUNTIME = NO`. A shared scheme
   `idios` so `-scheme idios` works. If the synchronized group form does
   not parse under `xcodebuild -list`, fall back to an explicit
   `PBXGroup` and say so in the report.
3. `macos/idios/App/IdiosApp.swift`: a `WindowGroup` showing a placeholder
   `Text("idios")`. `App/Screenshot.swift`: when the arguments contain
   `-screenshot <path>`, after the first window appears wait 1.5 s, render
   `window.contentView` through `bitmapImageRepForCachingDisplay` and
   `cacheDisplay(in:to:)`, write PNG, `NSApp.terminate`. `-route` is
   parsed in Task 3; this task ignores it.
4. Makefile: `app` runs `xcodebuild -project macos/idios.xcodeproj -scheme
   idios -configuration Debug -derivedDataPath macos/DerivedData
   -skipPackagePluginValidation build`; `app-test` runs `swift test
   --package-path macos`. `.gitignore` gains `macos/.build/`,
   `macos/DerivedData/`, `xcuserdata/`.
5. `hack/macos/screenshot.sh <route> <png>`: `make app`, then runs the
   built binary with `-screenshot` and `-route`; exits non-zero when
   nothing answers on `127.0.0.1:7770` first (a screenshot of a
   not-connected screen is a valid run only when asked for with
   `--allow-disconnected`).
6. Checkpoint: `make app-test && make app && make ascii`. Confirm
   `codesign -dv` on the built app shows `Signature=adhoc` and the built
   `Info.plist` has `LSMinimumSystemVersion 15.0`.
7. Commit: `macos: add xcode project and package skeleton`.

**Consumes:** `api/openapi/IdiosService.openapi.yaml`, `api/testdata/error.json`.
**Produces:** modules `IdiosAPI` (generated `Client`, `Components.Schemas.*`),
`IdiosModel`, the `idios` app target, `make app`, `make app-test`,
`hack/macos/screenshot.sh`, the `-screenshot` argument.

## Task 2: model layer

Section 9.2 (one initializer per model from its wire type; int64 strings
and enums converted; display derivations live here), 7.3 (pod name
suffix), 7.4 (the four treatments' inputs), 7.1 (scope rules), 3.1
(timestamps as stored strings), 11 item 5.

Model types, all `Sendable`, `Hashable`, `Identifiable` where the screen
selects them, each with `init(wire:)` and nothing else public that is not
a stored property or a pure derivation:

```swift
public enum Category: String, Sendable { case oom, crash, imagePull = "image_pull", config, probe, scheduling, nodePressure = "node_pressure", rescheduled, jobFailed = "job_failed", other }
public enum IncidentState: String, Sendable { case open, acknowledged, recovered, podDeleted = "pod_deleted", jobFinished = "job_finished", manual, dismissed }
public enum CloseReason: String, Sendable { case recovered, podDeleted = "pod_deleted", jobFinished = "job_finished", manual }
public enum CaptureGap: String, Sendable { case podDeleted = "pod_deleted", noPreviousRun = "no_previous_run", forbidden, noOutput = "no_output", kubeletError = "kubelet_error", unknown, unobservable }
public enum ArtifactKind: String, Sendable { case logPrevious = "log_previous", logCurrent = "log_current", podJSON = "pod_json" }
public enum ContainerKind: String, Sendable { case `init`, sidecar, app, ephemeral }
public enum ContainerState: String, Sendable { case waiting, running, terminated }
public enum DeletionSource: String, Sendable { case watch, reconcile, unwatched }
public enum DeletionReason: String, Sendable { case rollout, replaced, scaledDown = "scaled_down", jobPruned = "job_pruned", unknown }
public enum SubjectKind: String, Sendable { case pod, job }
public enum TimelineKind: String, Sendable { case containerTransition = "container_transition", condition, event, capture, rollout, lifecycle, cut }
public enum LifecycleStep: String, Sendable { case opened, closed }

public struct Timestamp: Hashable, Sendable {
    public let raw: String
    public let date: Date?
    public init(_ raw: String)
}

public struct Incident: Identifiable, Hashable, Sendable {
    public let id: String
    public let clusterID: String
    public let namespace: String
    public let subjectKind: SubjectKind
    public let podUID: String?
    public let jobUID: String?
    public let containerName: String?
    public let workloadKind: String
    public let workloadName: String
    public let category: Category
    public let firstReason: String
    public let lastReason: String
    public let lastMessage: String?
    public let image: String?
    public let imageTag: String?
    public let imageID: String?
    public let occurrences: Int32
    public let openedAt: Timestamp
    public let lastSeenAt: Timestamp
    public let closedAt: Timestamp?
    public let closeReason: CloseReason?
    public let acknowledgedAt: Timestamp?
    public let dismissedAt: Timestamp?
    public let note: String?
    public let state: IncidentState
    public let podName: String?
    public let podDeletedAt: Timestamp?
    public let podDeletionReason: DeletionReason?
    public let containerCount: Int32
    public let exitCode: Int32?
    public let signal: Int32?
    public init(wire: Components.Schemas.IncidentRow) throws
}
```

The initializer throws `ModelError.missing(field:)` when a field the
schema marks non-nullable is absent (`id`, `clusterId`, `namespace`,
`category`, `state`, `openedAt`, `lastSeenAt`, `firstReason`,
`lastReason`, `occurrences`, `workloadKind`, `workloadName`,
`subjectKind`); the store surfaces that as a decoding failure with the
field name rather than showing a blank. Absent non-optional zero scalars
take their zero (`containerCount`, `truncated`, and the booleans listed in
the facts above). The same pattern for every other type: `IncidentCounts`
(`byState: [IncidentState: Int32]`, `byCategory: [Category: Int32]`,
`byCluster: [String: Int32]`), `IncidentDetail`, `Pod`, `PodRow`,
`Container`, `SiblingPod`, `Artifact`, `PodCondition`,
`ContainerTransition`, `Event`, `TimelineEntry`, `Workload`,
`WorkloadDetail`, `Rollout`, `HourBucket`, `Job`, `Cluster`,
`DaemonStatus` (with `StatusCluster`, `WriterStats`, `HandlerStats`,
`CaptureStats`, `CloserStats`, `SweepRun`, `RowCounts`), `KubeContext`.
List wrappers become `Page<T>` (`rows: [T]`, `truncated: Bool`).

Display derivations, pure functions in `Display.swift`:

```swift
public func podNameSuffix(name: String, workloadName: String) -> String
public func middleElided(_ value: String, keeping: Int) -> String   // head, "...", tail; never a trailing ellipsis
public func scopePrefix(clusterName: String, namespace: String, selectedClusterCount: Int) -> String
public func byteCount(_ n: Int64) -> String
public func durationText(from: Date, to: Date) -> String             // "38m", "1h 29m", "3d 2h"
```

`ClusterScope` in `Cluster.swift`:

```swift
public struct ClusterScope: Hashable, Sendable {
    public var selected: Set<String>          // cluster row ids; empty means all
    public func reconciled(with clusters: [Cluster]) -> ClusterScope
    public func includes(_ clusterID: String) -> Bool
    public var isAll: Bool
}
```

Tests, `swift-testing`, one file per source file, fixtures read from
`api/testdata` through a path derived from `#filePath`:

- `IncidentTests`: a table of `(fixture, want)` with `incident_row.json`
  (every field set) and `incident_row_minimal.json` (every optional
  absent, `containerCount` 0, no `podName`) decoded through
  `Components.Schemas.IncidentRow` into `Incident`, whole-struct equality
  (trace: Section 9.2 "unwraps what is guaranteed, keeps optional what is
  nullable"; handoff fact "non-optional zero scalars are absent").
  `incidents.json` into `Page<Incident>` with `truncated == true` and both
  rows (Section 3.1 "one repeated field per list response").
  `incident_counts.json` into `IncidentCounts` (Section 4.2).
  `incident_detail.json` into `IncidentDetail` (Section 4.2: incident,
  pod, containers, siblings, artifacts, events, conditions).
- `PodTests`: `pods.json` (a full row and a row with no deletion, no
  counts, no `worstState`), `pod_detail.json`, `history.json`
  (transition with `gapReconstructed`, condition) (Section 4.3).
- `ArtifactTests`: `artifact.json` with `captureGap`, `captureNote`,
  `sizeBytes` parsed to 262144, `truncated` true (Section 4.4, 7.6).
- `EventTests`: `events.json` (Section 4.3).
- `TimelineTests`: `timeline.json`: the first entry carries every optional
  field, the second is a bare `lifecycle opened` with only `observedAt`
  (Section 4.2 timeline: "the observed time always").
- `WorkloadTests`: `workloads.json` (a row with every aggregate and a row
  with none; a `TagCount` with no `tag` is a digest-pinned image, Section
  7.5), `workload_detail.json`, `jobs.json` (`complete` true on the second
  row, `succeeded` absent on the first; Section 4.5 "succeeded is read
  from condition_type").
- `ClusterTests`: `clusters.json` (one cluster with runtime state and
  `lastError`, one with nothing but identity; Section 4.1) and the
  `ClusterScope` table (Section 7.1): all by default; a selected id that
  is no longer in the list is dropped; a selection that becomes empty
  resets to all; a selection of one keeps that one.
- `StatusTests`: `status.json` into `DaemonStatus`, `daemonRunning` true,
  cluster 2 with no runtime fields (Section 4.6). `kube_contexts.json`,
  `kube_namespaces.json`, `kube_namespaces_forbidden.json` (`forbidden`
  absent means false, present means true; Section 4.1).
- `TimestampTests`: a table with the process layout
  (`2026-08-27T14:03:11.482913Z`), the Kubernetes layout
  (`2026-08-27T14:38:00Z`) and an unparseable string (`date == nil`, `raw`
  kept) (Section 3.1 "timestamps are strings in the stored layout";
  `CLAUDE.md` "Kubernetes timestamps are stored verbatim").
- `DisplayTests`: `podNameSuffix` table (Section 7.3: prefix match gives
  the suffix; no match gives the full name; empty workload gives the full
  name; a name equal to the workload gives the full name);
  `middleElided` table (Section 7.4: a value shorter than the budget is
  unchanged; a long value keeps its head and tail with `...` between and
  never ends in `...`); `scopePrefix` table (Section 7.2: prefix only when
  more than one cluster is selected); `durationText` table (mockup 1a:
  `38m`, `1h 29m`; under a minute `<1m`).
- `ErrorTests`: `error.json` into `APIError.notFound(message:)`,
  `validation_error.json` into `APIError.badRequest(violations:)` with
  both violations (Section 3.1 errors row).

No test asserts a field-at-a-time; every expectation is a whole value.
No test for `byteCount` (formatting a library already formats) or for
the enum raw values (a constant equals its value).

Checkpoint, commit: `macos: add model layer with fixture decoding tests`.

**Consumes:** `Components.Schemas.*` from Task 1.
**Produces:** every type and function named above, used unchanged by
Tasks 3-10.

## Task 3: connection, scope and the incidents list

Section 9.3 row 1; 7.1, 7.2, 7.4, 7.6; 5 (the open list subscribes to
the incident stream); 9.1 (not connected state; preferences); 9.4; 9.5.
Mockup screens 1a, 1b, C1, C4.

App layer:

- `Preferences`: `@AppStorage`-backed daemon address (default
  `127.0.0.1:7770`), cluster scope (`ClusterScope.selected` as an array of
  ids), grouping mode, column widths keyed by table name. Nothing else is
  persisted (Section 9.1).
- `DaemonConnection`: builds `IdiosAPI.Client` with `URLSessionTransport`
  on `http://<address>`; exposes `state: .connected | .unreachable(error)`
  updated by every store call; a settings window edits the address.
- `Route`: `.incidents(IncidentState?)`, `.incident(id)`,
  `.timeline(id)`, `.pod(uid)`, `.workloads`, `.workload(cluster, ns,
  kind, name)`, `.status`, `.browser`; parsed from `-route` for
  screenshots and used by the menu bar extra in Task 9.

Stores (`@Observable`, `@MainActor`):

- `ClustersStore`: `GET /v1/clusters`, subscribes to `/v1/clusters/stream`,
  replaces by id, reconciles the scope on every load (Section 7.1).
- `IncidentsStore`: `GET /v1/incidents` for the selected folder and scope,
  `GET /v1/incidents/counts`; subscribes to `/v1/incidents/stream` with
  `cluster_ids`, replaces by id and refetches counts; on reconnect
  reloads then resubscribes (Section 5). Filter text narrows client-side
  over workload, pod name, reason, container.

Views:

- Sidebar: CLUSTERS checklist (checkbox, ready dot, name, `lastError`
  wrapped under the name in red when set, open count from
  `byCluster`); INCIDENTS folders (Open, Acknowledged, Recovered, Pod
  deleted, Marked resolved, Dismissed) with counts; CATEGORY folders with
  counts among open incidents; footer `Add cluster...` disabled with a
  tooltip "writes arrive in a later version".
- Toolbar: title from the folder, scope pill (`2 of 3 clusters`; hidden
  when all), Group popup (Workload, Namespace, Category, Flat), filter
  field bound to Cmd-K.
- List: grouped `List` of incident rows exactly as mockup 1a: category
  badge, workload name, container chip (or `pod-level` dimmed when
  `containerName` is nil), raw `lastReason`, second line with the scope
  prefix, pod suffix (middle-elide treatment with the full name on hover
  and copy), exit/signal, tag, deletion facts with `(inferred)`;
  occurrences; `open 38m` or `closed 19m ago`; state badge. Rows with
  `acknowledgedAt` set take the lighter weight (mockup 1b).
- Components: `CategoryBadge`, `StateBadge`, `ContainerStateBadge`,
  `GapBadge` with the C1 colours for both appearances as the only
  hard-coded colours (Section 9.5); `WrapText`, `MiddleElidedText` (hover
  shows the full value, a copy button, `.help` carries the raw string),
  `ExpandableText` (one line, click expands in place), and the
  `NotConnectedView` (address, last error, Retry) that every screen shows
  when `DaemonConnection.state` is unreachable.
- Selecting a row navigates to `.incident(id)`; the detail arrives in
  Task 4, so this task shows a placeholder with the id.

Screenshot review: `hack/macos/screenshot.sh incidents` against the smoke
daemon, and once against nothing running for the not-connected state
(`--allow-disconnected`). The reviewer compares with 1a: the four smoke
incidents grouped by workload, counts in the sidebar, the scope pill
hidden (one cluster), no prefix on rows (one cluster selected), the
cluster dot green.

No new unit tests: the derivations this screen uses are tested in Task 2;
the screen itself is Section 11 item 6.

Checkpoint, commit: `macos: add connection, cluster scope and incidents list`.

**Consumes:** Task 2 types; `Client.listIncidents`, `getIncidentCounts`,
`listClusters`, `streamIncidents`, `streamClusters`.
**Produces:** `Preferences`, `DaemonConnection`, `Route`, `ClustersStore`,
`IncidentsStore`, the badge and text-treatment components,
`NotConnectedView`, the sidebar and main split view every later screen
plugs into.

## Task 4: incident detail

Section 9.3 row 2; 4.2 (`GET /v1/incidents/{id}`), 4.4 (artifact
content), 7.4 treatment 4 (log pane), 7.5 (images), 7.6 (gap states,
deleted banner), Section 1 boundary 2 (disabled action controls with the
explanation), 9.4 (`NSTextView` log pane, prose tables as `List`s).
Mockup 2a, 2b.

- `IncidentDetailStore`: loads `IncidentDetail`; reloads on window focus
  and when the incident stream carries its id (Section 5); fetches
  artifact content on demand through `URLSession` from
  `http://<address>/v1/artifacts/{id}/content`, keeps `[artifactID:
  Result<Data, APIError>]`. A 404 is decoded as sebuf's `Error` and shown
  with the row's `captureGap` and `captureNote` quoted.
- Header: category and state tags, `incident <id> - occurrences N`, a
  sentence built from container, pod and `lastReason`, the identity line
  `cluster / namespace / Kind workload / controller / pod / container`
  (full values, expandable). The four action buttons (Note, Dismiss, Mark
  resolved, Acknowledge) disabled with the callout text from mockup 2a.
- Tabs: Overview, Timeline (Task 5), Events (count), Logs (file count),
  pod.json, Pod (navigates to Task 6).
- Overview: "What the kubelet reports" grid from the subject `Container`
  (state and reason, restart count, last terminated reason exit signal,
  `lastTerminatedAt` marked `(k8s)`, first and last reason, image at
  open, `imageID` middle-elided, ready with the kind-dependent meaning,
  phase with the note that phase is not used for category, kind, QoS);
  `lastMessage` wrapped; Captured logs card with a per-restart chip row
  from the incident's artifacts (`restart_NNN`, `current.log`,
  `pod.json`; a gap chip is dashed) and the log pane; a gap card for the
  selected chip when it has no file; Attached events as a `List` with
  wrap; Resource envelope (requests and limits as written, with the
  bytes in parentheses); Siblings (`pods idios has seen`, never `of N`).
- Right rail: owner chain, context (cluster name, context name, API
  server expandable, namespace, node, QoS, image tag, container id
  middle-elided), times (every timestamp the row carries, with `(k8s)` or
  `(observed)`, `Open for`, `Will close` text derived from state), and
  the deleted banner when `podDeletedAt` is set (`deletion_reason`
  labelled inferred, swept after `deletedAt + retentionDays` from the
  status store, Section 7.6).
- `LogPane`: `NSViewRepresentable` over `NSScrollView` + `NSTextView`,
  monospaced, non-editable, selectable, horizontal scroll by default, a
  Wrap toggle, find via the system find bar, Copy copies the raw bytes.
  `pod.json` renders in the same pane pretty-printed.

Screenshot review: `incident/<smoke crash id>` against the smoke daemon.
The reviewer checks: header sentence, disabled buttons with the callout,
kubelet grid values equal to `idios status` output for that incident,
the log chips including one gap chip if the smoke run produced one, a
wrapped event message, the rail.

Checkpoint, commit: `macos: add incident detail with log pane`.

**Consumes:** Task 3 shell; `Client.getIncident`; `URLSession` for content.
**Produces:** `IncidentDetailStore`, `LogPane`, `ArtifactChips`,
`EventsList`, `DeletedBanner`, reused by Tasks 5 and 6.

## Task 5: incident timeline

Section 9.3 row 3; 4.2 timeline row (six kinds, `cut`, hollow
reconstructed); 7.6. Mockup 3a.

- `TimelineStore` loads `GET /v1/incidents/{id}/timeline` on tab entry
  and on the same reload triggers as the detail.
- View: a segmented filter All / Transitions / Events / Captures; a
  vertical list with the Kubernetes time on the left (observed time when
  there is none, marked), a dot coloured by kind and by the transition's
  category, hollow and dashed when `gapReconstructed`; the `cut` entry as
  a dashed rule with its text spanning the row; lifecycle entries
  `incident opened` and `incident closed: <reason>`; a rollout entry
  with the ReplicaSet name, revision and image tag; a capture entry with
  the artifact kind and gap; an event entry with reason, count and
  wrapped message.

Screenshot review: `timeline/<id>` against the smoke daemon: order,
hollow dot if a reconstructed row exists, both lifecycle entries on a
closed incident.

Checkpoint, commit: `macos: add incident timeline`.

**Consumes:** `Client.incidentTimeline`, `TimelineEntry`.
**Produces:** `TimelineStore`, `TimelineView`.

## Task 6: pod

Section 9.3 row 4; 4.3 (`/pods/{uid}`, `/events`, `/history`); 7.6
deleted banner; 9.4. Mockup 4a, 4b.

- `PodStore` loads the three endpoints; events and history on tab entry.
- Tabs: Containers, Incidents, Events, Conditions, Logs, pod.json.
  Containers is a `Table` (fixed-height rows: name, kind badge, state and
  reason badge, exit, restarts, ready with its kind meaning, tag) with
  remembered column widths (`SceneStorage`); selecting a container fills
  the inspector rail and the Captured files table (file name from
  `filePath`, kind, index, size, truncated, early, captured at or gap
  badge with the quoted note). Incidents reuses the Task 3 row. Events
  and Conditions are `List`s with wrap. Logs and pod.json reuse
  `LogPane`.
- Inspector rail: state summary, uid middle-elided, namespace, node,
  controller, workload, QoS, every pod timestamp with `null (still
  exists)` for a nil `deletedAt`, the selected container's image,
  `imageID`, `containerId` (full, wrapped: detail pages never elide),
  resources, last terminated; latest condition per type.
- Deleted banner at the top when `deletedAt` is set: `deleted_at`,
  `deletion_source`, `deletion_reason (inferred)`, swept-after date; for
  `deletion_source = unwatched` the banner says the namespace was removed
  from the watched set; for `reconcile` that the pod vanished while idios
  was not running (mockup C3 text).
- Toolbar: Copy uid. `Open artifacts folder` is not built: the
  application never opens the artifact directory (roadmap decision 5);
  Task 11 removes it from the mockup note in the design doc.

Screenshot review: `pod/<uid>` for the smoke crash pod, and a deleted
pod if the smoke run has one.

Checkpoint, commit: `macos: add pod screen`.

**Consumes:** `Client.getPod`, `podEvents`, `podHistory`; Task 4
components.
**Produces:** `PodStore`, `PodView`, `ContainersTable`,
`ConditionsList`, reused by Task 10.

## Task 7: workloads and jobs

Section 9.3 row 5; 4.5. Mockup 5a, 5b.

- `WorkloadsStore` loads `GET /v1/workloads` for the scope; a tree by
  cluster then `(cluster_id, namespace)` then workload (Section 7.2:
  grouping on the pair) in the sidebar with a filter field; open counts
  per node. Selecting a workload loads `GET
  /v1/workloads/{cluster_id}/{namespace}/{kind}/{name}`. For a `CronJob`
  workload it also loads `GET /v1/jobs?cluster_ids=&namespace=` and keeps
  the rows whose `cronjobName` equals the workload name: the workload row
  does not carry `cronjob_uid`, and `JobRow` carries the name.
- Detail: four cards (incidents in window with open and closed; by
  category badges; occurrences with pod count; image tags at open with
  `x<count>`, a tagless entry shown as `digest`); Rollouts table (rev,
  ReplicaSet, images, first seen, incidents); Restarts per hour as bars
  over `restartsByHour`, hollow when `reconstructed`, with the window
  labels; Pods of this workload table (pod name, state badge from
  `worstState`, restarts not available on the row so the column shows
  open incidents, node, created, deleted with reason `(inferred)`).
- Jobs table for CronJob workloads: job name, condition badge (`Complete`
  green, `Failed` red, none is `running` with `active`), started,
  duration from `startedAt` to `finishedAt`, reason; the footer sentence
  from mockup 5b about counters versus conditions.

Screenshot review: `workloads` and `workload/<smoke-crash>` against the
smoke daemon.

Checkpoint, commit: `macos: add workloads and jobs`.

**Consumes:** `Client.listWorkloads`, `getWorkload`, `listJobs`.
**Produces:** `WorkloadsStore`, `WorkloadsView`, `WorkloadDetailView`,
`JobsTable`.

## Task 8: status

Section 9.3 row 6; 4.6; Section 5 (reloads every ten seconds while
visible). Mockup 6a.

- `StatusStore` polls `GET /v1/status` every 10 s while the screen is
  visible and once at launch (Task 4 and 6 read `retentionDays` from it).
- View: title line (`daemon pid N`, `daemonRunning` false shown as `no
  recorder behind this API` in orange, `writtenAt`); Clusters table
  (name from `ClustersStore`, ready badge, last object time with age,
  skew, `lastError` wrapped in red); four cards (writer transactions,
  errors, p99; handler errors and panics; capture queued, completed,
  dropped; closer last tick, closed, attached); Artifacts by outcome bars
  and by gap; Storage (`dbBytes`, `walBytes`, artifacts bytes and files,
  retention, row counts); Latest sweep table with error wrapped;
  configured intervals.

Screenshot review: `status` against the smoke daemon and against the
mock (which shows `daemonRunning` false).

Checkpoint, commit: `macos: add status screen`.

**Consumes:** `Client.getStatus`, `DaemonStatus`.
**Produces:** `StatusStore`, `StatusView`.

## Task 9: menu bar extra

Section 9.3 row 7; 4.7 (ignores the scope); 7.6 (a cluster with
`lastError` is red). Mockup 7a.

- `MenuBarStore`: `GET /v1/incidents?state=open&limit=5` and `GET
  /v1/clusters` with no `cluster_ids`, refreshed every 30 s and on the
  two streams' events (its own subscriptions, unscoped).
- `MenuBarExtra` with `.window` style: label is the open count with a red
  dot when any cluster has `lastError`; popover shows `N open incidents`,
  `M clusters - K with an error`, the five newest by `lastSeenAt` (each
  `workload / container - category, N occ.`, clicking opens the main
  window at `.incident(id)`), one line per cluster with ready state and
  age or the wrapped error, then `Open idios` (Cmd-Shift-I), `Status...`,
  `Clusters and namespaces...` disabled with the writes-later tooltip.

Screenshot review: the popover cannot be captured by the window
screenshot path; the implementer captures the main window opened from
the popover item and the reviewer checks the popover from a description
plus `-route menubar` rendering the popover content view in a plain
window for the screenshot only.

Checkpoint, commit: `macos: add menu bar extra`.

**Consumes:** `Client.listIncidents`, `listClusters`, `Route`.
**Produces:** `MenuBarStore`, `MenuBarView`.

## Task 10: column browser

Section 9.3 row 8; 7.3 (the identity chain is the navigation). Mockup
C2.

- `BrowserStore`: columns cluster (from `ClustersStore`, within scope),
  namespace (the cluster's watched namespaces), workload (`GET
  /v1/workloads?cluster_ids=&namespace=`), pod (`GET
  /v1/pods?cluster_ids=&namespace=&workload_kind=&workload_name=`, live
  and deleted, deleted dimmed with `(deleted)`), container (from `GET
  /v1/pods/{uid}`).
- View: five `List` columns in an `HStack` with a breadcrumb of chips on
  top; selecting a pod shows the Task 6 `PodView` below the columns or
  navigates on double-click; selecting a container selects it in that
  view. The filter chips row from C2 (`open incident`, `ready = 0`,
  `restart_count > 0`, `deleted_at IS NULL`, `category = crash`) filters
  the pod column client-side over `PodRow` fields; a chip the row set
  cannot answer (`restart_count` is not on `PodRow`) is not offered.

Screenshot review: `browser` with the smoke crash pod selected.

Checkpoint, commit: `macos: add column browser`.

**Consumes:** `ClustersStore`, `Client.listWorkloads`, `listPods`,
`getPod`, `PodView`.
**Produces:** `BrowserStore`, `BrowserView`.

## Task 11: docs and roadmap

- `docs/design/presentation.md`: Section 3.2 gains the sentence that the
  generated Swift enums are closed, so an additive enum value in the
  daemon requires an application rebuild until the contract emits named
  enum schemas; Section 9.2 shows the layout as built (package targets
  `IdiosAPI` and `IdiosModel` under `macos/Sources`, application folders
  under `macos/idios`); Section 9.3 pod row loses `Open artifacts folder`
  if the mockup note is echoed anywhere; Section 11 item 5 names
  `swift test --package-path macos` and the `#filePath` fixture path.
- `CLAUDE.md`: Conventions gain the Swift rules that outlive the phase
  (model types are built only through `init(wire:)`; views never import
  `IdiosAPI`; the only hard-coded colours are the badge vocabulary; the
  Xcode project is hand-written with a synchronized folder, files are
  added by creating them under `macos/idios/`; `Package.resolved` is
  committed; `make app`, `make app-test` in the checkpoint when `macos/`
  changed). Development environment gains: the fixture server and the
  smoke data directory commands, that both listen on 7770 and never run
  together, and `hack/macos/screenshot.sh`.
- Roadmap: the Phase 9 status line and the top status line.
- Commit: `docs: close phase 9`.

## Self-review

Spec coverage: Section 3.1 Task 2 (int64 strings, enums, optionals,
timestamps as strings); 3.2 Task 11 (limitation written down); 4.1 Tasks
3, 10; 4.2 Tasks 3, 4, 5; 4.3 Tasks 6, 10; 4.4 Task 4; 4.5 Task 7; 4.6
Task 8; 4.7 Task 9; Section 5 Tasks 3, 4, 9 (subscribe, replace by id,
reload on reconnect, detail reload on focus and on its id, status every
ten seconds); 7.1 Tasks 2, 3; 7.2 Tasks 2, 3, 7; 7.3 Tasks 2, 10; 7.4
Tasks 2, 3, 4; 7.5 Tasks 4, 7; 7.6 Tasks 3, 4, 5, 6, 9; Section 8 Tasks
3, 4, 9 (controls disabled with the explanation); 9.1 Tasks 1, 3; 9.2
Tasks 1, 2; 9.3 Tasks 3-10 in the roadmap's order; 9.4 Tasks 4, 6; 9.5
Task 3; Section 10 Task 4 (content shown and copied, never sent
elsewhere); 11 item 5 Task 2, item 6 every screen task; 13 Task 3.

Deviations from the roadmap, stated: the roadmap's `macos/idios/
Generated Model Store Views App` becomes a package for `Generated` and
`Model` beside the application folder, because `swift test` needs a
package and the plugin output is never a source folder. `Open artifacts
folder` from mockup 4a is not built (decision 5). Section 3.2's unknown
enum tolerance cannot be met with closed generated enums; recorded, not
worked around.

Rules checked: ASCII in every file including `project.pbxproj`; every
test in Task 2 names its spec sentence and compares whole values; no
test in Tasks 3-10 (screens are checked by a person); comments in Swift
carry reasons only; no code references a document; generated Swift is
build output and never committed; nothing under `macos/` imports SQLite
or reads a kubeconfig; one commit per green task with the checkpoint
`make app-test && make app && make ascii`.

Type consistency: `Incident`, `Page<T>`, `ClusterScope`, `Timestamp`
are defined in Task 2 and used unchanged by every later task; `Route` and
`DaemonConnection` (Task 3) are the only seams Tasks 4-10 plug into;
`LogPane` and `DeletedBanner` (Task 4) are reused by Task 6; `PodView`
(Task 6) by Task 10; `StatusStore.retentionDays` (Task 8) is read by
Tasks 4 and 6, which show the swept-after date only once it has loaded.

## Hands to the next phase

Phase 10 adds the write endpoints to the daemon and enables the controls
the application already draws disabled. The names it plugs into:

- The disabled controls: the four action buttons in the toolbar of
  `Views/IncidentDetail/IncidentDetailScreen.swift` (Note..., Dismiss,
  Mark resolved, Acknowledge, each with a `.help`), the `Add cluster...`
  footer of `Views/Incidents/IncidentsSidebar.swift`, and
  `Clusters and namespaces...` in `Views/MenuBar/MenuBarView.swift`. The
  "human actions" callout card in `IncidentHeader.swift` goes away when
  the buttons work.
- Every client call lives in a store under `macos/idios/Store/`; a write
  is a new method on the store that owns the row (`IncidentDetailStore`
  for the incident actions, `ClustersStore` for cluster and namespace
  changes). Errors map through `apiError(_:)` and
  `apiError(statusCode:body:)` in `Store/CallError.swift`; the generated
  outputs are `.ok`, `.badRequest` and `.default(statusCode, payload)`,
  there is no `.notFound` case. Client operation names are capitalised
  (`ListIncidents`, `GetIncident`); the `query:`/`path:` sugar lives on
  `APIProtocol`.
- After a write, nothing needs a manual refresh: the incident stream
  carries the updated row, `IncidentsStore.apply` replaces it by id and
  `IncidentDetailStore.watch` reloads the detail for its id; a cluster
  write reaches `ClustersStore` through the cluster stream and the scope
  reconciles on every load.
- Navigation: `Route` (`App/Route.swift`) names every screen; `Navigator`
  (`open(_:)`) opens a route in the main window from anywhere, which is
  how the menu bar extra does it; `IdiosApp.openMain` raises the window.
- `DaemonConnection` (`App/Connection.swift`) holds the client and the
  base URL; `-daemon host:port` overrides the preference at launch.
- The add-cluster flow needs `GET /v1/kube/contexts` and
  `GET /v1/kube/contexts/{context}/namespaces`; `KubeContext` and
  `KubeNamespaces` (with `forbidden`) are already in `IdiosModel`, no
  store calls them yet. `idios mock` answers 501 for both.
- Two confirmations are due (delete incident, remove cluster); the
  application has no alert code yet.
- The model layer refuses a row that lacks an identity field with
  `ModelError.missing(field:)`, shown by the screens as a decoding error
  naming the field; a write response that returns the updated row goes
  through the same `init(wire:)`.
- `hack/macos/screenshot.sh` is how every screen change is reviewed;
  routes are listed in `CLAUDE.md`. The `-screenshot` argument is only a
  trigger; the pixels come from `screencapture`.
- `api/testdata/status.json` has no gap-less capture bucket, while the
  daemon sends one for the attempts that produced a file; the Swift test
  covers it by mutating the fixture. A Go-side fixture with that bucket
  would let both sides read the same bytes again.
