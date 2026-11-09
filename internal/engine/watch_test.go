package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/model"
)

func toolLine(id, cmd string) string {
	return `{"type":"assistant","message":{"id":"msg_` + id + `","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_` + id + `","name":"Bash","input":{"command":"` + cmd + `"}}]}}` + "\n"
}

// watchFixture is one running attempt whose log is at the returned path. Its
// agent is a real child that does nothing — a sleep in its own process group,
// keyed by its pid like a spawned one — working in an empty worktree.
func watchFixture(t *testing.T) (*reapFixture, string) {
	t.Helper()
	f := newReapFixture(t, []model.Task{task("C-1", 1, "claude/opus")}, map[string]fakeResponse{}, SchedulerOptions{})
	f.s.Opts.Cfg.Watch = config.Defaults().Watch // liveness on, as shipped
	log := filepath.Join(t.TempDir(), "C-1.log")
	f.s.inFlight[idleChild(t)] = &InFlight{Task: task("C-1", 1, "claude/opus"), Attempt: 1,
		Route: model.RouteEntry{Backend: model.BackendClaude}, Dispatch: Dispatch{LogPath: log, Cwd: t.TempDir()}}
	return f, log
}

// idleChild starts a process that uses no CPU and writes nothing, as the
// leader of its own group like a spawned agent, and returns its pid.
func idleChild(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "3600")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the idle child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// attemptOf is the fixture's one attempt.
func attemptOf(f *reapFixture) *InFlight {
	for _, e := range f.s.inFlight {
		return e
	}
	return nil
}

// TestWatchDoesNotStallASilentAgentWhoseFilesGrow reproduces tradex F7.7
// (#524): an agent spent an hour downloading 60 days of candles, writing
// nothing to its log while the data file grew to 163 MB. The watch called it
// stalled. Growing files are work: the attempt is waiting, with the evidence,
// and never stalled.
func TestWatchDoesNotStallASilentAgentWhoseFilesGrow(t *testing.T) {
	f, log := watchFixture(t)
	write(t, log, toolLine("a", "python scripts/fetch_candles.py --days 60"))
	data := filepath.Join(attemptOf(f).Dispatch.Cwd, "data", "candles.csv")
	write(t, data, "")
	ctx, now := context.Background(), time.Now()
	const perTick = 163 << 20 / 120 // 163 MB over an hour of 30 s watches
	for i := range 121 {
		if err := os.Truncate(data, int64(i*perTick)); err != nil {
			t.Fatal(err)
		}
		f.s.WatchAgents(ctx, now.Add(time.Duration(i)*watchInterval))
	}

	for _, ev := range f.backend.eventTypes("C-1") {
		if ev == EventAgentStalled {
			t.Fatalf("events = %v: a silent agent whose files grow is not stalled", f.backend.eventTypes("C-1"))
		}
	}
	extra := f.backend.eventExtra("C-1", "agent_waiting")
	if extra == nil {
		t.Fatalf("events = %v, want agent_waiting with the evidence", f.backend.eventTypes("C-1"))
	}
	if reason, _ := extra["reason"].(string); !strings.Contains(reason, "MB") {
		t.Errorf("reason = %q, want the growth in MB", reason)
	}
}

// TestWatchStallsASilentAgentThatDoesNothing is the other half of #524: no
// output, no CPU, no file growth is stuck, at stall_after_s as before.
func TestWatchStallsASilentAgentThatDoesNothing(t *testing.T) {
	f, log := watchFixture(t)
	write(t, log, toolLine("a", "make build"))
	write(t, filepath.Join(attemptOf(f).Dispatch.Cwd, "main.go"), "package main\n")
	ctx, now := context.Background(), time.Now()
	for i := range 21 {
		f.s.WatchAgents(ctx, now.Add(time.Duration(i)*watchInterval))
	}
	if got := f.backend.eventTypes("C-1"); len(got) != 1 || got[0] != EventAgentStalled {
		t.Fatalf("events = %v, want one agent_stalled at 10 silent minutes", got)
	}
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

	// The log grows again: the agent is back, and the timeline says so — a
	// stall with nothing after it would read as stuck forever (#533).
	write(t, log, toolLine("a", "make build")+toolLine("b", "make test"))
	f.s.WatchAgents(ctx, now.Add(16*time.Minute))
	if got := f.backend.eventTypes("C-1"); len(got) != 2 || got[1] != EventAgentResumed {
		t.Fatalf("events = %v, want agent_stalled then agent_resumed", got)
	}
	f.s.WatchAgents(ctx, now.Add(26*time.Minute))
	if got := f.backend.eventTypes("C-1"); len(got) != 3 || got[2] != EventAgentStalled {
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

// TestTheWatchEventsAreOnesTheDatabaseTakes: the watch writes through
// state.Backend, whose event types are a closed list. agent_resumed (#533)
// was missing from it, so every "writing again" was refused at the write and
// the dashboard kept a resumed agent stalled; agent_waiting is #524's.
func TestTheWatchEventsAreOnesTheDatabaseTakes(t *testing.T) {
	rec := StateRecorder{Backend: openBackend(t)}
	for _, ev := range []string{EventAgentStalled, EventAgentLooping, EventAgentResumed, "agent_waiting"} {
		if err := rec.AppendEngineEvent(context.Background(), "run-1", ev, "B-020", "claude", map[string]any{"attempt": 1}); err != nil {
			t.Errorf("%s: %v", ev, err)
		}
	}
}
