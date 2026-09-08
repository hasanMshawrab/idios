// Command idios watches Kubernetes clusters for incidents and records them
// in a local SQLite database.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/store"
)

const version = "0.10.0"

const usage = `usage: idios [-config file] [-data-dir dir] [-kubeconfig file] [-listen addr] <command>

commands:
  run                                  watch the configured clusters until SIGINT
  status                               show the data directory and the running process
  mock [-listen addr]                  serve the wire fixtures instead of a daemon
  mcp [-daemon addr]                   serve the recorded incidents to an AI agent over stdio
  cluster add <name> [-context name]   add a cluster row; the context defaults to the name
  ns add <cluster> <namespace>         watch a namespace in a cluster
  version                              print the version
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "idios:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("idios", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	configPath := fs.String("config", "", "path to idios.toml (default <data-dir>/idios.toml)")
	dataDir := fs.String("data-dir", "", "override data_dir")
	kubeconfig := fs.String("kubeconfig", "", "override kubeconfig")
	listen := fs.String("listen", "", "override api_listen")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprint(stderr, usage)
		return errors.New("a command is required")
	}
	if rest[0] == "version" {
		fmt.Fprintln(stdout, "idios", version)
		return nil
	}
	cfg, err := loadConfig(*configPath, *dataDir, *kubeconfig, *listen)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch rest[0] {
	case "run":
		return runDaemon(ctx, cfg)
	case "status":
		return runStatus(ctx, cfg, stdout)
	case "mock":
		return runMock(ctx, cfg, rest[1:], stdout, stderr)
	case "mcp":
		return runMCP(ctx, cfg, rest[1:], stderr)
	case "cluster":
		return runCluster(ctx, cfg, rest[1:], stdout)
	case "ns":
		return runNamespace(ctx, cfg, rest[1:], stdout)
	}
	fmt.Fprint(stderr, usage)
	return fmt.Errorf("unknown command %q", rest[0])
}

// loadConfig reads the TOML file and applies the flag overrides. The file
// defaults to idios.toml inside the data directory the flags select, so
// -data-dir alone points at a self-contained directory.
func loadConfig(configPath, dataDir, kubeconfig, listen string) (config.Config, error) {
	if configPath == "" {
		base := config.DefaultDataDir()
		if dataDir != "" {
			base = dataDir
		}
		configPath = filepath.Join(base, "idios.toml")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return config.Config{}, err
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
		cfg.ArtifactsRoot = filepath.Join(dataDir, "artifacts")
	}
	if kubeconfig != "" {
		cfg.Kubeconfig = kubeconfig
	}
	// A global flag, not one per command: mock, mcp and status all default
	// to api_listen, and a dev daemon dodging a busy port needs them moved
	// together.
	if listen != "" {
		cfg.APIListen = listen
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

// openStore creates the directories, opens the database and migrates it.
func openStore(ctx context.Context, cfg config.Config) (*store.Store, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data_dir: %w", err)
	}
	if err := os.MkdirAll(cfg.ArtifactsRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create artifacts_root: %w", err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(ctx, clock.Real{}); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return st, nil
}
