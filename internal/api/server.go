// Package api implements the generated read endpoints over the read pool and
// maps the query row structs to the wire messages.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	sebufhttp "github.com/SebastienMelki/sebuf/http"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/status"
	"github.com/hasanMshawrab/idios/internal/store"
)

// shutdownGrace is how long in-flight requests have after ctx ends.
const shutdownGrace = 5 * time.Second

// Runtime is what only the running process knows. Without one the server
// answers from the database alone.
type Runtime interface {
	Snapshot() status.Snapshot
}

// KubeContext is one context of a kubeconfig, without its credentials.
type KubeContext struct {
	Name    string
	Cluster string
	Server  string
}

// KubeDiscovery reads the kubeconfig and asks a cluster for its namespaces.
// It never returns credentials. A forbidden result is a fact about the Role
// and not a failure, so it travels beside the names.
type KubeDiscovery interface {
	Contexts(ctx context.Context) ([]KubeContext, error)
	Namespaces(ctx context.Context, contextName string) (names []string, forbidden bool, err error)
}

// Server answers the read endpoints from the read pool and the facts the
// running process holds.
type Server struct {
	db       store.Querier
	cfg      config.Config
	clk      clock.Clock
	runtime  Runtime
	kube     KubeDiscovery
	writer   *store.Writer
	events   *notify.Notifier
	log      *slog.Logger
	stopping chan struct{}
	stopOnce sync.Once
}

// New returns a Server reading db, stamping with clk and answering with
// cfg's limits.
func New(db store.Querier, cfg config.Config, clk clock.Clock, log *slog.Logger) *Server {
	return &Server{db: db, cfg: cfg, clk: clk, log: log, stopping: make(chan struct{})}
}

// WithRuntime gives the server the facts of the process it runs in.
func (s *Server) WithRuntime(r Runtime) *Server {
	s.runtime = r
	return s
}

// WithKube gives the server the kubeconfig reader the add-cluster flow needs.
func (s *Server) WithKube(k KubeDiscovery) *Server {
	s.kube = k
	return s
}

// WithWriter gives the server the seam its write endpoints commit through.
func (s *Server) WithWriter(w *store.Writer) *Server {
	s.writer = w
	return s
}

// WithNotifier gives the server the broadcast its streams subscribe to.
func (s *Server) WithNotifier(n *notify.Notifier) *Server {
	s.events = n
	return s
}

// Handler returns the mux carrying every endpoint of the contract.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// Registration returns nil on every path: a duplicate pattern panics in
	// the mux rather than coming back as an error.
	_ = idiosv1.RegisterIdiosServiceServer(s, idiosv1.WithMux(mux), idiosv1.WithErrorHandler(s.writeError))
	mux.HandleFunc(artifactContentPattern, s.artifactContent)
	mux.HandleFunc(promptPattern, s.incidentPrompt)
	return mux
}

// stopStreams closes the channel every stream selects on, so a handler
// blocked on r.Context() alone still ends: Shutdown does not cancel that
// context, only RegisterOnShutdown's callbacks run while it waits.
func (s *Server) stopStreams() {
	s.stopOnce.Do(func() { close(s.stopping) })
}

// Run serves the endpoints on the configured address until ctx ends.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.APIListen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler()}
	srv.RegisterOnShutdown(s.stopStreams)
	s.log.Info("api listening", "addr", ln.Addr().String())
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	// The parent context is already done, so the grace period needs a fresh
	// one or Shutdown returns immediately.
	grace, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(grace); err != nil {
		return err
	}
	<-served
	return nil
}

// limit applies the configured default to a request that asked for none.
func (s *Server) limit(requested int32) int {
	if requested == 0 {
		return s.cfg.APIListLimit
	}
	return int(requested)
}

// limitViolation reports a limit no page can have, or nil when it is sound.
func limitViolation(limit int32) *sebufhttp.FieldViolation {
	if limit >= 0 {
		return nil
	}
	return &sebufhttp.FieldViolation{Field: "limit", Description: "limit must not be negative"}
}

