package main

import (
	"context"
	"flag"
	"io"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/mcp"
)

// runMCP serves the read-only tool vocabulary over stdio, for an AI agent
// the user starts it from. It is a client of the daemon's HTTP API and
// opens nothing in the data directory, which is read only for the address
// the daemon listens on.
func runMCP(ctx context.Context, cfg config.Config, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("idios mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	daemon := fs.String("daemon", cfg.APIListen, "address of the daemon to read")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return mcp.New(*daemon, version).Run(ctx, &sdk.StdioTransport{})
}
