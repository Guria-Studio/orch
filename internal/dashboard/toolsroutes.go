package dashboard

import (
	"net/http"
	"sync"
	"time"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/doctor"
	"github.com/hectorcanaimero/orch/internal/router"
)

func (s *Server) toolsRoutes() []route {
	return []route{
		{pattern: "GET /api/tools", name: "api_tools", handler: s.handleTools},
		{pattern: "GET /api/tools/missing", name: "api_tools_missing", handler: s.handleToolsMissing},
	}
}

// describeTools is doctor.Tools, which probes PATH; tests replace it.
var describeTools = doctor.Tools

// toolsPayload is `/api/tools`'s body: the System › Tools page.
type toolsPayload struct {
	Tools []doctor.Tool `json:"tools"`
	// Commands are the config.yaml shell commands whose program is not
	// installed, as `orch doctor`'s config.commands names them (#440).
	Commands []doctor.UnresolvedCommand `json:"commands"`
}

// toolsCacheTTL is how long the sidebar's count reuses a probe: probing every
// CLI takes seconds, and installing one is a person at a terminal.
const toolsCacheTTL = time.Minute

// toolsCache keeps the last probe; GET /api/tools always refreshes it.
type toolsCache struct {
	mu    sync.Mutex
	at    time.Time
	tools []doctor.Tool
}

func (c *toolsCache) put(tools []doctor.Tool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at, c.tools = time.Now(), tools
}

// fresh is the cached probe, or nil when there is none newer than the TTL.
func (c *toolsCache) fresh() []doctor.Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tools == nil || time.Since(c.at) > toolsCacheTTL {
		return nil
	}
	return c.tools
}

// handleToolsMissing is the sidebar's badge: how many tools this project uses
// are not installed. Answered from the last probe while it is under a minute
// old, probing again otherwise. Operator-only, like /api/tools.
func (s *Server) handleToolsMissing(w http.ResponseWriter, r *http.Request) {
	tools := s.tools.fresh()
	if tools == nil {
		tools = s.probeTools(r)
	}
	missing := 0
	for _, t := range tools {
		if t.Used && !t.Found {
			missing++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"missing": missing})
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
	cfg := config.Defaults()
	if loaded, err := config.Load(s.paths.ConfigYAML, s.paths.Root); err == nil {
		cfg = loaded.Config
	}
	commands := doctor.UnresolvedCommands(s.paths.Root, cfg)
	if commands == nil {
		commands = []doctor.UnresolvedCommand{}
	}
	writeJSON(w, http.StatusOK, toolsPayload{Tools: s.probeTools(r), Commands: commands})
}

// probeTools runs doctor.Tools for this project and caches the answer.
func (s *Server) probeTools(r *http.Request) []doctor.Tool {
	view, _ := s.loadView(r.Context())
	cfg := config.Defaults()
	if loaded, err := config.Load(s.paths.ConfigYAML, s.paths.Root); err == nil {
		cfg = loaded.Config
	}
	rtr, _ := router.Load(s.paths.RouterYAML())
	var latest doctor.LatestFunc
	if cfg.Update.CheckCLIs {
		latest = s.cliLatest
	}
	tools := describeTools(doctor.ToolsConfig{
		Root:     s.paths.Root,
		Backends: doctor.ReferencedBackends(view.Tasks, rtr),
		VCS: doctor.VCSConfig{
			WorktreeMode: cfg.Dispatch.WorktreeMode, AutoPR: cfg.VCS.AutoPR,
			Provider: cfg.VCS.Provider, Host: cfg.VCS.Host,
		},
		Tunnel: cfg.Tunnel.Enabled,
		E2E:    len(cfg.Verify.E2E) > 0,
		Latest: latest,
	})
	s.tools.put(tools)
	return tools
}
