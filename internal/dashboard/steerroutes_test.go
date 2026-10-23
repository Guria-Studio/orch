package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/project"
	"github.com/hectorcanaimero/orch/internal/prompt"
	"github.com/hectorcanaimero/orch/internal/state"
)

// newSteerServer is a dashboard with the operator's write endpoints over a real
// database whose T-1 is in-progress.
func newSteerServer(t *testing.T, profile Profile, token string) (*Server, state.Backend) {
	t.Helper()
	ctx := context.Background()
	root := writeProject(t, "spec_root: specs\n", testTasksJSON)
	db, _, err := state.Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "demo", root)
	tasks, err := model.LoadTasksFile(filepath.Join(root, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Bootstrap(ctx, tasks.Tasks); err != nil {
		t.Fatal(err)
	}
	if err := backend.Transition(ctx, "T-1", model.StatusInProgress, state.Note{}); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	s, err := New(cfg(profile, token), Options{
		Static: spaHandler(t),
		State:  backend,
		Paths:  config.Paths{Root: root, ID: "demo", ConfigYAML: filepath.Join(root, "config.yaml")},
		Steer:  &project.Steer{Backend: backend, StateDir: stateDir},
		Editor: &project.TaskEditor{TasksJSON: filepath.Join(root, "tasks.json"), StateDir: stateDir, Backend: backend, Source: "dashboard"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, backend
}

func steerPost(t *testing.T, s *Server, path, body, remote, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = "127.0.0.1:7420"
	req.RemoteAddr = remote
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.Header.Set("Authorization", "Bearer test-token-stakeholder")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestSteerEndpointsRefuseAnyoneButTheLocalOperator: #356. Stakeholders,
// callers off this machine and pages from another site are refused, and
// none of them moved the task.
func TestSteerEndpointsRefuseAnyoneButTheLocalOperator(t *testing.T) {
	for _, tc := range []struct {
		name, remote, origin string
		profile              Profile
	}{
		{"stakeholder", "127.0.0.1:50000", "", ProfileStakeholder},
		{"not loopback", "192.168.1.20:50000", "", ProfileOperator},
		{"cross-origin", "127.0.0.1:50000", "https://evil.example", ProfileOperator},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := ""
			if tc.profile == ProfileStakeholder {
				token = "test-token-stakeholder"
			}
			s, backend := newSteerServer(t, tc.profile, token)
			for _, path := range []string{"/api/task/T-1/stop", "/api/task/T-1/note"} {
				rec := steerPost(t, s, path, `{"note":"x"}`, tc.remote, tc.origin)
				if rec.Code != http.StatusForbidden {
					t.Errorf("%s = %d, want 403: %s", path, rec.Code, rec.Body)
				}
			}
			rt, _ := backend.Task(context.Background(), "T-1")
			if rt.Status != model.StatusInProgress || len(prompt.OperatorNotes(rt.Comments)) != 0 {
				t.Errorf("a refused request wrote: %s, %v", rt.Status, rt.Comments)
			}
		})
	}
}

func TestSteerEndpointsForTheLocalOperator(t *testing.T) {
	s, backend := newSteerServer(t, ProfileOperator, "")
	ctx := context.Background()

	rec := steerPost(t, s, "/api/task/T-1/note", `{"note":"keep it small"}`, "127.0.0.1:50000", "http://127.0.0.1:7420")
	if rec.Code != http.StatusOK {
		t.Fatalf("note = %d: %s", rec.Code, rec.Body)
	}
	rec = steerPost(t, s, "/api/task/T-1/stop", `{"hold":true}`, "127.0.0.1:50000", "http://127.0.0.1:7420")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "no orch run is running") {
		t.Fatalf("stop = %d: %s", rec.Code, rec.Body)
	}
	rt, _ := backend.Task(ctx, "T-1")
	if rt.Status != model.StatusBlocked {
		t.Errorf("T-1 = %s, want blocked (held, no run)", rt.Status)
	}
	if got := prompt.OperatorNotes(rt.Comments); len(got) != 1 || got[0] != "keep it small" {
		t.Errorf("notes = %q", got)
	}
	if rec := steerPost(t, s, "/api/task/NOPE/note", `{"note":"x"}`, "127.0.0.1:50000", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown task = %d, want 404", rec.Code)
	}
	if rec := steerPost(t, s, "/api/task/T-1/note", `{"note":"  "}`, "127.0.0.1:50000", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("empty note = %d, want 400", rec.Code)
	}
}

// A hold on a done task is an illegal move (409); a dashboard built without
// Steer has no such routes (404).
func TestSteerEndpointsConflictAndAbsence(t *testing.T) {
	s, backend := newSteerServer(t, ProfileOperator, "")
	if err := backend.Transition(context.Background(), "T-1", model.StatusDone, state.Note{}); err != nil {
		t.Fatal(err)
	}
	if rec := steerPost(t, s, "/api/task/T-1/stop", `{"hold":true}`, "127.0.0.1:50000", ""); rec.Code != http.StatusConflict {
		t.Errorf("hold on a done task = %d, want 409: %s", rec.Code, rec.Body)
	}

	off, err := New(cfg(ProfileOperator, ""), Options{Static: spaHandler(t), State: backend})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/task/T-1/stop", "/api/task/T-1/note"} {
		if rec := steerPost(t, off, path, `{"note":"x"}`, "127.0.0.1:50000", ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s without Steer = %d, want 404", path, rec.Code)
		}
	}
}
