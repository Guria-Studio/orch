package doctor

import "testing"

func TestCheckOnboarding(t *testing.T) {
	for _, tc := range []struct {
		o    Onboarding
		want string
	}{
		{Onboarding{Template: "blank"},
			"init ✓ (blank) → first dispatch · → first finished task · — next: `orch run` to send the first task to an agent"},
		{Onboarding{Dispatched: true, DispatchBackend: "codex"},
			"init ✓ → first dispatch ✓ (codex) → first finished task · — next: let `orch run` finish a task (`orch status` shows where it stands)"},
		{Onboarding{Template: "python-api", Dispatched: true, DispatchBackend: "claude", FinishedTask: true},
			"init ✓ (python-api) → first dispatch ✓ (claude) → first finished task ✓"},
	} {
		c := CheckOnboarding(tc.o)
		if c.Name != "onboarding.progress" || c.Status != StatusOK || c.Detail != tc.want {
			t.Errorf("CheckOnboarding(%+v) = %+v\nwant detail %s", tc.o, c, tc.want)
		}
	}
}
