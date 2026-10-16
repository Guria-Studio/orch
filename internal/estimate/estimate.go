// Package estimate answers "what will the next `orch run` cost" before it
// starts (#357): which tasks it would dispatch, what they would cost and how
// long they would take, from this project's own past attempts.
//
// Pure: everything it reads is an argument, so `orch estimate`, the line
// `orch run` prints and the dashboard's /api/estimate are the same function.
package estimate

import (
	"fmt"
	"math"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/hectorcanaimero/orch/internal/graph"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/pricing"
	"github.com/hectorcanaimero/orch/internal/router"
	"github.com/hectorcanaimero/orch/internal/state"
)

// With no history at all an attempt is assumed to take config.yaml's
// typical_dispatch_tokens of input (Input.TypicalTokens), a twentieth of that
// as output, and DefaultDuration.
const (
	defaultOutputShare = 20
	DefaultDuration    = 10 * time.Minute
)

// Input is everything an estimate reads.
type Input struct {
	// Tasks carry their runtime status (project.Hydrate / the queue's view).
	Tasks  []model.Task
	Routes router.Router
	// Spend is every finished attempt in this project (state.Backend.AllSpend).
	Spend  []state.Spend
	Prices pricing.Table
	// Slots is concurrency.global_max; less than 1 counts as 1.
	Slots int
	// Only and MaxTasks are `orch run`'s flags, with the same meaning.
	Only     string
	MaxTasks int
	// TypicalTokens is config.yaml's typical_dispatch_tokens, the no-history
	// guess for an attempt's input.
	TypicalTokens int
}

// Estimate is the answer. Durations are seconds, money is USD.
type Estimate struct {
	Tasks     []string           `json:"tasks"`
	CostUSD   float64            `json:"cost_usd"`
	ByBackend map[string]float64 `json:"cost_by_backend"`
	// WallS is max(critical path, total agent time / slots).
	WallS         float64 `json:"wall_s"`
	CriticalPathS float64 `json:"critical_path_s"`
	Slots         int     `json:"slots"`
	// PastAttempts is how many finished attempts the numbers come from;
	// AttemptsPerTask is the project's observed retry multiplier.
	PastAttempts    int     `json:"past_attempts"`
	AttemptsPerTask float64 `json:"attempts_per_task"`
	// Confidence is "none" (no history: defaults), "low", "medium" or "high".
	Confidence string `json:"confidence"`
}

// Compute is the estimate for in.
func Compute(in Input) Estimate {
	slots := max(in.Slots, 1)
	out := Estimate{Tasks: runSet(in), ByBackend: map[string]float64{}, Slots: slots,
		PastAttempts: len(in.Spend), AttemptsPerTask: 1, Confidence: confidence(len(in.Spend))}
	if len(in.Spend) > 0 {
		perTask := map[string]bool{}
		for _, s := range in.Spend {
			perTask[s.TaskID] = true
		}
		out.AttemptsPerTask = math.Round(float64(len(in.Spend))/float64(len(perTask))*100) / 100
	}

	byID := make(map[string]model.Task, len(in.Tasks))
	for _, t := range in.Tasks {
		byID[t.ID] = t
	}
	selected := make(map[string]bool, len(out.Tasks))
	for _, id := range out.Tasks {
		selected[id] = true
	}
	all := sample(in, in.Spend)
	finish := map[string]float64{}
	var totalS float64
	// out.Tasks is in dispatch order, so every selected dependency's finish
	// is known when its dependent is reached.
	for _, id := range out.Tasks {
		t := byID[id]
		backend, cliModel := route(in.Routes, t.Model)
		own := sample(in, modelRows(in.Spend, cliModel))
		cost := firstKnown(own.costs, all.costs, func() float64 {
			return in.Prices.EstimateCost(cliModel, in.TypicalTokens, in.TypicalTokens/defaultOutputShare)
		})
		dur := firstKnown(own.durations, all.durations, DefaultDuration.Seconds)
		cost *= out.AttemptsPerTask
		dur *= out.AttemptsPerTask
		out.CostUSD += cost
		out.ByBackend[backend] += cost
		totalS += dur
		start := 0.0
		for _, dep := range t.Dependencies {
			if selected[dep] {
				start = max(start, finish[dep])
			}
		}
		finish[id] = start + dur
		out.CriticalPathS = max(out.CriticalPathS, finish[id])
	}
	out.WallS = math.Round(max(out.CriticalPathS, totalS/float64(slots)))
	out.CriticalPathS = math.Round(out.CriticalPathS)
	out.CostUSD = cents(out.CostUSD)
	for b, c := range out.ByBackend {
		out.ByBackend[b] = cents(c)
	}
	return out
}

