package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
)

func TestStartComponentCancelsOnlyOnRealFailure(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		err       error
		wantCause error
		wantText  string
	}{
		{name: "clean stop", err: nil},
		{name: "shutdown", err: context.Canceled},
		{name: "wrapped shutdown", err: fmt.Errorf("watch: %w", context.Canceled)},
		{name: "failure", err: boom, wantCause: boom, wantText: "watchers: boom"},
		{name: "wrapped failure", err: fmt.Errorf("load clusters: %w", boom), wantCause: boom, wantText: "watchers: load clusters: boom"},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			var wg sync.WaitGroup
			startComponent(ctx, &wg, cancel, log, "watchers", func(context.Context) error { return tt.err })
			wg.Wait()
			cause := context.Cause(ctx)
			if tt.wantCause == nil {
				if cause != nil {
					t.Fatalf("cause = %v, want the daemon left running", cause)
				}
				return
			}
			if !errors.Is(cause, tt.wantCause) {
				t.Fatalf("cause = %v, want it to wrap %v", cause, tt.wantCause)
			}
			if cause.Error() != tt.wantText {
				t.Errorf("cause = %q, want %q", cause.Error(), tt.wantText)
			}
		})
	}
}
