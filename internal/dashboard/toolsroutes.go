package dashboard

import (
	"net/http"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/doctor"
	"github.com/hectorcanaimero/orch/internal/router"
)

func (s *Server) toolsRoutes() []route {
	return []route{
		{pattern: "GET /api/tools", name: "api_tools", handler: s.handleTools},
	}
}

// describeTools is doctor.Tools, which probes PATH; tests replace it.
var describeTools = doctor.Tools

// toolsPayload is `/api/tools`'s body: the System › Tools page.
type toolsPayload struct {
	Tools []doctor.Tool `json:"tools"`
}

// handleTools lists the programs orch runs on this machine — the same
// questions `orch doctor` asks, as one list: which this project uses, which
// are here, how to install the rest. Operator-only (not on the stakeholder
// allow-list): what a machine has installed is not a client's business.
//
// A tasks.json, router or config.yaml that does not load still answers: the
// tools are listed with what could be read, because this page is where
// someone setting a machine up looks.
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	view, _ := s.loadView(r.Context())
	cfg := config.Defaults()
	if loaded, err := config.Load(s.paths.ConfigYAML, s.paths.Root); err == nil {
		cfg = loaded.Config
	}
	rtr, _ := router.Load(s.paths.RouterYAML())
	tools := describeTools(doctor.ToolsConfig{
		Root:     s.paths.Root,
		Backends: doctor.ReferencedBackends(view.Tasks, rtr),
		VCS: doctor.VCSConfig{
			WorktreeMode: cfg.Dispatch.WorktreeMode, AutoPR: cfg.VCS.AutoPR,
			Provider: cfg.VCS.Provider, Host: cfg.VCS.Host,
		},
		Tunnel: cfg.Tunnel.Enabled,
		E2E:    len(cfg.Verify.E2E) > 0,
	})
	writeJSON(w, http.StatusOK, toolsPayload{Tools: tools})
}
