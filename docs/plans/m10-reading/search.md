# m10 step C - search: the pod_name filter and the Cmd-K palette

Goal: after this plan a person reaches anything by name. Cmd-K opens a
palette over whatever screen is up; typing `#187`, a pod suffix, a Job
name, a workload, a namespace, a container, a reason, an image tag or a
node lists what matches, sectioned as Pods, Runs, Workloads, Incidents
and Commands; `ns:`, `node:`, `tag:` and `reason:` narrow by field;
Return opens the hit, Cmd-Return opens it in Workloads, Shift-Return
copies its uid; an empty query lists the recent pages and every command
with its key, which is how the keyboard layer becomes visible. Matching
is client-side over the rows the stores already hold, and one daemon
change makes it exact at any size: an exact `pod_name` filter on
`GET /incidents` and `GET /pods`, served by the index `pods (cluster_id,
namespace, name)` already has. "/" keeps focusing the filter field;
search jumps, filter narrows.

Architecture: the daemon gains one query filter in `internal/query`
(`IncidentFilter.PodName`, `PodFilter.PodName`), one proto field per
request message, and nothing else; `make generate` rebuilds
`internal/apigen` and `api/openapi`, and the Swift client regenerates
from the OpenAPI document at build time. The reading of a query and the
ranking of hits are pure functions in `IdiosModel` (`Search.swift`) with
table tests: `SearchQuery` parses the text and its prefixes,
`searchResults` matches the rows of every section, ranks exact before
prefix before substring, caps a section and lists the commands. In the
application a `PaletteState` opened by the Go menu's Cmd-K item is
injected as an environment object; a `SearchStore` owns the live pod
rows, the daemon lookups for an id or an exact pod name the list limit
cut off, and the recent pages; `SearchPalette` draws the sections over
the screen as an overlay of `IncidentsScreen`, which performs the hit's
action with the openers it already has (`show`, `openIncident`,
`openWorkloadPods`, `openWorkloadRuns`). Views never import `IdiosAPI`;
stores own every call.

Tech stack: Go 1.26 daemon (`internal/query`, `internal/api`, protos
under `api/proto/idios/v1`, `buf generate` through `make generate`);
Swift 6 package `macos/Sources/IdiosModel` with tests in
`macos/Tests/IdiosModelTests` (`make app-test`); the Xcode application
(`make app`; a file under `macos/idios/` joins the target by existing).
SwiftUI `overlay` for the palette, `TextField` with `.onKeyPress` for
its keys, `CommandMenu("Go")` for the menu item, `NSPasteboard` for the
copy. Screenshots through a private daemon on 7771 with the application
launched with `-daemon 127.0.0.1:7771` and the new `-search <query>`
launch argument.

Spec: `docs/design/presentation.md` section 4.2 (`GET /v1/incidents`,
the `pod_name` sentence), 4.3 (`GET /v1/pods`, the `pod_name` sentence),
the Incidents row of 9.3 from "Search jumps and filter narrows" to the
end of that sentence, 9.4's keyboard paragraph (the Go menu holds Cmd-K
Search; the palette's empty query lists every command with its key).
Roadmap decision 6 and 10 of `docs/plans/m10-reading/roadmap.md`. Visual
reference: frame 1b of page 1 in `docs/mockups/idios-ui.html`.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Swift follows the Go rules: one-line doc comment per
public type and function, comments say why, tests trace to a spec
statement with whole-value assertions, variants are table rows. Views
never import `IdiosAPI`; stores own every client call; a cancelled call
maps to `APIError.cancelled` and every store ignores it. The only
hard-coded colours live in `BadgeStyle.swift`; the palette uses system
colours and the badges of step B. A bare-letter key goes through
`.onKeyPress` on the focused view, never a bare `keyboardShortcut`;
Cmd-K is a menu key equivalent because a menu is where it is
discovered. Read-model SQL lives in `internal/query`, one file per row
set; a new filter is one more pair in the equality loop, not new SQL
elsewhere. Generated code under `internal/apigen` and `api/openapi` is
never edited; `make generate-check` only passes on a committed tree, so
it runs after task 1's commit. `rm -rf macos/.build` before the first
`make app-test` after the proto change. Nothing in a test, comment, doc
or fixture names a real organisation, cluster, namespace, workload,
image or node; the invented names are the fixtures' (`checkout-api`,
`report`, `idios-smoke`, `api`, `node-a`) and step B's (`nightly`,
`worker`). The production daemon on 127.0.0.1:7770 is never touched,
read or screenshotted; every check runs against 7771. Checkpoint before
every commit: `go build ./... && go test ./... && make ascii && make
app-test && make app` (plus `make generate-check` after task 1's
commit). Implementers never commit; nothing is committed without the
user's review of the diff. The decisions are made; where the frame and
the sentence differ, the sentence of 9.3 wins and the doubt goes in the
self-review.

