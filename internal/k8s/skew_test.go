package k8s

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hasanMshawrab/idios/internal/clock"
)

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSkewKeepsLatestOffsetAndWarnsHourly(t *testing.T) {
	var logs bytes.Buffer
	clk := clock.NewFake(testNow)
	s := NewSkew(clk, slog.New(slog.NewTextHandler(&logs, nil)))
	var header string
	rt := s.RoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		h := http.Header{}
		if header != "" {
			h.Set("Date", header)
		}
		return &http.Response{StatusCode: 200, Header: h}, nil
	}))
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	steps := []struct {
		name       string
		advance    time.Duration
		date       string
		wantOffset time.Duration
		wantWarns  int
	}{
		{"no date header", 0, "", 0, 0},
		{"ten minutes ahead warns", 0, testNow.Add(10 * time.Minute).UTC().Format(http.TimeFormat), 10 * time.Minute, 1},
		{"still ahead a minute later stays quiet", time.Minute, testNow.Add(11 * time.Minute).UTC().Format(http.TimeFormat), 10 * time.Minute, 1},
		{"garbage date leaves the value", 0, "not a date", 10 * time.Minute, 1},
		{"an hour later warns again", time.Hour, testNow.Add(time.Hour + time.Minute - 6*time.Minute).UTC().Format(http.TimeFormat), -6 * time.Minute, 2},
		{"back in tolerance", time.Hour, testNow.Add(2*time.Hour + time.Minute + 30*time.Second).UTC().Format(http.TimeFormat), 30 * time.Second, 2},
	}
	for _, st := range steps {
		clk.Advance(st.advance)
		header = st.date
		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		_ = resp
		if got := s.Offset(); got != st.wantOffset {
			t.Errorf("%s: offset = %v, want %v", st.name, got, st.wantOffset)
		}
		if got := strings.Count(logs.String(), "clock skew"); got != st.wantWarns {
			t.Errorf("%s: warnings = %d, want %d\n%s", st.name, got, st.wantWarns, logs.String())
		}
	}
}
