package dashboard

import (
	"sort"
	"time"

	"github.com/hectorcanaimero/orch/internal/pricing"
	"github.com/hectorcanaimero/orch/internal/state"
)

// The Usage page's views of the spend rows: a period, a per-backend day
// series, and the latest dispatches. Each row is one dispatch (NFR-OBS-2).

// recentShown is how many dispatches the Usage page lists.
const recentShown = 15

// usagePeriod resolves `?period=` to the instant spend is read from: today
// starts at local midnight, the others count back from now. "" and "all"
// are all of history (the zero time). ok is false for anything else.
func usagePeriod(raw string, now time.Time) (period string, since time.Time, ok bool) {
	switch raw {
	case "", "all":
		return "all", time.Time{}, true
	case "today":
		y, m, d := now.Date()
		return raw, time.Date(y, m, d, 0, 0, 0, 0, now.Location()), true
	case "24h":
		return raw, now.Add(-24 * time.Hour), true
	case "7d":
		return raw, now.AddDate(0, 0, -7), true
	case "30d":
		return raw, now.AddDate(0, 0, -30), true
	}
	return "", time.Time{}, false
}

// backendDayStats is one backend's share of one UTC day: what the Usage
// chart stacks.
type backendDayStats struct {
	Date       string  `json:"date"`
	Backend    string  `json:"backend"`
	Tokens     int     `json:"tokens"`
	Dispatches int     `json:"dispatches"`
	CostUSD    float64 `json:"cost_usd"`
}

// metricsByBackendDay is metricsByDay split by backend, newest day first, at
// most `days` distinct days; backends sorted by name within a day.
func metricsByBackendDay(spends []state.Spend, table pricing.Table, days int) []backendDayStats {
	type key struct{ date, backend string }
	buckets := map[key]*backendDayStats{}
	for _, s := range spends {
		if len(s.TS) < 10 {
			continue
		}
		k := key{s.TS[:10], s.Backend}
		b := buckets[k]
		if b == nil {
			b = &backendDayStats{Date: k.date, Backend: k.backend}
			buckets[k] = b
		}
		b.Tokens += s.TokensIn + s.TokensOut
		b.Dispatches++
		b.CostUSD += table.ResolveCost(s.CostUSD, modelOrUnknown(s.Model), s.TokensIn, s.TokensOut)
	}
	out := make([]backendDayStats, 0, len(buckets))
	kept := map[string]bool{}
	for _, b := range buckets {
		b.CostUSD = round4(b.CostUSD)
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].Backend < out[j].Backend
	})
	trimmed := out[:0]
	for _, b := range out {
		if !kept[b.Date] && len(kept) == days {
			break
		}
		kept[b.Date] = true
		trimmed = append(trimmed, b)
	}
	return trimmed
}

// recentSpend is one dispatch in the Usage page's latest list.
type recentSpend struct {
	TS        string  `json:"ts"`
	TaskID    string  `json:"task_id"`
	Backend   string  `json:"backend"`
	Model     string  `json:"model"`
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
	CostUSD   float64 `json:"cost_usd"`
	// Estimated: the cost is priced from pricing.yaml, or the tokens were
	// inferred, rather than reported by the CLI.
	Estimated bool `json:"estimated"`
}

// recentSpends is the newest n rows. Timestamps are parsed, never compared
// as strings: one orch.db holds both +00:00 and Z spellings.
func recentSpends(spends []state.Spend, table pricing.Table, n int) []recentSpend {
	sorted := append([]state.Spend(nil), spends...)
	sort.SliceStable(sorted, func(i, j int) bool { return parseTS(sorted[i].TS).After(parseTS(sorted[j].TS)) })
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	out := make([]recentSpend, 0, len(sorted))
	for _, s := range sorted {
		out = append(out, recentSpend{
			TS: s.TS, TaskID: s.TaskID, Backend: s.Backend, Model: s.Model,
			TokensIn: s.TokensIn, TokensOut: s.TokensOut,
			CostUSD:   round4(table.ResolveCost(s.CostUSD, modelOrUnknown(s.Model), s.TokensIn, s.TokensOut)),
			Estimated: s.Estimated || s.CostUSD <= 0,
		})
	}
	return out
}

func modelOrUnknown(model string) string {
	if model == "" {
		return "unknown"
	}
	return model
}

// parseTS reads either timestamp spelling; an unparseable one sorts last.
func parseTS(ts string) time.Time {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}
	}
	return t
}
