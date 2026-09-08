# m10 step E - glance: the menu bar, Status, and the fields nothing drew

Goal: after this plan the glance is honest and nothing the model decodes
is invisible. The menu bar extra leads with the open count, keeps the
attention count as its subline, puts that same open count on the Dock
badge, folds its rows into one line per workload group the way the list
does, greys a closed group, counts the rest in one sentence whose window
is the daemon's, acknowledges everything it shows, and opens the group a
person clicks in the list. Status stops printing its header in a
monospaced font and stops printing configuration in seconds. The six
fields the application already receives and never drew are drawn where
the mockups place them: the incident's `node_name` beside the pod's node,
`related_incidents` (decoded for the first time), the container's
`running_since`, the resource byte and milli values beside the strings
the kubelet wrote, `Event.first_ts` as a span, and the Job's `succeeded`,
`completions` and `parallelism`. Then the README's screenshots are
retaken against the smoke store, and the milestone closes.

No proto change. `related_incidents` is field 11 of `IncidentDetail` in
`api/proto/idios/v1/incidents.proto` and is already served, already in
`api/openapi` and already generated as
`Components.Schemas.IncidentDetail.relatedIncidents`; decoding it is a
change to `IdiosModel.IncidentDetail` alone. Nothing under `api/proto`,
`api/openapi` or `internal/apigen` is touched, so there is no
`make generate` and no `make generate-check` in this step.

Architecture: what can be decided without a view is decided in
`IdiosModel` as a pure function with a table test - which related rows
belong on the Related tab, the resource line, the running-since line, the
node line, the Job's counters, the event span, the menu bar's tail
sentence and the scope a reveal widens to. The application draws them:
`KubeletCard`, `JobCard`, `EventsCard`, `PodPageRail` and `PodCardPane`
on the pod page; `MenuBarStore` and `MenuBarView` for the glance, folding
their rows through `incidentGroups`, the function the incidents list
already groups with, so the menu bar and the list cannot disagree about
what one problem is; `StatusScreen` for the header and the units. One
navigation path is new: a menu bar row asks `Navigator` for a group, and
`IncidentsScreen` answers by putting the list on Attention, grouped by
workload, with that group open, selected and scrolled to. Views never
import `IdiosAPI`; stores own every client call.

Tech stack: Swift 6 package `macos/Sources/IdiosModel` with tests in
`macos/Tests/IdiosModelTests` (`make app-test`); the Xcode application
under `macos/idios` (`make app`; a file under `macos/idios/` joins the
target by existing). SwiftUI for every view; `MenuBarExtra` in the
`.window` style, which is already how the popover is drawn;
`NSApp.dockTile.badgeLabel` from `AppKit` for the badge, which is the
only API that sets it; `ScrollViewReader` in the incidents list for the
one reveal that has to scroll; `.confirmationDialog` for the popover's
bulk acknowledge, as the list already asks. The daemon is unchanged;
`go build ./... && go test ./...` runs only to prove that.

