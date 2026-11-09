package engine

import (
	"strings"
	"sync"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/vcs"
	"github.com/Guria-Studio/orch/internal/worktree"
)

const testIssueURL = "https://github.com/acme/shop/issues/12"

// recordingIssues stands in for gh on the issue side.
type recordingIssues struct {
	mu     sync.Mutex
	closed map[string]string // url -> comment
	err    error
}

func (r *recordingIssues) CloseIssue(url, comment string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed == nil {
		r.closed = map[string]string{}
	}
	r.closed[url] = comment
	return r.err
}

func (r *recordingIssues) get(url string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.closed[url]
	return c, ok
}

func issueTask(id string, status model.Status) model.Task {
	t := task(id, 0, "claude/opus")
	t.Status = status
	t.SourceIssue = testIssueURL
	return t
}

// A task imported from an issue opens a PR whose body closes the issue when
// it is the issue's last unfinished task, and only refers to it while another
// task of the same issue is not done (#525).
func TestPRBodyClosesTheSourceIssue(t *testing.T) {
	tests := []struct {
		name  string
		tasks []model.Task
		want  string
		not   string
	}{
		{"the issue's only task closes it",
			[]model.Task{issueTask("F0.12.T1", model.StatusTodo)},
			"Closes acme/shop#12", "Refs"},
		{"a sibling not done yet: refer only",
			[]model.Task{issueTask("F0.12.T1", model.StatusTodo), issueTask("F0.12.T2", model.StatusBacklog)},
			"Refs acme/shop#12 — 1 other task(s) of this issue are not done yet (F0.12.T2)", "Closes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReapFixture(t, tt.tasks,
				map[string]fakeResponse{"F0.12.T1": okResponse()},
				SchedulerOptions{AutoPR: true, WorktreeMode: true, BaseBranch: "main"})
			f.s.Worktree = &recordingWorktree{dir: t.TempDir(), changed: []string{"a.go"}}
			fv := &fakeVCS{status: map[string]vcs.CIState{}, createdPR: "https://github.com/acme/shop/pull/30"}
			f.s.VCS = fv
			f.s.CIRecorder = &recordingPR{}

			f.runOnce(t)

			if fv.created != 1 {
				t.Fatalf("PRs opened = %d, want 1", fv.created)
			}
			if !strings.Contains(fv.prBody, tt.want) || strings.Contains(fv.prBody, tt.not) {
				t.Errorf("PR body = %q, want %q and no %q", fv.prBody, tt.want, tt.not)
			}
		})
	}
}

// With vcs.provider local there is no PR to carry the keyword, so the last
// task of an issue to land closes it through gh, with the evidence in the
// comment; one that still has an unfinished sibling leaves it open.
func TestLocalMergeClosesTheSourceIssue(t *testing.T) {
	tests := []struct {
		name      string
		tasks     []model.Task
		wantClose bool
	}{
		{"the last task closes the issue",
			[]model.Task{issueTask("F0.12.T1", model.StatusTodo), issueTask("F0.12.T0", model.StatusDone)}, true},
		{"a sibling not done leaves it open",
			[]model.Task{issueTask("F0.12.T1", model.StatusTodo), issueTask("F0.12.T2", model.StatusBacklog)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReapFixture(t, tt.tasks,
				map[string]fakeResponse{"F0.12.T1": okResponse()},
				SchedulerOptions{WorktreeMode: true, LocalMerge: true, BaseBranch: "main"})
			f.s.Worktree = &recordingWorktree{dir: t.TempDir(), land: worktree.Landing{Commit: "abc1234"}}
			issues := &recordingIssues{}
			f.s.Issues = issues

			f.runOnce(t)

			if got, _ := f.s.Queue.Status("F0.12.T1"); got != model.StatusDone {
				t.Fatalf("status = %q, want done", got)
			}
			comment, closed := issues.get(testIssueURL)
			if closed != tt.wantClose {
				t.Fatalf("closed = %v, want %v", closed, tt.wantClose)
			}
			if !closed {
				return
			}
			for _, want := range []string{"F0.12.T0", "F0.12.T1", "abc1234", "main"} {
				if !strings.Contains(comment, want) {
					t.Errorf("comment = %q, want it to name %q", comment, want)
				}
			}
			if got := f.backend.eventTypes("F0.12.T1"); got[len(got)-1] != EventIssueClosed {
				t.Errorf("events = %v, want %s last", got, EventIssueClosed)
			}
		})
	}
}

// A close gh refuses does not undo the landing: the work is on the base, the
// task is done, and the failure is an event the operator can read.
func TestLocalMergeIssueCloseFailureKeepsTheTaskDone(t *testing.T) {
	f := newReapFixture(t, []model.Task{issueTask("F0.12.T1", model.StatusTodo)},
		map[string]fakeResponse{"F0.12.T1": okResponse()},
		SchedulerOptions{WorktreeMode: true, LocalMerge: true, BaseBranch: "main"})
	f.s.Worktree = &recordingWorktree{dir: t.TempDir(), land: worktree.Landing{Commit: "abc1234"}}
	f.s.Issues = &recordingIssues{err: &vcs.CLIError{Binary: "gh", Reason: "not authenticated"}}

	f.runOnce(t)

	if got, _ := f.s.Queue.Status("F0.12.T1"); got != model.StatusDone {
		t.Fatalf("status = %q, want done", got)
	}
	if got := f.backend.eventTypes("F0.12.T1"); got[len(got)-1] != EventIssueCloseFailed {
		t.Errorf("events = %v, want %s last", got, EventIssueCloseFailed)
	}
}

// A task with no source issue never reaches gh.
func TestLocalMergeWithoutAnIssueClosesNothing(t *testing.T) {
	f := newReapFixture(t, []model.Task{task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": okResponse()},
		SchedulerOptions{WorktreeMode: true, LocalMerge: true})
	f.s.Worktree = &recordingWorktree{dir: t.TempDir(), land: worktree.Landing{Commit: "abc"}}
	issues := &recordingIssues{}
	f.s.Issues = issues

	f.runOnce(t)

	if len(issues.closed) != 0 {
		t.Errorf("closed %v, want nothing", issues.closed)
	}
}
