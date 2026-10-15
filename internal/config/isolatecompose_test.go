package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// dispatch.isolate_compose is on by default (#343): two tasks in parallel
// worktrees must not share one compose project unless the project says so.
func TestIsolateComposeKey(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		on   bool
	}{
		{"default", "", true},
		{"turned off", "dispatch:\n  isolate_compose: false\n", false},
		{"explicitly on", "dispatch:\n  isolate_compose: true\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			write(t, path, c.yaml)
			res, err := Load(path, filepath.Dir(path))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := res.Config.Dispatch.IsolateCompose; got != c.on {
				t.Errorf("isolate_compose = %v, want %v", got, c.on)
			}
			for _, w := range res.Warnings {
				if strings.Contains(w, "isolate_compose") {
					t.Errorf("isolate_compose warned as unknown: %s", w)
				}
			}
		})
	}
}
