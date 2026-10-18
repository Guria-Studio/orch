package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckAgyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if c := CheckAgyPermissions(path, false); c.Status != StatusSkip {
		t.Errorf("no agy task: %s, want skip", c.Status)
	}
	if c := CheckAgyPermissions(path, true); c.Status != StatusWarn {
		t.Errorf("no settings file: %s, want warn", c.Status)
	}
	// The broad forms do not match in agy 1.4.2; only the bare command does.
	_ = os.WriteFile(path, []byte(`{"permissions":{"allow":["command(scripts/task-finish.sh *)","command(scripts/task-block.sh)"]}}`), 0o600)
	if m := MissingAgyRules(path, AgyToolRules); len(m) != 1 || m[0] != "command(scripts/task-finish.sh)" {
		t.Errorf("missing = %q", m)
	}
	_ = os.WriteFile(path, []byte(`{"permissions":{"allow":["command(scripts/task-finish.sh)","command(scripts/task-block.sh)"]}}`), 0o600)
	if c := CheckAgyPermissions(path, true); c.Status != StatusOK {
		t.Errorf("both rules: %s, want ok (%s)", c.Status, c.Detail)
	}
}
