# idios Implementation Roadmap: milestone 2, presentation

Status: complete 2026-08-28. Frozen.

Goal: a person opens a macOS application and reads everything the recorder
kept, through an API inside the daemon, with nothing lost on the way. The
spec is `docs/design/presentation.md`; it argues from `data-storage.md` and
`process-architecture.md`, and the mockup `docs/mockups/idios-ui.html` is
the visual reference where the spec does not decide.

## Cross-cutting decisions this milestone adds

Every phase plan repeats the ones it touches in its Global Constraints.
The m1 decisions (single writer, `clock.Format`, categories on reason,
ASCII, TDD per task, test cluster) stay in force and are in `CLAUDE.md`.

1. **Go 1.26.** The sebuf runtime requires it. Phase 8 raises the `go`
   directive and, in the same task, the pins that were held back by 1.24
   (`modernc.org/sqlite`, `k8s.io/*`), then runs the whole suite and
   `make smoke` before anything else changes.
2. **The contract is proto, compiled by sebuf.** Source in
   `api/proto/idios/v1/`; generated Go in `internal/apigen/idiosv1`;
   generated OpenAPI in `api/openapi/`, committed, because the Swift build
   reads it. Generated files are never edited; `make generate` rebuilds
   them and CI fails if the tree differs afterwards.
3. **Wire rules** are `presentation.md` Section 3.1, verbatim: int64 as
   string, int32 for small counts, enums with `enum_value`, raw Kubernetes
   values as strings, timestamps as the stored strings, proto3 `optional`
   without `nullable`, one repeated field per list response.
4. **Vocabulary.** Endpoints are a method and a path. Plans, code and
   comments say endpoint, handler, client operation; never RPC.
5. **The application never opens the database, artifact files or the
   kubeconfig.** A Swift file importing SQLite or reading `~/.kube` fails
   review.
6. **macOS 15 minimum**, built with the current SDK (the app takes the
   system look of the machine it runs on; nothing in the spec needs a
   macOS 26 API, and the deployment target can be raised later in one
   setting), Swift 6 language mode, not
   sandboxed, signed to run locally. The Xcode project lives in `macos/`
   and builds from the command line with `xcodebuild`; no step needs the
   Xcode GUI.
7. **Swift follows the same rules as Go**: ASCII only, comments say why,
   every test traces to a spec statement, exact assertions, no reference
   to any document from code.
8. **Fixtures cross the boundary once.** The Go wire-contract test writes
   `api/testdata/*.json`; the Swift model tests read those files. Neither
   side hand-writes JSON.

## Phases

```
Phase 8   contract, read models, API      first; needs the Go 1.26 bump
Phase 9   macOS application, read-only    after 8 (can start against the mock server
                                          as soon as 8's protos exist)
Phase 10  writes and cluster onboarding   after 8 and 9
```

### Phase 8: contract, read models, API (plan: `08-contract-and-api.md`)

Status: complete 2026-08-28. Handoff notes at the end of the plan.

Delivers: the Go 1.26 upgrade; `api/proto/idios/v1/` covering every
endpoint in `presentation.md` Section 4 and the two streams in Section 5;
`make generate`; `internal/query` with one function per row set and table
tests on a seeded temp SQLite built from the ingest fixtures;
`internal/api` implementing the generated handler interface, the
row-to-message mapping, the artifact content endpoint and both SSE
streams; `internal/notify`, the in-process broadcast the producers call
after a commit; `api_listen` in `config`; the listener started as a
component of `idios run`; `idios status` calling the API when the daemon
is up and falling back to the database when it is not (`cluster add` and
`ns add` follow in Phase 10 with their endpoints); `idios mock` serving
the wire fixtures; the wire-contract test writing `api/testdata/*.json`;
the `archtest` rules for `query`, `api` and `notify`.

Order inside the phase: upgrade first; protos and generation second, so
Phase 9 can begin against the mock server; then query functions, handlers
and wiring endpoint by endpoint, incidents first.

Depends on: nothing new. Uses `store.Reader`, `store/status_sql.go`,
`status.Counters`, `k8s.Watcher.Ready()`, `k8s.Skew.Offset()`,
`capture.Pool.Dropped()` as they exist.

### Phase 9: macOS application, read-only (plan: `09-macos-app.md`)

Status: complete 2026-08-28. Handoff notes at the end of the plan.

