package engine

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
)

// commentsOf is a CommentReader over a fixed map.
type commentsOf map[string][]json.RawMessage

func (c commentsOf) TaskComments(_ context.Context, id string) ([]json.RawMessage, error) {
	return c[id], nil
}

// TestDispatchPromptCarriesTheOperatorsNotes: #356. A note the operator left
// on the task is in the prompt of its next dispatch, read from the task's own
// trail at dispatch time.
func TestDispatchPromptCarriesTheOperatorsNotes(t *testing.T) {
	routes := map[string]model.RouteEntry{"claude/opus": route(model.BackendClaude, "opus", false)}
	f := newSchedulerFixture(t, []model.Task{task("A-1", 1, "claude/opus")}, routes, SchedulerOptions{
		Mode: ModeAuto, GlobalMax: 1, PerProvider: map[string]int{"claude": 1},
	})
	f.s.Comments = commentsOf{"A-1": {
		json.RawMessage(`{"author":"operator","body":"do not touch the migration","at":"2026-10-16T12:00:00Z","kind":"operator_note"}`),
	}}
	if _, err := f.s.Refill(context.Background()); err != nil {
		t.Fatalf("Refill: %v", err)
	}
	if len(f.s.InFlight()) != 1 {
		t.Fatalf("in flight = %d, want 1", len(f.s.InFlight()))
	}
	for _, e := range f.s.InFlight() {
		raw, err := os.ReadFile(e.Dispatch.PromptPath)
		if err != nil {
			t.Fatalf("reading the prompt: %v", err)
		}
		if !strings.Contains(string(raw), "Operator notes") || !strings.Contains(string(raw), "  - do not touch the migration\n") {
			t.Errorf("the prompt lacks the operator's note:\n%s", raw)
		}
	}
}
