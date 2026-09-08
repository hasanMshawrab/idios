# idios

Local Kubernetes incident monitor (Go, client-go, SQLite) with a macOS
application. Design docs live in `docs/design/`: `data-storage.md` (what is
stored), `client-go-methods.md` (how it is filled), `process-architecture.md`
(how the process runs), and `presentation.md` (how it is shown: API and app).
The UI mockup is `docs/mockups/idios-ui.html`.

Plans live in `docs/plans/`, one directory per milestone; start with
`docs/plans/README.md`.

## Hard rules

- ASCII only in everything you write. See `.ai/ascii-only.md`.
- Tests earn their place: trace to a spec scenario or a bug, table-driven,
  exact assertions, edge cases first. See `.ai/tests.md`.
- Comments say why, never what. See `.ai/comments.md`.
- Code is the truth; never reference docs or plans from code. See
  `.ai/code-is-truth.md`.
- Build the task, nothing around it. See `.ai/scope.md`.
- Commits: `area: imperative subject`, body only for the why, no trailers
  and no AI attribution, never commit red. See `.ai/commits.md`.

## Conventions

- Every timestamp written to the database goes through `clock.Format`
  (`clock.Layout`, fixed width, UTC). Process time comes from an injected
  `clock.Clock`; Kubernetes timestamps are stored verbatim.
- Every mutation goes through `store.Writer.Tx`; display reads go through
  `store.Reader`. Mutation SQL lives in `internal/store`, one file per area
  (`ingest_sql.go`, `capture_sql.go`, `sweep_sql.go`, ...), never inline
  elsewhere. Read-model SQL lives in `internal/query`, one file per endpoint
  row set.
- Incident categories key on the Kubernetes `reason`, for an event its
  `source_component`, and whether the pod is terminating
  (`deletion_requested_at`), never on `phase`, `exit_code` or `message`; a
  test enforces it.
- Every schema change is a new `NNNN_description.sql` under
  `internal/store/migrations`; `0001_init.sql` is never edited again.
  `Store.Migrate` applies each file the database has not seen in its own
  transaction. There are no down migrations, so a migrated database is one
  an earlier build refuses to open.
- A list/watch failure of any kind is a cluster error and clears `ready`
  until the identity probe succeeds. The grace window and the stuck
  threshold are comparisons in `internal/incident` against an injected
  `Policy`, never timers; a persisting condition can open an incident but
  never attaches to one. An incident that must be able to close as
  `recovered` carries a `container_name`. `JobRow` lives in `jobs.proto`
  because `incidents.proto` cannot import `workloads.proto`, which imports
  `pods.proto`, which imports `incidents.proto`.
- SQLite `ON CONFLICT (cols) WHERE <predicate>` matches a partial index
  only when the predicate is spelled exactly as in the index.
- The captured pod object reaches an AI agent only through
  `sanitize.PodJSON`; the MCP server's `read_pod_json` and the snapshot
  prompt share it. `idios mcp` is a read-only client of the daemon's
  HTTP API and never opens the database (`internal/archtest` enforces
  the imports). Prompt text lives in `internal/prompt` and names the
  MCP tools and the cluster the way the tools take them, so the prompt
  and the tool vocabulary change together.
- Every exported identifier has a one-line doc comment; `Test*` functions
  are exempt. `.golangci.yml` excludes `unparam` for `_test.go`.
- Pins: Go 1.26 (`toolchain go1.26.7`), `modernc.org/sqlite` v1.57.0 and
  `k8s.io/*` v0.37.0; raise them together with the `go` directive.
- The app icon and the menu bar glyph are generated: `make app-icon` runs
  `hack/macos/icon.py`, which writes `macos/icon/idios.svg` and every PNG
  under `macos/idios/Assets.xcassets`; none of those are hand-edited, and
  `hack/ascii-check` skips `.png`. The 32pt and 16pt icons are separate
  reductions (three records, then two, then one), not downscales. Never
  pass `magick` a `-size` for an SVG: its bundled renderer crops the
  drawing to that box instead of scaling, and every size smaller than the
  artwork comes out blank. The menu bar asset is a template image, so the
  glyph carries no colour of its own; the icon's red tittle is fixed
  artwork and the Dock badge owns the incident count. The Dock keeps the
  tile it cached for a bundle, so a rebuilt icon needs
  `lsregister -f <app>` and `killall Dock` before it shows; the app icon
  lives in `Assets.car` under `CFBundleIconName`, and the four-representation
  `AppIcon.icns` beside it is only the legacy fallback, not a truncated
  build.
- Generated code under `internal/apigen` and `api/openapi` is never edited;
  `make generate` rebuilds it from `api/proto` and `make generate-check` must
  be clean before a commit that touches `api/proto`.
- gofmt rewrites a doubled straight quote in a doc comment into a curly
  quote; write "empty" instead of two quotes.
- Checkpoint before every commit: `go build ./... && go test ./... &&
  make ascii` (plus `make generate-check` when `api/proto` changed, plus
  `make app-test && make app` when `macos/` changed). `make generate-check`
  only passes on a committed tree, so run it after the commit when a task's
  checkpoint calls for both. A schema change needs no data directory
  reset: the next run migrates `.storage` and `.storage/smoke` in place.
