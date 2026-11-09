package estimate

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/pricing"
	"github.com/Guria-Studio/orch/internal/router"
	"github.com/Guria-Studio/orch/internal/state"
)

func task(id string, st model.Status, route string, deps ...string) model.Task {
	return model.Task{ID: id, Status: st, Model: route, Dependencies: deps, Phase: 1}
}

var routes = router.Router{
	"sonnet": {Backend: model.BackendClaude, CLIModel: "claude-sonnet"},
	"gpt":    {Backend: model.BackendCodex, CLIModel: "gpt-5"},
	"gem":    {Backend: model.BackendGemini, CLIModel: "gemini-flash"},
}

func spend(taskID, cliModel string, cost, durS float64) state.Spend {
	return state.Spend{TaskID: taskID, Model: cliModel, CostUSD: cost, DurationS: durS}
}

func TestCompute(t *testing.T) {
	prices := pricing.Load("")
	cases := []struct {
		name      string
		in        Input
		tasks     []string
		cost      float64
		wallS     float64
		critS     float64
		confident string
	}{
		{
			name: "no history: typical tokens and ten minutes per task",
			in: Input{Tasks: []model.Task{
				task("A", model.StatusTodo, "sonnet"),
				task("B", model.StatusTodo, "sonnet"),
			}, Routes: routes, Slots: 4, TypicalTokens: 200_000},
			tasks:     []string{"A", "B"},
			cost:      2 * prices.EstimateCost("claude-sonnet", pricing.Tokens{In: 200_000, Out: 10_000}),
			wallS:     600,
			critS:     600,
			confident: "none",
		},
		{
			name: "history on another model only falls back to the project's",
			in: Input{Tasks: []model.Task{task("A", model.StatusTodo, "sonnet")}, Routes: routes, Slots: 1,
				Spend: []state.Spend{spend("X", "gpt-5", 1.0, 100), spend("Y", "gpt-5", 3.0, 300)}},
			tasks:     []string{"A"},
			cost:      2.0,
			wallS:     200,
			critS:     200,
			confident: "low",
		},
		{
			name: "own model's history wins over the project's, times attempts per task",
			in: Input{Tasks: []model.Task{task("A", model.StatusTodo, "gpt")}, Routes: routes, Slots: 1,
				Spend: []state.Spend{spend("X", "gpt-5", 1.0, 100), spend("X", "gpt-5", 1.0, 100), spend("Y", "claude-sonnet", 9.0, 900)}},
			tasks:     []string{"A"},
			cost:      1.5,
			wallS:     150,
			critS:     150,
			confident: "low",
		},
		{
			name: "a model whose attempts report no usage takes the project's cost and its own duration",
			in: Input{Tasks: []model.Task{task("A", model.StatusTodo, "gem")}, Routes: routes, Slots: 1,
				Spend: []state.Spend{spend("X", "gemini-flash", 0, 30), spend("Y", "claude-sonnet", 2.0, 600)}},
			tasks:     []string{"A"},
			cost:      2.0,
			wallS:     30,
			critS:     30,
			confident: "low",
		},
		{
			name: "--only leaves out the rest, and what depends on it",
			in: Input{Tasks: []model.Task{
				task("A1", model.StatusTodo, "sonnet"),
				task("B1", model.StatusTodo, "sonnet"),
				task("A2", model.StatusTodo, "sonnet", "B1"),
				task("A3", model.StatusTodo, "sonnet", "A1"),
			}, Routes: routes, Slots: 4, Only: "A*",
				Spend: []state.Spend{spend("X", "claude-sonnet", 1.0, 60)}},
			tasks:     []string{"A1", "A3"},
			cost:      2.0,
			wallS:     120,
			critS:     120,
			confident: "low",
		},
		{
			name: "a chain longer than the slots: wall time is the critical path",
			in: Input{Tasks: []model.Task{
				task("A", model.StatusDone, "sonnet"),
				task("B", model.StatusTodo, "sonnet", "A"),
				task("C", model.StatusTodo, "sonnet", "B"),
				task("D", model.StatusTodo, "sonnet", "C"),
				task("E", model.StatusBacklog, "sonnet"),
				task("F", model.StatusTodo, "sonnet", "G"),
				task("G", model.StatusBlocked, "sonnet"),
			}, Routes: routes, Slots: 2,
				Spend: []state.Spend{spend("A", "claude-sonnet", 1.0, 60)}},
			tasks:     []string{"B", "C", "D"},
			cost:      3.0,
			wallS:     180,
			critS:     180,
			confident: "low",
		},
		{
			name: "independent tasks share the slots",
			in: Input{Tasks: []model.Task{
				task("A", model.StatusTodo, "sonnet"), task("B", model.StatusTodo, "sonnet"),
				task("C", model.StatusTodo, "sonnet"), task("D", model.StatusTodo, "sonnet"),
			}, Routes: routes, Slots: 2, MaxTasks: 3,
				Spend: []state.Spend{spend("X", "claude-sonnet", 1.0, 60)}},
			tasks:     []string{"A", "B", "C"},
			cost:      3.0,
			wallS:     90,
			critS:     60,
			confident: "low",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.Prices = prices
			got := Compute(c.in)
			if !reflect.DeepEqual(got.Tasks, c.tasks) {
				t.Errorf("tasks = %v, want %v", got.Tasks, c.tasks)
			}
			if math.Abs(got.CostUSD-c.cost) > 1e-9 {
				t.Errorf("cost = %v, want %v", got.CostUSD, c.cost)
			}
			if got.WallS != c.wallS || got.CriticalPathS != c.critS {
				t.Errorf("wall/critical = %v/%v, want %v/%v", got.WallS, got.CriticalPathS, c.wallS, c.critS)
			}
			if got.Confidence != c.confident {
				t.Errorf("confidence = %q, want %q", got.Confidence, c.confident)
			}
		})
	}
}