## File structure

    api/proto/idios/v1/incidents.proto                 ListIncidentsRequest.pod_name = 11 (task 1)
    api/proto/idios/v1/pods.proto                      ListPodsRequest.pod_name = 7 (task 1)
    internal/query/incidents.go                        IncidentFilter.PodName; one equality pair (task 1)
    internal/query/pods.go                             PodFilter.PodName; one equality pair (task 1)
    internal/query/incidents_test.go                   two rows in TestListIncidentsFilters (task 1)
    internal/query/pods_test.go                        two rows in TestListPodsFiltersAndCounts (task 1)
    internal/api/incidents.go                          req.GetPodName() into the filter (task 1)
    internal/api/pods.go                               req.GetPodName() into the filter (task 1)
    internal/apigen/..., api/openapi/...               regenerated, never edited (task 1)
    macos/Sources/IdiosModel/Search.swift              SearchQuery, SearchSection, SearchHit, SearchCommand, SearchAction, SearchSectionHits, searchResults, commandList (task 2)
    macos/Tests/IdiosModelTests/SearchTests.swift      the parse, the sections, the prefixes, the ranking, the cap, the empty query (task 2)
    macos/idios/App/PaletteState.swift                 PaletteState: isPresented, the -search launch argument (task 3)
    macos/idios/App/IdiosApp.swift                     the Go menu's Search item, Cmd-K; the environment object (task 3)
    macos/idios/Store/SearchStore.swift                live pods, the id and pod_name lookups, the recent pages (task 3)
    macos/idios/Views/Search/SearchPalette.swift       the field, the sections, the keys, the footer (task 4)
    macos/idios/Views/Search/SearchHitRow.swift        one hit of each section (task 4)
    macos/idios/Views/Incidents/IncidentsScreen.swift  the overlay, perform(action), the recent pages recorded (task 4)
    docs/plans/m10-reading/roadmap.md                  step C's status line (task 5)

## Task 1 - the pod_name filter in the daemon

`api/proto/idios/v1/incidents.proto`, `ListIncidentsRequest` (fields 1
to 10 are taken, `node_name` is 10):

    // pod_name is an exact match on the pod's name, so a client holding a
    // name from a truncated list still reaches its rows.
    string pod_name = 11 [(sebuf.http.query) = {}];

`api/proto/idios/v1/pods.proto`, `ListPodsRequest` (fields 1 to 6 are
taken, `limit` is 6):

    // pod_name is an exact match on name, so a client holding a name from a
    // truncated list still reaches its row.
    string pod_name = 7 [(sebuf.http.query) = {}];

