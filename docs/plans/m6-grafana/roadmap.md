# m6 - Grafana log links

Status: complete 2026-09-01. All three steps shipped; the user verified
the links against a live cluster, which surfaced and fixed two bugs
(windows built on the ingest clock instead of the kubelet's, and the
namespace picker's hidden submit).

Goal: a cluster can carry a Grafana + Loki configuration, and every pod
container and incident of that cluster gets an Explore link scoped to its
own time window - the escape hatch for the moment idios could not capture
a log (`artifacts.capture_gap`) and the fast path when it did. The
clusters sheet is rebuilt as a two-pane master-detail so the config has a
proper home and the existing rename/namespace editing stops being one
flat list. Visual reference: `docs/mockups/idios-ui.html` page 9. This
milestone is deliberately small: three columns, one write RPC, one pure
URL builder, two computed read-model fields, one sheet, two buttons.

## Decisions (made, do not relitigate)

1. Config is three columns on `clusters` (`grafana_url`,
   `loki_datasource_uid`, `log_selector`, all `TEXT NOT NULL DEFAULT ''`),
   not a JSON blob and not a side table. Schema edits are free until the
   first stable release, STRICT columns keep validation in the schema,
   and the read models select fields instead of parsing JSON. Configured
   means `grafana_url` is non-empty; clearing the URL clears the feature.
2. `log_selector` is one LogQL selector template. Placeholders
   `$namespace`, `$pod`, `$container`, `$workload`, `$node` and
   `$cluster` are filled from the row a link is built for (`$cluster` is
   the cluster's friendly `name`). A matcher whose placeholder
   substitutes to the empty string is dropped whole - `container=""`
   in LogQL means "streams without that label", which silently matches
   nothing useful. The application's label builder is the only writer of
   this string (static values are written inline); the daemon never
   parses it beyond substitution and matcher dropping.
3. The daemon builds the URLs. A pure builder in `internal/grafana`
   assembles the Explore URL (panes JSON, range, encoding); the read
   models in `internal/query` attach it. Open-ended windows use
   Grafana's literal `"now"`, so a served URL is never stale and the
   query layer needs no clock. The MCP server proxies the HTTP API, so
   `get_incident` and `get_pod` carry the links with zero MCP work.
4. Time windows, all UTC, all padded by a fixed 5 minutes each side (the
   pad absorbs `skew_seconds` and Loki ingest lag): an incident runs
   `opened_at` to `closed_at`, or to `now` while open; a pod container
   runs its own run, not the pod's lifetime, and every bound prefers the
   kubelet's clock (the state history's k8s times, `running_since`, the
   pod's `started_at`/`created_at`) over idios's `first_seen_at` and
   `updated_at` - Loki stamps with the kubelet's clock, and a pod
   ingested after it died would otherwise window on the ingest moment
   and hold no logs. An incident without a pod row (a job incident, or a
   swept pod) gets no link.
5. Placement: detail read models only. `IncidentDetail` gains one
   `grafana_url` (container-scoped when the incident carries a
   `container_name`); the `Container` message gains a per-run
   `grafana_url` filled by `GetPod`. List rows stay flat.
6. The app shows the link as the Grafana brand mark, colours and all - a
   documented exception to the BadgeStyle-only colour rule, nominative
   use of the trademark.
7. Nothing in fixtures, tests, docs or mockups names a real
   organisation, host or workload; example hosts are
   `*.example.grafana.net`.

## Steps

### A. Daemon - config, builder, links (`daemon.md`)

The three columns and the store setter; the proto changes (`Cluster`
fields, `SetClusterGrafanaRequest`, PATCH `/clusters/{id}/grafana`,
`IncidentDetail.grafana_url`, `Container.grafana_url`) and `make
generate`; validation in the handler (absolute http(s) URL or empty;
when set, a non-empty datasource uid and a selector containing `$pod`);
notify `notify.Cluster` after commit; `internal/grafana` with exact-URL
table tests; window rules and link attachment in `internal/query`; wire
fixtures regenerated so `idios mock` serves a configured cluster and
linked details. Spec statements land in `docs/design/data-storage.md`
(columns) and `docs/design/presentation.md` (fields, windows, builder
contract) in the same step.

### B. Application - the clusters sheet (`sheet.md`, written when B starts)

The two-pane sheet of mockup page 9: cluster rail, Cluster section
(rename moves here), Watched namespaces section (existing add/remove and
picker, resectioned), Grafana section with the label builder (rows of
label name = value source, value sources are the six placeholders plus
static text; three standard rows prefilled; the builder serialises to
the selector template and calls `SetClusterGrafana`), live preview
rendered from the rows, Remove section last. `Cluster(wire:)` gains the
three optional fields.

### C. Application - the links (`links.md`, written when C starts)

The Grafana button on every pod container row (per-run window, from
`Container.grafana_url`) and on the incident header (from
`IncidentDetail.grafana_url`), opening the served href in the browser;
the brand asset; prominence next to a `capture_gap` artifact row where
the captured log is missing; screenshots of the new sheet and a linked
detail via `hack/macos/screenshot.sh`.

## Order and status

A, then B, then C (both need A's fields; C also wants B's config to
exist to be demonstrable). Checkpoint before every commit per
`CLAUDE.md`; `make generate-check` after the commits that touch
`api/proto`; `make app-test && make app` for B and C; a schema change
means `rm -rf .storage .storage/smoke` before the next run. Nothing is
committed without the user's review of the diff.

- Step A: done (plan: `daemon.md`)
- Step B: done (plan: `sheet.md`)
- Step C: done (plan: `links.md`)
