# m8 - Noise

Status: complete 2026-09-02. Steps A to D shipped (`gate.md`, `counts.md`, `rollup.md`).

Goal: the incidents list says one true thing per thing that happened, and
never a thing that did not. The milestone comes from reading a day of
incidents on a cluster with a node autoscaler: every row was closed
`pod_deleted`, and nearly six in ten were `crash` and `probe` incidents that
opened after the kubelet had already started killing their pod. The rest
were one `rescheduled` per pod the autoscaler evicted. Read as a person
would, the day held two facts: the autoscaler consolidated nodes, which is
routine and nobody acts on, and several workloads exit non-zero when they
receive SIGTERM, which is a defect in those workloads that nobody could see
because it was filed as sixty `crash` rows under forty routine ones. The
recorder had the noise and the signal inverted. This milestone puts them the
right way round: a routine eviction opens nothing, a readiness miss on a
container that is being killed opens nothing, and a container that dies
badly while its pod is being terminated opens one incident of its own kind,
`unclean_exit`. That incident closes with its pod like every other, and the
list's default view changes so that a row which closed recently and was
never acknowledged stays in front of the person for a day: the fact is
finished, the reading of it is not. Around that, the list and the detail
also stated things that were false: a fold summed the occurrences of a pod's
siblings onto its lead row, and a header explained a `crash` with the
sentence of a sibling's probe event. Those are fixed (step C), and replica
fan-out folds into one line (step D), so twenty pods with one problem read
as one problem. The page a pod's incidents share is its own milestone,
`m9-pod-page`. No history row or event row stops being recorded; the one
recorded value that changes is the category of a `DisruptionTarget` history
row for a routine eviction, and what the gate costs in captured logs is
stated in decision 2.

## Decisions (made, do not relitigate)

1. A routine eviction is not an incident. The `DisruptionTarget`
   condition with reason `EvictionByEvictionAPI` opens nothing: it is the
   autoscaler, a drain, or anything that goes through the eviction API and
   respects a PodDisruptionBudget, and the API cannot say whether the
   operator drained for consolidation or because the node was sick. What
   makes it safe to stay quiet is that the eviction itself was never the
   actionable fact: a pod that cannot be placed afterwards opens
   `scheduling`, one that comes up broken opens `crash`, one that dies
   badly on the way out opens `unclean_exit` (decision 2). The condition
   history row, the events and the pod's inferred `deletion_reason`
   (`evicted`) still record the eviction, and every row on that pod
   prints "pod deleted, evicted (inferred)". The other `DisruptionTarget`
   reasons keep opening `rescheduled`, because they are problems:
   `DeletionByTaintManager` (the node went NotReady),
   `PreemptionByScheduler` (capacity was taken), `DeletionByPodGC` (an
   orphan was collected). `node_pressure` is untouched: the kubelet
   evicting for memory or disk is always real. `EventCategory` is
   untouched too, including m7's split of the `Evicted` event by source
   component: an eviction-API `Evicted` event keeps its `rescheduled`
   category, attaches to a `rescheduled` row when one of the other reasons
   opened it, and otherwise stays unattached with its category intact.
   This narrows the condition rule that m3 wrote and leaves m7's event
   rule as it shipped.
