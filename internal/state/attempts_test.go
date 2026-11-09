package state

import (
	"context"
	"testing"
)

// TestAttemptsKeepEveryRetry (#538): dispatches is keyed (run, task), so a
// retry in the same run overwrote the attempt before it. attempts keeps one
// row per finished attempt, even when a CI retry starts again at attempt 1.
func TestAttemptsKeepEveryRetry(t *testing.T) {
	ctx := context.Background()
	b := seeded(t, "T1", "T2")
	rows := []Attempt{
		{RunID: "r1", TaskID: "T1", Attempt: 1, Backend: "claude", Model: "sonnet", Tier: "standard",
			Result: "success", Verify: "fail", Review: "blocks", Reviewer: "codex",
			Findings: []AttemptFinding{{Severity: "blocking", File: "a.go", Line: 3, Title: "nil deref"}},
			TokensIn: 10, TokensOut: 5, CostUSD: 0.5, DurationS: 30, FinishedAt: "2026-11-08T10:00:00Z"},
		{RunID: "r1", TaskID: "T1", Attempt: 2, Backend: "claude", Model: "sonnet", Tier: "standard",
			Result: "fail", FailureClass: "transient", Reason: "rate limited", CostUSD: 0.25, FinishedAt: "2026-11-08T10:05:00Z"},
		{RunID: "r1", TaskID: "T1", Attempt: 1, Backend: "claude", Model: "sonnet", Tier: "standard",
			Result: "success", Verify: "pass", CostUSD: 0.1, FinishedAt: "2026-11-08T11:00:00Z"},
	}
	for _, a := range rows {
		if err := b.RecordAttempt(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	got, err := b.Attempts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("attempts = %d, want 3 (a CI retry restarting at 1 must not overwrite)", len(got))
	}
	if got[0].Attempt != 1 || got[1].Attempt != 2 || got[2].Attempt != 1 {
		t.Errorf("order = %d %d %d, want 1 2 1 (oldest first)", got[0].Attempt, got[1].Attempt, got[2].Attempt)
	}
	first := got[0]
	if first.Tier != "standard" || first.Verify != "fail" || first.Review != "blocks" || first.Reviewer != "codex" ||
		len(first.Findings) != 1 || first.Findings[0].Title != "nil deref" || first.Findings[0].Line != 3 ||
		first.CostUSD != 0.5 || first.TokensIn != 10 {
		t.Errorf("first row = %+v", first)
	}
	if got[1].FailureClass != "transient" || got[1].Reason != "rate limited" {
		t.Errorf("second row = %+v", got[1])
	}
}

// TestCIVerdictLandsOnTheLatestAttempt (#538): how CI went is the verdict on
// the attempt that pushed, so "CI failed after verify" can be counted. A
// failure that is retried (IncrementCIAttempts) counts as much as a final one.
func TestCIVerdictLandsOnTheLatestAttempt(t *testing.T) {
	ctx := context.Background()
	b := seeded(t, "T1", "T2")
	for _, a := range []Attempt{
		{RunID: "r1", TaskID: "T1", Attempt: 1, Result: "success", Verify: "pass"},
		{RunID: "r1", TaskID: "T2", Attempt: 1, Result: "success", Verify: "pass"},
	} {
		if err := b.RecordAttempt(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.IncrementCIAttempts(ctx, "T1"); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordAttempt(ctx, Attempt{RunID: "r1", TaskID: "T1", Attempt: 1, Result: "success", Verify: "pass"}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetTaskCIStatus(ctx, "T1", "pending"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetTaskCIStatus(ctx, "T1", "success"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetTaskCIStatus(ctx, "T2", "failure"); err != nil {
		t.Fatal(err)
	}
	got, err := b.Attempts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"failure", "failure", "success"} // T1#1, T2#1, T1 retry
	for i, a := range got {
		if a.CI != want[i] {
			t.Errorf("attempt %d (%s) ci = %q, want %q", i, a.TaskID, a.CI, want[i])
		}
	}
}

// TestAttemptsKeepTheEffectiveRoute (#580): an attempt keeps the effort and
// service tier it asked for beside what the CLI says it ran; unknown stays
// empty.
func TestAttemptsKeepTheEffectiveRoute(t *testing.T) {
	ctx := context.Background()
	b := seeded(t, "T1")
	want := Attempt{RunID: "r1", TaskID: "T1", Attempt: 1, Backend: "codex", Model: "gpt-6-luna", Tier: "cheap",
		Effort: "high", ServiceTier: "flex",
		EffectiveModel: "gpt-6-astra", EffectiveEffort: "high", EffectiveServiceTier: "default",
		Result: "success", FinishedAt: "2026-11-08T10:00:00Z"}
	if err := b.RecordAttempt(ctx, want); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordAttempt(ctx, Attempt{RunID: "r1", TaskID: "T1", Attempt: 2, Backend: "claude", Model: "sonnet", Result: "fail"}); err != nil {
		t.Fatal(err)
	}
	got, err := b.Attempts(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("attempts = %+v, %v", got, err)
	}
	g := got[0]
	if g.Effort != "high" || g.ServiceTier != "flex" || g.EffectiveModel != "gpt-6-astra" || g.EffectiveEffort != "high" || g.EffectiveServiceTier != "default" {
		t.Errorf("first = %+v", g)
	}
	if g := got[1]; g.EffectiveModel != "" || g.EffectiveEffort != "" || g.EffectiveServiceTier != "" {
		t.Errorf("unknown must stay empty: %+v", g)
	}
}
