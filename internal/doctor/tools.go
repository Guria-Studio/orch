package doctor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hectorcanaimero/orch/internal/ci"
	"github.com/hectorcanaimero/orch/internal/tunnel"
)

// Tool groups, stable on the wire: the dashboard's Tools page lists them in
// this order.
const (
	ToolGroupAgents  = "agents"
	ToolGroupRepo    = "repo"
	ToolGroupShare   = "share"
	ToolGroupQuality = "quality"
)

// Tool is one program orch runs on this machine: whether this project needs
// it, whether it is here, and how to get it. orch never installs anything;
// Install is for the person at the keyboard to copy.
type Tool struct {
	ID      string `json:"id"`
	Group   string `json:"group"`
	Used    bool   `json:"used"`
	Found   bool   `json:"found"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	// Install is one or more shell lines, newline-separated; "" when orch
	// knows no command for it (DocsURL then is the answer, if any).
	Install string `json:"install,omitempty"`
	DocsURL string `json:"docs_url,omitempty"`
	// Latest is an agent CLI's latest release, when orch could read it, and
	// Update the command that installs it when Version is older (#448).
	Latest string `json:"latest,omitempty"`
	Update string `json:"update,omitempty"`
}

// ToolsConfig is what decides which tools a project uses.
type ToolsConfig struct {
	Root string
	// Backends are the agent CLIs the project's tasks route to
	// (ReferencedBackends).
	Backends []string
	// VCS is the pull-request setup; its CLI (gh or glab) is used when
	// worktree mode or auto PRs are on, as CheckVCSReadiness decides.
	VCS VCSConfig
	// Tunnel is tunnel.enabled: the share programs are used only then.
	Tunnel bool
	// E2E is whether verify runs end-to-end commands (verify.e2e), which
	// orch's E2E reports read from Playwright.
	E2E bool
	// Latest reads an agent CLI's latest release; nil leaves Tool.Latest
	// empty (update.check_clis: false).
	Latest LatestFunc
}

// agentCLIs are the backends orch dispatches to, in the order the page lists them.
var agentCLIs = []string{"claude", "codex", "opencode", "gemini", "agy", "muse"}

// lookVersion finds command on PATH and asks its version; a missing binary
// costs no probe.
var lookVersion = func(command string) (found bool, path, version string) {
	path, err := exec.LookPath(command)
	if err != nil {
		return false, "", ""
	}
	_, version = ProbeVersion(command)
	return true, path, version
}

// Tools describes every program orch can run, probing them in parallel: a
// node CLI takes most of a second to print its version.
func Tools(cfg ToolsConfig) []Tool {
	used := func(id string) bool {
		for _, b := range cfg.Backends {
			if b == id {
				return true
			}
		}
		return false
	}
	pr := (cfg.VCS.WorktreeMode || cfg.VCS.AutoPR) && cfg.VCS.Provider != "local"
	host := tunnel.DetectHost()

	tools := make([]Tool, 0, 11)
	for _, id := range agentCLIs {
		t := Tool{ID: id, Group: ToolGroupAgents, Used: used(id)}
		for _, p := range ci.ReviewProviders {
			if p.Backend == id {
				t.Install = "npm install -g " + p.Package
			}
		}
		tools = append(tools, t)
	}
	tools = append(tools,
		Tool{ID: "git", Group: ToolGroupRepo, Used: true, DocsURL: "https://git-scm.com/downloads"},
		Tool{ID: "gh", Group: ToolGroupRepo, Used: pr && cfg.VCS.cli() == "gh", Install: vcsCLIInstallHint("gh")},
		Tool{ID: "glab", Group: ToolGroupRepo, Used: pr && cfg.VCS.cli() == "glab", Install: vcsCLIInstallHint("glab")},
		Tool{ID: tunnel.Command, Group: ToolGroupShare, Used: cfg.Tunnel,
			Install: guideCommands(tunnel.InstallGuides(host.Arch), host.RecommendedGuide), DocsURL: tunnel.DocsURL},
		Tool{ID: tunnel.TailscaleCommand, Group: ToolGroupShare, Used: cfg.Tunnel,
			Install: guideCommands(tunnel.TailscaleInstallGuides(), tailscaleGuide(host.OS)), DocsURL: tunnel.TailscaleDocsURL},
		Tool{ID: "playwright", Group: ToolGroupQuality, Used: cfg.E2E,
			Install: "npm install -D @playwright/test\nnpx playwright install --with-deps chromium", DocsURL: "https://playwright.dev/docs/intro"},
	)

	var wg sync.WaitGroup
	for i := range tools {
		wg.Add(1)
		go func(t *Tool) {
			defer wg.Done()
			if t.ID == "playwright" {
				t.Found, t.Path, t.Version = findPlaywright(cfg.Root)
				return
			}
			t.Found, t.Path, t.Version = lookVersion(t.ID)
			if t.Group == ToolGroupAgents && t.Found && cfg.Latest != nil {
				v := CompareCLI(context.Background(), t.ID, t.Path, t.Version, cfg.Latest)
				t.Latest = v.Latest
				if v.Behind {
					t.Update = v.Update
				}
			}
		}(&tools[i])
	}
	wg.Wait()
	return tools
}

// guideCommands is the chosen guide's commands, one block; "" without one.
func guideCommands(guides []tunnel.InstallGuide, id string) string {
	for _, g := range guides {
		if g.ID != id {
			continue
		}
		lines := make([]string, 0, len(g.Steps))
		for _, s := range g.Steps {
			lines = append(lines, s.Command)
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

func tailscaleGuide(goos string) string {
	switch goos {
	case "linux":
		return tunnel.GuideTailscaleLinux
	case "darwin":
		return tunnel.GuideTailscaleBrew
	}
	return ""
}

// findPlaywright looks where E2E commands find it: the project's own
// node_modules first (npx's answer), then PATH. The version comes from the
// package's manifest, which is cheaper than starting node.
func findPlaywright(root string) (found bool, path, version string) {
	if root != "" {
		bin := filepath.Join(root, "node_modules", ".bin", "playwright")
		if _, err := os.Stat(bin); err == nil {
			for _, pkg := range []string{"@playwright/test", "playwright"} {
				if v := packageVersion(filepath.Join(root, "node_modules", pkg, "package.json")); v != "" {
					return true, bin, v
				}
			}
			return true, bin, ""
		}
	}
	return lookVersion("playwright")
}

func packageVersion(manifest string) string {
	data, err := os.ReadFile(manifest) // #nosec G304 -- a package.json under the project's own node_modules
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	return pkg.Version
}
