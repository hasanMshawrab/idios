# m4 - prompt drafts and the answer rubric

The two prompts the daemon generates (roadmap phase 3), as v0 drafts to
tune in phase 5, and the rubric the tuning loop scores against. The
reference scenario is written with smoke-style names; any real cluster
reproduces the same shape.

## Reference scenario

A queue-consumer Deployment pod (app container `worker`, service-mesh
sidecar `proxy`) in `staging/queues` is deleted by a scale-down while
the queue is idle. The app does not handle SIGTERM: kubelet's Killing
event fires, the app keeps draining messages for the whole grace period,
readiness probes start failing with connection refused only after the
Killing, and at the end of grace both containers are SIGKILLed (exit
137). Four incidents record it - crash and probe per container - and the
crash incidents open so late that every event already attached to the
probe incidents. The logs also carry an unrelated repeating 4xx: one
malformed message that an upstream API rejects on every retry because
the queue has no effective dead-letter policy.

## Answer rubric

A passing answer, from either prompt, in order of weight:

1. Exit 137 is SIGKILL: the pod was terminated from outside, not by an
   application fault.
2. The Killing event precedes every probe failure; the readiness
   failures are a consequence (connection refused after the server
   closed), and a readiness probe cannot kill a pod.
3. The app logged to the last second of the grace period: it does not
   handle SIGTERM, which is the only reason a crash exists at all.
4. The deletion was a scale-down; the trigger (autoscaler or human) is
   not in the recorded data, and the answer says so instead of guessing.
5. The repeating 4xx is unrelated to the death and is the one human
   follow-up: a poison message with no dead-letter path.
6. Noise test: the answer does not blame the probes, the 4xx, or the
   sidecar; five ordered steps or fewer for "what happened"; about a
   page, no more.

An answer that inverts 2 (probes caused the kill), guesses at 4, or
leads with the 4xx fails regardless of everything else.

## Prompt v0 - mode mcp

    You are investigating one Kubernetes incident recorded by idios, a
    local incident recorder for pods and jobs in a few namespaces. The
    idios MCP server is connected and read-only. Pull only what you
    need; logs are read with tails, ranges or grep, never whole.

    Incident {id}: {category} - {subject line} - {exit / reason} -
    {state or close reason}.

    Suggested order, abandon it when the evidence points elsewhere:
    1. get_incident {id} for the detail and the timeline.
    2. get_pod for the whole event stream, containers and conditions.
    3. read_log tails of the subject container around the failure time.
    4. list_incidents by the pod or job uid for the siblings.

    Answer rules: separate what the data proves from what it only
    suggests; when the trigger lies outside the recorded data (scaling
    decisions, node history), say so rather than guess; finish with
    "What happened" as at most five ordered timestamped steps and
    "Follow-ups" listing only what a human should change.

## Prompt v0 - mode snapshot

    You are investigating one Kubernetes incident from a snapshot
    recorded by idios, a local incident recorder. The snapshot is
    complete as recorded, but the recorder watches only pods, jobs and
    their events in a few namespaces: scaling decisions, autoscalers
    and node history are outside the data - say so rather than guess.

    [incident]   detail and timeline
    [pod]        containers and conditions
    [events]     every event of the pod, attached or not, time-ordered
    [logs]       captured files, one section each, truncation marked
    [pod.json]   sanitized: environment values are stripped
    [related]    incidents sharing the pod or the job

    Answer rules: separate what the data proves from what it only
    suggests; finish with "What happened" as at most five ordered
    timestamped steps and "Follow-ups" listing only what a human
    should change.

## Tuning notes

Record each run as: date, prompt mode, rubric lines missed, prompt
change made. Runs were driven against a live cluster; only the shape of
each scenario is recorded here.

- 2026-08-31, mcp, on a pod-deleted scale-down (a CronJob pod evicted by
  node consolidation): no rubric line missed, no change. The answer
  refused to guess the trigger and named the recorded eviction chain.
- 2026-08-31, mcp and snapshot, on a crash mirroring the reference
  scenario (Killing, probes failing after it, exit 137 at the end of
  grace, a sidecar dying alongside, an unrelated 4xx in the logs): mcp
  missed nothing and proved the SIGKILL by matching the grace period
  from the pod spec; snapshot missed line 3 - it could not see the pod
  spec or the sibling containers' captures. No prompt text changed. The
  misses were composition and were fixed in code: the snapshot now
  carries the pod's whole evidence (a capture hangs off whichever of the
  pod's incidents was open, and a pod has one pod.json row), and both
  prompts name the cluster instead of printing its numeric id, because
  the tools take names. The mcp run also surfaced that skew: the agent
  pasted the id into get_workload and was refused.
- Line 5 watch item for later runs: both answers ruled the unrelated 4xx
  out of the death correctly but neither promoted it to a follow-up.
