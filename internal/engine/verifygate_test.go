package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/worktree"
)

// scriptedVerifier answers each call with the next failure in its script,
// "" for a pass, and repeats the last answer once the script runs out. It
// records the worktree of every call.
type scriptedVerifier struct {
	mu      sync.Mutex
	answers []string
	dirs    []string
}

func (v *scriptedVerifier) Verify(_ context.Context, _ string, _ model.Backend, dir string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.dirs = append(v.dirs, dir)
	i := min(len(v.dirs), len(v.answers)) - 1
	return v.answers[i]
}

// verifyFixture is a reap fixture over a real repository with a bare
// origin, in worktree mode, with v as the verifier.
func verifyFixture(t *testing.T, v Verifier, maxAttempts int) (*reapFixture, string) {
	t.Helper()
	// A newline after the output, as a real CLI prints: every attempt of a
	// run appends to the same log, and the retry's line must parse.
	ok := okResponse()
	ok.out += "\n"
	f := newReapFixture(t,
		[]model.Task{task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": ok},
		SchedulerOptions{WorktreeMode: true, BaseBranch: "main"})
	root := f.s.Opts.Cwd
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "-q", "--bare", remote)
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, filepath.Join(root, "README.md"), "initial\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "initial")
	gitRun(t, root, "remote", "add", "origin", remote)
	gitRun(t, root, "push", "-q", "origin", "main")
	f.s.Worktree = NewWorktreeManager(worktree.NewManager(root, true))
	f.s.Verifier = v
	f.s.Opts.Cfg.Verify.MaxAttempts = maxAttempts
	return f, remote
}

func remoteBranchFiles(t *testing.T, remote, branch string) (string, bool) {
	t.Helper()
	// #nosec G204 -- fixed git subcommand on a temp repo this test made.
	out, err := exec.Command("git", "-C", remote, "ls-tree", "--name-only", branch).CombinedOutput()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// TestVerifyFailureSendsTheTaskBackToItsWorktree: work the local verify
// fails is not pushed; the task goes back to its agent in the same worktree
// with the failure to read, and the work is pushed once it passes.
func TestVerifyFailureSendsTheTaskBackToItsWorktree(t *testing.T) {
	v := &scriptedVerifier{answers: []string{"`go test ./...` failed:\nFAIL TestLogin", ""}}
	f, remote := verifyFixture(t, v, 2)
	ctx := context.Background()

	f.runOnce(t)

	if _, pushed := remoteBranchFiles(t, remote, "orch/C-1"); pushed {
		t.Error("work that failed the verify was pushed")
	}
	if got := f.backend.eventTypes("C-1"); !contains(got, EventVerifyFailed) {
		t.Errorf("events = %v, want a verify_failed", got)
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusTodo {
		t.Errorf("status = %q, want todo for the send-back", got)
	}
	wt := v.dirs[0]
	feedback, err := os.ReadFile(filepath.Join(wt, CIFeedbackFile)) // #nosec G304 -- a temp dir this test made
	if err != nil {
		t.Fatalf("the kept worktree has no feedback file: %v", err)
	}
	if !strings.Contains(string(feedback), "FAIL TestLogin") {
		t.Errorf("feedback = %q, want the failure", feedback)
	}

	if _, err := f.s.Refill(ctx); err != nil {
		t.Fatalf("Refill: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(f.s.Opts.StateDir, "prompts", f.s.Opts.RunID, "C-1.txt")) // #nosec G304 -- a temp dir this test made
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Working dir: "+wt+"\n") || !strings.Contains(string(body), "READ FIRST "+CIFeedbackFile) {
		t.Errorf("the retry prompt does not send the agent back to %s with the feedback:\n%s", wt, body)
	}
	if _, err := f.s.DrainWait(ctx, 30*time.Second); err != nil {
		t.Fatalf("DrainWait: %v", err)
	}

	if len(v.dirs) != 2 || v.dirs[1] != wt {
		t.Errorf("verified in %v, want the same worktree twice", v.dirs)
	}
	files, pushed := remoteBranchFiles(t, remote, "orch/C-1")
	if !pushed {
		t.Fatal("work that passed the verify was not pushed")
	}
	if strings.Contains(files, CIFeedbackFile) {
		t.Errorf("the pushed branch carries the feedback file: %q", files)
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusDone {
		t.Errorf("status = %q, want done", got)
	}
}

// TestVerifyBlocksOnceTheSendBacksAreUsedUp: an agent that cannot pass is
// blocked with the failure, and nothing is pushed.
func TestVerifyBlocksOnceTheSendBacksAreUsedUp(t *testing.T) {
	v := &scriptedVerifier{answers: []string{"`make test` failed:\nboom"}}
	f, remote := verifyFixture(t, v, 1)

	f.runOnce(t)
	f.runOnce(t)

	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusBlocked {
		t.Fatalf("status = %q, want blocked", got)
	}
	if len(v.dirs) != 2 {
		t.Errorf("verified %d times, want the attempt and its one send-back", len(v.dirs))
	}
	if _, pushed := remoteBranchFiles(t, remote, "orch/C-1"); pushed {
		t.Error("work that never passed the verify was pushed")
	}
	reason, _ := f.backend.eventExtra("C-1", EventFail)["reason"].(string)
	if !strings.Contains(reason, "local verify still failing") || !strings.Contains(reason, "`make test` failed") {
		t.Errorf("block reason = %q, want the verify failure", reason)
	}
}

// TestNoVerifierPushesAsBefore: without verify.enabled a clean exit is
// pushed straight away, as it always was.
func TestNoVerifierPushesAsBefore(t *testing.T) {
	f, remote := verifyFixture(t, nil, 0)
	f.s.Verifier = nil

	f.runOnce(t)

	if _, pushed := remoteBranchFiles(t, remote, "orch/C-1"); !pushed {
		t.Error("a clean exit was not pushed")
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusDone {
		t.Errorf("status = %q, want done", got)
	}
}
