// Package sanitize strips secret material from captured Kubernetes objects.
package sanitize

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Redacted replaces every stripped value.
const Redacted = "(redacted)"

// lastAppliedAnnotation holds a verbatim copy of the applied manifest,
// inline environment values included, so it is replaced whole instead of
// being walked.
const lastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

// containerLists are the three places a pod spec keeps containers. Each
// entry can carry env values; a reference (envFrom, valueFrom) names a
// Secret but does not hold its contents, so references are kept. A secret
// passed as a command-line flag cannot be told from an ordinary argument,
// so command and args are kept too.
var containerLists = []string{"containers", "initContainers", "ephemeralContainers"}

// PodJSON returns a copy of a captured pod.json with every environment
// value and any other secret material replaced by Redacted; names and
// structure are kept.
func PodJSON(raw []byte) ([]byte, error) {
	// Decoding into a generic map rather than corev1.Pod keeps fields this
	// build does not know, so a newer cluster's pod.json survives the round
	// trip; UseNumber keeps numeric literals exactly as captured.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var pod map[string]any
	if err := dec.Decode(&pod); err != nil {
		return nil, fmt.Errorf("pod.json is not a JSON object: %w", err)
	}
	metadata, _ := object(pod, "metadata")
	if annotations, ok := object(metadata, "annotations"); ok {
		if _, has := annotations[lastAppliedAnnotation]; has {
			annotations[lastAppliedAnnotation] = Redacted
		}
	}
	spec, _ := object(pod, "spec")
	for _, list := range containerLists {
		for _, c := range array(spec, list) {
			for _, e := range array(c, "env") {
				if _, has := e["value"]; has {
					e["value"] = Redacted
				}
			}
		}
	}
	return json.Marshal(pod)
}

// object returns the object at key, and false when the field is absent or
// is not an object.
func object(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key].(map[string]any)
	return v, ok
}

// array returns the objects of the array at key, skipping anything that is
// not an object.
func array(m map[string]any, key string) []map[string]any {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		if o, ok := v.(map[string]any); ok {
			out = append(out, o)
		}
	}
	return out
}
