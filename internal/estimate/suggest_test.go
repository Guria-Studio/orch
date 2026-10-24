package estimate

import (
	"fmt"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/pricing"
	"github.com/hectorcanaimero/orch/internal/router"
	"github.com/hectorcanaimero/orch/internal/state"
)

// history builds n tasks of one kind whose first attempt ran on backend/cli,
// `ok` of them succeeding on it; the rest fail once and succeed on a second
// attempt on the same route. Spend timestamps are written with `Z` and the
// success events with `+00:00`, as one orch.db holds both.
type history struct {
	tasks  []model.Task
	spend  []state.Spend
	events []state.Event
	clock  time.Time
}

func (h *history) add(kind, backend, cli string, n, ok int, cost float64) {
	for i := range n {
		id := fmt.Sprintf("%s-%s-%d", kind, cli, i)
		h.tasks = append(h.tasks, model.Task{ID: id, Model: kind})
		attempts := 1
		if i >= ok {
			attempts = 2
		}
		for a := 1; a <= attempts; a++ {
			h.clock = h.clock.Add(time.Minute)
			h.spend = append(h.spend, state.Spend{TS: h.clock.Format("2006-01-02T15:04:05Z"), TaskID: id,
				Backend: backend, Model: cli, Attempt: a, CostUSD: cost, DurationS: 60})
			typ := "fail"
			if a == attempts {
				typ = "success"
			}
			h.events = append(h.events, state.Event{EventType: typ, TaskID: id,
				TS: h.clock.Format("2006-01-02T15:04:05+00:00")})
		}
	}
}

func TestSuggestRoutes(t *testing.T) {
	h := &history{clock: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	// "work" runs on codex/gpt-5 today: 88% first try at $1.00 an attempt.
	// claude-sonnet did 90% at half that while the router pointed there.
	h.add("work", "codex", "gpt-5", 25, 22, 1.00)
	h.add("work", "claude", "claude-sonnet", 10, 9, 0.50)
	// "rare" has the same shape, but too few tasks to say anything.
	h.add("rare", "codex", "gpt-5", 2, 2, 1.00)
	h.add("rare", "claude", "claude-sonnet", 2, 2, 0.50)
	// "hard" retries most of its tasks on the cheap route; opus does not.
	h.add("hard", "claude", "claude-haiku", 10, 3, 0.10)
	h.add("hard", "claude", "claude-opus", 6, 6, 2.00)

	rtr := router.Router{
		"work": {Backend: model.BackendCodex, CLIModel: "gpt-5", Tier: model.TierStandard},
		"rare": {Backend: model.BackendCodex, CLIModel: "gpt-5", Tier: model.TierStandard},
		"hard": {Backend: model.BackendClaude, CLIModel: "claude-haiku", Tier: model.TierCheap},
	}
	got := SuggestRoutes(SuggestInput{Tasks: h.tasks, Routes: rtr, Spend: h.spend,
		Events: h.events, Prices: pricing.Load("")})

	byKind := map[string]KindRoutes{}
	for _, k := range got {
		byKind[k.Kind] = k
	}
	work := byKind["work"]
	if work.Suggestion == nil || work.Suggestion.To != "claude/claude-sonnet" {
		t.Fatalf("work: want claude/claude-sonnet suggested, got %+v", work.Suggestion)
	}
	if len(work.Routes) != 2 || work.Routes[0].Route != "codex/gpt-5" || !work.Routes[0].Current ||
		work.Routes[0].Tasks != 25 || work.Routes[0].FirstTry != 0.88 || work.Routes[0].MeanAttempts != 1.12 ||
		work.Routes[0].MeanCostUSD != 1 || work.Routes[1].FirstTry != 0.9 || work.Routes[1].MeanCostUSD != 0.5 {
		t.Errorf("work routes: %+v", work.Routes)
	}
	if s := byKind["rare"].Suggestion; s != nil {
		t.Errorf("rare: two tasks per route is below the minimum sample, got %+v", s)
	}
	if s := byKind["hard"].Suggestion; s == nil || s.To != "claude/claude-opus" {
		t.Errorf("hard: retries dominate, want claude/claude-opus suggested, got %+v", s)
	}
}
