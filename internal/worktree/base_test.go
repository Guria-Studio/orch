package worktree

import (
	"os/exec"
	"testing"
)

// #376: a repository with no remote has nowhere to push; orch run keeps the
// work on local branches instead of blocking every task on a push.
func TestHasRemote(t *testing.T) {
	dir := t.TempDir()
	if HasRemote(dir) {
		t.Error("a directory that is not a repository has a remote")
	}
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil { // #nosec G204 -- a test driving git on its own temp dir
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if HasRemote(dir) {
		t.Error("a fresh repository has a remote")
	}
	git("remote", "add", "origin", "https://example.com/x.git")
	if !HasRemote(dir) {
		t.Error("a repository with origin has no remote")
	}
}
