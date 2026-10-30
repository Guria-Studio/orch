package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
)

// The operator's stop and note controls (#356). The dashboard stays read-only
// for state: these write requests `orch run` acts on, through the same
// project.Steer `orch task stop|note` use. Only the operator on this machine
// may: the same gates as the tunnel controls, so a write through a tunnel is
// refused.

func (s *Server) steerRoutes() []route {
	return []route{
		{pattern: "POST /api/task/{id}/stop", name: "api_task_stop", handler: s.handleTaskStop},
		{pattern: "POST /api/task/{id}/note", name: "api_task_note", handler: s.handleTaskNote},
	}
}

// steerRequestLimit bounds a request body: a note, or a task's definition.
const steerRequestLimit = 64 << 10

type steerBody struct {
	Hold bool   `json:"hold"`
	Note string `json:"note"`
	// Retry sends the task back to todo with the note (#454's Send back).
	Retry bool `json:"retry"`
}

// steerGate answers whether the handler may go on, and the body it sent.
func (s *Server) steerGate(w http.ResponseWriter, r *http.Request) (steerBody, bool) {
	var body steerBody
	// Two statements: in one return, Go leaves unspecified whether body is
	// read before or after operatorWrite fills it.
	ok := s.operatorWrite(w, r, s.steer != nil, &body)
	return body, ok
}

// operatorWrite gates every write the dashboard makes — steer and task edits:
// on (else 404), the operator profile, from this machine and the dashboard's
// own origin (else 403), so a tunnel cannot reach it. It decodes the body into
// dst; an empty body leaves it zero.
func (s *Server) operatorWrite(w http.ResponseWriter, r *http.Request, on bool, dst any) bool {
	switch {
	case !on:
		writeJSON(w, http.StatusNotFound, errorPayload{Detail: "not found"})
	case s.cfg.Profile != ProfileOperator:
		writeJSON(w, http.StatusForbidden, errorPayload{Detail: "operator only"})
	case !isLocalRequest(r):
		writeJSON(w, http.StatusForbidden, errorPayload{Detail: "loopback only"})
	case !sameOriginLocal(r):
		writeJSON(w, http.StatusForbidden, errorPayload{Detail: "cross-origin request refused"})
	default:
		err := json.NewDecoder(io.LimitReader(r.Body, steerRequestLimit)).Decode(dst)
		if err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, errorPayload{Detail: "the body is not JSON: " + err.Error()})
			return false
		}
		return true
	}
	return false
}

// steerFailed answers a refused or failed write.
func (s *Server) steerFailed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, state.ErrTaskNotFound):
		writeJSON(w, http.StatusNotFound, errorPayload{Detail: err.Error()})
	case errors.Is(err, state.ErrIllegalTransition), errors.Is(err, project.ErrRunOwnsTask):
		writeJSON(w, http.StatusConflict, errorPayload{Detail: err.Error()})
	default:
		s.log.Error("dashboard steer failed", "err", err)
		writeJSON(w, http.StatusBadRequest, errorPayload{Detail: err.Error()})
	}
}

func (s *Server) handleTaskStop(w http.ResponseWriter, r *http.Request) {
	body, ok := s.steerGate(w, r)
	if !ok {
		return
	}
	steer := *s.steer
	steer.Source = "dashboard"
	res, err := steer.Stop(r.Context(), r.PathValue("id"), body.Hold, body.Note)
	if err != nil {
		s.steerFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleTaskNote(w http.ResponseWriter, r *http.Request) {
	body, ok := s.steerGate(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if body.Retry {
		res, err := s.steer.SendBack(r.Context(), id, body.Note)
		if err != nil {
			s.steerFailed(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"task_id": id, "message": "sent back with the note: " + res.Message})
		return
	}
	if err := s.steer.Note(r.Context(), id, body.Note); err != nil {
		s.steerFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"task_id": id, "message": "note added; the task's next attempt's prompt carries it"})
}
