# m9 step B - daemon: siblings and the tool surface

Goal: after this plan `GET /v1/pods/{uid}` carries a pod's siblings the
way the pod page's rail draws them: the other pods of the same
controller in the same cluster, each with its state and the worst
category among its open incidents, pods with an open incident first
(worst category first), then live pods, then deleted ones, newest
created first inside each group, five at most, beside a total that
counts every pod of the controller idios has seen; `GET
/v1/incidents/{id}` stops carrying siblings; the MCP `get_incident`
description stops promising them and `get_pod` gains them; the `idios
mock` fixtures serve a pod with more siblings than the page shows. The
application keeps building: its models follow the wire, and the
incident detail's Siblings card, which m9 step C deletes with the
screen, goes now because the field it read is gone.

Architecture: the read model lives in `internal/query/pods.go`
(`SiblingPod` moves there from `detail.go`, gains `WorstOpenCategory`,
and `PodDetail` gains `Siblings` and `SiblingTotal`); the category order
worst first becomes one SQL expression in `internal/query/query.go`,
because the sibling order and the sibling badge both rank by it. The
proto change is two files: `SiblingPod` in `incidents.proto` gains
`worst_open_category`, `IncidentDetail` loses `siblings` (field 4
reserved), `PodDetail` in `pods.proto` gains `siblings` and
`sibling_total`. `internal/api/pods.go` maps them; the wire fixtures
under `api/testdata` are rewritten by the wire-contract test. The Swift
models in `macos/Sources/IdiosModel` follow: `SiblingPod` moves into
`Pod.swift`, `PodDetail` carries the two new fields, `IncidentDetail`
drops `siblings`, and `SiblingsCard` leaves the incident detail. The
MCP descriptions and the prompt's investigation order change last.

Tech stack: Go 1.26, `modernc.org/sqlite`, sebuf (`make generate`,
`make generate-check` on the committed tree), Swift 6 package
`IdiosModel` with tests in `macos/Tests/IdiosModelTests` (`make
app-test`), the Xcode application (`make app`). After the proto change
`rm -rf macos/.build` before `make app-test`: the incremental build has
crashed on a stale generated `Types.swift`.

Spec: `docs/design/presentation.md` section 6 (the endpoint table) rows
`GET /v1/incidents/{id}` and `GET /v1/pods/{uid}`, and the "Sibling
counts" sentence of section 8, as amended by task 2 of this plan;
section 3 (the MCP surface). Roadmap decisions 6, 7 and 10 of
`docs/plans/m9-pod-page/roadmap.md`.

Global constraints: `.ai/ascii-only.md`, `.ai/tests.md`,
`.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`,
`.ai/commits.md`. Read-model SQL lives in `internal/query`; every
exported identifier carries a one-line doc comment; gofmt turns two
straight quotes in a doc comment into a curly quote, so write "empty".
Generated code under `internal/apigen` and `api/openapi` is never
edited. Swift follows the Go rules; model types are built only through
`init(wire:)`, identity fields throw when absent, every other absent
scalar takes its zero. Nothing in a test, comment, doc or fixture names
a real organisation, cluster, namespace, workload, image or node; the
invented names are the ones the existing tests use (`web`,
`checkout-api`, `idios-smoke`, `registry.example.com`, `node-a`).
Checkpoint before every commit: `go build ./... && go test ./... &&
make ascii && make app-test && make app`, plus `make generate-check`
after the commit of task 2. Implementers never commit; nothing is
committed without the user's review of the diff.

