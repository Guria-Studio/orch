package project

import (
	"os"
	"testing"

	"github.com/Guria-Studio/orch/internal/state"
)

// TestDispatchAliveSeesAReusedPID (#541): the dashboard's Now page and the
// steer guard ask whether a dispatch still runs; a live PID that started at
// another time is someone else's process.
func TestDispatchAliveSeesAReusedPID(t *testing.T) {
	pid := os.Getpid()
	start := ProcStart(pid)
	if start == "" {
		t.Skip("this platform cannot tell a process's start time")
	}
	if !DispatchAlive(state.Dispatch{PID: pid, ProcStart: start}) {
		t.Error("the dispatch's own process reads as gone")
	}
	if !DispatchAlive(state.Dispatch{PID: pid}) {
		t.Error("a row with no start time recorded must fall back to the PID check")
	}
	if DispatchAlive(state.Dispatch{PID: pid, ProcStart: start + "1"}) {
		t.Error("a reused PID reads as the dispatch")
	}
}
