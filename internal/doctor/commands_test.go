package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
)

func TestUnresolvedCommands(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "setup.sh"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		cfg  config.Config
		want []string // Detail of each unresolved command
	}{
		"worktree_setup is checked with verify off": {
			cfg:  config.Config{Dispatch: config.Dispatch{WorktreeSetup: "tue"}},
			want: []string{`dispatch.worktree_setup: "tue" not found on PATH`},
		},
		"a worktree_setup script missing from the repo": {
			cfg:  config.Config{Dispatch: config.Dispatch{WorktreeSetup: "./scripts/missing.sh"}},
			want: []string{`dispatch.worktree_setup: "./scripts/missing.sh" not found in the repository`},
		},
		"verify commands are skipped while verify is off": {
			cfg: config.Config{Verify: config.Verify{Commands: []string{"tue"}}},
		},
		"builtins, assignments and repo scripts resolve": {
			cfg: config.Config{
				Dispatch: config.Dispatch{WorktreeSetup: "./scripts/setup.sh"},
				Verify:   config.Verify{Enabled: true, Commands: []string{"true", "cd web && tue", "CI=1 true", "$TOOL test"}},
			},
		},
		"verify.app.start on a typo": {
			cfg:  config.Config{Verify: config.Verify{Enabled: true, App: config.VerifyApp{Start: "tue"}}},
			want: []string{`verify.app.start: "tue" not found on PATH`},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := UnresolvedCommands(root, tt.cfg)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %q", got, tt.want)
			}
			for i, w := range tt.want {
				if got[i].Detail != w {
					t.Errorf("[%d] Detail = %q, want %q", i, got[i].Detail, w)
				}
			}
		})
	}
}

func TestCheckConfiguredCommandsIsOneOKWhenEverythingResolves(t *testing.T) {
	got := CheckConfiguredCommands(t.TempDir(), config.Config{Dispatch: config.Dispatch{WorktreeSetup: "true"}})
	if len(got) != 1 || got[0].Status != StatusOK || got[0].Name != "config.commands" {
		t.Fatalf("got %+v, want one ok config.commands check", got)
	}
}
