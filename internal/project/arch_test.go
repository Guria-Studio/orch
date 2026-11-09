package project

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The architecture rule of #550: internal/project is the only door to the
// state database and to tasks.json for the three front ends (cli, dashboard,
// mcp). Reading either directly is how .Status ends up read without Hydrate
// (review checklist rule 30).
//
// Only production files are checked: a test may open the database or the
// tasks file to set up or inspect a fixture, and that is not a second door.
//
// Both lists below are ratchets. A file not on its list fails when it starts
// importing state / loading tasks.json; a file on its list fails when it no
// longer does, so the entry has to be deleted and the lists only shrink.

var frontEnds = []string{"cli", "dashboard", "mcp"}

const statePkg = "github.com/Guria-Studio/orch/internal/state"
const modelPkg = "github.com/Guria-Studio/orch/internal/model"

// stateImportAllowed: files that still import internal/state directly, each
// with the reason it owns the database rather than going through project.
// Everything else — opening, filtering, the rows, the sentinels — has a
// project name since #587; a new entry needs a reason as strong as these.
var stateImportAllowed = map[string]string{
	// `orch migrate` imports a Python-era project: it reaches MigratedAt and
	// MarkMigrated, which are deliberately not on state.Backend (only the
	// importer may call them), and replays the old JSONL logs as raw
	// state.Event / state.Spend rows with their original timestamps,
	// skipping on state.ErrUnknownEventType. That is a schema-level import,
	// not a front end reading a project.
	"cli/migrate.go": "imports Python-era databases: needs *state.SQLite (MigratedAt, MarkMigrated), raw Event/Spend rows, ErrUnknownEventType",
}

// tasksLoadAllowed: files that still read tasks.json themselves — through
// model.LoadTasksFile or an os.ReadFile of it — instead of going through
// project (Open, Load, Definitions). Empty since #550: keep it that way.
var tasksLoadAllowed = map[string]string{}

func TestFrontEndsGoThroughProject(t *testing.T) {
	importsState := map[string]bool{}
	loadsTasks := map[string]bool{}
	fset := token.NewFileSet()
	for _, pkg := range frontEnds {
		files, err := filepath.Glob(filepath.Join("..", pkg, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			rel := filepath.ToSlash(filepath.Join(pkg, filepath.Base(path)))
			modelName, osName := "", ""
			for _, imp := range f.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				switch p {
				case statePkg:
					importsState[rel] = true
				case modelPkg:
					modelName = importName(imp, "model")
				case "os":
					osName = importName(imp, "os")
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if isCall(n, modelName, "LoadTasksFile") || isCall(n, osName, "ReadFile") && mentionsTasksJSON(n) {
					loadsTasks[rel] = true
				}
				return true
			})
		}
	}
	ratchet(t, "imports internal/state", "stateImportAllowed", importsState, stateImportAllowed)
	ratchet(t, "reads tasks.json itself", "tasksLoadAllowed", loadsTasks, tasksLoadAllowed)
}

func importName(imp *ast.ImportSpec, def string) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	return def
}

// isCall: n is a call to pkg.fn, pkg being the file's name for the import
// ("" when the file does not import it).
func isCall(n ast.Node, pkg, fn string) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok || pkg == "" {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != fn {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// mentionsTasksJSON: the call's arguments name tasks.json, as paths.TasksJSON()
// or a "tasks.json" literal.
func mentionsTasksJSON(n ast.Node) bool {
	found := false
	for _, arg := range n.(*ast.CallExpr).Args {
		ast.Inspect(arg, func(m ast.Node) bool {
			switch v := m.(type) {
			case *ast.SelectorExpr:
				found = found || v.Sel.Name == "TasksJSON"
			case *ast.BasicLit:
				found = found || strings.Contains(v.Value, "tasks.json")
			}
			return !found
		})
	}
	return found
}

func ratchet(t *testing.T, what, list string, got map[string]bool, allowed map[string]string) {
	t.Helper()
	var bad, stale []string
	for f := range got {
		if _, ok := allowed[f]; !ok {
			bad = append(bad, f)
		}
	}
	for f := range allowed {
		if !got[f] {
			stale = append(stale, f)
		}
	}
	sort.Strings(bad)
	sort.Strings(stale)
	for _, f := range bad {
		t.Errorf("%s %s: go through internal/project instead (or, if unavoidable, add it to %s with a reason)", f, what, list)
	}
	for _, f := range stale {
		t.Errorf("%s no longer %s: remove it from %s", f, what, list)
	}
}
