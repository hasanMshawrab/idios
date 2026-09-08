# m10 - the UI review this milestone answers

Condensed from a review made on 2026-09-03 over 29 screenshots of every
route, taken against a private daemon serving the smoke store (one
cluster, one namespace, about 190 incidents: a Deployment crash loop, an
OOM pod, an image pull, a config error, a Job, and a CronJob failing
every two minutes), plus a survey of the Swift views and stores, a
survey of the daemon API and store against what the application decodes
and draws, and an independent designer's pass over the screenshots and
`docs/mockups/idios-ui.html`. The roadmap's decisions are the answers;
this file is the evidence, kept short. Names below are the smoke
fixtures' invented names.

## Three causes

1. Rows are incidents, not problems. `Rollup.swift` keys on pod uid,
   container and category; a job-subject incident has no pod uid and is
   keyed `incident/<id>`, so it never folds. 56 identical `job failed`
   rows and a separate "118 pods of smoke-cron-fail" rollup for one
   CronJob, interleaved by `last_seen_at`. The first header's different
   rule: the cluster header is a plain non-folding row while groups
   below are pinned `Section` headers.
2. Colour and words each carry several meanings. Red: four categories,
   the open state, a namespace dot, a destructive button, owner-chain
   dots, an errored cluster. Green: recovered, `job finished` on a failed
   run, READY, synced, Complete. Blue: selection, Deployment kind,
   `rescheduled`. The sidebar says "Marked resolved", every badge says
   "manual". The only prose about state meaning is a tooltip quoting
   column names.
3. Nothing takes a person to a thing. The list filter is a substring
   match over `workloadName`, `podName`, `lastReason`, `containerName`;
   the tree filter over five fields; no id, namespace or cluster match,
   no jump, nothing server-side. Identity is printed in the window title,
   header, identity line, OWNER CHAIN and CONTEXT; none is a link. The
   keys `a`, `d`, `r`, Cmd-1/2/3, Cmd-[ are discoverable nowhere.

## Per screen

- Incidents (attention): 192 rows for five problems. Right-hand values
  are not columns; the age wraps ("closed 1h / 28m ago"); the state pill
  has three widths. "attention" appears in the title, a pill and a chip,
  unexplained. Sidebar pills wrap 3/2/1/1/2 and hide zero counts; twelve
  category dots always show.
- Job finished: 56 identical rows, all failed runs, all green.
  Acknowledged, Recovered, Dismissed: "Nothing here" with the category
  list still lit.
- Pod page: structure right, sentence title good. A one-segment
  segmented control painted as a primary button; five equal-weight
  action buttons; a 34-chip captured-logs grid saying "no output" 33
  times; the answer as the fourteenth key/value of a table; method
  footers as visible text (the spec says hover); "SIBLINGS 0 pods of
  this none"; "Logs 0 files" above 34 chips; "1 containers"; the events
  table wraps an ISO timestamp; the header tag flips READY / NOT READY
  every few seconds.
- Timeline: every entry prints its time three times; 31 identical cycles
  uncompressed, so "since when, how often" is unanswered.
- Workloads: "NO CONTROLLER 3" heads ten rows, five names twice (live
  and deleted pod, indistinguishable). Four colour meanings in the tree.
  Unlabelled trailing counts vanish at zero. No collapse-all; a filter
  clears collapse state. Restarts chart: four blocks, no axis. CronJob
  Runs: 50 rows of "smoke-cro...-29807159 Failed BackoffLimitExceeded",
  no link to pod or incident; the OK CronJob shows 50 "Complete" rows.
  Pods tab defaults to All. The mockup's 3d / 24h / 6h switcher did not
  ship.
- Status: honest and dense; the vocabulary values ("attention window
  86400 s") are buried under Storage in raw seconds; the header is a
  mono log line.
- Menu bar: "205 incidents need attention", four of five rows the same
  CronJob. Clusters and Add cluster sheets: the most native surfaces;
  a single context is not preselected; the Grafana builder shows even
  when not configured.

## Where the app diverged from the mockup

The mockup's page 1 grouped by workload with a header per workload; the
app ships one namespace header and no workload header. The mockup's
Runs example is hourly and neither mockup nor spec anticipated a
two-minute CronJob. The mockup's 3d / 24h / 6h switcher was dropped. The
pod page was transcribed faithfully, footnotes and raw field labels
included; there the mockup was wrong and Section 7.4 was right. The
mockup's menu bar counted open; the spec and app count attention. The
C1 vocabulary coloured categories and states independently, which is
where red-on-red rows and a green failed run came from.

## Data the UI leaves unused (from the API survey)

| Field or capability | Where | Status |
|---|---|---|
| `IncidentRow.node_name`, list filter `node_name` | incidents | decoded, never drawn or offered |
| `IncidentDetail.related_incidents` | detail | computed on every call, not decoded |
| `WorkloadDetail.runs` | workload detail | not decoded; Runs tab re-fetches `/jobs` |
| `Container.running_since`, `mem_limit_bytes`, `cpu_*_millis` | container | decoded, never drawn |
| `Pod.status_reason`, `status_message` | pod | decoded, never drawn |
| `K8sEvent.first_ts` | event | decoded, never drawn (span of a repeating event) |
| `JobRow.succeeded`, `completions`, `parallelism` | job | decoded, never drawn |
| `Rollout.available_replicas`, `first_seen_at`, `last_seen_at` | rollout | decoded, never drawn |
| `Cluster.identity`, `last_connected_at`, `last_error_at` | cluster | decoded, never drawn |
| `TimelineEntry.artifact_id` | timeline | a capture entry cannot open its file |
| `GET /pods` as a list | pods | called only with `limit=1` for the Grafana preview |
| `containers.message`, `container_state_history.message` | schema | stored, read by `query`, not on the wire |
| `rollout_history` per container and tag | schema | flattened to `images[]`; no per-container diff |

There is no text search server-side; every filter is an equality
predicate. The pods table already carries `(cluster_id, namespace,
name)`, so an exact pod-name filter is cheap. `/incidents/counts` is
global; the tree's numbers come from `/workloads`.

## State vocabulary, from the derivation

`state` is `dismissed` when `dismissed_at` is set; else the
`close_reason` when `closed_at` is set; else `acknowledged` when
`acknowledged_at` is set; else `open`. `attention` is `dismissed_at IS
NULL AND (closed_at IS NULL OR (closed_at >= now - attention_window AND
acknowledged_at IS NULL))`. `job_finished` also covers a Job whose
failure was first seen older than the stabilization window, and a run
that failed and will not retry. `recovered` is ten minutes stable, or a
pod scheduled. `pod_deleted` and `manual` never reopen. The smoke store
holds no `recovered`, `manual` or `dismissed` rows, so those states are
untested by real data.

## Answers recorded in the roadmap

Attention keeps its 24-hour rule (decision 3). Categories are neutral
chips; the muted-family fallback is recorded and not tried before B
(decision 2). Resolved everywhere; the wire keeps `manual` (decision 3).
Run strip first, matrix past 200 runs (decision 7). The only daemon
change is the `pod_name` filter (decision 6).
