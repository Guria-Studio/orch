package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/model"
)

func toolLine(id, cmd string) string {
	return `{"type":"assistant","message":{"id":"msg_` + id + `","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_` + id + `","name":"Bash","input":{"command":"` + cmd + `"}}]}}` + "\n"
}

// watchFixture is one running attempt whose log is at the returned path.
func watchFixture(t *testing.T) (*reapFixture, string) {
	t.Helper()
	f := newReapFixture(t, []model.Task{task("C-1", 1, "claude/opus")}, map[string]fakeResponse{}, SchedulerOptions{})
	f.s.Opts.Cfg.Watch = config.Watch{Enabled: true, StallAfterS: 600}
	log := filepath.Join(t.TempDir(), "C-1.log")
	f.s.inFlight[4242] = &InFlight{Task: task("C-1", 1, "claude/opus"), Attempt: 1,
		Route: model.RouteEntry{Backend: model.BackendClaude}, Dispatch: Dispatch{LogPath: log}}
	return f, log
}

// TestWatchFlagsALoopingAgent pins #487: an agent re-running one command is
// reported on the next watch, once.
func TestWatchFlagsALoopingAgent(t *testing.T) {
	f, log := watchFixture(t)
	var b strings.Builder
	for i := range 6 {
		b.WriteString(toolLine(string(rune('a'+i)), "go test ./..."))
	}
	write(t, log, b.String())

	now := time.Now()
	f.s.WatchAgents(context.Background(), now)
	f.s.WatchAgents(context.Background(), now.Add(watchInterval))

	if got := f.backend.eventTypes("C-1"); len(got) != 1 || got[0] != EventAgentLooping {
		t.Fatalf("events = %v, want one agent_looping", got)
	}
	if call := f.backend.eventExtra("C-1", EventAgentLooping)["call"]; call != "Bash go test ./..." {
		t.Errorf("call = %v, want the repeated command", call)
	}
}

// TestWatchFlagsASilentLogOnceUntilItGrows.
func TestWatchFlagsASilentLogOnceUntilItGrows(t *testing.T) {
	f, log := watchFixture(t)
	write(t, log, toolLine("a", "make build"))
	now := time.Now()
	ctx := context.Background()

	f.s.WatchAgents(ctx, now)
	f.s.WatchAgents(ctx, now.Add(9*time.Minute))
	if got := f.backend.eventTypes("C-1"); len(got) != 0 {
		t.Fatalf("events after 9 silent minutes = %v, want none", got)
	}
	f.s.WatchAgents(ctx, now.Add(10*time.Minute))
	f.s.WatchAgents(ctx, now.Add(15*time.Minute))
	if got := f.backend.eventTypes("C-1"); len(got) != 1 || got[0] != EventAgentStalled {
		t.Fatalf("events = %v, want one agent_stalled", got)
	}

	write(t, log, toolLine("a", "make build")+toolLine("b", "make test"))
	f.s.WatchAgents(ctx, now.Add(16*time.Minute))
	f.s.WatchAgents(ctx, now.Add(26*time.Minute))
	if got := f.backend.eventTypes("C-1"); len(got) != 2 {
		t.Fatalf("events = %v, want a second agent_stalled after the log grew and went silent again", got)
	}
}

// TestWatchIsQuietOnRealRuns: the providers' captured runs raise nothing.
func TestWatchIsQuietOnRealRuns(t *testing.T) {
	logs, err := filepath.Glob("../providers/testdata/*/*/*.jsonl")
	if err != nil || len(logs) == 0 {
		t.Fatalf("no captures: %v", err)
	}
	for _, p := range logs {
		raw, err := os.ReadFile(p) // #nosec G304 -- fixed testdata path
		if err != nil {
			t.Fatal(err)
		}
		if call, n := repeatedCall(raw); n >= loopRepeats {
			t.Errorf("%s: flagged %q x%d", p, call, n)
		}
	}
}

// TestWatchOffRecordsNothing.
func TestWatchOffRecordsNothing(t *testing.T) {
	f, log := watchFixture(t)
	f.s.Opts.Cfg.Watch.Enabled = false
	write(t, log, strings.Repeat(toolLine("a", "go test ./..."), 6))
	f.s.WatchAgents(context.Background(), time.Now())
	if got := f.backend.eventTypes("C-1"); len(got) != 0 {
		t.Fatalf("events = %v, want none", got)
	}
}
