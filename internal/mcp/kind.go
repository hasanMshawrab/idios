package mcp

import "strings"

// canonicalKinds are the workload kind spellings ingest stores from the
// owner reference: the common controllers, in the order the tool schemas
// and the prompt name them. The list documents, it does not enumerate - a
// CRD controller's kind is stored and matched as reported.
var canonicalKinds = []string{
	"Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "ReplicaSet", "none",
}

// canonicalKindByLower maps a lowercased kind to the spelling ingest stores.
var canonicalKindByLower = func() map[string]string {
	m := make(map[string]string, len(canonicalKinds))
	for _, k := range canonicalKinds {
		m[strings.ToLower(k)] = k
	}
	return m
}()

// normalizeKind maps a kind argument case-insensitively to the spelling the
// store compares exactly against; a string outside the known kinds passes
// through verbatim, since the owner kind is open-ended.
func normalizeKind(kind string) string {
	if canon, ok := canonicalKindByLower[strings.ToLower(kind)]; ok {
		return canon
	}
	return kind
}
