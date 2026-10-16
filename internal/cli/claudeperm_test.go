package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/doctor"
)

func runOrch(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root, _ := newRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// #361: the rules are shown before anything is written, and written only on
// a yes.
func TestClaudePermissionsAsksBeforeWriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	if out, err := runOrch(t, "", "init", dir); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	settings := filepath.Join(dir, ".claude", "settings.json")
	cmd := []string{"claude-permissions", "--project-root", dir}

	out, err := runOrch(t, "", append(cmd, "--dry-run")...)
	if err != nil || !strings.Contains(out, "mcp__orch") || fileExists(settings) {
		t.Fatalf("--dry-run: err=%v wrote=%v\n%s", err, fileExists(settings), out)
	}
	if out, err = runOrch(t, "n\n", cmd...); err != nil || fileExists(settings) || !strings.Contains(out, "Nothing written") {
		t.Fatalf("answered no: err=%v wrote=%v\n%s", err, fileExists(settings), out)
	}
	if out, err = runOrch(t, "\n", cmd...); err != nil || !fileExists(settings) {
		t.Fatalf("answered yes (default): err=%v\n%s", err, out)
	}
	if m := doctor.MissingOrchRules(dir); len(m) != 0 {
		t.Errorf("still missing %v", m)
	}
	if out, _ = runOrch(t, "", cmd...); !strings.Contains(out, "already allow") {
		t.Errorf("second run:\n%s", out)
	}
}

func TestClaudePermissionsYesDoesNotAsk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	if out, err := runOrch(t, "", "init", dir); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if out, err := runOrch(t, "", "claude-permissions", "--project-root", dir, "--yes"); err != nil || strings.Contains(out, "[Y/n]") {
		t.Fatalf("--yes: err=%v\n%s", err, out)
	}
	if m := doctor.MissingOrchRules(dir); len(m) != 0 {
		t.Errorf("still missing %v", m)
	}
}

// The wizard offers the rules once the project is written; batch init only
// names the command (TestInitNextStepsOnlyNameCommandsThatExist checks it
// resolves).
func TestInitWizardOffersClaudePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "wiz")
	// Project id, root, template; every other answer and the final offer take
	// their default.
	answers := "wiz\n" + dir + "\nblank\n" + strings.Repeat("\n", 30)
	out, err := runOrch(t, answers, "init")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Add them? [Y/n]") {
		t.Fatalf("the wizard did not offer the claude rules:\n%s", out)
	}
	if strings.Contains(out, "orch claude-permissions --project-root") {
		t.Errorf("the wizard both lists the command and asks:\n%s", out)
	}
	if m := doctor.MissingOrchRules(dir); len(m) != 0 {
		t.Errorf("still missing %v after accepting:\n%s", m, out)
	}

	batch := filepath.Join(t.TempDir(), "batch")
	out, _ = runOrch(t, "", "init", batch)
	if !strings.Contains(out, "orch claude-permissions --project-root") || strings.Contains(out, "Add them?") || fileExists(filepath.Join(batch, ".claude", "settings.json")) {
		t.Errorf("batch init should list the command, not ask or write:\n%s", out)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
