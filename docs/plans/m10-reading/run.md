# m10 step F - the run: one page per Job

Goal: after this plan a run is a thing a person can open. One screen
answers "what happened to this run", keyed by the Job's uid, reached from
the incidents list, the Workloads Runs table, the run strip, the pod
page's breadcrumb and rail, the Cmd-K palette and every job-subject
incident. It says in its title whether it is a scheduled run of a CronJob
or a standalone Job, leads with the Job's condition and a verdict built
from what the page holds, lists one line per attempt with the pod behind
it, and keeps the actions the pod page has. The pod-less job page goes:
the Run page replaces it, with or without a pod. A job-subject incident
stops borrowing a container's segment strip on the pod page, so the pod
page's counts and segments are the pod's own again. The incidents list
draws its children as children rather than as a flat run of lines. The
legend learns the kinds, so the words CronJob, Job, run, pod, container
and incident have one definition in the application. And the events table
stops wrapping one character per line in a narrow pane.

One proto change, the smallest that makes the page reachable: a `job_uid`
filter on `ListJobs`. The Run page is keyed by a Job uid and there is no
way to read a Job by uid today - `GetJob` does not exist, `ListJobsRequest`
carries `cluster_ids`, `namespace`, `cronjob_uid`, `cronjob_name`, `live`,
`failed` and `limit`, and `query.jobScope` has no uid predicate. The Job's
row reaches the application only as `IncidentDetail.job`, which needs an
incident, and a run that completed has none. One field, one predicate, one
handler line. Everything else the page needs is already served:
`ListIncidents?job_uid=` is a first-class filter (`incidents.proto` field
8, `query.IncidentFilter.JobUID`) and returns the job-subject row and
every pod row of the run, because `incidents.job_uid` is stamped on a
Job's pod incidents too; `GetIncident` on one of those rows carries that
attempt's `containers`, `artifacts` and `events`; `ListJobs?cronjob_name=`
already serves the other runs of the CronJob. `make generate` and
`make generate-check` run once, in the task that changes the proto, and
`rm -rf macos/.build` precedes the next `make app-test`.

What is deliberately not added: no endpoint that lists a Job's pods.
`ListPodsRequest` has no controller filter and `pods.controller_uid` has
no index, and the Attempts card does not need one - every line it draws
carries a category, a reason and an incident, so every line it draws is
an incident of the run, which `ListIncidents?job_uid=` already answers.
An attempt that succeeded opened no incident and is counted, not drawn.

Architecture: what can be decided without a view is decided in
`IdiosModel` as a pure function with a table test - the page's title, its
state tag, its counts line, its verdict sentences, the attempt rows, and
which route an incident opens. The application draws them: a new
`macos/idios/Views/RunPage` folder with the screen, its header, the
Attempts card and its rail, over a new `RunPageStore` that owns every
call; `IncidentsScreen` routes to it; `PodPageScreen` and `PodPageStore`
lose the job-subject special cases they carried for the page that is
going away. The list's nesting is `IncidentsList` and `ListColumns`
alone. The legend's Kinds section is `LegendPopover` over one new
vocabulary constant. Views never import `IdiosAPI`; stores own every
client call and stream.

Tech stack: Swift 6 package `macos/Sources/IdiosModel` with tests in
`macos/Tests/IdiosModelTests` (`make app-test`); the Xcode application
under `macos/idios` (`make app`; a file under `macos/idios/` joins the
target by existing, so the new folder needs no project edit). SwiftUI for
every view, reusing `DetailCard`, `FactRow`, `Badge`, `CategoryBadge`,
`RailHeading`, `RailRow`, `chainLink`, `IncidentTimesRail`,
`VerdictBlock`, `EventsCard`, `JobCard`, `CapturedLogsCard`,
`TimelineView`, `PaneActionsRow` and `RunStripView`, so the Run page is
the pod page's furniture rearranged and not a second design. Go 1.26 and
`modernc.org/sqlite` for the one daemon task; sebuf regenerates
`internal/apigen` and `api/openapi` from `api/proto`.

Spec: `docs/design/presentation.md` section 9.3 - the Incidents row (the
run fold, the group header shape, the row anatomy), the Pod page row (the
pod-less Job page this step replaces, the breadcrumb, the segmented
control, the events table's wrap rule), the Workloads row (the Runs
table, the run strip) - and 9.6 whole (the vocabulary and the legend).
4.5 (`succeeded` is read from `condition_type`, never from the counters).
7.3 (the identity chain), 7.4 (no value is silently cut; a card's
provenance is its title's hover), 7.6 (what idios does not know is said,
not hidden), 9.4 (the prose tables are custom rows with fixed column
budgets, and `Table` on macOS 15 clips rather than shrinks), 9.5 (the
colour rule). `docs/design/data-storage.md`: the `jobs` table rows, and
`pods.controller_uid` as the Job that owns an attempt. Roadmap decision
11 of `docs/plans/m10-reading/roadmap.md`, added by
`roadmap-stepF.patch.md` before this step starts. Visual reference: pages
2 and 5 of `docs/mockups/idios-ui.html`.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Swift follows the Go rules: one-line doc comment per
public type and function, comments say why, tests trace to a spec
statement with whole-value assertions, variants are table rows. A new
`Route` case is parsed by `Route(path:)` and covered in `RouteTests`.
Every count in the application is a count of incidents, and the Run page
is no exception: its counts line says attempts and incidents in separate
words and never adds them. The only hard-coded colours live in
`BadgeStyle.swift`; the list's guide rail, tint and rule are system
materials (`.quaternary`, `.tertiary`), which are not colours of ours,
and nothing is added to that file. Every time in the application is UTC,
as stored.

The word run means one Job created by a CronJob. A standalone Job is a
Job and never a run; the page is the same screen and says so in its
title. Nothing in a test, comment, doc or fixture names a real
organisation, cluster, namespace, workload, image or node. The invented
names this plan and its tests use are `checkout-api`, `report`,
`nightly`, `worker`, `idios-smoke`, `api` and `node-a`; the smoke
fixtures it checks against are `smoke-cron-fail`, `smoke-cronjob-retry`,
`smoke-crash`, `smoke-oom` and `smoke-bare` in namespace `idios-smoke`.

The application takes the daemon as a launch argument: the Debug build is
started with `nohup macos/DerivedData/Build/Products/Debug/idios.app
/Contents/MacOS/idios -daemon 127.0.0.1:7771 &` after `make app`
(`IDIOS_DAEMON` is read only by `hack/macos/screenshot.sh`). That build
shares its bundle id and its `UserDefaults` with the installed
application: never `defaults write` that domain; override a preference
for one run with an argument-domain flag (`-grouping workload
-density comfortable`).

The production daemon on 127.0.0.1:7770 is never touched, read or
screenshotted; every check and every screenshot in this step runs against
a private daemon on 127.0.0.1:7771. `make smoke PORT=7771` fills
`.storage/smoke` and `KUBECONFIG=./kube/config kubectl -n idios-smoke
apply -f hack/smoke/` reopens its incidents; then
`./bin/idios -data-dir .storage/smoke -kubeconfig ./kube/config -listen
127.0.0.1:7771 run` serves them. `smoke-cron-fail` is a CronJob on
`*/1 * * * *` with `backoffLimit: 1` whose container exits 1, so every
run has two attempts and the Run page has something to list;
`smoke-cronjob-retry` is a standalone Job with `backoffLimit: 2` whose
first attempt fails and whose retry succeeds, which is the standalone
title, the Complete tag and the run that ends well. Before any
screenshot run `screencapture -x /tmp/idios-probe.png` and stop if it
fails: the terminal needs Screen Recording permission. Screenshots for
the diff review go through a scratch script that starts the application
with `-screenshot <png> -route <route> -daemon 127.0.0.1:7771`, waits for
"idios-window <n>" on stdout, sleeps 2.5 s, runs `screencapture -x -o -l
<n> <png>` and kills the application.

The mock daemon ignores every list filter and its job-subject incident
carries no `job_uid`, so no Run page opens against `idios mock`; every
check in this step runs against the smoke store. That is why the store
matches the Job by uid inside whatever `ListJobs` answered rather than
trusting the filter to have been applied: a mock that ignores the query
then shows no Job instead of the wrong one.

Checkpoint before every commit: `go build ./... && go test ./... &&
make ascii && make app-test && make app`, plus `make generate-check`
after the commit of the one task that changes `api/proto`. Implementers
never commit; nothing is committed without the user's review of the diff.
The decisions are made; where a frame and a sentence differ, the sentence
of 9.3 wins and the doubt goes in the self-review.

