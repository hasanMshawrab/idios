#!/bin/sh
# Runs idios for two minutes against deliberately broken pods in the
# idios-smoke namespace of the OrbStack cluster and prints what it recorded.
# Only kube/config and the idios-smoke namespace are touched.
set -eu

export KUBECONFIG=./kube/config
NS=idios-smoke
DIR=${SMOKE_DIR:-.storage/smoke}
DURATION=${SMOKE_SECONDS:-120}
LISTEN=${SMOKE_LISTEN:-127.0.0.1:7770}
IDIOS="./bin/idios -data-dir $DIR -kubeconfig $KUBECONFIG -listen $LISTEN"

kubectl get ns "$NS" >/dev/null
rm -rf "$DIR"
mkdir -p "$DIR"

$IDIOS cluster add orbstack --context orbstack
$IDIOS ns add orbstack "$NS"

cleanup() {
    if [ -n "${pid:-}" ]; then
        kill -INT "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi
    kubectl -n "$NS" delete -f hack/smoke/ --ignore-not-found --wait=false
}
trap cleanup EXIT
trap 'exit 130' INT TERM

$IDIOS run &
pid=$!
sleep 5
if ! kill -0 "$pid" 2>/dev/null; then
    wait "$pid" || true
    pid=
    echo "idios run exited early; see its output above" >&2
    exit 1
fi
kubectl -n "$NS" apply -f hack/smoke/
echo "watching $NS for ${DURATION}s"
sleep "$DURATION"

kubectl -n "$NS" delete pod smoke-graceful --wait=false
sleep 35

echo
$IDIOS status
echo
echo "api"
curl -s "$LISTEN/v1/incidents?limit=5"
echo
echo
echo "graceful exit"
sqlite3 -header -column "$DIR/idios.db" "
SELECT i.category, i.close_reason, h.exit_code, h.signal, i.opened_at, i.closed_at
FROM incidents i
JOIN pods p ON p.uid = i.pod_uid
LEFT JOIN container_state_history h ON h.incident_id = i.id
WHERE p.name = 'smoke-graceful'
ORDER BY i.id"
echo
echo "incidents"
sqlite3 -header -column "$DIR/idios.db" "
SELECT id, workload_kind AS wkind, workload_name AS workload, container_name AS container, category,
       first_reason, last_reason, occurrences AS n, opened_at, close_reason
FROM incidents ORDER BY id"
echo
echo "artifacts"
sqlite3 -header -column "$DIR/idios.db" "
SELECT p.name AS pod, a.container_name AS container, a.kind, a.restart_count AS idx,
       a.file_path IS NOT NULL AS has_file, a.size_bytes AS bytes, a.captured_early AS early, a.capture_gap
FROM artifacts a JOIN pods p ON p.uid = a.pod_uid ORDER BY a.id"
