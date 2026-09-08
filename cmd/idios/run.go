package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/hasanMshawrab/idios/internal/api"
	"github.com/hasanMshawrab/idios/internal/capture"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/incident"
	"github.com/hasanMshawrab/idios/internal/k8s"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/processor"
	"github.com/hasanMshawrab/idios/internal/status"
	"github.com/hasanMshawrab/idios/internal/sweep"
)

var _ k8s.Handler = (*processor.Processor)(nil)

// startComponent runs fn until it returns and, on failure, cancels the
// shared context with that error. A daemon that lost its watcher, capture
// pool or sweeper must not keep running and reporting itself healthy.
func startComponent(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelCauseFunc, log *slog.Logger, name string, fn func(context.Context) error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error(name+" stopped", "err", err)
			cancel(fmt.Errorf("%s: %w", name, err))
		}
	}()
}

// runDaemon watches the configured clusters until ctx ends.
func runDaemon(ctx context.Context, cfg config.Config) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	logger, closeLog, err := newLogger(cfg.DataDir, os.Stderr)
	if err != nil {
		return err
	}
	defer func() { _ = closeLog.Close() }()
	slog.SetDefault(logger)
	ver, err := st.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	logger.Info("idios started",
		"version", version,
		"data_dir", cfg.DataDir,
		"db", cfg.DBPath(),
		"artifacts_root", cfg.ArtifactsRoot,
		"schema_version", ver,
		"kubeconfig", cfg.Kubeconfig,
	)

	counters := status.New(clock.Real{})
	st.Writer.Observer = counters
	pool := capture.New(capture.Config{
		Root: cfg.ArtifactsRoot, TailLines: cfg.LogTailLines, MaxBytes: cfg.LogMaxBytes,
		Workers: cfg.CaptureWorkersPerCluster, QueueSize: cfg.CaptureQueueSize,
		EarlyDebounce: cfg.EarlyCaptureDebounce, StabilizationWindow: cfg.StabilizationWindow,
	}, st.Writer, clock.Real{}, logger)
	events := notify.New()
	proc := processor.New(st.Writer, clock.Real{}, pool, incident.Policy{
		SchedulingGrace: cfg.SchedulingGrace, ProbeGrace: cfg.ProbeGrace, StabilizationWindow: cfg.StabilizationWindow,
	})
	proc.SetNotifier(events)
	sweeper := sweep.New(sweep.Config{
		Root: cfg.ArtifactsRoot, Retention: time.Duration(cfg.RetentionDays) * 24 * time.Hour, Interval: cfg.SweepInterval,
	}, st.Writer, clock.Real{}, logger)
	// The first pass runs before any watcher so the retention promise holds
	// from the first second of the process, not from the first tick.
	if err := sweeper.Sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("startup sweep: %w", err)
	}
	closer := incident.NewCloser(st.Writer, clock.Real{}, cfg.StabilizationWindow, cfg.StuckAfter, cfg.StabilizationCheckInterval, logger)
	closer.SetNotifier(events)

	var wg sync.WaitGroup
	start := func(name string, fn func(context.Context) error) {
		startComponent(ctx, &wg, cancel, logger, name, fn)
	}
	sup := newSupervisor(cfg, st, proc, pool, counters, events, logger)
	start("watchers", sup.run)
	start("capture pool", pool.Run)
	start("sweeper", sweeper.Run)
	start("closer", closer.Run)
	facts := runtimeFacts{sup: sup, pool: pool, closer: closer, counters: counters, clk: clock.Real{}}
	start("api", api.New(st.Reader.DB(), cfg, clock.Real{}, logger).
		WithRuntime(facts).
		WithKube(kubeDiscovery{kubeconfig: cfg.Kubeconfig}).
		WithNotifier(events).
		WithWriter(st.Writer).Run)
	start("status", func(ctx context.Context) error {
		return statusLoop(ctx, cfg, sup, pool, closer, counters, logger)
	})

	<-ctx.Done()
	wg.Wait()
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	logger.Info("idios stopping")
	return nil
}
