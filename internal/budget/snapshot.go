package budget

import (
	"context"
	"math"
	"time"
)

// ProviderSnapshot is one provider's row in the dashboard's budget section.
//
// The JSON names are Python's dict keys, unchanged: the SPA reads this shape
// from `/api/budgets` today and a rename here is a broken panel, not a
// refactor.
type ProviderSnapshot struct {
	// TokensUsed is what the gate compares with the cap: cache tokens
	// weighted (see weightedCacheDelta). RawTokensUsed is the same window
	// as the CLIs reported it. They differ only when rows carry cache usage.
	TokensUsed    int     `json:"tokens_used"`
	RawTokensUsed int     `json:"raw_tokens_used"`
	TokenBudget   int     `json:"token_budget"`
	UsagePct      float64 `json:"usage_pct"`
	ThresholdPct  float64 `json:"threshold_pct"`
	WindowHours   float64 `json:"window_hours"`
	Capped        bool    `json:"capped"`
	// ResetAt is null unless the provider is capped AND has rows in its
	// window. See the note in Snapshot about why those are not the same
	// condition as CanDispatch's.
	ResetAt *string `json:"reset_at"`
	// Estimated is whether any row in the window is orch's guess rather than
	// the CLI's report. Not serialised: the dashboard labels spend from
	// /api/metrics, and this JSON shape is Python's. The spend alerts read it.
	Estimated bool `json:"-"`
	// Source is "provider" for a budget written `source: provider` (#577),
	// absent for the token window. ProviderWindows are the account's
	// windows as its CLI last reported them, those not reset since; when
	// there are any, UsagePct, Capped and ResetAt are theirs and the token
	// fields above are only this project's count, for reading.
	Source          string           `json:"source,omitempty"`
	ProviderWindows []ProviderWindow `json:"provider_windows,omitempty"`
}

// ProviderWindow is one of the account's windows in a snapshot.
type ProviderWindow struct {
	Window string `json:"window"`
	// UsedPct is 0-100, as the provider reported it.
	UsedPct float64 `json:"used_pct"`
	// ResetsAt is UTC with a literal Z, as reset_at.
	ResetsAt string `json:"resets_at"`
}

// resetLayout is Python's `strftime("%Y-%m-%dT%H:%M:%SZ")`: UTC, second
// precision, literal Z. Not RFC3339 — that would write "+00:00" for a UTC
// time, and the dashboard's existing rows are all spelled with the Z.
const resetLayout = "2006-01-02T15:04:05Z"

// Snapshot is the per-provider payload the dashboard renders.
//
// Empty map when the gate is disabled — the dashboard hides the section
// entirely rather than drawing empty bars, which is how an operator can tell
// "no budgets.yaml" from "budgets.yaml with nothing spent yet".
//
// # A quirk preserved on purpose
//
// `reset_at` here is set only when the provider is capped *and* its window
// has rows in it. CanDispatch sets it whenever the provider is capped, dating
// an empty window from `now`. So a provider with a zero cap and no spend at
// all is reported capped with `reset_at: null` by this method and capped with
// a real instant by CanDispatch.
//
// Python does the same thing, and this is a port, not a correction: the
// dashboard has been rendering that null since Sprint 7 and the JSON shape is
// the contract. It is pinned by the window vector so it is a decision rather
// than an accident — and it only triggers on a threshold of zero, which is a
// config that blocks everything anyway.
func (g *Gate) Snapshot(ctx context.Context) (map[string]ProviderSnapshot, error) {
	out := map[string]ProviderSnapshot{}
	if g.Disabled() {
		return out, nil
	}
	now := g.now()
	for _, provider := range g.cfg.Names() {
		pb := g.cfg.Providers[provider]
		w, err := g.usage(ctx, provider, pb, now)
		if err != nil {
			return nil, err
		}

		capped := float64(w.tokens) >= pb.Cap()
		var resetAt *string
		if capped && !w.oldest.IsZero() {
			s := w.oldest.Add(time.Duration(pb.WindowHours * float64(time.Hour))).
				UTC().Format(resetLayout)
			resetAt = &s
		}

		usagePct := 0.0
		if pb.TokenBudget > 0 {
			usagePct = float64(w.tokens) / float64(pb.TokenBudget) * 100.0
		}

		snap := ProviderSnapshot{
			TokensUsed:    int(math.Round(w.tokens)),
			RawTokensUsed: w.raw,
			TokenBudget:   pb.TokenBudget,
			UsagePct:      usagePct,
			ThresholdPct:  pb.ThresholdPct,
			WindowHours:   pb.WindowHours,
			Capped:        capped,
			ResetAt:       resetAt,
			Estimated:     w.estimated,
		}
		if pb.Source == SourceProvider {
			snap.Source = SourceProvider
			r, ok, err := g.providerReport(ctx, provider, pb, now)
			if err != nil {
				return nil, err
			}
			if ok {
				snap.ProviderWindows, snap.UsagePct, snap.Capped, snap.ResetAt = r.windows, r.usedPct, r.capped, nil
				if r.capped {
					s := r.resetAt.Format(resetLayout)
					snap.ResetAt = &s
				}
			}
		}
		out[provider] = snap
	}
	return out, nil
}
