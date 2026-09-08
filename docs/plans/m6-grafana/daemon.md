# m6 step A - daemon: config, builder, links

Goal: after this step the daemon stores a Grafana configuration per
cluster, accepts it over one new write RPC, and serves a ready Explore
URL on the incident detail and on every pod container of a configured
cluster; `idios mock` serves a configured cluster so the application
steps can build against fixtures.

Architecture: three new columns on `clusters`; one setter in
`internal/store`; one pure URL builder package `internal/grafana`; window
rules and link attachment in `internal/query`; one write handler in
`internal/api`. No new process, no timer, no clock injection: open-ended
windows are the literal string `now`, so every served URL is
deterministic for the stored rows.

Tech stack: Go 1.26, modernc.org/sqlite v1.57.0, sebuf HTTP annotations,
`make generate` (buf) into `internal/apigen` and `api/openapi`.

Spec: `docs/design/data-storage.md` (clusters columns, added by task 1)
and `docs/design/presentation.md` (link fields, window rules, builder
contract, added by task 1). Visual reference: `docs/mockups/idios-ui.html`
page 9.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`; every timestamp through `clock.Format`/`clock.Parse`;
mutations through `store.Writer.Tx` with SQL in `internal/store`;
read-model SQL in `internal/query`; roadmap decisions 1-5 and 7
(`docs/plans/m6-grafana/roadmap.md`). A schema change: `rm -rf .storage
.storage/smoke` before the next run.

## File structure

    docs/design/data-storage.md              clusters columns (task 1)
    docs/design/presentation.md              Grafana links section (task 1)
    internal/store/migrations/0001_init.sql  three clusters columns (task 1)
    internal/store/cluster_sql.go            clusterColumns, scanCluster grow (task 1)
    internal/store/config_sql.go             SetClusterGrafana (task 1)
    api/proto/idios/v1/clusters.proto        Cluster 14-16, SetClusterGrafanaRequest (task 2)
    api/proto/idios/v1/service.proto         rpc SetClusterGrafana (task 2)
    api/proto/idios/v1/incidents.proto       IncidentDetail.grafana_url = 10 (task 2)
    api/proto/idios/v1/common.proto          Container.grafana_url = 29 (task 2)
    internal/grafana/grafana.go              Config, Values, Window, ExploreURL (task 3)
    internal/grafana/grafana_test.go         exact-URL table tests (task 3)
    internal/query/grafana.go                config fetch, windows, attach (task 4)
    internal/query/grafana_test.go           window and absence tables (task 4)
    internal/query/pods.go                   GetIncident/GetPod call attach (task 4)
    internal/api/clusters_writes.go          SetClusterGrafana handler (task 5)
    internal/api/wire_test.go                fixture cases regenerated (task 5)

## Task 1 - spec, schema, store setter

Write the spec statements first, in the same change: in
`docs/design/data-storage.md`, the three `clusters` columns
(`grafana_url`, `loki_datasource_uid`, `log_selector`, `TEXT NOT NULL
DEFAULT ''`; configured means `grafana_url` non-empty; the selector is a
LogQL template whose placeholders are `$namespace`, `$pod`, `$container`,
`$workload`, `$node`, `$cluster`). In `docs/design/presentation.md`, a
"Grafana links" section: the two computed fields and which detail carries
them; the window rules of roadmap decision 4 (5-minute pad, incident
`opened_at` to `closed_at` or open end, container per-run: running from
`running_since` open-ended, terminated from `running_since` - or the
pod's `first_seen_at` when never running - to `updated_at`, waiting from
the pod's `first_seen_at` open-ended, and a pod's `deleted_at` caps any
open end); the empty-placeholder rule (a matcher whose placeholder
substitutes empty is dropped whole); no pod row, no link.

Then the schema and the store:

- `0001_init.sql`: the three columns after `last_error_at`, edited in
  place per convention.
- `internal/store/cluster_sql.go`: `clusterColumns` and `scanCluster`
  grow the three fields; `store.Cluster` gains `GrafanaURL`,
  `LokiDatasourceUID`, `LogSelector string`.
- `internal/store/config_sql.go`:

      func SetClusterGrafana(ctx context.Context, tx *sql.Tx, id int64,
          grafanaURL, datasourceUID, selector string) (bool, error)

  following the `RenameCluster` shape (`execCount`, rows-affected
  report).

Test first (`internal/store`, table-driven): set then read back via
`GetCluster` asserting the whole `*Cluster`; a second row proving the
clear (all three back to empty); an unknown id reporting false. Trace:
data-storage.md clusters columns statement (task 1 wording).

Steps: failing test, run, implement, run, checkpoint
(`go build ./... && go test ./... && make ascii`), commit.

Consumes: `store.Writer.Tx`, `execCount`, `GetCluster`, `clock` (none).
Produces: `store.Cluster.GrafanaURL/LokiDatasourceUID/LogSelector`,
`store.SetClusterGrafana`.

## Task 2 - the contract

Proto only, then `make generate`; no behaviour yet.

- `clusters.proto`: `Cluster` fields
  `string grafana_url = 14`, `string loki_datasource_uid = 15`,
  `string log_selector = 16` (proto3 drops empty strings from JSON, so an
  unconfigured cluster serves nothing new);
  `SetClusterGrafanaRequest { int64 id = 1; string grafana_url = 2;
  string loki_datasource_uid = 3; string log_selector = 4; }` with field
  examples using `https://logs.example.grafana.net` and
  `grafanacloud-logs` only.
