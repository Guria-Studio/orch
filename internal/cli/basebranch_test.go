package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/doctor"
)

// #376: what keeps a task from branching off the base, said with the command
// that fixes it, and nothing once the base has a commit.
func TestBaseProblem(t *testing.T) {
	dir := t.TempDir()
	if got := baseProblem(dir, "trunk"); !strings.Contains(got, "not a git repository yet: git init") {
		t.Errorf("no repository: %q", got)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...) // #nosec G204 -- a test driving git on its own temp dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "trunk")
	if got := repoBranch(dir); got != "trunk" {
		t.Errorf("repoBranch = %q, want trunk before any commit", got)
	}
	if got := baseProblem(dir, "trunk"); !strings.Contains(got, "trunk, which has no commit yet") {
		t.Errorf("no commit: %q", got)
	}
	git("commit", "-q", "--allow-empty", "-m", "init")
	if got := baseProblem(dir, "trunk"); got != "" {
		t.Errorf("with a commit: %q, want none", got)
	}
	if got := baseProblem(dir, "main"); !strings.Contains(got, "main, which has no commit yet") {
		t.Errorf("a base the repository does not have: %q", got)
	}
	if c := checkRunBase(dir, "trunk", true); c.Status != doctor.StatusOK {
		t.Errorf("doctor with a commit on the base: %+v, want ok", c)
	}
}

// #392: doctor's verdict cannot say ready while `orch run` would refuse to
// start — worktree mode on and nothing to branch from is an error there.
func TestCheckRunBase(t *testing.T) {
	dir := t.TempDir()
	if c := checkRunBase(dir, "main", true); c.Status != doctor.StatusError || !strings.Contains(c.Detail, "git init") {
		t.Errorf("not a repository, worktree mode: %+v, want an error naming git init", c)
	}
	if c := checkRunBase(dir, "main", false); c.Status != doctor.StatusSkip {
		t.Errorf("worktree mode off: %+v, want skip", c)
	}
}
