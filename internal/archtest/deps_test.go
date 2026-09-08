// Package archtest checks import rules by parsing import declarations.
package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "idios"

var forbidden = map[string][]string{
	"internal/ingest":    {"k8s.io/client-go", module + "/internal/capture", module + "/internal/k8s"},
	"internal/incident":  {"k8s.io/client-go", module + "/internal/capture", module + "/internal/k8s"},
	"internal/processor": {"k8s.io/client-go", module + "/internal/capture", module + "/internal/k8s"},
	"internal/k8s":       {module + "/internal/store"},
	"internal/capture":   {module + "/internal/ingest"},
	"internal/store":     {"k8s.io/"},
}

var onlyInternal = map[string][]string{
	"internal/sweep": {module + "/internal/store", module + "/internal/clock"},
	"internal/query": {module + "/internal/store", module + "/internal/clock"},
	// The seed drives the recorder, so it reaches wider than query itself.
	"internal/query/querytest": {module + "/internal/store", module + "/internal/clock", module + "/internal/incident",
		module + "/internal/processor", module + "/internal/ingest"},
	"internal/status": {module + "/internal/clock"},
	// The prompt text is one endpoint's body, so it reads the same read models
	// that endpoint does and nothing else.
	"internal/prompt": {module + "/internal/query", module + "/internal/store"},
	"internal/api": {module + "/internal/query", module + "/internal/store", module + "/internal/apigen/idiosv1",
		module + "/internal/status", module + "/internal/config", module + "/internal/clock", module + "/internal/notify",
		module + "/internal/prompt", module + "/internal/sanitize"},
	// The fixture server answers from the committed bytes alone, so it needs
	// neither the store nor the handlers it stands in for.
	"internal/api/mock":       {module + "/api/testdata", module + "/internal/apigen/idiosv1"},
	"internal/apigen/idiosv1": {},
	// The broadcast carries ids, so it needs nothing else and can be
	// imported by every producer.
	"internal/notify": {},
	// The MCP server reads the daemon over HTTP like any other client, so
	// the database, the read models and the artifact files are out of reach.
	"internal/mcp": {module + "/internal/apigen/idiosv1", module + "/internal/sanitize"},
	// Sanitizing is a pure transform of captured bytes.
	"internal/sanitize": {},
}

func importsByPackage(t *testing.T, root string) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "testdata" || n == "bin" || strings.HasPrefix(n, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		if out[pkg] == nil {
			out[pkg] = map[string]bool{}
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			out[pkg][p] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func hasPrefix(imp, prefix string) bool {
	return imp == prefix || strings.HasPrefix(imp, prefix+"/") || strings.HasSuffix(prefix, "/") && strings.HasPrefix(imp, prefix)
}

func TestPackageDependencyRules(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	imports := importsByPackage(t, filepath.Join(wd, "..", ".."))

	for pkg, bad := range forbidden {
		for imp := range imports[pkg] {
			for _, prefix := range bad {
				if hasPrefix(imp, prefix) {
					t.Errorf("%s imports %s", pkg, imp)
				}
			}
		}
	}
	for pkg, allowed := range onlyInternal {
		for imp := range imports[pkg] {
			if !strings.HasPrefix(imp, module+"/") {
				continue
			}
			ok := false
			for _, a := range allowed {
				ok = ok || imp == a
			}
			if !ok {
				t.Errorf("%s imports %s; allowed: %v", pkg, imp, allowed)
			}
		}
	}
	for pkg, set := range imports {
		if !strings.HasPrefix(pkg, "internal/") {
			continue
		}
		for imp := range set {
			// The read models exist for the endpoints, so their callers inside
			// internal are api and the prompt text api serves.
			if hasPrefix(imp, module+"/internal/query") && pkg != "internal/api" && pkg != "internal/prompt" {
				t.Errorf("%s imports %s", pkg, imp)
			}
			if hasPrefix(imp, module+"/internal/api") || hasPrefix(imp, module+"/cmd") {
				t.Errorf("%s imports %s", pkg, imp)
			}
		}
	}
}
