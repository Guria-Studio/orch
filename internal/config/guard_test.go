package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// guard (#484) blocks secrets and warns on debug leftovers unless told
// otherwise; a mode orch does not know is refused at load.
func TestGuardModes(t *testing.T) {
	cases := []struct {
		name, yaml, secrets, debug, wantErr string
	}{
		{"defaults", "", "block", "warn", ""},
		{"one key keeps the other's default", "guard:\n  debug: Off\n", "block", "off", ""},
		{"unknown mode", "guard:\n  secrets: maybe\n", "", "", "guard.secrets"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			write(t, path, c.yaml)
			res, err := Load(path, filepath.Dir(path))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("Load error = %v, want one naming %s", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if g := res.Config.Guard; g.Secrets != c.secrets || g.Debug != c.debug {
				t.Errorf("guard = %+v, want secrets %q, debug %q", g, c.secrets, c.debug)
			}
		})
	}
}
