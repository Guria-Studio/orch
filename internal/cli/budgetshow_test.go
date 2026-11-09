package cli

import (
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/budget"
)

// `orch budget show` says which number decides for each provider, and that
// the token window is this project's own count, not the plan's quota (#577).
func TestWriteBudgetShow(t *testing.T) {
	reset := "2026-10-23T03:00:00Z"
	cfg := &budget.Config{Providers: map[string]budget.ProviderBudget{
		"claude":   {WindowHours: 5, TokenBudget: 800_000, ThresholdPct: 60},
		"codex":    {WindowHours: 5, TokenBudget: 400_000, ThresholdPct: 90, Source: "provider"},
		"opencode": {WindowHours: 24, TokenBudget: 2_000_000, ThresholdPct: 70, Source: "provider"},
	}}
	snap := map[string]budget.ProviderSnapshot{
		"claude": {TokensUsed: 850_000, TokenBudget: 800_000, UsagePct: 106.25, ThresholdPct: 60, WindowHours: 5, Capped: true, ResetAt: &reset},
		"codex": {TokensUsed: 507_223, TokenBudget: 400_000, UsagePct: 7, ThresholdPct: 90, WindowHours: 5, Source: "provider",
			ProviderWindows: []budget.ProviderWindow{
				{Window: "five_hour", UsedPct: 7, ResetsAt: reset},
				{Window: "seven_day", UsedPct: 44, ResetsAt: "2026-10-26T01:00:00Z"},
			}},
		"opencode": {TokensUsed: 10, TokenBudget: 2_000_000, WindowHours: 24, ThresholdPct: 70, Source: "provider"},
	}
	var b strings.Builder
	if err := writeBudgetShow(&b, "conservative", ".orchestrator/budgets.yaml", cfg, snap); err != nil {
		t.Fatal(err)
	}
	want := `Preset conservative (.orchestrator/budgets.yaml)

claude — token window (this project): 850,000 of 800,000 weighted tokens in 5h (106%), pauses at 60% — deferring until 2026-10-23T03:00:00Z
codex — provider quota (account): five_hour 7% used, resets 2026-10-23T03:00:00Z · seven_day 44% used, resets 2026-10-26T01:00:00Z; pauses at 90%
    this project's token window, for reference: 507,223 of 400,000 weighted tokens in 5h
opencode — provider quota (account): no fresh report yet, so the token window decides: 10 of 2,000,000 weighted tokens in 24h (0%), pauses at 70%

` + budgetScopeNote + "\n"
	if got := b.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
