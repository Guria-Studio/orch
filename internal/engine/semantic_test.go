package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/collide"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/vcs"
	"github.com/Guria-Studio/orch/internal/worktree"
)

const semanticPR = "https://github.com/o/r/pull/2"

func migrationsRule(mode string) []collide.Rule {
	return []collide.Rule{{Name: "migrations", Kind: collide.KindSequence,
		Paths: []string{"db/migrations/*.sql"}, Key: `^(\d+)_`, Mode: mode}}
}

// semanticCIFixture is the v5/v5 incident (#522) at the merge: C-2's PR adds
// migration 002_c2.sql, pushed and waiting on CI, while a sibling's
// 002_c1.sql was merged into main meanwhile. git merges the two cleanly; CI is
// green.
func semanticCIFixture(t *testing.T, mode string) (*ciFixture, string) {
	t.Helper()
	f := newCIFixture(t, pendingRow("C-2", semanticPR, 0))
	f.vcs.status[semanticPR] = vcs.CISuccess

	root := f.s.Opts.Cwd
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "-q", "--bare", remote)
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, filepath.Join(root, "db/migrations/001_init.sql"), "-- init\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "initial")
	gitRun(t, root, "remote", "add", "origin", remote)
	gitRun(t, root, "push", "-q", "origin", "main")

	wm := NewWorktreeManager(worktree.NewManager(root, true))
	f.s.Worktree = wm
	f.s.Opts.WorktreeMode = true
	f.s.Opts.BaseBranch = "main"
	f.s.Opts.AutoPR = true
	f.s.Opts.Cfg.SemanticChecks = migrationsRule(mode)
	f.s.VCS = f.vcs
	f.s.CIRecorder = &recordingPR{}
	f.poller.Worktree = wm
	f.poller.ConflictRetries = 1
	f.poller.AutoMerge = true
	f.poller.Squash = true

	ctx := context.Background()
	wt, err := wm.Create(ctx, "C-2", "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt, "db/migrations/002_c2.sql"), "-- C-2\n")
	if err := wm.CommitPending(ctx, "C-2"); err != nil {
		t.Fatal(err)
	}
	if err := wm.Push(ctx, "C-2"); err != nil {
		t.Fatal(err)
	}
	if err := wm.Remove(ctx, "C-2"); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(root, "db/migrations/002_c1.sql"), "-- C-1\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "C-1: add migration 002 (#1)")
	gitRun(t, root, "push", "-q", "origin", "main")
	return f, root
}

// TestSemanticCollisionStopsTheAutoMerge (#522): green CI and a clean git
// merge used to be enough to merge, and main ended with two migrations 002.
// A block rule now keeps the PR open and sends the task back to its agent
// with the collision in its feedback file and the base merged in, within
// retry.conflict; past it the task blocks. A warn rule records the collision
// and merges.
func TestSemanticCollisionStopsTheAutoMerge(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		attempts   int
		wantMerged bool
		wantRetry  bool
		wantStatus model.Status
	}{
		{"block sends it back to the agent", collide.ModeBlock, 0, false, true, model.StatusTodo},
		{"block with the retries spent blocks", collide.ModeBlock, 1, false, false, model.StatusBlocked},
		{"warn merges and says so", collide.ModeWarn, 0, true, false, model.StatusDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, root := semanticCIFixture(t, tt.mode)
			f.poller.conflictAttempts = map[string]int{"C-2": tt.attempts}

			if _, err := f.poller.Poll(context.Background(), f.s); err != nil {
				t.Fatalf("Poll: %v", err)
			}

			if merged := len(f.vcs.merged) > 0; merged != tt.wantMerged {
				t.Errorf("merged = %v, want %v", merged, tt.wantMerged)
			}
			extra := f.backend.eventExtra("C-2", EventSemanticCollision)
			if extra == nil || extra["key"] != "002" || extra["with"] != "base" || extra["file"] != "db/migrations/002_c2.sql" ||
				extra["with_file"] != "db/migrations/002_c1.sql" || extra["mode"] != tt.mode || extra["at"] != "merge" {
				t.Errorf("semantic_collision extra = %v, want 002 against the base's 002_c1.sql", extra)
			}
			if got, _ := f.s.Queue.Status("C-2"); got != tt.wantStatus {
				t.Errorf("status = %q, want %q", got, tt.wantStatus)
			}
			if got := len(f.s.retryQueueIDs()) == 1; got != tt.wantRetry {
				t.Errorf("queued for the agent = %v, want %v", got, tt.wantRetry)
			}
			if tt.wantRetry {
				wt := filepath.Join(worktree.Dir(root), "C-2")
				feedback, err := os.ReadFile(filepath.Join(wt, CIFeedbackFile)) // #nosec G304 -- a temp dir this test made
				if err != nil || !strings.Contains(string(feedback), "002_c1.sql") {
					t.Errorf("feedback = %q, %v; want the collision", feedback, err)
				}
				if _, err := os.Stat(filepath.Join(wt, "db/migrations/002_c1.sql")); err != nil {
					t.Errorf("the base's migration is not in the agent's worktree: %v", err)
				}
			}
			if tt.wantStatus == model.StatusBlocked {
				reason, _ := f.backend.eventExtra("C-2", EventCIBlocked)["reason"].(string)
				if !strings.Contains(reason, "002") {
					t.Errorf("block reason = %q, want the collision named", reason)
				}
			}
		})
	}
}

