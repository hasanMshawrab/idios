# Kubernetes Monitoring -- Presentation

Fourth of four. `data-storage.md` says what is stored, `client-go-methods.md` how it is filled, `process-architecture.md` how the process runs. This document says how what is stored is shown: the API the daemon exposes, the read models behind it, the macOS application that consumes it, and the rules both sides follow so that nothing the recorder kept is lost on the way to the screen.

Fragments of proto and Swift appear only where a shape is easier to show than to describe. They are not the implementation.

---

## 1. Purpose and boundaries

idios records what happened to Pods and Jobs in a few namespaces so a developer can answer "why did this break?" after the objects are gone. Presentation exists to make that record readable. It adds no knowledge the recorder does not have.

Three boundaries follow, and every later section respects them:

1. **The application never opens the database, the artifact files, or the kubeconfig.** It talks to the daemon over HTTP on the loopback interface and nothing else. The daemon is the only process that touches `idios.db`; the single-writer design in `process-architecture.md` Section 3 depends on that.
2. **Presentation writes only what a person decides.** The human actions the storage doc lists (acknowledge, mark resolved, dismiss, note, delete) and the cluster and namespace configuration are the twelve endpoints of Section 8; nothing else the API accepts changes the record, and nothing changes the cluster.
3. **Presentation shows what idios has, never what a live cluster browser would show.** No resource usage, no nodes, no Services or Deployments as objects, no severity, no remediation. Where a screen would tempt one of those, the screen says what idios does not know rather than guessing.

What is deliberately not here, and why, is in Section 12.

---

## 2. Shape of the process

The API is a component of `idios run`, started beside the watchers, capture pool, closer, sweeper and status loop by the same `startComponent` in `cmd/idios/run.go`. It is not a second binary:

- Every human action is a mutation and must go through `store.Writer.Tx`. A second process would need a second writer on the same file, which is the `SQLITE_BUSY_SNAPSHOT` hazard the design removed.
- The API reports facts only the running process knows: `status.Counters`, `capture.Pool` counts, `k8s.Watcher.Ready()`, `k8s.Skew.Offset()`. In-process they are a method call; across processes they are a file, which is what `status.json` is today.
- Cluster and namespace changes are already reloaded by the supervisor in `cmd/idios` every ten seconds; the endpoints that add them only write the two rows.

```
idios run
  watchers ----> processor ----> store.Writer
  capture pool -----------------> store.Writer
  closer / sweeper -------------> store.Writer
  api (127.0.0.1:<port>) -------> query (store.Reader)    reads
                          -------> store.Writer            writes (later phase)
                          -------> status.Counters, pool, watchers   runtime facts
```

`internal/api` holds the handlers and the mapping between row types and wire messages. `internal/query` holds the read models: one function per list or detail, taking a `store.Querier`, returning row sets. `query` imports `store` only; `api` imports `query`, `store` and the generated contract package; nothing else imports `api`. The dependency test in `internal/archtest` gains these three rules.

The listener binds `127.0.0.1` only. Binding elsewhere requires a flag whose name says it is insecure (`process-architecture.md` Section 12); it does not exist in this version.

`idios status` is a client of the API when the daemon is running (`GET /v1/status`) and keeps its direct database path for when it is not, because "the daemon is down, what is in the database" is a question `status` must still answer. `status.json` stays for that case. `idios cluster add` and `idios ns add` are clients the same way (`POST /v1/clusters`, `POST /v1/clusters/{id}/namespaces`) with the same database fallback, and print the same lines on both paths; a daemon that is up is never bypassed, because a second writer on the file is the hazard the single-writer design removes.

`idios mcp` is the third surface: a read-only MCP server over stdio that a local AI agent spawns for one session. Its seven tools (`list_incidents`, `get_incident`, `get_pod`, `read_log`, `read_pod_json`, `get_workload`, `get_status`) translate into calls on the same API through the committed generated client (`internal/mcp`); like the application it never opens the database, the artifact files or the kubeconfig, and `internal/archtest` enforces the imports. `read_log` serves tails, byte ranges or grep matches and never a whole file unless asked, so a large log does not cost an agent its context; `read_pod_json` passes through `internal/sanitize` (Section 10). Tools take the cluster by name as `get_status` lists it, and the prompts of `GET /v1/incidents/{id}/prompt` print the same name, so an agent can paste identity straight into a tool call. A daemon that is not running turns every tool into one error naming the address it tried.

---

## 3. Contract

The contract is a set of protobuf files under `api/proto/idios/v1/`, compiled by sebuf into Go HTTP handlers, a Go HTTP client and an OpenAPI 3.1 document. The Swift client is generated from the OpenAPI document by Apple's `swift-openapi-generator` at build time of the application. One source, three consumers; no hand-written request or response type on either side.

```
api/
  proto/idios/v1/*.proto        the contract
  openapi/*.openapi.yaml        generated, committed, read by the Swift build
internal/apigen/idiosv1/        generated Go: messages, handler interface, client
internal/api/                   hand-written handlers implementing the interface
macos/                          the application; its package plugin regenerates the
                                Swift client from api/openapi at build time
```

The proto `service` and `rpc` keywords are sebuf's grammar for an HTTP operation; on our side every operation is a method and a path (`GET /v1/incidents/{id}`), the Go side is a handler, the Swift side is a client operation. Nothing here is RPC in the transport sense.

### 3.1 Wire rules

Decided once, verified by a round trip through the generated Go server, Go client and Swift client, and applied to every message:

| Rule | Reason |
|---|---|
| Identifiers and byte counts are `int64`, serialized as JSON strings (`"id":"412"`). | proto3 JSON. Swift receives `String` and never does arithmetic on ids. A value above 2^53 survives. |
| Counts that fit are `int32`: exit code, signal, restart count, occurrences, event count. | proto3 serializes `int32` as a JSON number, so Swift gets `Int32?` where it wants a number. |
| Closed vocabularies are enums with `(sebuf.http.enum_value)` set to the stored string: `category`, `close_reason`, `capture_gap`, `deletion_source`, `deletion_reason`, `container kind`, `container state`, `artifact kind`, `subject_kind`. | Typed on both sides; the wire carries `"crash"`, not `CATEGORY_CRASH`; matches the database and the CLI output. |
| Raw Kubernetes values are strings: reasons, phases, condition types, event reasons, messages, image references. | Open vocabularies. The storage doc stores them verbatim so classification can change later; the wire does the same. |
| Timestamps are strings in the stored layout (`2026-08-27T14:03:11.482913Z`), never `google.protobuf.Timestamp`. | What the database holds is what the screen shows and what the clipboard gets. No second representation to disagree. |
| Nullable columns are proto3 `optional` fields. Unset means absent from the JSON and `nil` in Swift. `(sebuf.http.nullable)` is not used. | sebuf allows one JSON-marshaling feature per message, and the enum rule already uses it. Absent and null are the same fact here. |
| Field names are `snake_case` in proto and `lowerCamelCase` on the wire, as protojson emits them. | Default; no override. |
| Every list response is a message with one `repeated` field, never a bare array. | Room for a count or a cursor later without a breaking change. |
| Errors are sebuf's `Error` and `ValidationError` messages. A bad path or query value is a 400 with `violations[{field, description}]`; an unknown id is a 404 with `message`. | Typed on the Swift side as `.badRequest` / `.notFound` cases. |
| Filter query parameters with a closed vocabulary (`state`, `category`, `live`) are `string` fields validated by the handler, which answers 400 with one violation per bad value. | sebuf applies `enum_value` to JSON bodies only; an enum bound to a query parameter would expect `CATEGORY_CRASH`, not `crash`. Response fields stay typed enums. |

Proto3 has no required fields, so every generated Swift property is optional. The application converts each wire message into a model type once, at the edge (Section 9.2), and nothing past that layer sees optionals that the schema says are not nullable.

### 3.2 Versioning

All paths start with `/v1`. Within `v1`, changes are additive only: new fields, new endpoints, new enum values. Removing or renaming anything is `/v2`. A new enum value is the one additive change the application does not tolerate: `swift-openapi-generator` emits every enum as a closed `@frozen` type nested in its message, so a value the application was not built with fails the decoding of the whole response. Until the contract emits named enum schemas the generator can override, adding an enum value in the daemon requires an application rebuild.

### 3.3 Streaming

sebuf supports Server-Sent Events (`stream: true` on an operation). Two streams exist (Section 5). Everything else is request and response.

---

## 4. Read endpoints

One endpoint per list or detail a screen needs. Each is backed by one function in `internal/query` whose body is the SQL named in the right column. All lists accept `cluster_ids` (repeated; empty means all clusters) because cluster is the scope of every screen (Section 9.4). Lists are bounded by retention (three days of a few namespaces is thousands of rows at most), so there is no cursor in this version; `limit` defaults to 500 and the response says when it was hit.

### 4.1 Scope and navigation

| Endpoint | Returns | From |
|---|---|---|
| `GET /v1/clusters` | Every cluster row with its watched namespaces and its runtime state: `ready`, `last_event_at`, `skew_seconds`, plus `last_error`, `last_error_at`, `last_connected_at`, `context_name`, `api_server_url`. `ready` is a claim about now: any list or watch failure clears it and records `last_error`, whatever the error (not only a Forbidden one), and it stays false until an identity probe on the reconnect backoff succeeds -- client-go reports a reflector's failure and never its recovery, so a positive signal is the only thing that can clear one. There is no age threshold: with resync 0 a healthy watcher on an idle namespace legitimately delivers nothing for hours, which is not the same fact as a credential that stopped working. | `clusters`, `watched_namespaces`, `status.Counters`, `k8s.Watcher`. The only list that merges database rows with process memory. |
| `GET /v1/kube/contexts` | Context names in the kubeconfig the daemon is configured with, each with its cluster name and server URL. No credentials. | `clientcmd` on `config.Kubeconfig`. Reads a file, not the database. |
| `GET /v1/kube/contexts/{context}/namespaces` | Namespace names, or a `forbidden` marker when the Role cannot list them. | One API call to that cluster. The only read that talks to a cluster; used by the add-cluster flow (Section 8) and marked as such in the OpenAPI description. |

### 4.2 Incidents

