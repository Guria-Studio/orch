package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/worktree"
)

// cmdVerifier runs a shell command in the worktree it is asked to verify, the
// way verify.commands does: a non-zero exit is the failure, its output the
// reason. before runs ahead of each call, numbered from 1, so a test can move
// the base while a verify is under way.
type cmdVerifier struct {
	cmd    string
	before func(call int)

	mu   sync.Mutex
	reqs []VerifyRequest
}

func (v *cmdVerifier) Verify(ctx context.Context, req VerifyRequest) VerifyResult {
	v.mu.Lock()
	v.reqs = append(v.reqs, req)
	call := len(v.reqs)
	v.mu.Unlock()
	if v.before != nil {
		v.before(call)
	}
	// #nosec G204 -- the test's own fixed command.
	cmd := exec.CommandContext(ctx, "sh", "-c", v.cmd)
	cmd.Dir = req.Dir
	out, err := cmd.CombinedOutput()
	res := VerifyResult{Commands: []VerifyCommand{{Command: v.cmd}}, Reviewer: "gemini"}
	if req.SkipReview {
		res.Reviewer = ""
	}
	if err != nil {
		res.Failure = "`" + v.cmd + "` failed:\n" + string(out)
	}
	return res
}

func (v *cmdVerifier) requests() []VerifyRequest {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]VerifyRequest(nil), v.reqs...)
}

// landFixture is a local-merge run (vcs.provider: local) over a real
// repository whose main holds config.txt = port=80. C-1's agent adds
// feature.txt and its test, check.sh, which needs port=80. moveBase commits
// port=81 on main: a change git merges cleanly that breaks C-1's test.
func landFixture(t *testing.T, v Verifier, agentMovesBase bool) (f *reapFixture, root string, moveBase func()) {
	t.Helper()
	f = newReapFixture(t,
		[]model.Task{task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": okResponse()},
		SchedulerOptions{WorktreeMode: true, LocalMerge: true, BaseBranch: "main", ConflictRetries: 1})
	root = f.s.Opts.Cwd
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, filepath.Join(root, "config.txt"), "port=80\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "initial")
	f.s.Worktree = NewWorktreeManager(worktree.NewManager(root, false))
	f.s.Verifier = v
	f.s.Opts.Cfg.Verify.MaxAttempts = 2

	agent := "printf 'grep -q port=80 config.txt\\n' > check.sh && printf 'new\\n' > feature.txt\n"
	if agentMovesBase {
		// A sibling lands on main while this agent works.
		agent += "printf 'port=81\\n' > '" + root + "/config.txt' && " +
			"git -C '" + root + "' -c user.email=t@t.test -c user.name=t commit -qam 'C-0: move the port (#1)'\n"
	}
	write(t, filepath.Join(f.fakeDir, "C-1.sh"), agent)
	moveBase = func() {
		write(t, filepath.Join(root, "config.txt"), "port=81\n")
		gitRun(t, root, "commit", "-qam", "C-0: move the port (#1)")
	}
	return f, root, moveBase
}

// onMain reports whether path is in main's tree.
func onMain(t *testing.T, root, path string) bool {
	t.Helper()
	// #nosec G204 -- fixed git subcommand on a temp repo this test made.
	return exec.Command("git", "-C", root, "cat-file", "-e", "main:"+path).Run() == nil
}

// TestLocalLandVerifiesAgainstTheMovedBase (#522): the base moved while the
// agent worked, cleanly as far as git can tell, with a change that breaks the
// task's test. The work used to be verified against the base it started from
// and landed, breaking main; now the base is merged into the worktree first,
// the verify sees the combination, and the task goes back to its agent.
func TestLocalLandVerifiesAgainstTheMovedBase(t *testing.T) {
	v := &cmdVerifier{cmd: "sh check.sh"}
	f, root, _ := landFixture(t, v, true)

	f.runOnce(t)

	if onMain(t, root, "feature.txt") {
		t.Fatal("work that fails against the current base landed on main")
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusTodo {
		t.Errorf("status = %q, want todo: sent back to its agent", got)
	}
	reqs := v.requests()
	if len(reqs) != 1 || reqs[0].SkipReview {
		t.Fatalf("verify requests = %+v, want one, with the review", reqs)
	}
	feedback, err := os.ReadFile(filepath.Join(reqs[0].Dir, CIFeedbackFile)) // #nosec G304 -- a temp dir this test made
	if err != nil || !strings.Contains(string(feedback), "sh check.sh") {
		t.Errorf("feedback = %q, %v; want the failing check", feedback, err)
	}
	if got := f.backend.eventTypes("C-1"); !contains(got, EventVerifyFailed) {
		t.Errorf("events = %v, want a verify_failed", got)
	}
}

// TestLocalLandReverifiesWhenTheBaseMovesDuringTheVerify (#522): the base
// moves after the verify started — another task landed meanwhile. The passed
// verify no longer covers what would land, so the base is merged in and the
// checks run again, without the LLM review (decision 5), before anything
// lands.
func TestLocalLandReverifiesWhenTheBaseMovesDuringTheVerify(t *testing.T) {
	v := &cmdVerifier{cmd: "sh check.sh"}
	var moveBase func()
	v.before = func(call int) {
		if call == 1 {
			moveBase()
		}
	}
	f, root, mb := landFixture(t, v, false)
	moveBase = mb

	f.runOnce(t)

	if onMain(t, root, "feature.txt") {
		t.Fatal("work verified against a base that has since moved landed on main")
	}
	reqs := v.requests()
	if len(reqs) != 2 || reqs[0].SkipReview || !reqs[1].SkipReview {
		t.Fatalf("verify requests = %+v, want the verify and a re-verify without the review", reqs)
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusTodo {
		t.Errorf("status = %q, want todo: sent back to its agent", got)
	}
	if extra := f.backend.eventExtra("C-1", EventVerifyFailed); extra == nil || extra["base_moved"] != true {
		t.Errorf("verify_failed extra = %v, want base_moved", extra)
	}
}

// TestLocalLandVerifiesOnceWhenTheBaseStays: with the base where the task
// started, the verify that passed covers what lands — no second run.
func TestLocalLandVerifiesOnceWhenTheBaseStays(t *testing.T) {
	v := &cmdVerifier{cmd: "sh check.sh"}
	f, root, _ := landFixture(t, v, false)

	f.runOnce(t)

	if !onMain(t, root, "feature.txt") {
		t.Fatal("verified work did not land")
	}
	if got := len(v.requests()); got != 1 {
		t.Errorf("verified %d times, want once", got)
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusDone {
		t.Errorf("status = %q, want done", got)
	}
}

// TestLocalLandReverifyPassesAndLands: a base that moved without breaking
// anything is verified with the work and lands; the passed event keeps the
// reviewer of the attempt's own review.
func TestLocalLandReverifyPassesAndLands(t *testing.T) {
	v := &cmdVerifier{cmd: "test -f feature.txt"}
	var moveBase func()
	v.before = func(call int) {
		if call == 1 {
			moveBase()
		}
	}
	f, root, mb := landFixture(t, v, false)
	moveBase = mb

	f.runOnce(t)

	if !onMain(t, root, "feature.txt") {
		t.Fatal("work that passed the re-verify did not land")
	}
	if got := len(v.requests()); got != 2 {
		t.Errorf("verified %d times, want the verify and the re-verify", got)
	}
	if extra := f.backend.eventExtra("C-1", EventVerifyPassed); extra == nil || extra["reviewer"] != "gemini" {
		t.Errorf("verify_passed extra = %v, want the attempt's reviewer kept", extra)
	}
}
