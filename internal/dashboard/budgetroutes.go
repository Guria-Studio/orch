package dashboard

import (
	"errors"
	"net/http"

	"github.com/hectorcanaimero/orch/internal/budget"
	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/router"
)

// The Quota page's two additions: editing a provider's budget window
// (`orch budget set`'s function, budget.SetLimits) and the routing table
// from model_router.yaml.

func (s *Server) budgetRoutes() []route {
	return []route{
		{pattern: "POST /api/budget/{provider}", name: "api_budget_edit", handler: s.handleBudgetEdit},
		{pattern: "GET /api/router", name: "api_router", handler: s.handleRouter},
	}
}

// budgetEditPayload is the window as budgets.yaml now reads.
type budgetEditPayload struct {
	Provider     string  `json:"provider"`
	Preset       string  `json:"preset"`
	TokenBudget  int     `json:"token_budget"`
	WindowHours  float64 `json:"window_hours"`
	ThresholdPct float64 `json:"threshold_pct"`
}

// handleBudgetEdit keeps stop's and task edit's gates: only the local
// operator writes budgets.yaml. Limits refused are a 422 with the reason.
func (s *Server) handleBudgetEdit(w http.ResponseWriter, r *http.Request) {
	var l budget.Limits
	if !s.operatorWrite(w, r, true, &l) {
		return
	}
	loaded, err := config.Load(s.paths.ConfigYAML, s.paths.Root)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorPayload{Detail: "config.yaml: " + err.Error()})
		return
	}
	c := loaded.Config
	path := budget.ResolvePath(s.paths.Root, s.paths.ConfigYAML, c.BudgetsConfig)
	if path == "" {
		writeJSON(w, http.StatusConflict, errorPayload{Detail: "budgets_config is empty in config.yaml: the guardrail is off"})
		return
	}
	provider := r.PathValue("provider")
	pb, err := budget.SetLimits(path, c.BudgetsPreset, provider, l)
	var notFound *budget.PresetNotFoundError
	switch {
	case errors.Is(err, budget.ErrInvalidLimits):
		writeJSON(w, http.StatusUnprocessableEntity, errorPayload{Detail: err.Error()})
		return
	case errors.As(err, &notFound):
		writeJSON(w, http.StatusConflict, errorPayload{Detail: err.Error()})
		return
	case err != nil:
		writeJSON(w, http.StatusConflict, errorPayload{Detail: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, budgetEditPayload{
		Provider: provider, Preset: c.BudgetsPreset,
		TokenBudget: pb.TokenBudget, WindowHours: pb.WindowHours, ThresholdPct: pb.ThresholdPct,
	})
}

// routeRow is one model_router.yaml route: the `model` a task names, where
// it runs, and how many of that backend's tasks run at once.
type routeRow struct {
	Key         string `json:"key"`
	Backend     string `json:"backend"`
	CLIModel    string `json:"cli_model"`
	Tier        string `json:"tier"`
	MaxParallel int    `json:"max_parallel"`
}

type routerPayload struct {
	Path      string     `json:"path"`
	GlobalMax int        `json:"global_max"`
	Routes    []routeRow `json:"routes"`
	Error     string     `json:"error,omitempty"`
}

// handleRouter lists the routes, sorted by key, each with its backend's
// concurrency.per_provider (global_max when the backend has none).
// Operator-only. A router or config that does not load is reported in the
// payload: the page shows why the table is empty.
func (s *Server) handleRouter(w http.ResponseWriter, _ *http.Request) {
	out := routerPayload{Path: s.paths.RouterYAML(), Routes: []routeRow{}}
	cfg := config.Defaults()
	if loaded, err := config.Load(s.paths.ConfigYAML, s.paths.Root); err == nil {
		cfg = loaded.Config
	} else {
		out.Error = "config.yaml: " + err.Error()
	}
	out.GlobalMax = cfg.Concurrency.GlobalMax
	rtr, err := router.Load(out.Path)
	if err != nil {
		out.Error = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	for _, key := range rtr.Keys() {
		e := rtr[key]
		max := out.GlobalMax
		if n, ok := cfg.Concurrency.PerProvider[string(e.Backend)]; ok && n > 0 {
			max = n
		}
		out.Routes = append(out.Routes, routeRow{Key: key, Backend: string(e.Backend), CLIModel: e.CLIModel, Tier: string(e.Tier), MaxParallel: max})
	}
	writeJSON(w, http.StatusOK, out)
}