## File structure

    macos/idios/Views/IncidentDetail/EventsCard.swift   the columns that give way (task 1)
    docs/design/presentation.md                         9.3's Run page row, 9.6's kinds (task 2)
    api/proto/idios/v1/jobs.proto                       ListJobsRequest.job_uid (task 3)
    internal/query/workloads.go                         JobFilter.UID (task 3)
    internal/query/workloads_test.go                    the uid predicate (task 3)
    internal/api/workloads.go                           the handler mapping (task 3)
    internal/api/workloads_test.go                      the filter reaching the query (task 3)
    api/openapi, internal/apigen                        regenerated (task 3)
    macos/Sources/IdiosModel/Route.swift                run(String), incidentRoute (task 4)
    macos/Sources/IdiosModel/RunPage.swift              the title, the tag, the counts, the attempts, the verdict (task 4)
    macos/Tests/IdiosModelTests/RunPageTests.swift      all of it (task 4)
    macos/Tests/IdiosModelTests/RouteTests.swift        the run route and incidentRoute (task 4)
    macos/idios/Store/RunPageStore.swift                the page's calls (task 5)
    macos/idios/Views/RunPage/RunPageScreen.swift       the screen and its tabs (task 5)
    macos/idios/Views/RunPage/RunPageHeader.swift       the tag, the counts, the breadcrumb (task 5)
    macos/idios/Views/RunPage/AttemptsCard.swift        one line per attempt (task 5)
    macos/idios/Views/RunPage/RunPageRail.swift         the chain, the context, the times, the other runs (task 5)
    macos/idios/Views/Workloads/RunStripView.swift      the marked cell (task 5)
    CLAUDE.md                                           the screenshot route list (task 5)
    macos/idios/Views/PodPage/JobOnlyPage.swift         deleted (task 6)
    macos/idios/Views/PodPage/PodPageScreen.swift       the redirect, the segments (task 6)
    macos/idios/Store/PodPageStore.swift                resolve answers a run (task 6)
    macos/idios/Views/Incidents/IncidentsScreen.swift   the routes into the run (task 6)
    macos/idios/Views/PodPage/PodPageHeader.swift       the Job segment of the breadcrumb (task 6)
    macos/idios/Views/PodPage/PodPageRail.swift         the Job dot opens the run (task 6)
    macos/idios/Views/Workloads/WorkloadDetailView.swift the Runs table and strip open the run (task 6)
    macos/Sources/IdiosModel/Search.swift               a run hit opens the run page (task 6)
    macos/idios/Views/Incidents/ListColumns.swift       childIndent, the guide (task 7)
    macos/idios/Views/Incidents/IncidentsList.swift     the nesting (task 7)
    macos/Sources/IdiosModel/Vocabulary.swift           kindDefinitions, ownerChains (task 8)
    macos/Tests/IdiosModelTests/VocabularyTests.swift   the chains (task 8)
    macos/idios/Views/Incidents/LegendPopover.swift     the Kinds section (task 8)
    docs/plans/m10-reading/roadmap.md                   step F's status line (task 9)

## Task 1 - the events table fits a narrow pane

`macos/idios/Views/IncidentDetail/EventsCard.swift`. On the pod page with
the sidebar open the middle pane is about 650 points wide. The card's
columns are fixed at 62 + 124 + 96 + 44 + 190 + 74 with six 10-point
gaps, which is 650 before MESSAGE gets a point: the message column
collapses, `WrapText` wraps it one character per line, and a row with a
two-line kubelet message is hundreds of points tall.

The columns that are prose give way and the one that is a timestamp does
not. `ListColumns` is the incidents list's budget and is not touched;
this card gets its own:

    /// EventColumns is the card's column budget: the three prose columns
    /// give way in a narrow pane, MESSAGE keeps a floor, and the timestamp
    /// column is fixed because a stamp that wraps is unreadable.
    private enum EventColumns {
        static let type: CGFloat = 62
        static let reason = (min: 74.0, ideal: 124.0, max: 124.0)
        static let container = (min: 60.0, ideal: 96.0, max: 96.0)
        static let count: CGFloat = 44
        static let stamp: CGFloat = 190
        static let source = (min: 48.0, ideal: 74.0, max: 74.0)
        static let message: CGFloat = 180
    }

Rules. `heading` and `row` both take the same treatment, so the columns
stay on each other: REASON, CONTAINER and SOURCE become
`.frame(minWidth:idealWidth:maxWidth:alignment:)` over those triples;
TYPE, COUNT and LAST_TS keep the fixed widths they have; MESSAGE becomes
`.frame(minWidth: EventColumns.message, maxWidth: .infinity, alignment:
.leading)`. The stamp column keeps its `.lineLimit(1)` on both lines, and
the span under it keeps its own. The reason cell gains `.lineLimit(1)`
and `.truncationMode(.middle)` with `.help(event.reason)`, because a
reason that no longer fits is an identifier and 7.4 says nothing is
silently cut. The card is the only caller of these widths and the enum is
private to the file.

Two-line rows - the stamp and the source on a line of their own above the
message - are the alternative not taken: they double the height of every
row in the wide pane to fix the narrow one, and the events table is the
one place a person scans a column of reasons.

No test: this is a layout budget, and `.ai/tests.md` rule 5 keeps a test
off a constant. The check is the pane.

Check against the smoke store on 7771, the application started with
`-daemon 127.0.0.1:7771`: open `smoke-crash`'s pod page with the sidebar
showing, select the app container, Overview - the events table reads with
one line per short message, a long BackOff message wraps into a column
several words wide, no row is taller than its message needs, and no
timestamp wraps. Widen the window to full screen and the message column
takes the space. Narrow it until the split view stops: the reason column
truncates in the middle with the whole reason on hover, and the table
still reads. The same table on the Pod card's Events tab, which adds the
CONTAINER column, behaves the same. Screenshot `pod/<uid>` through the
scratch script for the diff review.

Step E's task 6 retakes `docs/screenshots/pod.png` after this task: the
image it produced shows the collapsed message column.

Checkpoint: `go build ./... && go test ./... && make ascii &&
make app-test && make app`.
Commit: `app: let the events table give way to the message`.

Consumes: `EventsCard`, `WrapText`, `Badge`, `eventSpan`.
Produces: the card's flexible columns and its message floor.

## Task 2 - the spec learns the run and the kinds

`docs/design/presentation.md`, section 9.3. The Pod page row's two
sentences about the page a Job's incident gets when no pod is kept ("A
Job's incident with no pod to open ... the keys act on the incident as
anywhere else.") are replaced by one sentence pointing at the new row:
"An incident whose subject is a Job opens the Run page, whether or not a
pod of the Job is kept, so a job-subject incident never joins a
container's segments and the page's counts line and segments are the
pod's own." The sentence after them, about the list row's container
column reading "job" and its Restarts column "-", stays: it is about the
list, not the page.

9.3 gains one row, after the Pod page row and before Workloads:

