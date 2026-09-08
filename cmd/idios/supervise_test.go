package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hasanMshawrab/idios/internal/store"
)

func TestReloadPlanRestartsOnlyChangedClusters(t *testing.T) {
	running := map[int64]clusterSpec{
		1: {contextName: "orbstack", namespaces: []string{"a", "b"}},
		2: {contextName: "prod", namespaces: []string{"payments"}},
		3: {contextName: "old", namespaces: []string{"x"}},
	}
	cases := []struct {
		name       string
		clusters   []store.Cluster
		namespaces map[int64][]string
		wantStart  []int64
		wantStop   []int64
	}{
		{"nothing changed, namespaces in another order", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod"}, {ID: 3, ContextName: "old"}},
			map[int64][]string{1: {"b", "a"}, 2: {"payments"}, 3: {"x"}}, nil, nil},
		{"namespace added restarts that cluster", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod"}, {ID: 3, ContextName: "old"}},
			map[int64][]string{1: {"a", "b", "c"}, 2: {"payments"}, 3: {"x"}}, []int64{1}, []int64{1}},
		{"context changed restarts that cluster", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod-admin"}, {ID: 3, ContextName: "old"}},
			map[int64][]string{1: {"a", "b"}, 2: {"payments"}, 3: {"x"}}, []int64{2}, []int64{2}},
		{"new cluster starts, removed cluster stops", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod"}, {ID: 4, ContextName: "new"}},
			map[int64][]string{1: {"a", "b"}, 2: {"payments"}, 4: {"y"}}, []int64{4}, []int64{3}},
		{"cluster with no namespaces still runs", []store.Cluster{{ID: 1, ContextName: "orbstack"}, {ID: 2, ContextName: "prod"}, {ID: 3, ContextName: "old"}, {ID: 5, ContextName: "bare"}},
			map[int64][]string{1: {"a", "b"}, 2: {"payments"}, 3: {"x"}}, []int64{5}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, stop := reloadPlan(running, specsFromRows(c.clusters, c.namespaces))
			if d := cmp.Diff(c.wantStart, start); d != "" {
				t.Error("start:", d)
			}
			if d := cmp.Diff(c.wantStop, stop); d != "" {
				t.Error("stop:", d)
			}
		})
	}
}
