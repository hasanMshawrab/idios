# Phase 10: writes and cluster onboarding

Goal: the twelve human actions and configuration endpoints of
`presentation.md` Section 8 exist in the daemon, each idempotent, each
going through `store.Writer.Tx`, each emitting on the incident or cluster
stream; `idios cluster add` and `idios ns add` are clients of them with the
database fallback `idios status` has; the application's action buttons
work, the two destructive ones confirm, and a cluster can be added from
the application. Three daemon gaps found while using the Phase 9
application are closed first.

Architecture: unchanged from Phase 8. `internal/api` gains a `*store.Writer`
seam (`WithWriter`) and the write handlers; mutation SQL is added to
`internal/store` in `action_sql.go` (incident human actions) and
`config_sql.go` (clusters and namespaces); file and directory removal for
the two hard deletes lives in `internal/api` beside the content endpoint,
because the store never touches files. The application adds write methods
to the stores that already own the rows, a note editor, two confirmation
alerts and two sheets (add cluster; clusters and namespaces).

Tech stack: unchanged. Go 1.26, sebuf, `modernc.org/sqlite` v1.57.0,
Swift 6, `swift-openapi-generator` through the package plugin.

Spec: `presentation.md` Sections 2 (the CLI commands become clients),
3.1 (wire rules), 5 (a human action emits on the incident stream), 8 (the
twelve endpoints), 9.3 (screens), 10 (security), 11 (testing);
`data-storage.md` Sections 5.1, 5.2, 5.7, 6.4 and 7.

Global constraints: every `.ai/*.md` rule; the m2 cross-cutting decisions
2 (generated code is never edited; `make generate-check` clean), 3 (wire
rules), 4 (endpoint, handler, client operation; never RPC), 5 (the
application never opens the database, the files or the kubeconfig), 7
(Swift follows the Go rules). Every timestamp written goes through
`clock.Format` of the injected clock. Every mutation is one `Writer.Tx`.
Notifications are sent after the commit, never inside the transaction.
Migrations are not touched: every write lands in existing columns.

Model per task: Sonnet implements, Opus reviews the diff against the task
and the `.ai` rules before the next task starts. The orchestrator reviews
every application screenshot itself.

## Decisions this plan makes

Written here because the spec left them open; Task 16 moves the ones that
outlive the phase into `presentation.md`.

1. **Idempotency is "the state after the call is the state asked for".**
   Acknowledge twice, dismiss twice, resolve twice, note twice with the
   same text, add a namespace twice: the second call changes nothing and
   answers 200 with the same row. Every incident UPDATE is written as
   `SET col = COALESCE(col, ?) WHERE id = ?`, so one statement both keeps
   the first value and reports through `RowsAffected` whether the row
   exists (SQLite counts matched rows, not changed ones). An unknown id is
   a 404 for every incident endpoint and every cluster endpoint that
   names an id.
2. **The two hard deletes answer 200 with an empty message whether or
   not the row existed.** After the call the row is gone either way, and
   the application calls them from a screen that already holds the row.
3. **`POST /v1/clusters` refuses a second cluster of the same name** with
   a 400 violation on `name` (`cluster "prod" exists (id 1)`), as the CLI
   does today: names are what a person types into `idios ns add` and the
   schema does not make them unique. This is the one create that is not
   safe to repeat, and it says so.
4. **`idios ns add` prints `watching <ns> in <cluster>` whether or not the
   row was new**, on both paths. The API answers the cluster row, not a
   created flag, and the two paths must print the same thing.
5. **The controller-less workload has no detail endpoint.** Go's
   `ServeMux` cannot match an empty path segment (`/none/` is a 404 from
   the mux before any handler runs), so
   `GET /v1/workloads/{cluster_id}/{namespace}/{kind}/{name}` requires a
   name and the contract says so. The application already shows that row
   from the list and `ListPods`; nothing else changes.
6. **A deleted incident leaves the stream silent.** The stream reloads a
   row by id and a missing row sends nothing; the application drops the
   row itself, because the only place a delete comes from is the
   application's own detail screen. The same holds for a removed cluster.
7. **Streams end on shutdown through `http.Server.RegisterOnShutdown`.**
   Every stream selects on one server-wide channel that Shutdown closes;
   in-flight request-and-response handlers keep their grace period
   untouched, which is what the grace period exists for.
8. **The application confirms with an `.alert`** (delete incident, remove
   cluster), and edits the note in a `.sheet`. Alerts and sheets are
   separate windows to the window server, so `hack/macos/screenshot.sh`
   photographs the screen behind them; the confirmation copy is reviewed
   in the diff, the screen state before and after in the screenshot.

## File structure

