package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
)

func operatorSend(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:7420"
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Origin", "http://127.0.0.1:7420")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func readTasks(t *testing.T, s *Server) model.TasksFile {
	t.Helper()
	tf, err := model.LoadTasksFile(filepath.Join(s.paths.Root, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	return tf
}

// The task endpoints keep stop's gates: nobody but the local operator writes
// tasks.json or moves a task.
func TestTaskEditEndpointsRefuseAnyoneButTheLocalOperator(t *testing.T) {
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
			before, _ := os.ReadFile(filepath.Join(s.paths.Root, "tasks.json"))
			for _, c := range []struct{ method, path, body string }{
				{http.MethodPost, "/api/tasks", `{"id":"T-9","title":"x","model":"claude/sonnet"}`},
				{http.MethodPatch, "/api/task/T-2", `{"title":"x"}`},
				{http.MethodPost, "/api/task/T-2/status", `{"status":"done"}`},
			} {
				req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
				req.Host = "127.0.0.1:7420"
				req.RemoteAddr = tc.remote
				if tc.origin != "" {
					req.Header.Set("Origin", tc.origin)
				}
				req.Header.Set("Authorization", "Bearer test-token-stakeholder")
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Errorf("%s %s = %d, want 403: %s", c.method, c.path, rec.Code, rec.Body)
				}
			}
			after, _ := os.ReadFile(filepath.Join(s.paths.Root, "tasks.json"))
			if string(before) != string(after) {
				t.Error("a refused request changed tasks.json")
			}
			if rt, _ := backend.Task(context.Background(), "T-2"); rt.Status != model.StatusTodo {
				t.Errorf("a refused request moved T-2 to %s", rt.Status)
			}
		})
	}
}

func TestTheOperatorAddsAndEditsTasks(t *testing.T) {
	s, backend := newSteerServer(t, ProfileOperator, "")
	ctx := context.Background()

	rec := operatorSend(t, s, http.MethodPost, "/api/tasks",
		`{"id":"T-9","title":"Docs","model":"claude/sonnet","dependencies":["T-2"],"phase":1,"backlog":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d: %s", rec.Code, rec.Body)
	}
	if rt, err := backend.Task(ctx, "T-9"); err != nil || rt.Status != model.StatusBacklog {
		t.Errorf("T-9 in the database = %v, %v; want backlog", rt.Status, err)
	}
	if rec := operatorSend(t, s, http.MethodPost, "/api/tasks", `{"id":"T-9","title":"again"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("duplicate id = %d, want 422: %s", rec.Code, rec.Body)
	}

	if rec := operatorSend(t, s, http.MethodPatch, "/api/task/T-2", `{"title":"Schema"}`); rec.Code != http.StatusOK {
		t.Fatalf("edit = %d: %s", rec.Code, rec.Body)
	}
	for _, task := range readTasks(t, s).Tasks {
		if task.ID == "T-2" && task.Title != "Schema" {
			t.Errorf("T-2's title in tasks.json = %q", task.Title)
		}
	}
	for _, c := range []struct {
		path, body string
		want       int
	}{
		{"/api/task/T-2", `{"dependencies":["NOPE"]}`, http.StatusUnprocessableEntity},
		{"/api/task/T-1", `{"dependencies":["T-3"]}`, http.StatusUnprocessableEntity}, // a cycle
		{"/api/task/NOPE", `{"title":"x"}`, http.StatusNotFound},
		{"/api/task/T-2", `{}`, http.StatusBadRequest},
	} {
		if rec := operatorSend(t, s, http.MethodPatch, c.path, c.body); rec.Code != c.want {
			t.Errorf("PATCH %s %s = %d, want %d: %s", c.path, c.body, rec.Code, c.want, rec.Body)
		}
	}
}

// Moving on the board: blocking needs a reason, a running task is stopped
// rather than moved behind its run's back, and only orch run starts a task.
func TestTheOperatorMovesTasks(t *testing.T) {
	s, backend := newSteerServer(t, ProfileOperator, "") // T-1 is in-progress
	ctx := context.Background()
	move := func(id, body string) *httptest.ResponseRecorder {
		return operatorSend(t, s, http.MethodPost, "/api/task/"+id+"/status", body)
	}
	status := func(id string) model.Status {
		rt, _ := backend.Task(ctx, id)
		return rt.Status
	}

	for _, c := range []struct {
		id, body string
		want     int
	}{
		{"T-2", `{"status":"blocked"}`, http.StatusBadRequest},   // no reason
		{"T-2", `{"status":"sideways"}`, http.StatusBadRequest},  // no such status
		{"T-2", `{"status":"in-progress"}`, http.StatusConflict}, // only orch run starts one
		{"T-1", `{"status":"done"}`, http.StatusConflict},        // running: stop it first
		{"NOPE", `{"status":"todo"}`, http.StatusNotFound},
	} {
		if rec := move(c.id, c.body); rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d: %s", c.id, c.body, rec.Code, c.want, rec.Body)
		}
	}
	if got := status("T-2"); got != model.StatusTodo {
		t.Fatalf("a refused move changed T-2 to %s", got)
	}

	if rec := move("T-2", `{"status":"blocked","note":"waiting on the API key"}`); rec.Code != http.StatusOK {
		t.Fatalf("block = %d: %s", rec.Code, rec.Body)
	}
	rt, _ := backend.Task(ctx, "T-2")
	if rt.Status != model.StatusBlocked || !strings.Contains(string(rt.Comments[len(rt.Comments)-1]), "waiting on the API key") {
		t.Errorf("T-2 = %s, %s; want blocked with the reason", rt.Status, rt.Comments)
	}
	if rec := move("T-2", `{"status":"todo"}`); rec.Code != http.StatusOK || status("T-2") != model.StatusTodo {
		t.Errorf("unblock = %d, T-2 %s", rec.Code, status("T-2"))
	}

	// No orch run is running, so stopping T-1 is a plain change, as stop says.
	rec := move("T-1", `{"status":"blocked","note":"wrong approach"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "no orch run is running") {
		t.Fatalf("block a running task = %d: %s", rec.Code, rec.Body)
	}
	if got := status("T-1"); got != model.StatusBlocked {
		t.Errorf("T-1 = %s, want blocked (held)", got)
	}
}

func TestTaskEditEndpointsNeedAnEditor(t *testing.T) {
	_, backend := newSteerServer(t, ProfileOperator, "")
	off, err := New(cfg(ProfileOperator, ""), Options{Static: spaHandler(t), State: backend})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/tasks"}, {http.MethodPatch, "/api/task/T-2"}, {http.MethodPost, "/api/task/T-2/status"},
	} {
		if rec := operatorSend(t, off, c.method, c.path, `{}`); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s with no editor = %d, want 404", c.method, c.path, rec.Code)
		}
	}
}
