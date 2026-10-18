package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/router"
)

// InstalledTierDefaults picks each tier's default model among the packaged
// routes whose CLI installed accepts — router.OnPath for a real machine — so
// a new project never defaults to a CLI that is not there (#376, #377). A
// tier no installed backend serves is left out.
func InstalledTierDefaults(installed func(model.Backend) bool) map[string]string {
	rtr, err := PackagedRouter()
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, tier := range []model.Tier{model.TierPremium, model.TierStandard, model.TierCheap} {
		if k := rtr.ForTier(tier, installed); k != "" {
			out[string(tier)] = k
		}
	}
	return out
}

// PackagedRouter is the router every scaffold starts from, parsed.
func PackagedRouter() (router.Router, error) {
	raw, err := packagedDefault("model_router.yaml")
	if err != nil {
		return nil, err
	}
	return router.Parse("model_router.yaml", raw)
}

// SeedInstalledRoutes appends to root's model_router.yaml the packaged routes
// whose CLI installed accepts and the file does not have yet, and returns
// their keys. A blank project's router is empty; a planner choosing models
// for it then has real routes, with their tiers, to choose from (#377).
func SeedInstalledRoutes(root string, installed func(model.Backend) bool) ([]string, error) {
	packaged, err := PackagedRouter()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, ".orchestrator", "model_router.yaml")
	have, err := router.Load(path)
	if err != nil {
		return nil, err
	}
	add := map[string]model.RouteEntry{}
	var keys []string
	for _, k := range packaged.Keys() {
		if e := packaged[k]; installed(e.Backend) {
			if _, ok := have[k]; !ok {
				add[k] = e
				keys = append(keys, k)
			}
		}
	}
	if len(add) == 0 {
		return nil, nil
	}
	body, err := yaml.Marshal(add)
	if err != nil {
		return nil, fmt.Errorf("encoding the routes: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- the project's own router
	if err != nil {
		return nil, err
	}
	_, werr := fmt.Fprintf(f, "\n# ---- the packaged routes of the CLIs on PATH, added by `orch start` ----\n%s", body)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return nil, fmt.Errorf("writing %s: %w", path, werr)
	}
	return keys, nil
}

var (
	reWorktreeMode = regexp.MustCompile(`(?m)^(\s+worktree_mode:[ \t]*)\S+`)
	reAutoPR       = regexp.MustCompile(`(?m)^(\s+auto_pr:[ \t]*)\S+`)
)

// RunLocally turns off dispatch.worktree_mode and vcs.auto_pr in root's
// config.yaml, for a repository with no remote: a branch per task needs a
// remote to reach the base through a pull request, and without one every
// finished task would end blocked as "not pushed" (#377).
func RunLocally(root string) error {
	path := filepath.Join(root, ".orchestrator", "config.yaml")
	raw, err := os.ReadFile(path) // #nosec G304 -- the project's own config
	if err != nil {
		return err
	}
	body := reWorktreeMode.ReplaceAllString(string(raw), "${1}false")
	body = reAutoPR.ReplaceAllString(body, "${1}false")
	return os.WriteFile(path, []byte(body), 0o600) // #nosec G703 -- the project's own config, a fixed path under root
}
