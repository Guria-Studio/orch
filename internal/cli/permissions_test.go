package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/doctor"
)

// fakeCLIs puts executables with these names alone on PATH.
func fakeCLIs(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o700); err != nil { // #nosec G306 -- a fake executable
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

// tempAgySettings points agySettingsPath at a file of the test's own.
func tempAgySettings(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	saved := agySettingsPath
	agySettingsPath = func() string { return path }
	t.Cleanup(func() { agySettingsPath = saved })
	return path
}

// #400: batch init grants orch's rules to the CLIs the project routes to,
// and prints where. agy is on PATH too, but every tier went to claude: agy's
// settings are not touched.
func TestInitBatchGrantsThePermissionsOfTheRoutedCLIs(t *testing.T) {
	agy := tempAgySettings(t)
	fakeCLIs(t, "claude", "agy")
	dir := filepath.Join(t.TempDir(), "proj")
	out, err := runOrch(t, "", "init", dir)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if m := doctor.MissingOrchRules(dir); len(m) != 0 {
		t.Errorf("claude still lacks %v:\n%s", m, out)
	}
	if !strings.Contains(out, filepath.Join(dir, ".claude", "settings.json")) || strings.Contains(out, "Add them?") {
		t.Errorf("want the path written and no question:\n%s", out)
	}
	if fileExists(agy) {
		t.Errorf("agy is not routed, yet its settings were written:\n%s", out)
	}
}

// agy keeps its rules in the user's home: written by default too, with the
// exact path and rules printed.
func TestInitBatchGrantsAgyWhenRouted(t *testing.T) {
	agy := tempAgySettings(t)
	fakeCLIs(t, "agy")
	dir := filepath.Join(t.TempDir(), "proj")
	out, err := runOrch(t, "", "init", dir)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if m := doctor.MissingAgyRules(agy, doctor.AgyToolRules); len(m) != 0 {
		t.Errorf("agy still lacks %v:\n%s", m, out)
	}
	if !strings.Contains(out, agy) || !strings.Contains(out, "command(scripts/task-finish.sh)") {
		t.Errorf("want the path and the rules printed:\n%s", out)
	}
}

// --no-permissions writes nothing and names the command that adds them.
func TestInitNoPermissions(t *testing.T) {
	agy := tempAgySettings(t)
	fakeCLIs(t, "agy")
	dir := filepath.Join(t.TempDir(), "proj")
	out, err := runOrch(t, "", "init", dir, "--no-permissions")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if fileExists(agy) || fileExists(filepath.Join(dir, ".claude", "settings.json")) {
		t.Errorf("--no-permissions wrote settings:\n%s", out)
	}
	if !strings.Contains(out, doctor.AgyPermissionsCommand) {
		t.Errorf("want the command named:\n%s", out)
	}
}

// #400's run-time guard: before the first dispatch, `orch run` names a used
// CLI whose rules are missing, and the command that adds them.
func TestWarnMissingPermissions(t *testing.T) {
	tempAgySettings(t)
	fakeCLIs(t, "claude")
	var out strings.Builder
	warnMissingPermissions(&out, t.TempDir(), []string{"claude"})
	if !strings.Contains(out.String(), doctor.ClaudePermissionsCommand) {
		t.Errorf("out = %q, want the command named", out.String())
	}
}
