package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeClaudeSettings(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckClaudeDoneChannelOKWithoutSettings(t *testing.T) {
	if c := CheckClaudeDoneChannel(t.TempDir()); c.Status != StatusOK {
		t.Errorf("c = %+v, want ok", c)
	}
}

// The deny list #328, #336 and #337 were filed from.
func TestCheckClaudeDoneChannelWarnsOnTheReportedDenyList(t *testing.T) {
	root := t.TempDir()
	writeClaudeSettings(t, root, "settings.json", `{"permissions":{
		"allow":["Bash(scripts/task-block.sh:*)"],
		"deny":["Bash(scripts/task-finish.sh:*)","Bash(scripts/task-start.sh:*)","mcp__orch__orch_set_status"]}}`)
	c := CheckClaudeDoneChannel(root)
	if c.Status != StatusWarn {
		t.Fatalf("c = %+v, want warn", c)
	}
	for _, want := range []string{"task-finish.sh:*", "mcp__orch__orch_set_status"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("detail %q does not name %q", c.Detail, want)
		}
	}
	if strings.Contains(c.Detail, "task-start.sh") {
		t.Errorf("detail %q names task-start.sh, which is not a done channel", c.Detail)
	}
}

func TestCheckClaudeDoneChannelReadsTheLocalFile(t *testing.T) {
	root := t.TempDir()
	writeClaudeSettings(t, root, "settings.local.json", `{"permissions":{"deny":["mcp__orch"]}}`)
	if c := CheckClaudeDoneChannel(root); c.Status != StatusWarn || !strings.Contains(c.Detail, "settings.local.json") {
		t.Errorf("c = %+v, want warn naming settings.local.json", c)
	}
}

func TestCheckClaudeDoneChannelIgnoresUnrelatedDenials(t *testing.T) {
	root := t.TempDir()
	writeClaudeSettings(t, root, "settings.json", `{"permissions":{"deny":["Bash(rm:*)","mcp__orch__orch_block","WebFetch"]}}`)
	if c := CheckClaudeDoneChannel(root); c.Status != StatusOK {
		t.Errorf("c = %+v, want ok", c)
	}
}

// #361: a project routing to claude without orch's rules is warned, with the
// command that adds them; broad rules count; a project without claude skips.
func TestCheckClaudePermissions(t *testing.T) {
	root := t.TempDir()
	if c := CheckClaudePermissions(root, false); c.Status != StatusSkip {
		t.Errorf("no claude: %+v, want skip", c)
	}
	c := CheckClaudePermissions(root, true)
	if c.Status != StatusWarn || !strings.Contains(c.Remediation, ClaudePermissionsCommand) || !strings.Contains(c.Detail, "mcp__orch") {
		t.Errorf("no settings: %+v", c)
	}
	writeClaudeSettings(t, root, "settings.json", `{"permissions":{"allow":["mcp__orch"]}}`)
	writeClaudeSettings(t, root, "settings.local.json", `{"permissions":{"allow":["Bash(scripts/task-finish.sh:*)"]}}`)
	if got := MissingOrchRules(root); len(got) != 1 || got[0] != "Bash(scripts/task-block.sh:*)" {
		t.Errorf("missing = %v, want only task-block.sh (rules add up across both files)", got)
	}
	writeClaudeSettings(t, root, "settings.local.json", `{"permissions":{"allow":["Bash"]}}`)
	if c := CheckClaudePermissions(root, true); c.Status != StatusOK {
		t.Errorf("mcp__orch + Bash: %+v, want ok", c)
	}
}