Spec: `docs/design/presentation.md` section 4.7 whole (the headline, the
Dock badge, the subline, the rows and where they come from), the Menu bar
extra row of 9.3, the Status row of 9.3, the Pod page row of 9.3 for the
rail's context and the Related incidents tab, 4.5 (`succeeded` is read
from `condition_type`, never from the counters), 4.2 (`node_name` is the
node the pod was on when the incident opened), 7.4 (no value is silently
cut; a card's provenance is its title's hover), 7.6 (what idios does not
know is said, not hidden), 9.5 (the colour rule) and 9.6 (the vocabulary
and the legend). `docs/design/data-storage.md`: the `k8s_events`
timestamp precedence paragraph ("a pod's whole story often happens inside
one minute"), the `containers` rows `running_since`, `mem_limit_bytes`
and `cpu_limit_millis`, and the `jobs` rows `succeeded`, `completions`
and `parallelism`. Roadmap decisions 9 and 10 of
`docs/plans/m10-reading/roadmap.md`. Visual reference: page 7 (frame 7a)
and the Status frame of `docs/mockups/idios-ui.html`.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Swift follows the Go rules: one-line doc comment per
public type and function, comments say why, tests trace to a spec
statement with whole-value assertions, variants are table rows. Views
never import `IdiosAPI`; stores own every client call; a cancelled call
maps to `APIError.cancelled` and every store ignores it. Every count in
the application is a count of incidents, and the menu bar's rows are no
exception: a group's badge counts the incidents open under it, and the
only number that counts anything else is the count of problems in the
tail sentence, which says the word "problems". The only hard-coded
colours live in `BadgeStyle.swift`: a menu bar row takes
`groupBadge(rows).tone.badge`, which is red, amber or grey by the one
state rule, and nothing new is added to that file. The Dock badge is
drawn by macOS in its own red and is not ours to colour.

Nothing in a test, comment, doc or fixture names a real organisation,
cluster, namespace, workload, image or node. The invented names this plan
and its tests use are `checkout-api`, `report`, `nightly`, `worker`,
`idios-smoke`, `api` and `node-a`.

The application takes the daemon as a launch argument: the Debug build is
started with `nohup macos/DerivedData/Build/Products/Debug/idios.app
/Contents/MacOS/idios -daemon 127.0.0.1:7771 &` after `make app`
(`IDIOS_DAEMON` is read only by `hack/macos/screenshot.sh`). That build
shares its bundle id and its `UserDefaults` with the installed
application: never `defaults write` that domain; override a preference
for one run with an argument-domain flag (`-grouping workload
-density comfortable`). It also shares the Dock: the badge this step
sets belongs to whichever build is running, so the Debug build is quit
after a check rather than left behind with a stale number.

The production daemon on 127.0.0.1:7770 is never touched, read or
screenshotted; every check and every screenshot in this step runs against
a private daemon on 127.0.0.1:7771. When the installed application holds
7770, `make smoke PORT=7771` fills and serves `.storage/smoke` on the
free port and `IDIOS_DAEMON=127.0.0.1:7771` points the application and
`hack/macos/screenshot.sh` at it. Before any screenshot run
`screencapture -x /tmp/idios-probe.png` and stop if it fails: the
terminal needs Screen Recording permission. For a check that is not a
README screenshot, capture through a scratch script that starts the
application with `-screenshot <png> -route <route> -daemon
127.0.0.1:7771`, waits for "idios-window <n>" on stdout, sleeps 2.5 s,
runs `screencapture -x -o -l <n> <png>` and kills the application; the
four README screenshots go through `hack/macos/screenshot.sh` itself,
which is what the README says they come from.

The mock daemon ignores every list filter and answers with the whole
fixture set, and its job incident carries no job uid, so the run fold
inside a group shows only against the smoke store: `.storage/smoke`,
refilled with `make smoke PORT=7771`, its incidents reopened with
`KUBECONFIG=./kube/config kubectl -n idios-smoke apply -f hack/smoke/`.
Its failing CronJob is what makes the menu bar's fold worth having: 56
job-level rows are one line once they are folded.

Checkpoint before every commit: `go build ./... && go test ./... &&
make ascii && make app-test && make app`. Implementers never commit;
nothing is committed without the user's review of the diff. The decisions
are made; where a frame and a sentence differ, the sentence of 4.7 or 9.3
wins and the doubt goes in the self-review.

## File structure

    macos/Sources/IdiosModel/Incident.swift            IncidentDetail.relatedIncidents (task 1)
    macos/Sources/IdiosModel/Related.swift             relatedElsewhere (task 1)
    macos/Sources/IdiosModel/Display.swift             ResourceUnit, resourceLine, millicores (task 1)
    macos/Sources/IdiosModel/Event.swift               eventSpan (task 1)
    macos/Sources/IdiosModel/PodPage.swift             runningSinceText, nodeLine (task 1)
    macos/Sources/IdiosModel/Workload.swift            jobCounters (task 1)
    macos/Tests/IdiosModelTests/RelatedTests.swift     the related rows, the node line (task 1)
    macos/Tests/IdiosModelTests/DisplayTests.swift     resourceLine, millicores (task 1)
    macos/Tests/IdiosModelTests/EventTests.swift       eventSpan (task 1)
    macos/Tests/IdiosModelTests/PodPageTests.swift     runningSinceText (task 1)
    macos/Tests/IdiosModelTests/WorkloadTests.swift    jobCounters (task 1)
    macos/idios/Views/IncidentDetail/KubeletCard.swift the resources, running since (task 2)
    macos/idios/Views/IncidentDetail/JobCard.swift     the run counters (task 2)
    macos/idios/Views/IncidentDetail/EventsCard.swift  the span under the stamp (task 2)
    macos/idios/Views/PodPage/PodPageRail.swift        the node at open (task 2)
    macos/idios/Views/PodPage/PodCardPane.swift        Related reads the detail (task 2)
    macos/idios/Store/PodPageStore.swift               related from the lit detail (task 2)
    macos/idios/Store/MenuBarStore.swift               openCount, the list limit, acknowledgeShown, the badge (task 3)
    macos/idios/Views/MenuBar/MenuBarView.swift        the headline, the subline, the bulk item (task 3)
    macos/Sources/IdiosModel/MenuBar.swift             menuBarTail (task 4)
    macos/Sources/IdiosModel/Cluster.swift             ClusterScope.including (task 4)
    macos/Tests/IdiosModelTests/MenuBarTests.swift     the tail sentence (task 4)
    macos/Tests/IdiosModelTests/ClusterTests.swift     the widened scope (task 4)
    macos/idios/App/Route.swift                        GroupTarget, Navigator.open(group:) (task 4)
    macos/idios/Views/Incidents/IncidentsScreen.swift  the reveal (task 4)
    macos/idios/Views/Incidents/IncidentsList.swift    the scroll to a revealed group (task 4)
    macos/idios/Views/Status/StatusScreen.swift        the system font, the human units (task 5)
    docs/screenshots/*.png                             retaken (task 6)
    README.md                                          the screenshot sentences (task 6)
    docs/plans/m10-reading/roadmap.md                  step E's status line (task 7)

## Task 1 - the fields nothing reads, in IdiosModel

`macos/Sources/IdiosModel/Incident.swift`. `IncidentDetail` gains one
stored property, decoded from the wire message it already reads:

    /// relatedIncidents is the other incidents of the same pod and of the
    /// same Job, newest activity first, as the daemon bounded them.
    public let relatedIncidents: [Incident]

`init(wire:)` reads `try (wire.relatedIncidents ?? []).map(Incident.init(
wire:))`, and `replacingIncident(_:)` carries the value through
unchanged, as it carries every other part of the read. The memberwise
initialiser gains the parameter last, so every existing call site names
it; there is one, in `init(wire:)`, plus the tests.

`macos/Sources/IdiosModel/Related.swift`:

    /// relatedElsewhere keeps the related rows a pod's own page cannot
    /// already show: the Job's own row and the retries that ran in other
    /// pods. This pod's incidents are the cards in the left column, so a
    /// related list that repeated them would say the same thing twice.
    public func relatedElsewhere(_ rows: [Incident], podUID: String?) -> [Incident]

Rules. A row is kept when its `podUID` is nil (a job-subject incident has
no pod) or differs from `podUID`; served order is kept, because the
daemon ordered by newest activity. A nil `podUID` argument (a pod-less
Job page) keeps every row.

`macos/Sources/IdiosModel/Display.swift`:

    /// ResourceUnit is how a parsed resource value is spelled once the
    /// quantity the kubelet wrote has been parsed.
    public enum ResourceUnit: Hashable, Sendable { case bytes, millicores }

    /// millicores writes a parsed cpu value the way a Kubernetes quantity
    /// spells the small end of the scale, so half a core reads as 500m
    /// whatever the spec said.
    public func millicores(_ millis: Int64) -> String

    /// resourceLine is one resource pair as the kubelet wrote it, with the
    /// parsed value beside a quantity that does not already read as one:
    /// "1" and "1000m" are the same cpu and only one of them can be
    /// compared with the other container's.
    public func resourceLine(
        name: String, request: String?, limit: String?,
        requestValue: Int64?, limitValue: Int64?, unit: ResourceUnit
    ) -> String?

Rules. `millicores` is `"\(millis)m"`. `resourceLine` answers nil when
neither `request` nor `limit` is set, so a container with no cpu written
drops the whole pair rather than saying "cpu none". Each side is
`"request <written>"` or `"limit <written>"`, and the parsed value
follows in parentheses when it is set and its text differs from the
written quantity (`byteCount` for `.bytes`, `millicores` for
`.millicores`). The sides are joined with `", "` and the name leads:
`"memory request 256Mi (268.4 MB), limit 512Mi (536.9 MB)"`,
`"cpu request 500m, limit 1 (1000m)"`. A side whose written quantity is
absent but whose parsed value is set spells the parsed value alone, since
a value the daemon parsed and a spec that no longer holds it is still a
number worth showing.

`macos/Sources/IdiosModel/Event.swift`:

    /// eventSpan is how long a repeated event went on: Kubernetes keeps one
    /// row per reason and counts the repeats, so the row's own stamp is only
    /// the last of them.
    public func eventSpan(count: Int32, firstTS: Timestamp?, lastTS: Timestamp?) -> String?

Rules. nil when `count` is under 2, when `firstTS` is nil, or when
`firstTS.raw` equals `lastTS?.raw`: one occurrence has no span and a
first that equals the last is the same instant, which the row's stamp
already says. Otherwise `"x<count> over <durationText>, first
<clockTime firstTS>"`, with the duration part dropped when either stamp
does not parse, because a stamp idios kept but cannot read is still a
first time.

`macos/Sources/IdiosModel/PodPage.swift`:

    /// runningSinceText is when the container that is running now started,
    /// which is not when the pod started once a container has restarted.
    public func runningSinceText(_ container: Container, now: Date) -> String?

    /// nodeLine is the node the pod is on, and the node the lit incident
    /// opened on when the pod has since been placed somewhere else.
    public func nodeLine(podNode: String?, incidentNode: String, deleted: Bool) -> String

Rules. `runningSinceText` answers nil unless `container.state` is
`.running` and `runningSince` is set; otherwise `"since <clockTime
runningSince> (<durationText from runningSince to now>)"`, with the
parenthetical dropped when the stamp does not parse. `nodeLine` answers
`"not scheduled"` for a nil `podNode` and an empty `incidentNode`,
`"<incidentNode> (at open; the pod is no longer placed)"` for a nil
`podNode` with an incident node, `"<podNode>"` when the two agree or the
incident node is empty, and `"<podNode> (opened on <incidentNode>)"` when
they differ; `deleted` prefixes the answer with `"was on "` whenever a
node is named, which is what the header's counts line already says about
a pod that is gone.

`macos/Sources/IdiosModel/Workload.swift`:

    /// jobCounters is what the Job's spec asked for and what its status
    /// counted: the completions wanted, how many pods reached them, how many
    /// ran at once and how many failed against the backoff limit.
    public func jobCounters(_ job: Job) -> String

Rules. `"succeeded <succeeded> of <completions>"` when `completions` is
above zero and `"succeeded <succeeded>"` otherwise, then `"parallelism
<parallelism>"` when it is above zero, then `"failed <failed> of backoff
limit <backoffLimit>"`, joined with `" - "`. The counters are context and
never the outcome: 4.5 fixes `succeeded` as read from `condition_type`
and the pod counters as context only, so this line says what the counters
counted and the Run outcome row above it keeps saying what happened.

Tests, Swift Testing, table-driven, whole-value, edge cases first, in the
existing files where one exists.

- `relatedRowsAreTheJobRowAndTheRetriesInOtherPods` in
  `RelatedTests.swift` (traces to 9.3's "Related incidents only for a
  Job's pod (the `job_failed` row and the retries in other pods, by
  `job_uid`)"): rows built from `crashIncident` - a row of this pod, a
  row with no pod uid, a row of another pod, and the same list with a
  nil `podUID` argument; the whole `[Incident]` compared, so the order
  is asserted with the filter.
- `nodeLineSaysWhereThePodIsAndWhereItOpened` in `RelatedTests.swift`
  (traces to 4.2's "`node_name`, the node the pod was on when the
  incident opened"): rows `(nil, "", false) -> "not scheduled"`,
  `(nil, "node-a", false) -> "node-a (at open; the pod is no longer
  placed)"`, `("node-a", "node-a", false) -> "node-a"`, `("node-a", "",
  false) -> "node-a"`, `("node-a", "node-b", false) -> "node-a (opened
  on node-b)"`, `("node-a", "node-b", true) -> "was on node-a (opened on
  node-b)"`.
- `resourceLinePutsTheParsedValueBesideTheWrittenOne` in
  `DisplayTests.swift` (traces to decision 9's "the resource byte and
  milli values" and to the `containers` rows `mem_limit_bytes` and
  `cpu_limit_millis` of `data-storage.md`, which are the parsed
  quantities): rows for neither side written (nil), a memory pair with
  both sides and both parsed, a cpu pair whose written limit is `"1"`
  and whose parsed limit is 1000 (the parenthetical), a cpu pair whose
  written limit is `"500m"` and whose parsed limit is 500 (no
  parenthetical, the texts agree), a side written with nothing parsed,
  and a side parsed with nothing written.
- `millicoresSpellTheSmallEndOfTheScale` in `DisplayTests.swift` (same
  sentence): rows `(0, "0m")`, `(500, "500m")`, `(1000, "1000m")`,
  `(2500, "2500m")`.
- `eventSpanSaysHowLongARepeatedEventWentOn` in `EventTests.swift`
  (traces to `data-storage.md`'s "a pod's whole story (scheduled,
  pulling, failed, backing off) often happens inside one minute", which
  is why `first_ts` is kept beside `last_ts`): rows for count 1 (nil),
  count 17 with no `firstTS` (nil), count 17 with `firstTS` equal to
  `lastTS` (nil), count 17 over 42 minutes (the sentence written out),
  count 17 whose stamps do not parse ("x17, first ..." with no
  duration).
- `runningSinceTextOnlyForAContainerThatIsRunning` in
  `PodPageTests.swift` (traces to the `containers` row `running_since`,
  `state.running.startedAt`): rows for a waiting container (nil), a
  running container with no `runningSince` (nil), a running container
  started two hours before `now` (the sentence), a terminated container
  that still carries a `runningSince` (nil, because the value is the
  previous run's and the card is about now).
- `jobCountersSayWhatWasAskedForAndWhatWasCounted` in
  `WorkloadTests.swift` (traces to 4.5's "`succeeded` is read from
  `condition_type`, never from the counters" and to the `jobs` rows
  `succeeded`, `completions` and `parallelism`): rows for a run with
  completions 1, parallelism 1, succeeded 0, failed 6, backoff limit 5;
  a run with completions 0 and parallelism 0 (both parts dropped); a
  completed run (succeeded 1 of 1); a parallel run (completions 5,
  parallelism 2).

Check: `make app-test` green. No view draws these yet, so the
application is unchanged and `make app` proves only that the package
still builds into it.

Checkpoint: `go build ./... && go test ./... && make ascii &&
make app-test && make app`.
Commit: `model: decode the related incidents and the unread fields`.

Consumes: `Incident`, `Container`, `Job`, `Timestamp`, `byteCount`,
`clockTime`, `durationText`, `Components.Schemas.IncidentDetail`.
Produces: `IncidentDetail.relatedIncidents`, `relatedElsewhere`,
`ResourceUnit`, `millicores`, `resourceLine`, `eventSpan`,
`runningSinceText`, `nodeLine`, `jobCounters`.

## Task 2 - the pod page draws them

`macos/idios/Views/IncidentDetail/KubeletCard.swift`.

- The `Current state` fact gains the running-since line under it: when
  `runningSinceText(container, now: now)` answers, a second `Text` in
  `.tertiary` at 10.5 points inside the same `FactRow`, with
  `.help("containers.running_since, the kubelet's
  state.running.startedAt")`. The card takes `let now: Date` for it,
  passed by the two panes that build it, so the age is the pane's clock
  and not a second one.
- `cpuMemory` and `resources(_:request:limit:)` are replaced by two
  calls to `resourceLine`: `resourceLine(name: "cpu", request:
  container.cpuRequest, limit: container.cpuLimit, requestValue:
  container.cpuRequestMillis, limitValue: container.cpuLimitMillis,
  unit: .millicores)` and the memory pair with `memRequestBytes` and
  `memLimitBytes` under `.bytes`. The two answers are joined with
  `" - "`, and `"no requests written"` stands when both are nil, as it
  does today. The private helper goes; the model function is the one
  place the rule lives.

`macos/idios/Views/IncidentDetail/JobCard.swift`. The `Counters` row's
text becomes `jobCounters(job)`. Its hover names where the numbers come
from: `.help("jobs.succeeded, completions, parallelism, failed and
backoff_limit")`. The card's `meta` already carries the sentence about
the counters counting pods rather than attempts and is unchanged.

`macos/idios/Views/IncidentDetail/EventsCard.swift`. The `LAST_TS (k8s)`
column keeps its 190 points and its one line, and under the stamp goes
`eventSpan(count: event.count, firstTS: event.firstTS, lastTS:
event.lastTS)` in `.tertiary` at 10 points with `.lineLimit(1)` and
`.help("first_ts \(event.firstTS?.raw ?? "null")")`. A row with no span
draws nothing there and keeps the height it has. The `COUNT` column
stays: the span repeats the count in words for the rows that have one,
and the column is what a person scans.

`macos/idios/Views/PodPage/PodPageRail.swift`. CONTEXT's `Node` row
becomes `Text(nodeLine(podNode: pod.nodeName, incidentNode:
lit?.incident.nodeName ?? "", deleted: pod.deletedAt != nil))` with
`.help("pods.node_name, and incidents.node_name where the incident
opened")`. CONTEXT keeps its four rows: Node, QoS class, Image id and
Container id.

`macos/idios/Store/PodPageStore.swift` and
`macos/idios/Views/PodPage/PodCardPane.swift`. The Related tab reads the
detail the page already fetched:

    /// related is the Job's other rows for the Related tab: the lit
    /// incident's own related rows when one is lit, and the Job's incidents
    /// fetched by uid when the page was opened on a pod with nothing lit.
    var related: [Incident]

`related` becomes a computed property over `lit?.relatedIncidents`
through `relatedElsewhere(_:podUID:)` with the page's pod uid, falling
back to the stored `fetchedRelated` rows. `loadRelated(jobUID:
connection:)` keeps its call and writes `fetchedRelated`, but
`PodPageScreen`'s `RelatedKey` task guards on `store.lit == nil` as well
as on `showingRelated`, so the second call is made only for a page that
has nothing lit to read them from. The card's `meta` becomes
`"incidents of this pod's Job, from GetIncident when one is lit"`, which
is where the rows now come from.

Check against the smoke store on 7771 (`make smoke PORT=7771`, then
`./bin/idios -data-dir .storage/smoke -kubeconfig ./kube/config -listen
127.0.0.1:7771 run`), the application started with `-daemon
127.0.0.1:7771`: the crash container's kubelet card says "running - ready"
with "since 09:12 (2h 14m)" under it and its cpu and memory line carries
the parsed value beside every quantity that reads differently; the OOM
container's memory limit shows the byte count; the failing CronJob's job
card says "succeeded 0 of 1 - parallelism 1 - failed 6 of backoff limit
5"; the events table shows "x17 over 42m, first 07:32" under the stamp of
a repeated BackOff and nothing under a single event; the rail's Node row
says the node alone for a pod that never moved; the Job's pod opens
Related incidents with the job row and the retries and makes no second
call while an incident is lit (the daemon's log shows one
`ListIncidents` for the page and none for the tab). Screenshots
`incident/<id>` and `pod/<uid>` through the scratch script for the diff
review.

Checkpoint as task 1.
Commit: `app: draw the node, the resources and the run counters`.

Consumes: `resourceLine`, `millicores`, `runningSinceText`, `nodeLine`,
`jobCounters`, `eventSpan`, `relatedElsewhere`,
`IncidentDetail.relatedIncidents`, `FactRow`, `DetailCard`.
Produces: the kubelet card's parsed resources and running-since line,
the job card's counters, the events table's span, the rail's node line,
the Related tab read from the detail.

## Task 3 - the open count, the subline and the Dock badge

`macos/idios/Store/MenuBarStore.swift`.

    /// openCount is how many incidents are open and unacknowledged: what
    /// the headline says and what the Dock badge carries.
    private(set) var openCount: Int32 = 0

    /// attentionCount is the subline: everything open plus what closed
    /// inside the daemon's window without anyone looking at it.
    private(set) var attentionCount: Int32 = 0

    /// rows are the attention rows the popover folds into problems, up to
    /// the daemon's list limit.
    private(set) var rows: [Incident] = []

    /// acknowledgeShown acknowledges every open, unacknowledged row behind
    /// the popover's rows and answers how many it changed.
    func acknowledgeShown(connection: DaemonConnection) async -> Int

Rules. `loadCounts` reads both states from the one call it already makes:
`counts.byState[.open] ?? 0` and `counts.byState[.attention] ?? 0`. The
open count is the endpoint's own `open`, which does not include the
acknowledged rows, so acknowledging a row takes it off the badge - which
is the point of the badge being open rather than attention.
`loadIncidents` drops `limit: 5` and asks for the daemon's default page
limit, because five rows cannot be folded into problems; the property it
fills is renamed from `newest` to `rows`. `acknowledgeShown` walks
`rows.filter { $0.closedAt == nil && $0.acknowledgedAt == nil }` in order
through an `IncidentWriteStore` the store owns, counts the calls that
answered with an incident, then reloads, so the counts are the daemon's
again rather than a local guess; `.cancelled` is ignored, every other
error goes through the existing `report(_:connection:)`.

The badge is set wherever the count is:

    // The Dock tile is a process-wide object with no owner of its own; the
    // store that holds the count is the one place that can keep it true,
    // and an empty label is how AppKit spells no badge.
    private func showBadge() {
        NSApp.dockTile.badgeLabel = openCount > 0 ? "\(openCount)" : nil
    }

called at the end of `loadCounts`, and once with a zero count when the
connection is unreachable, so a daemon that goes away does not leave a
number behind. The file gains `import AppKit`.

`macos/idios/Views/MenuBar/MenuBarView.swift`.

- `MenuBarLabel` shows `store.openCount` in place of the attention
  count; the glyph keeps its tint rule (red only while a cluster carries
  a `last_error`), because that is a watching failure and not an
  incident count.
- The header becomes three parts on one line: the dot, filled with
  `BadgeStyle.red.text` while `openCount > 0` and `Color.secondary`
  otherwise; `Text(headline)` at 12.5 semibold; `Text(subline)` at 11 in
  `.secondary`; then the cluster summary at the right as today.
  `headline` is `"\(openCount) open"`, or `"Nothing is open"` at zero.
  `subline` is `"\(attentionCount) need attention"`, or `"1 needs
  attention"`, dropped at zero.
- The actions gain the bulk item, above "Status...":

        MenuBarItem(action: { confirmingAcknowledge = true }, shortcut: "a",
            modifiers: [.option])

  labelled "Acknowledge everything shown" with `"Opt A"` at its right in
  the monospaced 11-point style the "Open idios" row uses for its key.
  It is disabled when nothing shown is open and unacknowledged. A
  `.confirmationDialog` on the popover's root names the count
  (`"Acknowledge \(plural(count, "incident"))?"`) and runs
  `store.acknowledgeShown(connection:)`; the popover stays open, because
  a person who acknowledges from here is watching the numbers fall.

Check against the smoke store on 7771: the status item reads the open
count and the Dock tile carries the same number; acknowledging a row in
the main window drops both by one and leaves the attention subline where
it was; the popover header reads "16 open" and "205 need attention";
"Acknowledge everything shown" asks first, then turns every row amber and
empties the headline; quitting the application clears the Dock badge.
Screenshot `menubar` through the scratch script for the diff review.

Checkpoint as task 1.
Commit: `app: count open incidents in the menu bar and the Dock`.

Consumes: `IncidentCounts.byState`, `IncidentWriteStore`,
`DaemonConnection`, `plural`, `BadgeStyle.red`, `MenuBarItem`.
Produces: `MenuBarStore.openCount`, `.rows`, `.acknowledgeShown`, the
Dock badge, the headline and the subline, the popover's bulk item.

## Task 4 - the popover's rows are problems

`macos/Sources/IdiosModel/MenuBar.swift`:

    /// menuBarTail is the line under the rows the popover could not draw:
    /// how many problems are left and, when every one of them is closed,
    /// the window they closed inside, read from the daemon rather than
    /// written here.
    public func menuBarTail(hidden: Int, allClosed: Bool, windowSeconds: Int32?) -> String?

Rules. nil at `hidden` zero. `"\(plural(hidden, "more problem"))
closed in the last \(humanDuration(seconds: windowSeconds))"` when
`allClosed` and the window is known; `"\(plural(hidden, "more problem"))
closed"` when `allClosed` with no window yet, because a sentence that
invents 24 hours is worse than one that leaves the window out;
`"\(plural(hidden, "more problem"))"` otherwise.

`macos/Sources/IdiosModel/Cluster.swift`:

    extension ClusterScope {
        /// including widens a narrowed scope to hold one more cluster; the
        /// all scope already holds every one and is returned unchanged.
        public func including(_ clusterID: String) -> ClusterScope
    }

`macos/idios/App/Route.swift`:

    /// GroupTarget is a workload group a menu bar row asks the list to
    /// show: the group's id as the list computes it, and the cluster it is
    /// in, because a narrowed scope has to widen before the group exists.
    struct GroupTarget: Hashable {
        let id: String
        let clusterID: String
    }

`Navigator` gains `private(set) var group: GroupTarget?`,
`private(set) var groupSerial = 0` and:

    /// open asks the main window to show one workload group in the list.
    func open(group: GroupTarget) { self.group = group; groupSerial += 1 }

`macos/idios/Views/MenuBar/MenuBarView.swift`. The `incidents` section
draws groups instead of rows:

    // The list's own grouping function folds the popover's rows, so a
    // CronJob failing every two minutes is one line in both places and the
    // two can never disagree about what one problem is. The menu bar
    // ignores the cluster scope, so the cluster count it passes is the
    // number of clusters the daemon reports.
    private var groups: [IncidentGroup]

is `incidentGroups(rows: store.rows, grouping: .workload, cluster: { id
in store.clusters.first { $0.id == id } }, selectedClusters:
store.clusters.count, facts: { _ in nil }).flatMap(\.groups)`, sorted
with the groups that have an open incident first and the served order
kept inside each half. `shown` is `groups.prefix(5)`; `hidden` is the
rest; the tail is `menuBarTail(hidden: hidden.count, allClosed:
hidden.allSatisfy { $0.openCount == 0 }, windowSeconds:
attentionWindowSeconds)`, drawn as a `MenuBarItem` that opens
`.incidents(.attention)` when it is there.

    /// groupRow is one problem: its state dot, the workload it is about,
    /// the plain-language summary the list's header uses and the newest
    /// time under it.
    private func groupRow(_ group: IncidentGroup) -> some View

One line: a 7-point `Circle` in `groupBadge(group.rows).tone.badge.text`,
so a closed group is grey by the one colour rule; the title in 12 points
(`"\(group.kind) \(group.title)"` prefixed with the cluster name and a
`" / "` while `store.clusters.count > 1`, which is what `group.meta`
already carries); `Text(group.summary)` in `.secondary` at 11 points
after a middle dot separator, truncated to one line; then the newest time
at the right in monospaced 11. The whole line and the badge's text are
the row's `.help`. A click calls `navigate` with nothing to navigate to,
so `MenuBarView` gains one more closure beside `open`:

    /// openGroup asks the main window to show one group in the list; the
    /// popover closes behind it as every other navigation does.
    let openGroup: (GroupTarget) -> Void

wired in `IdiosApp` to `navigator.open(group:)` followed by the same
window raising `openMain` does; the popover's own `dismiss()` rule is
unchanged, so the screenshot window is not closed under the capture.

`macos/idios/Views/Incidents/IncidentsScreen.swift` answers it:

    // A menu bar row names a group the list computes, so the list has to be
    // showing the view and the grouping that produce it before the id means
    // anything; the scope widens for the same reason, because the menu bar
    // ignores it and the list does not.
    .onChange(of: navigator.groupSerial) {
        guard let target = navigator.group else { return }
        screen = .incidents
        path = []
        filter = IncidentFilter(state: .attention)
        preferences.grouping = .workload
        preferences.scope = preferences.scope.including(target.clusterID)
        expansion.collapsedGroups.remove(target.id)
        selection = target.id
        revealed = target.id
    }

with `@State private var revealed: String?` passed to `IncidentsList` as
`let reveal: String?`.

`macos/idios/Views/Incidents/IncidentsList.swift`. The `List` is wrapped
in a `ScrollViewReader` and gains

    .onChange(of: reveal) { _, id in
        guard let id else { return }
        withAnimation { proxy.scrollTo(id, anchor: .center) }
    }

Only a reveal scrolls: a selection a person made with the arrow keys is
already where they are looking, and scrolling on every selection change
fights the list's own scrolling.

Tests.

- `menuBarTailCountsTheProblemsItCouldNotDraw` in `MenuBarTests.swift`
  (traces to 9.3's Menu bar extra row, "then 'N more problems closed in
  the last 24 h'"): rows `(0, true, 86400) -> nil`, `(2, true, 86400) ->
  "2 more problems closed in the last 24 h"`, `(1, true, 86400) -> "1
  more problem closed in the last 24 h"`, `(2, true, nil) -> "2 more
  problems closed"`, `(3, false, 86400) -> "3 more problems"`,
  `(2, true, 600) -> "2 more problems closed in the last 10 min"`.
- `aWidenedScopeHoldsTheClusterItWasGivenAndKeepsAll` in
  `ClusterTests.swift` (traces to 7.1's "the menu bar extra ignores the
  selection", which is what makes a menu bar row point at a cluster the
  screens have filtered out): rows for the all scope (unchanged), a
  selection that already holds the cluster (unchanged), a selection that
  does not (the cluster added), and the empty selection (one cluster).
  Whole `ClusterScope` values compared.

Check against the smoke store on 7771, with the application launched
`-grouping category` so the reveal has a grouping to change: the popover
shows one row per problem, the failing CronJob's 56 job rows among them
as one line reading "CronJob nightly" with "every 2m, 62 of 63 runs
failed, since 07:32" beside it; a closed problem's dot is grey; the tail
line reads "2 more problems closed in the last 24 h" and its 24 h comes
from the daemon (change `attention_window` in the config, restart the
7771 daemon, and the sentence follows); clicking a row raises the window
on Attention grouped by workload with that group open, selected and
scrolled into view, even when the scope was narrowed to another cluster
before the click. Screenshots `menubar` and `incidents` through the
scratch script for the diff review.

Checkpoint as task 1.
Commit: `app: fold the menu bar's rows by workload`.

Consumes: `incidentGroups`, `IncidentGroup`, `groupBadge`,
`StateTone.badge`, `groupSummary` through the group, `plural`,
`humanDuration`, `Navigator`, `ListExpansion`, `Preferences.scope` and
`.grouping`, `StatusStore.status?.attentionWindowSeconds`.
Produces: `menuBarTail`, `ClusterScope.including`, `GroupTarget`,
`Navigator.open(group:)` and `groupSerial`, the popover's group rows and
tail, the list's reveal and its scroll.

## Task 5 - Status in the system font and in human units

`macos/idios/Views/Status/StatusScreen.swift`.

- The header's three lines lose `design: .monospaced`:
  `connectionLine` and its unreachable twin take `.system(size: 12)`,
  `ageLine` takes `.system(size: 11)` and keeps `.monospacedDigit()`, so
  the seconds still do not jitter as they turn over. The address stays
  selectable and keeps its own monospaced `Text` inside the line, since
  it is an identifier a person copies and 7.4's identifier rule holds;
  everything else in the header is prose.
- `StorageCard`'s private `seconds(_:)` is deleted and every interval
  fact takes `humanDuration(seconds:)`, which is the function the legend
  popover already reads the attention window with, so the window a
  person sees in the legend and the one on this screen are the same
  string. `retention` keeps `"\(status.retentionDays) d"`: it is
  configured in days and is already in its own unit.
- Each interval keeps its `.help` naming the configuration key, which is
  now the only place the value in seconds can be read; the hover becomes
  `"\(key) = \(value) s"`, because a person changing the config file
  writes seconds and the screen no longer shows any.

The legend popover's link to this screen is already in place
(`LegendPopover` takes `openStatus` and `IncidentsSidebar` passes it):
decision 9's "linked from the legend popover" is checked here, not
rebuilt.

Check against the smoke store on 7771: the Status header reads in the
system font with the address alone monospaced; the storage card reads
"sweep interval 1 h", "attention window 24 h", "early capture debounce
10 s", "retention 3 d", and every one of them names its key and its
seconds on hover; the legend's "Status..." button opens this screen and
its attention sentence reads the same "24 h". Screenshot `status`
through the scratch script for the diff review.

Checkpoint as task 1.
Commit: `app: put Status in the system font and human units`.

Consumes: `humanDuration`, `DaemonStatus`, `FactRow`, `DetailCard`.
Produces: the system-font header, the human-unit intervals, the seconds
on the hover.

## Task 6 - the README's screenshots

The four images `README.md` shows are `docs/screenshots/incidents.png`,
`incident-detail.png`, `pod.png` and `workloads.png`. Every one of them
predates steps B, C, D and E, so every one is retaken, in the same order
and at the same routes, from the same data.

Preparation, in this order:

1. macOS appearance is set to Light: the README's images are light-mode
   and the application follows the system setting.
2. `make smoke PORT=7771` fills `.storage/smoke`, then
   `KUBECONFIG=./kube/config kubectl -n idios-smoke apply -f
   hack/smoke/` reopens the incidents the smoke run closed, so the list
   shows open problems rather than a screen of grey.
3. `./bin/idios -data-dir .storage/smoke -kubeconfig ./kube/config
   -listen 127.0.0.1:7771 run` serves them. The installed application's
   daemon keeps 7770 and is not touched.
4. `screencapture -x /tmp/idios-probe.png` proves Screen Recording
   permission; stop if it fails.

Then, with `IDIOS_DAEMON=127.0.0.1:7771` exported for each:

    hack/macos/screenshot.sh incidents docs/screenshots/incidents.png
    hack/macos/screenshot.sh incident/<id> docs/screenshots/incident-detail.png
    hack/macos/screenshot.sh pod/<uid> docs/screenshots/pod.png
    hack/macos/screenshot.sh workloads docs/screenshots/workloads.png

`<id>` is a crash incident with several restarts and captured logs, so
the verdict block, the scrubber and the timeline all have something to
show; `<uid>` is that incident's pod, so the Pod card's events and
conditions are filled. Both are read from the running daemon with
`./bin/idios -data-dir .storage/smoke incidents` rather than guessed.

`README.md` changes only where a sentence no longer describes its image:
the paragraph above `workloads.png` says the workloads screen
"aggregates incidents per Deployment, CronJob or bare pod, and a menu bar
item keeps the open count and the latest incidents one click away" - the
menu bar's rows are problems now and its count is the open one, so the
clause becomes "and a menu bar item keeps the open count and the problems
behind it one click away". The sentence above `incidents.png` and the two
about the pod page are read against the new images and left alone unless
they say something the image no longer shows. No new image is added and
none is removed; `hack/ascii-check` skips `.png` and the README is
checked as text.

Check: the four images open in Preview at the window's own size, in
light appearance, with no other application's window in them and no
personal data on screen; every workload, namespace and node visible in
them belongs to the smoke fixtures. `git diff --stat` shows the four
images and `README.md` and nothing else.

Checkpoint as task 1, plus `make ascii` over `README.md`.
Commit: `docs: retake the README screenshots`.

Consumes: `hack/macos/screenshot.sh`, `make smoke`, `hack/smoke/`.
Produces: the four screenshots and the README sentence that names what
the menu bar now holds.

## Task 7 - verification with the user and the status line

The user runs the application against the smoke store on 7771 and walks
the glance and the pod page: the status item's number against the list's
Open count; the Dock badge following it as rows are acknowledged and
reopened; the popover's headline, subline and cluster lines; its rows as
one line per problem with the CronJob folded, a grey closed problem, the
tail sentence and its window; a click on a row landing on that group in
the list with the scope widened when it had to be; "Acknowledge
everything shown" asking first and then emptying the headline; the pod
page's running-since line, its parsed cpu and memory, the job counters,
the event span, the rail's node line and the Related tab of a Job's pod;
Status in the system font with every interval in human units and the
legend's link into it. Anything the review turns up is fixed in the task
that owns it and committed as a new commit, never by rewriting one.

Then `docs/plans/m10-reading/roadmap.md`'s step E line becomes "complete
<date>". The milestone's own status line and the `docs/plans/README.md`
row for `m10-reading/` are the user's call, not this task's: closing the
milestone is a separate step with its own report.
Commit: `docs: close m10 step E`. The 7771 daemon is stopped, the Debug
build is quit so no stale Dock badge is left behind, and `.storage/smoke`
is left as it is for the next run.

## Hands to the next step

There is no next step in m10: E is the last. The milestone's closing
report should record, for whoever reads it after the plans are frozen:

- What the ten decisions cost and what they left. Every decision but
  one is in the code; the recorded fallback of decision 2 (one muted hue
  per category family) was never tried, because the list did not read
  flat after step B. It stays a recorded fallback and not a plan.
- The seams the steps built that outlive the milestone, so a later
  milestone finds them: `groupEntries`, `groupSummary` and `groupBadge`
  as the one fold two screens share; `incidentGroups` as the one
  grouping function, now with a second caller; `runCells` and
  `runTableRows` for the runs; `verdictSentences` and `timelineFolds`
  for the pod page; `SearchQuery` and `searchResults` for the palette;
  `humanDuration` as the one place a configured number of seconds
  becomes words; `BadgeStyle` as the one place colour lives.
- The one daemon change of the milestone: the exact `pod_name` filter on
  `GET /incidents` and `GET /pods` (step C), which is the only thing
  a client outside this repository would notice.
- What is left undone and why, from the doubts of all five step plans,
  so that the next milestone starts from them rather than rediscovering
  them. The doubts below are step E's contribution.
- Anything in `docs/design/presentation.md` that step A wrote and the
  code did not end up matching. The design doc is not scaffolding: where
  it and the code disagree, one of them is fixed before the milestone is
  called complete.

## Self-review

Spec coverage. 4.7 whole: task 3 (the headline is the open count from
`GET /incidents/counts`, the Dock badge follows it, the subline is the
attention count, the rows come from `state=attention` with the list
limit and no separate endpoint) and task 4 (folded client-side into one
row per workload group, the same fold as the list's, a closed group grey,
a row opens that group in the list; the cluster dots keep their own
call). The Menu bar extra row of 9.3: task 3 (Open idios, Acknowledge
everything shown, Status..., Clusters and namespaces...) and task 4 (each
row leading with its cluster name while more than one cluster exists,
then "N more problems closed in the last 24 h"). The Status row of 9.3:
task 5 (the system font, human units, and the legend's link, which is
checked rather than rebuilt). The Pod page row of 9.3: task 2 (Related
incidents for a Job's pod, the rail's Node, and the events table). 4.2:
task 1 and 2 (`node_name` is the node at open, so the line says both when
they differ). 4.5: task 1 (`succeeded` is the condition's, and the
counters are context, which is why `jobCounters` never says the outcome).
7.4: task 2 (the identifier in the Status header keeps its monospaced
`Text`, and every new number names its source on the hover rather than in
visible text). 7.6: task 1 (`nodeLine` says "at open; the pod is no
longer placed" rather than dropping the node). 9.5: task 4 (a group's dot
is `groupBadge(...).tone.badge`; no colour is added). 9.6: task 5 (the
legend and Status read one attention window through one function).
Decision 10: every name in this plan, its tests and its checks is
invented or a smoke fixture's.

Split. Seven tasks, five of them code. The model is one commit rather
than three because its six functions are one logical change - the fields
the model decoded and no view read - and every one of them is drawn by
task 2, which would otherwise be three commits that cannot be reviewed
apart. The menu bar is two commits rather than one because the counts and
the badge are true whatever the rows look like: task 3 can ship and be
used while task 4 is still being drawn, and a single commit would mix a
number with a navigation path. Task 5 is its own commit because Status
shares nothing with the popover but `humanDuration`. Task 6 is its own
commit because a commit that mixes four binary images with Swift is a
diff nobody can read.

Doubts. Decision 9 says the menu bar's rows are "the same fold as
decision 1", and this plan reads that as the whole of decision 1: the
group fold and, inside a CronJob's group, the run fold, because
`incidentGroups` computes both and taking half of it would need a second
code path. The popover draws only the group line, so the run fold is
invisible there and shows only in the badge's count and in the summary;
if the intent was that the popover fold runs into rows of their own, that
is a change to this task and not a deviation elsewhere. -- The menu bar
has no `GroupFactsStore`, so `groupSummary` gets nil facts and a CronJob
row reads "9 runs failed" rather than "62 of 63 runs failed", and a
Deployment row reads "3 pods looping" rather than "3 of 3 pods looping".
Loading `/workloads` and `/jobs` for a popover that is open for four
seconds is a second and a third call on every open; the plan takes the
weaker sentence. The mockup's frame draws the stronger one. -- "A row
opens that group in the list" has no `Route` case, because a group is not
an addressable screen: its id is a value the list computes from the rows
it happens to hold. The plan adds a `Navigator` request rather than a
`Route`, and makes the reveal set the view to Attention and the grouping
to workload so the id exists at all; a person who was on Open grouped by
namespace therefore loses that view to the click. Setting the grouping
was the only way to guarantee the row lands somewhere, and it is a
decision the roadmap does not make. -- The same reveal widens a narrowed
cluster scope, because the menu bar ignores the scope (7.1) and a row
that led nowhere would be worse than a scope that grew by one. Nothing in
decision 9 asks for it. -- The headline is the endpoint's `open`, which
excludes acknowledged rows, so acknowledging empties the badge while the
problem is still open. 4.7 and the mockup both say open, and the
acknowledged rows stay in the subline's attention count; if the badge is
wanted to hold acknowledged rows too, it is a change to 4.7 first. --
"Human units (24 h, 10 min)" is read as the shipped `humanDuration`,
which answers in the coarsest unit that divides the value exactly: 5400
seconds reads "90 min" and not "1 h 30 min", and a value that divides by
nothing stays in seconds. Retention stays in days. -- The Related tab
keeps its `job_uid` call for the case where no incident is lit, because
`related_incidents` only arrives with a `GetIncident`, and a Job's pod
opened from Workloads has nothing lit. Two sources for one tab is a seam
this plan accepts rather than hide; the alternative, fetching an incident
the person did not ask for, is a call for a tab that may never be opened.
-- Whether the README should gain a fifth image of the menu bar is not
decided here: the roadmap says the screenshots are retaken, not that one
is added, so the plan retakes four.

`.ai` rules. ascii-only: no symbol in any code block or string; the key
cap is written "Opt A", the separators are "-" and "/", and
`hack/ascii-check docs/plans/m10-reading/glance.md` is run before this
file is finished. tests: every test above names the sentence it traces
to, is table-driven, compares whole values, and leads with the edge cases
(the row with no pod uid, the pair with nothing written, the event seen
once, the event whose first equals its last, the container that is not
running, the Job with no completions, the tail with no window, the scope
that is already all); no test asserts a view, a constant or a store.
comments: every comment in a code block says why - the Dock tile's
ownership, the reveal's grouping, the scroll that only a reveal fires.
code-is-truth: no code block, comment or test name mentions a document, a
section or this plan. scope: no proto change, no new endpoint, no second
call for the popover's facts, no `Route` case, no colour added to
`BadgeStyle`, no fifth screenshot, no milestone-closing edit to
`docs/plans/README.md`. commits: seven subjects under 72 characters, one
`model:`, four `app:`, two `docs:`; no `make generate` anywhere, because
nothing under `api/proto` moves.

Type consistency. `IncidentDetail.relatedIncidents` (task 1) is
`[Incident]`, the same row type the list, the palette and the menu bar
carry, so `relatedElsewhere` (task 1) hands the Related tab's card (task 2)
what it already draws. `resourceLine` (task 1) takes the written strings and
the parsed values that sit beside each other on `Container`, so
`KubeletCard` (task 2) passes fields and not a computed shape.
`jobCounters` (task 1) takes `Job`, which is what `JobCard` (task 2)
already holds and what `runCells` (step D) reads. `eventSpan` (task 1)
takes the three fields of `Event` the table already draws two of.
`nodeLine` (task 1) takes `String?` and `String` because a pod's node is
optional and an incident's is not, which is how the two are stored.
`IncidentGroup` (step B, the application) is what task 4's popover rows
and the incidents list both draw, and `GroupTarget.id` is
`IncidentGroup.id`, so the reveal and the list agree by construction.
`ClusterScope` (existing) gains one non-mutating function and stays a
value. `menuBarTail` (task 4) returns `String?`, which is how every
optional line in the popover is already drawn. `humanDuration` (step B)
is the one function behind the legend's window (step B), the popover's
tail (task 4) and Status's intervals (task 5).
