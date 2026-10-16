package cli

import (
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/state"
)

// TestStopAnswerSaysWhatTheRunDid covers every answer the run writes back to
// a stop request (#356).
func TestStopAnswerSaysWhatTheRunDid(t *testing.T) {
	for _, tc := range []struct {
		stopped string
		hold    bool
		want    string
	}{
		{"attempt", true, "stopped: T's attempt ended; the task is held (blocked) — `orch task set --id T --status todo` releases it"},
		{"attempt", false, "stopped: T's attempt ended; it is dispatched again"},
		{"held", true, "T was not running; it is held (blocked)"},
		{"retry_now", false, "T was waiting to retry; it is dispatched now"},
		{"already_done", true, "T is already done; nothing to stop"},
		{"not_running", false, "T was not running; nothing to stop"},
	} {
		ev := state.Event{Extra: map[string]any{"stopped": tc.stopped, "hold": tc.hold}}
		if got := stopAnswer("T", ev); got != tc.want {
			t.Errorf("%s/hold=%v: %q, want %q", tc.stopped, tc.hold, got, tc.want)
		}
		if !strings.Contains(stopAnswer("T", ev), "T") {
			t.Errorf("%s: the answer does not name the task", tc.stopped)
		}
	}
}
