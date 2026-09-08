package k8s

import (
	"k8s.io/client-go/kubernetes"
)

// ClientFunc builds a clientset and reports the API server URL. It runs on
// every connection attempt so an edited kubeconfig is picked up on retry.
type ClientFunc func() (kubernetes.Interface, string, error)

// KubeconfigClient loads contextName from the kubeconfig at path, or from
// the usual places (KUBECONFIG, then the home directory) when path is
// empty, and routes every response through skew.
func KubeconfigClient(path, contextName string, skew *Skew) ClientFunc {
	return func() (kubernetes.Interface, string, error) {
		cfg, err := restConfig(path, contextName)
		if err != nil {
			return nil, "", err
		}
		cfg.Wrap(skew.RoundTripper)
		client, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		return client, cfg.Host, nil
	}
}
