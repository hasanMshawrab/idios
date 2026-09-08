package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/hasanMshawrab/idios/internal/api/mock"
	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/config"
)

// mockShutdownGrace is how long in-flight requests have after ctx ends.
const mockShutdownGrace = 5 * time.Second

// mockContentPattern is the artifact content endpoint, which is outside the
// proto contract and so outside the generated registration.
const mockContentPattern = "GET /v1/artifacts/{id}/content"

// mockPromptPattern is the prompt endpoint, outside the proto contract for
// the same reason.
const mockPromptPattern = "GET /v1/incidents/{id}/prompt"

// runMock serves the committed wire fixtures, so the application can be
// built and demonstrated against a stable target before a daemon has
// recorded anything. It reaches no cluster and writes nothing to the data
// directory, which is read only for idios.toml and the address in it.
func runMock(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("idios mock", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", cfg.APIListen, "address to serve the fixtures on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, "idios mock listening on", ln.Addr().String())
	return serveMock(ctx, ln)
}

// serveMock serves the fixtures on ln until ctx ends.
func serveMock(ctx context.Context, ln net.Listener) error {
	mux := http.NewServeMux()
	if err := idiosv1.RegisterIdiosServiceServer(mock.New(), idiosv1.WithMux(mux)); err != nil {
		return err
	}
	mux.HandleFunc(mockContentPattern, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, mock.Content)
	})
	mux.HandleFunc(mockPromptPattern, func(w http.ResponseWriter, r *http.Request) {
		text, ok := mock.Prompt(r.URL.Query().Get("mode"))
		if !ok {
			http.Error(w, "mode must be mcp or snapshot", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(text)))
		_, _ = io.WriteString(w, text)
	})
	return serve(ctx, &http.Server{Handler: mux}, ln)
}

// serve runs srv on ln until ctx ends, then lets in-flight requests finish.
func serve(ctx context.Context, srv *http.Server, ln net.Listener) error {
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
	grace, cancel := context.WithTimeout(context.WithoutCancel(ctx), mockShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(grace); err != nil {
		return err
	}
	<-served
	return nil
}
