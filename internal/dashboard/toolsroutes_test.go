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