| Endpoint | Returns | From |
|---|---|---|
| `GET /v1/incidents` | Rows for the triage list. Filters: `state` (`open`, `acknowledged`, `recovered`, `pod_deleted`, `job_finished`, `manual`, `dismissed`, `attention`), `category`, `namespace`, `workload_kind`, `workload_name`, `pod_uid`, `job_uid` (the Job's own incident and the incidents of its pods, since a pod incident carries the job uid too), `node_name` (the node the pod was on when the incident opened, which with the filters above makes a disruption storm on one machine a single query; it finds placed pods only, since a scheduling incident opens before placement and a job incident has no pod), `pod_name` (an exact match on the pod's name, so a client that holds a name from a truncated list still reaches its rows; it is served through the join to `pods`, whose `(cluster_id, namespace, name)` index already exists). Each row: the `incidents` columns plus `pod_name`, `pod_deleted_at`, `pod_deletion_reason`, `container_count` of the pod, and `exit_code` / `signal` of the container's last termination. | `incidents` joined to `pods` and `containers`. `state` is derived: `closed_at IS NULL AND acknowledged_at IS NULL` is `open`; `closed_at IS NULL AND acknowledged_at IS NOT NULL` is `acknowledged`; closed rows take their `close_reason`; `dismissed_at IS NOT NULL` is `dismissed` whatever else holds. `attention` is not a row's own state, so no row ever reports it: filtered on, it selects a row that is open, or that closed within `attention_window` and was never acknowledged or dismissed, so a recently closed row a person has not yet looked at stays in front of them for that window. |
| `GET /v1/incidents/counts` | The sidebar numbers, faceted: one count per state and one per category, each list narrowed by the other axis's optional `state` or `category` parameter and never by its own, so every displayed count equals the list its click opens while the current axis stays browsable. `attention` is one more state count, by the same predicate as the list filter. | `GROUP BY` on `incidents` with the same derivation, folded per axis. |
| `GET /v1/incidents/{id}` | Everything the detail page shows, and the one place the pod-wide projections are built: the incident; the pod snapshot; the subject container's snapshot (or all containers for a pod-level incident); the owner chain (`workload_*`, `controller_*`, pod, container); the artifacts of the pod with `capture_gap` and `capture_note`, each keeping the `incident_id` it attached to, because a capture runs for the pod and attaches to whichever of its incidents was open at the time; the subject pod's whole event stream, each row keeping its `incident_id` for the same reason; the latest condition per type; `related_incidents`, the other incidents of the same pod and of the same Job, newest activity first and bounded by the default page limit, since a pod's four incidents are one death seen four ways and a Job's retries are one failure seen once per run; `job` for every incident that names one -- the Job's own row for a job-subject incident, and the run above the failing pod for a pod incident, whose condition is what separates a retry that succeeded from a run that failed; and `last_pod_name` for a job-subject incident (the newest pod the recorder saw for it, which may since have been pruned). A job incident's pod parts are that newest pod's, shown whole: the failure a Job reports happened inside a pod, and the job row alone answers nothing, so the pod snapshot, its containers, its files and its events all ride along. The prompts of `/prompt` read these rows from here and compose none of their own. | `incidents`, `pods`, `containers`, `artifacts WHERE pod_uid` (of the incident's pod, or of the newest pod for a job incident), `k8s_events WHERE incident_id OR involved_uid` of the pod, `pod_condition_history` latest per type, `jobs WHERE uid`, `pods WHERE controller_uid ORDER BY created_at DESC LIMIT 1`, `incidents WHERE (pod_uid OR job_uid) AND id <> this`. |
| `GET /v1/incidents/{id}/timeline` | Time-ordered entries of six kinds: container transition (`container_state_history`), condition change (`pod_condition_history`), event (`k8s_events` attached to the incident or to the pod within the incident's span), capture (`artifacts` of the incident's pod, the same set the detail lists, so the detail never names a file its own timeline leaves out), rollout (`rollout_history` rows of the pod's Deployment first seen within the span), incident lifecycle (opened, closed, derived from the incident row; a reopen leaves no timestamp behind and has no entry). Each entry carries the Kubernetes time when there is one and the observed time always, plus `gap_reconstructed` on transitions. A `cut` entry marks the sweep cutoff when `sweep_runs` shows rows were removed from this pod's span. | Six queries merged in `query`, sorted by Kubernetes time then observed time. |
| `GET /v1/incidents/{id}/prompt` | The text an AI agent is handed for this incident, as `text/plain; charset=utf-8` with `Content-Length`, so the application can say how large it is before a person copies or saves it. Required `mode`: `mcp` is the short prompt (identity and one-line summary, the MCP tool vocabulary, the investigation order, the answer rules) for an agent that pulls its own evidence over `idios mcp`; `snapshot` composes the evidence itself (detail and timeline, pod, containers and conditions, the pod's whole event stream, every captured log with truncation marked, the `pod.json` with its environment values stripped, and the incidents sharing the pod or the job -- the files and the related rows as the detail lists them, never a second projection). Both modes end with the same answer rules. Unknown id is 404, any other `mode` is 400. | `internal/prompt` over the read models of `/incidents/{id}` and its timeline, the files under `artifacts_root`, and `internal/sanitize` for the pod object. The body is text, so it is a plain handler on the same mux, outside the proto contract and the OpenAPI document, and the application calls it with `URLSession`. The daemon renders it and the application never assembles one, so the prompt and the tool vocabulary cannot skew across versions. |

### 4.3 Pods

| Endpoint | Returns | From |
|---|---|---|
| `GET /v1/pods` | Pod rows. Filters: `namespace`, `workload_kind`, `workload_name`, `live` (`true` for `deleted_at IS NULL`, `false` for deleted, absent for both), `pod_name` (an exact match on `name`, so a client that holds a name from a truncated list still reaches its row; served by the `(cluster_id, namespace, name)` index the table already has), `job_uid` (an exact match on `controller_uid`, so a run reaches every pod its Job started and not only the ones that opened an incident; served by the `controller_uid` index). Each row: `pods` columns plus counts of open incidents and of containers, and the worst container state for a status badge. | `pods` with two correlated counts. |
| `GET /v1/pods/{uid}` | The pod row, its containers (all kinds, with the `ready` meaning implied by `kind`), latest condition per type, incidents on the pod (open and closed), artifacts grouped by container, and the pod's siblings: the other pods of the same `controller_uid` in the same cluster, live or deleted, each with its phase, readiness, restart count and the worst category among its open incidents (open meaning neither closed nor dismissed; worst in the order the sidebar lists categories: crash, oom, unclean_exit, image_pull, config, probe, scheduling, stuck, node_pressure, rescheduled, job_failed, then other), ordered pods with an open incident first (worst category first), then live pods, then deleted ones, newest created first inside each group, five at most; `sibling_total` counts every pod of the controller idios has seen in that cluster, this one included, so the rail can say "N pods of this ReplicaSet" and how many more than it shows. A pod with no controller has no siblings and a total of zero. | `pods`, `containers`, `pod_condition_history`, `incidents`, `artifacts`, `pods WHERE controller_uid`. |
| `GET /v1/pods/{uid}/events` | Every event whose `involved_uid` is the pod, attached or not, newest first. | `k8s_events`. |
| `GET /v1/pods/{uid}/history` | `container_state_history` and `pod_condition_history` rows for the pod, oldest first. | Two tables. |

### 4.4 Artifacts

| Endpoint | Returns | From |
|---|---|---|
| `GET /v1/artifacts/{id}` | The `artifacts` row. | `artifacts`. |
| `GET /v1/artifacts/{id}/content` | The file bytes as `text/plain; charset=utf-8` for logs and `application/json` for `pod_json`, with `Content-Length`. 404 with an `Error` whose message quotes the row's `capture_gap` and `capture_note` when there is no file; the typed values are on the `Artifact` row the client already holds. | The file under `artifacts_root` at `file_path`. The daemon serves it; the application never opens the directory. This endpoint returns raw bytes, so it is a plain handler on the same mux, outside the proto contract and the OpenAPI document; the application calls it with `URLSession`. |

Logs are at most `log_max_bytes` (256 KiB default) by construction, so there is no range request.

### 4.5 Workloads and jobs

| Endpoint | Returns | From |
|---|---|---|
| `GET /v1/workloads` | One row per `(cluster_id, namespace, workload_kind, workload_name)` seen in the window: incidents by category, open count, occurrences total, image tags at open with counts, live and deleted pod counts. Filter: `namespace`. A pod no controller owns is its own row, keyed on the pod's uid rather than on the empty `workload_name` every such pod shares: the row carries `pod_uid` and `pod_name`, both empty for a real workload, so the application can open the pod directly instead of a row with nothing to read. | `incidents GROUP BY workload_kind, workload_name` (plus the pod's uid for kind `none`) joined to `pods`. This is the storage doc's "which service fails most" query. |
| `GET /v1/workloads/{cluster_id}/{namespace}/{kind}/{name}` | The row above plus: rollouts (`rollout_history` for the Deployment, with `created_at`, `replicas`, `ready_replicas`, `available_replicas` and the incident count per ReplicaSet), restarts per hour over the window (`restart_count` increments in `container_state_history` bucketed by hour, with a flag for hours containing `gap_reconstructed` rows), the pods of the workload with their deletion facts, bounded and filtered by the request's `pods_live` and `pods_limit`, with `pods_truncated` set when the limit cut the list short, and `runs` for a CronJob or Job workload -- the Jobs of a CronJob by `cronjob_name`, the single run of a Job workload by its own name, newest start first and bounded by the same page limit -- so "is this recurring" is answered by the workload call an agent already makes; every other kind has none. `name` is required, so the workload kind `none` (a pod without a controller) has no detail here because the path cannot carry an empty segment; a client shows it from the list row and from `ListPods` filtered by `workload_kind=none`. | `rollout_history`, `container_state_history`, `pods`, `jobs`. |
| `GET /v1/jobs` | Job rows. Filters: `namespace`, `cronjob_uid`, `cronjob_name`, `live` (`true` for a run still in the cluster, `false` for deleted, absent for both), `failed` (`true` for a run whose condition is `Failed`). Each row: `jobs` columns; `succeeded` is read from `condition_type`, never from the counters. The response also carries `total` and `failed_total`, counted over the filter with `live` and `failed` ignored, so a bounded page still says how many runs it stands for -- what ties a run to its workload is `cronjob_name`, a name chain like every other workload key, not a uid. | `jobs ORDER BY started_at DESC`. |

### 4.6 Status

| Endpoint | Returns | From |
|---|---|---|
| `GET /v1/status` | What `idios status` prints: the snapshot fields (`status.Snapshot`: version, pid, per-cluster ready / last event / skew, writer, handlers, capture, closer), `daemon_running` (false when the API answers without a running recorder, as `idios mock` does) and `written_at` and the database aggregates (open incidents by category, closed by reason, artifacts by outcome, row counts, latest `sweep_runs` per table, database, WAL and artifacts sizes, retention and the configured intervals). The closer numbers are two of everything, `closed` / `attached` / `opened` for the last tick alone and `closed_total` / `attached_total` / `opened_total` cumulative since the process started, because "closed 0" after an hour of real work reads as a closer that has done nothing unless the two are told apart. The configured intervals include `scheduling_grace_seconds`, `stuck_after_seconds` and `attention_window_seconds`. | `status.Counters` and the functions in `store/status_sql.go`. |

### 4.7 The menu bar summary

The menu bar extra's headline is the open count from
`GET /v1/incidents/counts`, and the Dock badge follows it. Its subline is
the attention count from the same call, kept because a row that closed an
hour ago and nobody has looked at is still the menu bar's business, and
the subline is where that is said without making the headline false. Its
rows come from `GET /v1/incidents?state=attention` with the list limit,
folded client-side into one row per workload group -- the same fold as the
list's -- a closed group grey, and a row opens that group in the list.
`GET /v1/clusters` gives the cluster dots, ignoring the application's
cluster scope. No separate endpoint.

### 4.8 Grafana links

A cluster with a non-empty `grafana_url` gets a ready-to-open Grafana
Explore URL attached to two detail fields, built server-side by a pure
function in `internal/grafana` and assembled by `internal/query`; the
application never builds or edits one. List rows carry neither field.

- `GET /v1/incidents/{id}` gains `grafana_url`, scoped to the incident's
  container when it carries a `container_name` and to the whole pod's
  selector otherwise. An incident without a pod row (a job incident, or
  one whose pod was swept) has no `grafana_url`.
- `GET /v1/pods/{uid}` gains a `grafana_url` per container, scoped to
  that container's own run rather than the pod's lifetime.

Every window is UTC and padded by a fixed 5 minutes each side, absorbing
clock skew and Loki ingest lag:

- Incident: `opened_at - 5m` to `closed_at + 5m`, or the open end while
  the incident is still open.
- Container, running: `running_since - 5m` (or the pod's `started_at`
  then `created_at` when the kubelet reported no start), open-ended.
- Container, terminated: the newest terminated state's
  `k8s_started_at - 5m` to its `k8s_finished_at + 5m` from the state
  history; `running_since` (then the pod's `started_at`, then
  `created_at`) and `updated_at` stand in when the kubelet times are
  absent.
- Container, waiting: the pod's `created_at - 5m`, open-ended.

Every bound prefers the kubelet's clock over idios's own: Loki stamps
log lines with the kubelet's clock, while `first_seen_at` and
`updated_at` are observation times - for a pod ingested after it already
died they name the ingest moment, hours from the run, and a window built
on them holds no logs at all.
- A pod's `deleted_at` caps any open end at `deleted_at + 5m`: a deleted
  pod cannot still be logging.

The open end, when it is not capped by a deletion, renders as Grafana's
literal `now` rather than a timestamp, so a served URL never goes stale
and the query layer needs no clock.

`log_selector` is a LogQL selector template substituted with the
identity of the row a link is built for. A matcher (one comma-separated
element of the selector body) whose placeholder substitutes to the empty
string is dropped whole, braces and comma spacing rebuilt: `container=""`
in LogQL means "streams without that label", which would silently match
nothing useful rather than broaden the search.

The application shows a served link as an "Open Grafana" button, the
flame mark before the verb and an outward arrow after it, in exactly
three places: the lit incident's header on the pod page (the
incident's own `grafana_url`), each container's pane on the pod page (that
container's own `grafana_url`), and a captured-files row whose log is missing
(`capture_gap` set), where the container's link is the fallback the
capture could not provide. A row or detail without the field shows
nothing, and the app never assembles or edits an Explore URL.

---

## 5. Live updates

Two SSE streams. Each event carries a whole row (the same message the list endpoint returns), never a diff, so the client replaces by id and needs no reconciliation logic.

| Stream | Emits | When |
|---|---|---|
| `GET /v1/incidents/stream` | An incident list row. Filter: `cluster_ids`. | An incident is opened, attached to (occurrences or `last_*` changed), reopened, closed, or a human action changed it. |
| `GET /v1/clusters/stream` | A cluster row. | `ready`, `last_error` or the watched namespace set changes. |

The source of these events is `internal/notify`, a small in-process broadcast that the processor, the closer, the watcher (ready transitions), the supervisor (watcher start and stop) and (later) the write handlers call after a commit; the API subscribes. It carries row ids only; the API reloads the row before sending it. It is a broadcast to whoever is connected, with no replay: on connect and on every reconnect the client reloads the list it is watching and then applies events. A client that missed events therefore misses nothing after one reload. Events are throttled per incident id to one per second so a fast crash loop does not flood the connection.

Polling is the fallback and the rule for everything else: the open list and cluster list subscribe; detail pages reload on focus and on an incident event for their id; status reloads every ten seconds while visible.

---

## 6. Read models

`internal/query` is the package the storage doc reserved. One exported function per endpoint row set, taking `context.Context`, a `store.Querier` (the read pool) and a parameter struct, returning a slice of a result struct defined in `query`. Rules:

- Result structs are flat and named for the screen (`IncidentRow`, `IncidentDetail`, `TimelineEntry`), not for the tables. Joins happen in SQL, not in Go.
- Derived values that the wire needs (`state`, the owner chain, the pod name suffix) are computed in `query` when they need a join and in `api` when they are pure formatting. Nothing is computed in the application that the API could compute once.
- `query` never reads files and never touches process memory; the merge with `status.Counters` and the watcher state happens in `api`.
- Every function has a table test against a seeded temp SQLite asserting the exact result set (Section 11).

The mapping from `query` structs to generated wire messages is hand-written in `api`, one function per pair, and is the only place a wire enum is assigned.

---

## 7. Scope, identity and text: rules every screen follows

### 7.1 Cluster is a scope, not a grouping

Every list is filtered by the set of selected clusters before anything else. The selection is a checklist (all by default; one for "only prod"; several for "prod and staging"), kept across screens and across launches, keyed by cluster row id so a rename does not lose it. Counts, badges and headers recompute for the selection. If a selected cluster disappears the selection drops it; a selection that loses its last cluster is empty, and an empty scope means empty: the incidents list and the workload tree say no cluster is in scope, no call is made, and only checking a cluster again -- or checking every cluster, which is the all scope -- fills them. The menu bar extra ignores the selection.

### 7.2 Namespace is never shown without its cluster

Two clusters with the same namespace name is the normal case. Whenever more than one cluster is selected, rows carry `cluster / namespace`; when exactly one is selected the prefix drops and the scope indicator in the toolbar carries it. Grouping by namespace groups on `(cluster_id, namespace)`.

### 7.3 Identity chain

Navigation is cluster > namespace > workload > pod > container, which is the identity the database stores (`cluster_id`, `namespace`, `workload_kind`/`workload_name`, `pod_uid`, `container_name`). Pods are addressed by `uid` everywhere in the API; names are display only.

A pod name is displayed as its suffix after `workload_name-` when the name starts with that prefix (`checkout-api-7d9f8b6c4-x2kqp` under workload `checkout-api` shows `7d9f8b6c4-x2kqp`: the ReplicaSet hash, which distinguishes rollouts, and the pod hash, which distinguishes siblings). Otherwise the full name is shown. The full name is always in the tooltip and always what copy produces.

### 7.4 No value is ever silently cut

Every text field uses one of four treatments, chosen by the kind of value, and the treatment is visible: the user can tell there is more and can reach it without leaving the screen.

| Treatment | Used for |
|---|---|
| Wrap: full text, row grows. | Event messages, `last_message`, `capture_note`, `last_error`, condition messages, notes. |
| Middle elide, full value on hover, copy control. | Identifiers whose ends carry the information: uids, container ids, image digests, event uids. Never a trailing ellipsis. |
| One line, expand in place on click, full value on hover. | Values usually short and sometimes long: pod names, image references, context names, node names, API URLs. |
| Fixed-width monospace, horizontal scroll, Wrap toggle, whole lines selectable. | Log lines, `pod.json`. |

Everywhere: detail pages never elide; reasons, categories, numbers and timestamps are never elided; table columns are user-resizable and remembered; every elided or wrapped value has a copy action; the tooltip and the clipboard carry the raw stored value. A stored vocabulary word (category, state, close reason, deletion reason and source, capture gap) is drawn with its underscores as spaces and the raw value on hover; a card's provenance -- the table and predicate its values trace to -- is the card title's hover, never visible text.

### 7.5 Images

A list row shows `image_tag`; the full reference is on the detail page, on hover and on copy. A digest-pinned image has no tag and shows the short digest with the middle-elide treatment. Two incidents with the same tag are not assumed to run the same image; `image_id` is what says so, and the detail page shows both.

### 7.6 What idios does not know is said, not hidden

A deleted pod keeps its screens with a banner (`deleted_at`, `deletion_source`, `deletion_reason` labelled inferred, and the date it will be swept). A missing log shows `capture_gap` and quotes `capture_note`, never an empty pane. Reconstructed history is drawn hollow. A sweep that removed rows from a span is drawn as a cut line. A cluster with `last_error` is drawn red in every list that names it. Sibling counts say "pods idios has seen", not "of N replicas", because desired replicas are not stored. The label names the set it counts (this ReplicaSet, this StatefulSet, this Job), because a Deployment's pods span ReplicaSets and the Workloads screen's list of them may be longer.

---

## 8. Human actions and configuration

All go through `store.Writer.Tx` inside `idios run`; all are idempotent; each returns the updated row, and the incident or cluster stream emits it after the commit.

| Endpoint | Effect |
|---|---|
| `POST /v1/incidents/{id}/acknowledge` | `acknowledged_at = now` if null. Kept across reopen. |
| `DELETE /v1/incidents/{id}/acknowledge` | `acknowledged_at = NULL`. |
| `POST /v1/incidents/{id}/resolve` | `closed_at = now`, `close_reason = manual` if open. Final: a recurrence opens a new incident rather than reopening this one. |
| `DELETE /v1/incidents/{id}/resolve` | Unresolve: clears `closed_at` and `close_reason` when they are still `manual`; does nothing to a row closed any other way, or already open. The inverse of resolve. |
| `POST /v1/incidents/{id}/dismiss` | `dismissed_at = now`. Cleared by the system if the incident reopens. |
| `DELETE /v1/incidents/{id}/dismiss` | `dismissed_at = NULL`. |
| `PUT /v1/incidents/{id}/note` | `note = body`. Empty clears. One free-text field, not a thread. |
| `DELETE /v1/incidents/{id}` | Hard delete: artifact files first, then the row; history and events detached. The pod row stays. The application confirms. |
| `POST /v1/clusters` | Insert a `clusters` row from a context name and a friendly name; the supervisor starts the watcher within ten seconds. Preceded in the UI by `GET /v1/kube/contexts`. |
| `PATCH /v1/clusters/{id}` | `name` only; it is documented as user-editable. |
| `DELETE /v1/clusters/{id}` | `DELETE FROM clusters` (cascade) and its artifact directory; the watcher stops. The application confirms. |
| `POST /v1/clusters/{id}/namespaces` | Insert into `watched_namespaces`. Preceded in the UI by `GET /v1/kube/contexts/{context}/namespaces` when RBAC allows, with a free-text fallback. |
| `DELETE /v1/clusters/{id}/namespaces/{name}` | Delete the row; the next reconcile marks that namespace's pods `deletion_source = unwatched`. |
| `PATCH /v1/clusters/{id}/grafana` | Sets `grafana_url`, `loki_datasource_uid` and `log_selector` together. An empty `grafana_url` is the clear: it blanks all three and skips the other checks. Otherwise `grafana_url` must parse as an absolute `http` or `https` URL, `loki_datasource_uid` must be non-empty, and `log_selector` must be non-empty and contain the `$pod` placeholder. |

Rules the table implies:

- Idempotent means the state after the call is the state asked for. A second acknowledge, dismiss, resolve or note with the same text, or a second `POST .../namespaces` of the same name, changes nothing and answers 200 with the same row. The incident writes are `SET col = COALESCE(col, ?) WHERE id = ?`, one statement that keeps the first value and reports through the matched-row count whether the row exists. An unknown id is a 404 with `message` on every endpoint that names one.
- The two hard deletes answer 200 with an empty message whether or not the row existed; after the call the row is gone either way. A deleted row leaves the stream silent: the stream reloads by id and a missing row sends nothing, so the client that issued the delete drops the row itself, and a client that did not learns of it at its next reload.
- `POST /v1/clusters` refuses a second cluster of the same name with a 400 violation on `name` (`cluster "prod" exists (id 1)`): names are what `idios ns add` addresses a cluster by, and the schema does not make them unique. Empty `name` or `context_name` is a violation too, one per field, all reported together.
- The daemon's own `Writer.Tx` joins the closure's error with the rollback result, and the generated handler recognises a typed error (`ValidationError`, the 404) only as a direct value; a handler that wants a 400 or 404 out of a transaction sets a flag inside it and builds the error after it returns.

Not included: severity or per-incident priority (the storage doc rejects them), anything that changes the cluster, and mute, which is sketched in Section 12 as its own later design.

---

## 9. The macOS application

### 9.1 Shape

An Xcode project under `macos/`, SwiftUI, deployment target macOS 15, built with the current SDK. Nothing here needs a newer API; raising the target later is one project setting, and an app built with the current SDK already takes the appearance of the macOS it runs on. Not sandboxed: a local tool that only opens a loopback HTTP connection gains nothing from the sandbox and would need an entitlement to read nothing. Signed ad hoc ("Sign to Run Locally"); notarization needs a developer account and only matters for distribution to other machines.

The application finds the daemon at the address in its preferences, default `127.0.0.1:7770`, the same default as the daemon's `api_listen`. When the daemon is unreachable every screen shows one state: not connected, the address, and the last error. Nothing is cached across launches except UI state (scope, column widths, window layout).

### 9.2 Layers

```
macos/
  Package.swift               IdiosAPI, IdiosModel, IdiosModelTests
  Sources/IdiosAPI/           openapi.yaml (symlink to api/openapi) and the generator
                              config; the build plugin writes the client and types
  Sources/IdiosModel/         one struct per screen concept, built from the generated types
  Tests/IdiosModelTests/      decoding tests against api/testdata
  idios.xcodeproj             the application, depending on the local package
  idios/
    App/                      scene, menu bar extra, preferences, daemon connection, routes
    Store/                    @Observable stores: one per screen, own loading, errors, SSE
    Views/                    SwiftUI views; read stores, never the client
```

The generated client and the model layer are a Swift package because `swift test` needs one and the generated sources are build output, never a source folder. The application target links the package's two libraries.

The model layer exists for one reason: proto3 makes every generated property optional, and the screens must not be written against `String?` for fields the schema says are never null. Each `Model` type has one initializer from its wire type that unwraps what is guaranteed, keeps optional what is nullable, and converts int64 strings and enums. That layer is also where display derivations live: pod name suffix, relative durations, the four text treatments. It is unit-tested against the same JSON fixtures the Go tests produce (Section 11).

Stores own the client calls and the SSE subscriptions and expose plain state. Views are dumb.

### 9.3 Screens

The visual reference is `docs/mockups/idios-ui.html`. Where it and this document differ, this document wins. Every screen of the window carries a round [?] at its bottom right, and the search palette one in its footer, which turns on the help marks Section 9.7 explains; Cmd-/ turns them on from the Help menu and "?" does when no text field has focus. The screens, and which endpoints feed them:

| Screen | Endpoints |
|---|---|
| Incidents (home): the cluster scope as a titlebar menu (the same checklist, joined by the cluster sheets' entry points); the sidebar as a macOS source list -- Screens, then View (Attention, Open, Acknowledged, then a Closed disclosure holding Recovered, Pod deleted, Job finished, Resolved, Dismissed), then Category -- every row carrying its count, zero included, the categories with no incident folded into one dimmed "N more with none" row, and an active row taking the selection accent, never its state colour; the View and Category sections are shown only on this screen, since they filter nothing else, while Screens stays on every screen, including under a pushed pod page. Attention is the default view: open, or closed within `attention_window` and never acknowledged or dismissed. The list groups by workload by default, and every group header has one shape: a section header with a chevron, the same inset rule as its rows and a faint tint, carrying kind and name, a plain-language summary line ("every 2m, 62 of 63 runs failed, since 07:32" for a CronJob; "3 of 3 pods looping, tag 1.36" for a Deployment), the worst open category and one state badge with the open count; a cluster or namespace header appears only while more than one is in scope and is drawn as a section title over the group headers, larger and carrying only the chevron and the state badge with its open count; the other group modes (namespace, category, cluster, time) stand, time cutting the served order into UTC days. Inside a CronJob's or Job's group the unit is the run: the `job_failed` row and the pod rows of one Job (`job_uid`) fold into one run row naming the run's suffix, its pod count, the categories found among them, the exit code, the reason and the open count; the newest five runs are shown and a "+ N more runs" line opens Workloads on that CronJob's Runs. The replica rollup keeps its key (`container_name` and `category` across two or more pods of one workload) for the other kinds: the pod rows sharing that key fold under one rollup row named by the count of pods and their workload, carrying the shared container and category, every `last_reason` and exit code found among them once each, and a pill counting the incidents; its badge counts the incidents still open, its duration reads from the oldest open row and, once every row is closed, from the newest close; `last_reason` is not in the key because a crash loop alternates its reasons on every occurrence and a rollup that formed and dissolved as loops went in and out of phase would be worse than none; a key held by one pod is a plain pod fold; a bare pod never rolls up; a job-subject incident folds into its run; the rollup never crosses a group boundary and changes no count. Expansion state of groups, runs and rollups persists per group id across the window's life and across a grouping change. A row under a group header is drawn as its child: indented, with a neutral guide rail from the header to the last child that turns in at the end, a faint tint that stops where the children stop, and a heavier rule starting the next top-level row. A row is one line by default with real columns: state dot, category chip, subject as kind and name with a pod's suffix after the workload prefix, container (reading "job" for a job-subject row), reason with exit code, Restarts (the row's `occurrences`, "-" for a job-subject row), age, state badge; the last three have fixed widths and the age never wraps, because the word open or closed lives in the state badge. The second line of namespace, pod, tag and deletion is the row's hover and a Comfortable density in the View menu; the elision and copy rules of Section 7.4 hold; closed rows dim. `manual` is labelled Resolved everywhere, every state and category name carries the one-sentence tooltip of Section 9.6, and the "?" beside the View heading opens the legend popover: the lifecycle, the definition of Attention with its window read from `Status.attention_window_seconds`, and the keys. An empty view teaches: the state's definition, the key that produces it, and a button back to Attention. The summary line above the list says the problems, the open incidents, the workloads and the newest time, and holds Collapse all and Acknowledge all. Search jumps and filter narrows: Cmd-K opens a palette over the screen, matching an incident id (`#187`), a pod name including its suffix, a Job name, a workload, a namespace, a container, a reason, an image tag and a node, sectioned as Pods, Runs, Workloads, Incidents and Commands, with the field prefixes `ns:`, `node:`, `tag:` and `reason:`, Return opening, Cmd-Return opening in Workloads, Shift-Return copying the name (the pod's when the hit has one, the workload's otherwise), and an empty query listing the recent pages and every command with its key; matching is client-side over the rows the stores hold, plus the detail endpoints for an id and the `pod_name` filter for a name the list limit cut off. "/" focuses the filter field, which narrows the served rows and says its own text when it hides every one of them. The keys: the arrow keys move the selection, Space expands, Return opens, `a`/`d`/`r`/`n` act on the selected row or on every incident under a group, run or rollup, `d` and `r` asking first with the count and the workload named, and Command-Delete deletes one incident behind a confirmation. Every count is a count of incidents | `/incidents`, `/incidents/counts`, `/clusters`, `/incidents/stream` |
| Pod page: one screen keyed by pod uid, reached from every route the old screens had (`incident/<id>` fetches the incident, then opens its pod with that container selected and that incident lit on Overview; `timeline/<id>` the same on Timeline; `pod/<uid>` opens the Pod card on Events; `pod/<uid>/<tab>` keeps every old tab name: `containers`, `incidents` and `events` open Events, `conditions` opens Conditions, `logs` opens Files, `podjson` opens pod.json), the back chevron returning to wherever it was opened from and a forward chevron beside it (Cmd-]) returning to where Back came from. Three columns inside the pushed screen: the pod-and-containers column at the sidebar's width (the sidebar keeps its Screens section while the page is up; only View and Category go, because state and category filters are about the list), the selection in the middle, the rail on the right. The column is the Pod as a card, then the containers as cards indented under it (init, app, sidecar, ephemeral, by name inside a kind), each with its state line and one badge per incident on it in the fold order (open first, worst category, newest last seen), a closed one dimmed; the selected card fills with the accent; a pod-level incident's badge sits on the Pod card. `GetPod` is authoritative: the pod row, the containers, the latest conditions, the incidents that put the badges on the cards, the artifacts, the siblings. Selecting a container shows the lit incident's header, above it a segmented control with one segment per incident on the container in the fold order only when the container has two or more incidents. The header leads with a verdict block built from the fields the page already receives: what happened, how many times since when and last when, the memory limit from `mem_limit_bytes`, what was and was not captured from `capture_gap`, and whether the image digest changed since the incident opened (`image_id` at open against the container's); then the category and state tags, the incident's sentence, its explanation and its note; then its actions, where Acknowledge is the one primary button with its key shown, Note is beside it, and the rest are an Actions menu (Dismiss/Undismiss, Mark resolved while open, Unacknowledge, Unresolve exactly when the close was manual, Delete behind a confirmation, Ask AI for the lit incident). Then Overview (the kubelet facts of the container from `GetPod`, the container's files, its events: the pod's events whose `field_path` names it plus every pod-level event tagged as the pod's), Timeline (the lit incident's window) and Logs, where the container's captured logs are a scrubber over one file body (restart N of M, previous and next) and the tab title counts the files that exist; a card's method is its title's hover, never visible text; switching segments changes the header, the note and the Timeline, and nothing else; the previous incident stays until the next arrives. A container with no incident shows no control and no actions. Selecting the Pod card shows the pod-level incidents' control and header when it has any, then Events (every container, a container column, served order), Conditions (the latest per type, then the history), Files (every container's files together and `pod.json`), pod.json, and Related incidents only for a Job's pod (the `job_failed` row and the retries in other pods, by `job_uid`). An incident whose subject is a Job opens the Run page, whether or not a pod of the Job is kept, so a job-subject incident never joins a container's segments and the page's counts line and segments are the pod's own. Its list row's container column reads "job" and its Restarts column "-", because the incident has no pod to be about. The page header is the pod's and never moves: the state tag (DELETED, or the phase with READY / NOT READY from the Ready condition), the counts line (containers, incidents and how many open, node), the title `Pod <name>`, and the identity line cluster / namespace / the owner chain as it exists, stopping before the pod; the identity line is a breadcrumb whose segments open Workloads at that node. A deleted pod shows the banner under the header and dims the live-only fields. The rail is the pod's, and repeats neither the cluster, the namespace nor the image: the owner chain with the container as a fourth dot when one is selected; context, holding Node, QoS class, Image id and Container id; the times of what is selected (the lit incident's opened, last seen, open for, closed and close reason, acknowledged, dismissed; or the pod's created, started, first and last seen, deletion requested, deleted); siblings: this pod first, then the five the daemon serves with a category badge where one is open and a state where none is, and a "+ N more of this <controller kind>" line that opens Workloads > that workload > Pods. In the Timeline, repeating cycles fold into one entry ("x31, every ~4 min, first 07:32, last 09:52") with a disclosure; lifecycle, rollout and cut entries never fold; each entry shows one time, the exact pair on hover. The header tag is debounced and reads "RUNNING, LOOPING" when readiness flipped more than three times in the window. Plurals agree with their counts. The events table never wraps a timestamp: the time column widens and the message wraps. The keys `a`, `d`, `r` act on the lit incident. The incident stream is one subscription for the life of the page, filtered on this pod's rows; any of them re-reads `GetPod`; the lit segment is a local comparison. | `/pods/{uid}`, `/pods/{uid}/events`, `/pods/{uid}/history`, `/incidents/{id}`, `/incidents/{id}/timeline`, `/incidents?job_uid=`, `/artifacts/{id}/content`, `/incidents/{id}/prompt`, the incident writes of Section 8 |
| Run page: one screen for one Job, keyed by the Job's uid (`run/<job uid>`), reached from a run row in the incidents list, the job row inside a run, a run in the Workloads Runs table, a run strip cell, the pod page's breadcrumb Job segment and its rail's Job dot, the Runs section of the palette, and every job-subject incident. The title is `Run <suffix> of <CronJob name>` when a CronJob created the Job and `Job <name>` when nothing did; the word run means a Job a CronJob created, and a standalone Job is never called one. The header is the Job's condition as the state tag (`FAILED, BACKOFF LIMIT EXCEEDED`, `COMPLETE`, `RUNNING`, `DELETED`, `NOT RECORDED` when the sweeper removed the jobs row), a counts line of attempts, incidents and how many open, and how long the run ran, and the identity line cluster / namespace / CronJob as a breadcrumb. Then a verdict block of sentences built from the fields the page holds: how it ended and after how many attempts in how long, the backoff limit against the failed counter, the exit codes among the attempts, whether every attempt captured a log and the last line of the newest one, the image tag, and how many earlier runs of the same CronJob failed for the same reason. Acknowledge is the one primary button with its key shown and acts on every incident of the run, Note is beside it, and the rest are an Actions menu, as the pod page has. Then Overview (the Attempts card, the Job card, and for a CronJob's Job the run strip with this run marked), Timeline (the lead incident's window), Logs (the captured files of every attempt as one scrubber) and Events (the Job's events and its attempts'). The Attempts card is one line per attempt, oldest first: the attempt number, the pod's suffix, the category chip, the exit code and reason, the time, the incident's id and state, and a link into that pod's page on its Logs; the Job's own row closes the card when the Job itself failed. An attempt that opened no incident is drawn from its own pod row, with a pill of its outcome where the incident would be, so an attempt that succeeded is a line like any other; only a pod pruned before idios saw it is counted by the Job alone, in the card's footer. The rail is the owner chain (CronJob, Job, the attempts' pods), context (node, image, backoff limit), the times (scheduled, first attempt, finished, open for) and the other runs of the CronJob with their state pills. `Copy uid` copies the Job's uid, and the keys act on the run's lead incident | `/jobs?job_uid=`, `/incidents?job_uid=`, `/pods?job_uid=`, `/incidents/{id}`, `/incidents/{id}/timeline`, `/jobs?cronjob_name=`, `/artifacts/{id}/content`, the incident writes of Section 8 |
| Workloads: a tree by cluster and namespace, each namespace grouped by kind under plural captions with counts ("DEPLOYMENTS 1", "CRONJOBS 2", "BARE PODS 5") in the fixed order, long-lived kinds first and transient last; kinds carry no colour; the pane header names the trailing number ("open incidents") and holds Collapse all and Expand all, and a filter never clears the collapse state. Pods no controller owns are one row per name, carrying "N pods" when a deleted pod of the same name is kept, and the row opens the live pod or a chooser when more than one shares the name. Every row has a context menu: Show incidents, Show pods, Copy name, Acknowledge all open. The detail is tabs whose titles count what they hold. A CronJob opens on Runs, above the table a run strip: one cell per run in the window, oldest first, coloured by outcome, dashed where a sweep removed the record, hover for condition, duration and exit code, a click opening the run's pod, a drag acknowledging the incidents under the selection; the strip holds up to 200 runs and becomes an hour-by-minute matrix beyond that. The Runs table defaults to Failed when `failed_total > 0`, names runs by the suffix after `<cronjob>-`, links each run to its pod and to its incident, folds consecutive runs with the same reason, and its Condition badge takes the run's incident state (red open, grey closed). Pods defaults to Live (the newest 50, live / not-running / open-incident chips, a truncation line). Rollouts is five newest, the rest folded behind their count, one row per ReplicaSet dated by its own creation. Overview holds the 3d / 24h / 6h window over the stat cards and the restarts chart, which has a y-axis, hover values, true hourly bars and dashed bars for the hours containing reconstructed rows. Incidents is the last tab | `/workloads`, `/workloads/{...}`, `/jobs`, `/incidents` |
| Status: the header is in the system font, configuration values are shown in human units (24 h, 10 min), and the legend popover links here | `/status` |
| Menu bar extra: the headline is the open count and the Dock badge follows it, the subline the attention count; the rows are one per workload group (the fold the list uses), a closed group grey, each row leading with its cluster name while more than one cluster exists, then "N more problems closed in the last 24 h"; a row opens that group in the list and closes the popover; below them Open idios, Acknowledge everything shown, Status... and Clusters and namespaces... | `/incidents?state=attention`, `/incidents/counts`, `/clusters` |
| Add cluster sheet (cluster scope menu): contexts from the daemon, friendly name, namespaces from the cluster with a free-text fallback when the Role cannot list them | `/kube/contexts`, `/kube/contexts/{context}/namespaces`, `POST /clusters`, `POST /clusters/{id}/namespaces` |
| Clusters and namespaces sheet (menu bar extra, cluster scope menu): a two-pane master-detail -- a rail of every cluster (dot red when `last_error` is set) and, for the selected one, four sections: Cluster (rename, read-only identity and connection), Watched namespaces (add and remove, the from-the-cluster picker; picking a namespace starts watching it immediately), Grafana (the label builder below) and Remove behind a confirmation | `/clusters`, `PATCH /clusters/{id}`, `PATCH /clusters/{id}/grafana`, `DELETE /clusters/{id}`, `POST` and `DELETE /clusters/{id}/namespaces[/{name}]` |

