package scaffold

import (
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
)

func TestInstalledTierDefaultsOnlyNameInstalledCLIs(t *testing.T) {
	only := func(bs ...model.Backend) func(model.Backend) bool {
		return func(b model.Backend) bool {
			for _, x := range bs {
				if x == b {
					return true
				}
			}
			return false
		}
	}
	for _, tc := range []struct {
		name      string
		installed func(model.Backend) bool
		want      map[string]string
	}{
		{"claude wins when present", only(model.BackendClaude, model.BackendCodex, model.BackendOpencode), map[string]string{
			"premium": "claude/claude-opus-4-7", "standard": "claude/claude-sonnet-4-6", "cheap": "claude/claude-haiku-4-5"}},
		{"codex alone has no cheap route", only(model.BackendCodex), map[string]string{
			"premium": "codex/gpt-5.6", "standard": "codex/gpt-5.4"}},
		{"nothing installed", only(), map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := InstalledTierDefaults(tc.installed)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