// validateLimit rejects a limit no page can have.
func validateLimit(limit int32) error {
	v := limitViolation(limit)
	if v == nil {
		return nil
	}
	return &sebufhttp.ValidationError{Violations: []*sebufhttp.FieldViolation{v}}
}

// notFoundError is what a handler returns for an id the database does not
// hold.
type notFoundError struct {
	what string
	id   string
}

// Error names what was looked up and not found.
func (e *notFoundError) Error() string { return e.what + " " + e.id + " not found" }

// ProtoReflect makes the error a proto.Message. The generated handler passes
// an error to the error handler as itself only in that case; anything else
// arrives already flattened into a message and loses its type.
func (e *notFoundError) ProtoReflect() protoreflect.Message {
	return (&sebufhttp.Error{Message: e.Error()}).ProtoReflect()
}

// unconfiguredError is what an endpoint returns when the process did not
// give the server the seam that endpoint needs: the endpoint exists and this
// build cannot answer it, which is what 501 says. Its message names the seam
// because the fault is in how the daemon was built, not in the request.
type unconfiguredError struct {
	what string
}

// Error names the missing seam.
func (e *unconfiguredError) Error() string { return e.what + " not configured" }

// ProtoReflect makes the error a proto.Message. The generated handler passes
// an error to the error handler as itself only in that case; anything else
// arrives already flattened into a message and loses its type.
func (e *unconfiguredError) ProtoReflect() protoreflect.Message {
	return (&sebufhttp.Error{Message: e.Error()}).ProtoReflect()
}

// errNoKubeDiscovery is the answer of the two endpoints that read a
// kubeconfig when the server was built without one.
var errNoKubeDiscovery = &unconfiguredError{what: "kube discovery"}

// errNoWriter is the answer of every write endpoint on a server built
// without a writer.
var errNoWriter = &unconfiguredError{what: "writer"}

// writeError turns a handler error into a status and body: 404 for an unknown
// id, 400 for a bad filter value, 501 for an endpoint this build cannot
// answer, and 500 for everything else. The 500 body never carries the
// underlying message, which would leak SQL text to the client.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) proto.Message {
	var invalid *sebufhttp.ValidationError
	if errors.As(err, &invalid) {
		return nil
	}
	var missing *notFoundError
	if errors.As(err, &missing) {
		return writeStatus(w, r, http.StatusNotFound, missing.Error())
	}
	var unconfigured *unconfiguredError
	if errors.As(err, &unconfigured) {
		return writeStatus(w, r, http.StatusNotImplemented, unconfigured.Error())
	}
	s.log.Error("api request failed", "err", err)
	return writeStatus(w, r, http.StatusInternalServerError, "internal error")
}

// writeStatus answers with code and message. WriteHeader freezes the header map
// and the generated writer sets the content type only after it, so a status
// chosen here has to carry the negotiated type itself or the body is sniffed
// as text.
func writeStatus(w http.ResponseWriter, r *http.Request, code int, message string) proto.Message {
	w.Header().Set("Content-Type", responseContentType(r))
	w.WriteHeader(code)
	return &sebufhttp.Error{Message: message}
}

// responseContentType repeats the negotiation the generated writer does, so
// the header this package sets and the body that writer marshals agree: the
// Accept header decides, a wildcard or absent one falls back to the request's
// own type, and anything else is JSON.
func responseContentType(r *http.Request) string {
	switch accept := mediaType(r.Header.Get("Accept")); accept {
	case idiosv1.BinaryContentType, idiosv1.ProtoContentType:
		return accept
	case "", "*/*":
		switch ct := mediaType(r.Header.Get("Content-Type")); ct {
		case idiosv1.BinaryContentType, idiosv1.ProtoContentType:
			return ct
		}
	}
	return idiosv1.JSONContentType
}

// mediaType drops the parameters and any second entry of a content type
// header, leaving the bare type the generated writer matches on.
func mediaType(header string) string {
	if i := strings.IndexAny(header, "; "); i >= 0 {
		return header[:i]
	}
	return header
}
