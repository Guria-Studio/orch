package cli

import (
	"os/exec"
	"strings"
	"testing"
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
}
