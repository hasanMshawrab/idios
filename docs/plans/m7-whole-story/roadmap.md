# m7 - The whole story

Status: complete 2026-09-02. All five steps shipped.

Goal: an AI agent investigating one incident over the MCP server gets the
whole story from the calls it already makes. The milestone comes from
watching an agent-run investigation of a real incident, then auditing the
resulting plan against the code. The gaps were not missing data but
missing projections: the run outcome, the captured pod object and the
sibling incidents were all in the store, each one extra round trip away
from the incident detail, while one tool was unusable because its own
schema documented a vocabulary the store does not use. The audit also
found that half the machinery already exists in the wrong layer - the
snapshot prompt already composes the pod-wide artifact list and the
related incidents inside `internal/api` - so m7 is as much a
consolidation as a build: those projections move down into the incident
read model, the api-layer copies are deleted, and every surface reads the
same rows. The only new collection in the milestone is the container's
terminated message. Everything lands in the daemon's read models and the
HTTP API first; the MCP server stays a proxy and the application picks
the new fields up where its screens already are.

## Decisions (made, do not relitigate)

1. No new MCP tools, and nothing unbounded added to existing responses.
   Every tool schema costs context in every agent session and every
   extra round trip costs a full response, so a new fact rides an
   existing call as a compact scalar or row. The tool count stays at
   seven; a tool schema that gains a field changes in the same step as
   the prompt text that names it, per the m4 rule that the prompt and
   the tool vocabulary never skew. Bounds are explicit: related
   incidents and workload runs use the daemon's default page limit; the
   artifact list is metadata rows, never content; the terminated message
   is the one addition that rides a list row, and it is capped (step C).
2. Projection before collection. The only new collection in this
   milestone is the container's terminated message. Deliberately out of
   scope, each deferred until the recorded incidents prove the need:
   Node, NodeClaim, NodePool and PodDisruptionBudget informers
   (cluster-scoped RBAC, or a question the eviction already answered); a
   CronJob informer with missed-schedule detection (a new failure class,
   its own milestone); `metrics.k8s.io` polling (against the
   watch-driven design; usage over time is what the Grafana links are
   for); a stored episode or causal-link model (grouping is a read-model
   question; the node name plus the time window make a storm visible
   without new state); a longer retention window for incident rows -
   `incidents.pod_uid` is `ON DELETE CASCADE` under a CHECK tying it to
   the subject kind, so a pod incident cannot outlive its pod without
   schema surgery that reaches the open-incident partial index, the
   workload tree's union key and the related-incident projections; that
   is a costed milestone of its own, not a sweep tweak; and an extra
   early-capture trigger on the `DisruptionTarget` condition - the same
   transition already opens an incident, which already captures every
   container's live log, and the delete-time fallback cache is already
   fed by the `Killing` event; nothing measurable is left for a third
   trigger to add.
3. Workload kinds keep the verbatim spelling the owner chain reports
   (`CronJob`, `Deployment`) everywhere: that is what ingest stores from
   the owner reference and what every incident row prints. The MCP
   server normalises its `kind` and `workload_kind` inputs
   case-insensitively to the canonical spellings before calling the
   daemon; an unknown string passes through verbatim, because the kind
   is open-ended - a CRD controller's kind is stored as reported, so the
   documented list names the common kinds and is not an enum. The
   daemon's exact comparison is unchanged. Enforcement is a named test:
   one table-driven test asserts that the normaliser's canonical
   spellings, the tool schema strings and the prompt's kind list agree
   (test code may import across the archtest boundaries that keep the
   packages themselves apart).
4. `node_name` is denormalised onto `incidents` at open, like `pod_name`
   and `image`, so the row answers alone without a join. It is filled
   from the pod row at open and stays empty where there is no node to
   name: a scheduling incident opens before placement and a job incident
   has no pod, so the new filter finds placed pods only.
5. A pod incident that carries a `job_uid` fills the detail's existing
   `job` field, the same `JobRow` a job incident already gets - the
   loader and the proto field exist, and the application already decodes
   it, so this is a read-model change and a view row. The run's
   condition is the single fact that separates "a retry that succeeded"
   from "a run that failed"; today it is stored and withheld from pod
   incidents.
6. The category rule is amended: a category may key on the reason and
   the event's `source_component`, still never on phase, exit code or
   message; the enforcing test, `CLAUDE.md` and
   `docs/design/data-storage.md` section 6.1 change together. The one
   use: event reason `Evicted` stays `node_pressure` when the component
   is the kubelet or empty (an empty component is an older event stream,
   where kubelet pressure eviction is the fallback meaning); a named
   non-kubelet component is an eviction-API disruption and categorises
   as `rescheduled`, so the eviction event attaches to the `rescheduled`
   incident instead of floating mislabeled. `PodReasonCategory`'s
   `Evicted` mapping is untouched: `pod.status.reason` is written only
   by the kubelet. Incident-level categorisation via the
   `DisruptionTarget` condition is already correct and does not change;
   no new category value is added, so the schema CHECK and the Swift
   `Category` enum are untouched.
