package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/vcs"
)

// The failures of #347: several PRs went green together, the first merged,
// and the rest failed to merge on that same poll because the base had just
// moved — then were never asked again.
var (
	errMergeUnknown = &vcs.MergeError{
		Detail:     "X Pull request o/r#1 is not mergeable: the merge commit cannot be cleanly created.",
		MergeState: "UNKNOWN", Retryable: true,
	}
	errMergeBehind = &vcs.MergeError{
		Detail: "X Pull request o/r#1 is not mergeable", MergeState: "BEHIND",
		Retryable: true, BranchUpdated: true,
	}
	errMergeConflict = &vcs.MergeError{Detail: "conflicts", MergeState: "DIRTY"}
)

// mergeClock steps the fixture's clock; the poller's interval is set to a
// second so every step past it is a real poll.
type mergeClock struct {
	t   *testing.T
	f   *ciFixture
	now time.Time
}

func newMergeFixture(t *testing.T) (*ciFixture, *mergeClock) {
	t.Helper()
	f := newCIFixture(t, pendingRow("C-1", "https://github.com/o/r/pull/1", 0))
	f.vcs.status["https://github.com/o/r/pull/1"] = vcs.CISuccess
	f.poller.AutoMerge = true
	f.poller.PollInterval = time.Second
	c := &mergeClock{t: t, f: f, now: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
	f.s.now = func() time.Time { return c.now }
	return f, c
}

// poll advances the clock by d and polls once.
func (c *mergeClock) poll(d time.Duration) int {
	c.t.Helper()
	c.now = c.now.Add(d)
	acted, err := c.f.poller.Poll(context.Background(), c.f.s)
	if err != nil {
		c.t.Fatalf("Poll: %v", err)
	}
	return acted
}

func (f *ciFixture) queueStatus(t *testing.T, id string) model.Status {
	t.Helper()
	got, _ := f.s.Queue.Status(id)
	return got
}

func (f *ciFixture) lastEventExtra(t *testing.T, eventType string) map[string]any {
	t.Helper()
	f.backend.mu.Lock()
	defer f.backend.mu.Unlock()
	for i := len(f.backend.events) - 1; i >= 0; i-- {
		if e := f.backend.events[i]; e.eventType == eventType {
			return e.extra
		}
	}
	t.Fatalf("no %s event", eventType)
	return nil
}

// TestAutoMergeRetriesATransientFailure: a merge refused because the base
// just moved is asked again after a wait, and the task is not finished — so
// no dependent is released — until it merges (#347).
func TestAutoMergeRetriesATransientFailure(t *testing.T) {
	f, c := newMergeFixture(t)
	f.vcs.mergeErrs = []error{errMergeUnknown, nil}

	if acted := c.poll(0); acted != 0 {
		t.Errorf("acted = %d on a merge put off, want 0", acted)
	}
	if got := f.queueStatus(t, "C-1"); got == model.StatusDone {
		t.Fatalf("the task is done while its merge is still being retried; its dependents would start without it")
	}
	if got := f.backend.eventTypes("C-1"); len(got) != 0 {
		t.Errorf("events = %v, want none until the merge is settled", got)
	}
	if got := f.ci.statuses(); len(got) != 0 {
		t.Errorf("ci status writes = %v, want the row left pending", got)
	}

	// Before the backoff: CI is asked, the merge is not.
	c.poll(5 * time.Second)
	if got := f.vcs.mergedPRs(); len(got) != 1 {
		t.Fatalf("merge attempts = %d inside the backoff, want 1", len(got))
	}

	// Past it: the second attempt merges, and only now is the task done.
	if acted := c.poll(mergeBackoff(1)); acted != 1 {
		t.Errorf("acted = %d, want 1", acted)
	}
	if got := f.vcs.mergedPRs(); len(got) != 2 {
		t.Fatalf("merge attempts = %d, want 2", len(got))
	}
	if got := f.queueStatus(t, "C-1"); got != model.StatusDone {
		t.Errorf("status = %q, want done once merged", got)
	}
	if got := f.backend.eventTypes("C-1"); !equalStrings(got, []string{EventCISuccess, EventPRAutoMerged}) {
		t.Errorf("events = %v", got)
	}
}

// TestAutoMergeGivesUpVisibly: a merge that keeps failing finishes the task
// as before — done, PR open — after mergeMaxAttempts, with gh's own words in
// the event and the open PR in the task's note.
func TestAutoMergeGivesUpVisibly(t *testing.T) {
	f, c := newMergeFixture(t)
	f.vcs.mergeErr = errMergeUnknown

	for i := 1; i < mergeMaxAttempts; i++ {
		c.poll(mergeBackoff(i))
		if got := f.queueStatus(t, "C-1"); got == model.StatusDone {
			t.Fatalf("done after %d attempts, want %d", i, mergeMaxAttempts)
		}
	}
	c.poll(time.Minute)

	if got := len(f.vcs.mergedPRs()); got != mergeMaxAttempts {
		t.Errorf("merge attempts = %d, want %d", got, mergeMaxAttempts)
	}
	if got := f.queueStatus(t, "C-1"); got != model.StatusDone {
		t.Errorf("status = %q, want done: the existing semantics stand", got)
	}
	if got := f.backend.eventTypes("C-1"); !equalStrings(got, []string{EventCISuccess, EventPRAutoMergeFailed}) {
		t.Errorf("events = %v", got)
	}
	extra := f.lastEventExtra(t, EventPRAutoMergeFailed)
	if got, _ := extra["error"].(string); got != errMergeUnknown.Error() {
		t.Errorf("event error = %q, want gh's diagnosis %q", got, errMergeUnknown.Error())
	}
	if got := extra["attempts"]; got != mergeMaxAttempts {
		t.Errorf("event attempts = %v, want %d", got, mergeMaxAttempts)
	}
	f.backend.mu.Lock()
	notes := strings.Join(f.backend.notes, "\n")
	f.backend.mu.Unlock()
	if !strings.Contains(notes, "auto-merge failed after 5 attempt(s), the PR is still open: https://github.com/o/r/pull/1") ||
		!strings.Contains(notes, "cannot be cleanly created") {
		t.Errorf("the done note does not say the PR is open and why:\n%s", notes)
	}

	// Settled: no retry state is left behind for this task.
	if got := f.poller.merges["C-1"]; got != nil {
		t.Errorf("retry state kept after giving up: %+v", got)
	}
}

// TestAutoMergeDoesNotRetryWhatWaitingCannotFix: a conflict, a logged-out
// gh, or an error no provider diagnosed are reported on the first attempt.
func TestAutoMergeDoesNotRetryWhatWaitingCannotFix(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"conflicts", errMergeConflict},
		{"gh not authenticated", &vcs.CLIError{Binary: "gh", Reason: "not authenticated"}},
		{"not a merge failure", errors.New("branch is not up to date")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, c := newMergeFixture(t)
			f.vcs.mergeErr = tt.err

			if acted := c.poll(0); acted != 1 {
				t.Errorf("acted = %d, want 1", acted)
			}
			if got := len(f.vcs.mergedPRs()); got != 1 {
				t.Errorf("merge attempts = %d, want 1", got)
			}
			if got := f.queueStatus(t, "C-1"); got != model.StatusDone {
				t.Errorf("status = %q, want done", got)
			}
			if got, _ := f.lastEventExtra(t, EventPRAutoMergeFailed)["error"].(string); got != tt.err.Error() {
				t.Errorf("event error = %q, want %q", got, tt.err.Error())
			}
		})
	}
}

