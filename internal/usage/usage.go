// Package usage splits a project's token use the ways that show where it went
// (#417): input, cached input, output and reasoning, per model, per task and
// per attempt, with the retries apart. `orch report usage` and the
// dashboard's /api/usage read the same Build, so the two cannot disagree.
//
// These are orch's own records of what each dispatch reported. They are not
// the provider's subscription quota (a ChatGPT or Claude plan's 5-hour
// window), which orch cannot see; orch's budget guardrail is likewise its own
// limit on these numbers.
package usage

import (
	"cmp"
	"slices"

	"github.com/hectorcanaimero/orch/internal/state"
)

// Tokens is what a set of dispatches used.
type Tokens struct {
	Dispatches int `json:"dispatches"`
	// Input is fresh input: what the dispatches sent that the prompt cache
	// did not hold.
	Input int `json:"input"`
	// CachedInput is input read back from the prompt cache — context the
	// agent re-read, cheaper per token but often most of them.
	CachedInput int `json:"cached_input"`
	CacheWrite  int `json:"cache_write"`
	// Output includes Reasoning.
	Output    int     `json:"output"`
	Reasoning int     `json:"reasoning"`
	CostUSD   float64 `json:"cost_usd"`
	// Estimated is true when some rows' counts were inferred because the CLI
	// reported none.
	Estimated bool `json:"estimated"`
}

// Total is every token, input of all kinds and output.
func (t Tokens) Total() int { return t.Input + t.CachedInput + t.CacheWrite + t.Output }

func (t *Tokens) add(s state.Spend) {
	t.Dispatches++
	t.Input += max(s.TokensIn-s.CacheReadTokens-s.CacheCreationTokens, 0)
	t.CachedInput += s.CacheReadTokens
	t.CacheWrite += s.CacheCreationTokens
	t.Output += s.TokensOut
	t.Reasoning += s.ReasoningTokens
	t.CostUSD += s.CostUSD
	t.Estimated = t.Estimated || s.Estimated
}

// Model is one backend and model's use.
type Model struct {
	Backend string `json:"backend"`
	Model   string `json:"model"`
	Tokens
}

// Attempt is one attempt of a task. Attempt 0 holds rows written before
// orch recorded attempts.
type Attempt struct {
	Attempt int `json:"attempt"`
	Tokens
}

// Task is one task's use, attempt by attempt.
type Task struct {
	TaskID   string    `json:"task_id"`
	Attempts []Attempt `json:"attempts"`
	// RetryTokens is what attempts after the first used.
	RetryTokens int `json:"retry_tokens"`
	Tokens
}

// Usage is the whole breakdown. ByModel and ByTask lead with the heaviest.
type Usage struct {
	Total       Tokens  `json:"total"`
	RetryTokens int     `json:"retry_tokens"`
	ByModel     []Model `json:"by_model"`
	ByTask      []Task  `json:"by_task"`
}

// Build splits rows, a project's spend.
func Build(rows []state.Spend) Usage {
	u := Usage{ByModel: []Model{}, ByTask: []Task{}}
	models := map[[2]string]*Model{}
	tasks := map[string]*Task{}
	attempts := map[string]map[int]*Attempt{}
	for _, s := range rows {
		u.Total.add(s)
		key := [2]string{s.Backend, s.Model}
		if models[key] == nil {
			models[key] = &Model{Backend: s.Backend, Model: s.Model}
		}
		models[key].add(s)
		if tasks[s.TaskID] == nil {
			tasks[s.TaskID], attempts[s.TaskID] = &Task{TaskID: s.TaskID}, map[int]*Attempt{}
		}
		tasks[s.TaskID].add(s)
		if attempts[s.TaskID][s.Attempt] == nil {
			attempts[s.TaskID][s.Attempt] = &Attempt{Attempt: s.Attempt}
		}
		attempts[s.TaskID][s.Attempt].add(s)
	}
	for _, m := range models {
		u.ByModel = append(u.ByModel, *m)
	}
	for id, t := range tasks {
		for _, a := range attempts[id] {
			t.Attempts = append(t.Attempts, *a)
			if a.Attempt > 1 {
				t.RetryTokens += a.Total()
			}
		}
		slices.SortFunc(t.Attempts, func(a, b Attempt) int { return cmp.Compare(a.Attempt, b.Attempt) })
		u.RetryTokens += t.RetryTokens
		u.ByTask = append(u.ByTask, *t)
	}
	slices.SortFunc(u.ByModel, func(a, b Model) int {
		return cmp.Or(cmp.Compare(b.Total(), a.Total()), cmp.Compare(a.Backend, b.Backend), cmp.Compare(a.Model, b.Model))
	})
	slices.SortFunc(u.ByTask, func(a, b Task) int {
		return cmp.Or(cmp.Compare(b.Total(), a.Total()), cmp.Compare(a.TaskID, b.TaskID))
	})
	return u
}
