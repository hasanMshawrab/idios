// Package clock is the only source of process time and the only place the
// on-disk timestamp layout is spelled.
package clock

import (
	"sync"
	"time"
)

// Layout is fixed width so text order equals time order; a column mixing
// "...11Z" and "...11.482913Z" would sort the later value first.
const Layout = "2006-01-02T15:04:05.000000Z"

// Clock is the source of process time; Real in production, Fake in tests.
type Clock interface {
	Now() time.Time
}

// Real reads the system clock.
type Real struct{}

// Now reports the system time.
func (Real) Now() time.Time { return time.Now() }

// Fake is a settable clock for tests.
type Fake struct {
	mu sync.Mutex
	t  time.Time
}

// NewFake returns a Fake reporting t.
func NewFake(t time.Time) *Fake { return &Fake{t: t} }

// Now reports the current fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// Set moves the fake time to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t
}

// Advance moves the fake time forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

// Format renders t in Layout.
func Format(t time.Time) string {
	return t.UTC().Format(Layout)
}

// Parse reads a value written by Format; any other layout is an error.
func Parse(s string) (time.Time, error) {
	t, err := time.Parse(Layout, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
