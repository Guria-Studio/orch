package engine

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
	"github.com/Guria-Studio/orch/internal/vcs"
)

// memQueue is an in-memory MergeQueueStore.
type memQueue struct {
	mu      sync.Mutex
	entries []state.QueueEntry
}

func (q *memQueue) Enqueue(_ context.Context, id, pr string, at time.Time) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, e := range q.entries {
		if e.TaskID == id {
			if e.State == state.QueueQueued {
				return false, nil
			}
			q.entries[i] = state.QueueEntry{TaskID: id, PRURL: pr, EnqueuedAt: at, State: state.QueueQueued, Requeues: e.Requeues}
			return true, nil
		}
	}
	q.entries = append(q.entries, state.QueueEntry{TaskID: id, PRURL: pr, EnqueuedAt: at, State: state.QueueQueued})
	return true, nil
}

func (q *memQueue) SetQueueState(_ context.Context, id, st, reason string, _ time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, e := range q.entries {
		if e.TaskID == id {
			q.entries[i].State, q.entries[i].Reason = st, reason
		}
	}
	return nil
}

func (q *memQueue) QueueEntries(context.Context) ([]state.QueueEntry, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]state.QueueEntry(nil), q.entries...), nil
}

func (q *memQueue) state(id string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range q.entries {
		if e.TaskID == id {
			return e.State
		}
	}
	return ""
}

// queueVCS is a provider that can say whether GitHub's merge queue is on.
type queueVCS struct {
	*fakeVCS
	active bool
}

func (v queueVCS) MergeQueueActive(string, string) (bool, error) { return v.active, nil }

func prURL(id string) string { return "https://github.com/o/r/pull/" + id }

// queueFixture is a run with the given tasks, every one with a green PR
// waiting on CI, auto-merge and the merge queue on.
func queueFixture(t *testing.T, tasks []model.Task, green ...string) (*ciFixture, *memQueue) {
	t.Helper()
	f := newCIFixture(t, pendingRow(green[0], prURL(green[0]), 0))
	if _, err := f.s.Queue.Reload(tasks); err != nil {
		t.Fatal(err)
	}
	f.ci.rows = nil
	for _, id := range green {
		f.ci.rows = append(f.ci.rows, pendingRow(id, prURL(id), 0))
		f.vcs.status[prURL(id)] = vcs.CISuccess
	}
	q := &memQueue{}
	f.poller.AutoMerge, f.poller.MergeQueue, f.poller.Reverify, f.poller.Queue = true, true, "when_base_moved", q
	return f, q
}

// poll runs one CI sweep, as if the interval had passed.
func (f *ciFixture) poll(t *testing.T) {
	t.Helper()
	f.poller.lastPoll = time.Time{}
	if _, err := f.poller.Poll(context.Background(), f.s); err != nil {
		t.Fatalf("Poll: %v", err)
	}
}

// TestMergeQueueMergesInOrder (#522 §6.4): three green PRs are merged one
// per poll, by phase, then by how many tasks depend on each, whatever order
// CI went green in.
func TestMergeQueueMergesInOrder(t *testing.T) {
	tasks := []model.Task{
		task("Q-1", 2, "claude/opus"),
		task("Q-2", 1, "claude/opus"),
		task("Q-3", 1, "claude/opus"),
		task("Q-4", 3, "claude/opus", "Q-3"),
	}
	f, q := queueFixture(t, tasks, "Q-1", "Q-2", "Q-3")

	f.poll(t)
	if got := f.vcs.mergedPRs(); !equalStrings(got, []string{prURL("Q-3")}) {
		t.Fatalf("first poll merged %v, want Q-3 alone (phase 1, one dependent)", got)
	}
	f.poll(t)
	f.poll(t)
	want := []string{prURL("Q-3"), prURL("Q-2"), prURL("Q-1")}
	if got := f.vcs.mergedPRs(); !equalStrings(got, want) {
		t.Errorf("merged %v, want %v", got, want)
	}
	for _, id := range []string{"Q-1", "Q-2", "Q-3"} {
		if q.state(id) != state.QueueMerged {
			t.Errorf("%s queue state = %q, want merged", id, q.state(id))
		}
		if got := f.backend.eventTypes(id); !containsString(got, EventQueueEnqueued) || !containsString(got, EventQueueMerged) {
			t.Errorf("%s events = %v, want enqueued and merged", id, got)
		}
	}
}

