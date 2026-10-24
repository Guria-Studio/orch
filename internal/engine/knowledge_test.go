package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/knowledge"
	"github.com/hectorcanaimero/orch/internal/prompt"
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

// trail is a comment trail to read back, or the error reading it fails with.
type trail struct {
	comments []json.RawMessage
	err      error
}

func (c trail) TaskComments(context.Context, string) ([]json.RawMessage, error) {
	return c.comments, c.err
}

// captureFixture is a scheduler that drafts lessons into a temp worktree,
// with the comment trail c and its log in logs.
func captureFixture(t *testing.T, c CommentReader) (*Scheduler, *InFlight, *bytes.Buffer) {
	t.Helper()
	q, err := NewTaskQueue(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(q, nil, SchedulerOptions{})
	s.Opts.Cfg.Knowledge.Enabled = true
	s.Worktree = NewWorktreeManager(worktree.NewManager(t.TempDir(), false))
	s.Comments = c
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewTextHandler(&logs, nil))
	tk := task("C-1", 1, "claude/opus")
	tk.Files = []string{"internal/auth/login.go"}
	return s, &InFlight{Task: tk, Dispatch: Dispatch{Cwd: t.TempDir()}}, &logs
}

func lessonsIn(t *testing.T, dir string) map[string]knowledge.Lesson {
	t.Helper()
	ls, err := knowledge.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]knowledge.Lesson{}
	for _, l := range ls {
		out[l.Source] = l
	}
	return out
}

// Both drafts end with what the agent reported — its own note, not orch's
// bookkeeping after it.
func TestDraftLessonsCarryTheAgentsNote(t *testing.T) {
	c := trail{comments: []json.RawMessage{
		json.RawMessage(`{"author":"claude/opus","body":"refreshed the token before the retry"}`),
		json.RawMessage(`{"author":"orch","body":"dispatch succeeded"}`),
	}}
	s, entry, _ := captureFixture(t, c)
	s.verifyRejected["C-1"] = "`go test` failed:\nFAIL TestLogin"
	entry.Conflict = &prompt.Conflict{Base: "main", Files: []string{"a.go"}, Commits: []string{"abc fix (#1)"}}

	s.captureLessons(context.Background(), entry)

	got := lessonsIn(t, entry.Dispatch.Cwd)
	if b := got["verify"].Body; !strings.HasSuffix(b, "\n\nThe attempt that passed: refreshed the token before the retry") {
		t.Errorf("verify draft body:\n%s", b)
	}
	if b := got["conflict"].Body; !strings.HasSuffix(b, "\n\nHow the agent resolved it: refreshed the token before the retry") {
		t.Errorf("conflict draft body:\n%s", b)
	}
	if _, left := s.verifyRejected["C-1"]; left {
		t.Error("the rejection is still kept after its draft was written")
	}
}

// With no note to read — no trail, an empty one, or one that cannot be read
// (which is logged) — the drafts are written without it.
func TestAgentNote(t *testing.T) {
	for _, tc := range []struct {
		name  string
		c     CommentReader
		want  string
		warns bool
	}{
		{"no comment reader", nil, "", false},
		{"only orch's bookkeeping", trail{comments: []json.RawMessage{json.RawMessage(`{"author":"orch","body":"dispatch succeeded"}`)}}, "", false},
		{"the agent's note", trail{comments: []json.RawMessage{json.RawMessage(`{"author":"claude/opus","body":"  did it  "}`)}}, "did it", false},
		{"an unreadable trail", trail{err: errors.New("database is locked")}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, entry, logs := captureFixture(t, tc.c)
			if got := s.agentNote(context.Background(), "C-1"); got != tc.want {
				t.Errorf("agentNote = %q, want %q", got, tc.want)
			}
			warned := strings.Contains(logs.String(), "database is locked")
			if warned != tc.warns {
				t.Errorf("warned = %v, want %v: %s", warned, tc.warns, logs)
			}
			s.verifyRejected["C-1"] = "boom"
			s.captureLessons(context.Background(), entry)
			if b := lessonsIn(t, entry.Dispatch.Cwd)["verify"].Body; b == "" || strings.Contains(b, "The attempt that passed") != (tc.want != "") {
				t.Errorf("verify draft body:\n%s", b)
			}
		})
	}
}

// A draft's title is cut at 100 characters, not bytes.
func TestDraftTitleIsCutAtAHundredRunes(t *testing.T) {
	s, entry, _ := captureFixture(t, nil)
	s.verifyRejected["C-1"] = strings.Repeat("ñ", 150)
	s.captureLessons(context.Background(), entry)
	title := lessonsIn(t, entry.Dispatch.Cwd)["verify"].Title
	if r := []rune(title); len(r) != 101 || r[100] != '…' || !strings.HasPrefix(title, "Verify rejected C-1: ñ") {
		t.Errorf("title = %q (%d runes), want 100 runes and an ellipsis", title, len(r))
	}
}
