package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestToolsSaysWhatTheProjectUsesAndWhatIsHere(t *testing.T) {
	onPath := map[string]string{"claude": "2.1.301", "git": "git version 2.43.0", "gh": "gh version 2.60.0"}
	orig := lookVersion
	lookVersion = func(command string) (bool, string, string) {
		v, ok := onPath[command]
		if !ok {
			return false, "", ""
		}
		return true, "/usr/bin/" + command, v
	}
	t.Cleanup(func() { lookVersion = orig })

	root := t.TempDir()
	bin := filepath.Join(root, "node_modules", ".bin")
	pkg := filepath.Join(root, "node_modules", "@playwright", "test")
	for _, dir := range []string{bin, pkg} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "playwright"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version":"1.56.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tools := map[string]Tool{}
	for _, tool := range Tools(ToolsConfig{
		Root: root, Backends: []string{"claude", "codex"},
		VCS: VCSConfig{AutoPR: true, Provider: "github"}, Tunnel: false, E2E: true,
	}) {
		tools[tool.ID] = tool
	}

	for id, want := range map[string]struct{ used, found bool }{
		"claude": {true, true}, "codex": {true, false}, "gemini": {false, false},
		"git": {true, true}, "gh": {true, true}, "glab": {false, false},
		"cloudflared": {false, false}, "tailscale": {false, false}, "playwright": {true, true},
	} {
		got, ok := tools[id]
		if !ok {
			t.Errorf("%s is missing", id)
			continue
		}
		if got.Used != want.used || got.Found != want.found {
			t.Errorf("%s: used=%v found=%v, want used=%v found=%v", id, got.Used, got.Found, want.used, want.found)
		}
	}
	if v := tools["playwright"].Version; v != "1.56.1" {
		t.Errorf("playwright version = %q, want the project's 1.56.1", v)
	}
	if got := tools["codex"].Install; got != "npm install -g @openai/codex" {
		t.Errorf("codex install = %q", got)
	}
	if tools["agy"].Install != "" {
		t.Errorf("agy install = %q, orch knows no command for it", tools["agy"].Install)
	}
}

// An agent CLI older than its latest release carries the latest and the
// command that updates it; one that is current carries no command (#448).
func TestToolsSaysWhichAgentCLIsAreBehind(t *testing.T) {
	onPath := map[string]string{"claude": "2.1.290 (Claude Code)", "codex": "codex-cli 0.160.0"}
	orig := lookVersion
	lookVersion = func(command string) (bool, string, string) {
		v, ok := onPath[command]
		if !ok {
			return false, "", ""
		}
		return true, "/usr/lib/node_modules/x/" + command, v
	}
	t.Cleanup(func() { lookVersion = orig })
	latest := map[string]string{"@anthropic-ai/claude-code": "2.1.301", "@openai/codex": "0.160.0"}

	tools := map[string]Tool{}
	for _, tool := range Tools(ToolsConfig{Root: t.TempDir(), Latest: func(_ context.Context, pkg string) (string, error) {
		return latest[pkg], nil
	}}) {
		tools[tool.ID] = tool
	}
	if c := tools["claude"]; c.Latest != "2.1.301" || c.Update != "npm install -g @anthropic-ai/claude-code@latest" {
		t.Errorf("claude = %+v, want latest 2.1.301 and the npm command", c)
	}
	if c := tools["codex"]; c.Latest != "0.160.0" || c.Update != "" {
		t.Errorf("codex = %+v, want latest 0.160.0 and no command", c)
	}
}
