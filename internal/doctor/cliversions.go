package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
	"github.com/Guria-Studio/orch/internal/update"
)

// LatestFunc is an npm package's latest version (update.LatestCLI behind the
// CLI, a fake in tests). An error or "" is "unknown", never a failure.
type LatestFunc func(ctx context.Context, pkg string) (string, error)

// CLIVersion is how one agent CLI stands against its latest release (#448).
type CLIVersion struct {
	Installed string
	// Latest is "" when orch has no release feed for the CLI or could not
	// read it.
	Latest string
	Behind bool
	// Update is the command that updates the CLI as it was installed, or
	// every known one, newline-separated, when the install is not
	// recognized.
	Update string
}

// CompareCLI places name, installed at path and reporting version (its
// `--version` line), against its latest release.
func CompareCLI(ctx context.Context, name, path, version string, latest LatestFunc) CLIVersion {
	v := CLIVersion{Installed: version}
	if s, ok := update.LooseVersion(version); ok {
		v.Installed = s
	}
	p, err := providers.Get(model.Backend(name))
	if err != nil {
		return v
	}
	d, ok := p.(providers.Distributed)
	if !ok || latest == nil {
		return v
	}
	dist := d.Distribution()
	if l, err := latest(ctx, dist.NPM); err == nil {
		v.Latest = l
	}
	v.Behind = update.CLIBehind(v.Installed, v.Latest)
	v.Update = UpdateCommand(dist, path)
	return v
}

// UpdateCommand is the command that updates a CLI installed at path: npm's
// when the binary resolves into a node_modules, Homebrew's under a Homebrew
// prefix, the CLI's own for its install script's copy, else all of them.
func UpdateCommand(d providers.Distribution, path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	npm := "npm install -g " + d.NPM + "@latest"
	brew := ""
	if d.Brew != "" {
		brew = "brew upgrade " + d.Brew
	}
	switch {
	case strings.Contains(path, "/node_modules/"):
		return npm
	case brew != "" && (strings.Contains(path, "/Cellar/") || strings.Contains(path, "/Caskroom/") ||
		strings.Contains(path, "/homebrew/") || strings.Contains(path, "/linuxbrew/")):
		return brew
	case d.Self != "" && d.SelfPath != "" && strings.Contains(path, d.SelfPath):
		return d.Self
	}
	var all []string
	for _, c := range []string{npm, brew, d.Self} {
		if c != "" {
			all = append(all, c)
		}
	}
	return strings.Join(all, "\n")
}

// CheckCLIVersions is one `backend.<name>.version` check per installed
// backend: warn when a newer release is out, with the command to update; ok
// when it is the latest; skip when the latest is unknown. latest nil means
// the check is off (update.check_clis: false).
func CheckCLIVersions(ctx context.Context, backends []string, latest LatestFunc) []Check {
	out := make([]Check, len(backends))
	var wg sync.WaitGroup
	for i, name := range backends {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = checkCLIVersion(ctx, name, latest)
		}()
	}
	wg.Wait()
	return out
}

func checkCLIVersion(ctx context.Context, name string, latest LatestFunc) Check {
	c := Check{Name: "backend." + name + ".version", Status: StatusSkip}
	path, err := exec.LookPath(name)
	switch {
	case err != nil:
		c.Detail = name + " not on PATH"
	case latest == nil:
		c.Detail = "CLI update check off (update.check_clis: false)"
	default:
		_, version := ProbeVersion(name)
		v := CompareCLI(ctx, name, path, version, latest)
		switch {
		case v.Latest == "":
			c.Detail = name + " " + v.Installed + ": latest version unknown"
		case v.Behind:
			c.Status = StatusWarn
			c.Detail = CLIUpdateLine(name, v)
			c.Remediation = "Run: " + strings.ReplaceAll(v.Update, "\n", "  or  ") + " — then `orch router refresh` for its new models."
		default:
			c.Status = StatusOK
			c.Detail = name + " " + v.Installed + " is the latest"
		}
	}
	return c
}

// CLIUpdateLine is the one line that says a CLI is behind:
// "claude 2.1.290 → 2.1.301 available: `npm install -g …`".
func CLIUpdateLine(name string, v CLIVersion) string {
	return fmt.Sprintf("%s %s → %s available: `%s`", name, v.Installed, v.Latest, strings.ReplaceAll(v.Update, "\n", "` or `"))
}
