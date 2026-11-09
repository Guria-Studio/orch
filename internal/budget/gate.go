package budget

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Guria-Studio/orch/internal/pyfmt"
	"github.com/Guria-Studio/orch/internal/state"
)

// SpendReader is the slice of `state.Backend` the gate needs.
//
// Narrow on purpose. The gate reads one provider's rolling window and nothing
// else, and saying so in the type means a test can hand it two rows without
// standing up a database, and means nobody later reaches for a write method
// from inside a guardrail.
type SpendReader interface {
	SpendSince(ctx context.Context, backend string, since time.Time) ([]state.Spend, error)
}

// Gate answers "may I dispatch to this provider right now".
//
// One instance per run, shared across the loop. It caches nothing: every call
// re-reads the window, which is what makes it safe to call from the reap and
// refill paths at once without a lock. The read is a single indexed query
// over a table that holds tens of rows per project.
type Gate struct {
	cfg   *Config
	spend SpendReader
	// quota is where a `source: provider` budget reads the account's
	// windows from; nil when the spend reader cannot report them.
	quota QuotaReader

	// now is the clock, swapped in tests. See the package doc.
	now func() time.Time
}

// QuotaReader is the slice of `state.SQLite` a `source: provider` budget
// needs: the newest subscription windows a backend's CLI reported.
type QuotaReader interface {
	LatestQuota(ctx context.Context, backend string) (state.ProviderQuota, bool, error)
}

// NewGate builds a gate. A nil config, or one naming no providers, disables
// it: every call then answers "yes" without touching the database.
//
// When spend can also report quotas (the SQLite backend does), a provider
// budgeted with `source: provider` decides by them; otherwise it keeps to
// its token window.
func NewGate(spend SpendReader, cfg *Config) *Gate {
	g := &Gate{cfg: cfg, spend: spend, now: time.Now}
	g.quota, _ = spend.(QuotaReader)
	return g
}

// Disabled reports whether the gate will ever block anything.
//
// Worth asking before wiring the dashboard section or logging a startup line,
// so an operator can tell "no budgets.yaml" from "budgets.yaml with nothing
// over threshold" — two states that otherwise look identical from the outside.
func (g *Gate) Disabled() bool {
	return g.cfg == nil || len(g.cfg.Providers) == 0
}

// Decision is the answer for one provider.
//
// ResetAt is zero unless the provider is blocked. When it is set it is an
// estimate: the instant the oldest in-window row falls out, which is when
// usage drops *if nothing else is spent in the meantime*. With several rows
// near the cap, one falling out may not be enough — the caller sleeping on it
// will simply wake up, ask again, and be told to wait a little longer.
type Decision struct {
	OK      bool
	Reason  string
	ResetAt time.Time
}

// CanDispatch is the hot-path check, called before acquiring a provider's
// semaphore.
//
// A provider with no entry in the preset is never gated: an operator who has
// not configured a budget for `agy` has not asked orch to ration it.
//
// # Divergence: errors are returned, not swallowed
//
// Python catches `sqlite3.Error` here and yields no rows, with the reasoning
// that the gate "must never crash a run because the database is briefly
// locked". The effect is that a failing read is indistinguishable from zero
// spend, and zero spend always means "go ahead" — the same shape as the bug
// where the gate read the wrong source and saw nothing.
//
// So the error comes back instead. The caller still gets to choose to
// continue, and should; what it no longer gets to do is choose it silently.
// Fail-open belongs in the run loop, where it can be logged once, next to the
// decision it is affecting.
func (g *Gate) CanDispatch(ctx context.Context, provider string) (Decision, error) {
	if g.Disabled() {
		return Decision{OK: true}, nil
	}
	pb, ok := g.cfg.Providers[provider]
	if !ok {
		return Decision{OK: true}, nil
	}
	return g.decide(ctx, provider, pb, g.now())
}

