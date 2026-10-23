package dashboard

import (
	"net/http"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/state"
)

func TestUsagePeriod(t *testing.T) {
	now := time.Date(2026, 10, 22, 15, 30, 0, 0, time.UTC)
	for raw, want := range map[string]time.Time{
		"":      {},
		"all":   {},
		"today": time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
		"24h":   now.Add(-24 * time.Hour),
		"7d":    now.AddDate(0, 0, -7),
		"30d":   now.AddDate(0, 0, -30),
	} {
		if _, got, ok := usagePeriod(raw, now); !ok || !got.Equal(want) {
			t.Errorf("usagePeriod(%q) = %v, %v; want %v", raw, got, ok, want)
		}
	}
	if _, _, ok := usagePeriod("1y", now); ok {
		t.Error("an unknown period was accepted")
	}
}

// The Usage page's figures: tokens by kind, dispatches, the stacked series
// and the latest dispatches, newest first whatever the timestamp spelling.
func TestMetricsForTheUsagePage(t *testing.T) {
	codex := spend("gpt-5", 300, 100, 0, "2026-09-02T09:00:00+00:00", "T-3")
	codex.Backend = "codex"
	cached := spend("claude-sonnet-4-6", 1000, 500, 0.99, "2026-09-01T10:00:00Z", "T-1")
	cached.CacheReadTokens = 800
	f := &fakeState{spends: []state.Spend{
		cached,
		spend("claude-sonnet-4-6", 200, 50, 0.1, "2026-09-02T10:00:00Z", "T-2"),
		codex,
	}}
	s, _ := newMoneyServer(t, f, nil)

	var week metricsPayload
	decode(t, get(t, s, "/api/metrics?period=7d"), &week)
	if week.Period != "7d" || f.lastSince.IsZero() {
		t.Errorf("period = %q, since = %v; want 7d and a window", week.Period, f.lastSince)
	}

	var got metricsPayload
	decode(t, get(t, s, "/api/metrics?period=all"), &got)
	if got.Dispatches != 3 || got.TokensIn != 1500 || got.CacheReadTokens != 800 || got.TokensOut != 650 {
		t.Errorf("totals = %d dispatches, %d in, %d cached, %d out", got.Dispatches, got.TokensIn, got.CacheReadTokens, got.TokensOut)
	}
	if len(got.Recent) != 3 || got.Recent[0].TaskID != "T-2" || got.Recent[1].TaskID != "T-3" {
		t.Errorf("recent = %+v; want T-2, T-3, T-1 (newest first)", got.Recent)
	}
	if !got.Recent[1].Estimated || got.Recent[0].Estimated {
		t.Errorf("recent estimated = %v, %v; want codex's unpriced row flagged", got.Recent[0].Estimated, got.Recent[1].Estimated)
	}
	if len(got.ByBackendDay) != 3 || got.ByBackendDay[0].Date != "2026-09-02" || got.ByBackendDay[0].Backend != "claude" {
		t.Errorf("by_backend_day = %+v", got.ByBackendDay)
	}

	if len(got.ModelSources) != 2 || got.ModelSources[0].Model != "claude-sonnet-4-6" || got.ModelSources[0].Backend != "claude" ||
		got.ModelSources[0].Estimated || got.ModelSources[1].Backend != "codex" || !got.ModelSources[1].Estimated {
		t.Errorf("model_sources = %+v; want claude reported, codex's gpt-5 estimated", got.ModelSources)
	}

	if resp := get(t, s, "/api/metrics?period=1y"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown period = %d, want 400", resp.StatusCode)
	}
}

// The newest report per backend wins, and an event without one is ignored.
func TestQuotaIsTheLatestReportPerBackend(t *testing.T) {
	window := func(u float64) []any {
		return []any{map[string]any{"window": "five_hour", "utilization": u, "resets_at": 1792155000}}
	}
	f := &fakeState{events: []state.Event{
		{EventType: "success", Backend: "claude", TS: "2026-10-22T10:00:00Z", Extra: map[string]any{"quota": window(0.2)}},
		{EventType: "success", Backend: "claude", TS: "2026-10-22T12:00:00+00:00", Extra: map[string]any{"quota": window(0.6)}},
		{EventType: "success", Backend: "codex", TS: "2026-10-22T13:00:00Z", Extra: map[string]any{}},
	}}
	var got quotaPayload
	decode(t, get(t, newReadServer(t, f), "/api/quota"), &got)
	if len(got.Providers) != 1 || got.Providers[0].Backend != "claude" || got.Providers[0].Windows[0].Utilization != 0.6 {
		t.Errorf("providers = %+v; want claude at 0.6 only", got.Providers)
	}
	if resp := get(t, newGatedServer(t, f), "/api/quota?token=test-token-stakeholder"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("stakeholder: %d, want 403", resp.StatusCode)
	}
}
