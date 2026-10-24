package engine

import (
	"path/filepath"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/model"
)

// sameDecision compares two decisions; RouteEntry holds a slice (Failover),
// so == no longer compiles.
func sameDecision(a, b RetryDecision) bool { return reflect.DeepEqual(a, b) }

const codexOK = `{"type":"thread.started","thread_id":"t"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"ok"}}
{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5}}
`

// failoverFixture is a reap fixture whose claude route fails over to codex.
// claude answers C-1 with claudeC1 and every other task with a success, so a
// task that reached claude would finish there; codex answers everything with
// a success.
func failoverFixture(t *testing.T, claudeC1 fakeResponse) (*reapFixture, *time.Time) {
	t.Helper()
	f := newReapFixture(t,
		[]model.Task{
			task("C-1", 1, "claude/opus"),
			task("C-2", 1, "claude/opus", "C-1"),
			task("C-3", 1, "claude/opus", "C-2"),
			task("C-4", 1, "claude/opus", "C-3"),
		},
		map[string]fakeResponse{"C-1": claudeC1, "_default": okResponse()},
		SchedulerOptions{PerProvider: map[string]int{"claude": 4, "codex": 4}})
	fake, _ := FakeProviderDir()
	write(t, filepath.Join(fake, "codex", "_default.out"), codexOK)
	opus := route(model.BackendClaude, "opus", false)
	opus.Failover = []string{"codex/gpt"}
	f.s.Routes = map[string]model.RouteEntry{
		"claude/opus": opus,
		"codex/gpt":   route(model.BackendCodex, "gpt-5.5", false),
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	f.s.now = func() time.Time { return now }
	return f, &now
}

// dispatchedOn runs one tick to completion and returns the backend of every
// dispatch it started.
func (f *reapFixture) dispatchedOn(t *testing.T) []string {
	t.Helper()
	before := len(f.backend.events)
	f.runOnce(t)
	var out []string
	for _, e := range f.backend.events[before:] {
		if e.eventType == EventDispatch {
			out = append(out, e.taskID+"@"+e.backend)
		}
	}
	return out
}

// TestRateLimitFailsOverToTheNextProvider (#439): claude runs out of quota,
// the retry goes to codex at once and finishes, and the next claude task goes
// straight to codex until the rate-limit window has passed.
func TestRateLimitFailsOverToTheNextProvider(t *testing.T) {
	f, now := failoverFixture(t, failResponse("429 rate limit exceeded"))

	if got := f.dispatchedOn(t); !equalStrings(got, []string{"C-1@claude"}) {
		t.Fatalf("first tick dispatched %v, want C-1 on claude", got)
	}
	// No backoff: the retry is due now, on codex.
	if got := f.dispatchedOn(t); !equalStrings(got, []string{"C-1@codex"}) {
		t.Fatalf("retry dispatched %v, want C-1 on codex with no backoff", got)
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusDone {
		t.Fatalf("C-1 = %q after the failover, want done", got)
	}
	if got := f.dispatchedOn(t); !equalStrings(got, []string{"C-2@codex"}) {
		t.Fatalf("C-2 dispatched %v, want codex while claude is exhausted", got)
	}

	// Past retry.rate_limit's 60s, claude is back.
	*now = now.Add(61 * time.Second)
	if got := f.dispatchedOn(t); !equalStrings(got, []string{"C-3@claude"}) {
		t.Fatalf("C-3 dispatched %v, want claude after the reset", got)
	}
}

// TestClaudeRateLimitEventFailsOver: a run whose rate_limit_event says the
// window is rejected fails over even when its error reads as something else,
// and claude stays skipped until the event's own reset time.
func TestClaudeRateLimitEventFailsOver(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "providers", "testdata", "claude", "synthetic", "quota-exhausted.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	f, now := failoverFixture(t, fakeResponse{out: string(fixture), exitCode: "1"})
	// The fixture's window resets at 1792155000; start an hour before it.
	const resets = 1792155000
	*now = time.Unix(resets-3600, 0)

	for _, want := range []string{"C-1@claude", "C-1@codex", "C-2@codex"} {
		if got := f.dispatchedOn(t); !equalStrings(got, []string{want}) {
			t.Fatalf("dispatched %v, want %s", got, want)
		}
	}
	// 61s is not enough: the event said an hour.
	*now = now.Add(61 * time.Second)
	if got := f.dispatchedOn(t); !equalStrings(got, []string{"C-3@codex"}) {
		t.Fatalf("C-3 dispatched %v, want codex before the event's reset", got)
	}
	*now = time.Unix(resets+1, 0)
	if got := f.dispatchedOn(t); !equalStrings(got, []string{"C-4@claude"}) {
		t.Fatalf("C-3 dispatched %v, want claude after the reset", got)
	}
}

// TestNoFailoverKeepsTheBackoff: without `failover` a rate limit waits out
// its backoff on the same provider, as before.
func TestNoFailoverKeepsTheBackoff(t *testing.T) {
	f, _ := failoverFixture(t, failResponse("429 rate limit exceeded"))
	opus := f.s.Routes["claude/opus"]
	opus.Failover = nil
	f.s.Routes["claude/opus"] = opus

	f.runOnce(t)
	q := f.s.RetryQueue()
	if len(q) != 1 || q[0].Route.Backend != model.BackendClaude || !q[0].EarliestAt.After(f.s.now()) {
		t.Fatalf("retry queue = %+v, want claude after a backoff", q)
	}
}

// TestFailoverKeepsTheWorktree: the failed attempt's worktree is neither
// committed nor removed, and the codex retry runs in it.
func TestFailoverKeepsTheWorktree(t *testing.T) {
	f, _ := failoverFixture(t, failResponse("429 rate limit exceeded"))
	wt := &recordingWorktree{dir: t.TempDir()}
	f.s.Worktree = wt
	f.s.Opts.WorktreeMode = true

	f.runOnce(t)
	if got := wt.callList(); !equalStrings(got, []string{"create"}) {
		t.Fatalf("after the rate limit calls = %v, want the worktree kept", got)
	}
	if got := f.dispatchedOn(t); !equalStrings(got, []string{"C-1@codex"}) {
		t.Fatalf("retry dispatched %v, want codex", got)
	}
	if got := wt.callList(); !equalStrings(got, []string{"create", "commit", "push", "remove"}) {
		t.Errorf("calls = %v, want the retry to reuse the worktree", got)
	}
}
