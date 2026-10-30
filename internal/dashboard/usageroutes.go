package dashboard

import (
	"net/http"
	"time"

	"github.com/Guria-Studio/orch/internal/usage"
)

// /api/usage (#417): the project's tokens split by kind, model, task and
// attempt — `orch report usage --json`'s breakdown, from the same builder.
// Operator only: not on the stakeholder allow-list.

func (s *Server) usageRoutes() []route {
	return []route{{pattern: "GET /api/usage", name: "api_usage", handler: s.handleUsage}}
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if s.state == nil {
		s.failRead(w, "usage", errNoBackend)
		return
	}
	rows, err := s.state.AllSpend(r.Context(), time.Time{})
	if err != nil {
		s.failRead(w, "usage", err)
		return
	}
	writeJSON(w, http.StatusOK, usage.Build(rows))
}
