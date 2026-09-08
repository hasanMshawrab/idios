package clock

import (
	"testing"
	"time"
)

func TestFormatIsFixedWidthUTC(t *testing.T) {
	plus4 := time.FixedZone("plus4", 4*3600)
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"microseconds", time.Date(2026, 8, 26, 14, 3, 11, 482913000, time.UTC), "2026-08-26T14:03:11.482913Z"},
		{"whole seconds padded", time.Date(2026, 8, 26, 14, 3, 11, 0, time.UTC), "2026-08-26T14:03:11.000000Z"},
		{"non-UTC converted", time.Date(2026, 8, 26, 18, 3, 11, 0, plus4), "2026-08-26T14:03:11.000000Z"},
		{"nanoseconds truncated", time.Date(2026, 8, 26, 14, 3, 11, 482913999, time.UTC), "2026-08-26T14:03:11.482913Z"},
	}
	for _, c := range cases {
		if got := Format(c.in); got != c.want {
			t.Errorf("%s: Format = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseAcceptsOnlyLayout(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"2026-08-26T14:03:11.482913Z", time.Date(2026, 8, 26, 14, 3, 11, 482913000, time.UTC), true},
		{"2026-08-26T14:03:11.000000Z", time.Date(2026, 8, 26, 14, 3, 11, 0, time.UTC), true},
		{"2026-08-26T14:03:11Z", time.Time{}, false},
		{"2026-08-26T14:03:11.482Z", time.Time{}, false},
		{"2026-08-26T14:03:11.482913+00:00", time.Time{}, false},
		{"2026-08-26 14:03:11.482913Z", time.Time{}, false},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if (err == nil) != c.ok {
			t.Errorf("Parse(%q) err = %v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && (!got.Equal(c.want) || got.Location() != time.UTC) {
			t.Errorf("Parse(%q) = %v, want %v in UTC", c.in, got, c.want)
		}
	}
}
