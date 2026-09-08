# m6 step C - application: the links

Goal: the served Explore URLs become visible - a Grafana button on the
incident detail header, on every pod container row, and beside a
missing-log capture gap - each opening the daemon-built URL in the
browser. The app constructs nothing: step A builds, step B configured,
step C opens.

Architecture: two optional model fields decoded from wire fields that
already exist; three small view additions reusing `GrafanaMark`; URLs
open through SwiftUI's `openURL` environment action - the first external
URL the app opens, so the convention is set here: a served string that
does not parse as a URL renders no button rather than a dead one.

Tech stack: Swift / SwiftUI (macOS 15), swift-testing.

Spec: `docs/design/presentation.md` (link placement statements, added by
task 1); step A's and step B's "Hands to the next phase" notes.

Global constraints: as step B (CLAUDE.md Swift rules, `.ai` rules,
roadmap decisions 5-7). No proto or Go change anywhere in this step;
checkpoint is `make app-test && make app` plus one final
`go build ./... && go test ./... && make ascii`.

## File structure

    docs/design/presentation.md                      placement statements (task 1)
    macos/Sources/IdiosModel/Incident.swift          IncidentDetail.grafanaURL (task 1)
    macos/Sources/IdiosModel/Pod.swift               Container.grafanaURL (task 1)
    macos/Tests/IdiosModelTests/IncidentTests.swift  literals grow (task 1)
    macos/Tests/IdiosModelTests/PodTests.swift       literals split (task 1)
    macos/idios/Views/Components/GrafanaLinkButton.swift  the one button (task 2)
    macos/idios/Views/IncidentDetail/IncidentHeader.swift  header button (task 2)
    macos/idios/Views/Pod/PodContainers.swift        row + gap buttons (task 2)

## Task 1 - model: the two fields

Spec first, in `docs/design/presentation.md` beside the Grafana links
section: the application shows a served link as a Grafana-marked button
in exactly three places - the incident detail header (the incident's own
window), each pod container row (that container's run), and a captured-
files row whose log is missing (`capture_gap` set), where the container's
link is the fallback the capture could not provide; a row or detail
without the field shows nothing, and the app never assembles or edits an
Explore URL.

Then the model, following each struct's existing optional convention
(plain `String?` passes through):

- `IncidentDetail` gains `grafanaURL: String?` from `wire.grafanaUrl`;
  `replacingIncident(_:)` rebuilds memberwise and must carry it.
- `Container` gains `grafanaURL: String?` from `wire.grafanaUrl`.

Tests: the fixtures already disagree on purpose - step A fills
per-container URLs only on `GetPod`, so `pod_detail.json`'s container
carries `grafanaUrl` and `incident_detail.json`'s same container does
not. The shared `apiContainer` literal in `PodTests.swift` therefore
splits: the pod-detail test's literal carries the fixture's exact URL
string, the incident-detail tests' literal carries nil, and
`IncidentTests.swift` asserts the new top-level `grafanaURL` against
`incident_detail.json`'s value. Trace: the task 1 placement statements
and daemon.md task 4 (which detail fills which field).

Steps: failing tests (update the literals first, watch them fail against
the unextended model), `make app-test`, implement, green, `make app`.

Consumes: `Components.Schemas.IncidentDetail.grafanaUrl`,
`Components.Schemas.Container.grafanaUrl` (step A), the step A fixtures.
Produces: `IncidentDetail.grafanaURL`, `Container.grafanaURL`.

## Task 2 - the three buttons

`GrafanaLinkButton.swift` in Components, the single shared control:
`GrafanaMark` plus `.buttonStyle(.borderless)` and
`.help("Open logs in Grafana")`, following the copy-button shape in
`TextTreatments.swift`. It takes the served string, renders nothing when
the string is nil or does not parse as a URL, and opens through
`@Environment(\.openURL)`.

- `IncidentHeader.swift`: the button trails the `tags` HStack, fed
  `detail.grafanaURL`.
- `PodContainers.swift`, `ContainersTable.row`: the button trails the
  row, fed `container.grafanaURL`; absent field, no trailing element.
- `PodContainers.swift`, `CapturedFilesCard`: a captured-files row whose
  `filePath` is nil shows the button beside its `GapBadge`, fed the
  matching container's `grafanaURL` (the card gains the containers - or
  a name-to-URL lookup - from the pod detail it already renders beside).
  This is the feature's reason to exist: the log idios could not capture
  still has an address.

No new tests: the button is composition over task 1's decoded fields;
the URL-parse guard is one `URL(string:)` check inside a view. Verify
visually against the mock (its fixtures carry both fields since step A):
`hack/macos/screenshot.sh incident/<id> <png>` and `pod/<uid> <png>`
with `IDIOS_DAEMON` on a scratch-port mock, ids from
`curl <addr>/v1/incidents`.

Steps: implement, `make app-test && make app`, screenshots, final
`go build ./... && go test ./... && make ascii`.

Consumes: `IncidentDetail.grafanaURL`, `Container.grafanaURL` (task 1),
`GrafanaMark` (step B), `IncidentHeader.tags`, `ContainersTable.row`,
`CapturedFilesCard`.
Produces: the three buttons; m6's user-visible feature complete.

## Hands to the next phase

None: this is m6's last step. Facts that outlive the milestone move to
`CLAUDE.md` or the design docs at close (the colour exception and the
builder contract already did in steps A and B).

## Self-review

- Spec coverage: the three placements are one task 1 statement each,
  implemented by task 2; the field decoding is pinned by the grown
  fixture-literal tests.
- `.ai/tests.md`: the only decidable new behaviour (two optional field
  decodes, fixture asymmetry between the two details) is tested with
  whole-struct literals; view composition adds none.
- `.ai/scope.md`: no timeline per-run links, no link on incident list
  rows or artifact rows that have their log, no URL construction.
- Type consistency: both fields are `String?` end to end; the button
  takes the string as served, so daemon and screen can never disagree
  about the URL.
