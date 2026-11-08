package dashboard

import (
	"context"
	"net/http"
)

// ProjectFlow is the welcome page's plan and apply steps for the project this
// dashboard serves, and a run of its plan (#548). internal/cli implements it
// with the functions `orch start` and the welcome page use; this package only
// puts them on HTTP. Each step returns an error the page shows as is.
type ProjectFlow interface {
	State() any
	Plan(idea string) error
	Apply(ctx context.Context) error
	Run() error
}

// The dashboard's Plan page and the Now page's Run (#548): writes, so each
// step passes operatorWrite, as steer and the task edits do. The state is an
// operator read, off the stakeholder allow-list.
func (s *Server) flowRoutes() []route {
	return []route{
		{pattern: "GET /api/flow", name: "api_flow", handler: s.handleFlowState},
		{pattern: "POST /api/flow/plan", name: "api_flow_plan", handler: s.flowStep(func(_ *http.Request, b flowBody) error {
			return s.flow.Plan(b.Idea)
		})},
		{pattern: "POST /api/flow/apply", name: "api_flow_apply", handler: s.flowStep(func(r *http.Request, _ flowBody) error {
			return s.flow.Apply(r.Context())
		})},
		{pattern: "POST /api/flow/run", name: "api_flow_run", handler: s.flowStep(func(*http.Request, flowBody) error {
			return s.flow.Run()
		})},
	}
}

type flowBody struct {
	Idea string `json:"idea"`
}

func (s *Server) handleFlowState(w http.ResponseWriter, _ *http.Request) {
	if s.flow == nil {
		writeJSON(w, http.StatusNotFound, errorPayload{Detail: "not found"})
		return
	}
	writeJSON(w, http.StatusOK, s.flow.State())
}

// flowStep runs one step behind operatorWrite and answers with the new state,
// or 400 and the step's error as `detail`.
func (s *Server) flowStep(run func(*http.Request, flowBody) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body flowBody
		if !s.operatorWrite(w, r, s.flow != nil, &body) {
			return
		}
		if err := run(r, body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorPayload{Detail: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s.flow.State())
	}
}
