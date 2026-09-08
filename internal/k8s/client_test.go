package k8s

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/hasanMshawrab/idios/internal/clock"
)

const kubeconfigFor = `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: %s
    insecure-skip-tls-verify: true
contexts:
- name: orbstack
  context:
    cluster: c
    user: u
users:
- name: u
  user:
    token: t
`

func writeKubeconfig(t *testing.T, server string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte(fmt.Sprintf(kubeconfigFor, server)), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEmptyKubeconfigPathUsesTheDefaultResolution(t *testing.T) {
	fromEnv := writeKubeconfig(t, "https://env.invalid:6443")
	explicit := writeKubeconfig(t, "https://explicit.invalid:6443")
	t.Setenv("KUBECONFIG", fromEnv)
	cases := []struct {
		name string
		path string
		want string
	}{
		{"empty path follows KUBECONFIG", "", "https://env.invalid:6443"},
		{"explicit path wins over KUBECONFIG", explicit, "https://explicit.invalid:6443"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, host, err := KubeconfigClient(c.path, "orbstack", NewSkew(clock.NewFake(testNow), slog.New(slog.NewTextHandler(io.Discard, nil))))()
			if err != nil {
				t.Fatal(err)
			}
			if host != c.want {
				t.Fatalf("host = %q, want %q", host, c.want)
			}
		})
	}
}
