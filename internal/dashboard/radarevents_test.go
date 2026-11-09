package dashboard

import (
	"testing"

	"github.com/Guria-Studio/orch/internal/state"
)

// The conflict radar's events (#482) read as warnings naming the other task.
func TestRadarEventsReadInTheLog(t *testing.T) {
	for etype, want := range map[string]string{
		"files_overlap":   "[20:00:00] RADAR B files_overlap (with A)",
		"branch_conflict": "[20:00:00] RADAR B branch_conflict (with A)",
	} {
		got := formatEvent(state.Event{EventType: etype, TaskID: "B", TS: "2026-10-22T20:00:00Z",
			Extra: map[string]any{"with": "A", "files": []any{"README.md"}}})
		if got.Human != want || got.Severity != "warn" {
			t.Errorf("%s = %q %q, want %q warn", etype, got.Human, got.Severity, want)
		}
	}
}

// agent_waiting (#524) reads as information with its evidence: the agent is
// working, nothing to act on.
func TestAgentWaitingReadsInTheLog(t *testing.T) {
	got := formatEvent(state.Event{EventType: "agent_waiting", TaskID: "F7.7", TS: "2026-10-22T20:00:00Z",
		Extra: map[string]any{"reason": "downloading data: +163 MB in 1h0m0s", "activity": "download"}})
	if want := "[20:00:00] WATCH F7.7 agent_waiting (downloading data: +163 MB in 1h0m0s)"; got.Human != want || got.Severity != "info" {
		t.Errorf("agent_waiting = %q %q, want %q info", got.Human, got.Severity, want)
	}
}

// semantic_collision (#522) reads as a warning with the rule's reason.
func TestSemanticCollisionReadsInTheLog(t *testing.T) {
	got := formatEvent(state.Event{EventType: "semantic_collision", TaskID: "B", TS: "2026-10-22T20:00:00Z",
		Extra: map[string]any{"with": "base", "key": "005", "reason": "migrations: db/005_b.sql adds 005, which main already has as db/005_a.sql"}})
	if want := "[20:00:00] COLLIDE B semantic_collision (migrations: db/005_b.sql adds 005, which main already has as db/005_a.sql)"; got.Human != want || got.Severity != "warn" {
		t.Errorf("semantic_collision = %q %q, want %q warn", got.Human, got.Severity, want)
	}
}
