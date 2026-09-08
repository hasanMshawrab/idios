package k8s

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestIdentityIsNullWhenForbidden(t *testing.T) {
	kubeSystem := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "ks-uid"}}
	nsResource := schema.GroupResource{Resource: "namespaces"}
	cases := []struct {
		name    string
		react   error
		want    *string
		wantErr bool
	}{
		{"readable", nil, ptr("ks-uid"), false},
		{"forbidden", apierrors.NewForbidden(nsResource, "kube-system", errors.New("no")), nil, false},
		{"unreachable", apierrors.NewInternalError(errors.New("boom")), nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fake.NewClientset(kubeSystem)
			if c.react != nil {
				client.PrependReactor("get", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, c.react })
			}
			got, err := clusterIdentity(context.Background(), client)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Fatalf("identity = %v, want %v", deref(got), deref(c.want))
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
