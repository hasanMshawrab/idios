package sanitize

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// The captured pod.json is the one surface that carries inline environment
// values and the last-applied-configuration copy of them; everything else
// about the object has to survive the strip untouched.
func TestPodJSONStripsEnvValuesAndKeepsEverythingElse(t *testing.T) {
	raw, err := os.ReadFile("testdata/pod.json")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/pod_sanitized.json")
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, golden); err != nil {
		t.Fatal(err)
	}

	got, err := PodJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want.String() {
		t.Errorf("PodJSON =\n%s\nwant\n%s", got, want.String())
	}
}

// A capture that is not an object cannot be walked, and a caller must be
// told rather than handed bytes it believes were sanitized.
func TestPodJSONRejectsWhatIsNotAnObject(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"array", "[]"},
		{"truncated", `{"spec":{"containers":[`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := PodJSON([]byte(c.raw))
			if err == nil {
				t.Fatalf("PodJSON(%q) = %s, want an error", c.raw, got)
			}
			if got != nil {
				t.Errorf("PodJSON(%q) returned %s with an error, want nil", c.raw, got)
			}
		})
	}
}
