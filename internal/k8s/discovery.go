package k8s

import (
	"cmp"
	"context"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubeContext is one context of a kubeconfig, without its credentials.
type KubeContext struct {
	Name    string
	Cluster string
	Server  string
}

// loadingRules reads the kubeconfig at path, or the usual places
// (KUBECONFIG, then the home directory) when path is empty.
func loadingRules(path string) clientcmd.ClientConfigLoader {
	if path == "" {
		return clientcmd.NewDefaultClientConfigLoadingRules()
	}
	return &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
}

// restConfig resolves contextName in that kubeconfig to a connection.
func restConfig(path, contextName string) (*rest.Config, error) {
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules(path), overrides).ClientConfig()
}

// ListContexts returns the contexts of the kubeconfig, sorted by name.
func ListContexts(kubeconfig string) ([]KubeContext, error) {
	raw, err := loadingRules(kubeconfig).Load()
	if err != nil {
		return nil, err
	}
	out := make([]KubeContext, 0, len(raw.Contexts))
	for name, c := range raw.Contexts {
		kc := KubeContext{Name: name, Cluster: c.Cluster}
		if cluster, ok := raw.Clusters[c.Cluster]; ok {
			kc.Server = cluster.Server
		}
		out = append(out, kc)
	}
	slices.SortFunc(out, func(a, b KubeContext) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

// ListNamespaces asks the cluster of contextName for its namespace names,
// sorted. forbidden is true when the credentials may not list them, which
// is a fact about the Role and not a failure.
func ListNamespaces(ctx context.Context, kubeconfig, contextName string) (names []string, forbidden bool, err error) {
	cfg, err := restConfig(kubeconfig, contextName)
	if err != nil {
		return nil, false, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, false, err
	}
	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if apierrors.IsForbidden(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	for _, ns := range list.Items {
		names = append(names, ns.Name)
	}
	slices.Sort(names)
	return names, false, nil
}