// TestMergeQueueReverifiesAMovedBase: the head's base moved since its branch
// was verified. The base is merged in and the local verify runs again on the
// result without the LLM review; a pass is pushed (CI runs again, nothing is
// merged yet), a failure goes back to the agent and out of the queue.
func TestMergeQueueReverifiesAMovedBase(t *testing.T) {
	for _, tt := range []struct {
		name       string
		cmd        string
		wantPushed bool
		wantState  string
	}{
		{"a passing verify is pushed, not merged", "true", true, state.QueueQueued},
		{"a failing verify goes back to the agent", "test ! -f other.txt", false, state.QueueEjected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, wm, remote := conflictFixture(t, false)
			f.vcs.status[conflictPR] = vcs.CISuccess
			q := &memQueue{}
			f.poller.AutoMerge, f.poller.MergeQueue, f.poller.Reverify, f.poller.Queue = true, true, "when_base_moved", q
			v := &cmdVerifier{cmd: tt.cmd}
			f.s.Verifier = v
			f.poller.spawn = func(run func()) { run() }

			f.poll(t)

			if reqs := v.requests(); len(reqs) != 1 || !reqs[0].SkipReview {
				t.Errorf("verify requests = %+v, want one re-verify without the review", reqs)
			}
			if got := f.vcs.mergedPRs(); len(got) != 0 {
				t.Errorf("merged %v before the merged base was verified and its CI ran", got)
			}
			// #nosec G204 -- fixed git subcommand on a temp repo this test made.
			_, err := exec.Command("git", "-C", remote, "cat-file", "-e", wm.BranchName("C-2")+":other.txt").CombinedOutput()
			if pushed := err == nil; pushed != tt.wantPushed {
				t.Errorf("pushed the base merged in = %v, want %v", pushed, tt.wantPushed)
			}
			if got := q.state("C-2"); got != tt.wantState {
				t.Errorf("queue state = %q, want %q", got, tt.wantState)
			}
			if !tt.wantPushed {
				if len(f.s.retryQueueIDs()) != 1 {
					t.Error("the task was not sent back to its agent")
				}
				return
			}
			// CI passes on the pushed head: the branch now holds the base, so
			// the next turn merges it without verifying again.
			f.poll(t)
			if got := f.vcs.mergedPRs(); !equalStrings(got, []string{conflictPR}) {
				t.Errorf("merged %v after the re-verified head went green, want it", got)
			}
			if n := len(v.requests()); n != 1 {
				t.Errorf("verified %d times, want once (the base did not move again)", n)
			}
		})
	}
}

// TestMergeQueueSurvivesARestart: the queue lives in orch.db (migration 017).
// A new run finds the head it left mid-way — its CI running again after the
// push — and still merges nothing behind it until the head is through.
func TestMergeQueueSurvivesARestart(t *testing.T) {
	store, ok := openBackend(t).(MergeQueueStore)
	if !ok {
		t.Fatal("state.SQLite is not a MergeQueueStore")
	}
	tasks := []model.Task{task("Q-1", 1, "claude/opus"), task("Q-2", 1, "claude/opus")}
	ctx := context.Background()
	if _, err := store.Enqueue(ctx, "Q-1", prURL("Q-1"), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	// The restarted run: Q-1's CI is running again, Q-2 is green.
	f, _ := queueFixture(t, tasks, "Q-1", "Q-2")
	f.poller.Queue = store
	f.vcs.status[prURL("Q-1")] = vcs.CIPending
	f.poll(t)
	if got := f.vcs.mergedPRs(); len(got) != 0 {
		t.Fatalf("merged %v while the head from the last run waits on CI", got)
	}
	f.vcs.status[prURL("Q-1")] = vcs.CISuccess
	f.poll(t)
	f.poll(t)
	if got, want := f.vcs.mergedPRs(), []string{prURL("Q-1"), prURL("Q-2")}; !equalStrings(got, want) {
		t.Errorf("merged %v, want %v", got, want)
	}
	entries, err := store.QueueEntries(ctx)
	if err != nil || len(entries) != 2 || entries[0].State != state.QueueMerged || entries[1].State != state.QueueMerged {
		t.Errorf("queue = %+v, %v; want both merged", entries, err)
	}
}

// TestMergeQueueDelegatesToGitHub (#522 decision 3): a base with GitHub's own
// merge queue on gets the PR handed to it at once (gh pr merge --auto), with
// no orch queue.
func TestMergeQueueDelegatesToGitHub(t *testing.T) {
	f, q := queueFixture(t, []model.Task{task("Q-1", 2, "claude/opus"), task("Q-2", 1, "claude/opus")}, "Q-1", "Q-2")
	f.poller.Provider = queueVCS{fakeVCS: f.vcs, active: true}
	f.poll(t)
	if got := f.vcs.mergedPRs(); len(got) != 2 {
		t.Errorf("merged %v, want both handed to GitHub in one poll", got)
	}
	if entries, _ := q.QueueEntries(context.Background()); len(entries) != 0 {
		t.Errorf("orch queued %+v with GitHub's queue on", entries)
	}
}

// TestMergeQueueOffMergesAtOnce: without merge_queue every green PR merges in
// the poll that sees it, as before.
func TestMergeQueueOffMergesAtOnce(t *testing.T) {
	f, _ := queueFixture(t, []model.Task{task("Q-1", 2, "claude/opus"), task("Q-2", 1, "claude/opus")}, "Q-1", "Q-2")
	f.poller.MergeQueue = false
	f.poll(t)
	if got := f.vcs.mergedPRs(); len(got) != 2 {
		t.Errorf("merged %v, want both at once", got)
	}
}

func TestTransitiveDependents(t *testing.T) {
	got := project.TransitiveDependents([]model.Task{
		task("A", 1, "m"), task("B", 1, "m", "A"), task("C", 1, "m", "B"), task("D", 1, "m", "A", "B"),
	})
	want := map[string]int{"A": 3, "B": 2, "C": 0, "D": 0}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("dependents of %s = %d, want %d", id, got[id], n)
		}
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
