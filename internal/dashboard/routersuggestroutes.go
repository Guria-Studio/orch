package dashboard

import (
	"net/http"
	"time"

	"github.com/Guria-Studio/orch/internal/estimate"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/router"
)

// /api/router/suggest is `orch router suggest` for the Usage page (#457):
// per route key, how each route its tasks ran on did, and the change the
// numbers support. Read only — the page shows the command that applies it.
// Operator only, like the rest of Usage.

type routerSuggestResponse struct {
	Period    string                `json:"period"`
	MinSample int                   `json:"min_sample"`
	Kinds     []estimate.KindRoutes `json:"kinds"`
}

func (s *Server) routerSuggestRoutes() []route {
	return []route{{pattern: "GET /api/router/suggest", name: "api_router_suggest", handler: s.handleRouterSuggest}}
}

func (s *Server) handleRouterSuggest(w http.ResponseWriter, r *http.Request) {
	period, since, ok := usagePeriod(r.URL.Query().Get("period"), time.Now())
	if !ok {
		writeJSON(w, http.StatusBadRequest, errorPayload{Detail: "period: one of today, 24h, 7d, 30d, all"})
		return
	}
	if s.state == nil {
		s.failRead(w, "router suggest", errNoBackend)
		return
	}
	ctx := r.Context()
	spend, err := s.state.AllSpend(ctx, since)
	if err != nil {
		s.failRead(w, "router suggest", err)
		return
	}
	events, err := s.state.AllEvents(ctx, 0)
	if err != nil {
		s.failRead(w, "router suggest", err)
		return
	}
	// The route keys come from tasks.json; status is not read.
	var tasks []model.Task
	if tf, tErr := model.LoadTasksFile(s.paths.TasksJSON()); tErr == nil {
		tasks = tf.Tasks
	}
	rtr, rErr := router.Load(s.paths.RouterYAML())
	if rErr != nil {
		rtr = router.Router{}
	}
	writeJSON(w, http.StatusOK, routerSuggestResponse{Period: period, MinSample: estimate.MinSample,
		Kinds: estimate.SuggestRoutes(estimate.SuggestInput{Tasks: tasks, Routes: rtr,
			Spend: spend, Events: events, Prices: s.pricing()})})
}
