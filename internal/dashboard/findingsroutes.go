package dashboard

import (
	"net/http"
	"strconv"

	"github.com/Guria-Studio/orch/internal/findings"
	"github.com/Guria-Studio/orch/internal/project"
)

// FindingsDesk is what the Findings page reads the queue from and sends
// through (#536): the project's state and the operator's gh. A nil Filer
// keeps the page read-only.
type FindingsDesk struct {
	Store findings.Store
	Filer findings.Filer
}

func (s *Server) findingsRoutes() []route {
	return []route{
		{pattern: "GET /api/findings", name: "api_findings", handler: s.handleFindings},
		{pattern: "POST /api/findings/{id}/send", name: "api_findings_send", handler: s.handleFindingDecision(true)},
		{pattern: "POST /api/findings/{id}/dismiss", name: "api_findings_dismiss", handler: s.handleFindingDecision(false)},
	}
}

type findingPayload struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	TaskID   string `json:"task_id"`
	QueuedAt string `json:"queued_at"`
	URL      string `json:"url"`
}

type findingsPayload struct {
	Findings []findingPayload `json:"findings"`
	Pending  int              `json:"pending"`
	CanSend  bool             `json:"can_send"`
	Repo     string           `json:"repo"`
}

// handleFindings is the queue, newest first, with the exact title and body
// sending would file. An operator route: the stakeholder allow-list does not
// name it.
func (s *Server) handleFindings(w http.ResponseWriter, r *http.Request) {
	out := findingsPayload{Findings: []findingPayload{}, Repo: findings.Repo}
	if s.findings == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	rows, err := s.findings.Store.QueuedFindings(r.Context())
	if err != nil {
		s.failRead(w, "findings", err)
		return
	}
	for _, f := range rows {
		out.Findings = append(out.Findings, findingPayload{ID: f.ID, Status: f.Status, Title: f.Title, Body: f.Body,
			TaskID: f.TaskID, QueuedAt: f.QueuedAt.UTC().Format("2006-01-02T15:04:05Z"), URL: f.URL})
	}
	out.Pending = findings.Pending(rows)
	out.CanSend = s.findings.Filer != nil
	writeJSON(w, http.StatusOK, out)
}

// handleFindingDecision is the page's Send and Dismiss: the same
// findings.Send and findings.Dismiss as `orch findings send|dismiss`, behind
// the gates every operator write has.
func (s *Server) handleFindingDecision(send bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		on := s.findings != nil && (!send || s.findings.Filer != nil)
		var body struct{}
		if !s.operatorWrite(w, r, on, &body) {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorPayload{Detail: "not a finding id"})
			return
		}
		status, url := project.FindingDismissed, ""
		if send {
			status = project.FindingSent
			url, err = findings.Send(r.Context(), s.findings.Store, s.findings.Filer, id)
		} else {
			err = findings.Dismiss(r.Context(), s.findings.Store, id)
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorPayload{Detail: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": status, "url": url})
	}
}