// AllCapped reports whether every configured provider is over threshold —
// the condition under which the run loop sleeps instead of spinning.
//
// False for a disabled gate, and false for a config with no providers: with
// nothing configured there is nothing to be capped by, and answering "true"
// would park a run that has no budget at all.
func (g *Gate) AllCapped(ctx context.Context) (bool, error) {
	if g.Disabled() {
		return false, nil
	}
	now := g.now()
	for _, provider := range g.cfg.Names() {
		d, err := g.decide(ctx, provider, g.cfg.Providers[provider], now)
		if err != nil {
			return false, err
		}
		if d.OK {
			return false, nil
		}
	}
	return true, nil
}

// EarliestReset is the soonest reset among the providers that are currently
// blocked, or the zero time when none are.
//
// Note what it is not: it does not require every provider to be capped. A
// caller that sleeps on this without checking AllCapped first would park a
// run that still had a free provider to dispatch to. Python has the same
// shape and gets away with it because the one caller checks `all_capped()`
// immediately before — which is a property of that call site, not of this
// function.
func (g *Gate) EarliestReset(ctx context.Context) (time.Time, error) {
	var soonest time.Time
	if g.Disabled() {
		return soonest, nil
	}
	now := g.now()
	for _, provider := range g.cfg.Names() {
		d, err := g.decide(ctx, provider, g.cfg.Providers[provider], now)
		if err != nil {
			return time.Time{}, err
		}
		if d.OK || d.ResetAt.IsZero() {
			continue
		}
		if soonest.IsZero() || d.ResetAt.Before(soonest) {
			soonest = d.ResetAt
		}
	}
	return soonest, nil
}

// window is one provider's usage over its rolling window.
type window struct {
	// tokens is the weighted sum the cap is compared against; raw is the
	// same rows as reported, for display.
	tokens float64
	raw    int
	// estimated is whether any row in the window carries orch's guess
	// rather than the CLI's report.
	estimated bool
	// oldest is the timestamp of the earliest row still inside the window.
	// Zero when the window is empty.
	oldest time.Time
}

// usage reads and sums one provider's window.
func (g *Gate) usage(ctx context.Context, provider string, pb ProviderBudget, now time.Time) (window, error) {
	cutoff := now.Add(-time.Duration(pb.WindowHours * float64(time.Hour)))
	rows, err := g.spend.SpendSince(ctx, provider, cutoff)
	if err != nil {
		return window{}, fmt.Errorf("budget window for %s: %w", provider, err)
	}

	var w window
	for _, r := range rows {
		used := r.TokensIn + r.TokensOut
		if used == 0 && r.Estimated {
			used = g.cfg.UnreportedDispatchTokens
		}
		w.raw += used
		w.estimated = w.estimated || r.Estimated
		w.tokens += float64(used) + weightedCacheDelta(r)
		// SpendSince returns rows oldest first and drops any it could not
		// date, so the first one that parses is the oldest. Asking rather
		// than trusting the order costs nothing and does not go wrong if
		// that contract ever changes.
		at, ok := state.ParseTS(r.TS)
		if !ok {
			continue
		}
		if w.oldest.IsZero() || at.Before(w.oldest) {
			w.oldest = at
		}
	}
	return w, nil
}

// Cache weights, relative to a plain input token, from what Anthropic bills:
// reading from the prompt cache costs 10% of an input token, writing to it
// 125%. The window is a proxy for the provider's quota, and a cache read
// burns far less of it than the same count of fresh input.
const (
	cacheReadWeight     = 0.10
	cacheCreationWeight = 1.25
)

// WeightedTokens is a row's usage as the gate counts it: tokens in and out,
// with the cache part of the input weighted by what it bills. A row orch
// estimated with no counts is 0 here; the gate substitutes
// typical_dispatch_tokens for it, which a report of past usage should not.
func WeightedTokens(r state.Spend) float64 {
	return float64(r.TokensIn+r.TokensOut) + weightedCacheDelta(r)
}

// weightedCacheDelta is what weighting a row's cache tokens adds to its raw
// count. TokensIn already includes them at weight 1, so the correction is
// (weight - 1) per token: negative for reads, positive for writes, zero for
// a row with no cache breakdown (older rows, providers without a cache).
func weightedCacheDelta(r state.Spend) float64 {
	return float64(r.CacheReadTokens)*(cacheReadWeight-1) +
		float64(r.CacheCreationTokens)*(cacheCreationWeight-1)
}