// runSet is the tasks a run would dispatch, in the order it would: todo (or
// in-progress, which a new run re-dispatches) tasks whose dependencies are
// done or become done during the run, wave by wave in (phase, id) order, as
// TaskQueue.Ready offers them. A task --only excludes never runs, so its
// dependents wait too; backlog and blocked never dispatch.
func runSet(in Input) []string {
	done := map[string]bool{}
	pending := []model.Task{}
	for _, t := range graph.DisplayOrder(in.Tasks) {
		switch t.Status {
		case model.StatusDone:
			done[t.ID] = true
		case model.StatusTodo, model.StatusInProgress:
			if in.Only == "" || matchGlob(in.Only, t.ID) {
				pending = append(pending, t)
			}
		}
	}
	var out []string
	for {
		var wave []string
		var rest []model.Task
		for _, t := range pending {
			if depsIn(t, done) {
				wave = append(wave, t.ID)
			} else {
				rest = append(rest, t)
			}
		}
		if len(wave) == 0 {
			return out
		}
		for _, id := range wave {
			if in.MaxTasks > 0 && len(out) == in.MaxTasks {
				return out
			}
			out = append(out, id)
			done[id] = true
		}
		pending = rest
	}
}

func depsIn(t model.Task, done map[string]bool) bool {
	for _, d := range t.Dependencies {
		if !done[d] {
			return false
		}
	}
	return true
}

// matchGlob is engine's --only matching (path.Match, fnmatchcase-alike).
func matchGlob(pattern, id string) bool {
	ok, err := path.Match(pattern, id)
	return err == nil && ok
}

// route is the backend and CLI model a task's route key dispatches to — the
// router row, else what `orch router add-missing` would infer.
func route(r router.Router, key string) (backend, cliModel string) {
	if e, ok := r[key]; ok {
		return string(e.Backend), e.CLIModel
	}
	if e, err := router.InferEntry(key, model.TierStandard); err == nil {
		return string(e.Backend), e.CLIModel
	}
	return "unknown", key
}

func modelRows(rows []state.Spend, cliModel string) []state.Spend {
	var out []state.Spend
	for _, s := range rows {
		if s.Model == cliModel {
			out = append(out, s)
		}
	}
	return out
}

// stats are one attempt's cost and duration samples over some spend rows.
type stats struct{ costs, durations []float64 }

// sample collects rows' attempts. A row that resolves to no cost — no price
// recorded and no tokens to price, gemini's usual row — says nothing about
// cost and is left out of it; its duration still counts.
func sample(in Input, rows []state.Spend) stats {
	var out stats
	for _, s := range rows {
		out.durations = append(out.durations, s.DurationS)
		if c := in.Prices.ResolveCost(s.CostUSD, s.Model, s.TokensIn, s.TokensOut); c > 0 {
			out.costs = append(out.costs, c)
		}
	}
	return out
}

// firstKnown is the median of the task's own model's samples, else of the
// project's, else the stated default.
func firstKnown(own, project []float64, fallback func() float64) float64 {
	if len(own) > 0 {
		return median(own)
	}
	if len(project) > 0 {
		return median(project)
	}
	return fallback()
}

// cents rounds to a hundredth of a cent: an estimate, not a ledger.
func cents(v float64) float64 { return math.Round(v*10000) / 10000 }

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	m := len(v) / 2
	if len(v)%2 == 1 {
		return v[m]
	}
	return (v[m-1] + v[m]) / 2
}

func confidence(n int) string {
	switch {
	case n == 0:
		return "none"
	case n < 10:
		return "low"
	case n < 30:
		return "medium"
	}
	return "high"
}

// Lines is the estimate as `orch estimate` prints it: the numbers, then what
// they rest on.
func (e Estimate) Lines() [2]string {
	if len(e.Tasks) == 0 {
		return [2]string{"≈ 0 tasks: nothing is ready to run, nor becomes ready during a run", ""}
	}
	backends := make([]string, 0, len(e.ByBackend))
	for b := range e.ByBackend {
		backends = append(backends, b)
	}
	sort.Slice(backends, func(i, j int) bool {
		if e.ByBackend[backends[i]] != e.ByBackend[backends[j]] {
			return e.ByBackend[backends[i]] > e.ByBackend[backends[j]]
		}
		return backends[i] < backends[j]
	})
	parts := make([]string, len(backends))
	for i, b := range backends {
		parts[i] = fmt.Sprintf("%s $%.2f", b, e.ByBackend[b])
	}
	noun := "tasks"
	if len(e.Tasks) == 1 {
		noun = "task"
	}
	first := fmt.Sprintf("≈ %d %s · ≈ $%.2f (%s) · ≈ %s with %d slots (critical path %s)",
		len(e.Tasks), noun, e.CostUSD, strings.Join(parts, ", "), Duration(e.WallS), e.Slots, Duration(e.CriticalPathS))
	if e.PastAttempts == 0 {
		return [2]string{first, "no history yet — rough: each attempt assumed typical_dispatch_tokens and 10 minutes"}
	}
	return [2]string{first, fmt.Sprintf("based on %d past attempts in this project (%.2g per task) · confidence: %s",
		e.PastAttempts, e.AttemptsPerTask, e.Confidence)}
}

// Duration is seconds as "2h10m", "45m" or "<1m".
func Duration(s float64) string {
	m := int(math.Round(s / 60))
	switch {
	case m < 1:
		return "<1m"
	case m < 60:
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}
