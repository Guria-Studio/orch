package budget

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/state"
)

// fakeQuota answers LatestQuota from one report per backend and counts the
// reads, so a test can tell a gate that never asked from one that ignored
// the answer (rule 25).
type fakeQuota struct {
	reports map[string]state.ProviderQuota
	err     error
	calls   int
}

func (f *fakeQuota) LatestQuota(_ context.Context, backend string) (state.ProviderQuota, bool, error) {
	f.calls++
	if f.err != nil {
		return state.ProviderQuota{}, false, f.err
	}
	q, ok := f.reports[backend]
	return q, ok, nil
}

// spendAndQuota is a state backend as far as the gate can see: it reads
// spend and, being the real thing, the quota reports too.
type spendAndQuota struct {
	*fakeSpend
	*fakeQuota
}

const quotaNow = "2026-10-23T01:00:00Z"

func codexBudget(source string) *Config {
	return &Config{Providers: map[string]ProviderBudget{
		"codex": {WindowHours: 5, TokenBudget: 400_000, ThresholdPct: 90, Source: source},
	}}
}

// codexReport is a report seen at quotaNow minus an hour whose five_hour
// window is used to the given fraction and resets resetIn from quotaNow.
func codexReport(t *testing.T, used float64, resetIn time.Duration) state.ProviderQuota {
	now := mustParse(t, quotaNow)
	return state.ProviderQuota{
		SeenAt: now.Add(-time.Hour),
		Windows: []state.QuotaWindow{
			{Window: "five_hour", Utilization: used, ResetsAt: now.Add(resetIn).Unix()},
			{Window: "seven_day", Utilization: 0.44, ResetsAt: now.Add(72 * time.Hour).Unix()},
		},
	}
}

// overCap is a token window far past codex's 360,000 cap: the per-project
// count that deferred the reporter's tasks while the plan was at 7% (#577).
func overCap() map[string][]state.Spend {
	return map[string][]state.Spend{"codex": {{TS: "2026-10-23T00:30:00Z", Backend: "codex", TokensIn: 500_000, TokensOut: 7_223}}}
}

// With source: provider and a fresh report, the account's own percentage
// decides, and the token window is not even read.
func TestProviderSourceDecidesByTheReportedWindows(t *testing.T) {
	ctx := context.Background()
	t.Run("under threshold dispatches despite a full token window", func(t *testing.T) {
		sp := &fakeSpend{rows: overCap(), err: errors.New("the token window must not be read")}
		q := &fakeQuota{reports: map[string]state.ProviderQuota{"codex": codexReport(t, 0.07, 2*time.Hour)}}
		d, err := gateAt(t, spendAndQuota{sp, q}, codexBudget("provider"), quotaNow).CanDispatch(ctx, "codex")
		if err != nil || !d.OK {
			t.Fatalf("decision = %+v, %v; want OK", d, err)
		}
		if len(sp.calls) != 0 || q.calls != 1 {
			t.Errorf("spend reads = %d, quota reads = %d; want 0 and 1", len(sp.calls), q.calls)
		}
	})
	t.Run("at threshold defers until the window resets", func(t *testing.T) {
		sp := &fakeSpend{err: errors.New("the token window must not be read")}
		q := &fakeQuota{reports: map[string]state.ProviderQuota{"codex": codexReport(t, 0.93, 2*time.Hour)}}
		d, err := gateAt(t, spendAndQuota{sp, q}, codexBudget("provider"), quotaNow).CanDispatch(ctx, "codex")
		if err != nil || d.OK {
			t.Fatalf("decision = %+v, %v; want deferred", d, err)
		}
		if want := mustParse(t, quotaNow).Add(2 * time.Hour); !d.ResetAt.Equal(want) {
			t.Errorf("ResetAt = %v, want %v (the window's own reset)", d.ResetAt, want)
		}
		want := "codex over threshold: the provider reports five_hour 93% used, pause at 90% (account quota, seen 2026-10-23T00:00:00Z)"
		if d.Reason != want {
			t.Errorf("Reason =\n %q\nwant\n %q", d.Reason, want)
		}
	})
}

// A report that is stale (its window has reset since) or absent says nothing
// about now: the gate falls back to the per-project token window.
func TestProviderSourceFallsBackToTheTokenWindow(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		reports map[string]state.ProviderQuota
	}{
		{"no report yet", nil},
		{"every window reset since", map[string]state.ProviderQuota{"codex": {
			SeenAt:  mustParse(t, quotaNow).Add(-6 * time.Hour),
			Windows: []state.QuotaWindow{{Window: "five_hour", Utilization: 0.10, ResetsAt: mustParse(t, quotaNow).Add(-time.Minute).Unix()}},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sp := &fakeSpend{rows: overCap()}
			q := &fakeQuota{reports: tc.reports}
			d, err := gateAt(t, spendAndQuota{sp, q}, codexBudget("provider"), quotaNow).CanDispatch(ctx, "codex")
			if err != nil || d.OK {
				t.Fatalf("decision = %+v, %v; want the token window's deferral", d, err)
			}
			if !strings.Contains(d.Reason, "507,223 tokens used, cap 360,000") || len(sp.calls) != 1 {
				t.Errorf("Reason = %q, spend reads = %d; want the token window's", d.Reason, len(sp.calls))
			}
		})
	}
}

