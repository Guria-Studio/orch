package dashboard

import (
	"context"
	"net/http"
	"os"
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

// A configured command whose program is not installed is said on the Tools
// page too, with the same words as `orch doctor` (#440).
func TestToolsNamesConfiguredCommandsNotFound(t *testing.T) {
	saved := describeTools
	t.Cleanup(func() { describeTools = saved })
	describeTools = func(doctor.ToolsConfig) []doctor.Tool { return nil }
	s := newReadServer(t, &fakeState{})
	if err := os.WriteFile(s.paths.ConfigYAML, []byte("verify:\n  enabled: true\n  app:\n    start: tue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got toolsPayload
	decode(t, get(t, s, "/api/tools"), &got)
	if len(got.Commands) != 1 || got.Commands[0].Detail != `verify.app.start: "tue" not found on PATH` {
		t.Errorf("commands = %+v, want verify.app.start's tue", got.Commands)
	}
}

// The Tools page asks for agent CLIs' latest releases through
// Options.CLILatest, unless the project turned it off (#448).
func TestToolsAsksForLatestUnlessTurnedOff(t *testing.T) {
	saved := describeTools
	t.Cleanup(func() { describeTools = saved })
	var asked doctor.ToolsConfig
	describeTools = func(cfg doctor.ToolsConfig) []doctor.Tool { asked = cfg; return nil }
	s := newReadServer(t, &fakeState{})
	s.cliLatest = func(context.Context, string) (string, error) { return "9.9.9", nil }

	get(t, s, "/api/tools")
	if asked.Latest == nil {
		t.Fatal("Latest is nil with update.check_clis on by default")
	}
	if err := os.WriteFile(s.paths.ConfigYAML, []byte("update:\n  check_clis: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	get(t, s, "/api/tools")
	if asked.Latest != nil {
		t.Error("Latest is set with update.check_clis: false")
	}
}
