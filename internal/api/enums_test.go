package api

import (
	"testing"

	sebufhttp "github.com/SebastienMelki/sebuf/http"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// vocabulary is one mapping table widened so a single table test can walk
// every enum, beside the contract's own list of values for it.
type vocabulary struct {
	table  map[string]protoreflect.Enum
	values protoreflect.EnumValueDescriptors
}

// asEnums widens table and reads its value list from the enum type itself,
// which an empty table would otherwise leave unknown.
func asEnums[T protoreflect.Enum](table map[string]T) vocabulary {
	out := make(map[string]protoreflect.Enum, len(table))
	for k, v := range table {
		out[k] = v
	}
	var zero T
	return vocabulary{table: out, values: zero.Descriptor().Values()}
}

// The wire carries the string the daemon holds, so every value of every
// vocabulary the read models produce has a wire value whose enum_value spells
// it, and the contract has no value the daemon cannot produce.
func TestEveryStoreVocabularyHasAWireValue(t *testing.T) {
	cases := []struct {
		name string
		vocabulary
	}{
		{"category", asEnums(categories)},
		{"close_reason", asEnums(closeReasons)},
		{"incident_state", asEnums(incidentStates)},
		{"capture_gap", asEnums(captureGaps)},
		{"deletion_source", asEnums(deletionSources)},
		{"deletion_reason", asEnums(deletionReasons)},
		{"container_kind", asEnums(containerKinds)},
		{"container_state", asEnums(containerStates)},
		{"artifact_kind", asEnums(artifactKinds)},
		{"subject_kind", asEnums(subjectKinds)},
		{"timeline_kind", asEnums(timelineKinds)},
		{"lifecycle_step", asEnums(lifecycleSteps)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for stored, wire := range c.table {
				if wire.Number() == 0 {
					t.Errorf("%q maps to the unspecified value", stored)
					continue
				}
				desc := c.values.ByNumber(wire.Number())
				spelled, _ := proto.GetExtension(desc.Options(), sebufhttp.E_EnumValue).(string)
				if spelled != stored {
					t.Errorf("%q maps to %s, whose enum_value is %q", stored, desc.Name(), spelled)
				}
			}
			// One unmapped wire value is a vocabulary the application can be
			// sent and the store can never produce.
			if got, want := len(c.table), c.values.Len()-1; got != want {
				t.Errorf("%d stored values for %d wire values besides unspecified", got, want)
			}
		})
	}
}
