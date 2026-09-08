# m6 step B - application: the clusters sheet

Goal: the clusters sheet becomes the two-pane master-detail of mockup
page 9 - cluster rail on the left; Cluster, Watched namespaces, Grafana
and Remove sections on the right - and the Grafana section edits the
cluster's configuration through `SetClusterGrafana` with a label builder,
never a free-text selector.

Architecture: the selector template logic (rows to string, string to
rows, preview substitution) is pure Swift in `IdiosModel`, where the test
target lives; `ClustersStore` gains the two client calls; the sheet views
compose the existing pieces (`AddNamespaceField`, `NamespacePicker`,
rename, remove) into sections. The generated client already carries
`SetClusterGrafana`: `macos/Sources/IdiosAPI/openapi.yaml` is a symlink
to `api/openapi/IdiosService.openapi.yaml`, which step A regenerated.

Tech stack: Swift / SwiftUI (macOS 15), swift-openapi-generator build
plugin, swift-testing (`@Test`, `#expect`) in `IdiosModelTests`.

Spec: `docs/design/presentation.md` (the clusters sheet paragraph and the
label-builder contract, added by task 1); visual reference
`docs/mockups/idios-ui.html` page 9; step A's handoff
(`daemon.md`, "Hands to the next phase").

Global constraints: the Swift rules in `CLAUDE.md` (ASCII, one-line doc
comment per type, model types built only through `init(wire:)`, views
never import `IdiosAPI`, stores own every client call, `.cancelled`
ignored, UTC everywhere); `.ai` rules as in step A; roadmap decisions 2,
6 and 7. After step A's proto change, `rm -rf macos/.build` before the
first `make app-test`: the incremental build has crashed on a stale
generated `Types.swift`. Checkpoint for every task here:
`make app-test && make app` (plus `go build ./... && go test ./... &&
make ascii` once, nothing Go-side changes).

## File structure

    docs/design/presentation.md                       sheet + builder spec (task 1)
    macos/Sources/IdiosModel/Cluster.swift            three fields (task 1)
    macos/Sources/IdiosModel/GrafanaLabels.swift      rows, parse, render, preview (task 1)
    macos/Tests/IdiosModelTests/ClusterTests.swift    literals grow (task 1)
    macos/Tests/IdiosModelTests/GrafanaLabelsTests.swift  (task 1)
    macos/idios/Store/ClustersStore.swift             setGrafana, samplePreview (task 2)
    macos/idios/Views/Clusters/ClustersSheet.swift    two-pane shell (task 3)
    macos/idios/Views/Clusters/ClusterDetailPane.swift  the four sections (task 3)
    macos/idios/Views/Clusters/GrafanaSection.swift   label builder UI (task 3)
    macos/idios/Views/Components/GrafanaMark.swift    brand mark (task 3)
    CLAUDE.md                                         colour-rule exception (task 3)

## Task 1 - model: Cluster fields and the label builder

Spec first, in `docs/design/presentation.md` beside the existing
application sections: the clusters sheet is a two-pane master-detail
(rail of `clusters` rows; Cluster, Watched namespaces, Grafana, Remove
sections); the Grafana section is a label builder - one row per LogQL
matcher, the label name free text, the value one of Namespace, Pod name,
Container name, Workload name, Node name, Cluster name or a static Text;
the three standard rows are the prefill; the builder round-trips the
stored `log_selector` (it is its only writer, step A's daemon never
parses beyond substitution); a live preview substitutes one of the
cluster's real pods and falls back to fixed sample names when the
cluster has none.

Then the model:

- `Cluster.swift`: `grafanaURL`, `lokiDatasourceUID`, `logSelector`, all
  `String`, absent wire values taking `""` the way `namespaces` takes
  `[]` (proto3 drops empty strings). Update the memberwise literals in
  `ClusterTests.swift`; the fixture `clusters.json` already carries a
  configured cluster from step A's wire test regeneration.
