package dashboard

import (
	"net/http"
	"time"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/estimate"
	"github.com/hectorcanaimero/orch/internal/router"
)

// /api/estimate is `orch estimate` for the Now page (#357): what the next
// `orch run` would dispatch, cost and take. Operator only — it is not on the
// stakeholder allow-list.

func (s *Server) estimateRoutes() []route {
	return []route{{pattern: "GET /api/estimate", name: "api_estimate", handler: s.handleEstimate}}
}

func (s *Server) handleEstimate(w http.ResponseWriter, r *http.Request) {
	if s.state == nil {
		s.failRead(w, "estimate", errNoBackend)
		return
	}
	ctx := r.Context()
	view, err := s.loadView(ctx)
	if err != nil {
		s.failRead(w, "estimate", err)
		return
	}
	spend, err := s.state.AllSpend(ctx, time.Time{})
	if err != nil {
		s.failRead(w, "estimate", err)
		return
	}
	// The same fallbacks `orch run` takes: defaults for a config.yaml that
	// does not load, no routes for a router that does not.
	cfg := config.Defaults()
	if loaded, lErr := config.Load(s.paths.ConfigYAML, s.paths.Root); lErr == nil {
		cfg = loaded.Config
	}
	rtr, rErr := router.Load(s.paths.RouterYAML())
	if rErr != nil {
		rtr = router.Router{}
	}
	writeJSON(w, http.StatusOK, estimate.Compute(estimate.Input{
		Tasks: view.Tasks, Routes: rtr, Spend: spend, Prices: s.pricing(),
		Slots: cfg.Concurrency.GlobalMax, TypicalTokens: cfg.TypicalDispatchToken,
	}))
}