Delivers: the Xcode project in `macos/` with the four layers of
`presentation.md` Section 9.2; the Swift client regenerated from
`api/openapi/` by the build plugin; the model layer with decoding tests
against `api/testdata/*.json`; the screens of Section 9.3 in the order
incidents list, incident detail, pod, timeline, workloads and jobs,
status, menu bar extra, column browser; the cluster scope checklist and
its persistence; the four text treatments as reusable views; the log pane
on `NSTextView`; the prose tables as `List`s; the not-connected state;
the disabled human-action controls with their explanation.

Verification is a person running the daemon against the smoke namespace
and comparing each screen with the mockup and the spec; `swift test` and
`xcodebuild` are the automated gate. Each screen is one task and is
reviewed from a screenshot before the next begins.

Depends on: Phase 8's protos for the mock server; Phase 8's API for real
data.

### Phase 10: writes and cluster onboarding (plan: `10-writes.md`)

Status: complete 2026-08-28. Handoff notes at the end of the plan.

Delivers: the twelve endpoints of `presentation.md` Section 8 through
`store.Writer.Tx`, each idempotent and emitting on the incident or cluster
stream; the store helpers they need (`AcknowledgeIncident`,
`SetIncidentNote`, `DeleteIncident` with file removal, `RenameCluster`,
`RemoveCluster` with directory removal, `RemoveWatchedNamespace`); the
application's action buttons enabled, with confirmation on the two
destructive ones; the add-cluster flow (contexts from the daemon, friendly
name, namespaces from the cluster with a free-text fallback); tests for
every write against a seeded store asserting the exact rows and files
after the call and after calling it twice.

Depends on: Phases 8 and 9.

## Model choice per phase

Execution follows the m1 pattern: an orchestrator holds the plan, a fresh
implementer subagent runs each task, a reviewer subagent checks the diff
against the plan and the `.ai` rules before the next task. The
orchestrator pastes the `.ai/*.md` paths into every subagent prompt.

| Phase | Orchestrator | Implementer | Reviewer | Why |
|---|---|---|---|---|
| 8 | Fable | Opus | Opus | SQL read models with exact result sets and a generated contract; correctness depends on reading the schema and the spec tables exactly. |
| 9 | Fable | Opus | Fable | SwiftUI on macOS has version-specific behaviour and cannot be seen by the implementer; judgement about what to build natively versus custom matters. |
| 10 | Fable | Sonnet | Opus | Writes are small transactions over a fixed schema with a fixed list; the risk is in review, not in reasoning. |

Plan writing for every phase: Fable, inline, after reading the design
docs and the code that exists at that moment.

## Facts a phase plan needs that are not in the design docs

- The sebuf checkout used for the contract spike is at
  `~/Documents/dev/github/sebuf`; the module is
  `github.com/SebastienMelki/sebuf`. Plugins are installed with
  `go install github.com/SebastienMelki/sebuf/cmd/protoc-gen-{go-http,go-client,openapiv3}@latest`
  and `protoc-gen-go`; `buf` 1.52 and `protoc` 29 are on the machine.
- sebuf allows one JSON-marshaling feature per message (`enum_value`,
  `nullable`, `int64_encoding = NUMBER`, `unwrap`). The wire rules use
  only `enum_value`, so this never bites unless a plan adds another.
- Apple's `swift-openapi-generator` 1.13 consumed sebuf's OpenAPI 3.1
  with no edits; `swift-openapi-runtime` and `swift-openapi-urlsession`
  are the two runtime packages. Swift 6.2 is on the machine.
- The spike that established the wire rules lives outside the repository
  and is not kept; Phase 8's wire-contract test replaces it.
- A `Table` in SwiftUI on macOS has one row height per table; prose rows
  are `List`s. The log pane is `NSTextView` via `NSViewRepresentable`.
- Found while a person used the Phase 9 application and closed in Phase
  10: the controller-less workload has no detail endpoint (the design doc
  says so), the SSE handlers end on shutdown, `hack/smoke/run.sh` aborts
  when `idios run` exits early, and the cluster checkbox is disabled with
  one cluster.

## Plan files

- `docs/plans/m2-presentation/roadmap.md` (this file)
- `docs/plans/m2-presentation/08-contract-and-api.md` (Phase 8, written when it starts)
- `docs/plans/m2-presentation/09-macos-app.md` (Phase 9)
- `docs/plans/m2-presentation/10-writes.md` (Phase 10)
