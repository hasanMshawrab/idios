package mcp

import (
	"reflect"
	"strings"
	"testing"

	sebufhttp "github.com/SebastienMelki/sebuf/http"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/prompt"
	"github.com/hasanMshawrab/idios/internal/query"
)

// TestNormalizeKindMapsCaseInsensitively covers get_workload's kind and
// list_incidents' workload_kind: both map case-insensitively to the
// spelling ingest stores, and an unrecognized string passes through
// verbatim so an open-ended CRD kind still reaches the daemon's exact
// comparison.
func TestNormalizeKindMapsCaseInsensitively(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"unknown kind passes through verbatim", "Widget", "Widget"},
		{"lowercase none stays none", "none", "none"},
		{"uppercase none normalizes to lowercase", "NONE", "none"},
		{"lowercase deployment normalizes", "deployment", "Deployment"},
		{"mixed case statefulset normalizes", "StaTefulSet", "StatefulSet"},
		{"lowercase daemonset normalizes", "daemonset", "DaemonSet"},
		{"lowercase job normalizes", "job", "Job"},
		{"lowercase cronjob normalizes", "cronjob", "CronJob"},
		{"lowercase replicaset normalizes", "replicaset", "ReplicaSet"},
		{"already-canonical spelling is unchanged", "Deployment", "Deployment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeKind(tt.in); got != tt.want {
				t.Errorf("normalizeKind(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// kindListText is the canonical kinds joined the way the schema strings and
// the prompt name them: every kind but the last separated by ", ", "or"
// before the last one. Building it from canonicalKinds, rather than a
// second literal, is what lets the test below catch a skew in any one of
// the three surfaces.
func kindListText() string {
	if len(canonicalKinds) == 0 {
		return ""
	}
	if len(canonicalKinds) == 1 {
		return canonicalKinds[0]
	}
	return strings.Join(canonicalKinds[:len(canonicalKinds)-1], ", ") + " or " + canonicalKinds[len(canonicalKinds)-1]
}

// jsonschemaTag reads the jsonschema struct tag of one field of a mcp args
// type, so the test asserts the description the agent actually sees rather
// than a copy of it.
func jsonschemaTag(t *testing.T, v any, field string) string {
	t.Helper()
	f, ok := reflect.TypeOf(v).FieldByName(field)
	if !ok {
		t.Fatalf("type %T has no field %s", v, field)
	}
	tag, ok := f.Tag.Lookup("jsonschema")
	if !ok {
		t.Fatalf("field %s of %T has no jsonschema tag", field, v)
	}
	return tag
}

// minimalPrompt renders the MCP prompt of a bare incident: only the tools
// paragraph, which carries the kind vocabulary, needs to be well formed.
func minimalPrompt() string {
	return prompt.MCP(prompt.Input{
		Detail:      query.IncidentDetail{Incident: query.IncidentRow{}},
		ClusterName: "test",
	})
}

// unwrap collapses line-wrapping whitespace, so a kind list is found whether
// or not its source text happens to wrap a line in the middle of it.
func unwrap(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestVocabularyAgrees asserts that the normaliser's canonical spellings,
// the two tool schema strings and the prompt's kind list name the same
// kinds in the same order, so an edit to one without the others is caught
// here instead of surfacing as a tool call that fails or filters to
// nothing.
func TestVocabularyAgrees(t *testing.T) {
	want := kindListText()
	if want == "" {
		t.Fatal("canonicalKinds is empty")
	}

	got := jsonschemaTag(t, listIncidentsArgs{}, "WorkloadKind")
	if !strings.Contains(unwrap(got), want) {
		t.Errorf("list_incidents workload_kind schema %q does not name %q", got, want)
	}

	got = jsonschemaTag(t, workloadArgs{}, "Kind")
	if !strings.Contains(unwrap(got), want) {
		t.Errorf("get_workload kind schema %q does not name %q", got, want)
	}

	got = minimalPrompt()
	if !strings.Contains(unwrap(got), want) {
		t.Errorf("prompt %q does not name %q", got, want)
	}
}

// TestNodeFilterIsNamedOnBothSurfaces holds the rule that a tool schema and
// the prompt text that names it change together: the node filter is the one
// this step added, and an agent that reads only one of the two surfaces must
// still find it.
func TestNodeFilterIsNamedOnBothSurfaces(t *testing.T) {
	cases := []struct {
		name    string
		surface string
		want    string
	}{
		{"the list_incidents schema", jsonschemaTag(t, listIncidentsArgs{}, "NodeName"), "node"},
		{"the prompt's narrowing sentence", unwrap(minimalPrompt()), "node name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(c.surface, c.want) {
				t.Errorf("%q does not name %q", c.surface, c.want)
			}
		})
	}
}

// wireValueList joins the enum_value spellings of every value of E but
// unspecified the way the filter schemas name them, in the contract's order.
func wireValueList[E protoreflect.Enum](t *testing.T) string {
	t.Helper()
	var zero E
	values := zero.Descriptor().Values()
	var names []string
	for i := 0; i < values.Len(); i++ {
		v := values.Get(i)
		if v.Number() == 0 {
			continue
		}
		spelled, _ := proto.GetExtension(v.Options(), sebufhttp.E_EnumValue).(string)
		if spelled == "" {
			t.Fatalf("%s has no enum_value", v.Name())
		}
		names = append(names, spelled)
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// TestFilterSchemasNameEveryWireValue holds the state and category
// vocabularies together: the strings an agent reads in list_incidents'
// schema name exactly the values the contract's enums carry, so a value
// added to the proto without the schema, or the reverse, fails here rather
// than as a filter the daemon rejects or a value the agent never learns of.
func TestFilterSchemasNameEveryWireValue(t *testing.T) {
	cases := []struct {
		field string
		want  string
	}{
		{"State", wireValueList[idiosv1.IncidentState](t)},
		{"Category", wireValueList[idiosv1.Category](t)},
	}
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			got := jsonschemaTag(t, listIncidentsArgs{}, c.field)
			if !strings.Contains(unwrap(got), c.want) {
				t.Errorf("list_incidents %s schema %q does not name %q", c.field, got, c.want)
			}
		})
	}
}
