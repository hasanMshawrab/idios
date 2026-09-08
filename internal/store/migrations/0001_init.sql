CREATE TABLE clusters (
    id                INTEGER PRIMARY KEY,
    identity          TEXT UNIQUE,
    name              TEXT NOT NULL DEFAULT '',
    context_name      TEXT NOT NULL,
    api_server_url    TEXT NOT NULL,
    first_seen_at     TEXT NOT NULL,
    last_connected_at TEXT,
    last_error        TEXT,
    last_error_at     TEXT,
    grafana_url         TEXT NOT NULL DEFAULT '',
    loki_datasource_uid TEXT NOT NULL DEFAULT '',
    log_selector        TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE watched_namespaces (
    id         INTEGER PRIMARY KEY,
    cluster_id INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    added_at   TEXT NOT NULL,
    UNIQUE (cluster_id, name)
) STRICT;

CREATE TABLE pods (
    uid                   TEXT PRIMARY KEY,
    cluster_id            INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace             TEXT NOT NULL,
    name                  TEXT NOT NULL,
    node_name             TEXT,
    phase                 TEXT NOT NULL,
    status_reason         TEXT,
    status_message        TEXT,
    deletion_requested_at TEXT,
    qos_class             TEXT,
    controller_kind       TEXT NOT NULL DEFAULT 'none',
    controller_name       TEXT NOT NULL DEFAULT '',
    controller_uid        TEXT NOT NULL DEFAULT '',
    workload_kind         TEXT NOT NULL DEFAULT 'none',
    workload_name         TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    started_at            TEXT,
    first_seen_at         TEXT NOT NULL,
    last_seen_at          TEXT NOT NULL,
    deleted_at            TEXT,
    deletion_source       TEXT CHECK (deletion_source IN ('watch', 'reconcile', 'unwatched')),
    deletion_reason       TEXT CHECK (deletion_reason IN ('rollout', 'replaced', 'scaled_down', 'job_pruned', 'unknown', 'evicted'))
) STRICT;
CREATE INDEX pods_cluster_deleted ON pods (cluster_id, deleted_at);
CREATE INDEX pods_cluster_ns_name ON pods (cluster_id, namespace, name);
CREATE INDEX pods_workload        ON pods (cluster_id, workload_kind, workload_name);
CREATE INDEX pods_deleted         ON pods (deleted_at);
CREATE INDEX pods_controller       ON pods (controller_uid);

CREATE TABLE jobs (
    uid               TEXT PRIMARY KEY,
    cluster_id        INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace         TEXT NOT NULL,
    name              TEXT NOT NULL,
    cronjob_uid       TEXT,
    cronjob_name      TEXT,
    active            INTEGER NOT NULL DEFAULT 0,
    succeeded         INTEGER NOT NULL DEFAULT 0,
    failed            INTEGER NOT NULL DEFAULT 0,
    backoff_limit     INTEGER,
    completions       INTEGER,
    parallelism       INTEGER,
    active_deadline_seconds INTEGER,
    restart_policy    TEXT NOT NULL DEFAULT '',
    condition_type    TEXT CHECK (condition_type IN ('Complete', 'Failed', 'Suspended')),
    condition_reason  TEXT,
    condition_message TEXT,
    created_at        TEXT NOT NULL,
    started_at        TEXT,
    finished_at       TEXT,
    first_seen_at     TEXT NOT NULL,
    last_seen_at      TEXT NOT NULL,
    deleted_at        TEXT
) STRICT;
CREATE INDEX jobs_cronjob ON jobs (cluster_id, namespace, cronjob_uid, started_at);
CREATE INDEX jobs_sweep   ON jobs (deleted_at, finished_at);

CREATE TABLE incidents (
    id              INTEGER PRIMARY KEY,
    cluster_id      INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace       TEXT NOT NULL,
    subject_kind    TEXT NOT NULL CHECK (subject_kind IN ('pod', 'job')),
    pod_uid         TEXT REFERENCES pods(uid) ON DELETE CASCADE,
    -- No reference to jobs: a pod incident stamps the Job that owns its pod,
    -- and the two informers deliver in no order, so the jobs row may arrive
    -- after the incident opens and may be swept while the incident lives on.
    job_uid         TEXT,
    container_name  TEXT NOT NULL DEFAULT '',
    workload_kind   TEXT NOT NULL DEFAULT 'none',
    workload_name   TEXT NOT NULL DEFAULT '',
    category        TEXT NOT NULL CHECK (category IN (
                        'oom', 'crash', 'unclean_exit', 'image_pull', 'config', 'probe', 'scheduling',
                        'node_pressure', 'rescheduled', 'job_failed', 'stuck', 'other')),
    first_reason    TEXT NOT NULL,
    last_reason     TEXT NOT NULL,
    last_message    TEXT,
    image           TEXT,
    image_tag       TEXT,
    image_id        TEXT,
    -- Stamped at open and never back-filled: a join to pods would lose the
    -- node once the pod row is swept.
    node_name       TEXT,
    occurrences     INTEGER NOT NULL DEFAULT 1,
    opened_at       TEXT NOT NULL,
    last_seen_at    TEXT NOT NULL,
    closed_at       TEXT,
    close_reason    TEXT CHECK (close_reason IN ('recovered', 'pod_deleted', 'job_finished', 'manual')),
    acknowledged_at TEXT,
    dismissed_at    TEXT,
    note            TEXT,
    CHECK ((subject_kind = 'pod') = (pod_uid IS NOT NULL)),
    -- One-way: a job incident must name its Job, a pod incident may.
    CHECK (subject_kind <> 'job' OR job_uid IS NOT NULL),
    CHECK ((closed_at IS NULL) = (close_reason IS NULL))
) STRICT;
-- Only OPEN rows are unique per key; closed rows never conflict, so a plain
-- insert after a close cannot fail and reopen is a separate path.
CREATE UNIQUE INDEX incidents_open_pod ON incidents (pod_uid, container_name, category)
    WHERE closed_at IS NULL AND subject_kind = 'pod';
CREATE UNIQUE INDEX incidents_open_job ON incidents (job_uid, category)
    WHERE closed_at IS NULL AND subject_kind = 'job';
CREATE INDEX incidents_cluster_closed ON incidents (cluster_id, closed_at);
CREATE INDEX incidents_workload       ON incidents (workload_kind, workload_name, opened_at);
CREATE INDEX incidents_closed         ON incidents (closed_at);
CREATE INDEX incidents_pod_closed     ON incidents (pod_uid, closed_at);
CREATE INDEX incidents_job_closed     ON incidents (job_uid, closed_at);

CREATE TABLE pod_condition_history (
    id                INTEGER PRIMARY KEY,
    pod_uid           TEXT NOT NULL REFERENCES pods(uid) ON DELETE CASCADE,
    type              TEXT NOT NULL,
    status            TEXT NOT NULL,
    reason            TEXT NOT NULL DEFAULT '',
    message           TEXT,
    k8s_transition_at TEXT,
    observed_at       TEXT NOT NULL
) STRICT;
CREATE INDEX pod_condition_history_pod      ON pod_condition_history (pod_uid, observed_at);
CREATE INDEX pod_condition_history_observed ON pod_condition_history (observed_at);

CREATE TABLE containers (
    id                        INTEGER PRIMARY KEY,
    pod_uid                   TEXT NOT NULL REFERENCES pods(uid) ON DELETE CASCADE,
    name                      TEXT NOT NULL,
    kind                      TEXT NOT NULL CHECK (kind IN ('init', 'sidecar', 'app', 'ephemeral')),
    image                     TEXT NOT NULL,
    image_tag                 TEXT,
    image_id                  TEXT,
    container_id              TEXT,
    cpu_request               TEXT,
    cpu_limit                 TEXT,
    mem_request               TEXT,
    mem_limit                 TEXT,
    cpu_request_millis        INTEGER,
    cpu_limit_millis          INTEGER,
    mem_request_bytes         INTEGER,
    mem_limit_bytes           INTEGER,
    state                     TEXT NOT NULL CHECK (state IN ('waiting', 'running', 'terminated')),
    reason                    TEXT,
    message                   TEXT,
    exit_code                 INTEGER,
    signal                    INTEGER,
    ready                     INTEGER NOT NULL DEFAULT 0,
    restart_count             INTEGER NOT NULL DEFAULT 0,
    running_since             TEXT,
    last_terminated_reason    TEXT,
    last_terminated_exit_code INTEGER,
    last_terminated_signal    INTEGER,
    last_terminated_at        TEXT,
    updated_at                TEXT NOT NULL,
    UNIQUE (pod_uid, name)
) STRICT;

CREATE TABLE container_state_history (
    id                INTEGER PRIMARY KEY,
    pod_uid           TEXT NOT NULL REFERENCES pods(uid) ON DELETE CASCADE,
    container_name    TEXT NOT NULL,
    incident_id       INTEGER REFERENCES incidents(id) ON DELETE SET NULL,
    image             TEXT NOT NULL,
    image_id          TEXT,
    container_id      TEXT,
    state             TEXT NOT NULL CHECK (state IN ('waiting', 'running', 'terminated')),
    reason            TEXT,
    message           TEXT,
    exit_code         INTEGER,
    signal            INTEGER,
    restart_count     INTEGER NOT NULL,
    category          TEXT,
    k8s_started_at    TEXT,
    k8s_finished_at   TEXT,
    observed_at       TEXT NOT NULL,
    gap_reconstructed INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE INDEX container_state_history_pod      ON container_state_history (pod_uid, observed_at);
CREATE INDEX container_state_history_incident ON container_state_history (incident_id);
CREATE INDEX container_state_history_observed ON container_state_history (observed_at);

CREATE TABLE rollout_history (
    id              INTEGER PRIMARY KEY,
    cluster_id      INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace       TEXT NOT NULL,
    deployment_name TEXT NOT NULL DEFAULT '',
    deployment_uid  TEXT NOT NULL DEFAULT '',
    replicaset_uid  TEXT NOT NULL,
    replicaset_name TEXT NOT NULL,
    container_name  TEXT NOT NULL,
    image           TEXT NOT NULL,
    image_tag       TEXT,
    revision        INTEGER,
    created_at         TEXT NOT NULL,
    replicas           INTEGER,
    ready_replicas     INTEGER,
    available_replicas INTEGER,
    first_seen_at   TEXT NOT NULL,
    last_seen_at    TEXT NOT NULL,
    deleted_at      TEXT,
    UNIQUE (replicaset_uid, container_name)
) STRICT;
CREATE INDEX rollout_history_deployment ON rollout_history (cluster_id, namespace, deployment_name, first_seen_at);
CREATE INDEX rollout_history_sweep      ON rollout_history (deleted_at, last_seen_at);

CREATE TABLE k8s_events (
    id               INTEGER PRIMARY KEY,
    cluster_id       INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    event_uid        TEXT NOT NULL,
    namespace        TEXT NOT NULL,
    type             TEXT NOT NULL,
    involved_kind    TEXT NOT NULL,
    involved_name    TEXT NOT NULL,
    involved_uid     TEXT NOT NULL,
    field_path       TEXT NOT NULL DEFAULT '',
    reason           TEXT NOT NULL,
    message          TEXT NOT NULL DEFAULT '',
    source_component TEXT NOT NULL DEFAULT '',
    count            INTEGER NOT NULL DEFAULT 1,
    first_ts         TEXT NOT NULL,
    last_ts          TEXT NOT NULL,
    category         TEXT,
    incident_id      INTEGER REFERENCES incidents(id) ON DELETE SET NULL,
    raw_json         TEXT NOT NULL,
    UNIQUE (cluster_id, event_uid)
) STRICT;
CREATE INDEX k8s_events_involved   ON k8s_events (involved_uid, last_ts);
CREATE INDEX k8s_events_cluster_ns ON k8s_events (cluster_id, namespace, last_ts);
CREATE INDEX k8s_events_last_ts    ON k8s_events (last_ts);
CREATE INDEX k8s_events_incident   ON k8s_events (incident_id);

CREATE TABLE artifacts (
    id             INTEGER PRIMARY KEY,
    pod_uid        TEXT NOT NULL REFERENCES pods(uid) ON DELETE CASCADE,
    incident_id    INTEGER REFERENCES incidents(id) ON DELETE SET NULL,
    container_name TEXT NOT NULL DEFAULT '',
    kind           TEXT NOT NULL CHECK (kind IN ('log_previous', 'log_current', 'pod_json')),
    restart_count  INTEGER NOT NULL,
    file_path      TEXT,
    size_bytes     INTEGER NOT NULL DEFAULT 0,
    truncated      INTEGER NOT NULL DEFAULT 0,
    captured_early INTEGER NOT NULL DEFAULT 0,
    capture_gap    TEXT CHECK (capture_gap IN (
                       'pod_deleted', 'no_previous_run', 'forbidden', 'no_output',
                       'kubelet_error', 'unknown', 'unobservable')),
    capture_note   TEXT,
    captured_at    TEXT NOT NULL,
    UNIQUE (pod_uid, container_name, kind, restart_count),
    CHECK ((file_path IS NULL) = (capture_gap IS NOT NULL))
) STRICT;
CREATE INDEX artifacts_incident ON artifacts (incident_id);

CREATE TABLE sweep_runs (
    id            INTEGER PRIMARY KEY,
    ran_at        TEXT NOT NULL,
    cutoff        TEXT NOT NULL,
    table_name    TEXT NOT NULL,
    rows_removed  INTEGER NOT NULL DEFAULT 0,
    files_removed INTEGER NOT NULL DEFAULT 0,
    bytes_removed INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    error         TEXT
) STRICT;
