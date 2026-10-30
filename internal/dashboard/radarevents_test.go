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