`internal/query/incidents.go`: `IncidentFilter` gains `PodName string`
with the doc line "PodName keeps the rows of the pod with this exact
name." The equality loop that already carries `{"i.node_name",
f.NodeName}` gains one pair, `{"p.name", f.PodName}`, where `p` is the
alias the `incidents` to `pods` join already uses for `pod_name` in the
row (read the SELECT to confirm the alias before writing it). Nothing
else changes: the pair is appended as `p.name = ?` with the value, the
same as every equality filter, and the planner reaches the pods index
through the join. A job-subject incident has no pod row, so the filter
never returns one, which is what an exact pod name means.

`internal/query/pods.go`: `PodFilter` gains `PodName string` ("PodName
keeps the row with this exact name.") and the equality loop gains
`{"p.name", f.PodName}` beside `{"p.namespace", f.Namespace}`.

`internal/api/incidents.go` `ListIncidents` and `internal/api/pods.go`
`ListPods`: the filter literal gains `PodName: req.GetPodName()`. No
validation: an unknown name is an empty page, not an error, the same as
an unknown namespace.

Tests, as rows in the existing tables, traced to 4.2 and 4.3's
`pod_name` sentences ("an exact match on the pod's name, so a client
that holds a name from a truncated list still reaches its rows"):

- `internal/query/incidents_test.go`, `TestListIncidentsFilters`: two
  rows. `{"pod name", IncidentFilter{PodName: <the crash pod's seeded
  name>}, <the ids of that pod's incidents>}` and `{"pod name prefix
  misses", IncidentFilter{PodName: <the first half of that name>},
  nil}`. Read `internal/query/querytest/seed.go` for the name the
  crash pod (`querytest.CrashPodUID`) is seeded with and which incident
  ids it owns; the expected ids are written out, never computed.
- `internal/query/pods_test.go`, `TestListPodsFiltersAndCounts`: two
  rows the same way, the hit naming one pod's uid and the prefix miss
  naming none.

Then `make generate`. The diff under `internal/apigen` and `api/openapi`
is generated and is committed with the source. `rm -rf macos/.build`
before `make app-test` so the Swift client regenerates with the new
query fields.

`idios mock` answers `ListIncidents` and `ListPods` with the whole
fixture set whatever the request says (`internal/api/mock/mock.go`), so
the mock needs no change for the filter: a palette lookup against
fixtures gets every row, and the client-side match keeps the right
one. The fixture pods already carry suffixed names
(`checkout-api-7d9f8b6c4-x2kqp`), which is what task 4's fixture check
types.

Check: `go test ./internal/query/... ./internal/api/...` green;
`curl -s '127.0.0.1:7771/v1/incidents?pod_name=smoke-oom'` against the
smoke store returns the oom pod's rows and nothing else;
`?pod_name=smoke` returns an empty page.

Checkpoint: `go build ./... && go test ./... && make ascii &&
make app-test && make app`; after the commit, `make generate-check`.
Commit: `api: add the pod_name filter to incidents and pods`.

Consumes: `IncidentFilter`, `PodFilter`, the equality loops,
`querytest.Seed`, `ListIncidentsRequest`, `ListPodsRequest`.
Produces: `IncidentFilter.PodName`, `PodFilter.PodName`, the wire
fields `pod_name` on both requests, and in the generated Swift client
`Operations.ListIncidents.Input.Query.pod_name` and
`Operations.ListPods.Input.Query.pod_name`.

## Task 2 - the query and the hits in IdiosModel

`macos/Sources/IdiosModel/Search.swift`.

    /// SearchQuery is what a person typed, read once: the free text and the
    /// fields the prefixes name.
    public struct SearchQuery: Hashable, Sendable {
        public let text: String
        public let incidentID: String?
        public let namespace: String?
        public let node: String?
        public let tag: String?
        public let reason: String?

        /// isEmpty is a query with nothing to match on.
        public var isEmpty: Bool

        /// init reads the raw field text: whitespace-separated terms, a term
        /// starting with "#" is an incident id, "ns:", "node:", "tag:" and
        /// "reason:" name a field, everything else joins the free text.
        public init(parsing raw: String)
    }

Rules of the parse: terms split on whitespace; matching is
case-insensitive, so `text` and every field are lowercased; a prefix
with nothing after the colon (`ns:`) sets nothing; a second prefix of
the same field replaces the first; `#` followed by digits is
`incidentID` (the digits alone), `#` followed by anything else is free
text; a term that is only digits stays free text (it matches an id
exactly through the incidents section and a name by substring). The
raw string `""` and `"   "` give `isEmpty == true`.

    /// SearchSection is one group of hits, in the palette's order.
    public enum SearchSection: String, CaseIterable, Hashable, Sendable {
        case recent, pods, runs, workloads, incidents, commands
        /// title is the section's heading.
        public var title: String
    }

    /// SearchAction is what opening a hit does; the application maps a route
    /// onto its screens.
    public enum SearchAction: Hashable, Sendable {
        case open(Route)
        case openWorkloadPods(Route)
        case openWorkloadRuns(Route)
        case back
    }

    /// SearchCommand is a row of the Commands or Recent section: a title, the
    /// key that also reaches it, and what it does.
    public struct SearchCommand: Identifiable, Hashable, Sendable {
        public let id: String
        public let title: String
        public let key: String?
        public let action: SearchAction
        public init(id: String, title: String, key: String?, action: SearchAction)
    }

    /// SearchHit is one row of the palette carrying the value it stands for.
    public enum SearchHit: Identifiable, Hashable, Sendable {
        case pod(PodRow)
        case run(RunFold)
        case workload(Workload)
        case incident(Incident)
        case command(SearchCommand)

        /// id is distinct across sections: "pod/<uid>", "run/<job uid>",
        /// "workload/<Workload.id>", "incident/<id>", "command/<id>".
        public var id: String

        /// action is what Return does: a pod opens its page, a run opens its
        /// lead pod's page or the job row's incident when no pod is kept, a
        /// workload opens its Workloads node, an incident opens its page, a
        /// command does what it says.
        public var action: SearchAction

        /// workloadAction is what Cmd-Return does: the hit's workload in
        /// Workloads, on Runs for a CronJob or Job and on Pods otherwise; nil
        /// for a command and for a bare pod.
        public var workloadAction: SearchAction?

        /// copyText is what Shift-Return copies: a pod's uid, a run's job uid,
        /// an incident's id, a workload's name; nil for a command.
        public var copyText: String?
    }

    /// SearchSectionHits is one section as drawn: its hits after the cap and
    /// how many the cap cut.
    public struct SearchSectionHits: Hashable, Sendable {
        public let section: SearchSection
        public let hits: [SearchHit]
        public let more: Int
    }

    /// sectionLimit is how many hits a section shows; the rest are a count,
    /// because a palette answers a name, not a list.
    public let sectionLimit = 6

    /// searchResults matches a query against the rows every store holds and
    /// returns the sections that have anything to say, in order.
    public func searchResults(
        query: SearchQuery, pods: [PodRow], incidents: [Incident],
        workloads: [Workload], commands: [SearchCommand], recent: [SearchCommand]
    ) -> [SearchSectionHits]

    /// commandList is every command the palette lists on an empty query: the
    /// Go menu's items with their keys and one "Show <view>" per sidebar view.
    public func commandList(views: [IncidentState]) -> [SearchCommand]

`searchResults`:

- An empty query returns `recent` (as `.command` hits, most recent
  first, no cap) when it is not empty, then `commands` whole. No other
  section.
- Runs are `runFolds(incidents)` (the fold of step B, over the rows
  carrying a `jobUID`); the incidents section keeps every row,
  including the ones that fold into a run, because `#187` is one
  incident whatever it folds into.