## File structure

    internal/query/query.go                      categoryRank(col) (task 1)
    internal/query/pods.go                       SiblingPod with WorstOpenCategory, PodDetail.Siblings and SiblingTotal, podSiblings, siblingTotal (task 1)
    internal/query/pods_test.go                  the sibling order, cap and total; GetPod's existing cases carry siblings (task 1)
    internal/query/detail.go                     SiblingPod and siblings leave with IncidentDetail.Siblings (task 1 moves the type, task 2 deletes the rest)
    internal/query/detail_test.go                the sibling cases leave (task 2)
    api/proto/idios/v1/incidents.proto           SiblingPod.worst_open_category; IncidentDetail.siblings deleted, 4 reserved (task 2)
    api/proto/idios/v1/pods.proto                PodDetail.siblings, PodDetail.sibling_total (task 2)
    internal/apigen, api/openapi                 make generate (task 2)
    internal/api/pods.go                         siblingPod, podDetail maps the two fields (task 2)
    internal/api/detail.go                       siblingPod and the Siblings mapping leave (task 2)
    internal/api/pods_test.go, detail_test.go    expectations (task 2)
    internal/api/wire_test.go                    pod_detail carries five siblings and a total of eight, incident_detail none (task 2)
    api/testdata/pod_detail.json, incident_detail.json, incident_detail_job.json   rewritten by the wire-contract test (task 2)
    macos/Sources/IdiosModel/Pod.swift           SiblingPod with worstOpenCategory, PodDetail.siblings and siblingTotal (task 2)
    macos/Sources/IdiosModel/Incident.swift      SiblingPod and IncidentDetail.siblings leave (task 2)
    macos/Tests/IdiosModelTests/PodTests.swift   the pod detail fixture's siblings (task 2)
    macos/idios/Views/IncidentDetail/KubeletCard.swift, IncidentDetailScreen.swift   SiblingsCard leaves (task 2)
    docs/design/presentation.md                  the two endpoint rows and the sibling-count sentence (task 2)
    internal/mcp/tools.go, mcp.go                the get_incident and get_pod descriptions, the server instructions (task 3)
    internal/prompt/prompt.go                    the MCP prompt's investigation order (task 3)
    internal/api/prompt_test.go                  the expected order text (task 3)

## Task 1 - decision 6: the read model

The category order worst first is the application's `Category.ranked`
(`macos/Sources/IdiosModel/Fold.swift`): crash, oom, unclean_exit,
image_pull, config, probe, scheduling, stuck, node_pressure,
rescheduled, job_failed, then anything else. The daemon needs the same
order to pick a sibling's worst open category and to sort siblings, so
it becomes one expression in `internal/query/query.go`:

    // categoryRank orders the categories worst first, the order the
    // application's sidebar lists them; a category outside the list sorts
    // behind every one in it. col is the column or alias holding the
    // category.
    func categoryRank(col string) string {
    	return "CASE " + col + " WHEN 'crash' THEN 0 WHEN 'oom' THEN 1 WHEN 'unclean_exit' THEN 2" +
    		" WHEN 'image_pull' THEN 3 WHEN 'config' THEN 4 WHEN 'probe' THEN 5 WHEN 'scheduling' THEN 6" +
    		" WHEN 'stuck' THEN 7 WHEN 'node_pressure' THEN 8 WHEN 'rescheduled' THEN 9 WHEN 'job_failed' THEN 10" +
    		" ELSE 11 END"
    }

`SiblingPod` moves from `internal/query/detail.go` to
`internal/query/pods.go` and gains the badge's field; `PodDetail` gains
the list and the total:

    // SiblingPod is one other pod of the same controller, as the pod page's
    // rail lists it. WorstOpenCategory is empty when no incident of the pod
    // is open.
    type SiblingPod struct {
    	UID               string
    	Name              string
    	Phase             string
    	DeletedAt         *string
    	RestartCount      int64
    	Ready             bool
    	WorstOpenCategory string
    }

    type PodDetail struct {
    	Pod        PodRow
    	Containers []store.Container
    	Conditions []store.PodCondition
    	Incidents  []IncidentRow
    	Artifacts  []store.Artifact
    	// Siblings are the controller's other pods in this cluster, five at
    	// most; SiblingTotal counts every pod of the controller, this one
    	// included, because the rail's label names the set the pod belongs to.
    	Siblings     []SiblingPod
    	SiblingTotal int64
    	ContainerGrafanaURLs map[string]string
    }

`detail.go` keeps its `siblings` function and its scanner, renamed
`scanDetailSibling` so the name does not clash with the one below, until
task 2 deletes them with `IncidentDetail.Siblings`; in this task it
still scans six columns into the moved type, and the detail tests'
expected `SiblingPod` values keep an empty `WorstOpenCategory`, so they
are untouched.

