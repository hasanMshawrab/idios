package grafana

import (
	"testing"
	"time"
)

// TestExploreURLBuildsTheWholeURL covers presentation.md's "Grafana links"
// builder contract (substitution, escaping, panes shape) and its
// empty-placeholder rule, one row per behaviour, each asserting the exact
// URL string idios serves.
func TestExploreURLBuildsTheWholeURL(t *testing.T) {
	from := time.Unix(1700000000, 0).UTC()
	to := time.Unix(1700003600, 0).UTC()
	base := Config{BaseURL: "https://logs.example.grafana.net", DatasourceUID: "grafanacloud-logs", Selector: DefaultSelector}

	cases := []struct {
		name string
		cfg  Config
		v    Values
		w    Window
		want string
	}{
		{
			"all placeholders set, closed window",
			base,
			Values{Namespace: "shop", Pod: "checkout-api-abc123", Container: "checkout-api"},
			Window{From: from, To: to},
			`https://logs.example.grafana.net/explore?schemaVersion=1&panes=%7B%22a%22%3A%7B%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22expr%22%3A%22%7Bnamespace%3D%5C%22shop%5C%22%2C+pod%3D%5C%22checkout-api-abc123%5C%22%2C+container%3D%5C%22checkout-api%5C%22%7D%22%2C%22queryType%22%3A%22range%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22grafanacloud-logs%22%7D%2C%22direction%22%3A%22backward%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221700000000000%22%2C%22to%22%3A%221700003600000%22%7D%2C%22panelsState%22%3A%7B%22logs%22%3A%7B%22sortOrder%22%3A%22Descending%22%7D%7D%2C%22compact%22%3Afalse%7D%7D`,
		},
		{
			"empty container drops its matcher",
			base,
			Values{Namespace: "shop", Pod: "checkout-api-abc123", Container: ""},
			Window{From: from, To: to},
			`https://logs.example.grafana.net/explore?schemaVersion=1&panes=%7B%22a%22%3A%7B%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22expr%22%3A%22%7Bnamespace%3D%5C%22shop%5C%22%2C+pod%3D%5C%22checkout-api-abc123%5C%22%7D%22%2C%22queryType%22%3A%22range%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22grafanacloud-logs%22%7D%2C%22direction%22%3A%22backward%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221700000000000%22%2C%22to%22%3A%221700003600000%22%7D%2C%22panelsState%22%3A%7B%22logs%22%3A%7B%22sortOrder%22%3A%22Descending%22%7D%7D%2C%22compact%22%3Afalse%7D%7D`,
		},
		{
			"empty workload drops a $workload matcher",
			Config{BaseURL: "https://logs.example.grafana.net", DatasourceUID: "grafanacloud-logs", Selector: `{namespace="$namespace", pod="$pod", workload="$workload"}`},
			Values{Namespace: "shop", Pod: "checkout-api-abc123", Workload: ""},
			Window{From: from, To: to},
			`https://logs.example.grafana.net/explore?schemaVersion=1&panes=%7B%22a%22%3A%7B%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22expr%22%3A%22%7Bnamespace%3D%5C%22shop%5C%22%2C+pod%3D%5C%22checkout-api-abc123%5C%22%7D%22%2C%22queryType%22%3A%22range%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22grafanacloud-logs%22%7D%2C%22direction%22%3A%22backward%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221700000000000%22%2C%22to%22%3A%221700003600000%22%7D%2C%22panelsState%22%3A%7B%22logs%22%3A%7B%22sortOrder%22%3A%22Descending%22%7D%7D%2C%22compact%22%3Afalse%7D%7D`,
		},
		{
			"a pod name containing a quote is escaped",
			base,
			Values{Namespace: "shop", Pod: `checkout-api-"weird"`, Container: "checkout-api"},
			Window{From: from, To: to},
			`https://logs.example.grafana.net/explore?schemaVersion=1&panes=%7B%22a%22%3A%7B%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22expr%22%3A%22%7Bnamespace%3D%5C%22shop%5C%22%2C+pod%3D%5C%22checkout-api-%5C%5C%5C%22weird%5C%5C%5C%22%5C%22%2C+container%3D%5C%22checkout-api%5C%22%7D%22%2C%22queryType%22%3A%22range%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22grafanacloud-logs%22%7D%2C%22direction%22%3A%22backward%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221700000000000%22%2C%22to%22%3A%221700003600000%22%7D%2C%22panelsState%22%3A%7B%22logs%22%3A%7B%22sortOrder%22%3A%22Descending%22%7D%7D%2C%22compact%22%3Afalse%7D%7D`,
		},
		{
			"open window renders the literal now",
			base,
			Values{Namespace: "shop", Pod: "checkout-api-abc123", Container: "checkout-api"},
			Window{From: from},
			`https://logs.example.grafana.net/explore?schemaVersion=1&panes=%7B%22a%22%3A%7B%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22expr%22%3A%22%7Bnamespace%3D%5C%22shop%5C%22%2C+pod%3D%5C%22checkout-api-abc123%5C%22%2C+container%3D%5C%22checkout-api%5C%22%7D%22%2C%22queryType%22%3A%22range%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22grafanacloud-logs%22%7D%2C%22direction%22%3A%22backward%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221700000000000%22%2C%22to%22%3A%22now%22%7D%2C%22panelsState%22%3A%7B%22logs%22%3A%7B%22sortOrder%22%3A%22Descending%22%7D%7D%2C%22compact%22%3Afalse%7D%7D`,
		},
		{
			"a trailing slash on BaseURL does not double the slash",
			Config{BaseURL: "https://logs.example.grafana.net/", DatasourceUID: "grafanacloud-logs", Selector: DefaultSelector},
			Values{Namespace: "shop", Pod: "checkout-api-abc123", Container: "checkout-api"},
			Window{From: from, To: to},
			`https://logs.example.grafana.net/explore?schemaVersion=1&panes=%7B%22a%22%3A%7B%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22expr%22%3A%22%7Bnamespace%3D%5C%22shop%5C%22%2C+pod%3D%5C%22checkout-api-abc123%5C%22%2C+container%3D%5C%22checkout-api%5C%22%7D%22%2C%22queryType%22%3A%22range%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22grafanacloud-logs%22%7D%2C%22direction%22%3A%22backward%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221700000000000%22%2C%22to%22%3A%221700003600000%22%7D%2C%22panelsState%22%3A%7B%22logs%22%3A%7B%22sortOrder%22%3A%22Descending%22%7D%7D%2C%22compact%22%3Afalse%7D%7D`,
		},
	}
	for _, c := range cases {
		if got := ExploreURL(c.cfg, c.v, c.w); got != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.name, got, c.want)
		}
	}
}