- A field prefix applies only to the sections whose rows carry the
  field, and empties the others: `ns:` matches `namespace` on pods,
  runs (the lead row), workloads and incidents; `node:` matches
  `nodeName` on pods and incidents and the rows of a run, and empties
  workloads; `tag:` matches `imageTag` on incidents and the rows of a
  run and `imageTags` on workloads, and empties pods; `reason:` matches
  `lastReason` and `firstReason` on incidents and the reasons of a run,
  and empties pods and workloads. `incidentID` empties everything but
  incidents. Commands are empty whenever any field or an id is set.
- Free text matches by substring, case-insensitive, on: a pod's `name`,
  `namespace`, `nodeName`, `workloadName`; a run's `suffix`, `jobUID`,
  `lead.workloadName`, `lead.namespace`, each pod name among its rows,
  its reasons; a workload's `workloadName`, `namespace`,
  `workloadKind`, each `imageTags[].tag`, its `podName`; an incident's
  `id` (exact only), `podName`, `workloadName`, `namespace`,
  `containerName`, `lastReason`, `firstReason`, `imageTag`, `nodeName`;
  a command's `title`. Every term of the free text must match some
  field of the same row (`checkout crash` finds the checkout pod's
  crash incident). Empty free text with a field set matches every row
  the field keeps.
