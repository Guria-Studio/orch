package engine

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/worktree"
)

// A verify rejection followed by a pass leaves a draft lesson in the task's
// pushed branch (#451): what was rejected, under the task's files, for the
// reviewer to keep, edit or drop. With knowledge.auto_capture off, none.
func TestAVerifyRejectionTheNextAttemptFixedBecomesADraftLesson(t *testing.T) {
	off := false
	for _, tc := range []struct {
		name string
		auto *bool
		want bool
	}{{"auto-capture on", nil, true}, {"auto-capture off", &off, false}} {
		t.Run(tc.name, func(t *testing.T) {
			v := &scriptedVerifier{answers: []string{"`go test ./...` failed:\nFAIL TestLogin", ""}}
			f, remote := verifyFixture(t, v, 2)
			f.s.Opts.Cfg.Knowledge.Enabled = true
			f.s.Opts.Cfg.Knowledge.AutoCapture = tc.auto
			ctx := context.Background()

			f.runOnce(t)
			if _, err := f.s.Refill(ctx); err != nil {
				t.Fatalf("Refill: %v", err)
			}
			if _, err := f.s.DrainWait(ctx, 30*time.Second); err != nil {
				t.Fatalf("DrainWait: %v", err)
			}

			// #nosec G204 -- fixed git subcommand on a temp repo this test made.
			out, err := exec.Command("git", "-C", remote, "ls-tree", "-r", "--name-only", "orch/C-1", ".orch/knowledge").CombinedOutput()
			if err != nil {
				t.Fatalf("ls-tree: %v\n%s", err, out)
			}
			files := strings.Fields(string(out))
			if !tc.want {
				if len(files) != 0 {
					t.Errorf("auto-capture off, yet the branch carries %v", files)
				}
				return
			}
			if len(files) != 1 {
				t.Fatalf("lessons in the pushed branch = %v, want one draft", files)
			}
			// #nosec G204 -- fixed git subcommand on a temp repo this test made.
			body, err := exec.Command("git", "-C", remote, "show", "orch/C-1:"+files[0]).CombinedOutput()
			if err != nil {
				t.Fatalf("git show: %v\n%s", err, body)
			}
			for _, want := range []string{"source: verify", "task: C-1", "FAIL TestLogin", "`go test ./...` failed:"} {
				if !strings.Contains(string(body), want) {
					t.Errorf("the draft lesson is missing %q:\n%s", want, body)
				}
			}
		})
	}
}

// A conflict the agent resolved leaves a draft lesson in the PR (#451): the
// files, the base commits that touched them, under the files' area.
func TestAResolvedConflictBecomesADraftLesson(t *testing.T) {
	f, wm, remote := conflictFixture(t, true)
	f.s.Opts.Cfg.Knowledge.Enabled = true
	ctx := context.Background()

	if _, err := f.poller.Poll(ctx, f.s); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if _, err := f.s.Refill(ctx); err != nil {
		t.Fatalf("Refill: %v", err)
	}
	wt := filepath.Join(worktree.Dir(f.s.Opts.Cwd), "C-2")
	write(t, filepath.Join(wt, "shared.txt"), "from C-1\nfrom C-2\n")
	if _, err := f.s.DrainWait(ctx, 30*time.Second); err != nil {
		t.Fatalf("DrainWait: %v", err)
	}

	// #nosec G204 -- fixed git subcommand on a temp repo this test made.
	out, err := exec.Command("git", "-C", remote, "ls-tree", "-r", "--name-only", wm.BranchName("C-2"), ".orch/knowledge").CombinedOutput()
	if err != nil {
		t.Fatalf("ls-tree: %v\n%s", err, out)
	}
	files := strings.Fields(string(out))
	if len(files) != 1 {
		t.Fatalf("lessons in the PR branch = %v, want one draft", files)
	}
	body := remoteFile(t, remote, wm.BranchName("C-2"), files[0])
	for _, want := range []string{"source: conflict", "task: C-2", "- shared.txt", "conflicted with main in shared.txt", "C-1: edit (#1)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the draft lesson is missing %q:\n%s", want, body)
		}
	}
}
