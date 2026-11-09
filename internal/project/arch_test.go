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
	"cli/dashboard.go":               "uses state.Backend",
	"cli/doctorsmoke.go":             "uses state.Backend, state.Spend",
	"cli/estimate.go":                "uses state.Backend",
	"cli/eventjson.go":               "uses state.Backend, state.Event",
	"cli/events.go":                  "uses state.Event",
	"cli/explain.go":                 "uses state.Backend",
	"cli/findings.go":                "uses state.Backend, state.FindingQueued, state.QueuedFinding",
	"cli/knowledge.go":               "uses state.Backend, state.ParseTS, state.TaskFilter, ...",
	"cli/knowledgecurate.go":         "uses state.Backend, state.ParseTS, state.TaskFilter",
	"cli/metrics.go":                 "uses state.TaskFilter",
	"cli/migrate.go":                 "imports Python-era databases: state.Open, state.NewSQLite, state.Event, ...",
	"cli/notify.go":                  "uses state.Backend, state.Spend",
	"cli/onboarding.go":              "uses state.SQLite",
	"cli/project.go":                 "opens the backend: state.Open, state.NewSQLite",
	"cli/publish.go":                 "uses state.Backend",
	"cli/report.go":                  "uses state.Backend, state.TaskFilter",
	"cli/report_receipt.go":          "uses state.Backend",
	"cli/report_summary.go":          "uses state.SummarySpendID",
	"cli/reset.go":                   "uses state.Backend, state.Note, state.TaskFilter",
	"cli/run.go":                     "uses state.Backend, state.TaskFilter",
	"cli/status.go":                  "uses state.Backend, state.ErrNoRuns, state.Run",
	"cli/task.go":                    "uses state.Backend, state.ErrIllegalTransition, state.ErrTaskNotFound, ...",
	"cli/taskedit.go":                "uses state.ErrTaskNotFound",
	"cli/tasksalvage.go":             "uses state.ErrIllegalTransition, state.Event, state.Note",
	"cli/taskstatus.go":              "uses state.ErrIllegalTransition, state.ErrTaskNotFound, state.Note",
	"cli/tui.go":                     "uses state.Backend, state.ErrNoRuns, state.ParseTS, ...",
	"cli/welcome.go":                 "uses state.ErrIllegalTransition, state.Note",
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
	"dashboard/steerroutes.go":       "uses state.ErrIllegalTransition, state.ErrTaskNotFound",
	"dashboard/usage.go":             "uses state.Spend",
	"dashboard/view.go":              "uses state.AllEvents, state.Event, state.TaskFilter, ...",
	"mcp/findings.go":                "uses state.FindingQueued, state.QueuedFinding",
	"mcp/readtools.go":               "uses state.Event",
	"mcp/runstatus.go":               "uses state.ErrNoRuns",
	"mcp/server.go":                  "uses state.Backend",
	"mcp/status.go":                  "uses state.ErrIllegalTransition, state.ErrTaskNotFound, state.Note, ...",
	"mcp/tasks.go":                   "uses state.TaskFilter, state.TaskRuntime",
}

// tasksLoadAllowed: files that still call model.LoadTasksFile themselves
// instead of going through project.
var tasksLoadAllowed = map[string]string{
	"cli/doctor.go":                    "loads tasks.json itself",
	"cli/doctorsmoke.go":               "loads tasks.json itself",
	"cli/notify.go":                    "loads tasks.json itself",
	"cli/project.go":                   "loads tasks.json itself",
	"cli/report.go":                    "loads tasks.json itself",
	"cli/report_receipt.go":            "loads tasks.json itself",
	"cli/router.go":                    "loads tasks.json itself",
	"cli/routersuggest.go":             "loads tasks.json itself",
	"cli/run.go":                       "loads tasks.json itself",
	"cli/start.go":                     "loads tasks.json itself",
	"cli/syncissues.go":                "loads tasks.json itself",
	"cli/validate.go":                  "loads tasks.json itself",
	"cli/welcome.go":                   "loads tasks.json itself",
	"dashboard/routersuggestroutes.go": "loads tasks.json itself",
	"dashboard/view.go":                "loads tasks.json itself",
}

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
			modelName := ""
			for _, imp := range f.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				switch p {
				case statePkg:
					importsState[rel] = true
				case modelPkg:
					modelName = "model"
					if imp.Name != nil {
						modelName = imp.Name.Name
					}
				}
			}
			if modelName == "" {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "LoadTasksFile" {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == modelName {
					loadsTasks[rel] = true
				}
				return true
			})
		}
	}
	ratchet(t, "imports internal/state", "stateImportAllowed", importsState, stateImportAllowed)
	ratchet(t, "calls model.LoadTasksFile", "tasksLoadAllowed", loadsTasks, tasksLoadAllowed)
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
