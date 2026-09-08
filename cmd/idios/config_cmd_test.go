package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/hasanMshawrab/idios/internal/api"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/store"
)

// writeClosedAPIListen points api_listen at an address nothing listens on,
// so the test exercises the database path even when a real idios daemon
// happens to be running on the default port in this environment.
func writeClosedAPIListen(t *testing.T, dir string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	tomlPath := filepath.Join(dir, "idios.toml")
	if err := os.WriteFile(tomlPath, []byte("api_listen = \""+closed+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// clusterAndNamespaceSteps is the command sequence both the database path
// and the daemon path must answer identically, so the two are exercised
// against the same table.
var clusterAndNamespaceSteps = []struct {
	args    []string
	wantOut string
	wantErr string
}{
	{[]string{"cluster", "add", "orbstack"}, "added cluster orbstack (id 1, context orbstack)\n", ""},
	{[]string{"cluster", "add", "orbstack"}, "", `cluster "orbstack" exists (id 1)`},
	{[]string{"cluster", "add", "prod", "-context", "prod-admin"}, "added cluster prod (id 2, context prod-admin)\n", ""},
	{[]string{"cluster", "add"}, "", "usage: idios cluster add <name> [-context name]"},
	{[]string{"ns", "add", "orbstack", "idios-smoke"}, "watching idios-smoke in orbstack\n", ""},
	{[]string{"ns", "add", "orbstack", "idios-smoke"}, "watching idios-smoke in orbstack\n", ""},
	{[]string{"ns", "add", "nowhere", "x"}, "", `no cluster named "nowhere"; add it with idios cluster add`},
	{[]string{"bogus"}, "", `unknown command "bogus"`},
}

func runClusterAndNamespaceSteps(t *testing.T, dir string) {
	t.Helper()
	for _, s := range clusterAndNamespaceSteps {
		var out bytes.Buffer
		err := run(append([]string{"-data-dir", dir}, s.args...), &out, io.Discard)
		gotErr := ""
		if err != nil {
			gotErr = err.Error()
		}
		if gotErr != s.wantErr || out.String() != s.wantOut {
			t.Errorf("%v: out %q err %q; want out %q err %q", s.args, out.String(), gotErr, s.wantOut, s.wantErr)
		}
	}
}

func assertClusterAndNamespaceRows(t *testing.T, w *store.Writer) {
	t.Helper()
	ctx := context.Background()
	var clusters []store.Cluster
	var namespaces []string
	err := w.Tx(ctx, func(tx *sql.Tx) (err error) {
		if clusters, err = store.ListClusters(ctx, tx); err != nil {
			return err
		}
		namespaces, err = store.ListWatchedNamespaces(ctx, tx, 1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	wantClusters := []store.Cluster{
		{ID: 1, Name: "orbstack", ContextName: "orbstack"},
		{ID: 2, Name: "prod", ContextName: "prod-admin"},
	}
	if d := cmp.Diff(wantClusters, clusters, cmpopts.IgnoreFields(store.Cluster{}, "FirstSeenAt")); d != "" {
		t.Error(d)
	}
	if d := cmp.Diff([]string{"idios-smoke"}, namespaces); d != "" {
		t.Error(d)
	}
}

func TestClusterAndNamespaceCommandsWriteRows(t *testing.T) {
	dir := t.TempDir()
	writeClosedAPIListen(t, dir)
	runClusterAndNamespaceSteps(t, dir)
	cfg, err := loadConfig("", dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	assertClusterAndNamespaceRows(t, s.Writer)
}

// The same command table produces the same output and the same rows when a
// daemon answers at api_listen, so a user cannot tell which path ran. The
// API's store lives in a directory the CLI's -data-dir never points at, so
// a CLI that fell through to the database path would write rows nowhere
// this test looks, proving the daemon path is the one that ran.
func TestClusterAndNamespaceCommandsUseTheDaemonWhenItAnswers(t *testing.T) {
	serverDir, cliDir := t.TempDir(), t.TempDir()
	ctx := context.Background()

	serverCfg, err := loadConfig("", serverDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(serverCfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Migrate(ctx, clock.Real{}); err != nil {
		t.Fatal(err)
	}
	srv := api.New(st.Reader.DB(), serverCfg, clock.Real{}, slog.New(slog.DiscardHandler)).WithWriter(st.Writer)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	tomlPath := filepath.Join(cliDir, "idios.toml")
	listen := strings.TrimPrefix(httpSrv.URL, "http://")
	if err := os.WriteFile(tomlPath, []byte("api_listen = \""+listen+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runClusterAndNamespaceSteps(t, cliDir)
	assertClusterAndNamespaceRows(t, st.Writer)

	// The daemon never opened the CLI's own data directory, so a CLI that had
	// fallen through to the database path would have created it here.
	if _, err := os.Stat(filepath.Join(cliDir, "idios.db")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("cli data dir got a database: %v", err)
	}
}
