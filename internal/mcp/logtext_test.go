package mcp

import (
	"strings"
	"testing"
)

// A captured log costs the agent context by the byte, so read_log cuts it:
// the last lines when nothing is asked for, a byte range, or the matching
// lines, and the whole file only when the call says so.
func TestTrimCutsOnlyWhatWasAskedFor(t *testing.T) {
	long := strings.Repeat("noise\n", 250)
	cases := []struct {
		name        string
		body        string
		sel         selection
		out         string
		showing     string
		wantErrText string
	}{
		{name: "an empty file has no lines", body: "", out: "", showing: "the whole file, 0 lines"},
		{name: "a file with no final newline keeps its last line", body: "a\nb", sel: selection{tailLines: 1},
			out: "b\n", showing: "the last 1 of 2 lines"},
		{name: "a tail longer than the file is the file", body: "a\nb\n", sel: selection{tailLines: 9},
			out: "a\nb\n", showing: "the whole file, 2 lines"},
		{name: "a range past the end stops at the end", body: "abcde", sel: selection{rangeStart: 3, rangeEnd: 900},
			out: "de", showing: "bytes 3 to 5 of 5"},
		{name: "a range without an end reads to the end", body: "abcde", sel: selection{rangeStart: 2},
			out: "cde", showing: "bytes 2 to 5 of 5"},
		{name: "a range without a start reads from the start", body: "abcde", sel: selection{rangeEnd: 2},
			out: "ab", showing: "bytes 0 to 2 of 5"},
		{name: "a grep that matches nothing returns nothing", body: "a\nb\n", sel: selection{grep: "zz"},
			out: "", showing: `no line matching "zz"`},
		{name: "a grep returns the matching lines in order", body: "one\ntwo\nthree\n", sel: selection{grep: "^t"},
			out: "two\nthree\n", showing: `2 lines matching "^t"`},
		{name: "a grep matching everything is capped", body: long, sel: selection{grep: "noise"},
			out: strings.Repeat("noise\n", grepLimit), showing: `the first 200 of 250 lines matching "noise"`},
		{name: "no selection is the tail", body: long,
			out: strings.Repeat("noise\n", defaultTailLines), showing: "the last 200 of 250 lines"},
		{name: "whole is the whole file", body: long, sel: selection{whole: true},
			out: long, showing: "the whole file"},
		{name: "a start past the end is refused", body: "abcde", sel: selection{rangeStart: 6},
			wantErrText: "range_start 6 is past the end of the file, which holds 5 bytes"},
		{name: "an empty range is refused", body: "abcde", sel: selection{rangeStart: 3, rangeEnd: 3},
			wantErrText: "range_end 3 must be greater than range_start 3"},
		{name: "a negative offset is refused", body: "abcde", sel: selection{rangeStart: -1},
			wantErrText: "range_start and range_end must not be negative"},
		{name: "an unparsable pattern is refused", body: "abcde", sel: selection{grep: "("},
			wantErrText: "grep \"(\" is not a valid regular expression: error parsing regexp: missing closing ): `(`"},
		{name: "two cuts at once are refused", body: "abcde", sel: selection{tailLines: 1, whole: true},
			wantErrText: errOneSelection.Error()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, showing, err := trim([]byte(c.body), c.sel)
			if c.wantErrText != "" {
				if err == nil || err.Error() != c.wantErrText {
					t.Fatalf("trim error = %v, want %q", err, c.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != c.out {
				t.Errorf("trim = %q, want %q", out, c.out)
			}
			if showing != c.showing {
				t.Errorf("showing = %q, want %q", showing, c.showing)
			}
		})
	}
}
