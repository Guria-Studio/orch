package tunnel

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestURLPatternTakesTheQuickTunnelNotTheControlEndpoint(t *testing.T) {
	for line, want := range map[string]string{
		"2026-09-16T10:00:00Z INF |  https://seasonal-deck-organisms-sf.trycloudflare.com  |": "https://seasonal-deck-organisms-sf.trycloudflare.com",
		`failed to request quick Tunnel: Post "https://api.trycloudflare.com/tunnel"`:         "",
		"https://example.com": "",
	} {
		if got := urlPattern.FindString(line); got != want {
			t.Errorf("urlPattern on %q = %q, want %q", line, got, want)
		}
	}
}

func TestRecommendedGuide(t *testing.T) {
	for _, tc := range []struct {
		name, goos, osRelease string
		brew                  bool
		want                  string
	}{
		{"mac with brew", "darwin", "", true, GuideBrew},
		{"mac without brew", "darwin", "", false, GuideMacBinary},
		{"ubuntu", "linux", "NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\n", false, GuideApt},
		{"mint through ID_LIKE", "linux", "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", false, GuideApt},
		{"fedora", "linux", "ID=fedora\n", false, GuideDnf},
		{"rocky through ID_LIKE", "linux", "ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", false, GuideDnf},
		{"arch", "linux", "ID=arch\n", true, GuideLinuxBinary},
		{"no os-release", "linux", "", false, GuideLinuxBinary},
		{"windows", "windows", "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RecommendedGuide(tc.goos, tc.osRelease, tc.brew); got != tc.want {
				t.Errorf("RecommendedGuide = %q, want %q", got, tc.want)
			}
		})
	}
}

// Every recommendation must name a guide that exists, and every guide must
// end in something that proves the install worked.
func TestEveryRecommendedGuideExists(t *testing.T) {
	ids := map[string]bool{}
	for _, g := range InstallGuides("arm64") {
		ids[g.ID] = true
		if len(g.Steps) == 0 || g.Steps[len(g.Steps)-1].Command != "cloudflared --version" {
			t.Errorf("guide %s does not end by checking the binary", g.ID)
		}
	}
	for _, id := range []string{GuideBrew, GuideApt, GuideDnf, GuideLinuxBinary, GuideMacBinary} {
		if !ids[id] {
			t.Errorf("no guide for %s", id)
		}
	}
}

func TestFindConfigReportsTheFileThatBlocksQuickTunnels(t *testing.T) {
	empty, withConfig := t.TempDir(), t.TempDir()
	if got := findConfig([]string{empty}); got != "" {
		t.Errorf("findConfig on an empty dir = %q", got)
	}
	path := filepath.Join(withConfig, "config.yaml")
	if err := os.WriteFile(path, []byte("tunnel: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findConfig([]string{empty, withConfig}); got != path {
		t.Errorf("findConfig = %q, want %q", got, path)
	}
}

func TestTailscaleRunsInTheForegroundAgainstTheDashboard(t *testing.T) {
	for provider, verb := range map[string]string{ProviderTailscaleServe: "serve", ProviderTailscaleFunnel: "funnel"} {
		argv, err := commandLine(ManagerConfig{Provider: provider, Port: 7420})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"tailscale", verb, "http://127.0.0.1:7420"}; !slices.Equal(argv, want) {
			t.Errorf("%s argv = %q, want %q", provider, argv, want)
		}
	}
	if _, err := commandLine(ManagerConfig{Provider: "ngrok", Port: 7420}); err == nil {
		t.Error("an unknown provider got an argv")
	}
}

func TestTailscaleURLPatternTakesTheNodeNotTheLoginLink(t *testing.T) {
	out := "Funnel is not enabled on your tailnet.\nTo enable, visit:\n\n         https://login.tailscale.com/f/funnel?node=abc\n"
	if got := tailscaleURLPattern.FindString(out); got != "" {
		t.Errorf("matched %q in the enable-Funnel prompt", got)
	}
	if got := tailscaleURLPattern.FindString("https://box.tail1234.ts.net/"); got != "https://box.tail1234.ts.net" {
		t.Errorf("got %q", got)
	}
}