- Rank: a row whose matched field equals the text is `exact`, a row
  whose matched field starts with it is `prefix`, the rest `contains`;
  exact before prefix before contains, ties in the order the rows came
  (the daemon's, newest first). An `incidentID` hit is exact.
- Each section is cut at `sectionLimit` and `more` counts the cut. A
  section with no hits is omitted, so the view draws only what has
  rows.
- Contextual commands: for the first three workload hits of a
  non-empty free-text query the Commands section carries "Show <kind>
  <name> in Workloads" with no key, whose action is the hit's
  `workloadAction`. That is the only content of Commands while the
  text is not empty apart from the commands whose title matches.

`commandList(views:)`: "Incidents" `cmd-1` `.open(.incidents(nil))`,
"Workloads" `cmd-2` `.open(.workloads)`, "Status" `cmd-3`
`.open(.status)`, "Back" `cmd-[` `.back`, then "Show <state.title>"
with no key and `.open(.incidents(state))` for each of `views`; the
application passes `IncidentsSidebar.views + IncidentState.closedStates`.
Keys are written as the words `cmd-1`, `cmd-[`; the view draws the
symbols. Search itself is not in the list: the palette is open.

`SearchHit.action`: `.pod(row)` is `.open(.pod(row.uid, .containers))`;
`.run(fold)` is `.open(.pod(uid, .containers))` for the newest row
among `fold.rows` that has a `podUID`, else
`.open(.incident(fold.lead.id))`; `.workload(w)` is
`.open(.workload(cluster: w.clusterID, namespace:, kind:, name:))` for
a real workload and `.open(.pod(w.podUID, .containers))` for a bare
pod row (`podUID` set); `.incident(i)` is `.open(.incident(i.id))`;
`.command(c)` is `c.action`. The cluster segment of a workload route is
the cluster id here; `IncidentsScreen` already resolves an id to a name
for `openWorkloadRuns` and does the same in task 4.

`SearchHit.workloadAction`: nil for a command and for a pod or incident
whose `workloadKind` is `none` or whose `workloadName` is empty;
otherwise the workload route of the hit's cluster, namespace, kind and
name wrapped in `.openWorkloadRuns` when the kind is `CronJob` or
`Job`, else `.openWorkloadPods`.

Tests, `macos/Tests/IdiosModelTests/SearchTests.swift`, Swift Testing,
table-driven as `RouteTests.swift` is, with private fixture helpers
`incident(id:podName:workloadKind:workloadName:namespace:container:
reason:tag:node:jobUID:)`, `pod(uid:name:namespace:node:workloadKind:
workloadName:)` and `workload(kind:name:namespace:tags:)` built through
the memberwise initialisers with fixed timestamps and every other
field at its zero. Invented names only: `checkout-api`, `report`,
`nightly`, `worker`, `idios-smoke`, `node-a`. Every test traces to the
9.3 sentence "Cmd-K opens a palette ... matching an incident id (#187),
a pod name including its suffix, a Job name, a workload, a namespace, a
container, a reason, an image tag and a node, sectioned as Pods, Runs,
Workloads, Incidents and Commands, with the field prefixes ns:, node:,
tag: and reason: ... and an empty query listing the recent pages and
every command with its key" unless another is named:

- `searchQueryReadsPrefixesAndTheIncidentID`: rows `("", empty)`,
  `("   ", empty)`, `("#187", incidentID "187")`, `("187", text
  "187")`, `("#abc", text "#abc")`, `("ns:", empty)`, `("ns:Idios
  crash", namespace "idios", text "crash")`, `("tag:1.36 node:node-a",
  tag "1.36", node "node-a")`, `("ns:a ns:b", namespace "b")`,
  `("Report", text "report")`; whole `SearchQuery` compared.
- `searchResultsSectionsEveryKindOfName`: one fixture set of two pods,
  four incidents (a crash on the checkout pod with tag `1.36` on
  `node-a`, a `job_failed` row and a pod row of one Job under CronJob
  `report` with `jobUID`, an oom on a bare pod), two workloads; rows
  `("x2kqp" -> pods [the checkout pod], incidents [its crash])`,
  `("report" -> runs [the fold], workloads [report], incidents [both
  job rows], commands ["Show CronJob report in Workloads"])`,
  `("#187" -> incidents [187] only)`, `("idios-smoke" -> every
  section that carries a namespace)`, `("app" -> incidents whose
  container is app)`, `("OOMKilled" -> the oom incident)`,
  `("1.36" -> the crash incident, the checkout workload)`,
  `("node-a" -> the checkout pod, the crash incident)`; whole
  `[SearchSectionHits]` compared.
- `searchPrefixesNarrowByFieldAndEmptyTheSectionsWithoutIt`: rows
  `("ns:idios-smoke", pods, runs, workloads, incidents all kept, no
  commands)`, `("node:node-a", pods and incidents only)`,
  `("tag:1.36", incidents and workloads only)`, `("reason:oom",
  incidents only)`, `("ns:other", nothing)`.
- `searchRanksExactBeforePrefixBeforeContains`: three workloads
  `api`, `api-gateway`, `checkout-api` served in that reverse order;
  query `api` returns them exact, prefix, contains; query `#12` with
  an incident `12` and a pod named `checkout-12x` returns incidents
  only (an id empties the rest).
- `searchCapsASectionAndCountsTheRest`: eight incidents of one
  workload; the section has `sectionLimit` hits and `more == 2`; the
  first six are the newest six in served order.
- `searchEmptyQueryListsRecentThenEveryCommand` (traces to 9.4's "the
  palette's empty query lists every command with its key"): with two
  recent commands the sections are `recent` (two, most recent first)
  and `commands` (the full `commandList`); with no recent, `commands`
  alone. `commandList(views:)` compared whole against the literal
  list for `[.attention, .open]`.
- `searchHitActionsOpenTheHitAndItsWorkload` (traces to 9.3's "Return
  opening, Cmd-Return opening in Workloads, Shift-Return copying the
  uid"): rows for a pod under a Deployment (open pod page; pods tab of
  the workload; copies the uid), a run with a pod kept (its newest
  pod's page; runs tab; the job uid), a run whose rows are the job row
  alone (the incident page), a bare pod incident (open; no workload
  action; the id), a workload (its node; itself on pods; its name), a
  CronJob workload (runs), a command (its action; nil; nil).

No test for `SearchSection.title`: constants.

Checkpoint as task 1 without `make generate-check`. Commit:
`model: parse the search query and rank the palette's hits`.

Consumes: `Incident`, `PodRow`, `Workload`, `RunFold`, `runFolds`,
`Route`, `PodTab`, `IncidentState.title`, `IncidentState.closedStates`.
Produces: `SearchQuery`, `SearchSection`, `SearchAction`,
`SearchCommand`, `SearchHit` with `.action`, `.workloadAction`,
`.copyText`, `SearchSectionHits`, `sectionLimit`, `searchResults`,
`commandList`.

## Task 3 - the palette state, the Go menu, the search store

`macos/idios/App/PaletteState.swift`:

    /// PaletteState is whether the palette is up and what it opened with; the
    /// Go menu and the overlay share it through the environment.
    @Observable @MainActor
    final class PaletteState {
        var isPresented = false
        /// launchQuery is the "-search <query>" launch argument, so a
        /// screenshot run opens the palette on a query without a keyboard.
        let launchQuery: String?

        init(arguments: [String] = CommandLine.arguments)
    }

`launchQuery` is read the way `Preferences` reads `-daemon`: the value
after `-search` when present, else nil. `isPresented` starts true when
`launchQuery` is set.

`macos/idios/App/IdiosApp.swift`: `@State private var palette =
PaletteState()`, injected with `.environment(palette)` beside the
navigator, and in `CommandMenu("Go")`, after "Status" and before the
Divider that precedes "Back":

    Divider()
    Button("Search") { palette.isPresented = true }
        .keyboardShortcut("k", modifiers: .command)

`macos/idios/Store/SearchStore.swift`:

    /// SearchStore holds what the palette matches beyond the rows the other
    /// stores have: the live pods of the scope, the rows a daemon lookup
    /// found for an id or an exact pod name, and the pages a person visited.
    @Observable @MainActor
    final class SearchStore {
        private(set) var pods: [PodRow] = []
        private(set) var lookupIncidents: [Incident] = []
        private(set) var lookupPods: [PodRow] = []
        private(set) var recent: [SearchCommand] = []
        private(set) var error: APIError?

        /// recentLimit is how many visited pages the empty query lists.
        static let recentLimit = 8

        /// loadPods reads the live pods of the scope once per palette opening.
        func loadPods(connection: DaemonConnection, scope: ClusterScope) async

        /// lookup asks the daemon for what the held rows may have lost to the
        /// list limit: the incident of an id, and the incidents and pods of an
        /// exact pod name.
        func lookup(query: SearchQuery, connection: DaemonConnection, scope: ClusterScope) async

        /// visited records a page the person opened, most recent first, once.
        func visited(_ command: SearchCommand)

        /// clearLookup drops the last lookup when the query changes.
        func clearLookup()
    }

`loadPods` is `ListPods(query: .init(cluster_ids: scope.isAll ? nil :
scope.selected.sorted(), live: "true"))` decoded through
`PodRow(wire:)` into `pods`; an empty scope sets `[]` and calls nothing,
as `GroupFactsStore.loadWorkloads` does. `lookup`: when
`query.incidentID` is set, `GetIncident(path: .init(id:))` and the
detail's incident row (decode it the way `PodPageStore` decodes the
detail; read that store for the type) becomes `lookupIncidents`, a 404
(`.default(404, _)`) leaves it empty without reporting; otherwise, when
`query.text` has at least three characters and contains a `-` (a pod
name always does; a reason or a tag never needs the daemon),
`ListIncidents(query: .init(cluster_ids:, pod_name: query.text))` and
`ListPods(query: .init(cluster_ids:, pod_name: query.text))` fill the
two lists; anything else clears them. The exact match is on the whole
name, so a suffix alone stays a client-side match and only a pasted
full name reaches the daemon, which is the case the list limit
creates. Errors go through the `report` shape every store has;
`.cancelled` is ignored. `visited` inserts at the front, removes an
earlier entry with the same `id`, and cuts at `recentLimit`.

No model test: the store is calls and a list. Check: the Go menu shows
"Search" with the Cmd-K symbol; pressing it flips `isPresented` (task 4
draws it); `./bin/idios ... mock` on 7771 and a `curl` of
`/v1/pods?live=true` returns the fixture pods the store will hold.

Checkpoint as task 2. Commit: `app: add the search store and the Go
menu's Search item`.

Consumes: `DaemonConnection`, `ClusterScope`, `APIError`, `PodRow(wire:)`,
`Incident(wire:)`, the `report` shape of `GroupFactsStore`, task 1's
`pod_name` query fields, task 2's `SearchQuery`, `SearchCommand`.
Produces: `PaletteState`, `SearchStore`, the Go menu's Search item.

## Task 4 - the palette over the screen

`macos/idios/Views/Search/SearchPalette.swift`:

    /// SearchPalette is what Cmd-K opens: one field, the sections that match
    /// it, and the three ways out of a hit.
    struct SearchPalette: View {
        let sections: [SearchSectionHits]
        let now: Date
        @Binding var text: String
        let perform: (SearchAction) -> Void
        let copy: (String) -> Void
        let dismiss: () -> Void
    }

Width 640, top-aligned with a 12 percent inset from the window's top,
over a backdrop of `Color.black.opacity(0.25)` whose tap dismisses (a
system-tinted dim is not a vocabulary colour; it is the overlay's
shade, and `.black` with an alpha is what every macOS sheet backdrop
is). The panel: `.regularMaterial` background, corner radius 12, a
`TextField("Search", text: $text)` in `.plain` style at 15 points with
a magnifying glass, an `esc` hint at the right; under it the sections
in a `ScrollView`, each a header row with the section title in
`.secondary` caps and, when `more > 0`, "+N more" at the right; the
hits as `SearchHitRow`s; the selected hit carries
`Color.accentColor` background with
`Color(nsColor: .alternateSelectedControlTextColor)` text, as the
sidebar's active row does. The footer is one line in `.secondary`:
"return open", "cmd-return open in Workloads", "shift-return copy uid",
"type # for an incident id", drawn with the key symbols.

Selection: `@State private var selectedID: String?`, reset to the
first hit whenever `sections` changes. The field keeps the focus for
the whole life of the palette (`@FocusState` set on appear), and the
keys live on it: `.onKeyPress(.downArrow)` and `.upArrow` move the
selection across sections; `.onKeyPress(.return)` reads the modifiers
of the press: none performs `hit.action`, `.command` performs
`hit.workloadAction` when it is set (ignored otherwise), `.shift`
copies `hit.copyText` when it is set; every performed action and every
copy dismisses the palette. `.onKeyPress(.escape)` dismisses. A click
on a row selects it, a second click performs its action. Return with
no hit does nothing.

`macos/idios/Views/Search/SearchHitRow.swift`:

    /// SearchHitRow is one hit as the palette draws it: a glyph or badge, the
    /// name with what disambiguates it, and the state or time at the right.
    struct SearchHitRow: View {
        let hit: SearchHit
        let now: Date
        let selected: Bool
    }

Cells by case, from frame 1b: `.pod`: the pod name in monospace with
the suffix after the workload prefix in `.primary` and the prefix in
`.secondary` (the same split the incident row makes), then the
worst-state dot by `podBadge(worstState:phase:)` when a state is
known, the namespace, and at the right `durationText` from
`lastSeenAt`. `.run`: "run" in `.secondary`, the suffix (or the elided
uid), "N pods", the categories' `CategoryBadge`s, the reasons, at the
right the badge `groupBadge(fold.rows)`. `.workload`: the kind in
`.secondary`, the name, the namespace, the open count as a
`StateBadge`-shaped `Badge` in the open tone when it is above zero.
`.incident`: `#id`, `CategoryBadge`, the container or "job", the
subject (`workloadTitle` with the pod suffix), `StateBadge`, the age.
`.command`: the title, and at the right the key drawn as symbols
(`cmd-1` reads as the command glyph and 1; `cmd-[` as the glyph and
`[`). No colour but the badges' and the selection's.

`macos/idios/Views/Incidents/IncidentsScreen.swift`:

- `@Environment(PaletteState.self) private var palette`, `@State
  private var search = SearchStore()`, `@State private var searchText
  = ""`.
- The `NavigationSplitView` gains `.overlay { if palette.isPresented {
  SearchPalette(...) } }`, so the palette floats over whatever the
  split view shows, sidebar and pod page included.
- `sections` is `searchResults(query: SearchQuery(parsing: searchText),
  pods: search.pods + search.lookupPods, incidents: incidents.rows +
  search.lookupIncidents + (workloads.incidents(of: key) ?? []) for
  the workload key in view, workloads: workloads.workloads, commands:
  commandList(views: IncidentsSidebar.views +
  IncidentState.closedStates), recent: search.recent)`, with
  duplicates by `id` dropped, first wins, so a row the list holds and
  the daemon returned is one hit. Runs of the workload in view come
  through its incidents: `WorkloadsStore.incidents(of:)` already holds
  the incident rows the detail loaded, and `runFolds` folds them.
- `.task(id: palette.isPresented)` runs `search.loadPods` on opening
  (and `searchText = palette.launchQuery ?? ""` the first time it is
  set); `.task(id: searchText)` sleeps 250 ms then runs `search.lookup`
  with the parsed query, so a lookup fires once per pause in typing;
  closing the palette clears `searchText`.
- `perform(_ action: SearchAction)`: `.open(let route)` is `show(route)`
  for `.incidents`, `.workloads`, `.status` and `.workload`, and
  `openIncident`/a `path` push for `.incident` and `.pod` (the routes
  `destination` already maps); `.openWorkloadPods(route)` and
  `.openWorkloadRuns(route)` set `screen = .workloads`,
  `workloadsTree.tab` to `.pods` or `.runs`, `workloadRoute` to the
  route with its cluster id resolved to the cluster's name as
  `openWorkloadRuns` does, and `path = []`; `.back` is
  `navigator.goBack()`. A `.workload` route arriving with a cluster id
  where the tree expects a name is resolved through
  `clusters.cluster(id:)?.name ?? id` in one place, `resolvedWorkload(
  _ route: Route) -> Route`.
- `copy(_ text: String)`: `NSPasteboard.general.clearContents()` and
  `setString(text, forType: .string)`, the same two lines the pod
  page's Copy uid uses.
- Recent pages: `search.visited(...)` is called from `openIncident`
  ("#<id> <category label> - <workloadTitle>" opening `.incident(id)`),
  from `openWorkloadPods` and `openWorkloadRuns` ("<kind> <name>"
  opening the workload route), and from `show` for `.status` and
  `.workloads` ("Status", "Workloads"). The id of each recent command
  is the route spelled the way the screenshot routes are:
  "incident/<id>", "workload/<cluster>/<ns>/<kind>/<name>",
  "workloads", "status"; `Route` has no such property, so the screen
  builds the string in one private function, `recentID(_ route:
  Route) -> String`.

Check against the fixture daemon on 7771 (`./bin/idios -data-dir
.storage -listen 127.0.0.1:7771 mock`), launching the built
application with `-daemon 127.0.0.1:7771 -search x2kqp` for the
screenshot: the Pods section shows `checkout-api-7d9f8b6c4-x2kqp` with
its suffix in the primary colour and the Incidents section its crash;
`-search '#412'` shows the incident alone; `-search report` shows the
CronJob's run, the workload, its two rows and "Show CronJob report in
Workloads"; an empty `-search ''` shows the commands with their keys.
Then the smoke store (`./bin/idios -data-dir .storage/smoke -kubeconfig
./kube/config -listen 127.0.0.1:7771 run`): Cmd-K in the running
application, type `smoke-oom`, Return opens the pod page; Cmd-K,
`smoke-cron-fail`, Cmd-Return opens Workloads on its Runs; Cmd-K,
`ns:idios-smoke`, every section fills; Cmd-K on the pod page still
opens over it; `reason:OOMKilled` lists the oom rows; Esc closes and
"/" still focuses the list's filter. Paste a full pod name of a run
older than the list holds (from the Workloads Pods tab): the Pods
section carries it through the daemon lookup.

Checkpoint as task 2. Commit: `app: open the Cmd-K palette over the
screen`.

Consumes: task 2's types and functions, task 3's `PaletteState` and
`SearchStore`, `IncidentsStore.rows`, `WorkloadsStore.workloads` and
`.incidents(of:)`, `IncidentsScreen.show`, `.openIncident`,
`.openWorkloadPods`, `.openWorkloadRuns`, `Navigator.goBack`,
`clusters.cluster(id:)`, `CategoryBadge`, `StateBadge`, `Badge`,
`groupBadge`, `podBadge`, `durationText`, `workloadTitle`,
`IncidentsSidebar.views`, `IncidentState.closedStates`.
Produces: `SearchPalette`, `SearchHitRow`, `IncidentsScreen.perform`,
the `-search` launch argument honoured, the recent pages recorded.

## Task 5 - verification with the user and the status line

The user runs the application against the smoke store on 7771 and
tries the palette from every screen: an id, a suffix, a Job name, a
workload, a namespace, a container, a reason, a tag, a node, each
prefix, Return, Cmd-Return, Shift-Return, the empty query's recent
pages and commands, Esc, and "/" afterwards. Anything the review turns
up is fixed in the task that owns it and committed as a new commit,
never by rewriting one. Then `docs/plans/m10-reading/roadmap.md`'s step
C line becomes "complete <date>" and the step D and E lines stay "not
started". Commit: `docs: close m10 step C`. The 7771 daemon is stopped.

## Hands to the next step

Step D (`explore.md`) reads: `SearchHit.workloadAction` and
`IncidentsScreen.perform`, which are how a run strip cell or a tree
context menu opens a workload node with a tab (`.openWorkloadPods`,
`.openWorkloadRuns`, and `resolvedWorkload`); `SearchStore.recent` and
`visited`, which the pod page's Forward (Cmd-]) does not use (Forward is
a `path` concern; the recent list is the palette's); `PaletteState`
for nothing. Step E reads `SearchStore.loadPods` as the one `ListPods`
call over a scope, if the menu bar ever needs live pods; it does not
today. The `pod_name` filter is stated in 4.2 and 4.3 and needs no
further doc change.

## Self-review

Spec coverage. 4.2 and 4.3's `pod_name` sentences: task 1 (the two
fields, the two equality pairs, the two test rows each, the generated
client). 9.3's palette sentence: task 2 (every named match target is a
matched field; the five sections plus Recent, which the sentence calls
"the recent pages"; the four prefixes; Return, Cmd-Return and
Shift-Return as `action`, `workloadAction`, `copyText`), task 3 (Cmd-K
as a Go menu item, the live pods and the daemon lookups for an id and
an exact name), task 4 (the palette over the current screen, the
recent pages recorded, the keys). 9.4's paragraph: task 3 (the Go menu
holds Cmd-K Search), task 2 and 4 (the empty query lists every command
with its key). "/" is untouched: step B's `focusFilter`. Decision 6's
"No grouped count endpoint": nothing is added to the daemon but the
filter. Decision 10: every name in tests and checks is a fixture's or
invented.