```
api/proto/idios/v1/
  incidents.proto              + seven request messages, DeleteIncidentResponse
  clusters.proto               + five request messages, DeleteClusterResponse
  service.proto                + twelve operations; GetWorkload doc comment
api/openapi/, internal/apigen/ regenerated (make generate)
api/testdata/                  unchanged (responses reuse IncidentRow and Cluster)
internal/store/
  action_sql.go                AcknowledgeIncident, UnacknowledgeIncident,
                               ResolveIncident, DismissIncident,
                               UndismissIncident, SetIncidentNote
  action_sql_test.go
  config_sql.go                + GetCluster, RenameCluster, RemoveCluster,
                               RemoveWatchedNamespace
  config_sql_test.go           + tests for the four
internal/api/
  server.go                    WithWriter, stopping channel, RegisterOnShutdown,
                               errNoWriter
  streams.go                   stream() selects on s.stopping
  streams_test.go              + shutdown test
  actions.go                   the seven incident write handlers
  actions_test.go
  files.go                     removeArtifactFiles, removeClusterDir
  clusters_writes.go           the five cluster write handlers
  clusters_writes_test.go
  api_test.go                  testStack gains server and writer
  mock/mock.go                 + twelve methods answering fixtures
cmd/idios/
  config_cmd.go                cluster add and ns add through the API first
  config_cmd_test.go           + the daemon path
  run.go                       .WithWriter(st.Writer)
  api_cmd.go                   unchanged
hack/smoke/run.sh              abort when idios run has exited
macos/idios/
  Store/IncidentDetailStore.swift   + acknowledge, unacknowledge, resolve,
                                    dismiss, undismiss, setNote, delete
  Store/IncidentsStore.swift        + remove(id:connection:)
  Store/ClustersStore.swift         + add, rename, remove, addNamespace,
                                    removeNamespace, contexts, namespaces(of:)
  Views/IncidentDetail/IncidentDetailScreen.swift  toolbar actions live
  Views/IncidentDetail/IncidentHeader.swift        callout removed
  Views/IncidentDetail/NoteSheet.swift             new
  Views/Clusters/AddClusterSheet.swift             new
  Views/Clusters/ClustersSheet.swift               new
  Views/Incidents/IncidentsSidebar.swift           footer is a button; checkbox
                                                   disabled with one cluster
  Views/Incidents/IncidentsScreen.swift            sheet state, routes
  Views/MenuBar/MenuBarView.swift                  Clusters and namespaces... live
  App/Route.swift                                  + addCluster, clusters
docs/design/presentation.md   Sections 2, 4.5, 8, 9.3 updated in place
CLAUDE.md                     durable facts
docs/plans/m2-presentation/roadmap.md   Phase 10 status, milestone status
docs/plans/README.md          m2 row complete
```

## Task 1: streams end when the server shuts down

Bug (roadmap, "Facts a phase plan needs"): `idios run` always waits out
`shutdownGrace` while a client holds a stream, because `sseSender`'s
handler blocks on `r.Context()`, which `http.Server.Shutdown` does not
cancel.

Test, `internal/api/streams_test.go`,
`TestStreamsEndWhenTheServerShutsDown`: `newTestStack` with a notifier;
open the incident stream and the cluster stream; call the server's
`stopStreams()`; both row channels close within `streamWait`. Then
`TestRunReturnsPromptlyWithAStreamOpen`: pick a free loopback port (listen
on `127.0.0.1:0`, read the address, close), set `cfg.APIListen` to it,
start `Run` in a goroutine, poll until `GET /v1/status` answers, open a
stream with the generated client, cancel `Run`'s context; `Run` returns
within one second (well under `shutdownGrace`) with a nil error, and the
stream's `Next` returns false. Trace: the roadmap bug; process
architecture's "every component stops when the context ends".

Implementation, `server.go`: `Server` gains `stopping chan struct{}` and
`stopOnce sync.Once`; `New` makes the channel; `stopStreams()` closes it
once. `Run` calls `srv.RegisterOnShutdown(s.stopStreams)` before `Serve`.
`streams.go`: the select in `stream` gains `case <-s.stopping: return
nil`. The generated handler writes an `event: error` line when a handler
returns an error after committing, so the return is nil: the client sees
the stream end and reconnects, which is the contract in Section 5.
`api_test.go`: `testStack` gains `server *Server`.

Checkpoint, commit `api: end the streams when the server shuts down`.

Consumes: `Server`, `stream`, `newTestStack`, `openStream`.
Produces: `Server.stopStreams`, `testStack.server`.

## Task 2: the smoke script aborts when the daemon exits early

Bug (roadmap): `hack/smoke/run.sh` keeps going when `idios run` exits at
once (port 7770 in use), applies the pods and reports nothing.

No Go test: the script is shell and the check is one `kill -0`. Verify by
hand: start `./bin/idios -data-dir .storage mock`, run `make smoke`, the
script exits 1 after five seconds with `idios run exited early; see its
output above`, and `kubectl -n idios-smoke get pods` shows nothing was
applied. Stop the mock afterwards.

Implementation: after `sleep 5`, `if ! kill -0 "$pid" 2>/dev/null; then
wait "$pid" || true; pid=; echo "idios run exited early; see its output
above" >&2; exit 1; fi`. Clearing `pid` keeps `cleanup` from signalling a
process that is gone; the pod delete in `cleanup` stays, it is
`--ignore-not-found`.

Checkpoint (`make ascii`), commit `smoke: abort when idios run exits
early`.

## Task 3: the contract says the controller-less workload has no detail

Decision 5. No test: a documentation change to the contract and the
design doc.

Implementation: in `service.proto`, a doc comment on `GetWorkload`: "name
is required: a pod without a controller belongs to workload kind none
with an empty name, and that workload has no detail; the list row and
`ListPods` filtered by `workload_kind=none` are what a client shows for
it." `make generate` (the comment lands in the OpenAPI description);
`make generate-check` clean. `presentation.md` Section 4.5: add the same
sentence to the `GET /v1/workloads/{...}` row. Remove the item from the
roadmap's "Facts a phase plan needs" list? No: the roadmap is frozen at
milestone close and rule 3 of the plans README says nothing true only
during a phase is kept; the fact is now in the design doc, so delete the
sentence from the roadmap bullet in Task 16 when the status lines move.

Checkpoint, commit `api: say the controller-less workload has no detail
endpoint`.

## Task 4: incident human-action helpers in the store

Spec: `data-storage.md` Section 6.4 (acknowledge sets `acknowledged_at`
and nothing else; mark resolved sets `closed_at = now, close_reason =
manual`; dismiss sets `dismissed_at`; note is free text);
`presentation.md` Section 8 (acknowledge only if null; resolve only if
open; `DELETE .../acknowledge` and `.../dismiss` clear; empty note
clears).

File: `internal/store/action_sql.go`.

