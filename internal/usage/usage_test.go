package usage

import (
	"testing"

	"github.com/Guria-Studio/orch/internal/state"
)

func TestBuildSplitsByModelTaskAndAttempt(t *testing.T) {
	rows := []state.Spend{
		// T1 failed once on codex and passed on its retry.
		{TaskID: "T1", Backend: "codex", Model: "gpt", Attempt: 1, TokensIn: 1000, CacheReadTokens: 800, TokensOut: 50, ReasoningTokens: 30},
		{TaskID: "T1", Backend: "codex", Model: "gpt", Attempt: 2, TokensIn: 2000, CacheReadTokens: 1500, TokensOut: 100, ReasoningTokens: 40},
		{TaskID: "T2", Backend: "claude", Model: "sonnet", Attempt: 1, TokensIn: 300, CacheCreationTokens: 100, TokensOut: 20, CostUSD: 0.5},
		// A row from before migration 008: no attempt.
		{TaskID: "T2", Backend: "claude", Model: "sonnet", TokensIn: 10, TokensOut: 1, Estimated: true},
	}
	u := Build(rows)

	want := Tokens{Dispatches: 4, Input: 700 + 200 + 10, CachedInput: 2300, CacheWrite: 100, Output: 171, Reasoning: 70, CostUSD: 0.5, Estimated: true}
	if u.Total != want {
		t.Errorf("total = %+v\nwant    %+v", u.Total, want)
	}
	if u.Total.Total() != 910+2300+100+171 {
		t.Errorf("Total() = %d", u.Total.Total())
	}
	if len(u.ByModel) != 2 || u.ByModel[0].Model != "gpt" || u.ByModel[0].Reasoning != 70 {
		t.Errorf("by model = %+v; want gpt first, with its reasoning", u.ByModel)
	}
	if len(u.ByTask) != 2 || u.ByTask[0].TaskID != "T1" {
		t.Fatalf("by task = %+v; want T1, the heavier, first", u.ByTask)
	}
	t1 := u.ByTask[0]
	if len(t1.Attempts) != 2 || t1.Attempts[0].Attempt != 1 || t1.Attempts[1].Attempt != 2 {
		t.Errorf("T1 attempts = %+v", t1.Attempts)
	}
	if t1.RetryTokens != 500+1500+100 || u.RetryTokens != t1.RetryTokens {
		t.Errorf("retry tokens = %d (task), %d (total); want 2100", t1.RetryTokens, u.RetryTokens)
	}
	if t2 := u.ByTask[1]; len(t2.Attempts) != 2 || t2.Attempts[0].Attempt != 0 || t2.RetryTokens != 0 {
		t.Errorf("T2 = %+v; want an attempt-0 row apart and no retry", t2)
	}
}

func TestBuildOnNoSpendIsEmptyNotNull(t *testing.T) {
	u := Build(nil)
	if u.ByModel == nil || u.ByTask == nil || u.Total.Dispatches != 0 {
		t.Errorf("Build(nil) = %+v", u)
	}
}
