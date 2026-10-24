package estimate

import (
	"fmt"
	"math"
	"sort"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/pricing"
	"github.com/hectorcanaimero/orch/internal/router"
	"github.com/hectorcanaimero/orch/internal/state"
)

// `orch router suggest` (#457): per kind of task, how each route it ran on did
// — first-try success, attempts, cost and time — and, where the numbers are
// clear, which route the kind should run on instead. Plain statistics over
// this project's own spend rows and outcome events; no model is asked.
//
// A kind is the task's route key (its `model` in tasks.json): that is what a
// suggestion can change, by pointing the key at another backend and CLI model
// in model_router.yaml.
const (
	// MinSample is the fewest tasks a route needs, on both sides of a
	// comparison, before anything is suggested.
	MinSample = 5
	// SuccessMargin is how far first-try success may differ and still count
	// as equal; a stronger route must beat the current one by more.
	SuccessMargin = 0.05
	// cheaperBelow is the cost share a cheaper route must come under, so noise
	// in the averages does not flip a route back and forth.
	cheaperBelow = 0.9
	// retriesDominate is the mean attempts per task at which the current
	// route is taken to be too weak for the kind.
	retriesDominate = 1.5
)

// SuggestInput is everything SuggestRoutes reads.
type SuggestInput struct {
	// Tasks give each task's kind (its route key).
	Tasks  []model.Task
	Routes router.Router
	// Spend is the window's attempts (state.Backend.AllSpend), oldest first.
	Spend []state.Spend
	// Events are the outcome events; only `success` is read.
	Events []state.Event
	Prices pricing.Table
}

// RouteRecord is one route's record on one kind of task.
type RouteRecord struct {
	// Route is `backend/cli_model`, what the attempts actually ran on.
	Route   string `json:"route"`
	Current bool   `json:"current"`
	// Tasks is how many tasks had their first attempt here; FirstTry is the
	// share of them that succeeded on it, MeanAttempts their attempts.
	Tasks        int     `json:"tasks"`
	FirstTry     float64 `json:"first_try_rate"`
	MeanAttempts float64 `json:"mean_attempts"`
	// Attempts is every attempt on this route, the sample the means below
	// come from. A row with no cost to resolve is left out of the cost.
	Attempts      int     `json:"attempts"`
	MeanCostUSD   float64 `json:"mean_cost_usd"`
	MeanDurationS float64 `json:"mean_duration_s"`
}

// Suggestion points a route key at another route.
type Suggestion struct {
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
	// Why is "cheaper" or "stronger"; Reason says it in a sentence.
	Why         string        `json:"why"`
	ToBackend   model.Backend `json:"to_backend"`
	ToCLIModel  string        `json:"to_cli_model"`
	Reason      string        `json:"reason"`
	CostShare   float64       `json:"cost_share"`
	SuccessFrom float64       `json:"first_try_from"`
	SuccessTo   float64       `json:"first_try_to"`
}

// KindRoutes is one kind's routes, most tasks first, and its suggestion.
type KindRoutes struct {
	Kind string `json:"kind"`
	// Current is the route the router gives the kind now; "" when it has none.
	Current    string        `json:"current"`
	Routes     []RouteRecord `json:"routes"`
	Suggestion *Suggestion   `json:"suggestion"`
}

type routeAcc struct {
	backend, cliModel           string
	tasks, firstOK, taskAttempt int
	attempts                    int
	costs, durations            []float64
}