2. A terminating pod opens nothing on a probe or a stuck check, and its
   `Error` termination is an `unclean_exit`, not a `crash`. The signal is
   the pod's `deletionTimestamp` (`pods.deletion_requested_at`). Gated:
   the `probe` open in `ApplyEvent` (readiness failing on a container that
   is being killed says nothing) and the `stuck` candidates
   (`ListStuckCandidates` adds `deletion_requested_at IS NULL`).
   Recategorised: a `terminated` state with reason `Error` on a
   terminating pod is `unclean_exit`, one row per container, covering the
   app that dies on SIGTERM instead of draining (exit 1) and the process
   that outlives its grace period and is killed (exit 137) without reading
   the exit code; the row's own sentence (decision 7) and the history row
   carry the code and the signal, so the two shapes stay distinguishable
   to the person fixing them. It opens only when the pod has no open
   incident at all, of any category on any container, which is the
   predicate `stuck` already uses: a pod-level `rescheduled` from a
   preemption or a taint means the pod died because of the node, not
   because the workload mishandles SIGTERM, and a container that was
   already failing owns its own ending. When the same container has an
   open incident, the termination attaches to it as an occurrence through
   a category-blind lookup that `Apply` gains for this one case, and the
   history row takes that incident's category so a history row and the
   incident it names never disagree; when another part of the pod has the
   open incident, the termination is recorded in history as
   `unclean_exit` with no incident. Known limitation, stated rather than
   dressed up: a crash-looping container that also mishandles SIGTERM
   never has the SIGTERM defect named; its death is the end of the crash.
   Everything else is untouched: `OOMKilled` on a terminating pod is
   still `oom` (the limit is still the limit), the
   `waiting` states are still themselves, the pod-level condition and
   status-reason categories are untouched, the job path has no pod. The
   pod path compares the live object (`changes.Pod`); the event path
   reads the stored pod row and so depends on the pod update landing
   first, as its `deleted_at` check already does. What the gate costs:
   the open-time `log_current` capture is requested only when an incident
   opens, so a probe-only shutdown (no non-zero exit) captures no live
   log; the delete-time fallback and the early-copy keep are unchanged
   and fire on any non-zero exit, so an `unclean_exit` always has its
   file. A pod stuck terminating for hours is a real failure this
   milestone keeps quiet about; a future rule keyed on elapsed time.
3. The first `deletion_requested_at` is kept. The API server rewrites a
   pod's `deletionTimestamp` when a later delete arrives with a shorter
   grace period, and the kubelet's final delete does exactly that, so the
   last object seen carries the moment of removal and not the moment
   termination began. The keep lives where the other deletion columns are
   carried over, in `DiffPod` (`internal/ingest/pod.go`): when the
   snapshot has a value, `changes.Pod` keeps it, so the row, the diff and
   `UpsertPod`'s contract stay one thing. Section 5.3 of
   `docs/design/data-storage.md` says so, and its `deletion_reason`
   enumeration gains `evicted`, which the code has written since m1.
4. `unclean_exit` closes `pod_deleted` like every pod incident, and the
   default view changes instead of the lifecycle. Closing on delete keeps
   the invariants `docs/design/data-storage.md` states on purpose (an open
   incident exists only on a live object; the open list mirrors what is
   still broken), keeps the menu-bar count honest, and needs no new close
   reason. What changes is what the list shows by default: a new state
   `attention`, defined as open, or closed within `attention_window` and
   never acknowledged or dismissed. `attention_window` is a config value,
   default 24h, served in status like the other windows. The `state`
   filter and the counts endpoint gain the value; the sidebar's default
   pill becomes Attention, with Open kept as its own pill; the menu bar
   counts the attention set. A closed row in the set renders with its
   closed badge as today. Acknowledge is the act of having seen it, as
   section 6.3 already says; `a` on a rollup row (decision 9) makes
   twenty of them seen at once. After the window the row leaves the
   default view on its own and stays reachable under Pod deleted.
5. The category rule is amended once more: a category may key on the
   reason, the event's `source_component` and whether the pod is
   terminating, still never on phase, exit code or message. The
   enforcing test, `CLAUDE.md` and `docs/design/data-storage.md` section
   6.1 change together. `ContainerCategory` gains the terminating flag as
   a parameter and stays pure. In section 6.1 the `rescheduled` row is
   rewritten for decision 1 (the condition side narrows, the event side
   stays), the `unclean_exit` row is added, the `probe` row gains the
   terminating gate beside its ready check, the `stuck` paragraph gains
   its gate, and the last paragraph of 6.1 and the "Severity / actionable
   flags" row of section 9, which name a `muted` flag as the answer to
   autoscaler evictions, are rewritten: a mute chooses by category or
   identity, and the noise was categorised `crash` and `probe` on pods
   whose one true fact was routine. Sections 6.3 and 8 are untouched,
   because decision 4 keeps their invariants. The Mute row of
   `docs/design/presentation.md` is untouched.
