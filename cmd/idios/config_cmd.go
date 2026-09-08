package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	sebufhttp "github.com/SebastienMelki/sebuf/http"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/store"
)

var errClusterUsage = errors.New("usage: idios cluster add <name> [-context name]")

// runCluster handles `idios cluster add <name> [-context name]`. The name
// comes before the flags so the common case reads as a sentence.
func runCluster(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) < 2 || args[0] != "add" || strings.HasPrefix(args[1], "-") {
		return errClusterUsage
	}
	name := args[1]
	fs := flag.NewFlagSet("cluster add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	contextName := fs.String("context", "", "kubeconfig context (default: the name)")
	if err := fs.Parse(args[2:]); err != nil {
		return errClusterUsage
	}
	if *contextName == "" {
		*contextName = name
	}
	if c, _, ok := daemonClient(ctx, cfg); ok {
		return addClusterViaDaemon(ctx, c, name, *contextName, stdout)
	}
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	now := clock.Format(clock.Real{}.Now())
	var id int64
	err = st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		existing, err := store.FindClusterByName(ctx, tx, name)
		if err != nil {
			return err
		}
		if existing != nil {
			return fmt.Errorf("cluster %q exists (id %d)", name, existing.ID)
		}
		id, err = store.InsertCluster(ctx, tx, name, *contextName, now)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "added cluster %s (id %d, context %s)\n", name, id, *contextName)
	return nil
}

// addClusterViaDaemon adds the cluster through the API and prints the same
// line the database path prints.
func addClusterViaDaemon(ctx context.Context, c idiosv1.IdiosServiceClient, name, contextName string, stdout io.Writer) error {
	got, err := c.AddCluster(ctx, &idiosv1.AddClusterRequest{ContextName: contextName, Name: name})
	if err != nil {
		return daemonWriteError(err)
	}
	fmt.Fprintf(stdout, "added cluster %s (id %d, context %s)\n", got.GetName(), got.GetId(), got.GetContextName())
	return nil
}

// runNamespace handles `idios ns add <cluster> <namespace>`. Watching an
// already-watched namespace succeeds without a change on either path, so
// scripts can run it again.
func runNamespace(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 3 || args[0] != "add" {
		return errors.New("usage: idios ns add <cluster> <namespace>")
	}
	clusterName, ns := args[1], args[2]
	if c, _, ok := daemonClient(ctx, cfg); ok {
		return addNamespaceViaDaemon(ctx, c, clusterName, ns, stdout)
	}
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	now := clock.Format(clock.Real{}.Now())
	err = st.Writer.Tx(ctx, func(tx *sql.Tx) error {
		c, err := store.FindClusterByName(ctx, tx, clusterName)
		if err != nil {
			return err
		}
		if c == nil {
			return fmt.Errorf("no cluster named %q; add it with idios cluster add", clusterName)
		}
		_, err = store.AddWatchedNamespace(ctx, tx, c.ID, ns, now)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "watching %s in %s\n", ns, clusterName)
	return nil
}

// addNamespaceViaDaemon resolves clusterName to an id through the cluster
// list, the daemon addressing clusters by id, then adds the namespace and
// prints the same line the database path prints.
func addNamespaceViaDaemon(ctx context.Context, c idiosv1.IdiosServiceClient, clusterName, ns string, stdout io.Writer) error {
	list, err := c.ListClusters(ctx, &idiosv1.ListClustersRequest{})
	if err != nil {
		return daemonWriteError(err)
	}
	var id int64
	found := false
	for _, cl := range list.GetClusters() {
		if cl.GetName() == clusterName {
			id, found = cl.GetId(), true
			break
		}
	}
	if !found {
		return fmt.Errorf("no cluster named %q; add it with idios cluster add", clusterName)
	}
	if _, err := c.AddWatchedNamespace(ctx, &idiosv1.AddWatchedNamespaceRequest{Id: id, Name: ns}); err != nil {
		return daemonWriteError(err)
	}
	fmt.Fprintf(stdout, "watching %s in %s\n", ns, clusterName)
	return nil
}

// daemonWriteError turns a validation error from the daemon into its
// violations' descriptions joined by "; ", the same wording the database
// path already raises; any other error is returned as it is.
func daemonWriteError(err error) error {
	var ve *sebufhttp.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	descriptions := make([]string, len(ve.GetViolations()))
	for i, v := range ve.GetViolations() {
		descriptions[i] = v.GetDescription()
	}
	return errors.New(strings.Join(descriptions, "; "))
}