// SuggestRoutes is the per-kind table, sorted by kind.
func SuggestRoutes(in SuggestInput) []KindRoutes {
	kindOf := make(map[string]string, len(in.Tasks))
	for _, t := range in.Tasks {
		kindOf[t.ID] = t.Model
	}
	// An attempt succeeded when a `success` event for its task carries its
	// timestamp (RecordFinish writes both with one). Compared as instants:
	// the same second is spelled `Z` or `+00:00` depending on the writer.
	type key struct {
		task string
		at   int64
	}
	succeeded := map[key]bool{}
	for _, e := range in.Events {
		if e.EventType != "success" {
			continue
		}
		if at, ok := state.ParseTS(e.TS); ok {
			succeeded[key{e.TaskID, at.Unix()}] = true
		}
	}

	byTask := map[string][]state.Spend{}
	var order []string
	for _, s := range in.Spend {
		if _, known := kindOf[s.TaskID]; !known {
			continue // a task no longer in tasks.json has no kind
		}
		if _, seen := byTask[s.TaskID]; !seen {
			order = append(order, s.TaskID)
		}
		byTask[s.TaskID] = append(byTask[s.TaskID], s)
	}

	kinds := map[string]map[string]*routeAcc{}
	acc := func(kind string, s state.Spend) *routeAcc {
		if kinds[kind] == nil {
			kinds[kind] = map[string]*routeAcc{}
		}
		name := s.Backend + "/" + s.Model
		a := kinds[kind][name]
		if a == nil {
			a = &routeAcc{backend: s.Backend, cliModel: s.Model}
			kinds[kind][name] = a
		}
		return a
	}
	for _, id := range order {
		rows := byTask[id]
		kind := kindOf[id]
		for _, s := range rows {
			a := acc(kind, s)
			a.attempts++
			a.durations = append(a.durations, s.DurationS)
			if c := in.Prices.ResolveCost(s.CostUSD, s.Model, s.TokensIn, s.TokensOut); c > 0 {
				a.costs = append(a.costs, c)
			}
		}
		first := acc(kind, rows[0])
		first.tasks++
		first.taskAttempt += len(rows)
		if at, ok := state.ParseTS(rows[0].TS); ok && succeeded[key{id, at.Unix()}] {
			first.firstOK++
		}
	}

	out := make([]KindRoutes, 0, len(kinds))
	for kind, routes := range kinds {
		k := KindRoutes{Kind: kind, Routes: []RouteRecord{}}
		if e, ok := in.Routes[kind]; ok {
			k.Current = string(e.Backend) + "/" + e.CLIModel
		}
		for name, a := range routes {
			r := RouteRecord{Route: name, Current: name == k.Current, Tasks: a.tasks,
				Attempts: a.attempts, MeanCostUSD: cents(mean(a.costs)),
				MeanDurationS: math.Round(mean(a.durations))}
			if a.tasks > 0 {
				r.FirstTry = math.Round(float64(a.firstOK)/float64(a.tasks)*10000) / 10000
				r.MeanAttempts = math.Round(float64(a.taskAttempt)/float64(a.tasks)*100) / 100
			}
			k.Routes = append(k.Routes, r)
		}
		sort.Slice(k.Routes, func(i, j int) bool {
			if k.Routes[i].Tasks != k.Routes[j].Tasks {
				return k.Routes[i].Tasks > k.Routes[j].Tasks
			}
			return k.Routes[i].Route < k.Routes[j].Route
		})
		k.Suggestion = suggest(kind, k.Routes, routes)
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// suggest is the kind's one change, or nil. With retries dominating on the
// current route it looks for a stronger one: clearly better first-try
// success, the best of them. Otherwise for a cheaper one: first-try success
// within the margin at under cheaperBelow of the cost, the cheapest of them.
// Every route compared, the current one included, needs MinSample tasks.
func suggest(kind string, rows []RouteRecord, acc map[string]*routeAcc) *Suggestion {
	var cur *RouteRecord
	for i := range rows {
		if rows[i].Current {
			cur = &rows[i]
		}
	}
	if cur == nil || cur.Tasks < MinSample {
		return nil
	}
	var best *RouteRecord
	reason := ""
	for i := range rows {
		r := &rows[i]
		if r.Current || r.Tasks < MinSample {
			continue
		}
		if cur.MeanAttempts >= retriesDominate {
			if r.FirstTry > cur.FirstTry+SuccessMargin &&
				(best == nil || r.FirstTry > best.FirstTry ||
					r.FirstTry == best.FirstTry && r.MeanCostUSD < best.MeanCostUSD) {
				best, reason = r, "stronger"
			}
			continue
		}
		if r.FirstTry >= cur.FirstTry-SuccessMargin && r.MeanCostUSD > 0 &&
			r.MeanCostUSD < cur.MeanCostUSD*cheaperBelow &&
			(best == nil || r.MeanCostUSD < best.MeanCostUSD) {
			best, reason = r, "cheaper"
		}
	}
	if best == nil {
		return nil
	}
	s := &Suggestion{Key: kind, From: cur.Route, To: best.Route,
		ToBackend: model.Backend(acc[best.Route].backend), ToCLIModel: acc[best.Route].cliModel,
		Why: reason, SuccessFrom: cur.FirstTry, SuccessTo: best.FirstTry}
	if cur.MeanCostUSD > 0 {
		s.CostShare = math.Round(best.MeanCostUSD/cur.MeanCostUSD*100) / 100
	}
	if reason == "stronger" {
		s.Reason = fmt.Sprintf("retries dominate (%.2g attempts per task); %s succeeds first try %s of the time vs %s",
			cur.MeanAttempts, best.Route, pct(best.FirstTry), pct(cur.FirstTry))
	} else {
		s.Reason = fmt.Sprintf("first-try success as good or within 5 points (%s vs %s) at %s of the cost",
			pct(best.FirstTry), pct(cur.FirstTry), pct(s.CostShare))
	}
	return s
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }
