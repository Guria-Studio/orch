package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/vcs"
	"github.com/hectorcanaimero/orch/internal/worktree"
)

const conflictPR = "https://github.com/o/r/pull/2"

// conflictFixture is a real repo with a remote: C-2's work pushed to its PR
// branch and tidied away, as the reaper leaves it, and then a sibling's change
// squash-merged into main. sibling decides whether the sibling touched the
// same line as C-2 (a conflict) or another file (a clean merge).
func conflictFixture(t *testing.T, sameLine bool) (*ciFixture, WorktreeManager, string) {
	t.Helper()
	f := newCIFixture(t, pendingRow("C-2", conflictPR, 0))
	f.vcs.status[conflictPR] = vcs.CIConflict

	root := f.s.Opts.Cwd
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "-q", "--bare", remote)
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, filepath.Join(root, "shared.txt"), "original\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "initial")
	gitRun(t, root, "remote", "add", "origin", remote)
	gitRun(t, root, "push", "-q", "origin", "main")

	wm := NewWorktreeManager(worktree.NewManager(root, true))
	f.s.Worktree = wm
	f.s.Opts.WorktreeMode = true
	f.s.Opts.BaseBranch = "main"
	f.s.Opts.AutoPR = true
	f.s.VCS = f.vcs
	f.s.CIRecorder = &recordingPR{}
	f.poller.Worktree = wm
	f.poller.ConflictRetries = 1

	ctx := context.Background()
	wt, err := wm.Create(ctx, "C-2", "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt, "shared.txt"), "from C-2\n")
	if err := wm.CommitPending(ctx, "C-2"); err != nil {
		t.Fatal(err)
	}
	if err := wm.Push(ctx, "C-2"); err != nil {
		t.Fatal(err)
	}
	if err := wm.Remove(ctx, "C-2"); err != nil {
		t.Fatal(err)
	}

	// The sibling C-1 merged first.
	if sameLine {
		write(t, filepath.Join(root, "shared.txt"), "from C-1\n")
	} else {
		write(t, filepath.Join(root, "other.txt"), "from C-1\n")
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "C-1: edit (#1)")
	gitRun(t, root, "push", "-q", "origin", "main")
	return f, wm, remote
}

// remoteFile is path's content on the remote's branch.
func remoteFile(t *testing.T, remote, branch, path string) string {
	t.Helper()
	// #nosec G204 -- fixed git subcommand on a temp repo this test made.
	out, err := exec.Command("git", "-C", remote, "show", branch+":"+path).CombinedOutput()
	if err != nil {
		t.Fatalf("git show %s:%s: %v\n%s", branch, path, err, out)
	}
	return string(out)
}

// TestConflictRetryResolvesThroughTheAgent (#438): two tasks edit the same
// line, the first merges, and the second's PR conflicts. It used to be
// blocked; now the base is merged into its kept work, the agent is told which
// files and which base commits, its resolution is pushed to the same PR, and
// the task ends done once CI passes.
func TestConflictRetryResolvesThroughTheAgent(t *testing.T) {
	f, wm, remote := conflictFixture(t, true)
	ctx := context.Background()

	if _, err := f.poller.Poll(ctx, f.s); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got, _ := f.s.Queue.Status("C-2"); got == model.StatusBlocked {
		t.Fatalf("a conflicting PR blocked the task instead of retrying it")
	}
	if _, err := f.s.Refill(ctx); err != nil {
		t.Fatalf("Refill: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(f.s.Opts.StateDir, "prompts", f.s.Opts.RunID, "C-2.txt")) // #nosec G304 -- a temp dir this test made
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"conflicts with its base", "- shared.txt", "C-1: edit (#1)"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the conflict retry's prompt lacks %q:\n%s", want, body)
		}
	}

	// The agent resolves the conflict in the worktree it was given.
	wt := filepath.Join(worktree.Dir(f.s.Opts.Cwd), "C-2")
	write(t, filepath.Join(wt, "shared.txt"), "from C-1\nfrom C-2\n")
	if _, err := f.s.DrainWait(ctx, 30*time.Second); err != nil {
		t.Fatalf("DrainWait: %v", err)
	}
	if got := remoteFile(t, remote, wm.BranchName("C-2"), "shared.txt"); got != "from C-1\nfrom C-2\n" {
		t.Errorf("the PR branch holds %q, want the resolution", got)
	}

	f.vcs.status[conflictPR] = vcs.CISuccess
	f.poller.lastPoll = time.Time{}
	if _, err := f.poller.Poll(ctx, f.s); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got, _ := f.s.Queue.Status("C-2"); got != model.StatusDone {
		t.Errorf("status = %q, want done after one conflict retry", got)
	}
	if extra := f.backend.eventExtra("C-2", EventCIConflictRetry); extra == nil || extra["agent"] != true {
		t.Errorf("no %s event sending the conflict to the agent: %v", EventCIConflictRetry, extra)
	}
}

// TestCleanConflictRetryPushesWithoutAnAgent: a base that merges cleanly into
// the branch is pushed and polled again; no agent is dispatched.
func TestCleanConflictRetryPushesWithoutAnAgent(t *testing.T) {
	f, wm, remote := conflictFixture(t, false)
	ctx := context.Background()

	if _, err := f.poller.Poll(ctx, f.s); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got := len(f.s.retryQueueIDs()); got != 0 {
		t.Errorf("queued %d agent retries for a clean merge", got)
	}
	if got, _ := f.s.Queue.Status("C-2"); got == model.StatusBlocked {
		t.Errorf("a cleanly mergeable PR blocked the task")
	}
	if got := remoteFile(t, remote, wm.BranchName("C-2"), "other.txt"); got != "from C-1\n" {
		t.Errorf("the PR branch was not brought up to date with the base: other.txt = %q", got)
	}
	if extra := f.backend.eventExtra("C-2", EventCIConflictRetry); extra == nil || extra["agent"] != false {
		t.Errorf("no %s event for the clean merge: %v", EventCIConflictRetry, extra)
	}
}

// TestConflictPastTheCapBlocksNamingTheFiles: with the retries used up the
// task is blocked as before, and the reason names the conflicting files.
func TestConflictPastTheCapBlocksNamingTheFiles(t *testing.T) {
	f, _, _ := conflictFixture(t, true)
	f.poller.ConflictRetries = 0

	if _, err := f.poller.Poll(context.Background(), f.s); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got, _ := f.s.Queue.Status("C-2"); got != model.StatusBlocked {
		t.Fatalf("status = %q, want blocked", got)
	}
	reason, _ := f.backend.eventExtra("C-2", EventCIBlocked)["reason"].(string)
	if !strings.Contains(reason, "conflicts with its base branch") || !strings.Contains(reason, "shared.txt") {
		t.Errorf("block reason = %q, want the conflict and its files", reason)
	}
}

// TestConflictRetryOffPushesNothing: retry.conflict.max_attempts 0 is the
// behaviour before #438, clean merges included: blocked, nothing pushed.
func TestConflictRetryOffPushesNothing(t *testing.T) {
	f, wm, remote := conflictFixture(t, false)
	f.poller.ConflictRetries = 0

	if _, err := f.poller.Poll(context.Background(), f.s); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got, _ := f.s.Queue.Status("C-2"); got != model.StatusBlocked {
		t.Errorf("status = %q, want blocked", got)
	}
	// #nosec G204 -- fixed git subcommand on a temp repo this test made.
	if out, err := exec.Command("git", "-C", remote, "show", wm.BranchName("C-2")+":other.txt").CombinedOutput(); err == nil {
		t.Errorf("pushed the base with conflict retries off: other.txt = %q", out)
	}
}
