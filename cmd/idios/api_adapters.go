package main

import (
	"context"

	"github.com/hasanMshawrab/idios/internal/api"
	"github.com/hasanMshawrab/idios/internal/capture"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/k8s"
	"github.com/hasanMshawrab/idios/internal/status"
)

// runtimeFacts answers the API from the same parts the status file is
// written from, so status.json and GET /v1/status never disagree.
type runtimeFacts struct {
	sup      *supervisor
	pool     *capture.Pool
	closer   *incident.Closer
	counters *status.Counters
	clk      clock.Clock
}

// Snapshot reports what the running process knows this instant.
func (r runtimeFacts) Snapshot() status.Snapshot {
	return snapshot(r.sup, r.pool, r.closer, r.counters, r.clk.Now())
}

// kubeDiscovery reads the configured kubeconfig for the add-cluster flow.
type kubeDiscovery struct {
	kubeconfig string
}

// Contexts lists the contexts of the kubeconfig.
func (k kubeDiscovery) Contexts(context.Context) ([]api.KubeContext, error) {
	found, err := k8s.ListContexts(k.kubeconfig)
	if err != nil {
		return nil, err
	}
	out := make([]api.KubeContext, 0, len(found))
	for _, c := range found {
		out = append(out, api.KubeContext{Name: c.Name, Cluster: c.Cluster, Server: c.Server})
	}
	return out, nil
}

// Namespaces asks the cluster of contextName what namespaces it has.
func (k kubeDiscovery) Namespaces(ctx context.Context, contextName string) ([]string, bool, error) {
	return k8s.ListNamespaces(ctx, k.kubeconfig, contextName)
}
