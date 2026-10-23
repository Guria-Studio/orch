package dashboard

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/hectorcanaimero/orch/internal/providers"
)

func (s *Server) quotaRoutes() []route {
	return []route{
		{pattern: "GET /api/quota", name: "api_quota", handler: s.handleQuota},
	}
}

// providerQuota is the latest subscription usage one backend reported.
type providerQuota struct {
	Backend string                  `json:"backend"`
	Windows []providers.QuotaWindow `json:"windows"`
	// SeenAt is the outcome event that carried it: the report is as fresh as
	// the last dispatch to that backend, no fresher.
	SeenAt string `json:"seen_at"`
}

type quotaPayload struct {
	Providers []providerQuota `json:"providers"`
}

// handleQuota answers what each provider last said about its own limits —
// today claude's 5-hour and weekly windows, from the rate_limit_event of its
// latest dispatch (engine.outcomeEvent keeps it as extra.quota). These are
// the subscription's real windows, which see usage outside orch too; the
// budget window (/api/budget/summary) is orch's own count. Operator-only.
//
// ponytail: scans every event for the newest quota per backend; an index
// or a dedicated row if the event log grows large enough to notice.
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	if s.state == nil {
		s.failRead(w, "quota", errNoBackend)
		return
	}
	events, err := s.state.AllEvents(r.Context(), 0)
	if err != nil {
		s.failRead(w, "quota", err)
		return
	}
	latest := map[string]providerQuota{}
	for _, e := range events {
		raw, ok := e.Extra["quota"]
		if !ok || e.Backend == "" {
			continue
		}
		var windows []providers.QuotaWindow
		if b, err := json.Marshal(raw); err != nil || json.Unmarshal(b, &windows) != nil || len(windows) == 0 {
			continue
		}
		if prev, ok := latest[e.Backend]; ok && parseTS(prev.SeenAt).After(parseTS(e.TS)) {
			continue
		}
		latest[e.Backend] = providerQuota{Backend: e.Backend, Windows: windows, SeenAt: e.TS}
	}
	out := quotaPayload{Providers: make([]providerQuota, 0, len(latest))}
	for _, q := range latest {
		out.Providers = append(out.Providers, q)
	}
	sort.Slice(out.Providers, func(i, j int) bool { return out.Providers[i].Backend < out.Providers[j].Backend })
	writeJSON(w, http.StatusOK, out)
}
