package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/prompt"
	"github.com/hectorcanaimero/orch/internal/state"
	"github.com/hectorcanaimero/orch/internal/vcs"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	// #nosec G204 -- the test's own git commands.
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// withTaskBranch makes the project a repository whose orch/T-1 changed
// app.go and added notes.md, and whose main moved on after the branch left.
func withTaskBranch(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "app.go"), "package app\n\nfunc A() {}\n")
	gitIn(t, root, "init", "-q", "-b", "main")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "base")
	gitIn(t, root, "checkout", "-q", "-b", "orch/T-1")
	writeFile(t, filepath.Join(root, "app.go"), "package app\n\nfunc A() { B() }\n\nfunc B() {}\n")
	writeFile(t, filepath.Join(root, "notes.md"), "# notes\n")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "work")
	gitIn(t, root, "checkout", "-q", "main")
	writeFile(t, filepath.Join(root, "later.txt"), "main moved on\n")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "later")
}

func reviewGet(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer test-token-stakeholder")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestTaskDiffIsTheBranchAgainstItsMergeBase: #454. The Changes tab reads
// what orch/<id> changed since it left main, file by file with its counts —
// not what main did afterwards.
func TestTaskDiffIsTheBranchAgainstItsMergeBase(t *testing.T) {
	s, _ := newSteerServer(t, ProfileOperator, "")
	withTaskBranch(t, s.paths.Root)

	rec := reviewGet(t, s, "/api/task/T-1/diff")
	if rec.Code != http.StatusOK {
		t.Fatalf("diff = %d: %s", rec.Code, rec.Body)
	}
	var got taskDiff
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Branch != "orch/T-1" || got.Base != "main" {
		t.Errorf("branch %q base %q", got.Branch, got.Base)
	}
	if len(got.Files) != 2 {
		t.Fatalf("files %+v, want app.go and notes.md only", got.Files)
	}
	app, notes := got.Files[0], got.Files[1]
	if app.Path != "app.go" || app.Additions != 3 || app.Deletions != 1 || !strings.HasPrefix(app.Patch, "@@ ") {
		t.Errorf("app.go = %+v", app)
	}
	if notes.Path != "notes.md" || notes.Additions != 1 || notes.Deletions != 0 {
		t.Errorf("notes.md = %+v", notes)
	}

	if rec := reviewGet(t, s, "/api/task/T-2/diff"); rec.Code != http.StatusNotFound {
		t.Errorf("a task with no branch = %d, want 404", rec.Code)
	}
	if rec := reviewGet(t, s, "/api/task/nope/diff"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown task = %d, want 404", rec.Code)
	}
}

// TestTaskDiffIsNotAStakeholderPage: off the default allow-list, so a client
// sees no code unless the operator adds api_task_diff.
func TestTaskDiffIsNotAStakeholderPage(t *testing.T) {
	s, _ := newSteerServer(t, ProfileStakeholder, "test-token-stakeholder")
	withTaskBranch(t, s.paths.Root)
	if rec := reviewGet(t, s, "/api/task/T-1/diff"); rec.Code != http.StatusForbidden {
		t.Errorf("stakeholder diff = %d, want 403: %s", rec.Code, rec.Body)
	}
}

type fakeMerge struct {
	vcs.Provider
	merged []string
}

func (f *fakeMerge) MergePR(prURL string, _, _ bool) error {
	f.merged = append(f.merged, prURL)
	return nil
}

// TestAcceptMergesThroughTheCLIsFunction: Accept is project.Steer.Accept,
// `orch task accept`'s; only the local operator may.
func TestAcceptMergesThroughTheCLIsFunction(t *testing.T) {
	ctx := context.Background()
	s, backend := newSteerServer(t, ProfileOperator, "")
	fake := &fakeMerge{}
	s.steer.Provider = fake

	if rec := steerPost(t, s, "/api/task/T-1/accept", "", "127.0.0.1:50000", ""); rec.Code != http.StatusConflict {
		t.Errorf("accept with no PR = %d, want 409: %s", rec.Code, rec.Body)
	}
	if err := backend.SetTaskPR(ctx, "T-1", "https://github.com/o/r/pull/9"); err != nil {
		t.Fatal(err)
	}
	if rec := steerPost(t, s, "/api/task/T-1/accept", "", "192.168.1.20:50000", ""); rec.Code != http.StatusForbidden {
		t.Errorf("accept from off the machine = %d, want 403", rec.Code)
	}
	rec := steerPost(t, s, "/api/task/T-1/accept", "", "127.0.0.1:50000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("accept = %d: %s", rec.Code, rec.Body)
	}
	if len(fake.merged) != 1 || fake.merged[0] != "https://github.com/o/r/pull/9" {
		t.Errorf("merged %v, want the task's PR once", fake.merged)
	}
}

// TestSendBackIsANoteThatRetries: the note box's Send back notes the task
// and puts it back in todo, through project.Steer.SendBack.
func TestSendBackIsANoteThatRetries(t *testing.T) {
	ctx := context.Background()
	s, backend := newSteerServer(t, ProfileOperator, "")
	if err := backend.Transition(ctx, "T-1", model.StatusDone, state.Note{}); err != nil {
		t.Fatal(err)
	}
	rec := steerPost(t, s, "/api/task/T-1/note", `{"note":"rename the flag","retry":true}`, "127.0.0.1:50000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("send back = %d: %s", rec.Code, rec.Body)
	}
	got, err := backend.Task(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusTodo {
		t.Errorf("status %s, want todo", got.Status)
	}
	if notes := prompt.OperatorNotes(got.Comments); len(notes) != 1 || notes[0] != "rename the flag" {
		t.Errorf("notes %v", notes)
	}
}

func TestParseDiffKeepsBinaryAndDeletedFiles(t *testing.T) {
	out := "diff --git a/logo.png b/logo.png\nnew file mode 100644\nBinary files /dev/null and b/logo.png differ\n" +
		"diff --git a/old.txt b/old.txt\ndeleted file mode 100644\n--- a/old.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-gone\n"
	got := parseDiff(out)
	if len(got) != 2 || !got[0].Binary || got[0].Path != "logo.png" || got[1].Path != "old.txt" || got[1].Deletions != 1 {
		t.Errorf("parseDiff = %+v", got)
	}
	if len(parseDiff("")) != 0 {
		t.Error("an empty diff has no files")
	}
}

// TestRunGitSaysWhatFailed: a git that could not start writes no stderr; the
// error still names the command and why.
func TestRunGitSaysWhatFailed(t *testing.T) {
	_, err := runGit(context.Background(), filepath.Join(t.TempDir(), "gone"), "status")
	if err == nil || !strings.Contains(err.Error(), "git status") || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("runGit in a missing dir = %v, want the command and the cause", err)
	}
	root := t.TempDir()
	_, err = runGit(context.Background(), root, "rev-parse", "--verify", "nope^{commit}")
	if err == nil || !strings.Contains(err.Error(), "exit status") || !strings.Contains(err.Error(), "fatal") {
		t.Errorf("runGit outside a repo = %v, want the exit and git's stderr", err)
	}
}

// TestParseDiffCutsAHugeFile: one file's hunks stop at diffPatchLimit and say so.
func TestParseDiffCutsAHugeFile(t *testing.T) {
	line := "+" + strings.Repeat("x", 99) + "\n"
	out := "diff --git a/big.js b/big.js\n--- a/big.js\n+++ b/big.js\n@@ -0,0 +1,3000 @@\n" + strings.Repeat(line, 3000)
	got := parseDiff(out)
	if len(got) != 1 || !got[0].Truncated || len(got[0].Patch) != diffPatchLimit || got[0].Additions != 3000 {
		t.Errorf("parseDiff = truncated %v, patch %d bytes, +%d; want cut at %d, counts whole",
			got[0].Truncated, len(got[0].Patch), got[0].Additions, diffPatchLimit)
	}
}
