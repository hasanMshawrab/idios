# m8 steps A and B - daemon: the first deletion timestamp and the inversion

Goal: after this plan the daemon keeps the moment a pod's termination
began, opens nothing for a routine eviction, opens nothing for a probe or
a stuck check on a terminating pod, records a container that dies badly
while its pod terminates as `unclean_exit`, and answers a new list state,
`attention`, that keeps a recently closed, unacknowledged row in front of
the person for a configured window. Every category vocabulary agrees on
the new value, the smoke cluster reproduces the new shape, and the docs
say what the code does.

Architecture: one line in `ingest.DiffPod` (step A); the category rules
in `internal/incident/category.go` and the guard in `apply.go`; two
gates, one in `ApplyEvent` and one in `store.ListStuckCandidates`; a
predicate and a window in `internal/query` and `internal/config`, served
by `internal/api`; the vocabulary in the proto, the schema, the store,
the API enum map, the MCP jsonschema, the Swift enum and the docs. No
new process, no timer: the terminating signal is a column, the attention
window is a comparison against an injected clock.

Tech stack: Go 1.26, modernc.org/sqlite v1.57.0, k8s.io v0.37.0, sebuf
(`make generate` into `internal/apigen` and `api/openapi`), Swift package
`macos/Sources/IdiosModel` (`make app-test`).

Spec: `docs/design/data-storage.md` sections 5.3, 6.1 and 9 as amended
by tasks 1 and 2 of this plan; `docs/design/presentation.md` sections
4.2, 4.6 and 13 as amended by task 5. Roadmap decisions 1 to 6 and 10 of
`docs/plans/m8-noise/roadmap.md`.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Every timestamp through `clock.Format`; mutation SQL
in `internal/store`, read-model SQL in `internal/query`; categories key
on the reason, the event's source component and whether the pod is
terminating, never on phase, exit code or message. The schema changes,
so `rm -rf .storage .storage/smoke` before the next run;
`rm -rf macos/.build` before `make app-test` because the proto changes;
`make generate-check` after the commit that touches `api/proto`. No
fixture, test, comment or doc names a real organisation, cluster,
namespace, workload, image or node.

