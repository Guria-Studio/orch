package dashboard

import (
	"testing"

	"github.com/hectorcanaimero/orch/internal/estimate"
)

// The demo has history and work left: the estimate rests on its attempts.
func TestEstimateAgainstTheDemoProject(t *testing.T) {
	s := demoServer(t)
	var got estimate.Estimate
	decode(t, get(t, s, "/api/estimate"), &got)
	if len(got.Tasks) == 0 || got.PastAttempts == 0 || got.CostUSD <= 0 || got.WallS < got.CriticalPathS || got.Slots == 0 {
		t.Errorf("estimate = %+v", got)
	}
}