The Grafana section's `log_selector` is never typed free text: a label
builder holds one row per LogQL matcher, the label name free text and the
value one of six placeholders (Namespace, Pod name, Container name,
Workload name, Node name, Cluster name) or a static Text value, prefilled
with the three standard Kubernetes rows (`namespace`, `pod`, `container`)
so most users only rename a label or add one. The rows are the stored
string's only writer: they serialise to the brace-and-comma template
Section 4.8 substitutes, and parse back from it losslessly -- a matcher
the builder does not recognise round-trips as a Text row carrying the raw
value rather than being dropped. A live preview substitutes one of the
cluster's own pods, or fixed sample values when it has none, and renders
the same empty-placeholder drop rule Section 4.8 applies server-side.
Saving calls `PATCH /clusters/{id}/grafana`; clearing the URL clears all
three fields, per Section 8.

### 9.4 Native components, and the three that need care

Sidebar, split view, toolbar, tables with resizable columns, tooltips, context menus, Cmd-K filtering, light and dark appearance, the accent colour and the menu bar extra all come from the system. Three places use a deliberate implementation instead of the default control:

- **Tables whose rows contain prose** (events, conditions): SwiftUI `Table` has one row height per table, which contradicts the wrap treatment. These are `List`s with custom rows and remembered column widths, not `Table`s.
- **The log pane**: built on `NSTextView` behind `NSViewRepresentable`, so selection, find and copy behave like every other text view on the system, with horizontal scrolling by default and a Wrap toggle.
- **Remembered column widths**: `Table` does not persist them on its own; its `TableColumnCustomization` kept in `SceneStorage` does (width, order and visibility per column). The prose tables built as custom rows have fixed column budgets, because `Table` on macOS 15 lays columns out at their ideal widths and clips rather than shrinking.