- `service.proto`, copying the `RenameCluster` block shape:

      rpc SetClusterGrafana(SetClusterGrafanaRequest) returns (Cluster) {
        option (sebuf.http.config) = {
          path: "/clusters/{id}/grafana"
          method: HTTP_METHOD_PATCH
        };
      }

- `incidents.proto`: `IncidentDetail` gains
  `optional string grafana_url = 10`.
- `common.proto`: `Container` gains `optional string grafana_url = 29`.

No new test: the wire contract is asserted by the `internal/api` wire
test, which task 5 extends. Checkpoint, commit, then
`make generate-check` on the committed tree.

Consumes: field numbers 13/9/28 as the current tails of `Cluster`,
`IncidentDetail`, `Container`.
Produces: `idiosv1.SetClusterGrafanaRequest`, `idiosv1.Cluster` grafana
fields, `idiosv1.IncidentDetail.GrafanaUrl`,
`idiosv1.Container.GrafanaUrl`.

## Task 3 - internal/grafana

A pure package: no store, no clock, no imports beyond stdlib.

    const DefaultSelector = "{namespace=\"$namespace\", pod=\"$pod\", container=\"$container\"}"
    const Pad = 5 * time.Minute

    type Config struct{ BaseURL, DatasourceUID, Selector string }
    type Values struct{ Namespace, Pod, Container, Workload, Node, Cluster string }
    type Window struct {
        From time.Time
        To   time.Time  // zero means the literal "now"
    }

    func ExploreURL(cfg Config, v Values, w Window) string

Behaviour, each line a test row with an exact whole-URL assertion:

- Substitution: each placeholder replaced by its value with `"` escaped
  as `\"`; a matcher (one comma-separated element of the selector body)
  containing a placeholder whose value is empty is dropped, and the
  braces and comma spacing are rebuilt so the result is valid LogQL.
- The URL is `<BaseURL>/explore?schemaVersion=1&panes=<encoded>`: one
  pane keyed `"a"`, one query `refId "A"` with the substituted `expr`,
  `queryType "range"`, datasource `{type: "loki", uid: <DatasourceUID>}`,
  `direction "backward"`, range `from`/`to` as decimal epoch
  milliseconds (`clock`-parsed times come in as `time.Time`;
  `UnixMilli`), `to` the literal `"now"` when `w.To` is zero,
  `panelsState.logs.sortOrder "Descending"`, `compact false`; the panes
  JSON percent-encoded once.
- A trailing `/` on `BaseURL` does not double the slash.

Test rows: all placeholders set (closed window); empty Container
dropping its matcher; empty Workload dropping a `$workload` matcher; a
pod name containing `"`; open window rendering `"now"`; trailing-slash
base. Trace, one line each: presentation.md "Grafana links" builder
contract and empty-placeholder rule (task 1 wording).

Steps: failing tests, run, implement, run, checkpoint, commit.

Consumes: nothing from earlier tasks.
Produces: `grafana.ExploreURL`, `grafana.Config`, `grafana.Values`,
`grafana.Window`, `grafana.Pad`, `grafana.DefaultSelector`.

## Task 4 - windows and attachment in internal/query

`internal/query/grafana.go` (read-model SQL stays in `internal/query`,
one file for this concern):

- A one-row fetch of `grafana_url, loki_datasource_uid, log_selector,
  name FROM clusters WHERE id = ?`, used by both details; empty
  `grafana_url` short-circuits to no links.
- Incident window and values from `IncidentDetail`: requires `d.Pod`
  non-nil; `From = opened_at - Pad`; `To = closed_at + Pad`, or zero
  while open; container from `Incident.ContainerName`; workload and node
  from the pod row; cluster from the fetched `name`. Result goes to a
  new `IncidentDetail.GrafanaURL *string`.
- Container windows and values from `PodDetail`, one per
  `store.Container`, per the rules in presentation.md (task 1): running,
  terminated, waiting, and `deleted_at` capping any open end; `From`
  always padded, a capped `To` padded too. Result goes to a new
  `PodDetail.ContainerGrafanaURLs map[string]string` keyed by container
  name; the API layer copies it onto each `idiosv1.Container`.
- `GetIncident` and `GetPod` call the attach before returning.