6. `unclean_exit` is added everywhere a category lives, in one step, and
   a test makes the places agree: `common.proto` (`make generate`), the
   two comments in `incidents.proto` that enumerate the values, the
   `incidents.category` CHECK in `0001_init.sql`, `store.CategoryUncleanExit`,
   the API enum map, the `list_incidents` jsonschema in
   `internal/mcp/tools.go`, the Swift `Category` enum, the sidebar's
   ranked list written out in full as `crash, oom, unclean_exit,
   image_pull, config, probe, scheduling, stuck, node_pressure,
   rescheduled, job_failed` (a dirty SIGTERM exit ranks below an OOM
   kill; this rank is also the fold's lead choice and a group's worst
   category), the badge vocabulary in `BadgeStyle.swift` with the crash
   tint, the category list in `README.md`, the section 6.1 table and the
   Components page of `docs/mockups/idios-ui.html`. The agreement test
   mirrors the kind vocabulary test in `internal/mcp`: one table builds
   the store's list, the proto enum names and the jsonschema strings from
   one source and fails when any of them drifts. The schema edit means
   the databases are recreated.
7. A count is a count of the row it sits on, and every category has a
   sentence. The fold's lead row shows its own `occurrences`; the "+N on
   this pod" pill is the only place the siblings are counted, and it
   counts rows. `PodFold.occurrences` and `foldedOccurrences` go. The
   fold's order gains open before closed ahead of the category rank. The
   header's explanation fallback filters the pod-wide event list to
   `incidentID == incident.id` before taking the newest Warning, which is
   what its provenance label already claims. An `unclean_exit` has no
   `last_message` (the kubelet writes none for a grace-period kill) and
   no Warning of its own (`Killing` is Normal, `Unhealthy` is `probe`), so
   its sentence is built from the row: "exit 137, signal 9, while the pod
   was terminating", from the history row's exit code and signal and the
   pod's `deletion_requested_at`, with the provenance saying so. No
   category arrives with a blank header.
8. The list's pure logic moves into the package so it can be tested.
   `make app-test` runs the one test target over `Sources/IdiosModel`;
   the fold, the category rank it reads from `IncidentsSidebar` and the
   explanation choice live in the application target and nothing there
   is testable. Step C moves them into `IdiosModel` as functions over
   model values, dropping the `@MainActor` they carried only for the
   sidebar constant; the views call them. Step D's rollup is written
   there. Views still never import `IdiosAPI`.
9. Replica fan-out folds into one line, in the application only. Inside a
   workload group, rows from different pods that share
   `(container_name, category)` fold under one rollup row when two or
   more pods share the key; the row names the count of pods and expands
   to the pod folds of decision 7's shape; a key of one pod is a plain
   pod fold. `last_reason` is not in the key: it moves on every
   occurrence (a crash loop alternates `Error` and `CrashLoopBackOff`),
   and a rollup that formed and dissolved as loops went in and out of
   phase would be worse than no rollup; the lead line still prints the
   reasons and exit codes of what is folded. Every count stays a count of
   incidents. Actions over a rollup row apply to every incident folded
   under it, one write per row over the per-incident API, each row
   independently, the error shown once if one fails: `a` acts at once
   (reversible); `d` and `r` ask first, naming the count and the workload
   ("Mark 20 incidents of checkout-api resolved?"), because a manual
   close is final and twenty of them in one keystroke deserve the
   confirmation Command-Delete already has; Command-Delete stays per
   incident and is refused on a rollup row. Rollup expansion
   keys on the rollup key string, separate from the pod fold's `podUID`.
   `ListIncidents` rows already carry every field the key reads; no
   daemon change. It never crosses a group boundary and never applies to
   a job-subject incident.
10. Nothing in fixtures, tests, docs, comments, commits or this plan names
   a real organisation, cluster, namespace, workload, image or node; every
   example is invented and every shape is reproduced on the smoke cluster.

## Steps

### A. Daemon - the first deletion timestamp (`gate.md`, written when A starts)

Decision 3 in `DiffPod`, with an ingest test that diffs the same pod
twice with a later `deletionTimestamp` and reads the first back on
`changes.Pod`; section 5.3 amended in the same commit (the column's
sentence and the `evicted` reason).

### B. Daemon - the inversion (`gate.md`, same plan)