The keyboard layer rides the focus system. Single letters (`a`, `d`, `r`, `n` on the selected row) go through `.onKeyPress` on the focused list or screen, never bare `keyboardShortcut` equivalents, which fire even while a person types into a text field; "/" focuses the list's filter field the same way. "?" turns the screen's help marks on the same way, and Cmd-/ is the Help menu's Explain This Screen, because a menu is where a person discovers it. Cmd-modified keys are menu items, because a menu is where a person discovers them: the Go menu holds Cmd-1 Incidents, Cmd-2 Workloads, Cmd-3 Status, Cmd-K Search, Cmd-[ Back and Cmd-] Forward, and the View menu holds the group mode and the density (Compact, Comfortable). The palette's empty query lists every command with its key, which is the other place the keyboard layer is visible.

### 9.5 Appearance

System light and dark, following the user's setting, and one colour rule. State owns hue: red is open and unacknowledged, amber is acknowledged (and a degraded container state), grey is closed by any reason including `job_finished`, green is live and healthy only -- running and ready, a synced cluster, a Complete run -- and never a closed failure, and the accent is selection only. Categories are nouns: a neutral outlined chip with a small glyph and no hue. Kinds in the workloads tree have no colour. The rule applies to list badges, rail dots, tree dots, timeline dots, the run strip and the pod header tag alike. The mockup's C1 component draws the vocabulary in both appearances and `BadgeStyle.swift` is the one place the colours live; every other colour in the application is a system colour. Two rules are part of the vocabulary: a container that terminated with exit code 0 takes the neutral style, never red, and one rule colours every cluster dot -- red whenever `last_error` is set, green when ready with no error, else grey.