// TestSemanticCollisionIsReportedWhenThePROpens (#522): with both PRs open
// and neither merged, each adding a migration 002, the second PR to open is
// flagged against the first — always a warning at this point.
func TestSemanticCollisionIsReportedWhenThePROpens(t *testing.T) {
	ok := okResponse()
	f := newReapFixture(t,
		[]model.Task{task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": ok},
		SchedulerOptions{WorktreeMode: true, AutoPR: true, BaseBranch: "main"})
	root := f.s.Opts.Cwd
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "-q", "--bare", remote)
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, filepath.Join(root, "db/migrations/001_init.sql"), "-- init\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "initial")
	gitRun(t, root, "remote", "add", "origin", remote)
	gitRun(t, root, "push", "-q", "origin", "main")
	// C-9's PR is open: its branch adds 002_c9.sql.
	gitRun(t, root, "checkout", "-q", "-b", "orch/C-9")
	write(t, filepath.Join(root, "db/migrations/002_c9.sql"), "-- C-9\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "C-9")
	gitRun(t, root, "checkout", "-q", "main")

	write(t, filepath.Join(f.fakeDir, "C-1.sh"), "mkdir -p db/migrations && printf -- '-- C-1\\n' > db/migrations/002_c1.sql\n")
	f.s.Worktree = NewWorktreeManager(worktree.NewManager(root, true))
	f.s.VCS = &fakeVCS{status: map[string]vcs.CIState{}, createdPR: "https://github.com/o/r/pull/7"}
	f.s.CIRecorder = &recordingPR{}
	f.s.OpenPRs = fakeOpenPRs{{ID: "C-9", PRURL: "https://github.com/o/r/pull/6"}}
	f.s.Opts.Cfg.Radar = "warn"
	f.s.Opts.Cfg.SemanticChecks = migrationsRule(collide.ModeBlock)

	f.runOnce(t)

	extra := f.backend.eventExtra("C-1", EventSemanticCollision)
	if extra == nil || extra["key"] != "002" || extra["with"] != "C-9" || extra["with_file"] != "db/migrations/002_c9.sql" ||
		extra["at"] != "pr_open" || extra["pr_url"] != "https://github.com/o/r/pull/6" {
		t.Errorf("semantic_collision extra = %v, want 002 against C-9's PR", extra)
	}
}

// TestSemanticCollisionStopsALocalLand (#522): with vcs.provider local the
// check runs before the land; a block rule keeps the work off the base.
func TestSemanticCollisionStopsALocalLand(t *testing.T) {
	f := newReapFixture(t,
		[]model.Task{task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": okResponse()},
		SchedulerOptions{WorktreeMode: true, LocalMerge: true, BaseBranch: "main"})
	root := f.s.Opts.Cwd
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, filepath.Join(root, "db/migrations/001_init.sql"), "-- init\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "initial")
	// The sibling lands its 002 while C-1 works on its own.
	write(t, filepath.Join(f.fakeDir, "C-1.sh"), "printf -- '-- C-1\\n' > db/migrations/002_c1.sql\n"+
		"printf -- '-- C-0\\n' > '"+root+"/db/migrations/002_c0.sql' && git -C '"+root+"' add -A && "+
		"git -C '"+root+"' -c user.email=t@t.test -c user.name=t commit -qm 'C-0: migration 002 (#1)'\n")
	f.s.Worktree = NewWorktreeManager(worktree.NewManager(root, false))
	f.s.Opts.Cfg.SemanticChecks = migrationsRule(collide.ModeBlock)

	f.runOnce(t)

	if inMainTree(t, root, "db/migrations/002_c1.sql") {
		t.Fatal("a second migration 002 landed on main")
	}
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusBlocked {
		t.Errorf("status = %q, want blocked", got)
	}
	if extra := f.backend.eventExtra("C-1", EventSemanticCollision); extra == nil || extra["at"] != "land" || extra["key"] != "002" {
		t.Errorf("semantic_collision extra = %v, want 002 at the land", extra)
	}
}

// inMainTree reports whether path is in main's tree.
func inMainTree(t *testing.T, root, path string) bool {
	t.Helper()
	// #nosec G204 -- fixed git subcommand on a temp repo this test made.
	return exec.Command("git", "-C", root, "cat-file", "-e", "main:"+path).Run() == nil
}