7. `GetIncident` becomes the single owner of the pod-wide projections
   the api layer already built for the snapshot prompt:
   `query.ListPodArtifacts` and the related-incident lookup move into
   (or are called from) the detail read model, the snapshot builder
   reads the detail, and the api-layer copies in `internal/api` are
   deleted. The related list keeps its existing pod-OR-job scope - a job
   retry's sibling incident hangs off the job uid, not the pod - and the
   timeline's capture entries widen to the same artifact set, so the
   detail never lists an artifact its own timeline does not show. The
   snapshot prompt's content does not change: it already composed these
   rows; it now gets them from one place.
8. Nothing in fixtures, tests, docs or this plan names a real
   organisation, cluster, workload or node; examples are invented.

## Steps

### A. Vocabulary (daemon: prompt; mcp)

The kind normalisation of decision 3 in `internal/mcp`: `get_workload`'s
`kind` and `list_incidents`' `workload_kind` map case-insensitively to
the canonical spellings (an unknown string passes through and fails as
it does today, loudly for `get_workload`, emptily for the filter). The
two jsonschema strings change from the lowercase list to the canonical
one, and the generated MCP prompt in `internal/prompt` - daemon code,
served on the prompt endpoint - gains two lines: the kind spellings, and
that an absent scalar in any response is proto3 dropping a zero, so a
container with no resource fields has no requests, not missing data.
Ships with the agreement test of decision 3.

### B. The incident answers whole (daemon: store, query, api, proto)

The schema edit: `incidents.node_name TEXT`, filled at open from the pod
row (decision 4). Then the consolidation and the read-model work:

- `GetIncident` loads the `jobs` row for a pod incident carrying a
  `job_uid` (decision 5).
- The detail's artifact list becomes the pod's artifacts and the detail
  gains the related incidents, by moving the two existing api-layer
  projections down per decision 7; the timeline's capture entries widen
  with them; the api-layer copies are deleted in the same change.
- `IncidentRow` gains `node_name`; `ListIncidents` gains a `node_name`
  filter, which with the existing filters makes a disruption storm one
  query. The MCP `list_incidents` schema gains the filter and the
  prompt changes in this step: the investigation-order line that sends
  the agent back to `list_incidents` for siblings goes, since the
  detail now carries them.
- `WorkloadDetail` gains the recent runs of a CronJob or Job workload,
  bounded by the default page limit (`ListJobs` exists; `JobFilter`
  gains a `Name` for kind Job), so "is this recurring" is answered by
  the workload call the agent already makes.

Proto changes are confined to this step (`incidents.proto`: the row
field and the related-incidents field; `workloads.proto`: the
`repeated JobRow` field - the import already exists) with
`make generate`; the wire fixtures under `api/testdata` are regenerated
so `idios mock` serves a pod incident with a job row, related incidents,
node names and workload runs. Spec statements land in
`docs/design/data-storage.md` (column) and
`docs/design/presentation.md` (fields, filters, the detail's ownership
of the pod-wide projections) in the same step.

### C. Category and the terminated message (daemon: ingest, incident, store)

Two independent fixes:

- The `Evicted` split of decision 6 in `EventCategory`, now taking the
  source component; the rule test, `CLAUDE.md` and
  `docs/design/data-storage.md` section 6.1 amended together.
- The terminated message: `status.containerStatuses[].state.terminated
  .message` (the kubelet's reading of the termination log) lands on the
  container row, the state-history row and, when a termination opens or
  bumps an incident, on `last_message` - today it exists only inside
  the captured pod.json. It is capped at ingest (the cap and the
  columns are the phase plan's decision) because `last_message` rides
  every incident list row. It reaches agents as captured, like the
  pod.json it already lives in; sanitize's scope (env values, the
  last-applied annotation) is unchanged.

Wire fixtures are regenerated if a served row changes shape.

### D. Application (Swift)

View work on the incident detail: the run outcome line for a job-owned
pod incident ("run succeeded after retry" is the difference between
paging and not) - the model already decodes the detail's `job`, so this
is a row, not a model change - and the node name, for which
`Incident(wire:)` gains the optional field. `rm -rf macos/.build` after
the proto change, per `CLAUDE.md`.

### E. Verification loop (one fixture, then manual, the user drives)

`hack/smoke/` gains one fixture, `cronjob-retry`: a Job-owned pod that
fails once and succeeds on the retry (`backoffLimit: 2`), producing the
exact shape decision 5 exists for - a crash incident whose run
ultimately completed. Then the m4 rubric flow, rerun against this
milestone's claim: reproduce with `make smoke`, hand the MCP prompt to a
fresh agent session, and score the investigation on round trips as well
as answers - the incident detail alone must carry the run outcome, the
pod object, the related incidents and the node. Scored honestly within
the harness's reach: a single-node local cluster cannot produce an
eviction, so decision 6's split and the storm query are covered by the
unit tests and the mock fixtures, not the loop.

## Order and status

A is independent and can land first or in parallel; its prompt lines and
step B's prompt line touch different sentences. B then C (both edit
`0001_init.sql`), then D, then E. Checkpoint before every commit per
`CLAUDE.md`; `make generate-check` after the step B commit (the only
step that touches `api/proto`); `make app-test && make app` for D; every
schema change means `rm -rf .storage .storage/smoke` before the next
run. Nothing is committed without the user's review of the diff.

- Step A: done
- Step B: done
- Step C: done
- Step D: done
- Step E: done
