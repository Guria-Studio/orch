package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/worktree"
)

// gitRun runs git in dir with a fixed identity, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	// #nosec G204 -- this file's own fixed git subcommands.
	cmd := exec.Command("git", append([]string{"-c", "user.email=test@example.test", "-c", "user.name=test"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// eventExtra is the extra map of taskID's first event of eventType.
func (b *fakeBackend) eventExtra(taskID, eventType string) map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.events {
		if e.taskID == taskID && e.eventType == eventType {
			return e.extra
		}
	}
	return nil
}

func specTask(specRef string) model.Task {
	tk := task("F11.1.T1", 11, "claude/opus")
	tk.SpecRef = specRef
	return tk
}

// TestDispatchBlocksATaskWhoseSpecWasNeverPushed is #341 against real git: the
// spec is in the operator's working tree, where `orch atomize` read it, and not
// on origin/main, where the task's worktree starts. The agent must not be
// spawned to look for a file that is not there.
func TestDispatchBlocksATaskWhoseSpecWasNeverPushed(t *testing.T) {
	f := newReapFixture(t,
		[]model.Task{specTask("f11-dataset.md#F11.1.T1")},
		map[string]fakeResponse{"F11.1.T1": okResponse()},
		SchedulerOptions{WorktreeMode: true, BaseBranch: "main"})

	root := f.s.Opts.Cwd
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "-q", "--bare", remote)
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, filepath.Join(root, "specs", "f10.md"), "# F10 — Old\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "initial")
	gitRun(t, root, "remote", "add", "origin", remote)
	gitRun(t, root, "push", "-q", "origin", "main")
	// Written, atomized, never committed.
	write(t, filepath.Join(root, "specs", "f11-dataset.md"), "### F11.1.T1 — Load the dataset\n")

	f.s.Worktree = NewWorktreeManager(worktree.NewManager(root, true))

	f.runOnce(t)

	if got, _ := f.s.Queue.Status("F11.1.T1"); got != model.StatusBlocked {
		t.Fatalf("status = %q, want blocked", got)
	}
	if got := f.backend.eventTypes("F11.1.T1"); !equalStrings(got, []string{EventBlock}) {
		t.Errorf("events = %v, want only the block (no dispatch)", got)
	}
	want := "spec_missing: specs/f11-dataset.md is not in origin/main, which the task's worktree starts from; commit and push it, then `orch task-status F11.1.T1 todo`"
	if got := f.backend.eventExtra("F11.1.T1", EventBlock)["reason"]; got != want {
		t.Errorf("block reason = %q\nwant            %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(worktree.Dir(root), "F11.1.T1")); !os.IsNotExist(err) {
		t.Errorf("the blocked task's worktree was left behind (stat err = %v)", err)
	}
}

// TestDispatchTellsTheAgentWhenTheSpecDoesNotResolve covers the two cases
// that dispatch anyway: a spec file nowhere (#342 — a scaffolded project's
// tasks name specs not written yet, and blocking them all would stop the
// project), and a file without the anchor's heading. The prompt says so on
// the spec line, and a spec_ref_warning event records it.
func TestDispatchTellsTheAgentWhenTheSpecDoesNotResolve(t *testing.T) {
	tests := []struct {
		name        string
		inWorktree  string // the spec's content in the worktree, "" for none
		inProject   string // and in the project root
		wantLine    string
		wantProblem string
	}{
		{
			name:        "no spec file anywhere",
			wantLine:    "Spec ref (READ FIRST): specs/f11.md#F11.1.T1 (NOT FOUND in your working dir — do not look for it; take the acceptance criteria from the description)\n",
			wantProblem: "file_missing",
		},
		{
			name:        "the file without the anchor, here and there",
			inWorktree:  "### F11.1.T10 — Something else\n",
			inProject:   "### F11.1.T10 — Something else\n",
			wantLine:    "Spec ref (READ FIRST): specs/f11.md#F11.1.T1 (the file has no heading for F11.1.T1 — read it for context, and take the acceptance criteria from the description)\n",
			wantProblem: "anchor_missing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReapFixture(t,
				[]model.Task{specTask("f11.md#F11.1.T1")},
				map[string]fakeResponse{"F11.1.T1": okResponse()},
				SchedulerOptions{WorktreeMode: true, BaseBranch: "main"})
			wt := &specWorktree{recordingWorktree: recordingWorktree{dir: t.TempDir()}, spec: tt.inWorktree}
			f.s.Worktree = wt
			if tt.inProject != "" {
				write(t, filepath.Join(f.s.Opts.Cwd, "specs", "f11.md"), tt.inProject)
			}

			f.runOnce(t)

			if got := f.backend.eventTypes("F11.1.T1"); !contains(got, EventDispatch) {
				t.Fatalf("events = %v, want a dispatch", got)
			}
			if got := f.backend.eventExtra("F11.1.T1", EventSpecRefWarning)["problem"]; got != tt.wantProblem {
				t.Errorf("spec_ref_warning problem = %v, want %q", got, tt.wantProblem)
			}
			body, err := os.ReadFile(filepath.Join(f.s.Opts.StateDir, "prompts", "run-1", "F11.1.T1.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), "\n"+tt.wantLine) {
				t.Errorf("prompt has no line %q:\n%s", tt.wantLine, body)
			}
		})
	}
}

// TestDispatchBlocksWhenOnlyTheProjectsCopyHasTheAnchor: the spec was pushed,
// then the task's section was added and not pushed. Same fix as a missing
// file, so the same block.
func TestDispatchBlocksWhenOnlyTheProjectsCopyHasTheAnchor(t *testing.T) {
	f := newReapFixture(t,
		[]model.Task{specTask("f11.md#F11.1.T1")},
		map[string]fakeResponse{"F11.1.T1": okResponse()},
		SchedulerOptions{WorktreeMode: true, BaseBranch: "main"})
	wt := &specWorktree{recordingWorktree: recordingWorktree{dir: t.TempDir()}, spec: "# F11 — Data\n"}
	f.s.Worktree = wt
	write(t, filepath.Join(f.s.Opts.Cwd, "specs", "f11.md"), "# F11 — Data\n\n### F11.1.T1 — Load\n")

	f.runOnce(t)

	if got := f.backend.eventTypes("F11.1.T1"); !equalStrings(got, []string{EventBlock}) {
		t.Fatalf("events = %v, want only the block", got)
	}
	want := "spec_missing: specs/f11.md in origin/main has no heading for F11.1.T1, the project's copy does; commit and push it, then `orch task-status F11.1.T1 todo`"
	if got := f.backend.eventExtra("F11.1.T1", EventBlock)["reason"]; got != want {
		t.Errorf("block reason = %q\nwant            %q", got, want)
	}
	if got := wt.callList(); !equalStrings(got, []string{"create", "remove"}) {
		t.Errorf("worktree calls = %v, want create then remove", got)
	}
}

// TestDispatchLeavesAResolvingSpecAlone: a spec that is where it should be
// costs no event and leaves the prompt line as it always was.
func TestDispatchLeavesAResolvingSpecAlone(t *testing.T) {
	f := newReapFixture(t,
		[]model.Task{specTask("f11.md#F11.1.T1")},
		map[string]fakeResponse{"F11.1.T1": okResponse()},
		SchedulerOptions{WorktreeMode: true, BaseBranch: "main"})
	f.s.Worktree = &specWorktree{recordingWorktree: recordingWorktree{dir: t.TempDir()}, spec: "### F11.1.T1 — Load\n"}

	f.runOnce(t)

	if got := f.backend.eventTypes("F11.1.T1"); contains(got, EventSpecRefWarning) || !contains(got, EventDispatch) {
		t.Errorf("events = %v, want a dispatch and no spec_ref_warning", got)
	}
	body, err := os.ReadFile(filepath.Join(f.s.Opts.StateDir, "prompts", "run-1", "F11.1.T1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "\nSpec ref (READ FIRST): specs/f11.md#F11.1.T1\n") {
		t.Errorf("prompt spec line changed:\n%s", body)
	}
}

// specWorktree is a recordingWorktree whose fresh worktrees hold
// specs/f11.md with the given content, or nothing when it is "".
type specWorktree struct {
	recordingWorktree
	spec string
}

func (w *specWorktree) Create(ctx context.Context, taskID, base string, setupEnv []string, deps ...string) (string, error) {
	path, err := w.recordingWorktree.Create(ctx, taskID, base, setupEnv, deps...)
	if err != nil || w.spec == "" {
		return path, err
	}
	if err := os.MkdirAll(filepath.Join(path, "specs"), 0o750); err != nil {
		return "", err
	}
	return path, os.WriteFile(filepath.Join(path, "specs", "f11.md"), []byte(w.spec), 0o600)
}
