// Package mcp serves the recorded incidents to an AI agent over the Model
// Context Protocol. It is a read-only client of the daemon's HTTP API on
// the loopback interface and never opens the database or the artifact
// files: the daemon is the only process that touches those.
package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// name and title are what the agent's client lists this server as.
const (
	name  = "idios"
	title = "idios incident recorder"
)

// instructions tell the agent what this server holds and how an
// investigation runs through it, before it has called anything.
const instructions = `idios records Kubernetes pod and job failures on this machine: what the
kubelet said, the events around it, and the container logs captured at the
moment of the failure.

Start at list_incidents or, with an incident id already in hand, at
get_incident. get_incident names the pod and the captured artifacts;
get_pod adds the pod's whole event stream and the pods of the same
controller; read_log reads one captured log a piece at a time;
read_pod_json reads the captured object. Everything is read-only and
every timestamp is UTC.`

// Server answers MCP tool calls out of one daemon.
type Server struct {
	d   *daemon
	mcp *mcp.Server
}

// New builds a server reading the daemon at addr, a host:port on the
// loopback interface.
func New(addr, version string) *Server {
	s := &Server{
		d:   newDaemon(addr),
		mcp: mcp.NewServer(&mcp.Implementation{Name: name, Title: title, Version: version}, &mcp.ServerOptions{Instructions: instructions}),
	}
	s.addTools()
	return s
}

// Run serves the protocol over t until the client disconnects or ctx ends.
func (s *Server) Run(ctx context.Context, t mcp.Transport) error {
	return s.mcp.Run(ctx, t)
}
