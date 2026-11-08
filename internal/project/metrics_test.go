package project

import (
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// TestComputeMetrics (#538): the agents' quality figures from the attempts
// table, in total and by route and tier.
func TestComputeMetrics(t *testing.T) {
	leak := state.AttemptFinding{Severity: "blocking", File: "a.go", Title: "leak"}
	style := state.AttemptFinding{Severity: "minor", File: "b.go", Title: "naming"}
	attempts := []state.Attempt{
		// T1: done on its first attempt, verify passed, CI green.
		{TaskID: "T1", Attempt: 1, Backend: "claude", Model: "sonnet", Tier: "standard", Result: "success", Verify: "pass", CI: "success", CostUSD: 1},
		// T2: sent back by the review (leak + naming), the retry fixed the
		// leak, not the naming; then CI failed and a CI retry made it.
		{TaskID: "T2", Attempt: 1, Backend: "claude", Model: "sonnet", Tier: "standard", Result: "success", Verify: "fail", Review: "blocks", Findings: []state.AttemptFinding{leak, style}, CostUSD: 2},
		{TaskID: "T2", Attempt: 2, Backend: "claude", Model: "sonnet", Tier: "standard", Result: "success", Verify: "pass", Review: "pass", Findings: []state.AttemptFinding{style}, CI: "failure", CostUSD: 2},
		{TaskID: "T2", Attempt: 1, Backend: "claude", Model: "sonnet", Tier: "standard", Result: "success", Verify: "pass", CI: "success", CostUSD: 1},
		// T3: a cheap route that failed and is not done.
		{TaskID: "T3", Attempt: 1, Backend: "codex", Model: "mini", Tier: "cheap", Result: "fail", FailureClass: "transient", CostUSD: 0.5},
	}
	tasks := []state.TaskRuntime{
		{ID: "T1", Status: model.StatusDone},
		{ID: "T2", Status: model.StatusDone},
		{ID: "T3", Status: model.StatusTodo},
		{ID: "T4", Status: model.StatusDone}, // done before attempts were kept: no data
	}
	m := ComputeMetrics(attempts, tasks)

	tot := m.Total
	if tot.Attempts != 5 || tot.Tasks != 3 || tot.DoneTasks != 2 || tot.FirstAttempt != 1 {
		t.Fatalf("total counts = %+v", tot)
	}
	if tot.FirstAttemptRate != 0.5 || tot.RetriesPerDone != 1 || tot.CostPerDone != 3 {
		t.Errorf("total rates = first %.2f retries %.2f cost %.2f, want 0.5 1 3", tot.FirstAttemptRate, tot.RetriesPerDone, tot.CostPerDone)
	}
	if tot.Verified != 4 || tot.SentBack != 1 || tot.SendBackRate != 0.25 {
		t.Errorf("verify = %d verified, %d sent back, rate %.2f; want 4 1 0.25", tot.Verified, tot.SentBack, tot.SendBackRate)
	}
	// leak fixed by attempt 2; naming carried into attempt 2 (ignored), and
	// attempt 2's naming shipped (ignored again).
	if tot.FindingsFixed != 1 || tot.FindingsIgnored != 2 {
		t.Errorf("findings fixed %d ignored %d, want 1 2", tot.FindingsFixed, tot.FindingsIgnored)
	}
	if tot.CIAfterVerify != 3 || tot.CIFailedAfterVerify != 1 {
		t.Errorf("CI after verify = %d, failed %d; want 3 1", tot.CIAfterVerify, tot.CIFailedAfterVerify)
	}

	if len(m.ByRoute) != 2 || m.ByRoute[0].Key != "claude/sonnet" || m.ByRoute[1].Key != "codex/mini" {
		t.Fatalf("routes = %+v", m.ByRoute)
	}
	if r := m.ByRoute[1]; r.Attempts != 1 || r.Tasks != 1 || r.DoneTasks != 0 || r.CostPerDone != 0 {
		t.Errorf("codex/mini = %+v", r)
	}
	if len(m.ByTier) != 2 || m.ByTier[0].Key != "cheap" || m.ByTier[1].Key != "standard" || m.ByTier[1].DoneTasks != 2 {
		t.Errorf("tiers = %+v", m.ByTier)
	}
}
