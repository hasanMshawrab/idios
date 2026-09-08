// Package grafana builds Grafana Explore URLs for a cluster's configured
// Loki datasource. It is pure: no store, no clock, no network.
package grafana

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultSelector is the LogQL template a newly built label set starts from.
const DefaultSelector = `{namespace="$namespace", pod="$pod", container="$container"}`

// Pad is added to both ends of every window, absorbing clock skew and Loki
// ingest lag.
const Pad = 5 * time.Minute

// Config is one cluster's Grafana + Loki configuration.
type Config struct {
	BaseURL       string
	DatasourceUID string
	Selector      string
}

// Values fills the placeholders of a Selector for one link.
type Values struct {
	Namespace string
	Pod       string
	Container string
	Workload  string
	Node      string
	Cluster   string
}

// Window is the time range a link covers. A zero To means the open literal
// "now".
type Window struct {
	From time.Time
	To   time.Time
}

// dsRef names a Grafana datasource by type and uid.
type dsRef struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

// exploreQuery is the one Loki query a pane runs.
type exploreQuery struct {
	RefID      string `json:"refId"`
	Expr       string `json:"expr"`
	QueryType  string `json:"queryType"`
	Datasource dsRef  `json:"datasource"`
	Direction  string `json:"direction"`
}

// exploreRange is a pane's time range, each bound a decimal epoch
// millisecond string or the literal "now".
type exploreRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// logsPanelState requests the log list sorted newest first.
type logsPanelState struct {
	SortOrder string `json:"sortOrder"`
}

// panelsState carries per-panel-type Explore UI state.
type panelsState struct {
	Logs logsPanelState `json:"logs"`
}

// pane is one Explore split pane.
type pane struct {
	Queries     []exploreQuery `json:"queries"`
	Range       exploreRange   `json:"range"`
	PanelsState panelsState    `json:"panelsState"`
	Compact     bool           `json:"compact"`
}

// placeholders lists the six substitution names in the order they are
// checked; order does not affect the result since they are distinct tokens.
var placeholders = []struct {
	token string
	value func(Values) string
}{
	{"$namespace", func(v Values) string { return v.Namespace }},
	{"$pod", func(v Values) string { return v.Pod }},
	{"$container", func(v Values) string { return v.Container }},
	{"$workload", func(v Values) string { return v.Workload }},
	{"$node", func(v Values) string { return v.Node }},
	{"$cluster", func(v Values) string { return v.Cluster }},
}

// ExploreURL builds the Grafana Explore URL for one link: cfg.Selector
// substituted with v, scoped to the Loki datasource, over window w.
func ExploreURL(cfg Config, v Values, w Window) string {
	expr := substitute(cfg.Selector, v)
	p := pane{
		Queries: []exploreQuery{{
			RefID:      "A",
			Expr:       expr,
			QueryType:  "range",
			Datasource: dsRef{Type: "loki", UID: cfg.DatasourceUID},
			Direction:  "backward",
		}},
		Range:       exploreRange{From: epochMillis(w.From), To: rangeTo(w.To)},
		PanelsState: panelsState{Logs: logsPanelState{SortOrder: "Descending"}},
		Compact:     false,
	}
	// Marshaled from a struct, so key order is fixed regardless of Go's
	// randomized map iteration.
	panesJSON, err := json.Marshal(map[string]pane{"a": p})
	if err != nil {
		// pane holds only strings, bools and a slice of the same; Marshal
		// cannot fail on it.
		panic(err)
	}
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	return base + "/explore?schemaVersion=1&panes=" + url.QueryEscape(string(panesJSON))
}

// epochMillis renders t as a decimal epoch millisecond string.
func epochMillis(t time.Time) string {
	return strconv.FormatInt(t.UnixMilli(), 10)
}

// rangeTo renders an open end as Grafana's literal "now" so a served URL is
// never stale for an incident or run still going.
func rangeTo(t time.Time) string {
	if t.IsZero() {
		return "now"
	}
	return epochMillis(t)
}

// substitute fills selector's placeholders from v, dropping whole any
// comma-separated matcher whose placeholder substitutes to the empty
// string: container="" in LogQL matches streams without that label, which
// is never the intent of an unset value.
func substitute(selector string, v Values) string {
	body := strings.TrimSuffix(strings.TrimPrefix(selector, "{"), "}")
	parts := strings.Split(body, ",")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if droppedByEmptyPlaceholder(part, v) {
			continue
		}
		kept = append(kept, fillPlaceholders(part, v))
	}
	return "{" + strings.Join(kept, ", ") + "}"
}

// droppedByEmptyPlaceholder reports whether matcher names a placeholder
// whose value in v is empty.
func droppedByEmptyPlaceholder(matcher string, v Values) bool {
	for _, ph := range placeholders {
		if strings.Contains(matcher, ph.token) && ph.value(v) == "" {
			return true
		}
	}
	return false
}

// fillPlaceholders replaces every placeholder token in s with its escaped
// value from v.
func fillPlaceholders(s string, v Values) string {
	pairs := make([]string, 0, len(placeholders)*2)
	for _, ph := range placeholders {
		pairs = append(pairs, ph.token, escapeQuotes(ph.value(v)))
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// escapeQuotes backslash-escapes a double quote so a substituted value
// cannot break out of the LogQL matcher's quoted string.
func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