The queries, in `pods.go`. An open incident is one that is neither
closed nor dismissed, the predicate `podSelect` already counts with;
readiness ranks only the containers that serve traffic, as the moved
function did. The rank is computed twice, once inside the subquery to
pick the worst open category and once on the alias to sort the pods:

    // siblingLimit is how many siblings the pod page's rail shows; the
    // total says how many more there are.
    const siblingLimit = 5

    func scanSibling(rows *sql.Rows) (SiblingPod, error) {
    	var s SiblingPod
    	var ready int64
    	err := rows.Scan(&s.UID, &s.Name, &s.Phase, &s.DeletedAt, &s.RestartCount, &ready, &s.WorstOpenCategory)
    	s.Ready = ready == 1
    	return s, err
    }

    // podSiblings returns the controller's other pods in the same cluster,
    // pods with an open incident first, worst category first, then live pods,
    // then deleted ones, newest created first inside each group, at most
    // siblingLimit of them. Two clusters can hold the same controller uid,
    // since a uid is only unique within one; an empty controller uid means
    // the pod has no controller, which makes it nobody's sibling. An
    // incident dismissed while open is not the person's problem any more, so
    // it does not colour the sibling.
    func podSiblings(ctx context.Context, db store.Querier, clusterID int64, controllerUID, podUID string) ([]SiblingPod, error) {
    	if controllerUID == "" {
    		return nil, nil
    	}
    	return collect(ctx, db, scanSibling, `
    SELECT p.uid, p.name, p.phase, p.deleted_at,
           COALESCE((SELECT SUM(c.restart_count) FROM containers c WHERE c.pod_uid = p.uid), 0),
           (SELECT COUNT(*) FROM containers c WHERE c.pod_uid = p.uid AND c.kind IN ('app', 'sidecar')) > 0
           AND NOT EXISTS (SELECT 1 FROM containers c WHERE c.pod_uid = p.uid AND c.kind IN ('app', 'sidecar') AND c.ready = 0),
           COALESCE((SELECT i.category FROM incidents i
                      WHERE i.pod_uid = p.uid AND i.closed_at IS NULL AND i.dismissed_at IS NULL
                      ORDER BY `+categoryRank("i.category")+`, i.id LIMIT 1), '') AS worst
    FROM pods p
    WHERE p.cluster_id = ? AND p.controller_uid = ? AND p.uid <> ?
    ORDER BY CASE WHEN worst <> '' THEN 0 WHEN p.deleted_at IS NULL THEN 1 ELSE 2 END,
             `+categoryRank("worst")+`, p.created_at DESC, p.uid
    LIMIT `+strconv.Itoa(siblingLimit), clusterID, controllerUID, podUID)
    }

    // siblingTotal counts every pod of the controller in the cluster, the
    // pod itself included; zero for a pod with no controller.
    func siblingTotal(ctx context.Context, db store.Querier, clusterID int64, controllerUID string) (int64, error) {
    	if controllerUID == "" {
    		return 0, nil
    	}
    	var n int64
    	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pods WHERE cluster_id = ? AND controller_uid = ?`,
    		clusterID, controllerUID).Scan(&n)
    	return n, err
    }

`GetPod` fills both after the artifacts and before `attachPodGrafana`:

    	if d.Siblings, err = podSiblings(ctx, db, d.Pod.ClusterID, d.Pod.ControllerUID, uid); err != nil {
    		return nil, err
    	}
    	if d.SiblingTotal, err = siblingTotal(ctx, db, d.Pod.ClusterID, d.Pod.ControllerUID); err != nil {
    		return nil, err
    	}

If `store.Querier` lacks `QueryRowContext`, use `collect` with a
one-column scanner instead and say so in the report; do not widen the
interface.

Tests first, `internal/query/pods_test.go`. The seed's only controller
with two pods in one cluster is `rs-web-1` (`pod-crash` live with open
incidents 1 and 12, `pod-jump` deleted with a closed one), so the
existing case "captured files, by container then kind then instance" of
`TestGetPodReturnsContainersOfEveryKind` gains

    			Siblings: []SiblingPod{{UID: querytest.JumpPodUID, Name: "web-7d9f8c6b5-jump1", Phase: "Running",
    				DeletedAt: sp(deleted), RestartCount: 4, Ready: true}},
    			SiblingTotal: 2,

with `deleted` the package-level constant `incidents_test.go` already
holds. The other
cases of that test are pods whose fixture carries no controller, so
they gain nothing: the zero values are the assertion that a
controller-less pod has no siblings and no total. Confirm by running the
test; if a fixture pod turns out to carry a `controller_uid`, add its
siblings and total to that case rather than weakening the assertion.

The order, the cap and the total need pods the seed does not have, so
one new table test builds them, `TestGetPodSiblingsAreOrderedCappedAndCounted`,
tracing to the `GET /v1/pods/{uid}` row of the endpoint table as task 2
amends it. Setup, once, through `st.Writer.Tx`: pods under a fresh
controller `rs-sib` in `querytest.ClusterProd`, namespace `seedNS`,
`ControllerKind: "ReplicaSet"`, `ControllerName: "sib-5c88f"`,
`WorkloadKind: "Deployment"`, `WorkloadName: "sib"`, `Phase:
"Running"`, `FirstSeenAt` and `LastSeenAt` `"2026-08-27T12:00:00.000000Z"`,
built by a local helper `sibPod(uid, name, createdAt string, deletedAt
*string) store.Pod`; an `open(tx, podUID, category string) int64`
helper calling `store.OpenIncident` with `ClusterID:
querytest.ClusterProd, Namespace: seedNS, SubjectKind: store.SubjectPod,
PodUID: &podUID, ContainerName: "api", WorkloadKind: "Deployment",
WorkloadName: "sib", Category: category, FirstReason: "Error",
LastReason: "Error", Occurrences: 1, OpenedAt: t0, LastSeenAt: t0` where
`t0 = "2026-08-27T12:00:00.000000Z"`. The pods, with `created_at`:

| uid | name | created | state | incidents |
|---|---|---|---|---|
| `pod-sib-self` | `sib-5c88f-self1` | 11:35 | live | none; the pod under test |
| `pod-sib-crash-old` | `sib-5c88f-crold` | 11:40 | live | open `crash`, plus a closed `oom` (`store.CloseIncident(ctx, tx, id, store.CloseRecovered, t0)`; if the constant is named differently, use the recovered close reason the store exports) |
| `pod-sib-crash-new` | `sib-5c88f-crnew` | 11:45 | live | open `crash` |
| `pod-sib-probe` | `sib-5c88f-probe` | 11:50 | live | open `probe` |
| `pod-sib-dismissed` | `sib-5c88f-dism1` | 11:57 | live | a `crash` opened then `store.DismissIncident(ctx, tx, id, t0)` |
| `pod-sib-live-new` | `sib-5c88f-lvnew` | 11:55 | live | none |
| `pod-sib-live-old` | `sib-5c88f-lvold` | 11:30 | live | none |
| `pod-sib-gone` | `sib-5c88f-gone1` | 11:20 | `DeletedAt: sp("2026-08-27T11:59:00.000000Z")`, `Phase: "Failed"` | none |
| `pod-sib-away` | `sib-5c88f-away1` | 11:58 | live, in `querytest.ClusterStaging` | none |
| `pod-sib-lone` | `lone` | 11:36 | live, `ControllerUID: ""`, `ControllerKind: ""`, `WorkloadKind: "none"` | none |

Rows of the table, whole `(Siblings, SiblingTotal)` assertions through
`cmp.Diff` on a small struct `got{Siblings []SiblingPod; Total int64}`
built from `GetPod`'s result, edge cases first:

- `pod-sib-lone`: `nil` siblings, total 0; a pod with no controller
  belongs to no set;
- `pod-sib-self`: open incidents first, worst first and newest created
  first inside a category (`crash-new` then `crash-old`, both
  `WorstOpenCategory: "crash"`, then `probe`), then live pods newest
  first (`dismissed`, whose open incident is dismissed and so does not
  colour it, then `live-new`), five in all; `live-old` and `gone` fall
  past the cap, the staging pod is never in the set; total 8 (the eight
  prod pods of `rs-sib`, the pod itself among them);
- `pod-sib-gone`: the deleted pod's own siblings are the same five in
  the same order, because the order is about the siblings and not the
  pod; total 8;
- a second table test is not needed for the tie inside a category: it
  is the `crash-new` before `crash-old` row above.

Every expected `SiblingPod` is spelled whole: `UID`, `Name`, `Phase:
"Running"`, `DeletedAt: nil`, `RestartCount: 0`, `Ready: false` (the
pods have no containers), `WorstOpenCategory` as the row says.

Run `go test ./internal/query -run 'TestGetPod' -v`: the new test fails
on the missing fields, the existing case on the missing siblings.
Implement, run again, then the checkpoint. `make app-test` and `make
app` are unchanged by this task but run in the checkpoint as always.

Checkpoint: `go build ./... && go test ./... && make ascii && make
app-test && make app`. Commit: `query: order and cap a pod's siblings`.

Consumes: `PodDetail`, `GetPod`, `collect`, `podSelect`'s open-incident
predicate, `store.Querier`; `SiblingPod` as `detail.go` had it.
Produces: `query.SiblingPod` (UID, Name, Phase, DeletedAt, RestartCount,
Ready, WorstOpenCategory) in `pods.go`; `query.PodDetail.Siblings
[]SiblingPod` and `query.PodDetail.SiblingTotal int64`;
`categoryRank(col string) string`; `siblingLimit`.

## Task 2 - the wire: siblings move from the incident to the pod

Spec first, `docs/design/presentation.md`. In the endpoint table, the
`GET /v1/incidents/{id}` row loses the clause "sibling pods (`same
controller_uid`, live or deleted, with state and restart count);" and
its last sentence ends "so the pod snapshot, its containers, its files
and its events all ride along." (the siblings clause dropped); its
tables column loses the first `pods WHERE controller_uid` (the one
before `jobs WHERE uid`). The `GET /v1/pods/{uid}` row becomes:

    | `GET /v1/pods/{uid}` | The pod row, its containers (all kinds, with the `ready` meaning implied by `kind`), latest condition per type, incidents on the pod (open and closed), artifacts grouped by container, and the pod's siblings: the other pods of the same `controller_uid` in the same cluster, live or deleted, each with its phase, readiness, restart count and the worst category among its open incidents (open meaning neither closed nor dismissed; worst in the order the sidebar lists categories: crash, oom, unclean_exit, image_pull, config, probe, scheduling, stuck, node_pressure, rescheduled, job_failed, then other), ordered pods with an open incident first (worst category first), then live pods, then deleted ones, newest created first inside each group, five at most; `sibling_total` counts every pod of the controller idios has seen in that cluster, this one included, so the rail can say "N pods of this ReplicaSet" and how many more than it shows. A pod with no controller has no siblings and a total of zero. | `pods`, `containers`, `pod_condition_history`, `incidents`, `artifacts`, `pods WHERE controller_uid`. |

In section 8 the sentence "Sibling counts say "pods idios has seen",
not "of N replicas", because desired replicas are not stored." gains
one more: "The label names the set it counts (this ReplicaSet, this
StatefulSet, this Job), because a Deployment's pods span ReplicaSets and
the Workloads screen's list of them may be longer."

Proto. `api/proto/idios/v1/incidents.proto`, `SiblingPod`:

      // The worst category among the sibling's open incidents; absent when
      // none is open.
      optional Category worst_open_category = 7;

`IncidentDetail`: the line `repeated SiblingPod siblings = 4;` becomes
`reserved 4;`, so a stale client never reads another field as siblings.
`api/proto/idios/v1/pods.proto`, `PodDetail`:

      // The other pods of the same controller in this cluster, at most five,
      // ordered as the pod page's rail shows them.
      repeated SiblingPod siblings = 6;
      // Every pod of the controller in this cluster, this one included.
      int32 sibling_total = 7;

Run `make generate`. `Category` comes from `common.proto`, which
`incidents.proto` already imports; `pods.proto` already imports
`incidents.proto`.

Daemon. `internal/query/detail.go`: delete `IncidentDetail.Siblings`,
the `siblings` call in `GetIncident` (and in the job branch if it calls
it), the `siblings` function and the old `scanSibling` (task 1 left the
new one in `pods.go`). `internal/query/detail_test.go`: delete
`crashSiblings`, the `Siblings:` lines of every expected detail,
`TestGetIncidentSiblingsExcludeSelf` whole (its four behaviours are
rows of task 1's test: self excluded, live and deleted, other cluster,
no controller). `internal/api/detail.go`:
delete the `Siblings:` mapping and the `siblingPod` function.
`internal/api/pods.go` gains the mapper and fills the detail:

    // siblingPod maps one other pod of the same controller; the badge is
    // absent when nothing is open on it.
    func siblingPod(p query.SiblingPod) *idiosv1.SiblingPod {
    	s := &idiosv1.SiblingPod{
    		Uid:          p.UID,
    		Name:         p.Name,
    		Phase:        p.Phase,
    		DeletedAt:    p.DeletedAt,
    		RestartCount: int32(p.RestartCount),
    		Ready:        p.Ready,
    	}
    	if c, ok := categories[p.WorstOpenCategory]; ok {
    		s.WorstOpenCategory = &c
    	}
    	return s
    }

and in `podDetail`: `Siblings: mapAll(d.Siblings, siblingPod),
SiblingTotal: int32(d.SiblingTotal),`. `categories` is the string-to-enum
map `enums.go` already holds; an empty string is not a key of it, so the
badge stays absent.

Tests, daemon side, traced to the two amended rows. `internal/api/
detail_test.go`: delete `crashSiblingsWire` and every `Siblings:` line
(the container case and the job case around line 154), and drop "the
sibling pods," from the comment above `TestGetIncidentReturnsTheMappedDetail`
and "siblings," from the case name. `internal/api/pods_test.go`,
`TestGetPodReturnsTheMappedDetail`'s `want` gains

    		Siblings: []*idiosv1.SiblingPod{{
    			Uid: querytest.JumpPodUID, Name: "web-7d9f8c6b5-jump1", Phase: "Running",
    			DeletedAt: sp(deleted), RestartCount: 4, Ready: true,
    		}},
    		SiblingTotal: 2,

with `deleted` the constant the api tests already hold.
`internal/api/wire_test.go`:
`TestWireContractIncidentDetail`'s `detail` loses its `Siblings:` field;
`TestWireContractPods`' `detail` gains, after `Artifacts`,

    		Siblings: []*idiosv1.SiblingPod{
    			{Uid: "pod-sib-1", Name: "checkout-api-7d9f8b6c4-k9tld", Phase: "Running", RestartCount: 3, Ready: false,
    				WorstOpenCategory: ep(idiosv1.Category_CATEGORY_IMAGE_PULL)},
    			{Uid: "pod-sib-2", Name: "checkout-api-7d9f8b6c4-p8cg4", Phase: "Running", Ready: true},
    			{Uid: "pod-sib-3", Name: "checkout-api-7d9f8b6c4-ngq6w", Phase: "Running", Ready: true},
    			{Uid: "pod-sib-4", Name: "checkout-api-7d9f8b6c4-tmf48", Phase: "Running", Ready: true},
    			{Uid: "pod-sib-5", Name: "checkout-api-7d9f8b6c4-b7t2m", Phase: "Failed",
    				DeletedAt: sp("2026-08-27T14:39:30.000000Z"), RestartCount: 2},
    		},
    		SiblingTotal: 8,

so the fixture holds every shape the rail draws (a badge, a ready pod,
a deleted one) and more pods than it shows. Run `go test ./internal/api
-run TestWireContract -update` to rewrite `api/testdata/pod_detail.json`,
`incident_detail.json` and `incident_detail_job.json`, then `go test
./internal/api ./internal/api/mock` without the flag. Look at the diff
of the three JSON files: `pod_detail.json` gains `siblings` and
`sibling_total`, the two detail fixtures lose `siblings`, nothing else
moves.

Application. `macos/Sources/IdiosModel/Incident.swift`: delete
`SiblingPod` and its extension, `IncidentDetail.siblings`, the
`siblings:` argument of `replacingIncident` and the line of
`init(wire:)` that read `wire.siblings`. `macos/Sources/IdiosModel/
Pod.swift` gains, before `PodDetail`:

    /// SiblingPod is another pod of the same controller that idios has seen,
    /// with the worst category among its open incidents when one is open.
    public struct SiblingPod: Hashable, Sendable {
        public let uid: String
        public let name: String
        public let phase: String?
        public let deletedAt: Timestamp?
        public let restartCount: Int32
        public let ready: Bool
        public let worstOpenCategory: Category?
    }

    extension SiblingPod {
        /// init builds a sibling from its wire row.
        public init(wire: Components.Schemas.SiblingPod) throws {
            self.uid = try require(wire.uid, "uid")
            self.name = try require(wire.name, "name")
            self.phase = wire.phase
            self.deletedAt = wire.deletedAt.map(Timestamp.init)
            self.restartCount = wire.restartCount ?? 0
            self.ready = wire.ready ?? false
            self.worstOpenCategory = modelEnum(wire.worstOpenCategory)
        }
    }

and `PodDetail` gains `public let siblings: [SiblingPod]` and `public
let siblingTotal: Int32`, filled by `try (wire.siblings ??
[]).map(SiblingPod.init(wire:))` and `wire.siblingTotal ?? 0`. If the
generated property is not spelled `worstOpenCategory` or
`siblingTotal`, read the name from `macos/.build/.../Types.swift` after
the build and use that spelling; do not guess. `macos/idios/Views/
IncidentDetail/KubeletCard.swift`: delete `SiblingsCard` whole (its doc
comment, struct, `row`, `label`, `style`). `IncidentDetailScreen.swift`:
the `HStack` that held `ResourceEnvelopeCard` and `SiblingsCard` becomes
`ResourceEnvelopeCard(container: subject(detail), incident:
detail.incident)` alone; if `openPod` then has no caller in the screen,
leave the parameter, because the inline Pod tab still passes it (line
228) and step C deletes the screen.

Tests, application side. `macos/Tests/IdiosModelTests/PodTests.swift`,
`podDetailCarriesThePodAndEveryPartOfIt` gains `siblings:` and
`siblingTotal: 8` in the expected `PodDetail`, the five siblings spelled
whole as the fixture carries them (`deletedAt: Timestamp("2026-08-27T14:39:30.000000Z")`
for the fifth, `worstOpenCategory: .imagePull` for the first, `nil` for
the rest, `phase` as a string, `restartCount` 3, 0, 0, 0, 2, `ready`
false, true, true, true, false), traced to the amended `GET
/v1/pods/{uid}` row: proto3 drops the zero and the false, so the
absent-scalar-takes-its-zero rule is exercised on the second sibling's
`restartCount` and the fifth's `ready`. If `IncidentTests.swift` builds
an `IncidentDetail` by hand, drop its `siblings:` argument.

Order of work: proto, `make generate`, `rm -rf macos/.build`, then the
Go tests (`go build ./...` fails until the api mapping is done), then
the Swift tests (`make app-test` fails on `wire.siblings` until
`Incident.swift` is edited), then `make app`.

Checkpoint: `go build ./... && go test ./... && make ascii && make
app-test && make app`. Commit: `api: move the siblings onto the pod
detail`, body: "The incident detail listed the pod's siblings and the
pod detail did not, so the pod page would have had to open an incident
to draw its rail. The list is capped at five with a total, carries the
worst open category, and is ordered the way the rail shows it." Then
`make generate-check` on the committed tree.

Consumes: task 1's `query.SiblingPod`, `query.PodDetail.Siblings`,
`query.PodDetail.SiblingTotal`; `categories` of `internal/api/enums.go`;
`mapAll`; `ep`, `sp` of the api tests; `modelEnum`, `require`,
`Timestamp` of `IdiosModel`.
Produces: `idiosv1.SiblingPod.WorstOpenCategory *Category`,
`idiosv1.PodDetail.Siblings`, `idiosv1.PodDetail.SiblingTotal int32`;
`IdiosModel.SiblingPod` (uid, name, phase, deletedAt, restartCount,
ready, worstOpenCategory) in `Pod.swift`; `IdiosModel.PodDetail.siblings
[SiblingPod]` and `.siblingTotal Int32`; the fixtures `pod_detail.json`
with five siblings and a total of eight.

## Task 3 - decision 7: the tool surface says what is true

`internal/mcp/tools.go`:

    		Name:        "get_incident",
    		Description: "Get one incident whole: the failing container's kubelet facts, the owner chain, the captured artifacts and the time-ordered timeline of what happened around it.",

    		Name:        "get_pod",
    		Description: "Get one pod whole: its containers, the latest reading of every condition, its entire Kubernetes event stream, its state history, and the other pods of its controller, five at most with their total.",

`internal/mcp/mcp.go`, the server instructions (lines 26-27): "get_pod
adds the pod's whole event stream and the pods of the same controller;".
Keep the sentence's shape; the instructions are prose an agent reads
once.

`internal/prompt/prompt.go`, `MCP`'s order, line 2:

    		"2. get_pod for the whole event stream, containers, conditions and the\n" +
    		"   pods of the same controller.\n" +

`internal/api/prompt_test.go` line 63 and the line after it change to
the same two lines; that test is the trace (the MCP prompt names the
tools the way the tools take them, section 3 of the presentation doc).
Nothing in `identity` changes: its comment says "the sibling lookups"
meaning the agent's, which now go through `get_pod`.

Checkpoint: `go build ./... && go test ./... && make ascii && make
app-test && make app`. Commit: `mcp: send the agent to get_pod for the
siblings`.

Consumes: nothing from tasks 1 and 2 by name; the wire of task 2 is
what makes the descriptions true.
Produces: nothing later work relies on by name.

## Hands to the next step

m9 step C reads `IdiosModel.SiblingPod` (with `worstOpenCategory`) and
`IdiosModel.PodDetail.siblings` and `.siblingTotal` from `Pod.swift`;
the rail's "this pod" row is the page's own pod, not one of the five,
and the "+ N more" count is `siblingTotal - 1 - siblings.count`, never
negative because the total counts the pod itself. The incident detail
no longer has a Siblings card; step C deletes the screen. `idios mock`'s
`pod_detail` fixture serves five siblings and a total of eight, so the
`pod/<uid>` screenshot route shows the "+ N more" line.

## Self-review

Spec coverage: decision 6 lands in task 1 (the order, the cap, the
total, the worst open category, the same-cluster rule, the no-controller
rule) and task 2 (the two proto files, `make generate`, the fixtures
with more than five siblings, `IncidentDetail.siblings` deleted); the
MCP half of decision 7 lands in task 3 (both descriptions, the prompt
line); decision 10 holds (`sib`, `rs-sib`, `web`, `checkout-api`,
`idios-smoke`). The spec statements land in task 2, the task whose
tests trace to them, and task 1's tests trace to the row task 2 writes;
the plan is read whole before task 1 starts, so the trace is to a
statement the same plan makes.

Rulings this plan makes inside the decisions: "open" for the badge and
the order means neither closed nor dismissed, the predicate the pod
list's open count already uses, so a sibling's dot and its row's count
never disagree; the total counts the pod itself, because the label the
rail draws ("8 pods of this ReplicaSet") names the set the pod is in;
the siblings exclude the pod itself, as the incident detail's did,
because the page already has the pod; a deleted pod's siblings are
ordered by the same rule (the order is a property of the siblings); the
category order worst first is stated once in the spec and mirrored in
`categoryRank` and `Category.ranked`, two lists with one source; the
Siblings card leaves the incident detail in this step rather than in C
because the field it read no longer exists.

`.ai` rules: ascii (checked per task); tests trace to the amended
endpoint row (task 1, task 2) and to section 3 (task 3); variants are
table rows; assertions are whole structs and whole row sets; no test of
the language (no test that `siblingPod` copies six fields); comments
carry the reason for the same-cluster rule, the dismissed rule, the
total including the pod, the reserved field, the twice-computed rank;
code-is-truth: no doc named from code, the presentation doc changes in
task 2, the tool descriptions say what the wire carries; scope: no
store change, no new endpoint, no rail in this step, no change to
`Category.ranked`; commits per task, none red, none without the user's
review.

Type consistency: `query.SiblingPod.WorstOpenCategory` is `string` in
tasks 1 and 2 and maps through `categories` to `*idiosv1.Category`;
`query.PodDetail.SiblingTotal` is `int64` in task 1 and narrows to
`int32` in task 2's mapper, matching `sibling_total`'s `int32`;
`siblingLimit` is 5 in task 1 and the fixture of task 2 carries five
siblings with a total of eight; `IdiosModel.SiblingPod.worstOpenCategory`
is `Category?` and `PodDetail.siblingTotal` is `Int32` in task 2 and in
the handoff.