### 9.6 Vocabulary

Every state and category name in the application carries this text as its tooltip, and the legend popover shows the states with their keys and the definition of Attention, its window read from `Status.attention_window_seconds` and never hard-coded; the wire value is in parentheses where the label differs.

| Word | Tooltip |
|---|---|
| Open | Still happening, or still sitting broken. Nobody has acknowledged it. |
| Acknowledged | Still open, but a person has marked it seen (a). Kept if the incident reopens. |
| Recovered | Closed by the system: the container ran and stayed ready for 10 minutes, or the pod got scheduled. Can reopen. |
| Pod deleted | Closed because the pod is gone. Final. Shown with the inferred reason: rollout, scaled down, job pruned, evicted, unknown. |
| Job finished | The Job reached its final condition, was deleted, or a later run of the same CronJob completed. The run may have failed; finished is about the Job's lifecycle, not its outcome. Final. |
| Resolved (`manual`) | A person closed it (r). Final: a recurrence opens a new incident. Can be undone while still resolved. |
| Dismissed | Hidden by a person (d). Outranks every other state. Cleared if the incident reopens. |
| Attention | A view, never a row's state: everything open, plus anything that closed in the last N hours without being acknowledged or dismissed, where N is the daemon's attention window. |
| Restarts | Container restarts attached to the incident (`occurrences`). "-" for an incident whose subject is a Job. |
| job (container column) | The incident's subject is the Job itself, not a container. |
| crash | The container exits with a non-zero code and the kubelet restarts it (CrashLoopBackOff, Error). |
| oom | The kubelet killed the container for exceeding its memory limit (OOMKilled, OOMKilling). |
| unclean exit (`unclean_exit`) | The container died badly while its pod was terminating: a non-zero exit or a signal on the way out (Error). |
| image pull (`image_pull`) | The image cannot be pulled (ImagePullBackOff, ErrImagePull, InvalidImageName). |
| config | The container cannot be created or started from its spec: a missing ConfigMap, Secret or volume, or a command that cannot run (CreateContainerConfigError, CreateContainerError, ContainerCannotRun, StartError). |
| probe | A liveness or readiness probe fails while the container runs (Unhealthy). |
| scheduling | The pod has had no node for longer than the grace window (FailedScheduling, Unschedulable). |
| stuck | The pod has been pending or terminating for longer than the stuck threshold with no reason of its own. |
| node pressure (`node_pressure`) | The kubelet evicted the pod for node pressure (Evicted from the kubelet, TerminationByKubelet). |
| rescheduled | The pod was preempted or evicted by a controller and will be placed again (PreemptionByScheduler, DeletionByTaintManager, Evicted from the eviction API). |
| job failed (`job_failed`) | The Job reached its Failed condition (BackoffLimitExceeded, DeadlineExceeded). |
| other | A failure idios records but does not classify; the reason is shown as stored. |

