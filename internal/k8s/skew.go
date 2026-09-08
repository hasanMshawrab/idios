// Package k8s runs the informers of one cluster and hands every object to a
// Handler. It owns kubeconfig loading, cluster identity, start order,
// reconcile after sync and backoff; what the objects mean is decided
// elsewhere.
package k8s

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
)

const (
	skewWarnAbove = 5 * time.Minute
	skewWarnEvery = time.Hour
)

// Skew keeps the latest difference between the API server's Date header and
// local time. It is reported, never applied: stored timestamps keep the
// clock they came from.
type Skew struct {
	clk clock.Clock
	log *slog.Logger

	mu       sync.Mutex
	offset   time.Duration
	warnedAt time.Time
}

// NewSkew returns a Skew that logs through log when the offset is large.
func NewSkew(clk clock.Clock, log *slog.Logger) *Skew {
	return &Skew{clk: clk, log: log}
}

// Offset returns server time minus local time as of the last response.
func (s *Skew) Offset() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offset
}

type roundTripper struct {
	next http.RoundTripper
	skew *Skew
}

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err == nil {
		r.skew.observe(resp.Header.Get("Date"))
	}
	return resp, err
}

// RoundTripper wraps rt so every response's Date header is observed.
func (s *Skew) RoundTripper(rt http.RoundTripper) http.RoundTripper {
	return roundTripper{next: rt, skew: s}
}

func (s *Skew) observe(date string) {
	if date == "" {
		return
	}
	server, err := http.ParseTime(date)
	if err != nil {
		return
	}
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offset = server.Sub(now)
	// The Date header has whole-second resolution; the warning is only
	// worth a line when the difference could change what a human reads.
	if s.offset.Abs() <= skewWarnAbove {
		return
	}
	if !s.warnedAt.IsZero() && now.Sub(s.warnedAt) < skewWarnEvery {
		return
	}
	s.warnedAt = now
	s.log.Warn("clock skew against api server", "offset", s.offset)
}
