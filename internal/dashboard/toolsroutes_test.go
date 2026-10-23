package dashboard

import (
	"net/http"
	"testing"

	"github.com/hectorcanaimero/orch/internal/doctor"
)

func TestToolsListsWhatTheProjectUses(t *testing.T) {
	saved := describeTools
	t.Cleanup(func() { describeTools = saved })
	var asked doctor.ToolsConfig
	describeTools = func(cfg doctor.ToolsConfig) []doctor.Tool {
		asked = cfg
		return []doctor.Tool{{ID: "claude", Group: doctor.ToolGroupAgents, Used: true, Found: true, Version: "2.1.301"}}
	}

	s := newReadServer(t, &fakeState{})
	var got toolsPayload
	decode(t, get(t, s, "/api/tools"), &got)
	if len(got.Tools) != 1 || got.Tools[0].ID != "claude" || !got.Tools[0].Found {
		t.Errorf("tools = %+v", got.Tools)
	}
	if asked.Root != s.paths.Root || len(asked.Backends) == 0 {
		t.Errorf("asked with %+v, want the project root and the backends its tasks route to", asked)
	}
}

// What a machine has installed is the operator's business, not a client's.
func TestToolsIsOperatorOnly(t *testing.T) {
	s := newGatedServer(t, &fakeState{})
	if resp := get(t, s, "/api/tools?token=test-token-stakeholder"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("stakeholder token: status = %d, want 403", resp.StatusCode)
	}
}

// The sidebar's count reuses the last probe for a minute instead of running
// every CLI on each page load.
func TestToolsMissingCountsFromTheCachedProbe(t *testing.T) {
	saved := describeTools
	t.Cleanup(func() { describeTools = saved })
	probes := 0
	describeTools = func(doctor.ToolsConfig) []doctor.Tool {
		probes++
		return []doctor.Tool{
			{ID: "claude", Used: true, Found: true},
			{ID: "codex", Used: true, Found: false},
			{ID: "gemini", Used: false, Found: false},
		}
	}
	s := newReadServer(t, &fakeState{})
	for range 2 {
		var got map[string]int
		decode(t, get(t, s, "/api/tools/missing"), &got)
		if got["missing"] != 1 {
			t.Errorf("missing = %d, want 1 (codex: used, not installed)", got["missing"])
		}
	}
	if probes != 1 {
		t.Errorf("probed %d times, want 1: the second answer comes from the cache", probes)
	}
}