| Run page: one screen for one Job, keyed by the Job's uid (`run/<job uid>`), reached from a run row in the incidents list, the job row inside a run, a run in the Workloads Runs table, a run strip cell, the pod page's breadcrumb Job segment and its rail's Job dot, the Runs section of the palette, and every job-subject incident. The title is `Run <suffix> of <CronJob name>` when a CronJob created the Job and `Job <name>` when nothing did; the word run means a Job a CronJob created, and a standalone Job is never called one. The header is the Job's condition as the state tag (`FAILED, BACKOFF LIMIT EXCEEDED`, `COMPLETE`, `RUNNING`, `DELETED`, `NOT RECORDED` when the sweeper removed the jobs row), a counts line of attempts, incidents and how many open, and how long the run ran, and the identity line cluster / namespace / CronJob as a breadcrumb. Then a verdict block of sentences built from the fields the page holds: how it ended and after how many attempts in how long, the backoff limit against the failed counter, the exit codes among the attempts, whether every attempt captured a log and the last line of the newest one, the image tag, and how many earlier runs of the same CronJob failed for the same reason. Acknowledge is the one primary button with its key shown and acts on every incident of the run, Note is beside it, and the rest are an Actions menu, as the pod page has. Then Overview (the Attempts card, the Job card, and for a CronJob's Job the run strip with this run marked), Timeline (the lead incident's window), Logs (the captured files of every attempt as one scrubber) and Events (the Job's events and its attempts'). The Attempts card is one line per attempt, oldest first: the attempt number, the pod's suffix, the category chip, the exit code and reason, the time, the incident's id and state, and a link into that pod's page on its Logs; the Job's own row closes the card when the Job itself failed. An attempt that succeeded opened no incident and is counted in the header, not drawn. The rail is the owner chain (CronJob, Job, the attempts' pods), context (node, schedule, image, backoff limit), the times (scheduled, first attempt, finished, open for) and the other runs of the CronJob with their state pills. `Copy uid` copies the Job's uid, and the keys act on the run's lead incident | `/jobs?job_uid=`, `/incidents?job_uid=`, `/incidents/{id}`, `/incidents/{id}/timeline`, `/jobs?cronjob_name=`, `/artifacts/{id}/content`, the incident writes of Section 8 |

The Incidents row gains one clause where it describes the group header
and its rows, after "Expansion state of groups, runs and rollups persists
per group id across the window's life and across a grouping change.": "A
row under a group header is drawn as its child: indented, with a neutral
guide rail from the header to the last child that turns in at the end, a
faint tint that stops where the children stop, and a heavier rule
starting the next top-level row."

Section 9.6 gains a second table under the first, with its own sentence:
"Every kind name in the application carries this text as its tooltip, and
the legend popover shows them under a Kinds heading with the chains they
form."

| Word | Tooltip |
|---|---|
| Pod | One running copy of a program. The only thing that actually runs, and the thing that gets replaced. |
| Container | One process inside a pod. |
| Workload | What owns pods and decides how many run: a Deployment, a CronJob, a Job, or a bare pod that nothing owns. |
| Deployment | Keeps N copies running and replaces one that dies. |
| Job | Runs a task until it succeeds, retrying up to the backoff limit, each retry a new pod. Failed when the retries run out. |
| CronJob | Creates a new Job on every tick of its schedule. |
| Run | One Job created by a CronJob. |
| Incident | One failure idios recorded: one per container failure, and one more on the Job when it gives up. |

Then the three chains, as a fixed sentence under the table: "CronJob ->
Job (a run) -> Pod (an attempt) -> Container; Deployment -> ReplicaSet ->
Pod -> Container; bare Pod -> Container."

No code changes in this task, so no test. The check is the read: the doc
describes a page nothing has built yet, which is what step A did for the
tooltips and what makes the next tasks answerable.

Check: `make ascii` over the file, and every sentence added above is read
against 9.5 (no colour is claimed for a kind) and 7.6 (the `NOT
RECORDED` tag says what idios does not know rather than hiding it).

Checkpoint: `go build ./... && go test ./... && make ascii &&
make app-test && make app`.
Commit: `docs: spec the run page and the kind vocabulary`.

Consumes: 9.3, 9.6.
Produces: the Run page row, the kinds table and the chains every later
task quotes.

## Task 3 - the job list answers by uid

`api/proto/idios/v1/jobs.proto`, `ListJobsRequest` gains one field after
`failed`:

    // job_uid names one run exactly; a run page is keyed by it and no
    // other filter can reach a Job whose incidents were all swept.
    string job_uid = 8 [(sebuf.http.query) = {}];

`internal/query/workloads.go`. `JobFilter` gains `UID string` and its doc
comment gains the clause "UID names one run and is the only filter that
does". `jobScope`'s equality loop gains `{"uid", f.UID}` as its first
row, before `namespace`: the primary key is the narrowest predicate there
is. `JobCounts` is unchanged, because the counts break a scope down and
a scope of one run has nothing to break down; the filter still applies to
them, so `total` reads 1 for a uid that exists and 0 for one that does
not, which is what says the run is gone.

`internal/api/workloads.go`, `ListJobs`: `UID: req.GetJobUid()` joins the
`query.JobFilter` literal, first field.

`make generate` regenerates `internal/apigen` and `api/openapi`; neither
is edited. The mock is unchanged: `internal/api/mock` answers `ListJobs`
from the `jobs` fixture whatever the query says, and a filter it ignores
needs no fixture.

Tests, Go, table-driven, whole rows compared.

- `TestListJobsByUIDAnswersOneRun` in
  `internal/query/workloads_test.go` (traces to 9.3's new Run page row,
  "one screen for one Job, keyed by the Job's uid", which is the only
  reason a uid predicate exists): three job rows seeded under one
  cronjob, one of them deleted; cases for a uid that exists (the one row,
  compared whole against the expected `JobRow`), a uid that does not
  (no rows, no error), a uid combined with a namespace that does not hold
  it (no rows, so the predicates and), and an empty uid (every row, so
  the filter stays unapplied). The counts of each case are asserted with
  the rows, because `CountJobs` shares `jobScope` and a predicate added
  to one and not the other is exactly the bug this catches.
- `TestListJobsPassesTheJobUIDToTheQuery` is a case added to the existing
  `TestListJobsReturnsMappedRows` table in
  `internal/api/workloads_test.go` rather than a function of its own
  (rule 2): a request carrying `JobUid` answers only that run's row. No
  new function.

Check: `go test ./internal/query/... ./internal/api/...` green;
`make generate-check` clean after the commit; then, against a rebuilt
daemon on the smoke store,
`curl -s '127.0.0.1:7771/v1/jobs?job_uid=<uid>'` answers one job and
`total` 1, and the same call with a uid that does not exist answers no
jobs and `total` 0. The uid comes from
`curl -s '127.0.0.1:7771/v1/jobs?cronjob_name=smoke-cron-fail'` rather
than being guessed.

Checkpoint: `go build ./... && go test ./... && make ascii &&
make app-test && make app`, then `make generate-check` after the commit,
which only passes on a committed tree. `rm -rf macos/.build` before the
next task's `make app-test`.
Commit: `api: filter the job list by job uid`.

Consumes: `ListJobsRequest`, `query.JobFilter`, `jobScope`, `CountJobs`.
Produces: `GET /jobs?job_uid=`, `JobFilter.UID`, and the generated
`Operations.ListJobs` query parameter the store calls next.

## Task 4 - the run route and what the page decides

`macos/Sources/IdiosModel/Route.swift`. `Route` gains one case, after
`pod`:

    /// run is one Job as a page of its own, keyed by the Job's uid; a
    /// scheduled Job is a run and a standalone one is a Job, and both are
    /// this route.
    case run(String)

`Route(path:)` gains `case "run" where parts.count == 2: self =
.run(parts[1])`.

    /// incidentRoute is the page an incident opens: the Job's run page
    /// when the incident's subject is the Job, the pod page otherwise. A
    /// job-subject row with no job uid has no run to open and falls back
    /// to its own incident route, which resolves the pod the daemon
    /// borrowed for it.
    public func incidentRoute(_ incident: Incident) -> Route

Rules. `.run(uid)` when `incident.subjectKind == .job` and `jobUID` is a
non-empty string; `.incident(incident.id)` otherwise. Nothing here reads
`podUID`: an incident route already knows how to find a pod, and a second
rule for the same decision is a second place to be wrong.

`macos/Sources/IdiosModel/RunPage.swift`:

    /// runTitle is what the page is called: a Job a CronJob created is a
    /// run of that CronJob and is named by the segment after the CronJob's
    /// name, and a Job nothing created is a Job.
    public func runTitle(jobName: String?, cronjobName: String?, jobUID: String) -> String

    /// runStateTag is the Job's condition as the header's tag; the run's
    /// outcome is the condition and never the counters.
    public func runStateTag(_ job: Job?) -> String

    /// runCountsLine is the line beside the tag: how many attempts the run
    /// made, how many incidents they opened and how many are still open,
    /// and how long the run ran.
    public func runCountsLine(job: Job?, attempts: [RunAttempt], rows: [Incident], now: Date)
        -> String

    /// RunAttempt is one line of the Attempts card: one pod of the run, or
    /// the Job's own row that closes the card when the Job gave up.
    public struct RunAttempt: Identifiable, Hashable, Sendable {
        public let id: String
        /// number is the attempt's place in the run, oldest first; zero for
        /// the Job's own row, which is not an attempt.
        public let number: Int
        public let podUID: String?
        /// podSuffix is the pod's name after the Job's, which is all that
        /// tells one attempt of a run from the next.
        public let podSuffix: String
        public let category: Category
        public let reason: String
        public let exitCode: Int32?
        public let openedAt: Timestamp
        public let incidentID: String
        public let state: IncidentState
    }

    /// runAttempts reads the run's incidents into one line per attempt,
    /// oldest first, numbering the pods and closing with the Job's own row
    /// when one is there.
    public func runAttempts(rows: [Incident], jobName: String?) -> [RunAttempt]

    /// RunCapture is what the page could read of the attempts' captured
    /// logs, which is bounded by how many attempt details it fetched.
    public struct RunCapture: Hashable, Sendable {
        public let attemptsRead: Int
        public let attemptsWithLog: Int
        public let lastLine: String?

        public init(attemptsRead: Int, attemptsWithLog: Int, lastLine: String?)
    }

    /// runVerdictSentences is what the Run page leads with: how the run
    /// ended, what it was allowed, how its attempts exited, what they
    /// captured, what image ran and whether this has happened before, each
    /// sentence left out when its fields are not there.
    public func runVerdictSentences(
        job: Job?, attempts: [RunAttempt], capture: RunCapture, imageTag: String?,
        sameReasonRuns: Int?, now: Date
    ) -> [String]

Rules.

`runTitle`. With a non-empty `cronjobName` and a `jobName` carrying
`"<cronjobName>-"`, `"Run <suffix> of <cronjobName>"`; with a non-empty
`cronjobName` and a name that does not carry the prefix, `"Run <jobName>
of <cronjobName>"`, because a Job a CronJob owns is a run whatever it was
named; with no `cronjobName` and a name, `"Job <jobName>"`; with neither,
`"Job <middleElided(jobUID, keeping: 12)>"`, because a page opened on a
run whose jobs row was swept still has to be called something.

`runStateTag`. `"NOT RECORDED"` for a nil job, which is a run known only
from an incident's job uid. `"DELETED"` when `deletedAt` is set, as the
pod page's tag leads with deletion. Otherwise the condition: no
`conditionType` is `"RUNNING"`; a type with no reason is the type
uppercased (`"COMPLETE"`); a type with a reason is `"<TYPE>, <REASON>"`
with the reason split on its capitals and uppercased, so
`BackoffLimitExceeded` reads `"FAILED, BACKOFF LIMIT EXCEEDED"` and
`DeadlineExceeded` reads `"FAILED, DEADLINE EXCEEDED"`.

`runCountsLine`. `plural(attempts.count, "attempt")`, then
`"\(rows.count) incidents, \(open) open"` through the same agreement the
pod page's counts line uses, then the run's duration: `"ran
<durationText(startedAt, finishedAt)>"` when both stamps are there,
`"running for <durationText(startedAt, now)>"` when it started and has
not finished, and the part dropped when the Job is nil or never started.
Joined with `" - "`. Attempts and incidents are separate words because
they are separate counts: a run whose two pods each opened one incident
and whose Job then failed is 2 attempts and 3 incidents.

`runAttempts`. The pod rows, ordered by `openedAt.raw` ascending and by
`id` for a tie, numbered from 1; `podSuffix` is `podNameSuffix(name:
podName, workloadName: jobName ?? "")`, falling back to the whole pod
name when the prefix is not there and to `"pod unknown"` when the row
carries no pod name. Then the job-subject rows, in served order, as
closing lines with `number` zero and an empty `podSuffix`. A row with no
pod uid that is not job-subject cannot exist in this set and is dropped
rather than numbered, because an attempt is a pod.

`runVerdictSentences`, in order, each dropped when its fields are absent:

- The outcome. `"Failed after \(plural(attempts, "attempt")) in
  <duration>."` for a Failed condition, `"Complete after
  \(plural(attempts, "attempt")) in <duration>."` for Complete,
  `"Still going, \(plural(attempts, "attempt")) since <clockTime
  startedAt>."` with no condition, and `"The run is not recorded; what is
  left is \(plural(attempts, "attempt"))."` for a nil job. The duration
  part is dropped when the stamps do not parse.
- The allowance. `"The backoff limit is \(backoffLimit); the Job counted
  \(failed) failed pods."` when a job is there. The counters are context
  and never the outcome, which is why this sentence names the limit and
  the counter and says nothing about success.
- The exits. `"Every attempt exits \(code)."` when the attempts' distinct
  exit codes are one, `"Attempts exit \(codes joined by ", ")."` when
  they are more, dropped when none carries one.
- The capture. `"Nothing was captured."` when `attemptsWithLog` is zero
  and `attemptsRead` is not; `"Every attempt captured a log."` when they
  are equal; `"\(attemptsWithLog) of \(attemptsRead) attempts captured a
  log."` otherwise; and `" The last line is \"<lastLine>\"."` appended
  when `lastLine` is there. Dropped whole when `attemptsRead` is zero,
  because a page that has read nothing must not say nothing was
  captured.
- The image. `"Image tag \(tag)."`
- The history. `"\(plural(sameReasonRuns, "earlier run")) of this CronJob
  failed the same way."` when the count is above zero.

Tests, Swift Testing, table-driven, whole-value, edge cases first.

- `aJobSubjectIncidentOpensItsRunAndEveryOtherOpensItsPod` in
  `RouteTests.swift` (traces to 9.3's Run page row, "reached from ...
  every job-subject incident"): rows for a job-subject incident with a
  job uid (`.run(uid)`), a job-subject incident with a nil job uid
  (`.incident(id)`), a job-subject incident with an empty job uid
  (`.incident(id)`), a pod incident that carries a job uid because its
  pod belongs to a Job (`.incident(id)`, since only the subject decides),
  and a plain pod incident (`.incident(id)`). Whole `Route` values.
- `theRunRouteParsesAJobUID` is a case added to the existing route table
  in `RouteTests.swift` rather than a function of its own: `"run/abc"`
  parses to `.run("abc")`, `"run"` and `"run/a/b"` to nil.
- `theRunTitleNamesTheCronJobOrTheJob` in `RunPageTests.swift` (traces to
  9.3's "The title is `Run <suffix> of <CronJob name>` when a CronJob
  created the Job and `Job <name>` when nothing did"): rows for a nil
  name and nil cronjob (the elided uid), a name with no cronjob (`"Job
  nightly"`), a name carrying its cronjob's prefix (`"Run 28812345 of
  report"`), a name that does not carry the prefix under a cronjob
  (`"Run oddly-named of report"`), and an empty cronjob name treated as
  none.
- `theRunTagIsTheConditionAndNeverTheCounters` in `RunPageTests.swift`
  (traces to 4.5's "`succeeded` is read from `condition_type`, never from
  the counters"): rows for a nil job (`"NOT RECORDED"`), a deleted job
  whose condition is Complete (`"DELETED"`, because deletion leads), a
  job with no condition (`"RUNNING"`), Complete with no reason
  (`"COMPLETE"`), Failed with `BackoffLimitExceeded` (`"FAILED, BACKOFF
  LIMIT EXCEEDED"`), Failed with `DeadlineExceeded`, and a job whose
  `succeeded` is 1 while its condition is Failed (still `"FAILED, ..."`,
  which is the sentence this test exists for).
- `theCountsLineSeparatesAttemptsFromIncidents` in `RunPageTests.swift`
  (traces to 9.3's "a counts line of attempts, incidents and how many
  open, and how long the run ran", and to the milestone's rule that every
  count is a count of incidents): rows for a run with no attempts and no
  job, one attempt and one incident (both singular), two attempts and
  three incidents with one open, a run still going (the "running for"
  half), and a run whose job never started (the duration dropped).
- `attemptsAreNumberedOldestFirstAndTheJobRowCloses` in
  `RunPageTests.swift` (traces to 9.3's "one line per attempt, oldest
  first ... the Job's own row closes the card when the Job itself
  failed"): rows for an empty list, one job-subject row alone (one line,
  number zero), two pod rows served newest first (renumbered 1 and 2 by
  `openedAt`), two pod rows and a job row (the job row last), a pod row
  whose pod name does not carry the Job's prefix (the whole name as the
  suffix), and a pod row with no pod name (`"pod unknown"`). Whole
  `[RunAttempt]` compared.
- `theRunVerdictSaysOnlyWhatThePageRead` in `RunPageTests.swift` (traces
  to 7.6's "what idios does not know is said, not hidden", and to 9.3's
  list of the sentences): rows for a nil job with no attempts read (the
  not-recorded sentence alone, no capture sentence), a Failed run of two
  attempts with a backoff limit, one exit code, both attempts captured
  and a last line, a Complete run with no exit code (the exits sentence
  dropped), a run where one of two read attempts captured nothing, a run
  with `attemptsRead` zero (no capture sentence at all), and a Failed run
  with `sameReasonRuns` 18 and an image tag (every sentence). Whole
  `[String]` compared, so the order is asserted with the text.

Check: `make app-test` green. No view reads these yet, so `make app`
proves only that the package still builds into the application.

Checkpoint as task 1, with `rm -rf macos/.build` first, because task 3
regenerated `Types.swift`.
Commit: `model: decide the run page's title, tag and attempts`.

Consumes: `Incident`, `Job`, `Timestamp`, `Category`, `IncidentState`,
`plural`, `clockTime`, `durationText`, `podNameSuffix`, `middleElided`,
`distinct`.
Produces: `Route.run`, `incidentRoute`, `runTitle`, `runStateTag`,
`runCountsLine`, `RunAttempt`, `runAttempts`, `RunCapture`,
`runVerdictSentences`.

## Task 5 - the Run page

`macos/idios/Store/RunPageStore.swift`:

    /// RunPageStore holds one Run page: the Job as the job list answers it
    /// by uid, the run's incidents, the detail of each attempt, the other
    /// runs of the same CronJob, the lead incident's timeline and the bytes
    /// of the files a person opened.
    @Observable @MainActor
    final class RunPageStore {
        private(set) var job: Job?
        /// jobMissing is the jobs row having been swept while an incident
        /// that names it outlived it: the page still has the attempts.
        private(set) var jobMissing = false
        private(set) var rows: [Incident] = []
        /// details is the GetIncident of each attempt, by incident id: what
        /// the Logs tab, the Events tab and the verdict's capture sentence
        /// read.
        private(set) var details: [String: IncidentDetail] = [:]
        private(set) var siblingRuns: [Job] = []
        private(set) var timeline: [TimelineEntry] = []
        private(set) var timelineTruncated = false
        private(set) var timelineLoading = false
        private(set) var timelineError: APIError?
        private(set) var isLoading = false
        private(set) var error: APIError?

        let writes = IncidentWriteStore()

        /// attemptLimit bounds the per-attempt fetches: a backoff limit is
        /// single digits, and a run that somehow made hundreds of attempts
        /// must not make hundreds of calls.
        static let attemptLimit = 12

        /// watch loads the run and reloads it on every stream row that
        /// names one of its incidents, until cancelled.
        func watch(jobUID: String, connection: DaemonConnection) async

        /// reload fetches the Job, the run's incidents, each attempt's
        /// detail and the CronJob's other runs again.
        func reload(jobUID: String, connection: DaemonConnection) async

        /// loadTimeline fetches the lead incident's timeline.
        func loadTimeline(incidentID: String, connection: DaemonConnection) async

        /// acknowledgeRun marks every open, unacknowledged incident of the
        /// run seen and answers how many it changed.
        func acknowledgeRun(connection: DaemonConnection) async -> Int

        /// content fetches a captured file's bytes once.
        func content(of artifact: Artifact, connection: DaemonConnection) async
    }

Rules. `reload` calls, in order: `ListJobs(query: .init(job_uid:
jobUID, limit: 1))` and keeps `rows.first { $0.uid == jobUID }` - the
match is by uid inside the answer, because a daemon that ignores the
filter must show no Job rather than another run's; `ListIncidents(query:
.init(job_uid: jobUID))` for `rows`; `GetIncident` for the newest
`attemptLimit` attempt rows and for the job-subject row, filling
`details`; and, when the Job names a `cronjobName`, `ListJobs(query:
.init(cluster_ids:, namespace:, cronjob_name:, limit:))` for
`siblingRuns`. A 404 or an empty answer for the Job sets `jobMissing` and
never `error`: the sweeper removing a jobs row is retention working, as
the pod page already treats a swept pod. `.cancelled` is ignored
everywhere and every other error goes through one `report(_:connection:)`
as `PodPageStore` does. `watch` mirrors `PodPageStore.watch`: reload,
subscribe to `StreamIncidents`, reload when `event.data?.jobUid ==
jobUID`, reconnect after two seconds, reload before every resubscribe
because the stream has no replay.

    /// lead is the incident the header's actions, the timeline and Ask AI
    /// act on: the Job's own row when it has one, because that is the
    /// incident about the run, and the newest attempt's otherwise.
    var lead: Incident?

    /// capture is what the fetched attempt details say about the run's
    /// logs, for the verdict.
    var capture: RunCapture

    /// logArtifacts is every attempt's captured file, oldest attempt
    /// first, for the Logs tab's one scrubber.
    var logArtifacts: [Artifact]

    /// runEvents is the Job's events and its attempts', newest stamp
    /// first, for the Events tab.
    var events: [Event]

`capture` counts `details` entries that hold at least one log artifact
with a `filePath`, over the count of details for attempt rows;
`lastLine` is the last non-empty line of the newest attempt's newest
captured file once `content` has answered for it, and nil until then.

`macos/idios/Views/RunPage/RunPageScreen.swift`:

    /// RunPane is the tab a Run page shows.
    enum RunPane: Hashable { case overview, timeline, logs, events }

    /// RunPageScreen is one Job: what it did, one line per attempt, and
    /// the actions that reach every incident it opened.
    struct RunPageScreen: View {
        let jobUID: String
        let clusters: [Cluster]
        let openPod: (String, PodTab) -> Void
        let openWorkload: (Route, WorkloadTab) -> Void
        let revealNamespace: (String, String) -> Void
        let deleted: (String) -> Void
    }

Rules. The shape is `PodPageScreen`'s: a header, a divider, then an
`HStack` of the middle `ScrollView` and a 310-point rail with
`.background(.quaternary.opacity(0.25))`; there is no left column,
because the attempts are a card and not a source list. `.focusable()`
with `.onKeyPress(keys: ["a", "d", "r"])` acting on `store.lead`, except
that `a` calls `store.acknowledgeRun` rather than one write, because
9.3's Run page row says Acknowledge acts on every incident of the run.
The toolbar is the pod page's: Back and Forward through `Navigator`, and
`Copy uid` copying the Job's uid. `.navigationTitle(runTitle(...))`. The
tasks mirror the pod page's: one keyed on
`(connection.generation, jobUID)` for `watch`, one on
`(generation, leadID, showingTimeline)` for the timeline, one on the
selected artifact's id for its content, and `.onChange(of: activeState)`
reloading when the window becomes key. `.sheet` for the note, `.alert`
for the delete, `.sheet(item:)` for Ask AI, all on `store.lead`, as
`PodPageScreen` has them.

The middle column: `PaneActionsRow` for the lead incident with the
Acknowledge button relabelled by the count it acts on when the run has
more than one open incident ("Acknowledge 3"), the verdict block, the
lead incident's `IncidentHeader`, the note, the action error, the tab
strip and the tab content, at the pod page's 16-point padding and
4-point verdict inset.

    private var verdict: some View

is a plain `VStack` over `runVerdictSentences(...)` drawn the way
`VerdictBlock` draws its own, so the two pages read the same; the
existing `VerdictBlock` takes an `Incident` and a `Container` and is not
what a run has, so the Run page's block is its own small view in this
file rather than a parameter added to a type that has one shape.

Tabs. Overview draws `AttemptsCard`, then `JobCard(job: store.job,
jobUID: jobUID, lastPodName: nil)`, then, when the Job names a CronJob,
`RunStripView` over `runCells(jobs: store.siblingRuns, incidents:
store.rows, cronjobName: name)` with the run marked. Timeline is
`TimelineView` over the store's entries. Logs is `CapturedLogsCard` over
`store.logArtifacts` with the pod page's `wrap` binding and selection.
Events is one `EventsCard(events: store.events, title: "Events of this
run", meta: "k8s_events of the Job and of its attempts' pods", emptyText:
"idios kept no event for this run.", showsContainer: true)` with no
`incidentID`, so nothing dims: on this page every event belongs to the
run.

`macos/idios/Views/Workloads/RunStripView.swift` gains one parameter:

    /// marked is the run this strip is drawn beside, outlined so a person
    /// can see where they are in the line; nil on the Workloads tab, where
    /// the newest run is the only one worth pointing at.
    var marked: String? = nil

and `cellView` draws the accent outline it already draws for a drag when
`cell.jobUID == marked`, in place of the newest-run outline for that
cell. The Workloads caller passes nothing and is unchanged.

`macos/idios/Views/RunPage/RunPageHeader.swift`:

    /// RunPageHeader is the run's own header: the Job's condition, what it
    /// cost, and the chain that owns it.
    struct RunPageHeader: View {
        let job: Job?
        let jobUID: String
        let attempts: [RunAttempt]
        let rows: [Incident]
        let clusterName: String
        let namespace: String
        let cronjobName: String?
        let openWorkload: (Route, WorkloadTab) -> Void
        let revealNamespace: (String, String) -> Void
    }

Rules. `Badge(text: runStateTag(job), style: tagStyle)` then the counts
line in tertiary 11-point monospaced, then the title as
`PodPageHeader`'s treatment (a 20-point semibold `Text` with the name in
`mono`), then the breadcrumb. `tagStyle` is the pod page's rule read for
a Job: `IncidentState.podDeleted.badge` for a deleted Job,
`BadgeStyle.green` for Complete, `BadgeStyle.red` for Failed, `.neutral`
otherwise - green is a run that completed, which 9.5 allows as live and
healthy, and grey is what a swept record gets. The breadcrumb is
`PodPageHeader.identityLine`'s shape: cluster and namespace segments
calling `revealNamespace`, then a CronJob segment calling
`openWorkload(.workload(...), .runs)` when the Job names one. The Job
itself is not a segment: it is the page.

`macos/idios/Views/RunPage/AttemptsCard.swift`:

    /// AttemptsCard is the run attempt by attempt: one line per pod the
    /// Job started, in order, with the incident it opened and the way into
    /// that pod's logs.
    struct AttemptsCard: View {
        let attempts: [RunAttempt]
        let job: Job?
        let openPod: (String, PodTab) -> Void
        let openIncident: (String) -> Void
    }

Rules. A `DetailCard(title: "Attempts", meta: "incidents WHERE job_uid =
<uid>, one line per pod")` holding a heading row and one row per attempt,
built as custom rows with a fixed column budget, because 9.4 says a
`Table` on macOS 15 clips instead of shrinking. The columns: `#` 26,
`POD` 150, `CATEGORY` 96, `EXIT` 44, `REASON` flexible with a 140 floor,
`OPENED` 52, `INCIDENT` 96. The number cell reads the attempt number, or
`"job"` for the closing row, which is 9.6's word for a subject that is
the Job. The pod cell is a link opening `openPod(uid, .logs)`, elided in
the middle with the whole name on hover; the closing row's pod cell reads
`"no pod"` in tertiary. The category cell is `CategoryBadge`, the exit
cell the code or `"-"`, the reason cell `ExpandableText`, the opened cell
`clockTime`, and the incident cell a `"#<id>"` link beside a
`StateBadge`. Under the rows, when `job` is there and its `failed`
counter exceeds the attempts drawn, one tertiary line: `"The Job counted
\(failed) failed pods; idios kept \(plural(drawn, "attempt")). The rest
were pruned before it saw them."` - 7.6, said rather than hidden.

`macos/idios/Views/RunPage/RunPageRail.swift`:

    /// RunPageRail is the run's identity: what owns it, where and how it
    /// was told to run, its times, and the other runs of the same CronJob.
    struct RunPageRail: View {
        let job: Job?
        let jobUID: String
        let attempts: [RunAttempt]
        let rows: [Incident]
        let siblingRuns: [Job]
        let lead: Incident?
        let now: Date
        let openPod: (String, PodTab) -> Void
        let openRun: (String) -> Void
        let openWorkload: (Route, WorkloadTab) -> Void
    }

Rules. Four groups separated by dividers, using the rail furniture the
pod page already has. OWNER CHAIN through `chainLink`: a CRONJOB link
when the Job names one, the JOB link carrying the uid and the name, then
one POD link per attempt, each opening that pod's page, the last of them
`last: true`; a run with no attempt kept draws one grey `POD` link
reading `"none kept"` with the note "every pod of this run was pruned or
swept", which is the sentence the page it replaces carried. CONTEXT:
Node (the distinct node names among the run's rows, or `"not
scheduled"`), Schedule (the CronJob's, when a sibling run carries it -
otherwise the row is dropped rather than invented), Image (the distinct
image tags among the rows), Backoff limit (`job.backoffLimit`, with
`.help("jobs.backoff_limit")`). TIMES run: scheduled
(`job.createdAt`), first attempt (the oldest attempt's `openedAt`),
finished (`job.finishedAt`), and open for, read from the lead incident
the way `IncidentTimesRail` reads it; when a lead incident is there the
group is `IncidentTimesRail` under a TIMES run group rather than instead
of it, because the run's times and the incident's are different
questions. OTHER RUNS: the five newest siblings other than this one, each
a link calling `openRun(uid)` with its suffix, a state pill taking
`runCellStyle(cell.outcome)` from the `RunCell` the strip already builds,
and a "+ N more of this CronJob" line opening Workloads on the CronJob's
Runs.

`CLAUDE.md`'s screenshot route list gains `run/<job uid>` beside
`pod/<uid>[/<tab>]`, and the sentence about a Job's incident with no pod
opening the pod-less job page becomes "a job-subject incident opens the
run page".

No test in this task: every rule with a decision in it was decided and
tested in task 4, and `.ai/tests.md` keeps a test off a view.

Check against the smoke store on 7771, the application started with
`-daemon 127.0.0.1:7771`. Open a run of `smoke-cron-fail` from the
Workloads Runs table: the title reads "Run <suffix> of smoke-cron-fail",
the tag reads "FAILED, BACKOFF LIMIT EXCEEDED", the counts line reads "2
attempts - 3 incidents, 3 open - ran 12s", the verdict says it failed
after 2 attempts, names the backoff limit 1 against the failed counter,
says every attempt exits 1, says both captured a log and quotes "the work
failed", and names the earlier runs that failed the same way. The
Attempts card lists both pods in order with their incident ids, and a
click on a pod lands on that pod's page with the Logs tab showing.
Overview's run strip marks this run in the line. The rail's chain runs
CronJob, Job, two pods, and OTHER RUNS opens a neighbour. Timeline,
Logs and Events all fill. Press `a`: every open incident of the run turns
amber and the Acknowledge button greys. Then open `smoke-cronjob-retry`:
the title reads "Job smoke-cronjob-retry", the tag reads "COMPLETE", the
Attempts card has one line (the failed first attempt) and the card's
footer says the Job counted one failed pod. Screenshots
`run/<uid>` for both through the scratch script for the diff review.

Checkpoint as task 1.
Commit: `app: add the run page`.

Consumes: `runTitle`, `runStateTag`, `runCountsLine`, `runAttempts`,
`runVerdictSentences`, `RunCapture`, `runCells`, `runCellStyle`,
`IncidentWriteStore`, `PaneActionsRow`, `DetailCard`, `FactRow`,
`EventsCard`, `JobCard`, `CapturedLogsCard`, `TimelineView`,
`IncidentTimesRail`, `chainLink`, `railLink`, `RailHeading`, `RailRow`,
`Navigator`, `ArtifactContentStore`.
Produces: `RunPageStore`, `RunPageScreen`, `RunPane`, `RunPageHeader`,
`AttemptsCard`, `RunPageRail`, `RunStripView.marked`.

## Task 6 - every run opens the run page

`macos/idios/Views/Incidents/IncidentsScreen.swift`. `destination(_:)`
gains

    case .run(let uid):
        RunPageScreen(
            jobUID: uid, clusters: clusters.clusters,
            openPod: { push(.pod($0, $1)) }, openWorkload: openWorkload,
            revealNamespace: revealNamespace, deleted: deleteIncident)

and `openIncident(_:)` becomes the one place the choice is made:

    // A job-subject incident is about the run, not about the pod the
    // daemon borrowed to describe it.
    private func openIncident(_ id: String) {
        let route = rows.first { $0.id == id }.map(incidentRoute) ?? .incident(id)
        visited(route, title: incidentTitle(id))
        guard path.last != route else { return }
        push(route)
    }

where `rows` is the `incidents.rows + search.lookupIncidents` the title
already reads. `recentID(_:)` gains `case .run(let uid): "run/\(uid)"`,
so a run joins the palette's recent pages. `perform(_:)`'s `.open` switch
gains `.run` beside `.pod` and `.timeline`, which push.

`macos/Sources/IdiosModel/Search.swift`. `SearchHit.run`'s `action`
becomes `.open(.run(fold.jobUID))`: the Runs section of the palette is
named for runs and now has a run page to open. `copyText` and
`workloadAction` are unchanged. `newestPodRow` keeps its one remaining
caller in `copyText`.

`macos/idios/Store/PodPageStore.swift`:

    /// ResolvedIncident is what an incident route resolved to: the pod and
    /// container to select, or the run the route belongs on instead.
    enum ResolvedIncident: Hashable {
        case pod(uid: String, container: String?)
        case run(jobUID: String)
    }

    /// resolve answers where an incident route belongs and seeds lit with
    /// the detail it fetched; nil when the incident is unknown (its error
    /// is in litError) or names neither a pod nor a run.
    func resolve(incidentID: String, connection: DaemonConnection) async -> ResolvedIncident?

Rules. A job-subject incident with a job uid answers `.run(uid)` before
any pod is considered, whether or not the daemon borrowed a pod for it;
`podless` and the `container(of:)` fallback that picked a Job's failing
container both go, because nothing reaches the pod page as a Job any
more. A job-subject incident with no job uid keeps today's behaviour and
answers the borrowed pod, with no container selected. A pod incident
whose row names no pod stays the daemon contradicting itself and sets
`litError`.

`macos/idios/Views/PodPage/PodPageScreen.swift`. The screen gains
`let openRun: (String) -> Void`, its `.task(id: opening)` sends a
`.run(uid)` answer straight to it, and every arm that existed for the
pod-less page goes: the `JobOnlyPage` branch of `content`, the `jobPane`
state, `showingTimeline`'s `podless` arm, `subjectUID`'s job half and
`title`'s. `containerIncidents(_:)` drops the append of the lit
job-subject incident and `failingContainer(_:)` goes with it, so a
container's segmented control is that container's own incidents and the
header's counts line counts the pod's. `macos/idios/Views/PodPage/
JobOnlyPage.swift` is deleted whole, with `JobPane`.

`macos/idios/Views/PodPage/PodPageHeader.swift`. The breadcrumb's
controller segment opens the run when the controller is a Job: the
`segment("\(kind) \(name)")` for `controllerKind == "Job"` calls
`openRun(uid)` with `pod.controllerUID` rather than
`openWorkload(.workload(...), .pods)`, because a Job in Workloads is a
page about one run and the run page is that page. The header takes
`let openRun: (String) -> Void` for it. Every other segment is
unchanged.

`macos/idios/Views/PodPage/PodPageRail.swift`. The owner chain's
controller link does the same: for `controllerKind == "Job"` the name
becomes a link calling `openRun(uid)`; the rail takes the closure for it.

`macos/idios/Views/Workloads/WorkloadDetailView.swift`. `RunsTab`,
`JobsCard` and the strip take `let openRun: (String) -> Void` and use it
where they used `openPod`: `JobsCard.runRow` wraps its cells in a button
calling `openRun(run.jobUID)` for every run, kept or pruned, and its POD
cell keeps its own click into the pod; `RunStripView.open(_:)` becomes
`openRun(cell.jobUID)`, and `RunMatrixView` the same. The Runs table's
`#<id>` link keeps opening the incident, which now resolves through
`incidentRoute`. `WorkloadsScreen` threads `openRun` from
`IncidentsScreen` as it threads `openPod`.

`macos/idios/Views/Incidents/IncidentsList.swift`. `openOrToggle(_:)`'s
incident branch already calls `open`, which is `openIncident`, so a job
row inside a run reaches the run page without a change here. The run
row itself is a fold and not a route: `openOrToggle` keeps toggling it,
and the run row gains a link into its page instead - the run's suffix in
`RunRowView.subject` becomes a `.link` button calling a new
`openRun: (String) -> Void` the list takes and `IncidentsScreen` wires to
`push(.run(uid))`. A double click on the row still expands it, which is
what a chevron row does everywhere else in this list.

Tests. The routing decision is `incidentRoute`, tested in task 4; the
rest of this task is wiring, and `.ai/tests.md` rule 6 deletes a test
that asserts a closure was passed.

Check against the smoke store on 7771. Every one of these lands on the
same page, and Back returns to where it came from: a job row inside a
`smoke-cron-fail` run in the list; the run row's suffix link; the run
strip on the CronJob's Workloads page; a row of the Runs table; the pod
page breadcrumb's `Job` segment and the rail's Job dot from a
`smoke-cron-fail` pod; the palette's Runs section on a query of the run's
suffix; and `-route incident/<the job_failed incident's id>` on the
command line. A pod row inside a run still opens the pod page. On that
pod's page, the app container's segmented control no longer holds the
Job's incident, the header's counts line counts one fewer incident, and
the Related incidents tab still lists the Job's rows. Screenshots
`incidents`, `workload/<cluster>/idios-smoke/CronJob/smoke-cron-fail` and
`pod/<uid>` through the scratch script for the diff review.

Checkpoint as task 1.
Commit: `app: open the run page from every run`.

Consumes: `incidentRoute`, `Route.run`, `RunPageScreen`, `RunFold`,
`RunCell`, `Navigator`.
Produces: the `.run` destination, `ResolvedIncident`, the breadcrumb and
rail links into a run, the Runs table and strip opening a run, and a pod
page with no job-subject segments.

## Task 7 - the list draws children as children

`macos/idios/Views/Incidents/ListColumns.swift`:

    /// childIndent is how far a row under a group header steps in, so the
    /// eye reads one problem and its parts rather than a run of lines.
    static let childIndent: CGFloat = 24

    /// GroupChild is the treatment every row under a header takes: the
    /// indent, the guide rail from the header down to the last child, and
    /// the tint that stops where the children stop.
    struct GroupChild: ViewModifier {
        let isLast: Bool
        func body(content: Content) -> some View
    }

Rules. The modifier pads the content by `childIndent` on the leading
edge and draws, in an overlay aligned to the leading edge, a 1-point
`Rectangle` in `.quaternary` at x = 9 running the row's full height, and
for the last child a rectangle running half the height with an 8-point
horizontal stub at its foot, which is the turn-in. The tint is
`.listRowBackground(Color.clear.overlay(.quaternary.opacity(0.12)))` so
it reaches the row's whole width and stops with the last child; the
selection still paints over it, because a `listRowBackground` sits under
the selection. Every colour here is a system material: `.quaternary` and
`.tertiary` are hierarchical styles and not hues, so 9.5 holds and
`BadgeStyle.swift` gains nothing.

`macos/idios/Views/Incidents/IncidentsList.swift`. `entries(_:)` knows
which entry is the group's last, so it passes `isLast` down; `runRow`,
`rollupRow` and `foldRows` each apply `.modifier(GroupChild(isLast:))` to
every row they draw, and the rows they nest a second level deep (a run's
pod folds, a rollup's folds, a fold's siblings) keep the
`indented: true` spacer they already draw inside the child treatment, so
the second level steps in twice. The `moreRuns` line takes the same
treatment and is always last when it is there. `header(_:)` gains

    // A group header starts a new problem, and the eye needs a stronger
    // line than the one between two children of the same problem.
    .overlay(alignment: .top) { Divider().overlay(.tertiary) }

on every header but the first of its bucket. `hasHeader(_:)` is
unchanged, so a headerless group's one row draws no indent and no guide:
there is no parent to be a child of.

The recorded fallback, if the indent does not read on real data: each
folded problem as a bordered box - a `RoundedRectangle` stroked in
`.quaternary` around the header and its children together, rows at
today's padding and no indent. Not to be tried before this task ships and
is looked at.

No test: this is a layout treatment, and no decision in it belongs in
`IdiosModel`.

Check against the smoke store on 7771, grouped by workload and by
namespace, in both densities and both appearances: a CronJob group reads
as a header with five runs stepping in under it, the guide rail running
from the header to the last run and turning in, the tint stopping there,
and the next workload's header sitting on a heavier line. Expanding a run
steps its pods in again and the guide follows. A headerless group's row
sits at the list's left edge with no rail. Selecting a child still paints
the whole row in the accent. A group with one child draws a rail that is
only the turn-in. Screenshot `incidents` through the scratch script for
the diff review, and read it beside the previous one.

Checkpoint as task 1.
Commit: `app: indent the rows under a group header`.

Consumes: `ListColumns`, `IncidentsList`, `GroupHeaderView`.
Produces: `ListColumns.childIndent`, `GroupChild`, the list's nesting.

## Task 8 - the legend learns the kinds

`macos/Sources/IdiosModel/Vocabulary.swift`:

    /// KindWord is one word of the kind vocabulary: what a person reads and
    /// the sentence every mention of it carries.
    public struct KindWord: Identifiable, Hashable, Sendable {
        public let id: String
        public let word: String
        public let sentence: String
    }

    /// kindWords is the kind vocabulary in the order it teaches: the thing
    /// that runs, then what is inside it, then what owns it.
    public let kindWords: [KindWord]

    /// ownerChains is how the kinds fit together, one chain per way a pod
    /// comes to exist.
    public let ownerChains: [String]

Rules. `kindWords` is Pod, Container, Workload, Deployment, Job, CronJob,
Run, Incident, with the sentences of 9.6's kinds table written out
exactly. `ownerChains` is the three chains of 9.6:
`"CronJob -> Job (a run) -> Pod (an attempt) -> Container"`,
`"Deployment -> ReplicaSet -> Pod -> Container"`,
`"bare Pod -> Container"`.

`macos/idios/Views/Incidents/LegendPopover.swift`. A `Divider` and a
Kinds section under the Attention section, above the Status button:

    private var kinds: some View

is a `Grid` in the shape `stateGrid` already uses - the word in 11.5
semibold in the leading column, the sentence in 11-point secondary in the
trailing one - over `kindWords`, followed by the chains, each on its own
line in 11-point monospaced tertiary. Nothing in it carries a colour:
9.5 says kinds have none. The popover's width goes from 420 to 460 so the
chains do not wrap, and the whole body becomes a `ScrollView` with a
560-point maximum height, because the lifecycle, the seven states, the
attention paragraph and eight kinds no longer fit a popover on a small
display.

Test.

- `theKindVocabularySaysWhatOwnsWhat` in `VocabularyTests.swift`, a new
  file (traces to 9.6's kinds table and its chain sentence): one case
  asserting the whole `[KindWord]` against the eight expected values, so
  a word added to the application without a sentence fails here, and one
  asserting the whole `[String]` of chains. This is the one test in the
  step that asserts constants, and it earns its place because 9.6 fixes
  the text and `.ai/code-is-truth.md` rule 3 says the doc and the code
  must not disagree; nothing else compares them.

Check against the smoke store on 7771: the "?" beside the View heading
opens the popover, the Kinds section reads under Attention, the three
chains fit on one line each in both appearances, the popover scrolls
rather than clipping on a 900-point-tall window, and "Status..." is still
reachable at its foot. Screenshot `incidents` with the popover open
through the scratch script for the diff review.

Checkpoint as task 1.
Commit: `app: add the kinds to the legend popover`.

Consumes: `LegendPopover`, `IncidentState.tooltip`, `Flow`, `Grid`.
Produces: `KindWord`, `kindWords`, `ownerChains`, the legend's Kinds
section.

## Task 9 - verification with the user and the status line

The user runs the application against the smoke store on 7771 and walks
the run: a job row in the list opening the Run page; the title reading
"Run <suffix> of smoke-cron-fail" for a scheduled run and "Job
smoke-cronjob-retry" for the standalone one; the tag, the counts line and
the verdict against what `kubectl -n idios-smoke describe job` says; the
Attempts card in order with its links into each pod's logs; the run strip
marking this run; the rail's chain, other runs and times; Acknowledge
taking every incident of the run; Timeline, Logs and Events; every one of
the eight ways in landing on the same page and Back returning; a pod row
still opening the pod page, with the Job's incident gone from its
container's segments; the list's indent, guide rail, tint and rule on
real data, and the recorded box fallback considered against it; the
legend's Kinds section; and the events table in a narrow pane. Anything
the review turns up is fixed in the task that owns it and committed as a
new commit, never by rewriting one.

Then `docs/plans/m10-reading/roadmap.md`'s step F line becomes "complete
<date>". The milestone's own status line and the `docs/plans/README.md`
row for `m10-reading/` are the user's call, not this task's: closing the
milestone is a separate step with its own report.
Commit: `docs: close m10 step F`. The 7771 daemon is stopped, the Debug
build is quit, and `.storage/smoke` is left as it is for the next run.

## Hands to the next step

F is the last step of m10 as the roadmap stands, and step E's closing
report is what the milestone ends on. This step adds to it:

- The one daemon change of this step, `job_uid` on `GET /jobs`, joins
  step C's exact `pod_name` filter as the second thing a client outside
  this repository would notice in m10.
- The seams this step builds that outlive the milestone: `incidentRoute`
  as the one place an incident's page is chosen, so a later subject kind
  changes one function; `runAttempts` and `RunAttempt` as the run's row
  shape, which a Workloads Runs table could read instead of building
  `RunCell` twice; `RunPageStore.attemptLimit` as the one bound on a
  page that fetches per attempt; `kindWords` and `ownerChains` as the one
  place the kind vocabulary lives, which every kind tooltip in the
  application should eventually read; `GroupChild` as the one nesting
  treatment.
- What the Attempts card cannot say, so a later milestone starts from it
  rather than rediscovering it: an attempt that succeeded is counted and
  never drawn, because the daemon exposes no way to list a Job's pods.
  The join exists in the schema (`pods.controller_uid`, unindexed) and is
  used by `lastPod` and `podSiblings`; a `job_uid` filter on `ListPods`
  with an index behind it is the change that would let the card draw a
  run's whole history.
- The doubts below.

## Self-review

Spec coverage. 9.3's new Run page row: tasks 4, 5 and 6 build every
clause of it, and task 2 writes it. 9.3's Pod page row: task 6 removes
the pod-less-Job page the row described and task 2 removes the sentences
that described it; task 1 answers "the events table never wraps a
timestamp: the time column widens and the message wraps", reading "the
time column widens" as the message column widening while the timestamp
column stays fixed and unwrapped, which is the only reading that fixes
the pane. 9.3's Incidents row: task 7 draws the children as children and
task 2 states it; the run fold and the group header shape are step B's
and are not touched. 9.3's Workloads row: task 5 marks the strip and
task 6 points its cells at the run. 9.6: task 2 fixes the kinds and the
chains, task 8 draws them. 4.5: task 4 (`runStateTag` and the verdict's
allowance sentence read the condition and name the counters as counters).
7.3: task 5's breadcrumb and rail are the identity chain, and the Job is
the page rather than a segment of itself. 7.4: task 1 (the reason
truncates in the middle with the whole value on hover) and task 5 (every
card names its source on its title's hover). 7.6: task 5's Attempts
footer and the `NOT RECORDED` tag, task 4's capture sentence dropped when
nothing was read. 9.4: task 5's Attempts card is custom rows with a fixed
budget. 9.5: task 5's tag styles are the existing vocabulary, task 7's
guide and tint are system materials and task 8's kinds carry no colour;
`BadgeStyle.swift` gains nothing. Decision 11: every clause of the
roadmap paragraph `roadmap-stepF.patch.md` adds is a task here. Decision
10: every name in this plan, its tests and its checks is invented or a
smoke fixture's.

Split. Nine tasks, six of them code. Task 1 is first and alone because it
is a bug in a shipped pane that step E's screenshots need fixed, and it
shares nothing with the run. Task 2 is the spec before the code, as step
A was for the milestone: the Run page row is what tasks 4 to 6 are
answerable against. Task 3 is its own commit because it is the only one
that touches Go, the only one that regenerates, and it can ship and be
curled while the application is still being drawn. Task 4 is one commit
rather than five because its six functions are one decision - what a Run
page says - and every one of them is drawn by task 5. Task 5 and task 6
are apart because the page can be built and read before anything routes
to it, and a single commit would mix a new screen with edits to six
existing files. Task 7 and task 8 are apart from the run entirely and
from each other: the list's nesting and the legend's kinds share nothing
but the milestone.

Doubts. The Attempts card draws only the attempts that opened an
incident, because `ListIncidents?job_uid=` is the only way to reach a
Job's pods and a pod that succeeded has no incident. A run of three
attempts whose third succeeded therefore draws two lines and a Complete
tag, with the header's attempt count also reading 2. The footer says the
Job's own failed counter when it exceeds the lines drawn, which covers
the pruned case but not the succeeded one: nothing in the page says "and
one more attempt succeeded". The honest fix is a `job_uid` filter on
`ListPods`, which this plan does not build because decision 11 asks for
one screen and not two daemon changes; the counts line's "attempts" is
therefore attempts idios recorded a failure for, and that is a weaker
sentence than the word suggests. -- `runCountsLine` says attempts and
incidents; 9.3's pod page counts line says containers and incidents. Two
pages, two nouns, and a person reading quickly could take "2 attempts - 3
incidents" for a contradiction. Spelling it "2 attempts, 3 incidents
between them" was rejected as wordy; the doubt stands. -- Acknowledge on
the Run page acts on every incident of the run without asking, where the
list asks before a bulk dismiss or resolve and not before a bulk
acknowledge. That follows the list's rule (acknowledge is reversible and
acts at once), but the Run page's button carries no count until the run
has more than one open incident, so a person who presses `a` on a run
with six open incidents gets six writes from one keystroke with no
confirmation. The list already does exactly that on a group header, so
this is consistent rather than new. -- The rail's Schedule row reads the
CronJob's schedule off a sibling run, and the `jobs` table does not store
a schedule at all: no sibling means no row. Dropping the row rather than
inventing it is 7.6, but a rail that sometimes has a Schedule and
sometimes does not is a rail whose shape moves. -- `RunPageStore` fetches
one `GetIncident` per attempt, up to twelve, to fill Logs, Events and the
verdict's capture sentence. That is up to fourteen calls for one page
where the pod page makes four. A run's attempts are bounded by the
backoff limit and are single digits in practice, and the alternative -
fetching nothing until a tab is opened - would leave the verdict unable
to say what was captured, which is the sentence the pod page's verdict is
most read for. -- The Related tab of a Job's pod is kept, not replaced.
The breadcrumb's Job segment now opens the Run page, which lists the same
rows better than the tab does, so the tab repeats a link that is two
lines above it; but the tab is the only place a person on a pod page sees
the run's other pods without leaving the page, and removing a tab that
step E just rebuilt to read from `related_incidents` would undo working
code for tidiness. Kept, and recorded here as the decision. -- The list's
child tint at `.quaternary.opacity(0.12)` is close to the group header's
own `.quaternary.opacity(0.35)`, and on a dark appearance the two may
read as one block rather than as a header and its children. The rule
above the next header is what separates them; if that is not enough on
real data, the bordered-box fallback is the recorded answer and not a
tweak to the opacity. -- `Route.run` carries a job uid and nothing else,
so a run page opened from a command line route with a uid the daemon does
not hold shows the not-recorded tag, an empty Attempts card and no
breadcrumb. Carrying the cluster and namespace in the route would fix the
breadcrumb and would make the route four segments where every other
identity route in this application is keyed by a uid alone.

`.ai` rules. ascii-only: no symbol in any code block, string or table
above; the arrows in the chains are `->`, the separators are `-` and `/`,
and `hack/ascii-check docs/plans/m10-reading/run.md
docs/plans/m10-reading/roadmap-stepF.patch.md` is run before these files
are finished. tests: every test above names the sentence it traces to, is
table-driven, compares whole values and leads with the edge cases (the
job-subject row with no job uid, the run with no attempts, the job with
no condition, the deleted job whose condition says Complete, the pod
whose name does not carry the Job's prefix, the page that has read no
attempt, the uid that matches nothing); tasks 1, 5, 6 and 7 write no test
because a layout budget, a view, a wiring and a treatment are none of the
three sources rule 1 allows, and the one test in task 8 that asserts
constants says in this plan why it earns its place. comments: every
comment in a code block says why - the uid predicate being the narrowest
there is, the match by uid inside the answer, the attempt limit, the
heavier rule, the Job that is the page and not a segment.
code-is-truth: no code block, comment or test name mentions a document, a
section or this plan, and task 2 changes the doc in the same step the
code changes rather than leaving the two standing apart. scope: one proto
field and no second one, no `ListPods` filter, no index, no mock fixture,
no colour added to `BadgeStyle`, no new screenshot in the README, no
change to the run fold or the group header of step B, no milestone-
closing edit to `docs/plans/README.md`. commits: nine subjects under 72
characters, one `api:`, one `model:`, four `app:`, three `docs:`; one
`make generate` and one `make generate-check`, in task 3 alone.

Type consistency. `Route.run(String)` (task 4) carries a job uid, the
same string `RunFold.jobUID`, `RunCell.jobUID`, `Incident.jobUID` and
`Job.uid` all carry, so every entry point in task 6 hands the page a
value it already holds. `incidentRoute` (task 4) takes an `Incident`,
which is what the list, the palette, the menu bar and
`IncidentDetail.incident` all carry, so one function serves every caller.
`RunAttempt` (task 4) is built from `[Incident]` and read by
`AttemptsCard`, `RunPageHeader`, `RunPageRail` and
`runVerdictSentences`, so the page's rows and its sentences cannot
disagree about how many attempts there were. `RunCapture` (task 4) is
built by the store from `[Artifact]` and read by the verdict, keeping the
count of what was fetched beside the count of what captured, so the
sentence can be dropped rather than guessed. `runCells` (step D) takes
`[Job]` and `[Incident]`, which is exactly what `RunPageStore` holds for
the strip and the rail's other runs, so task 5 adds no second run shape.
`Job` (existing) is what `ListJobs` answers, what `JobCard` draws and
what `runStateTag`, `runTitle` and the verdict read, so the one new
daemon field feeds every one of them. `KindWord` (task 8) is a value
type with an id, like every other row the application draws in a `Grid`
or a `ForEach`.