## File structure

    docs/design/data-storage.md                     5.3 (task 1); 6.1, 9, 11 (task 2)
    internal/ingest/pod.go                          DiffPod keeps the first deletion timestamp (task 1)
    internal/ingest/pod_test.go                     TestFirstDeletionTimestampIsKept (task 1)
    api/proto/idios/v1/common.proto                 CATEGORY_UNCLEAN_EXIT, INCIDENT_STATE_ATTENTION (task 2)
    api/proto/idios/v1/incidents.proto              the two vocabulary comments (task 2)
    api/proto/idios/v1/status.proto                 attention_window_seconds = 28 (task 2)
    internal/store/rows.go                          CategoryUncleanExit (task 2)
    internal/store/migrations/0001_init.sql         incidents.category CHECK (task 2)
    internal/store/ingest_sql_test.go               CHECK accepts the value (task 2)
    internal/api/enums.go                           categories, incidentStates gain a row each (task 2)
    internal/api/wire_test.go, api/testdata/*.json  counts and status fixtures (task 2)
    internal/mcp/tools.go                           state and category jsonschema strings (task 2)
    internal/mcp/vocabulary_test.go                 TestFilterSchemasNameEveryWireValue (task 2)
    macos/Sources/IdiosModel/Enums.swift            Category.uncleanExit (task 2)
    macos/idios/Views/Components/BadgeStyle.swift   .uncleanExit is red (task 2)
    macos/idios/Views/Incidents/IncidentsSidebar.swift  ranked list written out (task 2)
    macos/Tests/IdiosModelTests/IncidentTests.swift counts fixture expectation (task 2)
    README.md, CLAUDE.md, docs/mockups/idios-ui.html, docs/design/process-architecture.md,
    docs/design/client-go-methods.md                the vocabulary and the rule (task 2)
    internal/incident/category.go                   ConditionCategory narrows; ContainerCategory(terminating) (task 3)
    internal/incident/category_test.go              tables and the enforcing test (task 3)
    internal/incident/apply.go                      terminating flag, guard, category-blind attach (task 3)
    internal/incident/apply_test.go                 terminating rows; the eviction-API row (task 3)
    internal/ingest/testdata/terminating-exit/      before.json, after.json, oom.json, first-sight-pull.json (task 3)
    internal/ingest/testdata/disruption-taint/      after.json (task 3)
    internal/processor/pod_test.go                  condition row without an incident; unclean_exit captures (tasks 3, 4)
    internal/incident/event.go, event_test.go       probe gate (task 4)
    internal/store/stuck_sql.go, stuck_sql_test.go  stuck gate (task 4)
    internal/processor/event_test.go                probe-only shutdown captures nothing (task 4)
    internal/processor/delete_test.go               unclean_exit closes pod_deleted (task 4)
    internal/config/config.go, config_test.go       AttentionWindow (task 5)
    internal/query/incidents.go, incidents_test.go  StateAttention, AttentionSince, counts (task 5)
    internal/api/incidents.go, status.go, *_test.go the window applied and served (task 5)
    docs/design/presentation.md                     4.2, 4.6, 13 (task 5)
    hack/smoke/graceful-exit.yaml, hack/smoke/run.sh  the reproduction (task 6)

## Task 1 - step A: the first deletion timestamp

Spec first. In `docs/design/data-storage.md` section 5.3 the
`deletion_requested_at` row says: the first `metadata.deletionTimestamp`
observed; the API server rewrites the field when a later delete arrives
with a shorter grace period and the kubelet's final delete does exactly
that, so the last object seen carries the moment of removal and not the
moment termination began; the row keeps the first value. The
`deletion_reason` row's enumeration gains `evicted`, and the paragraph
below it gains the sentence the code already implements: a
`DisruptionTarget=True` condition at delete time is `evicted` and is
checked before every other input.

Test first, `internal/ingest/pod_test.go`, table-driven, whole
`*string` assertions on `changes.Pod.DeletionRequestedAt`:

- snapshot without a timestamp, object with one: the object's value;
- snapshot with one, object with a later one: the snapshot's value;
- snapshot with one, object without: the snapshot's value;
- first sight with one: the object's value.

The object is `disruption-api/after.json` with `DeletionTimestamp` set
in code, as `TestPodRowMapsEveryField` already does. Trace: section 5.3
as amended above.

Implementation, one line in `DiffPod` beside the other carried-over
deletion columns:

    if snap.Pod.DeletionRequestedAt != nil {
        ch.Pod.DeletionRequestedAt = snap.Pod.DeletionRequestedAt
    }

with the one-sentence reason inline. `UpsertPod` writes `changes.Pod`
whole, so the row, the diff and the write stay one thing.

Checkpoint: `go build ./... && go test ./... && make ascii`. Commit:
`ingest: keep the first deletion timestamp of a pod`.

Consumes: `ingest.DiffPod`, `store.Pod.DeletionRequestedAt`.
Produces: the invariant every later task reads: `deletion_requested_at`
is the moment termination began.

## Task 2 - the vocabulary

Spec first, all of `docs/design/data-storage.md` in one edit:

- 6.1 first paragraph: the rule gains "and whether the pod is
  terminating (`pods.deletion_requested_at`)"; the parenthesis naming a
  process doc section is dropped, the test is named by package.
- 6.1 table: `crash` says "on a pod that is not terminating";
  `unclean_exit` row added after `crash`: container terminated with
  reason `Error` while `deletion_requested_at` is set, one row per
  container, exit 1 (the app dies on SIGTERM instead of draining) and
  exit 137 (the process outlives its grace and is killed) alike, the
  code and the signal on the history row and in the row's own sentence;
  opens only when the pod has no open incident of any category on any
  container; when the same container has one, the termination attaches
  to it and the history row takes that incident's category; when
  another part of the pod has one, the history row says `unclean_exit`
  with no incident; a crash-looping container that also mishandles
  SIGTERM never has the SIGTERM defect named. `OOMKilled` on a
  terminating pod stays `oom`. `probe` row gains "and the pod is not
  terminating" beside the ready check, with the reason (readiness
  failing on a container that is being killed says nothing).
  `rescheduled` row: the condition side lists `PreemptionByScheduler`,
  `DeletionByTaintManager`, `DeletionByPodGC` and says why
  `EvictionByEvictionAPI` opens nothing (decision 1, in the doc's
  words); the event side is unchanged.
- 6.1 `stuck` paragraph: "with no incident already open on it and no
  `deletion_requested_at`".
- 6.1 last paragraph and section 9 "Severity / actionable flags" row:
  rewritten per decision 5. A mute chooses by category or identity; the
  autoscaler noise was categorised `crash` and `probe` on pods whose one
  true fact was routine, so the fix was the category rule, not a flag.
- Section 11 gains `attention_window`, 24 hours, "the list's default
  view (presentation doc)".

Then the vocabulary, in one change, every place decision 6 names:

- `common.proto`: `CATEGORY_UNCLEAN_EXIT = 12 [(sebuf.http.enum_value) =
  "unclean_exit"]`; `INCIDENT_STATE_ATTENTION = 8 [(sebuf.http.enum_value)
  = "attention"]` with a comment: a filter and a count value, never a
  row's own state. `incidents.proto`: the four comments enumerating
  states and categories. `status.proto`: `int32 attention_window_seconds
  = 28`. `make generate`.
- `store.CategoryUncleanExit = "unclean_exit"`; the CHECK in
  `0001_init.sql`; `ingest_sql_test.go`'s CHECK table gains the row.
- `internal/api/enums.go`: `categories` and `incidentStates` gain a row
  each; `TestEveryStoreVocabularyHasAWireValue` then holds again.
- `internal/mcp/tools.go`: the `State` and `Category` jsonschema strings
  gain `attention` and `unclean_exit`.
- Swift: `Category.uncleanExit = "unclean_exit"`; `BadgeStyle` puts it
  with `.crash` in red; `IncidentsSidebar.categories` written out as
  `crash, oom, unclean_exit, image_pull, config, probe, scheduling,
  stuck, node_pressure, rescheduled, job_failed`. The `IncidentState`
  Swift enum is step C's.
- `README.md` category list; `CLAUDE.md` category rule sentence;
  `docs/mockups/idios-ui.html`: `.c-unclean_exit` in both appearances
  beside `.c-crash`, and the badge in both C1 rows; `process-architecture.md`
  line naming the disruption scenario and `client-go-methods.md`'s
  category split sentence say what the code does after task 3
  (code-is-truth rule 3: a doc that disagrees with the code is fixed,
  not left standing).

Test first, `internal/mcp/vocabulary_test.go`,
`TestFilterSchemasNameEveryWireValue`: for each of `State` and
`Category`, the jsonschema tag of `listIncidentsArgs` names exactly the
`enum_value` strings of `idiosv1.IncidentState` and `idiosv1.Category`
(every value but unspecified), read from the enum descriptor with
`proto.GetExtension(..., sebufhttp.E_EnumValue)`, in the schema's
wording (comma-separated, `or` before the last). This is the agreement
test decision 6 asks for, built from one source: `internal/mcp` may
import only `idiosv1` and `sanitize` (`internal/archtest`), so the
proto is the source it can reach, and `internal/api`'s existing
vocabulary test already ties the proto to the store's constants; the two
together are the chain store -> proto -> jsonschema. Trace: decision 6.

Wire fixtures: `incident_counts` gains `{ATTENTION, 5}` in `by_state`
and `{UNCLEAN_EXIT, 1}` in `by_category`; the status fixture gains
`AttentionWindowSeconds: 86400`. `go test ./internal/api -update`, then
the Swift `incidentCountsBecomeOneEntryPerStateAndCategoryFolder`
expectation grows by the same two entries (`IncidentState.attention` is
step C's, so the Swift counts test maps the new state only once C adds
the case; until then `IncidentCounts(wire:)` must not fail on it -
check how the model treats an unknown state, and if it throws, add the
`attention` case to the Swift `IncidentState` enum here rather than in C
and say so in the report).

Checkpoint: `go build ./... && go test ./... && make ascii && rm -rf
macos/.build && make app-test && make app`. Commit:
`api: add the unclean_exit category and the attention state`. Then
`make generate-check`.

Consumes: nothing from task 1.
Produces: `store.CategoryUncleanExit`,
`idiosv1.Category_CATEGORY_UNCLEAN_EXIT`,
`idiosv1.IncidentState_INCIDENT_STATE_ATTENTION`,
`idiosv1.Status.AttentionWindowSeconds`, the schema CHECK.

## Task 3 - the category rules and the guard

Fixtures, `internal/ingest/testdata/terminating-exit/`, the pod
`pod-term` (`web-7d9f8c6b5-term1`, ReplicaSet `web-7d9f8c6b5`, node
`node-a`, image `registry.example.com/web:1.4.2`):

- `before.json`: running, ready, no `deletionTimestamp`.
- `after.json`: `deletionTimestamp` set, container `api` terminated,
  reason `Error`, exit 137, signal 9, `restartCount` 0, `Ready=False`.
- `oom.json`: as `after.json` with reason `OOMKilled`.
- `first-sight-pull.json`: `deletionTimestamp` set, container waiting
  `ImagePullBackOff`, no snapshot (first sight).

`internal/ingest/testdata/disruption-taint/after.json`: as
`disruption-api/after.json` with reason `DeletionByTaintManager`,
`pod-drain` again so `disruption-api/before.json` is its before.

Tests first.

`category_test.go`: `TestContainerCategoryKeysOnReason` gains the
`terminating` column; rows: `terminated Error terminating ->
unclean_exit`, `terminated OOMKilled terminating -> oom`, `waiting
CrashLoopBackOff terminating -> crash`, `waiting ImagePullBackOff
terminating -> image_pull`, `terminated Error not terminating -> crash`.
`TestConditionAndPodReasonCategories`: `EvictionByEvictionAPI -> ""`,
the other three unchanged. `TestCategoriesNeverReadPhaseExitCodeOrMessage`:
the comment states the amended rule; the AST check is unchanged (the
flag is a parameter, not a field read). Trace: section 6.1 as amended.

`apply_test.go`, rows in `TestApplyScenarios`:

- "disruption by eviction api opens nothing": `Ops{}`.
- "disruption by taint manager is rescheduled": the `rescheduled` open,
  reason `DeletionByTaintManager`.
- "unclean exit on a pod with nothing open opens": `unclean_exit` on
  `api`, reason `Error`, `HistoryIndexes: [0]`, history category
  `unclean_exit`.
- "unclean exit attaches to the container's open crash": one `Attach`
  to the crash row, history category `crash`.
- "unclean exit attaches to the container's open probe": history
  category `probe`.
- "unclean exit under a pod-level rescheduled opens nothing": `Ops{}`
  except history category `unclean_exit` with no index claimed.
- "oom while terminating is still oom".
- "first sight image pull on a terminating pod opens".

Trace: the `unclean_exit`, `oom`, `image_pull` and `rescheduled` rows of
section 6.1.

`internal/processor/pod_test.go`, `TestPodScenariosWriteRows`: a row for
`disruption-api` asserting the condition row is written and no incident
exists (the history stays, the category changes). Trace: 6.1
`rescheduled` row and the milestone goal's "no history row or event row
stops being recorded".

Implementation.

`category.go`:

    func ContainerCategory(state, reason, lastTerminatedReason string, terminating bool) string

`terminated Error` returns `unclean_exit` when `terminating`, `crash`
otherwise; nothing else reads the flag. `ConditionCategory` drops
`EvictionByEvictionAPI` from the `rescheduled` list; the doc comment
carries decision 1's one sentence. `schedulingMessage` in the processor
calls `ConditionCategory` unchanged.

`apply.go`: `terminating := changes.Pod.DeletionRequestedAt != nil`,
passed to both `ContainerCategory` calls. For a problem whose category
is `unclean_exit`, before it joins `problems`:

    switch {
    case openOnContainer(incidents, uid, container) found:
        p.category = that incident's category   // attaches through the normal loop
    case anyOpen(incidents, uid):
        history category stays unclean_exit; the problem is dropped
    }

`openOnContainer` is the category-blind lookup decision 2 names; it
returns the open pod incident on that container, if any. The
`HistoryCategories[i]` entry follows `p.category` in the attach case
(the history row and the incident it names never disagree) and stays
`unclean_exit` in the dropped case. Two containers dying in the same
update with nothing open open two rows: the guard reads `incidents`,
not the opens of this batch. The reopen path is untouched: with no
open incident on the pod, `resolve` may still reopen a `recovered`
`unclean_exit` of the same container, which is section 6.2 step 3.

Checkpoint, commit:
`incident: open unclean_exit for a bad death on a terminating pod`.

Consumes: `store.CategoryUncleanExit`, `changes.Pod.DeletionRequestedAt`.
Produces: `ContainerCategory(state, reason, last, terminating)`;
the fixture `terminating-exit` for task 4.

## Task 4 - the two gates, and what the gate costs

Tests first.

`event_test.go`, `TestApplyEventProbeGate`, two rows with a pod whose
`DeletionRequestedAt` is set: "unhealthy on a terminating pod opens
nothing" (`Ops{}`, `OpenIndex -1`) and "unhealthy on a terminating pod
attaches without a bump" (the open probe row's id). Trace: 6.1 `probe`
row as amended.

`stuck_sql_test.go`: a pod `p-terminating`, old, waiting, live, with
`deletion_requested_at` set: not a candidate. Trace: 6.1 `stuck`
paragraph as amended.

`internal/processor/event_test.go`: "unhealthy on a terminating pod
requests no capture": the `terminating-exit/before.json` pod with
`deletion_requested_at` set through a pod update carrying the timestamp
(feed `after.json` with the container still running - build it in the
test from `before.json` with `DeletionTimestamp` set), then the
`Unhealthy` event; no incident and no open-time `log_current` request
(the early capture for the `Unhealthy` reason still fires: decision 2
leaves the cache unchanged). `internal/processor/pod_test.go`,
`TestPodScenariosEnqueueCaptures`: "unclean exit captures like any
death": `terminating-exit` before and after requests `log_previous`
index 0 (not previous), `log_current` `TriggerIncidentOpen`, `pod_json`
`TriggerRestart`. Trace: decision 2's "what the gate costs" and section
6.2 steps 4 and 5.

`delete_test.go`, `TestPodDeletedScenarios`: "terminating pod's unclean
exit closes pod_deleted" with the `terminating-exit` steps, one closed,
one `TriggerDelete` request. Trace: section 6.3 "Pod deleted" row and
decision 4.

Implementation: `ApplyEvent` adds `pod.DeletionRequestedAt == nil` to
the probe-open condition, with the sentence in the doc comment.
`ListStuckCandidates` adds `AND p.deletion_requested_at IS NULL` with a
one-line reason in the doc comment.

Checkpoint, commit:
`incident: open nothing on a probe or a stuck check while a pod terminates`.

Consumes: `terminating-exit` fixtures, `store.Pod.DeletionRequestedAt`.
Produces: nothing new; the gates.

## Task 5 - attention

Spec first, `docs/design/presentation.md`: 4.2 `GET /v1/incidents`
`state` list gains `attention`, defined in the right column: open, or
closed within `attention_window` and never acknowledged or dismissed;
a row never reports it as its own state. 4.2 counts row: `attention` is
one more count, by the same predicate. 4.6: the configured intervals
include `attention_window_seconds`. Section 13 gains `attention_window`,
24 h, "Section 4.2; the list's default view".

Tests first.

`config_test.go`: `TestDefaultsMatchDesign` gains `AttentionWindow: 24 *
time.Hour`; `TestValidateRejectsZeroOrNegative` gains the negative case
(zero is a setting: attention is then exactly the open set). Trace:
section 13 of the presentation doc.

`internal/query/incidents_test.go`: `TestListIncidentsDerivesState`
gains the attention rows over the seed with an explicit cutoff:
a cutoff before every close keeps the open rows, the acknowledged open
row and the closed unacknowledged undismissed rows (4, 6, 10, 11) and
drops the dismissed ones (3, 7); a cutoff between the close of 4 and the
close of 6 drops 4; acknowledging 6 in the test drops it. Trace: 4.2 as
amended. `TestCountIncidentsFacetsEachAxis`: `ByState["attention"]` in
every existing case (the value the seed yields), and one case with
`state = attention` whose `ByCategory` counts the attention set.

`internal/api/incidents_test.go`: `state=attention` accepted and the
rows it returns; an unknown state still 400. `status_test.go`:
`AttentionWindowSeconds: 86400`.

Implementation.

`config.Config.AttentionWindow time.Duration` (`toml:"attention_window"`),
default 24h, validated `>= 0`.

`internal/query`:

    const StateAttention = "attention"
    type IncidentFilter struct { ...; AttentionSince string }
    const attentionExpr = `(i.dismissed_at IS NULL AND (i.closed_at IS NULL OR (i.closed_at >= ? AND i.acknowledged_at IS NULL)))`

Dismissal gates both branches: the derived state already makes a
dismissed row `dismissed` whatever else holds, so the Open pill hides it
and Attention, which contains Open, must too.

`ListIncidents`: when `f.State == StateAttention` the predicate is
`attentionExpr` bound to `f.AttentionSince`, else the state equality as
today. `CountIncidents(ctx, db, clusterIDs, state, category,
attentionSince string)`: the GROUP BY gains `attentionExpr` as a third
column; `ByState[StateAttention]` sums the rows where it is true under
the category facet, and `state == StateAttention` selects those rows
for `ByCategory`.

`internal/api`: `incidentStates[query.StateAttention] =
INCIDENT_STATE_ATTENTION` (task 2 added the row; the constant moves here
if task 2 spelled it as a literal). `ListIncidents` and
`GetIncidentCounts` compute
`clock.Format(s.clk.Now().Add(-s.cfg.AttentionWindow))`. `status.go`
serves `AttentionWindowSeconds`. `idios status` prints it where it
prints the other windows (check `cmd/idios`).

Checkpoint, commit: `query: add the attention state and its window`.

Consumes: `idiosv1.IncidentState_INCIDENT_STATE_ATTENTION`,
`Status.AttentionWindowSeconds`, `Server.clk`, `Server.cfg`.
Produces: `query.StateAttention`, `IncidentFilter.AttentionSince`,
`config.Config.AttentionWindow`.

## Task 6 - the reproduction

`hack/smoke/graceful-exit.yaml`: Pod `smoke-graceful`, `busybox:1.36`,
`terminationGracePeriodSeconds: 20`, command
`sh -c 'trap "" TERM; touch /tmp/ready; while :; do sleep 1; done'`
(PID 1 ignores SIGTERM), readiness `exec cat /tmp/ready` with
`periodSeconds: 2`, `failureThreshold: 1` (the defaults would take
longer than the grace to fail once), preStop `exec rm /tmp/ready`. The
comment says what it reproduces and that the kubelet's final status
write can lose the race to the object's removal on a single node, in
which case no termination is observed and the query is empty, which is
the race the early-capture cache exists for and not a failure of the
gate.

`run.sh`: after the watch window, `kubectl -n $NS delete pod
smoke-graceful --wait=false`, sleep the grace plus 15 seconds, then a
labelled `sqlite3` query of the incidents naming that pod (category,
close reason, exit code and signal from the history row), expected one
`unclean_exit` closed `pod_deleted` and no `probe`, before the existing
tables. `hack/smoke/` is applied whole, so the fixture joins the run
by existing.

`make smoke` is run and its output is the evidence; the report quotes
the query. `crash-loop.yaml` is step D's.

Checkpoint (`make ascii`), commit: `smoke: add a pod that mishandles SIGTERM`.

## Self-review

Spec coverage: 5.3 (task 1); 6.1 every amended row (tasks 3, 4), the
`stuck` gate (task 4), the last paragraph and section 9 (task 2);
presentation 4.2, 4.6, 13 (task 5). Decisions: 1 and 2 in task 3 and
4; 3 in task 1; 4 in tasks 2 and 5; 5 in tasks 2 and 3; 6 in task 2;
10 everywhere (`web-7d9f8c6b5`, `registry.example.com`, `node-a`,
`smoke-graceful`).

`.ai` rules: ascii (checked per task); tests trace to 6.1 rows, 6.2
steps, 6.3, decision 2's cost statement, 4.2 and 13 - variants are
table rows, assertions are whole `Ops`, whole row sets, whole `Status`;
comments carry the kubelet facts (the rewritten deletionTimestamp, the
readiness miss on a dying container, the grace-period kill) and no
what; code-is-truth: no doc named from code, the two design docs that
would disagree are fixed in task 2; scope: no `muted` flag, no elapsed
time rule for a pod stuck terminating, no `IncidentState.attention` in
Swift unless decoding forces it; commits per task, none red, none
without the user's review.

Type consistency: `ContainerCategory`'s fourth parameter is `bool` in
task 3 and both callers in `apply.go`; `IncidentFilter.AttentionSince`
and `CountIncidents`'s last parameter are `clock.Format` strings;
`AttentionWindowSeconds` is `int32` like the other windows;
`CategoryUncleanExit` is spelled `unclean_exit` in the store, the CHECK,
the proto `enum_value`, the jsonschema, Swift and the docs.