```go
// AcknowledgeIncident stamps acknowledged_at once and reports whether the
// incident exists. A second call keeps the first stamp.
func AcknowledgeIncident(ctx context.Context, tx *sql.Tx, id int64, at string) (bool, error)
func UnacknowledgeIncident(ctx context.Context, tx *sql.Tx, id int64) (bool, error)
// ResolveIncident closes an open incident as manual and reports whether the
// incident exists. A closed incident keeps its close.
func ResolveIncident(ctx context.Context, tx *sql.Tx, id int64, closedAt string) (bool, error)
func DismissIncident(ctx context.Context, tx *sql.Tx, id int64, at string) (bool, error)
func UndismissIncident(ctx context.Context, tx *sql.Tx, id int64) (bool, error)
// SetIncidentNote replaces the note; an empty note clears it.
func SetIncidentNote(ctx context.Context, tx *sql.Tx, id int64, note string) (bool, error)
```

Each is one `execCount` returning `n == 1`. The SQL shapes:
`SET acknowledged_at = COALESCE(acknowledged_at, ?) WHERE id = ?`;
`SET acknowledged_at = NULL WHERE id = ?`;
`SET closed_at = COALESCE(closed_at, ?), close_reason = COALESCE(close_reason, ?) WHERE id = ?`
with `CloseManual`; `SET note = NULLIF(?, '') WHERE id = ?`. Reasons stay
in the doc comments: SQLite reports every matched row as changed, so one
statement is both the idempotent write and the existence check.

Test, `internal/store/action_sql_test.go`,
`TestHumanActionsAreIdempotentAndReportExistence`: table-driven over the
six helpers. Seed one cluster, one pod, one open incident with
`insertPodIncident` (in the existing test helpers); for each row: call
the helper twice with two different timestamps; assert `(true, nil)`
both times; assert the whole `Incident` row (`loadAllIncidents`) equals
the seeded row with exactly the expected fields set, and that the first
timestamp survived the second call; call once with id 999 and assert
`(false, nil)` and the row unchanged. Rows: acknowledge; unacknowledge
after acknowledge (field back to nil); resolve on open (`closed_at`,
`close_reason = manual`); resolve on a recovered incident (close kept as
recovered); dismiss; undismiss; note "fixed in PR 123"; note "" after a
note (nil). Edge rows first: the unknown id, the already-closed resolve.
Trace: Section 6.4 and Section 8 rows above; the "kept across reopen"
sentence is `AttachIncident`'s and already tested there.

Checkpoint, commit `store: add the incident human-action helpers`.

Consumes: `execCount`, `CloseManual`, `Incident`, `scanIncident`,
`insertPodIncident`, `loadAllIncidents`.
Produces: the six functions above.

## Task 5: cluster configuration helpers in the store

Spec: `data-storage.md` Section 5.1 (`name` user-editable; removing a
cluster is one `DELETE FROM clusters` plus its artifact directory),
Section 5.2 (`UNIQUE (cluster_id, name)`); `presentation.md` Section 8
(`PATCH` renames only; `DELETE .../namespaces/{name}` deletes the row).

File: `internal/store/config_sql.go`, appended.

```go
// GetCluster returns one cluster row, nil when there is none.
func GetCluster(ctx context.Context, tx *sql.Tx, id int64) (*Cluster, error)
// RenameCluster sets the friendly name and reports whether the row exists.
func RenameCluster(ctx context.Context, tx *sql.Tx, id int64, name string) (bool, error)
// RemoveCluster deletes the row; the cascades take its namespaces, pods,
// jobs, incidents, events and rollouts. Reports whether a row was there.
func RemoveCluster(ctx context.Context, tx *sql.Tx, id int64) (bool, error)
// RemoveWatchedNamespace stops watching name in the cluster and reports
// whether a row was there.
func RemoveWatchedNamespace(ctx context.Context, tx *sql.Tx, clusterID int64, name string) (bool, error)
```

Tests, `internal/store/config_sql_test.go`:

- `TestRenameClusterChangesNameOnly`: insert a cluster, mark it
  connected, rename twice to the same name; the whole `Cluster` row
  equals the connected row with the new name, `(true, nil)` both times;
  id 999 is `(false, nil)`. Trace: Section 5.1 "friendly name,
  user-editable"; Section 8 "`name` only".
