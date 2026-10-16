package estimate

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/pricing"
	"github.com/hectorcanaimero/orch/internal/router"
	"github.com/hectorcanaimero/orch/internal/state"
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
			cost:      2 * prices.EstimateCost("claude-sonnet", 200_000, 10_000),
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

func TestLines(t *testing.T) {
	e := Estimate{Tasks: []string{"A", "B"}, CostUSD: 3.2, ByBackend: map[string]float64{"codex": 0.8, "claude": 2.4},
		WallS: 7800, CriticalPathS: 5400, Slots: 4, PastAttempts: 37, AttemptsPerTask: 1.3, Confidence: "medium"}
	l := e.Lines()
	want := "≈ 2 tasks · ≈ $3.20 (claude $2.40, codex $0.80) · ≈ 2h10m with 4 slots (critical path 1h30m)"
	if l[0] != want || !strings.Contains(l[1], "37 past attempts") || !strings.Contains(l[1], "confidence: medium") {
		t.Errorf("lines = %q", l)
	}
}