// The default source never asks for a report: a project that did not opt in
// keeps exactly the gate it had.
func TestTokenSourceNeverReadsTheQuota(t *testing.T) {
	sp := &fakeSpend{rows: overCap()}
	q := &fakeQuota{err: errors.New("the quota must not be read")}
	d, err := gateAt(t, spendAndQuota{sp, q}, codexBudget(""), quotaNow).CanDispatch(context.Background(), "codex")
	if err != nil || d.OK || q.calls != 0 {
		t.Errorf("decision = %+v, %v, quota reads = %d; want the token deferral and no quota read", d, err, q.calls)
	}
}

// A gate built over something that cannot report quotas falls back too.
func TestProviderSourceWithoutAQuotaReader(t *testing.T) {
	d, err := gateAt(t, &fakeSpend{rows: overCap()}, codexBudget("provider"), quotaNow).CanDispatch(context.Background(), "codex")
	if err != nil || d.OK {
		t.Errorf("decision = %+v, %v; want the token window's deferral", d, err)
	}
}

// A failing quota read is an error, like a failing spend read: the caller
// decides to fail open, and says so.
func TestProviderSourceQuotaErrorIsReturned(t *testing.T) {
	q := &fakeQuota{err: errors.New("disk on fire")}
	if _, err := gateAt(t, spendAndQuota{&fakeSpend{}, q}, codexBudget("provider"), quotaNow).CanDispatch(context.Background(), "codex"); err == nil {
		t.Error("err = nil, want the quota read's error")
	}
}

// The snapshot names the source and carries the reported windows, so the
// dashboard and `orch budget show` can say which number is deciding.
func TestSnapshotShowsTheProviderSource(t *testing.T) {
	q := &fakeQuota{reports: map[string]state.ProviderQuota{"codex": codexReport(t, 0.93, 2*time.Hour)}}
	snap, err := gateAt(t, spendAndQuota{&fakeSpend{rows: overCap()}, q}, codexBudget("provider"), quotaNow).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := snap["codex"]
	if s.Source != "provider" || !s.Capped || s.UsagePct != 93 || s.ResetAt == nil || *s.ResetAt != "2026-10-23T03:00:00Z" {
		t.Errorf("snapshot = %+v", s)
	}
	if len(s.ProviderWindows) != 2 || s.ProviderWindows[0].Window != "five_hour" || s.ProviderWindows[0].UsedPct != 93 {
		t.Errorf("provider windows = %+v", s.ProviderWindows)
	}
	// The per-project count is still there to read, just not deciding.
	if s.TokensUsed != 507_223 {
		t.Errorf("tokens used = %d, want the token window's 507,223", s.TokensUsed)
	}

	tok, err := gateAt(t, &fakeSpend{rows: overCap()}, codexBudget(""), quotaNow).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s := tok["codex"]; s.Source != "" || s.ProviderWindows != nil || !s.Capped {
		t.Errorf("token snapshot = %+v; want no source field and the token cap", s)
	}
}

// `source` is optional and has two spellings; anything else is refused by
// name, like the other fields.
func TestLoadConfigReadsTheSource(t *testing.T) {
	body := func(source string) string {
		return "presets:\n  p:\n    codex:\n      window_hours: 5\n      token_budget: 400000\n      threshold_pct: 90\n" + source
	}
	for _, tc := range []struct{ line, want string }{
		{"", ""},
		{"      source: tokens\n", ""},
		{"      source: provider\n", "provider"},
	} {
		cfg, err := LoadConfig(writeYAML(t, body(tc.line)), "p")
		if err != nil {
			t.Fatalf("%q: %v", tc.line, err)
		}
		if got := cfg.Providers["codex"].Source; got != tc.want {
			t.Errorf("%q: Source = %q, want %q", tc.line, got, tc.want)
		}
	}
	if _, err := LoadConfig(writeYAML(t, body("      source: account\n")), "p"); err == nil || !strings.Contains(err.Error(), "source") {
		t.Errorf("source: account: err = %v, want one naming source", err)
	}
}

// `orch budget set --source` writes the key, and only that key.
func TestSetLimitsWritesTheSource(t *testing.T) {
	path := writeYAML(t, setFixture)
	src := "provider"
	got, err := SetLimits(path, "conservative", "codex", Limits{Source: &src})
	if err != nil {
		t.Fatal(err)
	}
	if got != (ProviderBudget{WindowHours: 5, TokenBudget: 1_000_000, ThresholdPct: 60, Source: "provider"}) {
		t.Errorf("returned %+v", got)
	}
	raw, _ := os.ReadFile(path) // #nosec G304 -- path is under t.TempDir()
	if !strings.Contains(string(raw), "source: provider") {
		t.Errorf("file:\n%s", raw)
	}
	bad := "account"
	if _, err := SetLimits(path, "conservative", "codex", Limits{Source: &bad}); !errors.Is(err, ErrInvalidLimits) {
		t.Errorf("source account: err = %v, want ErrInvalidLimits", err)
	}
}

// The real backend reports quotas, so NewGate picks it up without wiring.
var _ QuotaReader = (*state.SQLite)(nil)