- `TestRemoveClusterCascades`: two clusters, each with a watched
  namespace, a pod, an incident on the pod, an event and an artifact row
  (inline inserts as the sweep tests do); remove cluster 1 twice
  (`(true, nil)` then `(false, nil)`); assert with `CountRows`-style
  `SELECT COUNT(*)` per table that every row of cluster 1 is gone and
  every row of cluster 2 is intact, comparing a whole struct of counts.
  Trace: Section 5, "Foreign keys ... Removing a cluster is one `DELETE
  FROM clusters`".
- `TestRemoveWatchedNamespaceLeavesTheOthers`: cluster with `a`, `b`;
  remove `a` twice (`true`, then `false`); `ListWatchedNamespaces` is
  `["b"]`; removing from cluster 999 is `(false, nil)`. Trace: Section 8
  last row.

Checkpoint, commit `store: add the cluster configuration helpers`.

Consumes: `execCount`, `scanCluster`, `clusterColumns`, `insertCluster`,
`insertPod`, `mustExec`, `inTx`.
Produces: the four functions above.

## Task 6: the twelve operations in the contract

Spec: `presentation.md` Section 8 (paths and methods), Section 3.1 (wire
rules), Section 3.2 (additive within `v1`).

`incidents.proto` gains:

```proto
message AcknowledgeIncidentRequest { int64 id = 1; }
message UnacknowledgeIncidentRequest { int64 id = 1; }
message ResolveIncidentRequest { int64 id = 1; }
message DismissIncidentRequest { int64 id = 1; }
message UndismissIncidentRequest { int64 id = 1; }
// SetIncidentNoteRequest carries the whole note; an empty note clears it.
message SetIncidentNoteRequest { int64 id = 1; string note = 2; }
message DeleteIncidentRequest { int64 id = 1; }
// DeleteIncidentResponse is empty: the row is gone, there is nothing to
// return.
message DeleteIncidentResponse {}
```

`clusters.proto` gains:

```proto
// AddClusterRequest names a kubeconfig context and the friendly name the
// application shows for it.
message AddClusterRequest { string context_name = 1; string name = 2; }
message RenameClusterRequest { int64 id = 1; string name = 2; }
message DeleteClusterRequest { int64 id = 1; }
message DeleteClusterResponse {}
message AddWatchedNamespaceRequest { int64 id = 1; string name = 2; }
message RemoveWatchedNamespaceRequest { int64 id = 1; string name = 2; }
```

`service.proto` gains, under the same service, with `field_examples` on
every string the OpenAPI document shows:

| Operation | Method and path | Returns |
|---|---|---|
| `AcknowledgeIncident` | `POST /incidents/{id}/acknowledge` | `IncidentRow` |
| `UnacknowledgeIncident` | `DELETE /incidents/{id}/acknowledge` | `IncidentRow` |
| `ResolveIncident` | `POST /incidents/{id}/resolve` | `IncidentRow` |
| `DismissIncident` | `POST /incidents/{id}/dismiss` | `IncidentRow` |
| `UndismissIncident` | `DELETE /incidents/{id}/dismiss` | `IncidentRow` |
| `SetIncidentNote` | `PUT /incidents/{id}/note` | `IncidentRow` |
| `DeleteIncident` | `DELETE /incidents/{id}` | `DeleteIncidentResponse` |
| `AddCluster` | `POST /clusters` | `Cluster` |
| `RenameCluster` | `PATCH /clusters/{id}` | `Cluster` |
| `DeleteCluster` | `DELETE /clusters/{id}` | `DeleteClusterResponse` |
| `AddWatchedNamespace` | `POST /clusters/{id}/namespaces` | `Cluster` |
| `RemoveWatchedNamespace` | `DELETE /clusters/{id}/namespaces/{name}` | `Cluster` |

The service doc comment stops saying "read side". `id` is bound from the
path; sebuf binds the body first and the path after, so a body that also
carries `id` loses to the URL. The note body is a plain message with a
string field: sebuf allows one JSON-marshaling feature per message and a
wrapped value would need a second.

`make generate`, then `make generate-check`. `internal/api/mock/mock.go`
implements the twelve methods (the interface grew): the incident writes
answer the `incident_row` fixture, the cluster writes the first cluster
of the `clusters` fixture, the deletes an empty response; `mock_test.go`
already asserts every method answers, extend its table if it enumerates
them.

Test: `internal/api/wire_test.go` stays as it is; the responses are
messages it already covers. One new case in `internal/api/api_test.go`
is not needed either. The generated Swift client is checked by `make
app-test` and `make app` in Task 11, where the first body-carrying
operation is used.

Checkpoint (`go build ./... && go test ./... && make ascii && make
generate-check`), commit `api: add the write operations to the contract`.

Consumes: `IncidentRow`, `Cluster`, `service.proto`.
Produces: the twelve request messages, the two empty responses, the
twelve handler and client operations in `idiosv1`.

## Task 7: incident action handlers

Spec: `presentation.md` Section 8 rows one to six; Section 5 ("a human
action changed it" emits the row); Section 3.1 (404 with `message` for an
unknown id).

`server.go`: `WithWriter(w *store.Writer) *Server`; `errNoWriter =
&unconfiguredError{what: "writer"}`, answered when a write arrives on a
server built without one. `api_test.go`: `newTestStack` passes
`st.Writer` unless a test opts out; `testStack` gains `writer`.

`internal/api/actions.go`:

```go
func (s *Server) AcknowledgeIncident(ctx context.Context, req *idiosv1.AcknowledgeIncidentRequest) (*idiosv1.IncidentRow, error)
// ... one per operation ...

