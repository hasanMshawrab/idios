package k8s

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// clusterIdentity returns the kube-system namespace uid, or nil when RBAC
// hides it; the cluster row is then matched on its URL instead.
func clusterIdentity(ctx context.Context, client kubernetes.Interface) (*string, error) {
	ns, err := client.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if apierrors.IsForbidden(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	uid := string(ns.UID)
	return &uid, nil
}