Tests (`internal/query`, table-driven over seeded rows, whole-value
assertions on the computed URL strings using `grafana.ExploreURL` for
the expected side only where the input differs, exact strings where the
row is the point): open incident ends in `"now"`; closed incident is
padded epoch millis of its stored timestamps; job incident (no pod) has
no URL; incident without `container_name` drops the container matcher;
running container from `running_since`; terminated container ends at
`updated_at`; waiting container starts at pod `first_seen_at`; deleted
pod caps the open end at `deleted_at`; unconfigured cluster produces
nil/empty on both details. Trace, one line each: presentation.md window
rules and field placement (task 1 wording).

Steps: failing tests, run, implement, run, checkpoint, commit.

Consumes: `grafana.*` (task 3), `store.Cluster` fields (task 1),
`query.IncidentDetail`, `query.PodDetail`, `store.Container`,
`clock.Parse`.
Produces: `query.IncidentDetail.GrafanaURL`,
`query.PodDetail.ContainerGrafanaURLs`.

## Task 5 - the write handler, the wire, the mock

`internal/api/clusters_writes.go`, following `RenameCluster` exactly:

- `func (s *Server) SetClusterGrafana(ctx, req) (*idiosv1.Cluster, error)`.
- Validation before the transaction, returned as
  `*sebufhttp.ValidationError` (a typed error from inside `Writer.Tx`
  would be joined with the rollback result and lose its type): empty
  `grafana_url` is the clear and skips the other checks but blanks all
  three; otherwise the URL must parse absolute with scheme `http` or
  `https` and a host, `loki_datasource_uid` must be non-empty, and
  `log_selector` must be non-empty and contain `$pod`.
- Inside `Writer.Tx`: `store.SetClusterGrafana`; a false rows-affected
  sets a flag that becomes `notFoundError{what: "cluster", id: ...}`
  after the transaction returns.
- After the commit: `s.events.Notify(notify.Cluster, req.Id)`, so the
  sheet sees its own edit through `StreamClusters`.

Then the wire: extend the wire-contract cases in
`internal/api/wire_test.go` so the fixture cluster carries a grafana
config (`https://logs.example.grafana.net`, `grafanacloud-logs`,
`grafana.DefaultSelector`) and the incident and pod details carry
computed `grafana_url` values, and regenerate `api/testdata/*.json` with
the test's `-update` flag; `idios mock` serves the new fields with no
further change. Handler tests, table-driven, following the existing
write-handler tests: each validation violation by field; the clear;
unknown id is a 404; a set answers the updated `Cluster` message whole.
Trace: presentation.md "Grafana links" write contract (task 1 wording)
per row.

Steps: failing tests, run, implement, run, regenerate fixtures,
checkpoint, commit, `make generate-check` if anything under `api/proto`
moved in the same change.

Consumes: `store.SetClusterGrafana` (task 1), generated
`SetClusterGrafanaRequest` (task 2), `query` attach (task 4),
`notify.Cluster`, `notFoundError`, `sebufhttp.ValidationError`.
Produces: PATCH `/clusters/{id}/grafana`; fixtures a mock daemon serves.

## Hands to the next phase

- `Cluster` protojson: `grafanaUrl`, `lokiDatasourceUid`, `logSelector`;
  absent means empty (proto3), so `Cluster(wire:)` takes zeros.
- Swift generated operation `SetClusterGrafana`, body
  `.json(Components.Schemas.SetClusterGrafanaRequest(...))`, the path id
  repeated in the body; outputs `.ok` / `.badRequest` / `.default`.
- The prefill template for the label builder equals
  `grafana.DefaultSelector`; the app mirrors it as its own constant, and
  its builder emits exactly the placeholder vocabulary of roadmap
  decision 2 (the daemon never validates beyond `$pod` presence).
- Step C reads `IncidentDetail.grafanaUrl` and `Container.grafanaUrl`
  as served; the app never builds or edits an Explore URL.

## Self-review

- Spec coverage: every data-storage.md and presentation.md statement
  task 1 adds is implemented by exactly one task (columns: 1; write
  contract: 5; builder contract and empty-placeholder rule: 3; window
  rules and placement: 4) and every test traces to one of them.
- `.ai/tests.md`: all tests table-driven, whole-value assertions, edge
  cases (empty placeholder, quote escaping, unknown id, unconfigured
  cluster) first-class rows.
- `.ai/comments.md` and doc comments: every exported identifier in
  `internal/grafana`, the new store setter and the new struct fields
  carry one line; no comment restates code.
- `.ai/scope.md`: no timeline per-run links, no test-connection call, no
  configurable pad, no MCP change (the proxy carries the fields by
  construction).
- Type consistency: `store.Cluster` string fields flow to `idiosv1.
  Cluster` strings; `grafana.Window.To` zero-value convention is used by
  exactly one producer (task 4) and one consumer (task 3);
  `ContainerGrafanaURLs` keys equal `store.Container.Name`, the same key
  the API layer uses to fill `idiosv1.Container.GrafanaUrl`.
- Anti-leak: no real organisation, host, workload or namespace anywhere;
  examples are `logs.example.grafana.net` and the smoke/fixture names.
