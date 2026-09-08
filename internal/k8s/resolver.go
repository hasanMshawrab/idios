package k8s

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
)

// listerResolver answers owner lookups from the informer stores of each
// watched namespace. It is filled before any informer starts and read-only
// afterwards, so it needs no lock.
type listerResolver struct {
	rs   map[string]appslisters.ReplicaSetNamespaceLister
	jobs map[string]batchlisters.JobNamespaceLister
}

func newListerResolver() *listerResolver {
	return &listerResolver{rs: map[string]appslisters.ReplicaSetNamespaceLister{}, jobs: map[string]batchlisters.JobNamespaceLister{}}
}

func (r *listerResolver) add(ns string, rs appslisters.ReplicaSetNamespaceLister, jobs batchlisters.JobNamespaceLister) {
	r.rs[ns] = rs
	r.jobs[ns] = jobs
}

// ReplicaSetOwner returns the ReplicaSet's controller, or nil when the store
// does not hold it or it has none.
func (r *listerResolver) ReplicaSetOwner(namespace, name string) *metav1.OwnerReference {
	l, ok := r.rs[namespace]
	if !ok {
		return nil
	}
	rs, err := l.Get(name)
	if err != nil {
		return nil
	}
	return metav1.GetControllerOf(rs)
}

// JobOwner returns the Job's controller, or nil when the store does not hold
// it or it has none.
func (r *listerResolver) JobOwner(namespace, name string) *metav1.OwnerReference {
	l, ok := r.jobs[namespace]
	if !ok {
		return nil
	}
	job, err := l.Get(name)
	if err != nil {
		return nil
	}
	return metav1.GetControllerOf(job)
}