Every kind name in the application carries this text as its tooltip, and the legend popover shows them under a Kinds heading with the chains they form.

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

CronJob -> Job (a run) -> Pod (an attempt) -> Container; Deployment -> ReplicaSet -> Pod -> Container; bare Pod -> Container.

### 9.7 Explanations

The [?] at the bottom right of every screen, and the one in the search
palette's footer, turns help mode on: each part of the screen the tables
below explain carries a small round "?" mark of its own, inside its top right
corner, or just outside its trailing edge and vertically centred where
the region is under thirty points tall and has no corner to spare.
Clicking a mark outlines that region with the accent and opens a popover
carrying the region's name, the stored fields, tables or endpoints it
draws as monospace chips, and one or two sentences on what it means and
why to look there. The [?] stays where it is, tinted, because it is also
the way out; "?" with no text field focused and Cmd-/ do the same, and
Esc closes help mode. The outline is the accent, which is selection only
and this is a selection of regions; nothing in help mode carries a colour
of its own. A screen whose content depends on what is selected has one
table per selection state, and help mode marks the state in view. Where a
region's name is a state, a category or a kind, its sentence is that
word's tooltip from Section 9.6 unchanged, so a note and the tooltip never
say two different things. Help mode marks the major parts of a screen and
not every control on it: a screen covered in marks is a screen a person
reads none of.

The pod page with a container selected:

| Region | Draws | What the note says |
|---|---|---|
| Pod state tag | `pods.phase`, `conditions[Ready]`, `deleted_at` | Look here first. The kubelet keeps reporting phase Running while every container fails, so the Ready condition is what says the pod is not serving. It reads LOOPING when readiness flipped more than three times in the window, and DELETED once the pod is gone. |
| Container cards | `containers.kind`, `category`, `occurrences` | One card per container, init then app then sidecar then ephemeral, the selected one filled with the accent. Each badge is one incident with its category and how many times it has happened, so this column says whether one container is failing or the whole pod is. |
| Verdict block | `occurrences`, `mem_limit_bytes`, `capture_gap`, `image_id` | Read this second, and often stop here: sentences built from fields the page already holds. What happened and how often since when, the memory limit when the kill was an OOM, what was and was not captured, and whether the image digest changed since the incident opened. |
| What the kubelet reports | `restart_count`, `last_terminated_reason`, `image_id`, `mem_limit_bytes` | Read this third: the evidence the verdict was built from, in the kubelet's own words. Restart count is the container's whole life while occurrences above is this incident's share of it, and the image and digest are the ones recorded when the incident opened, not the current ones. |
| Captured logs | `artifacts`, `capture_gap`, `capture_note` | Read this fourth. The scrubber walks one file per dead instance, oldest to newest, so you can see whether every attempt failed the same way. A missing file is never an empty pane: the gap is named and the API's own words are quoted, which separates a container that printed nothing from a log that could never have been fetched. |
| Owner chain | `workload_kind`, `controller_kind`, `pod_uid`, `container_name` | Read this last, when the verdict is not enough: four links from what decides how many copies run down to the process that failed, each with the uid that identifies it. It answers who will replace this pod, and whether the thing to change is this container or the workload above it. |
| Siblings | `controller_uid`, `category` | This pod first, then the others of the same controller, with a category badge where one is failing and a readiness word where none is. One red dot among green is this pod's problem; all red is the image, the config or the cluster. |

The container's Logs tab drops the kubelet's card, and its Timeline tab
drops that and the captured logs; the tag, the cards, the verdict and the
rail's two are the same regions with the same notes.

