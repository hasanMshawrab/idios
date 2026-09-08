package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// defaultTailLines is what read_log returns when the caller names no
// selection: the point of the tool is to keep a large log out of the
// agent's context, so the whole file is never the default.
const defaultTailLines = 200

// grepLimit caps the matching lines one grep returns; a pattern that
// matches everything would otherwise return the whole file.
const grepLimit = 200

// errOneSelection is raised when a call names more than one way to cut the
// file, which has no single meaning.
var errOneSelection = errors.New("read_log takes one of tail_lines, range_start/range_end, grep or whole")

// selection is the part of an artifact one read_log call asks for.
type selection struct {
	tailLines  int
	rangeStart int64
	rangeEnd   int64
	grep       string
	whole      bool
}

// named reports how many ways of cutting the file the call asked for.
func (s selection) named() int {
	n := 0
	for _, on := range []bool{s.tailLines > 0, s.rangeStart != 0 || s.rangeEnd != 0, s.grep != "", s.whole} {
		if on {
			n++
		}
	}
	return n
}

// trim applies the selection to body and returns the bytes to show and the
// one-line description of what was cut, for the caller to read back.
func trim(body []byte, s selection) ([]byte, string, error) {
	switch {
	case s.named() > 1:
		return nil, "", errOneSelection
	case s.whole:
		return body, "the whole file", nil
	case s.grep != "":
		return grepLines(body, s.grep)
	case s.rangeStart != 0 || s.rangeEnd != 0:
		return byteRange(body, s.rangeStart, s.rangeEnd)
	case s.tailLines > 0:
		return tail(body, s.tailLines)
	}
	return tail(body, defaultTailLines)
}

// tail returns the last n lines of body.
func tail(body []byte, n int) ([]byte, string, error) {
	lines := splitLines(body)
	if len(lines) <= n {
		return body, fmt.Sprintf("the whole file, %s", plural(len(lines), "line")), nil
	}
	cut := bytes.Join(lines[len(lines)-n:], []byte("\n"))
	return append(cut, '\n'), fmt.Sprintf("the last %d of %s", n, plural(len(lines), "line")), nil
}

// byteRange returns body[start:end); an end of zero means the end of the
// file, and an end past it is the end of the file.
func byteRange(body []byte, start, end int64) ([]byte, string, error) {
	size := int64(len(body))
	if start < 0 || end < 0 {
		return nil, "", errors.New("range_start and range_end must not be negative")
	}
	if start > size {
		return nil, "", fmt.Errorf("range_start %d is past the end of the file, which holds %s", start, plural(int(size), "byte"))
	}
	if end == 0 || end > size {
		end = size
	}
	if end <= start {
		return nil, "", fmt.Errorf("range_end %d must be greater than range_start %d", end, start)
	}
	return body[start:end], fmt.Sprintf("bytes %d to %d of %d", start, end, size), nil
}

// grepLines returns the lines of body matching pattern, newest lines last
// as they were captured.
func grepLines(body []byte, pattern string) ([]byte, string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, "", fmt.Errorf("grep %s is not a valid regular expression: %w", strconv.Quote(pattern), err)
	}
	var matched [][]byte
	total := 0
	for _, line := range splitLines(body) {
		if !re.Match(line) {
			continue
		}
		total++
		if len(matched) < grepLimit {
			matched = append(matched, line)
		}
	}
	if total == 0 {
		return nil, fmt.Sprintf("no line matching %s", strconv.Quote(pattern)), nil
	}
	out := append(bytes.Join(matched, []byte("\n")), '\n')
	if total > len(matched) {
		return out, fmt.Sprintf("the first %d of %s matching %s", len(matched), plural(total, "line"), strconv.Quote(pattern)), nil
	}
	return out, fmt.Sprintf("%s matching %s", plural(total, "line"), strconv.Quote(pattern)), nil
}

// splitLines splits body on newlines. A file ending in a newline does not
// end in an empty line, and an empty file holds no lines at all.
func splitLines(body []byte) [][]byte {
	if len(body) == 0 {
		return nil
	}
	return bytes.Split(bytes.TrimSuffix(body, []byte("\n")), []byte("\n"))
}

// plural renders a count with its noun, so a message reads "1 line".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
