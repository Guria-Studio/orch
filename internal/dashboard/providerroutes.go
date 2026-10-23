package dashboard

import (
	"net/http"
)

func (s *Server) providerRoutes() []route {
	return []route{
		{pattern: "POST /api/provider/{backend}/disable", name: "api_provider_disable", handler: s.handleProviderSwitch(true)},
		{pattern: "POST /api/provider/{backend}/enable", name: "api_provider_enable", handler: s.handleProviderSwitch(false)},
	}
}

// handleProviderSwitch is the Providers and quota page's switch: the same
// project.Steer.SetProvider as `orch provider disable|enable`, behind the
// gates every operator write has (local operator, same origin).
func (s *Server) handleProviderSwitch(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reason string `json:"reason"`
		}
		if !s.operatorWrite(w, r, s.steer != nil, &body) {
			return
		}
		backend := r.PathValue("backend")
		if err := s.steer.SetProvider(r.Context(), backend, disabled, body.Reason); err != nil {
			writeJSON(w, http.StatusBadRequest, errorPayload{Detail: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"backend": backend, "disabled": disabled})
	}
}
