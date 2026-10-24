package worktree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLandLocally(t *testing.T) {
	root := newTestRepo(t)
	m := NewManager(root, false)
	work := func(id, file, body string) {
		t.Helper()
		wt, err := m.Create(id, "main")
		if err != nil {
			t.Fatal(err)
		}
		if file == "" {
			return
		}
		if err := os.WriteFile(filepath.Join(wt, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := m.CommitPending(id, id+" work"); err != nil {
			t.Fatal(err)
		}
	}
	commitOnMain := func(file, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "on main: "+file)
	}
	head := func(ref string) string { return strings.TrimSpace(runGit(t, root, "rev-parse", ref)) }

	// The base moved while T1 worked: its squash goes on top, with T1's
	// subject, and the checkout of main is brought along.
	work("T1", "a.txt", "one\n")
	commitOnMain("other.txt", "x\n")
	before := head("main")
	got, err := m.LandLocally("T1", "main", "first\n\nTask: T1")
	if err != nil || got.Commit == "" || len(got.Conflicts) != 0 {
		t.Fatalf("LandLocally(T1) = %+v, %v; want a commit, no conflicts", got, err)
	}
	if head("main") != got.Commit || head("main^") != before {
		t.Errorf("main = %s (parent %s), want the squash %s on %s", head("main"), head("main^"), got.Commit, before)
	}
	if s := strings.TrimSpace(runGit(t, root, "log", "-1", "--format=%s", "main")); s != "first" {
		t.Errorf("subject = %q, want first", s)
	}
	if b, err := os.ReadFile(filepath.Join(root, "a.txt")); err != nil || string(b) != "one\n" { // #nosec G304 -- a file in the test repo
		t.Errorf("a.txt in the checkout = %q, %v; want the task's work", b, err)
	}
	if branchExists(t, root, "orch/T1") || m.Exists("T1") {
		t.Error("orch/T1 or its worktree is still there after landing")
	}

	// A conflict changes nothing and keeps the branch for the retry.
	work("T2", "README.md", "from T2\n")
	commitOnMain("README.md", "from main\n")
	before = head("main")
	got, err = m.LandLocally("T2", "main", "second")
	if err != nil || got.Commit != "" || len(got.Conflicts) != 1 || got.Conflicts[0] != "README.md" {
		t.Fatalf("LandLocally(T2) = %+v, %v; want the conflict on README.md", got, err)
	}
	if len(got.Commits) != 1 || !strings.HasSuffix(got.Commits[0], "on main: README.md") {
		t.Errorf("commits = %v, want main's README commit", got.Commits)
	}
	if head("main") != before || !branchExists(t, root, "orch/T2") || !m.Exists("T2") {
		t.Error("a conflicting land moved main or dropped the task's branch")
	}

	// Nothing to land: main stays, the empty branch goes.
	work("T3", "", "")
	if got, err := m.LandLocally("T3", "main", "third"); err != nil || got.Commit != "" || len(got.Conflicts) != 0 {
		t.Fatalf("LandLocally(T3) = %+v, %v; want nothing", got, err)
	}
	if head("main") != before || branchExists(t, root, "orch/T3") {
		t.Error("an empty task moved main or kept its branch")
	}

	// main not checked out anywhere: the ref moves, the checkout stays put.
	runGit(t, root, "checkout", "-q", "-b", "side")
	work("T4", "d.txt", "four\n")
	got, err = m.LandLocally("T4", "main", "fourth")
	if err != nil || got.Commit == "" || head("main") != got.Commit {
		t.Fatalf("LandLocally(T4) = %+v, %v; main = %s", got, err, head("main"))
	}
	if b := strings.TrimSpace(runGit(t, root, "symbolic-ref", "--short", "HEAD")); b != "side" {
		t.Errorf("checkout on %s, want side", b)
	}
}
