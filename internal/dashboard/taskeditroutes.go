package dashboard

import (
	"errors"
	"net/http"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
)

// The operator's task management (v0.22): add a task, edit its definition,
// move it on the board. Definitions go through the same project.TaskEditor
// as `orch task add|edit|set`, moves through project.Steer.Move; the gates
// are stop's and note's.

func (s *Server) taskEditRoutes() []route {
	return []route{
		{pattern: "POST /api/tasks", name: "api_task_add", handler: s.handleTaskAdd},
		{pattern: "PATCH /api/task/{id}", name: "api_task_edit", handler: s.handleTaskEdit},
		{pattern: "POST /api/task/{id}/status", name: "api_task_move", handler: s.handleTaskMove},
	}
}

// taskAddBody is a new task: the definition fields, its id, and whether it
// waits in the backlog instead of todo.
type taskAddBody struct {
	project.TaskPatch
	ID      string `json:"id"`
	Backlog bool   `json:"backlog"`
}

func orZero[T any](v *T) T {
	var zero T
	if v == nil {
		return zero
	}
	return *v
}

func (s *Server) handleTaskAdd(w http.ResponseWriter, r *http.Request) {
	var body taskAddBody
	if !s.operatorWrite(w, r, s.editor != nil, &body) {
		return
	}
	p := body.TaskPatch
	t := model.Task{
		ID: body.ID, Title: orZero(p.Title), Model: orZero(p.Model), Description: orZero(p.Description),
		Reason: orZero(p.Reason), SpecRef: orZero(p.SpecRef), Phase: orZero(p.Phase),
		EstimateHours: orZero(p.EstimateHours), Dependencies: orZero(p.Dependencies), Files: orZero(p.Files),
	}
	if body.Backlog {
		t.Status = model.StatusBacklog
	}
	added, err := s.editor.Add(r.Context(), t)
	if err != nil {
		s.editFailed(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"task_id": added.ID, "status": string(added.Status)})
}

func (s *Server) handleTaskEdit(w http.ResponseWriter, r *http.Request) {
	var p project.TaskPatch
	if !s.operatorWrite(w, r, s.editor != nil, &p) {
		return
	}
	if p == (project.TaskPatch{}) {
		writeJSON(w, http.StatusBadRequest, errorPayload{Detail: "nothing to change"})
		return
	}
	id := r.PathValue("id")
	if _, err := s.editor.Update(r.Context(), id, p); err != nil {
		s.editFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"task_id": id, "message": "task " + id + " updated"})
}

type taskMoveBody struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func (s *Server) handleTaskMove(w http.ResponseWriter, r *http.Request) {
	var body taskMoveBody
	if !s.operatorWrite(w, r, s.steer != nil, &body) {
		return
	}
	to, err := model.ParseStatus(body.Status)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload{Detail: err.Error()})
		return
	}
	steer := *s.steer
	steer.Source = "dashboard"
	res, err := steer.Move(r.Context(), r.PathValue("id"), to, body.Note)
	if err != nil {
		s.steerFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// editFailed answers a refused edit: 404 unknown task, 422 an edit the DAG
// would not survive.
func (s *Server) editFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, project.ErrInvalidEdit) {
		writeJSON(w, http.StatusUnprocessableEntity, errorPayload{Detail: err.Error()})
		return
	}
	s.steerFailed(w, err)
}
