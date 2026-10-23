package doctor

import (
	"path/filepath"
	"strings"
	"testing"
)

// #400/#392: one <cli>.permissions check per CLI the project uses, none for
// the others, and an honest "unknown" where orch cannot tell.
func TestCheckPermissionsOnePerUsedCLI(t *testing.T) {
	root := t.TempDir()
	agy := filepath.Join(t.TempDir(), "settings.json")
	help := func() string { return "  --add-dir <DIR>\n" }
	byName := map[string]Check{}
	for _, c := range CheckPermissions([]string{"agy", "claude", "codex", "gemini", "muse", "opencode"}, root, agy, help) {
		byName[c.Name] = c
	}
	want := map[string]Status{
		"claude.permissions":   StatusWarn, // no .claude/settings.json
		"agy.permissions":      StatusWarn, // no agy settings
		"codex.permissions":    StatusOK,   // --add-dir grants the state dir per dispatch (#387)
		"opencode.permissions": StatusOK,   // --auto
		"muse.permissions":     StatusOK,   // --approval-mode never --disable-sandbox
		"gemini.permissions":   StatusSkip, // unknown
	}
	if len(byName) != len(want) {
		t.Fatalf("got %d checks, want %d: %+v", len(byName), len(want), byName)
	}
	for name, st := range want {
		if byName[name].Status != st {
			t.Errorf("%s = %+v, want %s", name, byName[name], st)
		}
	}
	if !strings.HasPrefix(byName["gemini.permissions"].Detail, "unknown") {
		t.Errorf("gemini detail = %q, want it to say unknown", byName["gemini.permissions"].Detail)
	}

	if got := CheckPermissions([]string{"codex"}, root, agy, func() string { return "usage: codex exec" }); got[0].Status != StatusWarn {
		t.Errorf("a codex without --add-dir: %+v, want warn", got[0])
	}
	if got := CheckPermissions(nil, root, agy, help); len(got) != 0 {
		t.Errorf("no CLI used: %+v, want no check", got)
	}
}
