package scaffold

import (
	"os/exec"
	"slices"
	"sort"

	"gopkg.in/yaml.v3"
)

// BackendPreference is every backend orch dispatches to, in the order a
// default model is taken from when several of their CLIs are installed.
var BackendPreference = []string{"claude", "codex", "gemini", "opencode", "agy"}

// lookPath is exec.LookPath; a variable so a test decides what is installed.
var lookPath = exec.LookPath

// InstalledBackends lists the backends whose CLI is on PATH, in
// BackendPreference order.
func InstalledBackends() []string {
	var out []string
	for _, b := range BackendPreference {
		if _, err := lookPath(b); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// TierDefaults picks a default model per tier among the packaged routes whose
// backend is installed: the most preferred installed backend that has the
// tier, then the first key of it. A tier no installed CLI covers is left out.
//
// #376: the wizard used to offer the first key alphabetically — `agy/pro` and
// `agy/flash` — whether agy was installed or not.
func TierDefaults(installed []string) map[string]string {
	routes := packagedRoutes()
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]string{}
	for _, tier := range []string{"premium", "standard", "cheap"} {
		for _, b := range BackendPreference {
			if !slices.Contains(installed, b) {
				continue
			}
			if k := firstKey(keys, routes, b, tier); k != "" {
				out[tier] = k
				break
			}
		}
	}
	return out
}

func firstKey(keys []string, routes map[string]packagedRoute, backend, tier string) string {
	for _, k := range keys {
		if routes[k].Backend == backend && routes[k].Tier == tier {
			return k
		}
	}
	return ""
}

// PackagedBackend is the backend orch's own router sends key to, or "".
func PackagedBackend(key string) string {
	return packagedRoutes()[key].Backend
}

type packagedRoute struct {
	Backend  string `yaml:"backend"`
	CLIModel string `yaml:"cli_model"`
	Tier     string `yaml:"tier"`
}

// packagedRoutes is the router orch ships, by key. Empty if the packaged copy
// does not parse — the picker and the defaults then offer nothing rather than
// a guess.
func packagedRoutes() map[string]packagedRoute {
	raw, err := packagedDefault("model_router.yaml")
	if err != nil {
		return nil
	}
	var entries map[string]packagedRoute
	if err := yaml.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	return entries
}