The pod page with the Pod card selected keeps the tag, the container
cards and the rail's two with the same notes, and marks the pane's own
tab in place of the container's three regions:

| Region | Draws | What the note says |
|---|---|---|
| Events | `k8s_events`, `involved_uid = pod` | Every container's events together in one served-order table with a container column, plus the pod-level events no container claims. It is the pod's whole event stream, not one container's slice of it. |
| Conditions | `pod_condition_history` | The latest reading of every condition type the pod carries, PodScheduled through Ready, with the history beneath it. A condition is a fact about the pod as a whole; no container has one. |
| Files | `artifacts` | Every container's captured files in one list together, container named on each row. Grouped here because a person comparing two containers' logs for the same failure should not have to leave the pod. |
| pod.json | `artifacts` (kind `pod_json`) | The captured pod object as idios stored it, sanitized before an agent ever sees it. One object describes the pod's whole spec and status; no container has its own copy. |
| Related incidents | `job_uid` | The `job_failed` row and the retries in the Job's other pods, joined by `job_uid`. Shown only when the pod's controller is a Job, because only a Job has other pods to compare this one against. |

The incidents list with rows in it:

| Region | Draws | What the note says |
|---|---|---|
| Summary line | `incidents.id`, `pods.uid`, `last_seen_at` | What the whole view amounts to before a single group is opened: how many problems, meaning groups, how many incidents inside them are still open, how many workloads and bare pods they touch, and the newest time anything was seen. Collapse all folds every group; Acknowledge all marks every open incident of the view seen and asks first, because the rows it writes are not all on screen. |
| Group header | `workload_kind`, `workload_name`, `controller_uid` | One problem, not one incident: every incident of the same workload on one line, with the worst category among them, how many are still open and how many pods they touch. Clicking it folds the group and opens it again, and a key pressed on it acts on every incident it stands for at once. |
| Run row | `job_uid`, `GET /jobs`, `occurrences` | One Job and the attempts it made, which is what a CronJob produces on every tick of its schedule. Each retry is a new pod, so this line is what says whether the whole task failed or only one attempt did; opening it lists the attempts, and clicking the run opens the page that puts them side by side. |
| Rollup row | `workload_kind`, `category`, `pods.uid` | One line standing for several pods of one workload that are failing the same way, drawn instead of repeating the same reason down the screen. The number is how many pods, which is the fact that says the workload is broken rather than one pod; opening it lists them. |
| Pod row | `incidents.id`, `pods.uid`, `incidents.state` | One incident: the pod, the container, the reason and how long it has been going on. The badge at the end of the line is its state, which says whether anyone has dealt with it and how it ended if it is over. Return opens the pod page on that container, a, d and r act on the row without leaving the list, and a pod that failed more than once folds its other incidents under this line. |

The incidents list with nothing in the chosen view has one region, which
is the word the view is empty of:

| Region | Draws | What the note says |
|---|---|---|
| What the state means | `incidents.state`, `incidents.category` | The definition of the state whose view this is, word for word the sentence its badge carries on hover, with the chosen category's definition under it. The last line is the key that puts a row here: a for acknowledged, d for dismissed, r for resolved. An empty view is where those words are worth reading. |

The search palette, which carries its own help mode because it is an
overlay itself:

| Region | Draws | What the note says |
|---|---|---|
| Query field | `ns:`, `node:`, `tag:`, `reason:`, `#` | One field over whatever screen is up: type part of a pod, a workload or a reason and the sections below fill in as you go. A prefix narrows what is searched, ns: to a namespace, node: to a node, tag: to an image tag and reason: to the kubelet's word, and a leading # goes straight to an incident id. Nothing typed here changes the list behind it. |
| Footer keys | `Return`, `Cmd-Return`, `Shift-Return` | The three ways out of the chosen hit: Return opens it, Cmd-Return opens the workload it belongs to in Workloads, and Shift-Return copies its name and leaves the palette open. Esc closes the palette without opening anything. |

The Workloads tree, whose three regions stay marked beside every tab of
the detail because the tree keeps its column:

| Region | Draws | What the note says |
|---|---|---|
| Pane header | `open_incidents` | It names what every trailing number down the tree counts: incidents still open, not pods and not restarts, summed up the tree so a cluster's number is its namespaces' and a namespace's is its workloads'. Collapse all folds every cluster and namespace at once and Expand all opens them again, which is how a tree of many namespaces is read one at a time. |
| Kind caption | `workload_kind` | A heading and not a level: it names the kind of the rows under it and counts them. The kinds are in a fixed order, the long-lived ones first and the pods nothing owns last, so the same namespace reads the same way every time rather than reordering itself as failures move around. |
| Pods nothing owns | `workload_kind = none`, `pods.name` | A pod with no controller has no workload to be grouped under, so it is listed under its own caption and opens a pod page rather than a pane. There is one row per name, not per pod: a name that has been used by a pod that was deleted and one that is live reads "2 pods" and asks which of them to open, because the name is what a person knows and the uid is what identifies one of them. |

A workload's Overview tab adds two:

| Region | Draws | What the note says |
|---|---|---|
| Window cards | `incidents`, `occurrences`, `category`, `image tag` | What the window amounts to in four numbers: how many incidents and how many of them are still open, which kinds of failure they were, how many restarts are attached to them and across how many pods, and which image tags were running when they opened. A tag that appears once beside a count of many is the fact that says a single build is behind them. |
| Restarts per hour | `container_state_history`, `gap_reconstructed` | One bar per hour of the window, including the quiet hours, because a chart that skips them draws a busy stretch where there was none. The axis is labelled with the peak hour, half of it and zero. A hollow dashed bar is an hour idios pieced together after missing part of it, so a bar you can see through is a count that may be short. |

A workload's Pods tab adds two:

| Region | Draws | What the note says |
|---|---|---|
| Pod chips | `pods_live`, `worst container state`, `open_incidents` | Live is the only one the daemon filters by, and it is where the tab opens: pods it has not seen deleted. The other two are read off the page that came back, so they narrow what is already here rather than asking for more: whose worst container is waiting or terminated, and who carries an open incident. |
| What the page stands for | `pods_truncated`, `live_pods`, `deleted_pods` | How many pods this is out of how many exist, and it says so when the page is the newest few of many. A bounded page is not a lie about the total: the number after "of" is the stored count, so a workload that has churned through hundreds of pods still says so. |

A schedule's Runs tab, which is the tab a CronJob opens on, adds three:

| Region | Draws | What the note says |
|---|---|---|
| Run strip | `GET /jobs`, `condition_type`, `condition_reason` | One cell per run, oldest at the left, coloured by how the run ended. A hollow dashed cell is a run whose record the sweeper removed: the schedule ticked and idios no longer holds what happened, which is not the same as a run that succeeded. The strip stands for the whole window whatever the chips say, because a picture that changes with a filter is no longer a picture. Hovering a cell gives the condition, the duration and the exit code, clicking one opens that run, and dragging across several acknowledges their open incidents. Past two hundred runs the line folds into one row per hour and one cell per minute, so a schedule that ticks every minute still fits the pane. |
| Condition column | `condition_type`, `condition_reason`, `failed counter` | Whether the run succeeded comes from the condition the cluster wrote on it, never from the counters: the failed counter counts pods, and a run that retried twice and then succeeded would read as a failure. A run with nothing on it yet is still going. Missed schedules are not detected, on purpose. |
| Folded runs | `condition_reason` | Runs that follow one another and failed for the same reason are one line with the rest counted behind it, so a schedule that has failed every tick for a day does not fill the pane with one repeated sentence. Show opens them, and the times are what say whether it is still happening. |

A Deployment's Rollouts tab adds two:

| Region | Draws | What the note says |
|---|---|---|
| Rollouts table | `rollout_history`, `revision`, `ready_replicas` | One line per revision a Deployment has had, newest first: the revision number, the ReplicaSet that carried it, its images, when it shipped, how many of its copies came up ready, and how many incidents opened on it. This is the table that answers whether the failures arrived with a deploy. |
| What SHIPPED means | `rollout_history.created_at` | The time is the ReplicaSet's own creation time, so it says when the revision shipped and not when idios met it, which is what makes reading it against the incident times worth anything. Reading them together is a correlation; idios does not assert that the deploy caused the failure. |

A workload's Incidents tab adds one, because the tab is one card:

| Region | Draws | What the note says |
|---|---|---|
| Incidents of this workload | `GET /incidents`, `workload_kind`, `workload_name`, `pod_uid` | Every incident idios opened on this workload, open and closed, in the same row the triage list uses, so a badge means here what it means there and clicking a row opens the pod page on the container that failed. Closed ones are kept: a workload that failed and recovered twice this week is a different workload from one that has failed once, and the tab's own count says how many of these are still open. |

A run's Overview tab:

| Region | Draws | What the note says |
|---|---|---|
| Run outcome tag | `GET /jobs`, `condition_type`, `condition_reason`, `jobs.deleted_at` | Look here first. How a run ended is the condition the cluster wrote on the Job and never its counters: a run that retried twice and then succeeded has a failed counter above zero and still completed. A Job with no condition on it yet reads RUNNING, a Job that is gone from the cluster reads DELETED, and NOT RECORDED means the sweep removed the Job's own row and what is left here are the incidents. |
| Counts line | `incidents.job_uid`, `jobs.started_at`, `jobs.finished_at` | Attempts, then incidents and how many are still open, then how long the run took. They are separate words because they count different things: an attempt is one pod the Job started, and the Job giving up is an incident of its own, so two attempts can carry three incidents. |
| Verdict block | `backoff_limit`, `failed`, `succeeded`, `exit_code`, `image_tag` | Read this second, and often stop here: sentences built from fields the page already holds. How the run ended and after how many attempts, the retry budget it was given and what was counted against it, how the attempts exited, what they captured, the image tag they ran, and whether earlier runs of the same schedule failed the same way. |
| Attempts card | `pods.controller_uid`, `incidents.job_uid`, `exit_code`, `failed` | Read this third: one line per pod the Job started, oldest first, with the reason, the exit code and the incident it opened; an attempt that opened none says how its pod ended instead, and a last line closes the card where the Job gave up. The pod cell opens that attempt's captured logs. The footer is the one thing the page cannot draw: a pod pruned before idios saw it is counted by the Job and has no line here. |
| What the Job reports | `condition_message`, `completions`, `parallelism`, `active_deadline_seconds` | Read this fourth: the Job's own row as stored, in its own words. The condition message often names what the controller objected to, the counters say what the run was told to do and how far it got, and the deadline is the one a DeadlineExceeded run exceeded. A run whose row the sweep removed says so here rather than drawing zeroes. |
| Other runs | `GET /jobs`, `cronjob_name`, `condition_reason` | The five newest other runs of the same schedule, newest first, each with the condition it ended on, and a link to the schedule's whole history when there are more. It answers whether this run is the exception or the rule without leaving the page. |

