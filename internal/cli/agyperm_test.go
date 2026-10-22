package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/doctor"
)

// No test in this binary — testscripts included, which re-exec it — may read
// or write the real agy settings in the user's home: agy is on PATH on a
// developer's machine, and a wizard test answering yes would write there.
func init() {
	dir, err := os.MkdirTemp("", "orch-test-agy-")
	if err != nil {
		panic(err)
	}
	path := filepath.Join(dir, "settings.json")
	agySettingsPath = func() string { return path }
}

// #383: the rules are shown before anything is written, written only on a
// yes, and the settings' other keys survive.
func TestAgyPermissionsAsksBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	saved := agySettingsPath
	agySettingsPath = func() string { return path }
	t.Cleanup(func() { agySettingsPath = saved })
	if err := os.WriteFile(path, []byte(`{"trustedWorkspaces": ["/w"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runOrch(t, "", "agy-permissions", "--dry-run")
	if err != nil || !strings.Contains(out, "command(scripts/task-finish.sh)") || len(doctor.MissingAgyRules(path, doctor.AgyToolRules)) != 2 {
		t.Fatalf("--dry-run: err=%v\n%s", err, out)
	}
	if out, err = runOrch(t, "n\n", "agy-permissions"); err != nil || !strings.Contains(out, "Nothing written") || len(doctor.MissingAgyRules(path, doctor.AgyToolRules)) != 2 {
		t.Fatalf("answered no: err=%v\n%s", err, out)
	}
	if out, err = runOrch(t, "\n", "agy-permissions"); err != nil || len(doctor.MissingAgyRules(path, doctor.AgyToolRules)) != 0 {
		t.Fatalf("answered yes (default): err=%v\n%s", err, out)
	}
	raw, _ := os.ReadFile(path) // #nosec G304 -- a test temp file
	if !strings.Contains(string(raw), `"trustedWorkspaces"`) || strings.Index(string(raw), "trustedWorkspaces") > strings.Index(string(raw), "permissions") {
		t.Errorf("the file's own keys were lost or reordered:\n%s", raw)
	}
	if out, _ = runOrch(t, "", "agy-permissions"); !strings.Contains(out, "already allow") {
		t.Errorf("second run:\n%s", out)
	}
}

// init and start never write the user's agy settings without asking: with no
// way to ask they name the command.
func TestAgyPermissionsOfferWithoutAskNamesTheCommand(t *testing.T) {
	if _, err := os.Stat(agySettingsPath()); err == nil {
		t.Fatal("the test settings file exists before the test")
	}
	dir := t.TempDir()
	agy := filepath.Join(dir, "agy")
	if err := os.WriteFile(agy, []byte("#!/bin/sh\n"), 0o700); err != nil { // #nosec G306 -- a fake executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var out strings.Builder
	if err := offerAgyPermissionsIfInstalled(nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), doctor.AgyPermissionsCommand) {
		t.Errorf("out = %q, want the command named", out.String())
	}
	if _, err := os.Stat(agySettingsPath()); err == nil {
		t.Error("wrote agy's settings without asking")
	}
}

// --all-commands adds command(*) too, and says what it means first.
func TestAgyPermissionsAllCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	saved := agySettingsPath
	agySettingsPath = func() string { return path }
	t.Cleanup(func() { agySettingsPath = saved })

	out, err := runOrch(t, "", "agy-permissions", "--all-commands", "--yes")
	if err != nil || !strings.Contains(out, "every agy session") {
		t.Fatalf("err=%v\n%s", err, out)
	}
	if m := doctor.MissingAgyRules(path, []string{doctor.AgyAllCommandsRule}); len(m) != 0 {
		t.Errorf("command(*) not added:\n%s", out)
	}
	if c := doctor.CheckAgyPermissions(path, true); c.Status != doctor.StatusOK || !strings.Contains(c.Detail, "every command") {
		t.Errorf("doctor after --all-commands: %s %s", c.Status, c.Detail)
	}
}
