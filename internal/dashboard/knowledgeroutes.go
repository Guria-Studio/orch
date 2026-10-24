package dashboard

import (
	"errors"
	"net/http"
	"time"

	"github.com/hectorcanaimero/orch/internal/knowledge"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/state"
)

// The Knowledge page (#453): the project's lessons, how each one did, and the
// operator's add, edit and delete — through the same knowledge.Editor as
// `orch knowledge add|rm`. None of these is on the stakeholder allow-list.

func (s *Server) knowledgeRoutes() []route {
	return []route{
		{pattern: "GET /api/knowledge", name: "api_knowledge", handler: s.handleKnowledge},
		{pattern: "POST /api/knowledge", name: "api_knowledge_add", handler: s.handleKnowledgeAdd},
		{pattern: "PATCH /api/knowledge/{id}", name: "api_knowledge_edit", handler: s.handleKnowledgeEdit},
		{pattern: "DELETE /api/knowledge/{id}", name: "api_knowledge_rm", handler: s.handleKnowledgeRemove},
	}
}

type lessonRow struct {
	knowledge.Lesson
	knowledge.Use
}

type knowledgePayload struct {
	Lessons []lessonRow `json:"lessons"`
	Sources []string    `json:"sources"`
}

// handleKnowledge lists the lessons with their use, or, with ?q=, the ones
// that text finds, best first — knowledge.Search, as orch_knowledge_search
// ranks them. Filtering by area and source is the page's.
func (s *Server) handleKnowledge(w http.ResponseWriter, r *http.Request) {
	if s.knowledge == nil {
		writeJSON(w, http.StatusNotFound, errorPayload{Detail: "not found"})
		return
	}
	ctx := r.Context()
	lessons, err := knowledge.Load(s.knowledge.Root)
	if err != nil {
		s.failRead(w, "knowledge", err)
		return
	}
	if q := r.URL.Query().Get("q"); q != "" {
		ranker, _ := s.state.(knowledge.Ranker)
		if lessons, err = knowledge.Search(ctx, ranker, lessons, q, len(lessons)); err != nil {
			s.failRead(w, "knowledge", err)
			return
		}
	}
	events, err := s.state.AllEvents(ctx, 0)
	if err != nil {
		s.failRead(w, "knowledge", err)
		return
	}
	done, err := s.state.Tasks(ctx, state.TaskFilter{Statuses: []model.Status{model.StatusDone}})
	if err != nil {
		s.failRead(w, "knowledge", err)
		return
	}
	use := knowledge.Usage(events, done)
	out := knowledgePayload{Lessons: make([]lessonRow, 0, len(lessons)), Sources: knowledge.Sources}
	for _, l := range lessons {
		out.Lessons = append(out.Lessons, lessonRow{Lesson: l, Use: use[l.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) knowledgeEditor() knowledge.Editor {
	ed := *s.knowledge
	ed.Source = "dashboard"
	return ed
}

func (s *Server) handleKnowledgeAdd(w http.ResponseWriter, r *http.Request) {
	var l knowledge.Lesson
	if !s.operatorWrite(w, r, s.knowledge != nil, &l) {
		return
	}
	added, err := s.knowledgeEditor().Add(r.Context(), l, time.Now())
	if err != nil {
		s.knowledgeFailed(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, added)
}

func (s *Server) handleKnowledgeEdit(w http.ResponseWriter, r *http.Request) {
	var l knowledge.Lesson
	if !s.operatorWrite(w, r, s.knowledge != nil, &l) {
		return
	}
	edited, err := s.knowledgeEditor().Update(r.Context(), r.PathValue("id"), l, time.Now())
	if err != nil {
		s.knowledgeFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, edited)
}

func (s *Server) handleKnowledgeRemove(w http.ResponseWriter, r *http.Request) {
	var none struct{}
	if !s.operatorWrite(w, r, s.knowledge != nil, &none) {
		return
	}
	id := r.PathValue("id")
	if err := s.knowledgeEditor().Remove(r.Context(), id, time.Now()); err != nil {
		s.knowledgeFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"lesson": id})
}

// knowledgeFailed answers a refused write: 404 an unknown lesson, 400 one
// that does not validate.
func (s *Server) knowledgeFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, knowledge.ErrNoLesson) {
		writeJSON(w, http.StatusNotFound, errorPayload{Detail: err.Error()})
		return
	}
	s.log.Warn("dashboard knowledge write refused", "err", err)
	writeJSON(w, http.StatusBadRequest, errorPayload{Detail: err.Error()})
}
