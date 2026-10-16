package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHasAnchor(t *testing.T) {
	spec := "# F11 — Data\n\n## F11.1 — Package: loading\n\n### F11.1.T1 — Load the dataset\n\n### F11.1.T10: Clean it\n\n" +
		"```markdown\n### F11.1.T7 — An example, not a section\n```\n\n" +
		"## Acceptance criteria\n\n<a id=\"custom-anchor\"></a>\n## Notes {#notes-id}\r\n"
	tests := []struct {
		anchor string
		want   bool
	}{
		{"F11.1.T1", true},
		{"F11.1.T10", true},
		{"F11.1.T2", false},
		{"F11.1", true},               // the package heading
		{"F11.1.T7", false},           // only inside a fence
		{"acceptance-criteria", true}, // a GitHub slug
		{"Acceptance-Criteria", true}, // slugs are matched case-insensitively
		{"custom-anchor", true},       // <a id="...">
		{"notes-id", true},            // {#...}
		{"F11", true},                 // the phase heading
		{"F1", false},                 // a prefix of F11 is not F11's anchor
		{"", true},
	}
	for _, tt := range tests {
		if got := HasAnchor(spec, tt.anchor); got != tt.want {
			t.Errorf("HasAnchor(%q) = %v, want %v", tt.anchor, got, tt.want)
		}
	}
}

func TestCheckSpecRef(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "specs", "f1.md"), []byte("### F1.1.T1 — x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		ref  string
		want SpecRefCheck
	}{
		{"f1.md#F1.1.T1", SpecRefCheck{Ref: "f1.md#F1.1.T1", File: "specs/f1.md", Anchor: "F1.1.T1", FileFound: true, AnchorFound: true}},
		// Pre-bug-12 refs carry the spec root themselves.
		{"specs/f1.md#F1.1.T1", SpecRefCheck{Ref: "specs/f1.md#F1.1.T1", File: "specs/f1.md", Anchor: "F1.1.T1", FileFound: true, AnchorFound: true}},
		{"f1.md#F1.1.T2", SpecRefCheck{Ref: "f1.md#F1.1.T2", File: "specs/f1.md", Anchor: "F1.1.T2", FileFound: true}},
		{"f1.md", SpecRefCheck{Ref: "f1.md", File: "specs/f1.md", FileFound: true, AnchorFound: true}},
		{"f11.md#F11.1.T1", SpecRefCheck{Ref: "f11.md#F11.1.T1", File: "specs/f11.md", Anchor: "F11.1.T1"}},
		{"", SpecRefCheck{AnchorFound: true}},
	}
	for _, tt := range tests {
		got, err := CheckSpecRef(root, "specs", tt.ref)
		if err != nil {
			t.Fatalf("CheckSpecRef(%q): %v", tt.ref, err)
		}
		if got != tt.want {
			t.Errorf("CheckSpecRef(%q) = %+v\nwant %+v", tt.ref, got, tt.want)
		}
		if wantOK := tt.ref == "" || (tt.want.FileFound && tt.want.AnchorFound); got.OK() != wantOK {
			t.Errorf("CheckSpecRef(%q).OK() = %v, want %v", tt.ref, got.OK(), wantOK)
		}
	}
}

// TestGitStateOf is the state `orch atomize` warns about (#341): where a spec
// stands against the branch a task's worktree starts from.
func TestGitStateOf(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitIn(t, "", "init", "-q", "--bare", remote)
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	writeFile(t, root, "specs/pushed.md", "a\n")
	writeFile(t, root, "specs/edited.md", "a\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "one")
	gitIn(t, root, "remote", "add", "origin", remote)
	gitIn(t, root, "push", "-q", "origin", "main")
	writeFile(t, root, "specs/committed.md", "a\n")
	gitIn(t, root, "add", "specs/committed.md")
	gitIn(t, root, "commit", "-q", "-m", "two")
	writeFile(t, root, "specs/edited.md", "b\n")
	writeFile(t, root, "specs/new.md", "a\n")

	tests := []struct {
		path string
		want SpecGitState
	}{
		{"specs/pushed.md", SpecGitState{InRepo: true, BaseRef: "origin/main", OnBase: true}},
		{"specs/edited.md", SpecGitState{InRepo: true, Modified: true, BaseRef: "origin/main", OnBase: true}},
		{"specs/committed.md", SpecGitState{InRepo: true, BaseRef: "origin/main"}},
		{"specs/new.md", SpecGitState{InRepo: true, Untracked: true, BaseRef: "origin/main"}},
	}
	for _, tt := range tests {
		if got := GitStateOf(root, tt.path, "main"); got != tt.want {
			t.Errorf("GitStateOf(%s) = %+v\nwant %+v", tt.path, got, tt.want)
		}
	}
	if got := GitStateOf(t.TempDir(), "specs/new.md", "main"); got != (SpecGitState{}) {
		t.Errorf("outside a repo = %+v, want the zero state", got)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	// #nosec G204 -- this file's own fixed git subcommands.
	cmd := exec.Command("git", append([]string{"-c", "user.email=test@example.test", "-c", "user.name=test"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
