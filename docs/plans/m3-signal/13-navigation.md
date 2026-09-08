# Phase 13: navigation and information

Goal: getting to the row that matters takes the fewest possible motions,
and every grouping, heading and filter on the way says enough to be worth
reading. The sidebar stops pretending folders are places: the selection is
the screen, state and category are filters that compose, and what is in
force is visible and removable in the toolbar. The workloads tree puts the
long-lived kinds where the eye left them and gives a controller-less pod a
row that opens the pod. The incidents list survives a second cluster
without lying about where a row lives. The menu bar popover navigates and
gets out of the way. An empty cluster scope means empty. A keyboard reaches
the rows, the actions and the screens. And the Browser screen, whose every
capability now lives on a better screen, is deleted with its store and its
route.

Architecture: unchanged. Model types are built only through `init(wire:)`
in `IdiosModel`; stores own every client call and stream; views read stores
and never import `IdiosAPI`; the only hard-coded colours are the badge
vocabulary in `BadgeStyle.swift`. The daemon, the contract and the schema
are untouched: no proto change, no Go change, no fixture change. The only
non-Swift edits are two YAML lines in `hack/smoke/` and the docs.

Tech stack: unchanged. Swift, SwiftUI, deployment target macOS 15, the
generated OpenAPI client, `swift test --package-path macos` and
`xcodebuild` via `make app-test` and `make app`. No new dependency.

Spec: `presentation.md` 4.1 and 4.7 (the scope and menu bar endpoints),
5 (streams reload on resubscribe), 7.1 (cluster is a scope), 7.2
(namespace never without its cluster), 7.3 (identity chain), 9.3 (the
screens), 9.4 (the components that need care), Section 8 (the writes the
keys call, all idempotent); roadmap decisions 5, 6, 7 and 8. The visual
reference is `docs/mockups/idios-ui.html` (s1 incidents, s5 workloads, s7
menu bar), amended in place by the task that changes a screen; s5 already
carries the tree-by-kind and pod-row decisions.

Global constraints: every `.ai/*.md` rule, applied to Swift as CLAUDE.md
says (ASCII, comments say why, one-line doc comment per type, tests trace
to a spec statement with whole-value assertions). The m3 cross-cutting
decisions this phase touches:

- 3 (the reader's words on the surface, the store's words on hover --
  every new heading, chip and label follows it),
- 5 (long-lived kinds first, transient kinds last; inside one kind live
  before deleted; the order is never a user setting),