// A synthetic spend row (a verify review, an AI summary) is neither a task
// nor an attempt: it moves neither attempts_per_task nor the per-attempt cost
// and duration a task is estimated from (#470).
func TestAttemptsPerTaskIgnoresSyntheticSpend(t *testing.T) {
	e := Compute(Input{Tasks: []model.Task{task("A", model.StatusTodo, "sonnet")}, Routes: routes, Slots: 1,
		Spend: []state.Spend{
			spend("X", "claude-sonnet", 1, 100), spend("X", "claude-sonnet", 1, 100),
			spend(state.ReviewSpendID("X", 1), "claude-sonnet", 0.1, 10),
			spend(state.ReviewSpendID("X", 2), "claude-sonnet", 0.1, 10),
			spend(state.SummarySpendID("run-1"), "claude-sonnet", 0.1, 10),
		}})
	if e.AttemptsPerTask != 2 || e.PastAttempts != 2 {
		t.Errorf("attempts = %d past, %v per task, want 2 and 2", e.PastAttempts, e.AttemptsPerTask)
	}
	// Two attempts per task of the real rows' $1 and 100s each.
	if e.CostUSD != 2 || e.WallS != 200 {
		t.Errorf("estimate = $%v, %vs; want $2, 200s from the real attempts only", e.CostUSD, e.WallS)
	}
}

func TestLines(t *testing.T) {
	e := Estimate{Tasks: []string{"A", "B"}, CostUSD: 3.2, ByBackend: map[string]float64{"codex": 0.8, "claude": 2.4},
		WallS: 7800, CriticalPathS: 5400, Slots: 4, PastAttempts: 37, AttemptsPerTask: 1.3, Confidence: "medium"}
	l := e.Lines()
	want := "≈ 2 tasks · ≈ $3.20 (claude $2.40, codex $0.80) · ≈ 2h10m with 4 slots (critical path 1h30m)"
	if l[0] != want || !strings.Contains(l[1], "37 past attempts") || !strings.Contains(l[1], "confidence: medium") {
		t.Errorf("lines = %q", l)
	}
}

