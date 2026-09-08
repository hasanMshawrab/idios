package query

import (
	"context"
	"database/sql"
	"errors"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/grafana"
	"github.com/hasanMshawrab/idios/internal/store"
)

// clusterGrafana is one cluster's Grafana configuration plus its friendly
// name, the value $cluster substitutes to.
type clusterGrafana struct {
	cfg  grafana.Config
	name string
}

// loadClusterGrafana returns the cluster's Grafana configuration, or nil
// when the cluster row is gone or its grafana_url is empty: an empty
// grafana_url means unconfigured, and a link built from an empty base URL
// would open nothing.
func loadClusterGrafana(ctx context.Context, db store.Querier, clusterID int64) (*clusterGrafana, error) {
	var url, uid, selector, name string
	err := db.QueryRowContext(ctx, "SELECT grafana_url, loki_datasource_uid, log_selector, name FROM clusters WHERE id = ?", clusterID).
		Scan(&url, &uid, &selector, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if url == "" {
		return nil, nil
	}
	return &clusterGrafana{cfg: grafana.Config{BaseURL: url, DatasourceUID: uid, Selector: selector}, name: name}, nil
}

// attachIncidentGrafana fills IncidentDetail.GrafanaURL. It leaves it nil
// when the incident carries no pod (a job incident, or one whose pod was
// swept) or the cluster is unconfigured: no pod means no run to scope a
// link to.
func attachIncidentGrafana(ctx context.Context, db store.Querier, d *IncidentDetail) error {
	if d.Pod == nil {
		return nil
	}
	cg, err := loadClusterGrafana(ctx, db, d.Pod.ClusterID)
	if err != nil || cg == nil {
		return err
	}
	w, err := incidentWindow(d.Incident.Incident)
	if err != nil {
		return err
	}
	v := grafana.Values{
		Namespace: d.Pod.Namespace, Pod: d.Pod.Name, Container: d.Incident.ContainerName,
		Workload: d.Pod.WorkloadName, Node: strOrEmpty(d.Pod.NodeName), Cluster: cg.name,
	}
	url := grafana.ExploreURL(cg.cfg, v, w)
	d.GrafanaURL = &url
	return nil
}

// incidentWindow is the incident's own span, padded, open-ended while the
// incident has not closed.
func incidentWindow(inc store.Incident) (grafana.Window, error) {
	opened, err := clock.Parse(inc.OpenedAt)
	if err != nil {
		return grafana.Window{}, err
	}
	w := grafana.Window{From: opened.Add(-grafana.Pad)}
	if inc.ClosedAt != nil {
		closed, err := clock.Parse(*inc.ClosedAt)
		if err != nil {
			return grafana.Window{}, err
		}
		w.To = closed.Add(grafana.Pad)
	}
	return w, nil
}

// attachPodGrafana fills PodDetail.ContainerGrafanaURLs, one entry per
// container keyed by name. It leaves the map nil when the cluster is
// unconfigured.
func attachPodGrafana(ctx context.Context, db store.Querier, d *PodDetail) error {
	cg, err := loadClusterGrafana(ctx, db, d.Pod.ClusterID)
	if err != nil || cg == nil {
		return err
	}
	runs, err := terminatedRuns(ctx, db, d.Pod.UID)
	if err != nil {
		return err
	}
	urls := make(map[string]string, len(d.Containers))
	for _, c := range d.Containers {
		w, err := containerWindow(c, d.Pod.Pod, runs[c.Name])
		if err != nil {
			return err
		}
		v := grafana.Values{
			Namespace: d.Pod.Namespace, Pod: d.Pod.Name, Container: c.Name,
			Workload: d.Pod.WorkloadName, Node: strOrEmpty(d.Pod.NodeName), Cluster: cg.name,
		}
		urls[c.Name] = grafana.ExploreURL(cg.cfg, v, w)
	}
	d.ContainerGrafanaURLs = urls
	return nil
}

// terminatedRun is the kubelet's own start and finish of a container's
// newest terminated state, from the state history.
type terminatedRun struct {
	started  *string
	finished *string
}

// terminatedRuns maps each of the pod's containers to its newest terminated
// state's kubelet times; a container idios never saw terminate is absent.
func terminatedRuns(ctx context.Context, db store.Querier, podUID string) (map[string]terminatedRun, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT container_name, k8s_started_at, k8s_finished_at
		 FROM container_state_history
		 WHERE pod_uid = ? AND state = 'terminated'
		 ORDER BY observed_at, id`, podUID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	runs := make(map[string]terminatedRun)
	for rows.Next() {
		var name string
		var run terminatedRun
		if err := rows.Scan(&name, &run.started, &run.finished); err != nil {
			return nil, err
		}
		runs[name] = run
	}
	return runs, rows.Err()
}

// containerWindow is one container's own run, padded. Every bound prefers
// the kubelet's clock (the state history's k8s times, running_since, the
// pod's started_at and created_at) over idios's first_seen_at and
// updated_at: Loki stamps log lines with the kubelet's clock, and a pod
// ingested after it already died would otherwise window on the ingest
// moment and miss every log. The pod's deleted_at caps any open end, since
// a deleted pod cannot still be logging.
func containerWindow(c store.Container, pod store.Pod, run terminatedRun) (grafana.Window, error) {
	var w grafana.Window
	switch c.State {
	case store.StateRunning:
		since, err := clock.Parse(firstOf(c.RunningSince, pod.StartedAt, &pod.CreatedAt))
		if err != nil {
			return grafana.Window{}, err
		}
		w.From = since.Add(-grafana.Pad)
	case store.StateTerminated:
		start, err := clock.Parse(firstOf(run.started, c.RunningSince, pod.StartedAt, &pod.CreatedAt))
		if err != nil {
			return grafana.Window{}, err
		}
		end, err := clock.Parse(firstOf(run.finished, &c.UpdatedAt))
		if err != nil {
			return grafana.Window{}, err
		}
		w.From, w.To = start.Add(-grafana.Pad), end.Add(grafana.Pad)
	default: // waiting: nothing has logged yet, so the pod's birth is the floor
		created, err := clock.Parse(pod.CreatedAt)
		if err != nil {
			return grafana.Window{}, err
		}
		w.From = created.Add(-grafana.Pad)
	}
	if w.To.IsZero() && pod.DeletedAt != nil {
		deleted, err := clock.Parse(*pod.DeletedAt)
		if err != nil {
			return grafana.Window{}, err
		}
		w.To = deleted.Add(grafana.Pad)
	}
	return w, nil
}

// firstOf is the first candidate timestamp that is present and non-empty.
func firstOf(candidates ...*string) string {
	for _, c := range candidates {
		if c != nil && *c != "" {
			return *c
		}
	}
	return ""
}

// strOrEmpty dereferences a nullable string column, empty when absent.
func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
