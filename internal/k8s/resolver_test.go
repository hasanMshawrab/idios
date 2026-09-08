package k8s

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
)

func TestResolverReadsControllerFromListers(t *testing.T) {
	yes := true
	owned := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "web-7d9f8c6b5", Namespace: "idios-smoke",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "web", UID: "dep-web", Controller: &yes}}}}
	bare := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "idios-smoke"}}
	cronOwned := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "import-28812346", Namespace: "idios-smoke",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: "import", UID: "cj-import", Controller: &yes}}}}
	rsIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	jobIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, o := range []any{owned, bare} {
		if err := rsIndexer.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	if err := jobIndexer.Add(cronOwned); err != nil {
		t.Fatal(err)
	}
	r := newListerResolver()
	r.add("idios-smoke", appslisters.NewReplicaSetLister(rsIndexer).ReplicaSets("idios-smoke"), batchlisters.NewJobLister(jobIndexer).Jobs("idios-smoke"))

	cases := []struct {
		name string
		got  *metav1.OwnerReference
		want *metav1.OwnerReference
	}{
		{"deployment-owned replicaset", r.ReplicaSetOwner("idios-smoke", "web-7d9f8c6b5"), &owned.OwnerReferences[0]},
		{"bare replicaset", r.ReplicaSetOwner("idios-smoke", "standalone"), nil},
		{"unknown replicaset", r.ReplicaSetOwner("idios-smoke", "nope"), nil},
		{"unwatched namespace", r.ReplicaSetOwner("payments", "web-7d9f8c6b5"), nil},
		{"cronjob-owned job", r.JobOwner("idios-smoke", "import-28812346"), &cronOwned.OwnerReferences[0]},
		{"unknown job", r.JobOwner("idios-smoke", "nope"), nil},
	}
	for _, c := range cases {
		if d := cmp.Diff(c.want, c.got); d != "" {
			t.Errorf("%s: %s", c.name, d)
		}
	}
}