- 6 (a group heading says the category, a row says an identity; "No
  controller" is a heading, never a row),
- 7 (one highlight, one place you are; state and category are filters,
  composing, visible in the toolbar while in force),
- 8 (a screen that duplicates another is removed, not left dangling).

Everything lands on `m3-signal`; nothing merges to `main` until the user
agrees in that session. Phase 12's handoff names what the sidebar rebuild
must keep: `clusterDot(ready:hasError:)` as the one cluster-dot rule,
`Category.label` folder titles with the raw value on hover, and
`last_error` as the sidebar row's hover with the Status screen holding the
full text. It also names what Task 5 links into (`WorkloadTab`,
`defaultTab`, `WorkloadsStore.loadDetail/loadRuns/loadIncidents`) and
leaves the Browser screen word for word for Task 1's deletion.

Model per task: a fresh implementer per task -- Sonnet where the work is
mechanical or compiler-checked (Tasks 1, 6, 7), Opus where it is
structural, semantic or version-specific (Tasks 2, 3, 4, 5, 8). This
narrows the roadmap's phase-level "Opus" the way Phase 12 did; the risk
the roadmap names (many files at once, the seams) sits in the Opus tasks.
Opus reviews every diff against the task and the `.ai` rules before the
next task starts. Every screen task ends in `hack/macos/screenshot.sh`
output that a person reads against the spec before the next begins.

Verification data: `make smoke` fills `.storage/smoke`, then
`./bin/idios -data-dir .storage/smoke -kubeconfig ./kube/config run`
serves it on 7770 (never together with `idios mock`). Task 5 adds a bare
pod to `hack/smoke/` so the "No controller" heading has a row to show.
Task 4's more-than-one-cluster review runs against `./bin/idios -data-dir
.storage mock`, whose fixtures carry two clusters (ids 1 and 2); the
smoke cluster is one, and one cluster cannot show a cluster grouping.

## Decisions this plan makes

Written here because the specs left them open; Task 9 moves the durable
ones into `presentation.md`.

1. **A filter pair replaces the folder.** `IncidentFolder` (one state, or
   one category implying open) becomes `IncidentFilter { state:
   IncidentState?, category: Category? }`. Both nil is every incident;
   a category no longer implies open. Within each group the rows are a
   single-select toggle (the API takes one `state` and one `category`);
   across the groups they compose into one query. Clicking the active row
   clears it, as does the chip's remove control; the "All incidents" row
   is `state == nil`. The launch route `incidents` still opens
   `state: open` -- the home is triage -- and `incidents/<state>` still
   works; no route spells a category, which the toolbar composes by hand.
2. **The sidebar selection is the screen, filters are buttons.** The
   `List` selection carries only `RootScreen` rows (Incidents, Workloads,
   Status). State and category rows are check-marked buttons under
   `.selectionDisabled()`: activating one sets the filter and switches the
   screen to incidents, but the highlight stays on the Screens row,
   because the screen is where you are and the filter is what it shows
   (decision 7). The state list gains `job_finished`, which the folder
   sidebar never offered although the state exists.
3. **Chips say what is in force, including the default.** One removable
   chip per non-nil filter ("state: open", "category: crash"), each with
   the stored predicate on hover (`state = open` is derived, so the hover
   says the derivation source `incidents.closed_at / acknowledged_at`;
   the category hover is `incidents.category = 'crash'`). Removing the
   state chip is the all-states scope. The chips sit beside the Group
   control, which gets its visible "Group by" label back.
4. **Grouping modes are workload, namespace, category, cluster, time;
   flat goes.** Time is the flat list with UTC-day headings -- the daemon
   orders by `last_seen_at DESC`, so the headings are cuts in the served
   order and the key is the first ten characters of the stored timestamp
   (the layout is fixed-width UTC). A stored `flat` preference falls back
   to the default the existing `Grouping(rawValue:) ?? .workload` already
   provides. In workload mode with more than one cluster in scope, rows
   nest cluster above workload (roadmap deliverable); namespace mode
   already carries the cluster in its `(cluster, namespace)` key, and
   category and time modes are about neither.
5. **A group header earns its collapse.** Every section header in the
   grouped list is a disclosure; collapsed or open it shows the identity
   (kind and name, or the cluster with its dot), "N incidents, M open",
   the worst open category as a dot (worst by the sidebar's worst-first
   category order), and the newest `last_seen_at` as a clock time.
   Collapse state is per group id, per session; it is not a preference.
6. **An empty scope is a fact, not a reset.** `ClusterScope` becomes
   `{ all: Bool, selected: Set<String> }`: `all` is the default and what
   an empty stored selection migrates to; `!all && selected.isEmpty` is
   the empty scope, kept by `reconciled` instead of snapping back to all.
   An empty scope makes no list call -- `cluster_ids` empty means every
   cluster on the wire, the opposite fact -- and the incidents and
   workloads panes say "No cluster is in scope. Check a cluster in the
   sidebar." The scope pill reads "0 of N clusters". The single-cluster
   checkbox disable goes: unchecking the only cluster now means
   something. The menu bar keeps ignoring scope.
7. **Kind order is a model derivation.** `kindRank(_ kind: String) ->
   Int` in `IdiosModel/Display.swift`: Deployment 0, StatefulSet 1,
   DaemonSet 2, any other kind 3 (alphabetical among themselves), CronJob
   4, Job 5, `none` 6 -- the fixed order of decision 5, with a rank for a
   kind it has never heard of. Sorting is `(rank, kind, name)`. Empty
   kinds produce no caption because a caption is derived from rows.
8. **A kind caption is a heading, not a level.** Inside a namespace
   disclosure the kind captions are non-selectable secondary rows at the
   same indentation as the workload rows they head (decision: "no extra
   indentation level"). The last caption is "No controller" with the
   disambiguating hover ("pods no controller owns; each row is one pod").
   Under it, one row per pod showing the pod's name (`podNameSuffix`
   against an empty workload name is the full name), live rows before
   deleted (`livePods > 0` is the row's own liveness), then by name --
   the row carries no creation time, and adding one is contract growth
   this phase does not make. The row opens the pod screen.
9. **The controller-less workload detail goes with its reason.** Once a
   pod row opens the pod screen, `WorkloadsStore.loadUncontrolled`, the
   `pod_uid` branch of `loadIncidents`, the "(no controller)" title
   fallback and the four-segment `workload/` route are dead code and are
   deleted (decision 8: the capability moved; the pod screen already
   shows containers, incidents, events and files).
10. **The popover closes itself after navigating.** Every `MenuBarItem`
    action that calls `open(_:)` then dismisses the popover: first
    through the SwiftUI `dismiss` environment, and if that proves inert
    for a `.window`-style `MenuBarExtra` on macOS 15, by ordering out the
    item's own hosting `NSWindow` (the key window at click time, captured
    before `open` activates the main window). One of the two works; the
    task proves which by hand and keeps only that one.
11. **The popover names the cluster whenever there is more than one.**
    The menu bar ignores scope, so 7.2's "more than one cluster is
    selected" is `store.clusters.count > 1` here; each incident row leads
    with the cluster name, resolved from the store's own cluster list.
    Below the five newest, "N more open incidents" (openCount minus the
    rows shown, only when positive) is a row that opens the Open filter
    in the main window.
12. **Bare keys go through focus, never through key equivalents.** A
    `keyboardShortcut` with no modifiers fires while a person types in
    the filter field, so the single letters live in `.onKeyPress` on the
    focused incidents list: `a` acknowledge, `d` dismiss, `r` resolve,
    on the selected row; Return opens it (arrows come with `List`
    selection). Single click selects, double click or Return opens --
    the macOS convention the deliverable's "row selection" asks for.
    Cmd-modified keys are real menu items in a new Go menu: Cmd-1
    Incidents, Cmd-2 Workloads, Cmd-3 Status, Cmd-[ Back. The same three
    letters act on the incident detail screen through `.onKeyPress` on
    its root behind `.focusable()`; if macOS 15 focus proves unreliable
    there, the keys stay list-only and the handoff says so.
13. **The list writes live in `IncidentsStore`.** Acknowledge, dismiss
    and resolve from the list call the same three operations the detail
    store wraps, apply the returned row at once through the store's
    existing `apply`, and let the stream confirm; the store keeps the
    filter it is watching so `apply` can decide whether the row still
    belongs. All three are idempotent (Section 8), so a repeated key is
    harmless.
14. **The namespace picker gets a bound, a search and a select-all.**
    The checklist becomes a bordered, scrolling list of fixed height with
    a filter field above it, a "Select all" / "Select none" toggle acting
    on the filtered names, and a "N of M checked" count. `namesToAdd`
    and the free-text fallback are unchanged; both sheets share the one
    component as today.

## File structure

```
macos/Sources/IdiosModel/
  Cluster.swift                    ClusterScope: all, empty, reconciled
  Display.swift                    kindRank
macos/Tests/IdiosModelTests/
  ClusterTests.swift               the scope semantics
  DisplayTests.swift               the kind order
macos/idios/App/
  Route.swift                      browser route and BrowserPath deleted;
                                   4-segment workload route deleted;
                                   Navigator.goBack
  IdiosApp.swift                   the Go commands menu
  Preferences.swift                scope persistence gains the all flag;
                                   Grouping gains cluster and time, drops
                                   flat
macos/idios/Store/
  BrowserStore.swift               deleted
  IncidentsStore.swift             IncidentFilter replaces IncidentFolder;
                                   empty-scope short circuit; the three
                                   list writes
  WorkloadsStore.swift             empty-scope short circuit;
                                   loadUncontrolled and the pod_uid branch
                                   deleted
macos/idios/Views/Browser/         deleted
macos/idios/Views/Incidents/
  IncidentsSidebar.swift           screens as selection, filters as
                                   check-marked rows, all-states row
  IncidentsScreen.swift            filter state, chips, labelled group
                                   control, empty-scope view, selection,
                                   keys, back
  IncidentGrouping.swift           cluster nesting, time mode, header
                                   facts
  IncidentsList.swift              disclosure headers, List selection
macos/idios/Views/Workloads/
  WorkloadsScreen.swift            kind captions, pod rows, dead paths out
macos/idios/Views/MenuBar/
  MenuBarView.swift                dismiss on navigate, cluster names,
                                   the more row
macos/idios/Views/Clusters/
  NamespacePicker.swift            scroll, search, select all
macos/idios/Views/Pod/
  PodScreen.swift                  the browser-only container binding goes
hack/smoke/bare-pod.yaml           new: a pod no controller owns
docs/mockups/idios-ui.html         amended by each screen task (s1, s7)
docs/design/presentation.md        7.1 in Task 2, 9.3 rows in Task 9;
                                   the browser row goes in Task 1
docs/plans/m3-signal/roadmap.md    Task 9
CLAUDE.md                          the screenshot route list in Task 1
```

## Task 1: the Browser screen is removed

Spec: roadmap decision 8 and the Phase 13 deliverable ("the Browser
screen removed with its store and its route, unwired from the sidebar and
the screen switcher"); Phase 12's handoff ("The Browser screen is left
word for word for Phase 13's deletion: its own `readyColor` and two
accent colours are the only colour decisions outside `BadgeStyle.swift`").
Implementer: Sonnet (the compiler names every seam).

Today `BrowserScreen.swift` (332 lines) and `BrowserStore.swift` (177)
duplicate three screens: its columns are the workloads tree, its pod pane
is the pod screen, its filters are the Pods tab chips. `RootScreen.browser`
sits in the sidebar, `Route.browser(BrowserPath)` in the parser,
`browserPath`/`browserPane` in `IncidentsScreen`, and `PodScreen` carries a
`container:` binding parameter that only the browser passes.

- Delete `macos/idios/Views/Browser/` and
  `macos/idios/Store/BrowserStore.swift` (the synchronized folder drops
  them from the target by ceasing to exist).
- `Route.swift`: delete `case browser`, the `BrowserPath` struct and the
  `"browser"` parse case.
- `IncidentsSidebar.swift`: delete `RootScreen.browser` with its title
  and symbol.
- `IncidentsScreen.swift`: delete `browserPath`, `browserPane`, and the
  browser arms of `init`, `show` and `destination`.
- `PodScreen.swift`: delete the `container:` parameter and whatever
  plumbing existed only for it; the pod screen owns its selection again.
- Anything the deletion strands (`PodFilter` was private to the screen
  and goes with it; check `workloadTitle`, `kindStyle`, `podBadge` for
  remaining callers before touching them -- the tree and the workload
  detail still use them).
- `docs/design/presentation.md`: delete the "Column browser" row from
  9.3's screens table (code-is-truth: the doc must not describe a screen
  that does not exist).
- `CLAUDE.md`: drop `browser/...` from the screenshot route list.
- `docs/mockups/idios-ui.html` already has no browser section; verify,
  do not edit.

No test: nothing new to prove; the deletion is proven by the build and by
`swift test` staying green.

Screenshot review: `hack/macos/screenshot.sh incidents <png>` against the
smoke daemon -- the sidebar's Screens section reads Incidents, Workloads,
Status and nothing else.

Checkpoint: `go build ./... && go test ./... && make ascii && make
app-test && make app`. Commit `macos: remove the browser screen`.

Consumes: the compiler.
Produces: a three-screen `RootScreen`; a `Route` with no browser; the
sidebar and switcher Phase 13 rebuilds without dragging a corpse.

## Task 2: an empty cluster scope means empty

Spec: `presentation.md` 7.1 (amended by this task: today it says "if it
becomes empty it resets to all"); roadmap deliverable "an empty cluster
scope that means empty and says so"; `presentation.md` 11 item 5 (the
cluster scope rules are model-tested). Implementer: Opus (a semantic
change that ripples through persistence, two stores and three screens).

Today `ClusterScope.selected` empty means every cluster: the sidebar
checkbox refuses to uncheck the last cluster (disabled below two, reset
to empty-means-all at all-checked), `reconciled` maps an emptied
selection back to all, and an empty selection cannot be told apart from
the all default in `UserDefaults`. On the wire the same pun exists --
`cluster_ids` absent means all -- so an honest empty scope must never
make the call.

- `IdiosModel/Cluster.swift`: `ClusterScope` becomes `{ all: Bool,
  selected: Set<String> }` with `static let all`, `init(selected:)` for
  an explicit selection, `isAll`, `isEmpty` (`!all && selected.isEmpty`),
  `includes` (all or member), and `reconciled` intersecting with the
  known ids while keeping `all` as `all` and keeping empty empty.
- `Preferences.swift`: persist the flag under a new key beside the
  existing array. Migration is the absent flag: no stored flag and an
  empty stored array is the old default and reads as all, so an existing
  install changes nothing.
- `IncidentsSidebar.swift`: the checkbox starts from
  `scope.isAll ? all ids : scope.selected` as today, but unchecking the
  last cluster produces the empty scope instead of all, checking every
  cluster produces `.all`, and the below-two disable and its help text
  go.
- `IncidentsStore.watch` and `WorkloadsStore.load`: when
  `scope.isEmpty`, clear the rows (and the counts, for the sidebar) and
  return without calling; the task ids already carry the scope, so a
  re-check restarts the watch.
- `IncidentsScreen`: when the scope is empty the incidents pane shows
  "No cluster is in scope. Check a cluster in the sidebar." in the empty
  view's voice; the scope pill reads "0 of N clusters". `WorkloadsScreen`
  says the same sentence in its tree pane.
- `presentation.md` 7.1: the reset sentence becomes the new fact: a
  selection that loses its last cluster is empty, the screens say so,
  and only checking a cluster (or every cluster) fills them again.

Tests (`ClusterTests.swift`, whole-value):

- The scope table gains rows: the all scope includes an arbitrary id;
  an explicit selection includes its members and nothing else; the empty
  scope includes nothing; `reconciled` keeps all as all, drops unknown
  ids, and keeps an emptied selection empty. Trace: 7.1 as amended.

Screenshot review: `hack/macos/screenshot.sh incidents <png>` for the
healthy state; the empty state is a hand check (uncheck the smoke
cluster: the list empties with the sentence, the pill says "0 of 1
clusters", the workloads tree says the same, and re-checking restores
both).

Checkpoint, commit `macos: let an empty cluster scope mean empty`.

Consumes: `ClusterScope` call sites (sidebar, both stores, pill).
Produces: `ClusterScope.all` / `.isEmpty`, the persistence flag, the
empty-scope sentence; Task 3 builds the sidebar on these semantics.

## Task 3: state and category become filters that compose

Spec: roadmap decision 7 and the deliverable "the sidebar of decision 7,
where the selection is the screen, state and category are check-marked
filters that compose, an all-states row exists, and the active filter
shows as removable chips in the toolbar"; `presentation.md` 4.2 (the
`state` and `category` query parameters are independent predicates).
Implementer: Opus (the selection model of the home screen).

Today the sidebar is folders: one `IncidentFolder` is selected at a time,
a category folder implies `state=open`, there is no all-states view, no
`job_finished` folder although the state exists, and the only sign of
what the list holds is the window title. The `List` selection carries
both screens and folders, so choosing a folder repaints the screen rows.

- `IncidentsStore.swift`: `IncidentFolder` becomes `IncidentFilter {
  var state: IncidentState?; var category: Category? }` (decision 1 of
  this plan). `matches` tests the two independently; `query` passes both
  raw values when set; `title` keeps the current reader phrasings per
  state ("Open incidents", "Marked resolved", ...), says "All incidents"
  for the empty filter, composes "Open crash incidents" when both are
  set, and says "crash incidents, every state" for a category alone.
- `IncidentsSidebar.swift`: the `List` selection carries `RootScreen`
  only. The "Incidents" section becomes "State": an "All incidents" row
  then the seven states in triage order (open, acknowledged, recovered,
  pod deleted, job finished, manual, dismissed -- `job_finished` joins).
  The "Category" section keeps its ten rows, worst first, labels from
  `Category.label` with the raw value on hover as Phase 12 left them.
  Filter rows are buttons under `.selectionDisabled()`: a tap sets its
  half of the filter (toggling off when already active), switches the
  screen to incidents and clears the pushed path; the active row draws a
  leading check mark where the state square or category dot sits today,
  with the glyph moving beside it. Counts stay: `byState` per state row,
  `byCategory` (open only, as the endpoint counts) per category row with
  that fact on hover.
- `IncidentsScreen.swift`: `folder` becomes `filter: IncidentFilter`;
  the launch and `show` route arms map `.incidents(nil)` to
  `state: .open` and `.incidents(state)` to that state, category nil.
  The toolbar gains one removable chip per set filter half, each an
  HStack of the label and an x button, with the stored predicate on
  hover (decision 3 of this plan); removing the state chip is the
  all-states filter. The `WatchKey` carries the filter. The navigation
  title is `filter.title`.
- `docs/mockups/idios-ui.html` s1: the sidebar panel and the page note
  are amended to the filter model (check marks, all-states row, chips).

No new Swift unit test: the filter type lives in the app target, which
`swift test` does not reach by design (`presentation.md` 11 item 6); the
composition is verified on screen against the daemon's own filtering.

Screenshot review, against the smoke daemon:
`hack/macos/screenshot.sh incidents <png>` (Open default: state chip
"state: open", check mark on Open, screens selection on Incidents) and
`incidents/dismissed <png>` (the state chip follows the route). By hand:
compose dismissed + crash and read the list narrow to both; remove the
state chip and read "crash incidents, every state"; select All incidents
and read every state at once.

Checkpoint, commit `macos: make sidebar states and categories composing
filters`.

Consumes: Task 2's scope semantics; Phase 12's `Category.label`,
`clusterDot`, the hover rules.
Produces: `IncidentFilter`, the chips, the all-states row; Task 4's
grouping and Task 8's keys act on this list.

## Task 4: grouping that survives a second cluster

Spec: roadmap deliverable "incidents grouped by cluster above workload
when more than one cluster is in scope, with cluster and time as explicit
modes and a group header that says enough to stay collapsed";
`presentation.md` 7.2 (grouping by namespace groups on `(cluster_id,
namespace)`, already true today). Implementer: Opus (the list's
structure).

Today `incidentGroups` returns one flat array of sections; a second
cluster only reaches the header as a `cluster / namespace` meta string in
workload mode; there is no cluster mode and no time mode but there is a
flat mode that says nothing the time mode would not say better; headers
cannot collapse, and a category header shows its label without the raw
value on hover (a Phase 12 loose end this rewrite absorbs).

- `Preferences.swift`: `Grouping` becomes `workload, namespace,
  category, cluster, time`; `flat` goes, and a stored `flat` falls back
  to `.workload` through the existing `?? .workload`.
- `IncidentGrouping.swift`: the result becomes two-level --
  `[IncidentClusterGroup]`, each `{ id, title, dotStyle, groups:
  [IncidentGroup] }`. Workload mode with more than one cluster in scope
  buckets by cluster first (order: cluster name), then by workload
  within; with one cluster in scope, or in every other mode, there is a
  single unnamed cluster bucket, so the list code has one shape. Cluster
  mode groups by cluster alone (rows flat under the cluster header).
  Time mode keys on the first ten characters of `lastSeenAt.raw` (the
  stored layout is fixed-width UTC), title the date with "UTC" as meta,
  keeping the served newest-first order. Category headers gain
  `.help(rawValue)`.
- `IncidentGroup` gains the header facts: `openCount`, `worstCategory`
  (worst by the sidebar's worst-first order among the group's open
  rows), `newestSeen` -- derived once in `incidentGroups`, not in the
  view.
- `IncidentsList.swift`: cluster headers are rows with
  `clusterDot(ready:hasError:)` when the cluster is known (the closure
  the screen already passes resolves names; extend it to hand the
  cluster row so the dot has its facts). Every group section becomes a
  disclosure: the header shows the existing identity plus "N incidents,
  M open", the worst-category dot, and `clockTime(newestSeen)`;
  collapsing keeps the header visible and the rows folded. Collapse
  state is a `Set<String>` of group ids held in `IncidentsScreen` beside
  the tree state, reset never (session-scoped, like the tree's).
- `IncidentsScreen.swift`: the Group control gets its label back --
  "Group by" as visible text beside the menu picker (the `.menu` style
  swallows the `Picker` label) -- and hands the new modes to the picker.
- `docs/mockups/idios-ui.html` s1: the grouping note names the five
  modes and the collapsible headers.

No new Swift unit test: grouping lives in the app target (11 item 6);
the daemon's own two-cluster fixtures are the check.

Screenshot review, against `./bin/idios -data-dir .storage mock` with
`IDIOS_DAEMON` unset (mock listens on 7770): `hack/macos/screenshot.sh
incidents <png>` in workload mode -- two cluster headers with dots, the
workload groups nested under them, each header carrying its counts and
newest time. By hand: collapse a group and read the header still saying
enough; switch to cluster and time modes and read both; switch to the
smoke daemon and read workload mode with no cluster level.

Checkpoint, commit `macos: group incidents by cluster and time`.

Consumes: Task 3's filter list; `clusterDot`; `clockTime`.
Produces: the two-level grouping, the disclosure headers, the five
modes; Task 8's selection walks this list.

## Task 5: the tree groups a namespace by kind

Spec: roadmap decisions 5 and 6 and the deliverable "a workloads tree
that groups a namespace by kind in the order of decision 5, with roll-up
counts and no extra indentation level, where a pod no controller owns is
a row of its own under the last heading and opens the pod, not a
workload"; mockup s5's note (already amended to this shape);
`presentation.md` 7.3 (pods are addressed by uid), 9.2 (display
derivations live in the model layer). Implementer: Opus (the tree, the
route and a store path deleted at once).

Today a namespace lists every workload alphabetically, so the two-minute
CronJobs sort above the Deployment a person came for; the pods no
controller owns are one `(no controller)` row per pod that sorts first
carrying the least; that row opens a synthesized workload detail
(`loadUncontrolled`) whose only content is the pod list the pod screen
already shows better; `adoptSelection` has a hack to skip those rows on
entry; and the four-segment `workload/` route exists only to address
them.

- `IdiosModel/Display.swift`: `kindRank(_ kind: String) -> Int` per
  decision 7 of this plan (Deployment 0, StatefulSet 1, DaemonSet 2,
  other 3, CronJob 4, Job 5, none 6).
- `WorkloadsScreen.swift`: `NamespaceGroup` gains kind buckets sorted by
  `(kindRank, kind, name)`. Rendering inside the namespace disclosure:
  a caption row per non-empty kind (secondary small-caps text at the row
  indentation, count of the kind's open incidents at the trailing edge),
  then its rows. The `none` caption reads "No controller" with the hover
  "pods no controller owns; each row is one pod". A kind-none row shows
  the pod name in the row's monospaced style, live rows before deleted
  (`livePods > 0`) then by name, and its tap calls `openPod(podUID)`
  rather than selecting a tree row; a row missing `podUid` (a daemon
  older than Phase 11) is drawn but disabled with "the daemon did not
  say which pod this is" on hover rather than a dead click.
- The dead paths go: `loadUncontrolled` and its `WorkloadDetail`
  synthesis, the `pod_uid` branch of `loadIncidents`, the
  `"(no controller)"` branch of `workloadTitle` (its callers now never
  see an empty name), `adoptSelection`'s skip-the-bare-pods fallback
  (plain `rows.first`), and `Route`'s four-segment `workload` case with
  its comment. `WorkloadKey` selection now only ever names a real
  workload.
- `hack/smoke/bare-pod.yaml`: one pod with no owner (a `sleep`
  container, the smoke namespace, tight resources) so the heading has a
  row on every later screenshot; `hack/smoke/run.sh` already applies the
  directory.
- `docs/mockups/idios-ui.html` s5: verify against the note (already
  amended when the decision was taken); amend the drawn tree panel if it
  still shows the alphabetical order.

Tests (`DisplayTests.swift`, table-driven):

- `kindRank` ordering: the fixed six, an operator kind (`Cluster`)
  landing between DaemonSet and CronJob, two unknown kinds ordering
  alphabetically, `none` last. One table comparing a shuffled list
  sorted by `(rank, kind)` against the exact expected order. Trace:
  roadmap decision 5 ("the order must have a place for a kind it has
  never heard of").

Screenshot review, against the smoke daemon after `make smoke` with the
bare pod applied: `hack/macos/screenshot.sh workloads <png>` -- captions
in the order Deployment, CronJob, No controller (the smoke set has those
three), roll-up counts intact on cluster and namespace, the bare pod one
row under the last caption. By hand: click the pod row and land on the
pod screen; filter for the pod name and see its group open.

Checkpoint, commit `macos: group the workload tree by kind`.

Consumes: Task 1's browser-free `workloadTitle`/`kindStyle`; Phase 12's
tabs and `WorkloadsStore` bounds (untouched); `podNameSuffix`.
Produces: `kindRank`; a tree whose rows are workloads or pods; a smoke
set with a controller-less pod.

## Task 6: the menu bar popover navigates and closes

Spec: roadmap deliverable "a menu bar popover that closes after it
navigates, names the cluster on each row, and says how many more there
are"; `presentation.md` 4.7 (the popover's two endpoints), 7.2 (the
cluster prefix rule). Implementer: Sonnet (decisions 10 and 11 of this
plan say exactly what to build; the dismiss fallback is spelled out).

Today a click opens the main window and leaves the popover hanging over
it until the person clicks elsewhere; a row says `workload / container -
category` with no cluster, so the same-named Deployment in two clusters
is one ambiguous line; and the popover shows five rows and the count in
the header, leaving "are there more than five" to arithmetic.

- `MenuBarView.swift`: every `MenuBarItem` action that navigates runs
  `open(route)` then closes the popover per decision 10 (the `dismiss`
  environment first; the captured hosting window ordered out if
  `dismiss` is inert in a `.window` `MenuBarExtra` on macOS 15 -- prove
  by hand, keep one).
- `incidentRow`: when `store.clusters.count > 1`, the row leads with the
  cluster name (from the store's own list, id resolved as the screens
  do) before the workload, and the hover carries the full line as today.
- Below the newest rows: when `store.openCount` exceeds the rows shown,
  a `MenuBarItem` reading "N more open incidents" opens `.incidents(nil)`
  (the Open filter) in the main window and closes like the rest.
- `docs/mockups/idios-ui.html` s7: the popover panel gains the cluster
  prefix and the more row.

No Swift unit test: view behaviour, a person's check.

Screenshot review: `hack/macos/screenshot.sh menubar <png>` against the
mock daemon (two clusters, so the prefix shows; its open count exceeds
five if the fixtures allow -- otherwise the more row is checked against
smoke with six incidents open). By hand, against the real status item:
click a row, watch the main window come up on the incident and the
popover close.

Checkpoint, commit `macos: close and label the menu bar popover`.

Consumes: `MenuBarStore.openCount/newest/clusters`; `Route`.
Produces: a popover that gets out of the way; the cluster-named rows.

## Task 7: the namespace picker scrolls, searches and selects all

Spec: roadmap deliverable "a namespace picker that scrolls, searches and
selects all"; `presentation.md` 9.3's add-cluster row (contexts from the
daemon, namespaces from the cluster with the free-text fallback).
Implementer: Sonnet (one component, decision 14 says the shape).

Today `NamespacePicker` is an unbounded `VStack` of checkboxes: a
cluster with sixty namespaces makes the add-cluster sheet taller than
the screen, there is no way to find one name but reading, and watching
most of a cluster means clicking most of sixty boxes.

- `NamespacePicker.swift`: a filter field ("Filter namespaces") above a
  bordered scrolling list of fixed height (about 180pt, enough for eight
  rows); the checkboxes render the filtered names only. A header line
  carries "N of M checked" and one button reading "Select all" when any
  filtered name is unchecked, else "Select none", acting on the filtered
  set (so a search then select-all checks exactly the matches).
  `namesToAdd` and both call sites are unchanged; the free-text field
  stays the fallback for names the Role cannot list.

No Swift unit test: `namesToAdd` keeps its behaviour and its existing
coverage; the new surface is layout and a person's check.

Screenshot review: `hack/macos/screenshot.sh addcluster <png>` against
the smoke daemon (the sheet is the key window and is what the capture
photographs) -- the picker is bounded, the count line reads true, and
the sheet fits the screen. By hand: type a fragment, Select all, clear
the filter, read the count carrying the checks made under the filter.

Checkpoint, commit `macos: let the namespace picker scroll, search and
select all`.

Consumes: `NamespacePicker`, both sheets.
Produces: the bounded picker.

## Task 8: the first keyboard layer

Spec: roadmap deliverable "the first keyboard layer (row selection with
arrows and Return, one key each for acknowledge, dismiss and resolve,
back, and the screens)"; `presentation.md` Section 8 (the three writes
are idempotent, so a repeated key is harmless); decision 12 and 13 of
this plan. Implementer: Opus (focus and key routing are version-specific
and the failure mode is silent).

Today the incidents list opens a row on single click through
`onTapGesture` and holds no selection, so there is nothing for an arrow
key to move; the three actions exist only as buttons on the detail
screen; nothing navigates between screens or back but the mouse; and
the only shortcuts in the application are Cmd-K (filter), Cmd-Shift-I
(menu bar) and the sheets' Return/Escape.

- `IncidentsList.swift`: the rows become a `List(selection:)` over the
  incident id; single click selects, double click and Return open
  (`.contextMenu(forSelectionType:primaryAction:)` is the native pair
  for it), arrow keys move the selection natively. The selection lives
  in `IncidentsScreen` beside the filter; opening on Return pushes the
  same route the tap pushed.
- Bare keys per decision 12: `.onKeyPress` on the list for `a`, `d`,
  `r`, calling the new `IncidentsStore.acknowledge/dismiss/resolve`
  with the selected id; the presses land only while the list has focus,
  so the filter field keeps its letters. No key deletes: delete is
  destructive and keeps its confirmation-guarded button.
- `IncidentsStore.swift` per decision 13: the three writes wrap the
  same operations the detail store calls (`AcknowledgeIncident`,
  `DismissIncident`, `ResolveIncident`), unwrap the returned row and
  hand it to `apply` under the filter and scope the store keeps from
  `watch`; `.cancelled` is ignored as everywhere.
- `IdiosApp.swift`: a `CommandMenu("Go")` -- Incidents Cmd-1, Workloads
  Cmd-2, Status Cmd-3 through `navigator.open`, a divider, Back Cmd-[
  through a new `Navigator.goBack()` (a serial the screen observes,
  popping one route when the path is not empty). The menu makes the
  keys discoverable, which bare letters on a list cannot be.
- `IncidentDetailScreen.swift`: the same three letters through
  `.onKeyPress` on the screen root behind `.focusable()` with the focus
  effect disabled, calling the detail store's existing methods. If
  macOS 15 will not keep focus on the root without stealing it from the
  log pane and the note sheet, this half is dropped, the keys stay
  list-only, and the handoff records it (the buttons remain the detail's
  path either way).
- `docs/mockups/idios-ui.html` s1: the page note names the keys.

No Swift unit test: focus and key routing are the view layer, a
person's check (11 item 6); the writes reuse operations whose behaviour
is covered on the Go side.

Screenshot review: `hack/macos/screenshot.sh incidents <png>` (the
selection highlight on the first row proves the list is selectable).
By hand, the real check: arrows walk the rows; Return opens and Cmd-[
comes back with the selection kept; `a` acknowledges the selected row
(the row restyles from the returned row at once); `d` dismisses; `r`
resolves; typing `a` into the filter field filters instead of
acknowledging; Cmd-1/2/3 switch screens with the menu items visible in
the Go menu.

Checkpoint, commit `macos: add the first keyboard layer`.

Consumes: Task 3's filter list, Task 4's grouped sections, the detail
store's writes, `Navigator`.
Produces: list selection, the three store writes, the Go menu,
`Navigator.goBack`.

## Task 9: docs and roadmap

- `docs/design/presentation.md`: 9.3's incidents row gains the filter
  pair, the chips, the five grouping modes and the collapsible headers;
  the workloads row gains the kind captions and the pod rows that open
  the pod; the menu bar row gains the cluster prefix, the more row and
  the close-on-navigate; a sentence beside 9.4's component notes records
  the keyboard layer and decision 12's bare-keys-through-focus rule.
  7.1 was amended in Task 2 and the browser row deleted in Task 1;
  verify both read as one document.
- `docs/mockups/idios-ui.html` was amended per screen task; verify
  nothing contradicts the doc.
- `CLAUDE.md`: the route list was trimmed in Task 1. Add a durable
  gotcha only if a task uncovered one (candidates: the bare
  `keyboardShortcut` firing inside text fields, the `MenuBarExtra`
  dismissal path that actually worked).
- `docs/plans/m3-signal/roadmap.md`: Phase 13 status complete, with
  one-line deviations if any task was cut down; the milestone's phases
  are then all complete, and the merge to `main` waits for the user's
  agreement in that session, as the roadmap requires.
- This plan gains its closing handoff section ("Hands to the merge"):
  what the next milestone inherits -- the Phase 12 loose ends no Phase
  13 task absorbed (the job-subject title sentence, the pod rail's
  column-name labels, the stat cards' missing hovers, the gap histogram
  and worst-state cells drawing `rawValue`, `SiblingsCard`'s orange
  Succeeded, the workload Incidents tab's missing `limit`, `idios mock`
  not serving the job detail fixture), whatever Task 8 dropped, and
  whatever contradicted the roadmap.

Checkpoint (`make ascii`), commit `docs: close phase 13`.

## Self-review

Spec coverage. Every roadmap Phase 13 deliverable has one task: the
sidebar of decision 7 with the all-states row and the toolbar chips ->
T3; the labelled group control -> T4 (it lives beside the modes the task
builds); the workloads tree by kind with roll-up counts, no extra
indentation and pod rows that open the pod -> T5; cluster-above-workload
grouping, the cluster and time modes and headers that stay collapsed ->
T4; the menu bar popover's three fixes -> T6; the empty cluster scope ->
T2; the namespace picker -> T7; the keyboard layer -> T8; the Browser
deletion, unwired from sidebar and switcher with its store and route ->
T1. The roadmap's "its capability is not rebuilt elsewhere" is T5's pod
rows plus the deletion of the synthesized controller-less detail
(decision 9). The phase depends on Phase 11 only through the bounded
Pods tab, which Phase 12 already built; nothing here touches the
contract.

Ordering and the shared file. T1 shrinks the seams before anything is
rebuilt. T2 changes the scope semantics T3's sidebar builds on. T3
before T4 because the chips and the group control share the toolbar; T4
before T8 because the keys walk the grouped list. T5, T6, T7 are
independent of T3/T4 and of each other; they run in plan order for
review's sake. `IncidentsScreen.swift` is touched by T2, T3, T4 and T8
in sequence, never concurrently.

`.ai` rules. ASCII at every checkpoint. Tests: two new test surfaces
only -- the scope semantics (T2, tracing to 7.1 as amended, the rule
`presentation.md` 11 item 5 already assigns to the model suite) and
`kindRank` (T5, tracing to decision 5's "a kind it has never heard of"),
both table-driven with exact whole-value assertions; everything else is
the view layer, which is a person's check by the spec's own testing
order, and no test is written for it. Comments: the reasons named in
the decisions (why bare keys avoid key equivalents, why an empty scope
must not call, why the pod row has no timestamp to sort by, why flat
went) are the only ones. Scope: no daemon change, no contract change,
no schema change; the new files are one smoke YAML and nothing else;
deletions are named per task and each deletes only what its change
strands. Commits: one per task, `area: imperative subject`, never red.
Code is truth: T1 and T2 fix the two spec sentences their code changes
falsify in the same commit as the change; no code references this plan.

Type consistency. `IncidentFilter` is the one filter type: the store's
`watch`/`matches`/`query`, the screen's state, the `WatchKey` and the
sidebar rows all carry it (T3), and T8's writes reuse the store's kept
copy. `ClusterScope.isEmpty` is the one empty test: both stores and
both empty-state views call it (T2). `kindRank` sorts `(rank, kind,
name)` in T5's one comparator. The grouping result is
`[IncidentClusterGroup]` with a single unnamed bucket in the
one-cluster case, so `IncidentsList` has one rendering path (T4).

Risks named. The two version-specific pieces are T8's focus routing
(the detail-screen half is explicitly droppable to list-only, recorded
in the handoff if dropped) and T6's popover dismissal (two mechanisms
named, one kept after a hand test). T4's two-cluster review depends on
`idios mock`, whose fixtures carry clusters 1 and 2 today; if the mock's
open count never exceeds five, T6's more row is reviewed against smoke
instead, and the task says so. T5 deletes a route the screenshot script
could once open (`workload/` with four segments); nothing in the repo
calls it, and CLAUDE.md's route list never spelled it. Changing single
click from open to select (T8) is the one behaviour change a person
might feel as a regression; it is the deliverable's own words ("row
selection with arrows and Return"), and double click and Return both
open. If the phase runs long, T7 is the one task with no dependent
(droppable without a dangling seam), and T8's detail-screen keys are the
one sub-scope already marked cuttable; the roadmap would get that
sentence either way.

## Hands to the merge

Every task landed; nothing was cut. What the merge review and milestone 4
inherit:

Hand checks still open (everything else was read from screenshots or
proven by hand during the phase): the incidents list's keys (arrows,
Return, `a`/`d`/`r` on the selected row, and that typing `a` into the
filter field filters instead of acknowledging); the detail screen's
`a`/`d`/`r`, whose `.focusable()` root is compile-proven only -- if focus
fights the log pane's text selection or the note sheet, the four
`onKeyPress` lines in `IncidentDetailScreen.swift` are the whole half and
come out cleanly; Cmd-1/2/3 and Cmd-[ from the Go menu; the namespace
picker's filter and select-all inside the add-cluster sheet (the
automated capture cannot select a context); composing a state and a
category filter by hand. The menu bar popover's dismissal was proven by
hand in Task 6 (`dismiss()` works in a `.window` `MenuBarExtra` on
macOS 15; the screenshot route is guarded and still captures).

Single click on an incident row now selects, double click or Return
opens. The user reserved the right to revert this if it proves
inconvenient; the change is `IncidentsList`'s selection wiring and
`openIncident` in `IncidentsScreen.swift`.

Fixes found by review during the phase, all committed: a `Preferences`
migration that would have read an existing narrowed scope as all
(`clusterScopeAll` absent takes its meaning from the stored array); the
workloads detail pane claiming "no workload has been seen" on an empty
scope; the `Workload.id` collision that collapsed every bare-pod tree
row into one (now keyed on the pod uid, with the model test); the tree
filter not matching pod names; the CronJob row suffix made redundant by
its kind caption.

Loose ends inherited from Phase 12, still owned by no task (milestone 4
candidates): a job-subject incident's title sentence still reads "Pod
<id> is <reason>"; the pod rail keeps its column-name labels; the
workload Overview stat cards carry no provenance hover; the Status gap
histogram and the workload pods tab's worst-state cell still draw
`rawValue`; `SiblingsCard` colours a Succeeded sibling orange;
the workload Incidents tab sends no `limit`; `idios mock` does not serve
`incident_detail_job.json`.

New loose ends from this phase: a deleted bare pod's tree row is not
visually dimmed (order alone separates live from deleted); the mock
fixtures' sidebar counts disagree with their incident list because each
fixture file is static (cosmetic, mock only); the incidents list's
selection does not follow a row that the stream moves or removes.

The merge itself: every commit of the milestone is on `m3-signal`,
one per task; nothing merges to `main` until the user agrees in that
session, per the roadmap.