// reported is a provider's account windows as its CLI last reported them,
// the ones that have not reset since (fresh); the rest say nothing about now.
type reported struct {
	windows []ProviderWindow
	// usedPct is the fullest fresh window's percentage, worst its name.
	usedPct float64
	worst   string
	capped  bool
	// resetAt is when the last window at or over threshold resets: until
	// then at least one still is.
	resetAt time.Time
	seenAt  time.Time
}

// providerReport reads a `source: provider` budget's windows. ok is false
// when there is nothing fresh to decide by: no reader, no report, or every
// window reset since it was reported.
func (g *Gate) providerReport(ctx context.Context, provider string, pb ProviderBudget, now time.Time) (reported, bool, error) {
	if g.quota == nil {
		return reported{}, false, nil
	}
	q, ok, err := g.quota.LatestQuota(ctx, provider)
	if err != nil {
		return reported{}, false, fmt.Errorf("budget quota for %s: %w", provider, err)
	}
	if !ok {
		return reported{}, false, nil
	}
	r := reported{seenAt: q.SeenAt}
	for _, w := range q.Windows {
		if w.ResetsAt <= now.Unix() {
			continue
		}
		// Two decimals: 0.93 is 93.00000000000001 in floating point.
		pct := math.Round(w.Utilization*100*100) / 100
		reset := time.Unix(w.ResetsAt, 0).UTC()
		r.windows = append(r.windows, ProviderWindow{Window: w.Window, UsedPct: pct, ResetsAt: reset.Format(resetLayout)})
		if len(r.windows) == 1 || pct > r.usedPct {
			r.usedPct, r.worst = pct, w.Window
		}
		if pct >= pb.ThresholdPct {
			r.capped = true
			if reset.After(r.resetAt) {
				r.resetAt = reset
			}
		}
	}
	return r, len(r.windows) > 0, nil
}

// decide turns a window into an answer.
func (g *Gate) decide(ctx context.Context, provider string, pb ProviderBudget, now time.Time) (Decision, error) {
	if pb.Source == SourceProvider {
		r, ok, err := g.providerReport(ctx, provider, pb, now)
		if err != nil {
			return Decision{}, err
		}
		if ok && !r.capped {
			return Decision{OK: true}, nil
		}
		if ok {
			return Decision{
				OK: false,
				Reason: fmt.Sprintf("%s over threshold: the provider reports %s %g%% used, pause at %g%% (account quota, seen %s)",
					provider, r.worst, r.usedPct, pb.ThresholdPct, state.FormatTS(r.seenAt)),
				ResetAt: r.resetAt,
			}, nil
		}
	}

	w, err := g.usage(ctx, provider, pb, now)
	if err != nil {
		return Decision{}, err
	}

	cap := pb.Cap()
	// Strictly less than: usage landing exactly on the cap blocks. Copied
	// from Python rather than reasoned about, and pinned by the vector,
	// because an off-by-one on the boundary is invisible until the one run
	// that hits it exactly.
	if float64(w.tokens) < cap {
		return Decision{OK: true}, nil
	}

	// The oldest in-window row falling out is when usage next drops. With an
	// empty window — which happens when the cap is zero, since 0 >= 0 blocks
	// — Python dates it from `now` instead, so the estimate is still a real
	// instant rather than the epoch.
	oldest := w.oldest
	if oldest.IsZero() {
		oldest = now
	}
	return Decision{
		OK: false,
		Reason: fmt.Sprintf("%s over threshold: %s tokens used, cap %s (%.0f%% of %s)",
			provider,
			pyfmt.Commas(int64(w.tokens)),
			pyfmt.Commas(int64(cap)),
			pb.ThresholdPct,
			pyfmt.Commas(int64(pb.TokenBudget)),
		),
		ResetAt: oldest.Add(time.Duration(pb.WindowHours * float64(time.Hour))),
	}, nil
}
