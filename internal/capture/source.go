// Package capture turns capture requests into files under the artifacts root
// and artifacts rows, off the informer goroutines. It owns the queue, the
// GetLogs call and its truncation and error mapping, the file layout, and
// the early-capture cache; what to capture is decided upstream.
package capture

import (
	"context"
	"database/sql"
	"errors"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// LogSource reads one container log stream. The pool never touches a
// clientset directly so tests can script every response.
type LogSource interface {
	Logs(ctx context.Context, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error)
}

// TxRunner runs one write transaction; *store.Writer is the implementation.
type TxRunner interface {
	Tx(ctx context.Context, fn func(*sql.Tx) error) error
}

var errNotConnected = errors.New("cluster not connected")

type clientLogs struct {
	get func() kubernetes.Interface
}

// ClientLogs reads logs through the clientset get returns at call time, so a
// cluster that reconnects with a new clientset is picked up without a
// restart. A nil clientset means no connection yet.
func ClientLogs(get func() kubernetes.Interface) LogSource {
	return clientLogs{get: get}
}

func (c clientLogs) Logs(ctx context.Context, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	client := c.get()
	if client == nil {
		return nil, errNotConnected
	}
	return client.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(ctx)
}
