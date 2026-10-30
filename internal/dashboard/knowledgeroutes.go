package dashboard

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Guria-Studio/orch/internal/knowledge"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
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
		{pattern: "POST /api/knowledge/curate/estimate", name: "api_knowledge_curate_estimate", handler: s.handleCurateEstimate},
		{pattern: "POST /api/knowledge/curate", name: "api_knowledge_curate", handler: s.handleCurate},
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

// curatePayload is what a curation would do and cost, as `orch knowledge
// curate` prints it before asking, and the task it runs as.
type curatePayload struct {
	Task     string `json:"task"`
	Estimate string `json:"estimate"`
}

// handleCurateEstimate is Curate now's dialog: the curation's estimate,
// answered no, so nothing is dispatched.
func (s *Server) handleCurateEstimate(w http.ResponseWriter, r *http.Request) {
	var none struct{}
	if !s.operatorWrite(w, r, s.curate != nil, &none) {
		return
	}
	var out bytes.Buffer
	var asked *curatePayload
	err := s.curate(r.Context(), &out, func(id string) bool {
		asked = &curatePayload{Task: id, Estimate: out.String()}
		return false
	})
	if asked == nil {
		s.curateFailed(w, err, out.String())
		return
	}
	writeJSON(w, http.StatusOK, asked)
}

// handleCurate is Curate now confirmed: the curation runs in the background,
// and this answers once it is dispatched, with its task. One at a time.
func (s *Server) handleCurate(w http.ResponseWriter, r *http.Request) {
	var none struct{}
	if !s.operatorWrite(w, r, s.curate != nil, &none) {
		return
	}
	if !s.curating.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusConflict, errorPayload{Detail: "a curation is already running"})
		return
	}
	type step struct {
		started *curatePayload
		err     error
		out     string
	}
	first := make(chan step, 1)
	go func() {
		defer func() {
			s.curating.Store(false)
			if s.curated != nil {
				s.curated()
			}
		}()
		var out bytes.Buffer
		dispatched := false
		// ponytail: outlives the request but not the process — Ctrl+C on the
		// dashboard ends it like a killed `orch run`, and the reaper picks up.
		err := s.curate(context.WithoutCancel(r.Context()), &out, func(id string) bool {
			dispatched = true
			first <- step{started: &curatePayload{Task: id, Estimate: out.String()}}
			return true
		})
		if !dispatched {
			first <- step{err: err, out: out.String()}
		} else if err != nil {
			s.log.Warn("dashboard knowledge curation failed", "err", err)
		}
	}()
	st := <-first
	if st.started == nil {
		s.curateFailed(w, st.err, st.out)
		return
	}
	writeJSON(w, http.StatusAccepted, st.started)
}

// curateFailed answers a curation that ended before its estimate: nil with
// what it printed is nothing to curate (409), an error a refusal (400).
func (s *Server) curateFailed(w http.ResponseWriter, err error, out string) {
	if err == nil {
		writeJSON(w, http.StatusConflict, errorPayload{Detail: strings.TrimSpace(out)})
		return
	}
	s.log.Warn("dashboard knowledge curation refused", "err", err)
	writeJSON(w, http.StatusBadRequest, errorPayload{Detail: strings.TrimSpace(err.Error())})
}
