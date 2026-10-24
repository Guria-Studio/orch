package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/providers"
)

func latestIs(version string, err error) LatestFunc {
	return func(context.Context, string) (string, error) { return version, err }
}

// #448's acceptance: newer → warn with the update command; equal → ok;
// registry unreachable → no warning and no failure.
func TestCheckCLIVersions(t *testing.T) {
	withFakeBin(t)
	t.Setenv("FAKE_CLAUDE_VERSION_STDOUT", "2.1.290 (Claude Code)")
	for _, c := range []struct {
		name   string
		latest LatestFunc
		status Status
		detail string
	}{
		{"newer", latestIs("2.1.301", nil), StatusWarn, "claude 2.1.290 → 2.1.301 available: `"},
		{"equal", latestIs("2.1.290", nil), StatusOK, "claude 2.1.290 is the latest"},
		{"unreachable", latestIs("", errors.New("offline")), StatusSkip, "latest version unknown"},
		{"off", nil, StatusSkip, "update.check_clis: false"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := CheckCLIVersions(context.Background(), []string{"claude"}, c.latest)
			if len(got) != 1 || got[0].Name != "backend.claude.version" || got[0].Status != c.status || !strings.Contains(got[0].Detail, c.detail) {
				t.Fatalf("got %+v, want %s containing %q", got, c.status, c.detail)
			}
			if c.status == StatusWarn && !strings.Contains(got[0].Remediation, "@anthropic-ai/claude-code") {
				t.Errorf("remediation %q names no update command", got[0].Remediation)
			}
		})
	}
}

// agy has no release feed: unknown, never a warning or an error.
func TestCheckCLIVersionsWithoutAFeed(t *testing.T) {
	withFakeBin(t)
	got := CheckCLIVersions(context.Background(), []string{"agy"}, latestIs("99.0.0", nil))
	if got[0].Status != StatusSkip {
		t.Errorf("agy: %+v, want skip", got[0])
	}
}

func TestUpdateCommandFollowsTheInstall(t *testing.T) {
	d := providers.ClaudeProvider{}.Distribution()
	for path, want := range map[string]string{
		"/home/u/.nvm/versions/node/v24/lib/node_modules/@anthropic-ai/claude-code/cli.js": "npm install -g @anthropic-ai/claude-code@latest",
		"/opt/homebrew/Caskroom/claude-code/2.1.0/claude":                                  "brew upgrade --cask claude-code",
		"/home/u/.local/share/claude/versions/2.1.310":                                     "claude update",
		"": "npm install -g @anthropic-ai/claude-code@latest\nbrew upgrade --cask claude-code\nclaude update",
	} {
		if got := UpdateCommand(d, path); got != want {
			t.Errorf("UpdateCommand(%q) = %q, want %q", path, got, want)
		}
	}
}
