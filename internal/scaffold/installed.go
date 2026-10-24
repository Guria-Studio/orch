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
// tier no installed backend serves is left out. A route whose CLI listed its
// models without the route's (lists, DiscoverModels) is not a candidate (#397).
func InstalledTierDefaults(installed func(model.Backend) bool, lists ModelLists) (map[string]string, error) {
	rtr, err := PackagedRouter()
	if err != nil {
		return nil, err
	}
	rtr, _ = lists.Available(rtr)
	out := map[string]string{}
	for _, tier := range []model.Tier{model.TierPremium, model.TierStandard, model.TierCheap} {
		if k := rtr.ForTier(tier, installed); k != "" {
			out[string(tier)] = k
		}
	}
	return out, nil
}

// PackagedRouter is the router every scaffold starts from, parsed.
func PackagedRouter() (router.Router, error) {
	raw, err := packagedDefault("model_router.yaml")
	if err != nil {
		return nil, fmt.Errorf("reading the packaged model_router.yaml: %w", err)
	}
	rtr, err := router.Parse("model_router.yaml", raw)
	if err != nil {
		return nil, fmt.Errorf("parsing the packaged model_router.yaml: %w", err)
	}
	return rtr, nil
}

// SeedInstalledRoutes appends to root's model_router.yaml the packaged routes
// whose CLI installed accepts and the file does not have yet, and returns
// their keys. A blank project's router is empty; a planner choosing models
// for it then has real routes, with their tiers, to choose from (#377). Routes
// lists says their CLI no longer runs are left out (#397).
func SeedInstalledRoutes(root string, installed func(model.Backend) bool, lists ModelLists) ([]string, error) {
	plan, err := PlanRefresh(root, installed, lists)
	if err != nil {
		return nil, err
	}
	return plan.Keys, AppendRoutes(root, plan, "orch start")
}

// RefreshPlan is what the packaged routes and the CLIs' model lists would
// change in a project's model_router.yaml (`orch router refresh`, #448).
type RefreshPlan struct {
	// Add are the packaged routes of installed CLIs the file lacks and,
	// where the CLI lists its models, that it still runs; Keys sorted.
	Add  map[string]model.RouteEntry
	Keys []string
	// Stale are the file's routes whose CLI listed its models without the
	// route's cli_model: a dispatch to one fails as version_drift.
	Stale []string
	// Unrouted are, per CLI, models it lists that no route runs: new models
	// orch has no tier for, for the operator to route by hand.
	Unrouted map[model.Backend][]string
}

// PlanRefresh reads root's model_router.yaml against the packaged routes and
// lists (DiscoverModels); it writes nothing.
func PlanRefresh(root string, installed func(model.Backend) bool, lists ModelLists) (RefreshPlan, error) {
	packaged, err := PackagedRouter()
	if err != nil {
		return RefreshPlan{}, err
	}
	packaged, _ = lists.Available(packaged)
	have, err := router.Load(filepath.Join(root, ".orchestrator", "model_router.yaml"))
	if err != nil {
		return RefreshPlan{}, fmt.Errorf("reading the project's router: %w", err)
	}
	plan := RefreshPlan{Add: map[string]model.RouteEntry{}}
	for _, k := range packaged.Keys() {
		if e := packaged[k]; installed(e.Backend) {
			if _, ok := have[k]; !ok {
				plan.Add[k] = e
				plan.Keys = append(plan.Keys, k)
			}
		}
	}
	_, plan.Stale = lists.Available(have)
	all := router.Router{}
	for k, e := range have {
		all[k] = e
	}
	for k, e := range plan.Add {
		all[k] = e
	}
	plan.Unrouted = lists.Unrouted(all)
	return plan, nil
}

// AppendRoutes appends plan's additions to root's model_router.yaml under a
// comment naming the command that added them.
func AppendRoutes(root string, plan RefreshPlan, by string) error {
	if len(plan.Add) == 0 {
		return nil
	}
	path := filepath.Join(root, ".orchestrator", "model_router.yaml")
	body, err := yaml.Marshal(plan.Add)
	if err != nil {
		return fmt.Errorf("encoding the routes: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- the project's own router
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	_, werr := fmt.Fprintf(f, "\n# ---- the packaged routes of the CLIs on PATH, added by `%s` ----\n%s", by, body)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("writing %s: %w", path, werr)
	}
	return nil
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
		return fmt.Errorf("reading %s: %w", path, err)
	}
	body := reWorktreeMode.ReplaceAllString(string(raw), "${1}false")
	body = reAutoPR.ReplaceAllString(body, "${1}false")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil { // #nosec G703 -- the project's own config, a fixed path under root
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