// A project with nothing left to dispatch — a fresh `orch init` with no
// tasks, or one whose every task is done — still answers `"tasks": []`. It
// answered `null`, and the dashboard's Now page read `.tasks.length` off it
// and rendered nothing at all: a black page on the two projects most likely
// to be opened, the new one and the finished one.
func TestComputeWithNothingToRunEncodesAnEmptyList(t *testing.T) {
	for name, tasks := range map[string][]model.Task{
		"no tasks":     nil,
		"all done":     {{ID: "A", Status: model.StatusDone}},
		"only blocked": {{ID: "A", Status: model.StatusBlocked}},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(Compute(Input{Tasks: tasks}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"tasks":[]`) {
				t.Errorf("estimate = %s, want \"tasks\":[]", raw)
			}
		})
	}
}

// A task in backlog never dispatches, and `orch run` used to say only that
// nothing was ready (#579): the operator who just ran `orch atomize --apply`
// had no way to know the new tasks were waiting to be promoted, nor how.
func TestBacklogNote(t *testing.T) {
	cases := []struct {
		name    string
		in      Input
		backlog []string
		line2   string
	}{
		{
			name:    "one backlog task, nothing else: its exact command",
			in:      Input{Tasks: []model.Task{task("F0.1.T1", model.StatusBacklog, "sonnet")}, Only: "F0.1.T1"},
			backlog: []string{"F0.1.T1"},
			line2:   "1 task in backlog is not dispatched — promote it with: orch task set --id F0.1.T1 --status todo",
		},
		{
			name: "several, narrowed by --only, in dispatch order: one command for all",
			in: Input{Tasks: []model.Task{
				task("F0.1.T2", model.StatusBacklog, "sonnet", "F0.1.T1"),
				task("F0.1.T1", model.StatusBacklog, "sonnet"),
				task("F1.1.T1", model.StatusBacklog, "sonnet"),
				task("F0.1.T0", model.StatusDone, "sonnet"),
			}, Only: "F0.1.*"},
			backlog: []string{"F0.1.T1", "F0.1.T2"},
			line2: `2 tasks in backlog are not dispatched — promote them with: ` +
				`for id in F0.1.T1 F0.1.T2; do orch task set --id "$id" --status todo; done`,
		},
		{
			name:    "an id the shell would split is quoted",
			in:      Input{Tasks: []model.Task{task("odd id", model.StatusBacklog, "sonnet")}},
			backlog: []string{"odd id"},
			line2:   "1 task in backlog is not dispatched — promote it with: orch task set --id 'odd id' --status todo",
		},
		{
			name:    "nothing in backlog: no note",
			in:      Input{Tasks: []model.Task{task("A", model.StatusBlocked, "sonnet")}},
			backlog: []string{},
			line2:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Routes = routes
			e := Compute(tc.in)
			if !reflect.DeepEqual(e.Backlog, tc.backlog) {
				t.Errorf("Backlog = %#v, want %#v", e.Backlog, tc.backlog)
			}
			if got := e.BacklogNote(); got != tc.line2 {
				t.Errorf("BacklogNote() = %q\nwant           %q", got, tc.line2)
			}
			l := e.Lines()
			if l[0] != "≈ 0 tasks: nothing is ready to run, nor becomes ready during a run" || l[1] != tc.line2 {
				t.Errorf("Lines() = %q, want the zero line then %q", l, tc.line2)
			}
		})
	}
}

// With something to dispatch, backlog is the operator's choice (orch-tasks
// leaves later work there on purpose): the estimate lists it but says
// nothing about it.
func TestBacklogBesideReadyWorkIsListedNotNoted(t *testing.T) {
	e := Compute(Input{Routes: routes, Tasks: []model.Task{
		task("A", model.StatusTodo, "sonnet"),
		task("B", model.StatusBacklog, "sonnet"),
	}})
	if !reflect.DeepEqual(e.Backlog, []string{"B"}) {
		t.Errorf("Backlog = %#v, want [B]", e.Backlog)
	}
	if l := e.Lines(); strings.Contains(l[1], "backlog") {
		t.Errorf("Lines()[1] = %q, want no backlog note while A runs", l[1])
	}
	raw, err := json.Marshal(Compute(Input{Tasks: []model.Task{task("A", model.StatusDone, "sonnet")}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"backlog":[]`) {
		t.Errorf("estimate = %s, want \"backlog\":[]", raw)
	}
}