Doubts. The mockup's frame 1b draws "Show CronJob smoke-cron-fail in
Workloads" with a `cmd-2` key; the plan gives the contextual command
no key, because Cmd-2 opens Workloads without a node and the sentence
names Cmd-Return for the node. Frame 1b lists a run by its Job name;
the plan's run row says "run <suffix>" as step B's list row does, so
one word means one thing. The roadmap's step C paragraph names "the
mock fixtures"; the mock ignores every filter and its pods already
carry suffixes, so task 1 changes no fixture and says why. The
`GetIncident` lookup for an id reads the detail endpoint for one row;
a `ListIncidents` with an id filter does not exist and the sentence
says "the detail endpoints for an id", so the detail call is what it
names. A daemon lookup runs only for a query with a dash and three
characters, which is a heuristic; a pod name always has one, and the
alternative (a call on every keystroke) is what the sentence's
"client-side" excludes.

`.ai` rules. ascii-only: no symbol in any code block; the key names are
words (`cmd-1`) and the view draws the glyphs. tests: every test is
traced above, table-driven, whole-value; no test of a constant, a
title or the store. comments: every comment in a code block says why;
the doc lines are one sentence. code-is-truth: no code block names a
document, section or plan. scope: no history for screen switches
(raised in step B's review and not a decision), no fuzzy matching, no
persistence of the recent pages across launches, no new endpoint.
commits: five subjects under 72 characters, `api:`, `model:`, `app:`,
`app:`, `docs:`; `make generate-check` after task 1's commit.

Type consistency. `SearchQuery(parsing:)` in task 2 is what task 3's
`lookup(query:)` and task 4's `sections` take; `SearchSectionHits`
(task 2) is what `SearchPalette.sections` (task 4) draws;
`SearchAction` (task 2) is what `SearchCommand.action`, `SearchHit
.action`, `.workloadAction` and `IncidentsScreen.perform` (task 4)
share; `SearchCommand` (task 2) is the type of `SearchStore.recent`
(task 3) and of the `recent:` argument (task 2); `commandList(views:)`
takes `[IncidentState]` and `IncidentsSidebar.views +
IncidentState.closedStates` is `[IncidentState]`; the query fields
`pod_name` (task 1) are the ones `SearchStore.lookup` (task 3) sends;
`Route.pod(String, PodTab)` and `.workload(cluster:namespace:kind:
name:)` (existing) are the routes task 2's actions carry and task 4
resolves.
