package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/prompt"
	"github.com/Guria-Studio/orch/internal/state"
)

// TestTaskNoteReachesThePromptNotes: orch_task_note writes the note the next
// attempt's prompt renders (#356).
func TestTaskNoteReachesThePromptNotes(t *testing.T) {
	cs, _, backend := newTestSession(t)
	var out taskNoteOut
	if res := call(t, cs, "orch_task_note", map[string]any{"task_id": "F0.T1", "note": "use uv, not pip"}, &out); res.IsError {
		t.Fatalf("orch_task_note refused: %+v", res.Content)
	}
	rt, err := backend.Task(context.Background(), "F0.T1")
	if err != nil {
		t.Fatal(err)
	}
	if got := prompt.OperatorNotes(rt.Comments); len(got) != 1 || got[0] != "use uv, not pip" {
		t.Errorf("operator notes = %q", got)
	}
	if rt.Status != model.StatusTodo {
		t.Errorf("a note moved the task to %s", rt.Status)
	}
}

// TestTaskStopWithoutARunIsAStatusChange and, with one, a request the run reads.
func TestTaskStopWithoutARunIsAStatusChange(t *testing.T) {
	root, backend := newTestProject(t)
	stateDir := filepath.Join(root, "state")
	cs := connect(t, Options{Backend: backend, TasksJSON: filepath.Join(root, "tasks.json"), StateDir: stateDir})
	ctx := context.Background()
	if err := backend.Transition(ctx, "F0.T1", model.StatusInProgress, state.Note{}); err != nil {
		t.Fatal(err)
	}

	var res project.StopResult
	call(t, cs, "orch_task_stop", map[string]any{"task_id": "F0.T1"}, &res)
	if res.Requested || res.To != model.StatusTodo || !strings.Contains(res.Message, "no orch run is running") {
		t.Errorf("no run: %+v", res)
	}

	// A run holds the lock: the stop is a request, not a status change.
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(stateDir, project.RunLockFile)) // #nosec G304 -- a temp dir
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	call(t, cs, "orch_task_stop", map[string]any{"task_id": "F0.T1", "hold": true, "note": "wait for me"}, &res)
	if !res.Requested {
		t.Fatalf("with a run: %+v", res)
	}
	events, err := backend.Events(ctx, "F0.T1", 0)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.EventType != project.EventOperatorStop || last.Extra["hold"] != true || last.Extra["requested_by"] != "mcp" {
		t.Errorf("request event = %+v", last)
	}
	if rt, _ := backend.Task(ctx, "F0.T1"); rt.Status != model.StatusTodo {
		t.Errorf("the request moved the task itself: %s", rt.Status)
	}
}