- `GrafanaLabels.swift`, pure and total:

      public enum GrafanaLabelValue: Hashable, Sendable {
          case namespace, pod, container, workload, node, cluster
          case text(String)
      }
      public struct GrafanaLabelRow: Hashable, Sendable {
          public var name: String
          public var value: GrafanaLabelValue
      }
      public struct GrafanaLabels: Hashable, Sendable {
          public var rows: [GrafanaLabelRow]
          public static let standard: GrafanaLabels
          public init(selector: String)
          public var selector: String
          public func preview(values: [GrafanaLabelValue: String]) -> String
      }

  `standard` is the three rows whose `selector` renders exactly step A's
  `grafana.DefaultSelector` string (`{namespace="$namespace",
  pod="$pod", container="$container"}`) - one test pins the two byte for
  byte. `init(selector:)` splits the brace body on commas outside
  quotes, reads `name="value"`, maps `$namespace`/`$pod`/`$container`/
  `$workload`/`$node`/`$cluster` to their cases and anything else to
  `.text` with `\"` unescaped; a matcher it cannot read becomes a
  `.text` row carrying the raw value, so nothing stored is ever lost.
  `selector` is the inverse (Text values `\"`-escaped). `preview`
  substitutes the given values and renders the same dropped-matcher rule
  as the daemon: a row whose placeholder value is empty disappears.

