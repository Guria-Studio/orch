package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/project"
	"github.com/hectorcanaimero/orch/internal/state"
)

// stopRequestsAt hands the loop one stop request from its second read on —
// by then the child the first tick dispatched is running — and runs onAsk
// when it does.
type stopRequestsAt struct {
	mu    sync.Mutex
	reads int
	req   state.Event
	onAsk func()
}

func (f *stopRequestsAt) LatestEventID(context.Context) (int64, error) { return 0, nil }

func (f *stopRequestsAt) EventsSince(_ context.Context, after int64, _ int) ([]state.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.reads < 2 || after >= f.req.ID {
		return nil, nil
	}
	if f.onAsk != nil {
		f.onAsk()
		f.onAsk = nil
	}
	return []state.Event{f.req}, nil
}

func stopRequest(taskID string, hold bool) state.Event {
	return state.Event{ID: 7, EventType: project.EventOperatorStop, TaskID: taskID,
		Extra: map[string]any{"hold": hold, "requested_by": "cli"}}
}

// slowTask makes the fake CLI for C-1 run for a minute, and returns the file
// that does it.
func slowTask(t *testing.T) string {
	t.Helper()
	dir, _ := FakeProviderDir()
	path := filepath.Join(dir, string(model.BackendClaude), "C-1"+fakeSleepExt)
	write(t, path, "60")
	return path
}

// TestStopAndHoldEndsARunningChild: #356. The request kills the real child
// long before its minute is up, the attempt is recorded as stopped (not a
// failure), and the task ends held, not retried.
func TestStopAndHoldEndsARunningChild(t *testing.T) {
	f := newRunnerFixture(t, []model.Task{task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": okResponse()}, SchedulerOptions{})
	slowTask(t)
	f.r.Requests = &stopRequestsAt{req: stopRequest("C-1", true)}

	start := time.Now()
	if code := f.runWithin(t, 30*time.Second); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("the run took %s: the child was not stopped", took)
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusBlocked {
		t.Errorf("C-1 = %q, want blocked (held)", got)
	}
	if f.s.Dispatched() != 1 {
		t.Errorf("dispatched = %d, want 1: a held task is not dispatched again", f.s.Dispatched())
	}
	if len(f.backend.finishes) != 1 || !f.backend.finishes[0].Stopped || !f.backend.finishes[0].Held || f.backend.finishes[0].Failure != "" {
		t.Errorf("finishes = %+v, want one stopped and held, unclassified", f.backend.finishes)
	}
	if !strings.Contains(strings.Join(f.backend.notes, "\n"), "C-1=blocked: "+project.HeldReason) {
		t.Errorf("notes = %q, want the hold", f.backend.notes)
	}
}

// TestStopWithoutHoldDispatchesAgainWithTheSameAttempt: a redirect ends the
// attempt and dispatches the task again at once, as the same attempt — the
// operator's stop does not count against max_attempts.
func TestStopWithoutHoldDispatchesAgainWithTheSameAttempt(t *testing.T) {
	f := newRunnerFixture(t, []model.Task{task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": okResponse()}, SchedulerOptions{})
	sleep := slowTask(t)
	// The second attempt finishes at once.
	f.r.Requests = &stopRequestsAt{req: stopRequest("C-1", false), onAsk: func() { _ = os.Remove(sleep) }}

	if code := f.runWithin(t, 30*time.Second); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusDone {
		t.Errorf("C-1 = %q, want done after the second attempt", got)
	}
	var attempts []any
	for _, e := range f.backend.events {
		if e.eventType == EventDispatch {
			attempts = append(attempts, e.extra["attempt"])
		}
	}
	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 1 {
		t.Errorf("dispatch attempts = %v, want [1 1]", attempts)
	}
	if !f.backend.finishes[0].Stopped || f.backend.finishes[0].Held {
		t.Errorf("first finish = %+v, want stopped, not held", f.backend.finishes[0])
	}
}

// TestStopHoldsATaskWaitingToRetry: not running, but in the retry queue —
// held, and the retry is dropped.
func TestStopHoldsATaskWaitingToRetry(t *testing.T) {
	f := newRunnerFixture(t, []model.Task{task("C-1", 1, "claude/opus")}, nil, SchedulerOptions{})
	tk, _ := f.s.Queue.Task("C-1")
	f.s.retryQueue = []RetryItem{{Task: tk, Attempt: 2, EarliestAt: f.r.now().Add(time.Hour)}}
	f.s.StopTask(context.Background(), "C-1", true)
	if len(f.s.RetryQueue()) != 0 {
		t.Errorf("retry queue = %+v, want empty", f.s.RetryQueue())
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusBlocked {
		t.Errorf("C-1 = %q, want blocked", got)
	}
	last := f.backend.events[len(f.backend.events)-1]
	if last.eventType != project.EventOperatorStop || last.extra["stopped"] != "held" {
		t.Errorf("answer = %+v", last)
	}
}