// incidentAction runs one store write, then answers the row as the list
// shows it and tells the stream. The notify happens after the commit:
// a subscriber reloads the row on the event, and a row still inside an
// open transaction is the old row.
func (s *Server) incidentAction(ctx context.Context, id int64, write func(*sql.Tx) (bool, error)) (*idiosv1.IncidentRow, error)
```

`incidentAction`: `Writer.Tx` calling `write`; `false` becomes
`&notFoundError{what: "incident", id: ...}`; then
`query.ListIncidents(... IncidentFilter{ID: id}, Page{Limit: 1})` and
`incidentRow`; then `s.events.Notify(notify.Incident, id)`. The
timestamp is `clock.Format(s.clk.Now())`.

Test, `internal/api/actions_test.go`,
`TestIncidentActionsWriteOnceAndAnswerTheRow`: table over the six
operations against the seed (`querytest.Seed`; incident 1 open, 2
acknowledged, 4 recovered, 3 dismissed while open). Each row calls the
operation twice through the generated client and asserts, with
`cmp.Diff` and `protocmp.Transform()`, that both responses equal the
expected `IncidentRow` (the seeded row from `ListIncidents` with the
changed fields set: `AcknowledgedAt: sp(testNow)` and `State:
ACKNOWLEDGED`, and so on) and that `listedIncident` afterwards equals
the response. Rows: acknowledge 1; unacknowledge 2 (state back to
`open`); resolve 1 (`closed_at testNow`, `close_reason manual`, state
`manual`); resolve 4 (unchanged: still `recovered`); dismiss 1 (state
`dismissed`); undismiss 3 (state `open`); note 1 "fixed in PR 123"; note 3
"" (note absent). The unknown-id case is
`TestIncidentActionsSayWhenTheIdIsUnknown`: each of the six against id
999 over plain HTTP (`get` has a sibling `call(method, path, body)`),
status 404, body `{"message":"incident 999 not found"}`.
`TestIncidentActionsEmitOnTheStream`: open the incident stream, acknowledge
1, the next row equals the response. Trace: Section 8 rows; Section 5;
Section 3.1 errors row.

Checkpoint, commit `api: add the incident action endpoints`.

Consumes: Task 4 helpers, `notFoundError`, `unconfiguredError`,
`incidentRow`, `query.ListIncidents`, `notify.Incident`, `listedIncident`,
`incidentStream`.
Produces: `WithWriter`, `incidentAction`, `testStack.writer`, `call`.

## Task 8: delete an incident with its files

Spec: `presentation.md` Section 8 (`DELETE /v1/incidents/{id}`: files
first, then the row; history and events detached; the pod row stays);
`data-storage.md` Section 6.4 (delete) and Section 7 (write order: a
crash between file and row leaves an orphan file, which the sweep
removes; the reverse leaves a row pointing at nothing).

`internal/api/files.go`:

```go
// removeArtifactFiles deletes the files of the given rows under root and
// stops at the first failure, so the rows stay and a retry finds them. A
// file already gone is not a failure: the row is what says it existed.
func removeArtifactFiles(root string, files []store.ArtifactFile) error
```

It uses `underRoot` on every path and refuses one that escapes, as the
content endpoint does. `actions.go`: `DeleteIncident` runs one `Tx`:
`store.ListIncidentArtifactFiles`, `removeArtifactFiles`,
`store.DeleteIncidentArtifacts`, `store.DeleteIncident`; then
`s.events.Notify(notify.Incident, id)` (the stream finds no row and sends
nothing; the notify is for symmetry with every other write and costs one
query) and `&idiosv1.DeleteIncidentResponse{}`. Decision 2: no 404.

Test, `internal/api/actions_test.go`,
`TestDeleteIncidentRemovesFilesAndDetachesHistory`: write the two seeded
files (`prod/pod-crash/pod.json`, `prod/pod-crash/api-0.log`) under
`stack.artifactsRoot`; add a third file the seed's gap row does not own
and a sibling file under the same pod directory owned by no row. Delete
incident 1 twice; both answer the empty response. Assert as one struct:
the incident row is gone (`ListIncidents` with `ID: 1` empty), the pod
`pod-crash` still lists, the three artifact rows of incident 1 are gone
and no other artifact row changed (`SELECT COUNT(*)`), the events and
transitions that carried `incident_id = 1` now carry NULL and are still
there (count before equals count after), the two owned files are gone,
the unowned sibling file is still there. Trace: the Section 8 row and
Section 6.4.

Checkpoint, commit `api: add the delete incident endpoint`.

Consumes: `store.ListIncidentArtifactFiles`, `store.DeleteIncidentArtifacts`,
`store.DeleteIncident`, `underRoot`, `incidentAction`'s pattern.
Produces: `removeArtifactFiles`.

## Task 9: cluster write handlers

Spec: `presentation.md` Section 8 rows eight to twelve; Section 2 (the
supervisor picks the rows up within ten seconds; the endpoints only write
the rows); `data-storage.md` Sections 5.1, 5.2 and 7 (the artifact
directory is `<artifacts_root>/<cluster_id>/`).

`internal/api/clusters_writes.go`: the five handlers plus
`clusterRowAfter(ctx, id) (*idiosv1.Cluster, error)`, which reloads
through `query.ListClusters` and merges `s.clusterState()` as
`ListClusters` does, then notifies `notify.Cluster`. Validation, one
violation per empty field, reported together as `validateListIncidents`
does: `AddCluster` needs `context_name` and `name`; `RenameCluster` and
`AddWatchedNamespace` need `name`. `AddCluster`: inside the `Tx`,
`FindClusterByName`; a hit is a `ValidationError` on `name` with the
CLI's wording (decision 3); else `InsertCluster` with an empty
`api_server_url`, the watcher fills it. `RenameCluster` and
`AddWatchedNamespace` and `RemoveWatchedNamespace`: `GetCluster` first,
nil is the 404; then the store helper. `DeleteCluster`: `RemoveCluster`
in the `Tx`, then `removeClusterDir(root, id)` (`files.go`,
`os.RemoveAll` of `filepath.Join(root, strconv.FormatInt(id, 10))`, after
the commit: a directory left by a crash between the two is the orphan
files sweep's), then notify, then the empty response. Decision 2 applies.

Tests, `internal/api/clusters_writes_test.go`:

- `TestAddClusterRefusesASecondRowOfTheSameName`: add `dev` with context
  `orbstack`; the response is the whole `Cluster` with id 3, `FirstSeenAt
  testNow`, empty `ApiServerUrl`, no namespaces, not ready; add `dev`
  again: 400 with `{"violations":[{"field":"name","description":"cluster
  \"dev\" exists (id 3)"}]}`; add with an empty name and context: 400 with
  two violations. Trace: Section 8 `POST /v1/clusters`; decision 3.
- `TestClusterWritesAreIdempotentAndAnswerTheRow`: table: rename `prod`
  to `production` twice (`seededClustersWire()[0]` with the new name both
  times); add namespace `payments` to `staging` twice (`Namespaces`
  sorted `idios-smoke, payments, staging-web` both times); remove
  `staging-web` twice (`idios-smoke` left, then unchanged); every row also
  asserts `ListClusters` afterwards equals the response. Trace: Section 8
  rows; Section 5.2 uniqueness.
- `TestClusterWritesSayWhenTheIdIsUnknown`: rename, add namespace, remove
  namespace against id 999: 404 `{"message":"cluster 999 not found"}`.
- `TestDeleteClusterRemovesRowsAndDirectory`: write a file under
  `artifactsRoot/1/idios-smoke/pod-crash/x.log` and one under
  `artifactsRoot/2/...`; delete cluster 1 twice; both answer the empty
  response; `ListClusters` is `seededClustersWire()[1:]`; `ListIncidents`
  holds only cluster 2's rows; directory `1` is gone, `2` is intact.
  Trace: Section 5.1 "one `DELETE FROM clusters` plus deleting its
  artifact directory".
- `TestClusterWritesEmitOnTheStream`: open the cluster stream, rename
  `prod`; the next row equals the response. Trace: Section 5, "the
  watched namespace set changes".

Checkpoint, commit `api: add the cluster and namespace endpoints`.

Consumes: Task 5 helpers, `store.InsertCluster`, `store.FindClusterByName`,
`store.AddWatchedNamespace`, `cluster`, `clusterState`, `seededClustersWire`.
Produces: `clusterRowAfter`, `removeClusterDir`.

## Task 10: the CLI commands become clients

Spec: `presentation.md` Section 2, last paragraph: `idios cluster add` and
`idios ns add` become clients the way `idios status` is, keeping the
database path for when the daemon is not running.

`cmd/idios/run.go`: `.WithWriter(st.Writer)` on the server. `config_cmd.go`:
both commands first probe the daemon exactly as `reportFromDaemon` does
(`GetStatus` under `statusTimeout`); when it answers, `AddCluster` or
`ListClusters` plus `AddWatchedNamespace` (the CLI addresses the cluster
by name and the endpoint by id, so the name is resolved through the
list; a name that is not listed is the existing `no cluster named ...`
error); a `*sebufhttp.ValidationError` from the daemon is printed as its
violations' descriptions joined by `; ` (so a duplicate cluster prints
`cluster "orbstack" exists (id 1)` on both paths); any other error is
returned as it is. When nothing answers, the existing code runs unchanged
except decision 4: `ns add` prints `watching <ns> in <cluster>` whether
or not the row was new, and the `already watched` branch goes. Nothing
is printed about which path ran: the output is the same, which is the
point. The probe is shared with `status.go`; lift it into
`daemonClient(ctx, cfg) (idiosv1.IdiosServiceClient, bool)` there and
call it from both.

Test, `cmd/idios/config_cmd_test.go`: the existing table gets the
`already watched` row's expected output changed to `watching idios-smoke
in orbstack\n`. New `TestClusterAndNamespaceCommandsUseTheDaemonWhenItAnswers`:
open a store in a temp data dir, serve `api.New(...).WithWriter(...)` on
an `httptest` server, write `idios.toml` in the data dir with
`api_listen` set to the server's `host:port` (check `config.Load` for the
key name), run the same command table through `run`, assert the same
outputs and errors, then assert the rows through the store as the
existing test does. A `cmd/idios` test may import `internal/api` (it
already imports it for `FileSize`). Trace: Section 2's sentence; the
existing test's rows.

Checkpoint, commit `cmd/idios: add clusters and namespaces through the
api when the daemon is up`.

Consumes: Task 9 endpoints, `reportFromDaemon`, `statusTimeout`,
`runCluster`, `runNamespace`.
Produces: `daemonClient`.

## Task 11: application, incident actions

Spec: `presentation.md` Section 9.3 (incident detail: action buttons),
Section 8 (the six actions), mockup 2a (toolbar: Note..., Dismiss, Mark
resolved, Acknowledge; the human-actions callout goes), 2b (a closed
incident's toolbar: Note...). Phase 9 handoff: the stores own the calls;
errors map through `apiError`; the stream carries the updated row, so
nothing refreshes by hand.

`IncidentDetailStore`: `acknowledge(id:connection:)`,
`unacknowledge`, `resolve`, `dismiss`, `undismiss`,
`setNote(_:id:connection:)`; each calls the client operation, replaces
`detail.incident` with `Incident(wire:)` of the returned row, and reports
errors through `report`. Check how the generator names the body enum on
`Operations.SetIncidentNote.Input.Body` before writing the method (the
first JSON body in the client). `private(set) var actionError: APIError?`
for a failed write, shown under the toolbar as a wrapped line, distinct
from the detail's own error. `IncidentDetailScreen.actions`: `Note...`
opens `NoteSheet` (a `.sheet` with a `TextEditor` prefilled with the
note, Cancel and Save, Save disabled while unchanged; an empty text
clears); `Dismiss` becomes `Undismiss` when `dismissedAt` is set; `Mark
resolved` is enabled only while `closedAt` is nil, with `.help` saying
why otherwise (`closed as <reason>`); `Acknowledge` becomes
`Unacknowledge` when `acknowledgedAt` is set. `IncidentHeader`: remove
`callout`. `Views/IncidentDetail/NoteSheet.swift` is new. The note is
shown on the rail already? Check `IncidentRail`; if the note is not drawn
anywhere, draw it as a wrapped `FactRow("note")` on the rail, because a
person who wrote one must see it.

No Swift unit test: nothing new is decoded; `IncidentRow` already has
its fixture test. `make app-test && make app` green.

Screenshot review: `hack/macos/screenshot.sh incident/<open id> /tmp/...`
against the daemon: the four buttons enabled, no callout; then after
acknowledging through `curl -X POST 127.0.0.1:7770/v1/incidents/<id>/acknowledge`,
a second screenshot shows `Unacknowledge` and the state tag
`ACKNOWLEDGED`.

Checkpoint (`make app-test && make app && make ascii`), commit `macos:
enable the incident actions`.

Consumes: `IncidentDetailStore`, `IncidentDetailScreen.actions`,
`IncidentHeader.callout`, `apiError`, `Incident.init(wire:)`.
Produces: the six store methods, `NoteSheet`, `actionError`.

## Task 12: application, delete an incident

Spec: Section 8 (`DELETE /v1/incidents/{id}`: "The application
confirms"); mockup 2b (`Delete incident` in the toolbar). Decision 6 and 8.

`IncidentDetailStore.delete(id:connection:) async -> Bool`. The toolbar
gains `Delete incident` as the last item with `.tint(.red)` where the
style allows, opening an `.alert` (`Delete incident <id>?`, message: "The
row and its captured files are removed now. History and events stay
until their own sweep. This cannot be undone.", buttons Delete
(destructive) and Cancel). On success the screen calls a new closure
`deleted: (String) -> Void`, threaded from `IncidentsScreen.destination`
like `openPod`; `IncidentsScreen` pops the path and calls
`incidents.remove(id:connection:)`, a new `IncidentsStore` method that
drops the row and reloads the counts. The detail store's `watch` task is
cancelled by the pop, so the 404 its reload would hit never shows.

Screenshot review: `incident/<id>` shows the fifth button; after a
delete through `curl -X DELETE` the incidents list screenshot no longer
shows the row.

Checkpoint, commit `macos: delete an incident after confirmation`.

Consumes: `IncidentDetailScreen`, `IncidentsScreen.destination`,
`IncidentsStore.apply`, `loadCounts`.
Produces: `IncidentDetailStore.delete`, `IncidentsStore.remove`.

## Task 13: application, add a cluster

Spec: Section 8 (`POST /v1/clusters` preceded by `GET /v1/kube/contexts`;
`POST .../namespaces` preceded by `GET .../namespaces` when RBAC allows,
with a free-text fallback); Section 9.3 (sidebar footer `Add cluster...`);
Section 10 (the application never reads the kubeconfig; contexts come
from the daemon). Mockup 1a footer.

`ClustersStore`: `contexts(connection:) async -> [KubeContext]`,
`namespaces(of context:connection:) async -> KubeNamespaces?`,
`add(context:name:connection:) async -> Cluster?`,
`addNamespace(_:to:connection:) async -> Bool`; write errors land in
`private(set) var actionError: APIError?`. `Views/Clusters/AddClusterSheet.swift`:
a `.sheet` with a `Picker` of contexts (name, with cluster and server as
the secondary line; the list loads on appear; a 501 or an unreachable
daemon shows its message in place of the list), a `Name` field prefilled
with the context name, a namespaces section that loads when a context is
chosen: a checklist of the names when the cluster answered, a free-text
field (`one namespace per line`) when `forbidden`, and the same free-text
field under the checklist so a namespace the Role cannot list can still
be typed; `Add` posts the cluster, then each namespace, then dismisses;
a failure keeps the sheet open with the message. The sidebar footer
becomes a `Button` with the same look. `Route.addCluster` opens the
sheet on launch for the screenshot; `IncidentsScreen` holds
`showAddCluster`.

Swift test, `Tests/IdiosModelTests`: none new; `KubeContext` and
`KubeNamespaces` are already decoded from fixtures.

Screenshot review, against the real daemon only (`idios mock` answers
501 for discovery): `addcluster` route shows the sheet's screen behind
it... it does not: a sheet is its own window (decision 8). For this task
`Screenshot` reports the key window's number when a sheet is up, which
is the sheet: change `mainWindow()` to prefer `NSApp.keyWindow` when its
`sheetParent` is non-nil. Then `hack/macos/screenshot.sh addcluster
/tmp/...` photographs the sheet with the OrbStack context listed and its
namespaces. Do not press Add against the smoke database in review; the
add is exercised through the Go tests and by `idios cluster add`.

Checkpoint, commit `macos: add a cluster from the sidebar`.

Consumes: `ClustersStore`, `KubeContext`, `KubeNamespaces`,
`IncidentsSidebar.footer`, `Route`, `Screenshot.mainWindow`.
Produces: `AddClusterSheet`, `Route.addCluster`, the four store methods.

## Task 14: application, clusters and namespaces

Spec: Section 8 (`PATCH /v1/clusters/{id}`, `DELETE /v1/clusters/{id}`
"The application confirms", `DELETE .../namespaces/{name}`); mockup 7a
(`Clusters and namespaces...` menu item). Decision 6 and 8.

`ClustersStore`: `rename(_:to:connection:)`, `remove(_:connection:)`
(drops the row locally on success and reconciles the scope through
`preferences.scope.reconciled`), `removeNamespace(_:from:connection:)`.
`Views/Clusters/ClustersSheet.swift`: one section per cluster: the name
as an editable `TextField` committing on submit (rename), context and
server on a secondary line, the namespaces as rows each with a remove
button, an `Add namespace` field at the bottom of the section that
reuses the discovery of Task 13 (checklist when the cluster answers,
text otherwise), and a `Remove cluster...` button opening an `.alert`
(`Remove cluster <name>?`, message: "Its watcher stops within ten
seconds. Every pod, incident, event and captured file of this cluster
is removed now. This cannot be undone."). `Add cluster...` at the bottom
opens the Task 13 sheet. The menu bar item `Clusters and namespaces...`
calls `open(.clusters)`; `Route.clusters` shows the sheet in the main
window; a context menu on a sidebar cluster row offers `Clusters and
namespaces...` too.

Screenshot review: `clusters` route, with the key-window rule of Task
13, shows the sheet with the smoke cluster, its namespace and the
buttons. Rename is exercised by `curl -X PATCH` and the sidebar
screenshot shows the new name through the stream.

Checkpoint, commit `macos: manage clusters and namespaces`.

Consumes: Task 13 sheet and store methods, `MenuBarView.actions`,
`Navigator`, `ClusterScope.reconciled`.
Produces: `ClustersSheet`, `Route.clusters`, the three store methods.

## Task 15: application, the cluster checkbox with one cluster

Bug (roadmap): the checkbox is a no-op with one cluster because an empty
scope means all; it should be disabled then.

`IncidentsSidebar.clusterRow`: `.disabled(clusters.count < 2)` on the
`Toggle`, with `.help("the only cluster is always in scope")`.

Screenshot review: `incidents` against the smoke daemon (one cluster):
the checkbox is drawn disabled and checked.

Checkpoint, commit `macos: disable the cluster checkbox with one cluster`.

## Task 16: docs, roadmap, milestone close

- `presentation.md`: Section 1 boundary 2 stops saying the application
  draws the controls disabled (the actions exist); Section 2 says the two
  CLI commands are clients with the database fallback; Section 4.5 got
  its sentence in Task 3; Section 8 loses "(later phase)" and "built
  after the reads ship", gains decisions 1 to 4 and 6 as prose under the
  table (idempotent semantics, the empty delete responses, the duplicate
  name violation, the silent stream after a delete); Section 9.3 gains
  two rows (Add cluster sheet: `/kube/contexts`,
  `/kube/contexts/{context}/namespaces`, `POST /clusters`, `POST
  /clusters/{id}/namespaces`; Clusters and namespaces sheet: `/clusters`,
  `PATCH`, `DELETE`, the namespace pair) and the incident detail row
  names the actions; Section 11 item 2 gains "each write once and twice".
- `CLAUDE.md`, Conventions: the Swift client facts from the Phase 9 and
  10 handoffs that every later Swift change needs (no `.notFound` output
  case, a 404 is `.default(404, body)`; operation names are capitalised;
  `connection.streamClient` for streams and `connection.client` for
  calls; a cancelled call is `APIError.cancelled` and every store ignores
  it; the body enum naming the implementer found in Task 11); all times
  in the application are UTC; `idios mock` answers 501 for kube
  discovery so the sheets are screenshotted against the daemon;
  `Screenshot` photographs the key window when a sheet is up.
- `roadmap.md`: Phase 10 status line `complete <date>`; the top status
  line `complete <date>`; the two closed bullets leave the "Facts" list
  (the SSE and smoke gaps are fixed, the workload fact is in the design
  doc, the checkbox is fixed); `docs/plans/README.md` m2 row `complete
  <date>`. No edit under `m1-recorder/`.
- This plan gains its "Hands to the next phase" section.

Checkpoint (`make ascii`), commit `docs: close phase 10 and milestone 2`.

## Self-review

Spec coverage: Section 8, twelve rows, Tasks 6 to 9, one handler each,
each tested once and twice. Section 5's "a human action changed it":
Tasks 7 and 9 emit and test the emission. Section 2's CLI sentence: Task
10. Section 9.3 controls: Tasks 11 to 14; the two confirmations: Tasks 12
and 14. The roadmap's three daemon gaps and the checkbox: Tasks 1, 2, 3,
15. Section 10: contexts and namespaces still come from the daemon (Task
13). Section 11 item 2: handler tests through the generated client with
exact rows and files.

`.ai` rules: ASCII everywhere (`make ascii` at every checkpoint); tests
trace to Section 6.4, Section 8, Section 5.1, Section 7 or a roadmap bug,
variants as table rows, whole-row and whole-response assertions (Tasks 4,
5, 7, 8, 9, 10); comments carry reasons only (the COALESCE reason, the
notify-after-commit reason, the mux reason); no document referenced from
code (the reasons are written inline); scope: no migration, no new
package, no wrapper; the store gets exactly the helpers the roadmap
names plus `GetCluster`, which Task 9 needs for its 404; commits one per
task, `area: imperative subject`.

Type consistency: `store.AcknowledgeIncident(ctx, tx, id, at) (bool,
error)` is what Task 7's `incidentAction` closure calls;
`store.ArtifactFile` from `sweep_sql.go` is what `removeArtifactFiles`
takes; `query.ListClusters` and `cluster(r, live)` from Phase 8 answer
the cluster writes; `idiosv1.IncidentRow` and `idiosv1.Cluster` are the
response types, so the Swift model needs no new type;
`Route.addCluster` and `Route.clusters` are parsed as `addcluster` and
`clusters` in `Route.init(path:)`.

Risks named: sebuf's handling of `DELETE` with a path parameter and no
body, and of `PATCH` with a body, is exercised for the first time in
Task 6; if the generated Swift client names bodies in an unexpected way,
Task 11 records the name in `CLAUDE.md`. `RegisterOnShutdown` runs its
functions in their own goroutines; the `sync.Once` covers a second
Shutdown.

## Hands to the next phase

Milestone 2 is complete with this phase; the next milestone starts its own
roadmap. What it plugs into:

- Write handlers live in `internal/api/actions.go` (incidents) and
  `internal/api/clusters_writes.go` (clusters and namespaces); the shared
  shape is `incidentAction` and `clusterRowAfter`: run the store helper in
  `Writer.Tx`, set a flag for the 404 or the violation, notify after the
  commit, then reload the row through `query`. File and directory removal
  is `internal/api/files.go`. `Server.WithWriter` is the seam; without it
  every write answers 501 `writer not configured`.
- Store helpers: `store/action_sql.go` (six incident writes, COALESCE
  shape) and `store/config_sql.go` (`GetCluster`, `RenameCluster`,
  `RemoveCluster`, `RemoveWatchedNamespace` beside the Phase 7 inserts).
- The CLI probe is `daemonClient` in `cmd/idios/status.go`; `cluster add`
  and `ns add` use it and fall back to the database. The CLI tests point
  `api_listen` at a closed port so a live daemon on 7770 cannot be
  written to by accident; any new CLI test that opens a store must do the
  same (`writeClosedAPIListen`).
- Application: `ClustersStore` owns every cluster and namespace write and
  the discovery calls; `IncidentDetailStore` owns the incident writes;
  `IncidentsStore.remove` drops a deleted row; `AddClusterSheet`,
  `ClustersSheet` and `NamespacePicker` under `Views/Clusters/`;
  `NoteSheet` under `Views/IncidentDetail/`. Routes `addcluster` and
  `clusters` open the sheets; `Screenshot` photographs the key window
  when a sheet is up, so those routes capture the sheet.
- Deletes leave the streams silent; a second application window or a
  second client learns of a deleted incident or cluster at its next
  reload. A tombstone event is the additive change if that ever matters.
- Not built, by decision: pruning of emptied pod directories after
  `DELETE /v1/incidents/{id}` (the sweeper's `pruneDirs` covers it on
  its next pass); the Section 7.4 treatments on the context and API URL
  lines of the two sheets (plain text today); a timezone preference
  (every time is UTC).
- Facts corrected during the phase: `idios mock` answers kube discovery
  with fixtures, not 501; a 501 comes from a daemon built without
  `WithKube`. `RegisterOnShutdown` is how the streams end; the shutdown
  grace is only for request-and-response handlers now.
