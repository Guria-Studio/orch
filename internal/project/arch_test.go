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

// stateImportAllowed: files that still import internal/state directly.
var stateImportAllowed = map[string]string{
	"cli/doctorsmoke.go":             "uses state.Backend, state.Spend",
	"cli/eventjson.go":               "uses state.Backend, state.Event",
	"cli/events.go":                  "uses state.Event",
	"cli/findings.go":                "uses state.Backend, state.FindingQueued, state.QueuedFinding",
	"cli/knowledge.go":               "uses state.Backend, state.ParseTS, state.TaskFilter, ...",
	"cli/knowledgecurate.go":         "uses state.Backend, state.ParseTS, state.TaskFilter",
	"cli/migrate.go":                 "imports Python-era databases: state.Open, state.NewSQLite, state.Event, ...",
	"cli/notify.go":                  "uses state.Backend, state.Spend",
	"cli/onboarding.go":              "uses state.SQLite",
	"cli/project.go":                 "opens the backend: state.Open, state.NewSQLite",
	"cli/report_summary.go":          "uses state.SummarySpendID",
	"cli/status.go":                  "uses state.Backend, state.ErrNoRuns, state.Run",
	"cli/tui.go":                     "uses state.Backend, state.ErrNoRuns, state.ParseTS, ...",
	"dashboard/events.go":            "uses state.Event",
	"dashboard/findingsroutes.go":    "uses state.FindingDismissed, state.FindingSent",
	"dashboard/knowledgeroutes.go":   "uses state.AllEvents, state.TaskFilter, state.Tasks",
	"dashboard/metricroutes.go":      "uses state.AllSpend, state.Attempts, state.LastEventByTask, ...",
	"dashboard/metrics.go":           "uses state.Spend",
	"dashboard/nowroutes.go":         "uses state.CountDoneLastNDays, state.Dispatch, state.ErrNoRuns, ...",
	"dashboard/readroutes.go":        "uses state.AllEvents, state.TaskFilter, state.Tasks",
	"dashboard/reviewroutes.go":      "uses state.TaskFilter, state.Tasks",
	"dashboard/server.go":            "uses state.Attempt, state.Dispatch, state.Event, ...",
	"dashboard/sprint.go":            "uses state.Event",
	"dashboard/stakeholderroutes.go": "uses state.AllEvents, state.AllSpend, state.CountDoneLastNDays, ...",
	"dashboard/usage.go":             "uses state.Spend",
	"dashboard/view.go":              "uses state.AllEvents, state.Event, state.TaskFilter, ...",
	"mcp/findings.go":                "uses state.FindingQueued, state.QueuedFinding",
	"mcp/readtools.go":               "uses state.Event",
	"mcp/server.go":                  "uses state.Backend",
	"mcp/tasks.go":                   "uses state.TaskFilter, state.TaskRuntime",
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