Tests (`GrafanaLabelsTests.swift`, `@Test(arguments:)` tables,
whole-value assertions): standard equals the daemon default; round-trip
selector -> rows -> selector for the standard, a renamed label, a static
text row, a text value containing a comma and an escaped quote; parse of
an unreadable matcher preserving it as text; preview substituting a full
value set; preview dropping an empty container. Each row traces to the
task 1 presentation.md builder contract; the round-trip rows also trace
to roadmap decision 2 (the builder is the selector's only writer).

Steps: failing tests, `rm -rf macos/.build`, `make app-test`, implement,
`make app-test` green, `make app`, done.

Consumes: `Components.Schemas.Cluster.grafanaUrl/lokiDatasourceUid/
logSelector` (step A), `require(_:_:)`, fixture `clusters.json`.
Produces: `Cluster.grafanaURL/lokiDatasourceUID/logSelector`,
`GrafanaLabels`, `GrafanaLabelRow`, `GrafanaLabelValue`.

## Task 2 - store: the two calls

`ClustersStore.swift`, following `rename` exactly (three-arm output
switch, `reportAction`, `.cancelled` ignored):

- `func setGrafana(_ clusterID: String, url: String, datasourceUID:
  String, selector: String, connection: DaemonConnection) async -> Bool`
  calling `SetClusterGrafana(path: .init(id:), body: .json(.init(id:,
  grafanaUrl:, lokiDatasourceUid:, logSelector:)))` - the body repeats
  the path id, the daemon binds the path last. The stream echoes the
  updated row (step A notifies `notify.Cluster`), so the store does not
  edit `clusters` locally.
- `func previewPod(of clusterID: String, connection: DaemonConnection)
  async -> GrafanaPreviewValues?` calling `ListPods(query:
  .init(clusterIds: [id], limit: 1))` then `GetPod` on the returned uid
  for its first container name; nil on no pods or any error (the
  preview falls back, it never reports). `GrafanaPreviewValues` is a
  small `IdiosModel` struct (namespace, pod, container, workload, node,
  cluster) so task 1's `preview(values:)` takes it without the view
  layer touching wire types.

No store tests: the package has none by convention, and both methods are
one switch over a generated call; the logic they feed is task 1's tested
model code.

Steps: implement, `make app-test && make app` green, done.

Consumes: `GrafanaLabels`/`GrafanaPreviewValues` (task 1), generated
`SetClusterGrafana`, `ListPods`, `GetPod` operations, `reportAction`.
Produces: `ClustersStore.setGrafana`, `ClustersStore.previewPod`.

## Task 3 - the sheet

Rework `ClustersSheet.swift` to the mockup: `.frame(minWidth: 640,
minHeight: 520)`; an `HStack(spacing: 0)` of a fixed-width rail
(`.frame(width: 196)`), a `Divider`, and a `ClusterDetailPane` for the
selection, copying the `WorkloadsScreen` two-pane shape. The rail lists
`store.clusters` (status dot red when `lastError` is set, name, context
below in monospaced caption), keeps `@State selectedClusterID` defaulting
to the first row and reconciling when the selected cluster is removed;
"Add cluster..." stays at the rail's foot and keeps presenting
`AddClusterSheet`. The footer keeps Done; the remove alert and its copy
move untouched.

`ClusterDetailPane.swift`, four sections, each a titled inset group:

- Cluster: the rename `TextField` (submit behaviour moved as-is),
  read-only context, API server and connection line (ready dot,
  `lastEventAt`, `skewSeconds`).
- Watched namespaces: one row per namespace with the remove button, the
  existing `AddNamespaceField` at the bottom.
- Grafana: `GrafanaSection`.
- Remove: the destructive button and its warning sentence.

`GrafanaSection.swift`: two text fields (Grafana URL, Loki datasource
uid) and the label rows - label name `TextField`, value `Menu` with the
six sources and Text (a Text row adds an inline value field), minus per
row, an add-label button; rows start from `GrafanaLabels(selector:
cluster.logSelector)` when configured, `GrafanaLabels.standard`
otherwise. A preview line renders `labels.preview(values:)` from
`store.previewPod` (fetched `.task(id: cluster.id)`), falling back to
fixed sample names. Edits are local state; a Save button (disabled until
the state differs from the stored row and the URL field is non-empty)
calls `store.setGrafana` with `labels.selector`, and a Clear button
(shown only when the stored row is configured) calls it with three empty
strings - the daemon treats an empty URL as the clear. `actionError`
renders under the section the way the current sheet shows it. The
mockup's sheet has no Save control; the button is the one deliberate
addition, because the daemon validates the three fields together and
per-keystroke PATCHes would reject every half-typed state.

`GrafanaMark.swift`: the Grafana flame as a small `Path`-drawn view in
the brand orange, the sheet's section header its only current caller.
Update the `CLAUDE.md` hard-coded-colour sentence to name it beside
`BadgeStyle.swift` and `icon.py` (roadmap decision 6); no binary asset,
no new imageset.

No new tests: the sheet is composition; everything decidable
(round-trip, preview, dropping) was pinned in task 1. Verify visually:
`hack/macos/screenshot.sh clusters <png>` against `./bin/idios -data-dir
.storage mock` (the mock cluster is configured since step A) and once
against an empty configuration for the not-configured state.

Steps: implement, `make app-test && make app`, screenshots, done.

Consumes: everything above; `AddClusterSheet`, `AddNamespaceField`,
`NamespacePicker`, `BadgeStyle`, `Route.clusters` (unchanged).
Produces: the sheet of mockup page 9.

## Hands to the next phase

- `GrafanaMark` is the shared brand view; step C's link buttons reuse it
  rather than drawing a second flame.
- Step C reads `IncidentDetail.grafanaUrl` and `Container.grafanaUrl`
  from the wire as served and opens them with
  `NSWorkspace.shared.open`; nothing in B constructs a URL and nothing
  in C should either.
- The not-configured state (empty `Cluster.grafanaURL`) is what hides
  step C's buttons; the fields land on the model in task 1.

## Self-review

- Spec coverage: the presentation.md sheet paragraph is task 3, the
  builder contract task 1, the write call task 2; every
  `GrafanaLabelsTests` row traces to the builder contract or roadmap
  decision 2.
- `.ai/tests.md`: argument-table tests, byte-for-byte and whole-struct
  assertions; the untestable view layer adds none.
- `.ai/scope.md`: no link buttons yet (step C), no per-keystroke saves,
  no new routes, no store test scaffolding the package never had.
- CLAUDE.md Swift rules: model changes only through `init(wire:)` with
  absent-scalar zero defaults; views import `IdiosModel` only; the two
  new store methods are the only client callers; the colour exception is
  written into CLAUDE.md in the same commit that adds the colour.
- Type consistency: `GrafanaLabels.selector` output is byte-identical to
  `grafana.DefaultSelector` for `standard` (pinned by test), so a sheet
  that saves the untouched prefill stores exactly what the daemon's
  examples document.
