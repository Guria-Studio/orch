package contract

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePrefix = "github.com/Guria-Studio/orch/internal/"

// imports is every internal package the non-test files of dir import.
func imports(t *testing.T, dir string) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if name, ok := strings.CutPrefix(p, modulePrefix); ok {
				got[name] = true
			}
		}
	}
	return got
}

// ci and verify need the engine's contract types, not the engine: they read
// them from this package, so the engine's internals stay out of their build
// (#550).
func TestCIAndVerifyDoNotImportEngine(t *testing.T) {
	for _, pkg := range []string{"ci", "verify"} {
		if imports(t, filepath.Join("..", pkg))["engine"] {
			t.Errorf("internal/%s imports internal/engine; take the types from internal/contract", pkg)
		}
	}
}

// contract is a leaf: besides model and providers (leaves themselves) it
// imports nothing of orch's, so anyone can depend on it.
func TestContractIsALeaf(t *testing.T) {
	for p := range imports(t, ".") {
		if p != "model" && p != "providers" {
			t.Errorf("internal/contract imports internal/%s", p)
		}
	}
}