// TestAutoMergeRetriesAPlainMergeFailure: a provider that only says
// ErrMergeFailed (no diagnosis) is still asked again within the budget.
func TestAutoMergeRetriesAPlainMergeFailure(t *testing.T) {
	f, c := newMergeFixture(t)
	f.vcs.mergeErrs = []error{vcs.ErrMergeFailed, nil}

	c.poll(0)
	c.poll(mergeBackoff(1))
	if got := f.backend.eventTypes("C-1"); !equalStrings(got, []string{EventCISuccess, EventPRAutoMerged}) {
		t.Errorf("events = %v", got)
	}
}

// TestAutoMergeBranchUpdateDoesNotSpendAnAttempt: a PR brought up to date
// waits for its new CI run rather than burning the merge budget, and a base
// that keeps moving is still bounded.
func TestAutoMergeBranchUpdateDoesNotSpendAnAttempt(t *testing.T) {
	f, c := newMergeFixture(t)
	f.vcs.mergeErrs = []error{errMergeBehind, errMergeUnknown, errMergeUnknown, errMergeUnknown, errMergeUnknown, nil}

	c.poll(0)
	for i := 1; i <= mergeMaxAttempts; i++ {
		c.poll(time.Minute)
	}
	// One update plus four transient failures is still inside the budget:
	// the sixth call merges.
	if got := len(f.vcs.mergedPRs()); got != 6 {
		t.Errorf("merge attempts = %d, want 6", got)
	}
	if got := f.backend.eventTypes("C-1"); !equalStrings(got, []string{EventCISuccess, EventPRAutoMerged}) {
		t.Errorf("events = %v", got)
	}
}

func TestAutoMergeBranchUpdatesAreBounded(t *testing.T) {
	f, c := newMergeFixture(t)
	f.vcs.mergeErr = errMergeBehind

	for i := 0; i < 10; i++ {
		c.poll(time.Minute)
	}
	if got := len(f.vcs.mergedPRs()); got != mergeMaxBranchUpdates+1 {
		t.Errorf("merge attempts = %d, want %d", got, mergeMaxBranchUpdates+1)
	}
	if got := f.backend.eventTypes("C-1"); !equalStrings(got, []string{EventCISuccess, EventPRAutoMergeFailed}) {
		t.Errorf("events = %v", got)
	}
}

func TestMergeBackoff(t *testing.T) {
	tests := []struct {
		failed int
		want   time.Duration
	}{
		{0, 15 * time.Second},
		{1, 15 * time.Second},
		{2, 30 * time.Second},
		{3, 45 * time.Second},
		{4, time.Minute},
		{9, time.Minute},
	}
	for _, tt := range tests {
		if got := mergeBackoff(tt.failed); got != tt.want {
			t.Errorf("mergeBackoff(%d) = %v, want %v", tt.failed, got, tt.want)
		}
	}
}
