# Plans

Plans are build scaffolding: they order the work of one milestone and are
frozen when it ships. The design docs in `docs/design/` describe the system
as it is; the code is the truth. Read this file, then the active
milestone's `roadmap.md`, and nothing else in this directory unless a plan
points you there.

## Milestones

| Directory | Scope | Status |
|---|---|---|
| `m1-recorder/` | The daemon: store, ingest, incidents, watcher, capture, closer, sweeper, status, smoke | complete 2026-08-27 |
| `m2-presentation/` | The API inside the daemon, the read models, the macOS application, then the human actions. Spec: `docs/design/presentation.md`; visual reference: `docs/mockups/idios-ui.html` | complete 2026-08-28 |
| `m3-signal/` | What is worth recording and what is true about it: incident semantics and the contract, then the screens, then the navigation. Specs: `docs/design/data-storage.md`, `docs/design/presentation.md` | complete 2026-08-31 |
| `m4-ask-ai/` | The [Ask AI] button: a read-only MCP server over the daemon's API, two daemon-generated prompts (MCP and full snapshot), and a manual tuning loop against a written rubric | complete 2026-08-31 |
| `m5-ship/` | The remote, the README with light-mode screenshots, the app-owned bundled daemon with first-run kubeconfig setup, and `install.sh` | complete 2026-08-31 |
| `m6-grafana/` | Per-cluster Grafana config and Explore links on pod containers and incidents, plus the two-pane clusters sheet. Visual reference: `docs/mockups/idios-ui.html` page 9 | complete 2026-09-01 |
| `m7-whole-story/` | The incident answers whole for an AI agent: the MCP kind vocabulary fixed and its skew tested, the run outcome, pod-wide artifacts, related incidents and node on the detail (the snapshot's api-layer projections moved down and deduplicated), the eviction event category split by source component, and the terminated message recorded | complete 2026-09-02 |
| `m8-noise/` | The noise and the signal put the right way round: a routine eviction opens nothing, a probe on a terminating pod opens nothing, a container that dies badly while its pod terminates opens `unclean_exit`; the list's default view keeps a recently closed, unacknowledged row in front for a day; the first deletion timestamp kept; fold counts and the header explanation stop inventing; replica fan-out folded into one line. Specs: `docs/design/data-storage.md`, `docs/design/presentation.md` | complete 2026-09-02 |
| `m9-pod-page/` | One page per pod: a master-detail screen keyed by pod replaces the incident detail, the timeline screen and the pod screen, the pod and its containers on the left, the selection in the middle, the rail on the right; siblings capped and carrying their category. Depends on m8 steps B and C. Visual reference: pages 1 to 4 of `docs/mockups/idios-ui.html`, redrawn by m8 step D and m9 step A. Spec: `docs/design/presentation.md` | complete 2026-09-03 |
| `m10-reading/` | The application made easy to read, search and explore: problems grouped by workload with CronJob runs folded, one colour rule and one vocabulary with a legend, a Cmd-K palette that jumps by name, the workloads tree and CronJob run strip, the pod page by subtraction, the menu bar counting open. Evidence: `m10-reading/review.md`. Spec: `docs/design/presentation.md`; visual reference: `docs/mockups/idios-ui.html` pages 1, 2, 5, 7 and C, redrawn by step A. Closing report: `m10-reading/closing.md` | complete 2026-09-05 |

## Rules

1. One directory per milestone, one `roadmap.md` per directory, 100-300
   lines: goal, phase list with one paragraph each, dependency order, the
   cross-cutting decisions the milestone adds, one status line per phase.
2. Handoff notes ("this phase gives the next phase these names") live at
   the end of the phase plan that produced them, under a "Hands to the
   next phase" heading. The roadmap points at them and does not repeat
   them.
3. A fact that outlives its milestone moves to `CLAUDE.md` (a rule agents
   follow) or to a design doc (a fact about the system). Nothing that was
   true only during a phase is kept.
4. A completed milestone is frozen: one status line at the top of its
   roadmap, no further edits (`.ai/code-is-truth.md` rule 4).
5. Phase plans follow the conventions in
   `m1-recorder/roadmap.md`, section "Writing the next phase plan":
   header (Goal, Architecture, Tech Stack, Spec, Global Constraints), file
   structure block, one task per independently testable deliverable with
   failing test then implementation, a one-line trace from every test to a
   design doc statement or a bug, Consumes/Produces per task, a closing
   self-review.
6. Design docs stay few, one per subsystem, and are updated in place when
   a milestone changes the system.
