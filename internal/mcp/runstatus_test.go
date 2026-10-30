package mcp

import (
	"context"
	"testing"

	"github.com/Guria-Studio/orch/internal/state"
)

// ---- orch_run_status -------------------------------------------------------

func TestRunStatusWithoutARunSaysSo(t *testing.T) {
	cs, _, _ := newTestSession(t)

	var out runStatusOut
	call(t, cs, "orch_run_status", map[string]any{}, &out)
	if out.Run != nil {
		t.Errorf("run = %+v, want nil before any run", out.Run)
	}
	if len(out.Working) != 0 {
		t.Errorf("working = %+v, want none", out.Working)
	}
	// The python-api template is a two-task DAG; both start todo.
	if out.Total != 2 || out.Counts["todo"] != 2 {
		t.Errorf("total = %d, counts = %v; want 2 todo", out.Total, out.Counts)
	}
}

func TestRunStatusShowsWhatIsInFlight(t *testing.T) {
	ctx := context.Background()
	cs, _, backend := newTestSession(t)

	if err := backend.StartRun(ctx, "run-1", "auto"); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := backend.RecordDispatch(ctx, state.Dispatch{
		RunID: "run-1", TaskID: "F0.T1", Backend: "claude", PID: 4242,
		Attempt: 1, StartedAt: "2026-10-18T10:00:00Z",
	}); err != nil {
		t.Fatalf("RecordDispatch: %v", err)
	}

	var out runStatusOut
	call(t, cs, "orch_run_status", map[string]any{}, &out)
	if out.Run == nil || out.Run.RunID != "run-1" {
		t.Fatalf("run = %+v, want run-1", out.Run)
	}
	if len(out.Working) != 1 {
		t.Fatalf("working = %+v, want the one in-flight dispatch", out.Working)
	}
	w := out.Working[0]
	if w.TaskID != "F0.T1" || w.Backend != "claude" || w.Attempt != 1 || w.Title == "" {
		t.Errorf("working[0] = %+v, want F0.T1 on claude, attempt 1, with a title", w)
	}
}