- Swift follows the Go rules: ASCII, comments say why, one-line doc comment
  per type, tests trace to a spec statement with whole-value assertions.
  Model types are built only through `init(wire:)`; identity fields and
  the timestamps the daemon always writes throw when absent, every other
  absent scalar takes its zero (proto3 drops empty strings and zero
  scalars from the JSON, so a controller-less pod has `workloadKind`
  `none` and no `workloadName`). Views never import `IdiosAPI`; stores own
  every client call and stream. The only hard-coded colours are the badge
  vocabulary in `BadgeStyle.swift`, the Grafana brand mark in
  `GrafanaMark.swift`, and the app icon in `hack/macos/icon.py`.
  After a proto change to a message the
  app decodes, `rm -rf macos/.build` before `make app-test`: the
  incremental build has crashed on a stale generated `Types.swift`. A new
  `Category` value is added to `macos/Sources/IdiosModel/Enums.swift` in
  the same commit that adds it to `common.proto`: `Incident(wire:)` requires
  it, and one row the application does not recognize fails the decoding of
  the whole response.
- Generated Swift client: operation names are capitalised (`GetIncident`);
  outputs are `.ok`, `.badRequest` and `.default(statusCode, body)`, there
  is no `.notFound` case (a 404 is `.default(404, Error)`); a JSON body is
  `body: .json(Components.Schemas.<Op>Request(...))` and a body that
  repeats the path id must carry the same id (the daemon binds the path
  last). Streams use `connection.streamClient`, calls `connection.client`.
  A cancelled call maps to `APIError.cancelled` and every store ignores it.
  Every time in the application is UTC, as stored.
- Write handlers: a typed error (`ValidationError`, `notFoundError`)
  returned from inside `Writer.Tx` is joined with the rollback result and
  reaches the client as a 500; set a flag inside the transaction and
  build the error after it returns. Notify after the commit, never inside.
- `idios mock` answers kube discovery with fixtures; a daemon built without
  `WithKube` answers 501. `Screenshot` photographs the key window when a
  sheet is up, so `addcluster` and `clusters` routes capture the sheet.
- The Xcode project is hand-written with a synchronized folder: a file
  under `macos/idios/` joins the target by existing. `macos/Package.resolved`
  is the one pin file (the workspace copy is ignored). `xcodebuild` needs
  `-skipPackagePluginValidation`; `make app` passes it.
- Never `.fixedSize(horizontal: false, vertical: true)` on a concatenated
  multi-font `Text` in a header: it overflows the window. `lineLimit(nil)`
  wraps. SwiftUI `Table` on macOS 15 clips columns at their ideal widths
  instead of shrinking them; prose tables are custom rows.
- A bare-letter `keyboardShortcut` fires even while a `TextField` has
  focus; single-letter keys go through `.onKeyPress` on the focused view.
  `ForEach` silently repeats one element's content when two elements share
  an `Identifiable` id; a kind-none workload row keys on its pod uid for
  that reason.

## Development environment

- `data_dir` defaults to the OS user data directory; every command in this
  repository passes `-data-dir .storage` (gitignored) and
  `-kubeconfig ./kube/config`; `make run` and `make smoke` do so.
- Test cluster: OrbStack Kubernetes (server 1.35) via
  `KUBECONFIG=./kube/config`, namespace `idios-smoke` only. Never read
  `~/.kube/config`; never touch other namespaces. `orb start` if the API
  refuses connections. `kind` is not installed and not needed.
- Real data for the application: `make smoke` fills `.storage/smoke`
  (its pods are deleted at the end, so the incidents close as
  `pod_deleted`; `kubectl -n idios-smoke apply -f hack/smoke/` reopens
  them). Serve it with
  `./bin/idios -data-dir .storage/smoke -kubeconfig ./kube/config run`.
  Fixture data: `./bin/idios -data-dir .storage mock`. Both listen on
  `127.0.0.1:7770`; never run two daemons on one port. When the installed
  application's daemon holds 7770, `make run PORT=7771` and
  `make smoke PORT=7771` move the dev daemon (the global `-listen` flag
  overrides `api_listen`), and `IDIOS_DAEMON=127.0.0.1:7771` points the
  application and `hack/macos/screenshot.sh` at it.
- Application screenshots: `hack/macos/screenshot.sh <route> <png>`
  builds the app, opens the route (`incidents`, `incidents/<state>`,
  `incident/<id>`, `timeline/<id>`, `pod/<uid>[/<tab>]`, `run/<job uid>`,
  `workloads`, `workload/<cluster>/<ns>/<kind>/<name>`, `status`, `menubar`,
  `addcluster`, `clusters`; `incident/<id>`, `timeline/<id>` and
  `pod/<uid>[/<tab>]` all open the pod page, on the incident's container, its
  Timeline tab, or the Pod card; a job-subject incident opens the run page)
  and captures the window with `screencapture`, which
  needs Screen Recording permission for the terminal. `IDIOS_DAEMON=host:port`
  points the app elsewhere; `--allow-disconnected` skips the daemon check.
  `--explain` opens the route's help marks before the capture, and the run
  fails when a screen's regions and its explanations disagree.
