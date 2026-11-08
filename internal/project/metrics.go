package project

import (
	"slices"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// Metrics is how good the agents' work is, from the attempts table (#538):
// in total, and by route (backend/model) and by tier.
type Metrics struct {
	Total   MetricsRow   `json:"total"`
	ByRoute []MetricsRow `json:"by_route"`
	ByTier  []MetricsRow `json:"by_tier"`
}

// MetricsRow is one group's figures. Task figures (Tasks, DoneTasks,
// FirstAttempt, RetriesPerDone, CostPerDone) put a task in the group of its
// first attempt, the route it was planned for; attempt figures (Attempts,
// verify, findings, CI) count each attempt in the group it ran on.
type MetricsRow struct {
	Key      string `json:"key,omitempty"`
	Attempts int    `json:"attempts"`
	Tasks    int    `json:"tasks"`
	// DoneTasks are the tasks at done with at least one recorded attempt;
	// FirstAttempt those of them done in a single attempt.
	DoneTasks        int     `json:"done_tasks"`
	FirstAttempt     int     `json:"first_attempt"`
	FirstAttemptRate float64 `json:"first_attempt_rate"`
	// RetriesPerDone and CostPerDone count every attempt of a done task,
	// CI retries included.
	RetriesPerDone float64 `json:"retries_per_done"`
	CostPerDone    float64 `json:"cost_per_done_usd"`
	// Verified attempts went through the local verify; SentBack failed it.
	Verified     int     `json:"verified"`
	SentBack     int     `json:"sent_back"`
	SendBackRate float64 `json:"send_back_rate"`
	// A review finding is fixed when the task's next reviewed attempt no
	// longer has it, and ignored when it does, or when the task finished
	// with no later review. Findings of a task still in the works count in
	// neither.
	FindingsFixed   int `json:"findings_fixed"`
	FindingsIgnored int `json:"findings_ignored"`
	// CIAfterVerify are attempts that passed verify and whose CI finished;
	// CIFailedAfterVerify those whose CI failed anyway.
	CIAfterVerify       int     `json:"ci_after_verify"`
	CIFailedAfterVerify int     `json:"ci_failed_after_verify"`
	CIFailRate          float64 `json:"ci_fail_rate"`

	retries int
	cost    float64
}

// ComputeMetrics derives the figures from every attempt, oldest first, and
// every task's runtime row.
func ComputeMetrics(attempts []state.Attempt, tasks []state.TaskRuntime) Metrics {
	done := map[string]bool{}
	for _, t := range tasks {
		done[t.ID] = t.Status == model.StatusDone
	}
	byTask := map[string][]state.Attempt{}
	var order []string
	for _, a := range attempts {
		if _, ok := byTask[a.TaskID]; !ok {
			order = append(order, a.TaskID)
		}
		byTask[a.TaskID] = append(byTask[a.TaskID], a)
	}

	total := &MetricsRow{}
	routes, tiers := map[string]*MetricsRow{}, map[string]*MetricsRow{}
	group := func(m map[string]*MetricsRow, key string) *MetricsRow {
		if m[key] == nil {
			m[key] = &MetricsRow{Key: key}
		}
		return m[key]
	}
	routeOf := func(a state.Attempt) string { return a.Backend + "/" + a.Model }

	for _, id := range order {
		as := byTask[id]
		taskRows := []*MetricsRow{total, group(routes, routeOf(as[0])), group(tiers, as[0].Tier)}
		for _, r := range taskRows {
			r.Tasks++
			if done[id] {
				r.DoneTasks++
				r.retries += len(as) - 1
				if len(as) == 1 {
					r.FirstAttempt++
				}
				for _, a := range as {
					r.cost += a.CostUSD
				}
			}
		}
		for i, a := range as {
			fixed, ignored := findingOutcomes(as, i, done[id])
			for _, r := range []*MetricsRow{total, group(routes, routeOf(a)), group(tiers, a.Tier)} {
				r.Attempts++
				if a.Verify != "" {
					r.Verified++
				}
				if a.Verify == "fail" {
					r.SentBack++
				}
				if a.Verify == "pass" && a.CI != "" {
					r.CIAfterVerify++
					if a.CI == "failure" {
						r.CIFailedAfterVerify++
					}
				}
				r.FindingsFixed += fixed
				r.FindingsIgnored += ignored
			}
		}
	}

	out := Metrics{Total: *finish(total), ByRoute: []MetricsRow{}, ByTier: []MetricsRow{}}
	for _, m := range []struct {
		groups map[string]*MetricsRow
		into   *[]MetricsRow
	}{{routes, &out.ByRoute}, {tiers, &out.ByTier}} {
		for _, r := range m.groups {
			*m.into = append(*m.into, *finish(r))
		}
		slices.SortFunc(*m.into, func(a, b MetricsRow) int { return strings.Compare(a.Key, b.Key) })
	}
	return out
}

// findingOutcomes splits attempt i's findings into fixed and ignored by the
// task's next attempt whose review completed.
func findingOutcomes(as []state.Attempt, i int, done bool) (fixed, ignored int) {
	if len(as[i].Findings) == 0 {
		return 0, 0
	}
	for _, next := range as[i+1:] {
		if next.Review == "" {
			continue
		}
		for _, f := range as[i].Findings {
			if slices.ContainsFunc(next.Findings, func(g state.AttemptFinding) bool { return g.File == f.File && g.Title == f.Title }) {
				ignored++
			} else {
				fixed++
			}
		}
		return fixed, ignored
	}
	if done {
		return 0, len(as[i].Findings)
	}
	return 0, 0
}

func finish(r *MetricsRow) *MetricsRow {
	if r.DoneTasks > 0 {
		r.FirstAttemptRate = float64(r.FirstAttempt) / float64(r.DoneTasks)
		r.RetriesPerDone = float64(r.retries) / float64(r.DoneTasks)
		r.CostPerDone = r.cost / float64(r.DoneTasks)
	}
	if r.Verified > 0 {
		r.SendBackRate = float64(r.SentBack) / float64(r.Verified)
	}
	if r.CIAfterVerify > 0 {
		r.CIFailRate = float64(r.CIFailedAfterVerify) / float64(r.CIAfterVerify)
	}
	return r
}