Decisions 1, 2, 4, 5 and 6 on the daemon side. `ConditionCategory` drops
the eviction-API mapping; `ContainerCategory` takes the terminating flag
and returns `unclean_exit`; `Apply` applies the no-open-incident guard
and the category-blind attach; `ApplyEvent` and `ListStuckCandidates`
take the gate; `IncidentFilter`, the counts query and `config` gain
`attention` and `attention_window`, served in status; the proto, schema,
enum map, jsonschema and agreement test change together. Table-driven
tests trace to the amended section 6.1: an `EvictionByEvictionAPI`
condition opens nothing and its history row is written;
`DeletionByTaintManager` still opens `rescheduled`; a container exiting
137 after the timestamp on a pod with nothing open opens `unclean_exit`
and its history row carries it; the same exit on a container with an
open `crash` attaches as an occurrence and the history row says `crash`;
the same exit on a pod whose only open incident is a pod-level
`rescheduled` opens nothing and the history row says `unclean_exit`; an
`Unhealthy` after the timestamp attaches to nothing new; `OOMKilled`
after the timestamp is still `oom`; a first-sight `ImagePullBackOff` on a
terminating pod still opens; the delete path closes `unclean_exit`
`pod_deleted`; the attention predicate keeps a row closed an hour ago
and unacknowledged, drops the same row once acknowledged, and drops a
row closed longer than the window ago. The capture tests in
`internal/processor` gain the case that a probe-only shutdown requests
no open-time `log_current` and an `unclean_exit` does.
`docs/design/data-storage.md`, `CLAUDE.md`, `README.md` and the mockup's
Components page change in the same step. `hack/smoke/` gains one
fixture, `graceful-exit`: a Pod whose PID 1 ignores SIGTERM, a readiness
probe reading a file with `periodSeconds: 2` and `failureThreshold: 1`
(the defaults would take longer than the grace to fail once), a preStop
hook that removes the file, and `terminationGracePeriodSeconds: 20`.
`run.sh` deletes that pod after the watch window (longer than
`probe_grace`, so a probe row could open if the gate were absent), waits
the grace period plus slack, and prints a labelled query of the
incidents naming that pod, expected to be one `unclean_exit` closed
`pod_deleted` and no `probe`; the fixture's comment says that the
kubelet's final status write can lose the race to the object's removal
on a single node, in which case no termination is observed and the query
is empty, which is the race the early-capture cache exists for and not a
failure of the gate. Wire fixtures under `api/testdata` are regenerated
so `idios mock` serves the category and the state.

### C. Application - counts and attention (`counts.md`, written when C starts)

Decision 8's move first: the fold, the rank order and the explanation
choice into `IdiosModel`, the views calling them, the app building and
looking the same. Then decision 7 on the moved code, including the
`unclean_exit` sentence, and decision 4's application side: the
`IncidentState` enum gains `attention`, the sidebar's default pill and
the menu bar's count use it, the status screen shows the window. Package
tests trace to the amended `docs/design/presentation.md` fold sentence
("never changing a count"), to the header's provenance label, and to the
attention definition. The fold paragraph of `docs/design/presentation.md`
drops the summed-occurrences clause and states the lead order; the
sidebar paragraph states the default. `rm -rf macos/.build` first: the
proto changed in B.

### D. Application - the workload rollup (`rollup.md`, written when D starts)

Decision 9 in `IdiosModel` (the grouping), `IncidentsList.swift` and
`IncidentRowView.swift` (the row, its pill, its expansion key, its
actions over the folded set, the confirmation). Package tests trace to
the new `docs/design/presentation.md` paragraph on rollups. Page 1 of
`docs/mockups/idios-ui.html` is redrawn as the list now is: the pod as
the row with one chip per container, the rollup row collapsed and
expanded, the lead row carrying its own count, Attention as the default
pill; its page note is rewritten in the present tense, because the
mockup states what is. The smoke cluster reproduces the shape with
`crash-loop.yaml`, a Deployment, at `replicas: 3`; the smoke tables then
print three crash incidents for it instead of one, which the fixture's
comment says.

## Order and status

A and B are independent (B's gate is a null test that holds with either
value of the column); A goes first only because it is one line and its
review is quick, and the two share one plan. Then C, then D (D expands
into the fold C reshapes). Checkpoint before every commit per
`CLAUDE.md`; B touches `api/proto` and `0001_init.sql`, so
`make generate-check` runs after its commit, `rm -rf .storage
.storage/smoke` before the next run, and `rm -rf macos/.build` before
`make app-test`; `make app-test && make app` for B (the Swift enum), C
and D. Nothing is committed without the user's review of the diff.

- Step A: complete 2026-09-02
- Step B: complete 2026-09-02
- Step C: complete 2026-09-02
- Step D: complete 2026-09-02
