package mcp

import (
	"context"
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/prompt"
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
