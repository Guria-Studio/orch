package dashboard

import (
	"net/http"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
)

// TestAgentMetricsServesWhatOrchMetricsPrints (#538): the operator's
// dashboard reads the same figures `orch metrics` prints, and the
// stakeholder profile does not.
func TestAgentMetricsServesWhatOrchMetricsPrints(t *testing.T) {
	st := &fakeState{
		tasks: []state.TaskRuntime{{ID: "T-1", Status: model.StatusDone}},
		attempts: []state.Attempt{
			{TaskID: "T-1", Attempt: 1, Backend: "claude", Model: "sonnet", Tier: "standard", Result: "success", Verify: "fail", CostUSD: 1},
			{TaskID: "T-1", Attempt: 2, Backend: "claude", Model: "sonnet", Tier: "standard", Result: "success", Verify: "pass", CostUSD: 2},
		},
	}
	var got project.Metrics
	decode(t, get(t, newReadServer(t, st), "/api/agent-metrics"), &got)
	if got.Total.Attempts != 2 || got.Total.DoneTasks != 1 || got.Total.CostPerDone != 3 || got.Total.SentBack != 1 ||
		len(got.ByRoute) != 1 || got.ByRoute[0].Key != "claude/sonnet" {
		t.Errorf("metrics = %+v", got)
	}

	if resp := get(t, newGatedServer(t, st), "/api/agent-metrics?token=test-token-stakeholder"); resp.StatusCode == http.StatusOK {
		t.Errorf("stakeholder got the agent metrics (status %d)", resp.StatusCode)
	}
}