The run's other three tabs keep the tag, the counts, the verdict and
Other runs with the same notes, and mark one region of their own in place
of the Overview tab's two cards:

| Region | Draws | What the note says |
|---|---|---|
| Timeline of the lead incident | `GET /incidents/{id}/timeline`, `container_state_history`, `k8s_events` | The transitions and events of the incident the header's actions act on, oldest first, with repeated crash cycles folded into one entry that counts them. It is one attempt's window and not the whole run's: the way to another attempt's is its pod, from the Attempts card on Overview. |
| Captured logs of this run | `artifacts`, `capture_gap`, `capture_note` | Every log idios captured for this run in one scrubber, so the attempts can be read one after another and compared. A missing file is never an empty pane: the gap is named and the API's own words are quoted, which separates an attempt that printed nothing from a log that could never have been fetched. |
| Events of this run | `k8s_events`, `involved_uid` | The Job's own events and those of its attempts' pods together in one served-order table with a container column. The Job's events are where the controller says why it created another pod or stopped creating them, which no pod's own stream carries. |

The Status screen:

| Region | Draws | What the note says |
|---|---|---|
| What each watcher is doing | `status.clusters`, `ready`, `last_event_at`, `skew_seconds`, `last_error` | Read this second: one line per cluster idios watches, whether its watch is synced, when an object last arrived, how far its clock is from this machine's, and the last error it stored. The dot is coloured by one rule and one only: green while the cluster is ready and carries no stored error, and otherwise not green, because a list that has stopped filling is the one failure that makes every other number on this screen a lie. |
| Process counters | `writer`, `handlers`, `capture`, `closer` | Read this third: what the four moving parts have done since the daemon started. The writer's p99 is how long a transaction takes. The capture queue is bounded on purpose, so queued is how much work is waiting and dropped is captures it refused rather than fall behind: a dropped count above zero is why a log is missing. The closer is the pass that ends incidents nothing is reporting any more. |
| Artifacts by outcome | `artifacts`, `capture_gap` | What the captures produced: a bar per outcome, file being the ones that exist and every other word a reason a log could not be read. Under them the attempts that came back with a gap instead of a file, counted by the same word, which is what says whether the misses are one cluster's permissions or the kubelet's. |
| Latest sweep | `sweep_runs`, `cutoff`, `rows_removed`, `retention_days` | Read this fourth: the newest retention sweep of every table it touches, with the cutoff it cut at and what it removed. This is why a row goes away: idios keeps what is newer than the retention and deletes the rest, so a run or a pod that is no longer on a page was not lost, it aged out. |

---

## 10. Security

- The listener binds `127.0.0.1`. There is no authentication in this version: the daemon and the application run as the same user on the same machine, and loopback is not reachable from elsewhere. Adding a token is additive if a listener ever binds elsewhere.
- `pod.json` and logs are served as stored to the application. Container logs contain secrets (`process-architecture.md` Section 12); the application shows them and copies them to the clipboard on request, and never sends them anywhere else. On the surfaces built for an AI agent -- the MCP server's `read_pod_json` and the snapshot prompt -- the pod object passes through `internal/sanitize` first: every environment value and the `last-applied-configuration` annotation are replaced by a marker, names and structure kept. Secret references (`envFrom`, `valueFrom`, volume and pull-secret names) stay: they are names, not contents. Log content is served as captured on every surface.
- The application never reads the kubeconfig. Context discovery is a daemon endpoint that returns names and server URLs only.
- Data directory permissions are unchanged (`0700` / `0600`); the API adds no files.

---

## 11. Testing

Ordered by value.

**1. Query functions** against a seeded temp SQLite, table-driven, asserting the exact result set (whole structs, `go-cmp`). Seeds are built from the same scenario fixtures `internal/ingest/testdata` already holds, pushed through the processor, so a query test reads the rows the recorder really writes. Every function; every filter; every derived value (`state`, owner chain, timeline merge order, the `cut` entry).

**2. Handlers** through the generated Go client against `httptest`: each endpoint returns the mapped rows; 400 and 404 shapes; `cluster_ids` scoping; the artifact content endpoint's content types and its 404 body for a gap; every write once and twice against the seeded store, asserting the exact rows and files after each call, and its row on the stream.

**3. Wire contract.** One test per message family serializes a fixture through the generated Go server and asserts the exact JSON bytes (int64 as string, enum as stored string, unset optional absent). Those bytes are written to `api/testdata/*.json` and are the fixtures the Swift model tests decode, so both sides are tested on identical input. A change in the wire shape fails both suites.

**4. Stream.** A fake notifier emits open, attach, close, reopen; the Go SSE client receives whole rows in order; reconnect reloads.

**5. Swift model layer.** Decoding tests against `api/testdata/*.json`: every model initializer, the pod name suffix rule, the text treatments' inputs, the cluster scope rules, the two stored timestamp shapes. Run with `swift test --package-path macos` (`make app-test`), no simulator, no UI; the tests find `api/testdata` from their own file path, so they run only inside the checkout.

**6. The screens** are checked by a person against the running daemon and the mockup. `idios mock` serves the fixtures of item 3 on `api_listen`, so the application can be built against a stable target before a daemon has data. sebuf's generated mock (`generate_mock`) is not used: it does not compile for messages with `optional` scalars and `int32` counts.

---

## 12. What is deliberately not here

| Not included | Why |
|---|---|
| Resource usage (CPU and memory bars) | idios stores requests and limits as written in the spec; nothing measures usage. Would need `metrics.k8s.io` polling: a new watcher, a new table, and a second source of truth for pod state. Revisit as its own design. |
| Nodes, Services, Deployments, HPA, PVCs, certificates as objects | Cluster-scoped or not watched; the design assumes namespace-only RBAC. Each is one more informer, table and diff, and a scope decision. |
| Severity, priority, SLOs, paging, assignees | Rejected by the storage doc; no definition beyond "derived from category". |
| Live log streaming, exec, port-forward, scale, restart, delete pod | Cluster mutation and live browsing are not this tool's job. The API is a view of the record. |
| Mute | Wanted, later. Shape: a mute is a rule chosen from an incident's own identity chain (this pod, this container, this workload, this job, this category on this workload, this namespace), stored in a small table and matched at read time; recording is untouched; muted incidents leave the default lists and counts and live under a Muted folder. The list response carries a computed `muted` flag so v1 clients need no change when it lands. |
| Display rules for long image references (strip known registry prefixes) | Later and UI-only; the row already shows `image_tag`, which covers the common case. |
| Authentication, non-loopback listener | Same user, same machine. Additive when needed. |
| Cursor pagination | Lists are bounded by retention. `limit` and a truncated marker suffice; the list message shape leaves room. |

---

## 13. Configuration values referenced by this design

| Setting | Default | Used in |
|---|---|---|
| `api_listen` | `127.0.0.1:7770` | Section 2. A different port is the only expected change. |
| `api_stream_throttle` | 1 s per incident id | Section 5 |
| `api_list_limit` | 500 | Section 4 |
| `kubeconfig` | unchanged | Section 4.1 context discovery |
| `scheduling_grace` | 60 s | Section 4.6 status; the `scheduling` category's grace window |
| `stuck_after` | 10 min | Section 4.6 status; the `stuck` category's threshold |
| `attention_window` | 24 h | Section 4.2; the list's default view |

The application stores its own preferences (daemon address, cluster scope, column widths, window layout) in `UserDefaults`; none of them reach the daemon.

---

## 14. Distribution

The installed application owns the daemon; nothing else in the design
changes. The Go binary is bundled at `idios.app/Contents/Resources/idios`
and the whole story is built on building locally, because the app is
unsigned: `install.sh` builds the daemon and the app on the user's
machine (Xcode and Go are the prerequisites), embeds the daemon, signs
both ad hoc, and installs to `/Applications`. An app built locally never
carries the quarantine attribute, so Gatekeeper has nothing to block.

On launch the application decides one of three actions, in this order:

1. **connect** -- something already answers `/v1/status` on the
   connection address. The running daemon is used as found; this keeps a
   development daemon authoritative. The same action, with the
   not-connected screen as the outcome, applies whenever spawning is off
   the table: a `-daemon` launch argument, a non-default address, a
   `-screenshot` run, or a bundle without the embedded binary (a plain
   `make app` build).
2. **setup** -- nothing answers, spawning is allowed, and the daemon's
   `idios.toml` has no `kubeconfig`. The first-run sheet opens and
   cannot be dismissed: step one asks for the kubeconfig path
   (prefilled with `~/.kube/config` when it exists; the file picker
   shows hidden files), validates that the file is readable, writes the
   toml, spawns the daemon and waits for `/v1/status`. Step two is the
   existing add-cluster flow and can be skipped.
3. **spawn** -- nothing answers, spawning is allowed, and the
   kubeconfig is configured. The application starts the bundled binary
   with `run` and no flags (the defaults already point at
   `~/Library/Application Support/idios`), and terminates it with
   SIGTERM when the application quits.

The kubeconfig choice is the daemon's setting, not the application's:
the app reads and writes only the `kubeconfig` line of
`<data_dir>/idios.toml` (directory `0700`, file `0600`) and preserves
every other line byte for byte, so a hand-edited setting survives and a
hand-started daemon reads the same truth. Settings shows the current
path with a Change button -- changing it rewrites the toml and restarts
a spawned daemon, and is disabled with a note when the daemon is
external -- and a Start at login toggle (`SMAppService`), which is what
makes an app-owned daemon effectively continuous.
