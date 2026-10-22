package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The operator's Tests page: every E2E run the local verify collected
// (verify.e2e_report), one per task attempt, and what each test left behind.
// The runs come from the e2e_report events; each run's report and files live
// under the state directory, where the verify kept them. Operator only: the
// screenshots and videos are of the app as it is being built.

// eventE2EReport is engine.EventE2EReport, the event a collected run records.
const eventE2EReport = "e2e_report"

// e2eReportFile is verify.ReportFile, the report inside an attempt's directory.
const e2eReportFile = "report.json"

func (s *Server) testsRoutes() []route {
	return []route{
		{pattern: "GET /api/tests", name: "api_tests", handler: s.handleTests},
		{pattern: "GET /api/tests/{run}/{task}/{attempt}", name: "api_tests_report", handler: s.handleTestReport},
		{pattern: "GET /api/tests/{run}/{task}/{attempt}/files/{file...}", name: "api_tests_file", handler: s.handleTestFile},
	}
}

// e2eRun is one collected E2E run: a task attempt and its counts.
type e2eRun struct {
	RunID   string `json:"run_id"`
	TaskID  string `json:"task_id"`
	Attempt int    `json:"attempt"`
	At      string `json:"at"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Flaky   int    `json:"flaky"`
	Skipped int    `json:"skipped"`
}

type testsPayload struct {
	// Runs are newest first.
	Runs []e2eRun `json:"runs"`
}

func (s *Server) handleTests(w http.ResponseWriter, r *http.Request) {
	if s.state == nil {
		s.failRead(w, "tests", errNoBackend)
		return
	}
	events, err := s.state.AllEvents(r.Context(), 0)
	if err != nil {
		s.failRead(w, "tests", err)
		return
	}
	out := testsPayload{Runs: []e2eRun{}}
	for _, e := range events {
		if e.EventType != eventE2EReport {
			continue
		}
		out.Runs = append(out.Runs, e2eRun{
			RunID:   e.RunID,
			TaskID:  e.TaskID,
			Attempt: intExtra(e.Extra["attempt"]),
			At:      e.TS,
			Passed:  intExtra(e.Extra["passed"]),
			Failed:  intExtra(e.Extra["failed"]),
			Flaky:   intExtra(e.Extra["flaky"]),
			Skipped: intExtra(e.Extra["skipped"]),
		})
	}
	// AllEvents is oldest first, already ordered the way the database means
	// it; newest first is that, reversed.
	slices.Reverse(out.Runs)
	writeJSON(w, http.StatusOK, out)
}

// intExtra reads a count from an event's extra, which JSON decoding leaves
// as a float64.
func intExtra(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func (s *Server) handleTestReport(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.attemptDir(w, r)
	if !ok {
		return
	}
	b, err := os.ReadFile(filepath.Join(dir, e2eReportFile)) // #nosec G304 -- attemptDir validated every component
	if errors.Is(err, fs.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "no E2E report for this attempt"})
		return
	}
	if err != nil {
		s.failRead(w, "tests", err)
		return
	}
	if !json.Valid(b) {
		s.failRead(w, "tests", fmt.Errorf("%s is not JSON", filepath.Join(dir, e2eReportFile)))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// attachmentTypes are the content types of what Playwright leaves that Go's
// own table does not know; a video needs its type for the browser to play it.
var attachmentTypes = map[string]string{
	".webm": "video/webm",
	".zip":  "application/zip",
	".md":   "text/markdown; charset=utf-8",
}

func (s *Server) handleTestFile(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.attemptDir(w, r)
	if !ok {
		return
	}
	name := r.PathValue("file")
	clean := path.Clean("/" + name)[1:]
	if name == "" || clean != name || strings.Contains(name, "\\") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "bad file path"})
		return
	}
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(clean))) // #nosec G304 -- a cleaned path inside the attempt's directory
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "no such file"})
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "no such file"})
		return
	}
	if ct, ok := attachmentTypes[strings.ToLower(filepath.Ext(clean))]; ok {
		w.Header().Set("Content-Type", ct)
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(clean)))
	}
	// ServeContent answers Range requests, which is how a video seeks.
	http.ServeContent(w, r, path.Base(clean), info.ModTime(), f)
}

// attemptDir is <state>/e2e/<run>/<task>/attempt-<n> for the request's path,
// the layout engine.E2EArtifactsDir writes. Every component is checked to be
// one plain path segment, so no request reaches outside the e2e directory.
func (s *Server) attemptDir(w http.ResponseWriter, r *http.Request) (string, bool) {
	run, task := r.PathValue("run"), r.PathValue("task")
	attempt, err := strconv.Atoi(r.PathValue("attempt"))
	if err != nil || attempt < 1 || !plainSegment(run) || !plainSegment(task) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "bad run, task or attempt"})
		return "", false
	}
	return filepath.Join(s.paths.StateDir(), "e2e", run, task, "attempt-"+strconv.Itoa(attempt)), true
}

func plainSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`)
}
