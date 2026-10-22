package verify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/ci"
	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/engine"
	"github.com/hectorcanaimero/orch/internal/model"
)

func TestVerifyPassesWhenTheCommandsPass(t *testing.T) {
	v := Verifier{Cfg: config.Verify{Commands: []string{"true", "test -d ."}, TimeoutS: 10}}
	if got := v.Verify(context.Background(), "T-1", model.BackendClaude, t.TempDir()); got != "" {
		t.Errorf("Verify = %q, want a pass", got)
	}
}

// The agent is shown the command and the end of its output: that is where a
// test runner or a compiler says what broke.
func TestVerifyReportsTheFailingCommandWithItsOutput(t *testing.T) {
	v := Verifier{Cfg: config.Verify{Commands: []string{"true", "echo FAIL TestLogin: want 401; exit 3", "touch never-ran"}, TimeoutS: 10}}
	dir := t.TempDir()
	got := v.Verify(context.Background(), "T-1", model.BackendClaude, dir)
	for _, want := range []string{"`echo FAIL TestLogin: want 401; exit 3`", "exit status 3", "FAIL TestLogin: want 401"} {
		if !strings.Contains(got, want) {
			t.Errorf("Verify = %q, want it to contain %q", got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "never-ran")); err == nil {
		t.Error("a command after the failing one ran")
	}
}

func TestVerifyBoundsEachCommand(t *testing.T) {
	v := Verifier{Cfg: config.Verify{Commands: []string{"sleep 30"}, TimeoutS: 1}}
	if got := v.Verify(context.Background(), "T-1", model.BackendClaude, t.TempDir()); !strings.Contains(got, "timed out after 1s") {
		t.Errorf("Verify = %q, want a timeout", got)
	}
}

// Detected checks run per package, from the package's directory, and one
// whose tool is not installed is skipped rather than failing the agent.
func TestVerifyRunsTheDetectedChecks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "Makefile"), "test:\n\t@echo detected-make-test; exit 4\n")
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	v := Verifier{Cfg: config.Verify{TimeoutS: 10}}
	if got := v.Verify(context.Background(), "T-1", model.BackendClaude, dir); !strings.Contains(got, "detected-make-test") {
		t.Errorf("Verify = %q, want the detected make test to have run and failed", got)
	}
}

func TestVerifySendsBackABlockingReview(t *testing.T) {
	dir := branchWithWork(t)
	fakeReviewer(t, "0", readFixture(t, "claude", "2.1.272", "review-verdict.json"))
	v := Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main"}

	got := v.Verify(context.Background(), "T-1", model.BackendCodex, dir)
	for _, want := range []string{"the review asks for changes", "Tests: new behaviour comes with a test that exercises it"} {
		if !strings.Contains(got, want) {
			t.Errorf("Verify = %q, want it to contain %q", got, want)
		}
	}
}

func TestVerifyPassesAnApprovingReview(t *testing.T) {
	dir := branchWithWork(t)
	fakeReviewer(t, "0", []byte(`{"type":"result","subtype":"success","is_error":false,"result":"{\"verdict\":\"approve\",\"summary\":\"fine\",\"findings\":[]}"}`))
	v := Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main"}

	if got := v.Verify(context.Background(), "T-1", model.BackendCodex, dir); got != "" {
		t.Errorf("Verify = %q, want a pass", got)
	}
}

// A reviewer that is down is not the agent's fault: the work passes.
func TestVerifyPassesWhenTheReviewCannotComplete(t *testing.T) {
	dir := branchWithWork(t)
	fakeReviewer(t, "1", []byte(`{"type":"result","subtype":"error","is_error":true,"terminal_reason":"api_error"}`))
	v := Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main"}

	if got := v.Verify(context.Background(), "T-1", model.BackendCodex, dir); got != "" {
		t.Errorf("Verify = %q, want a pass", got)
	}
}

func TestReviewerIsNeverTheWriterByDefault(t *testing.T) {
	bin := t.TempDir()
	for _, b := range []string{"claude", "gemini"} {
		write(t, filepath.Join(bin, b), "#!/bin/sh\n")
		if err := os.Chmod(filepath.Join(bin, b), 0o700); err != nil { // #nosec G302 -- an executable stub in the test's temp dir
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	v := Verifier{}
	if got := v.reviewer(model.BackendClaude); got != model.BackendGemini {
		t.Errorf("reviewer for claude's work = %q, want gemini", got)
	}
	if got := v.reviewer(model.BackendCodex); got != model.BackendClaude {
		t.Errorf("reviewer for codex's work = %q, want claude", got)
	}
	v.Cfg.Reviewer = "claude"
	if got := v.reviewer(model.BackendClaude); got != model.BackendClaude {
		t.Errorf("configured reviewer = %q, want claude", got)
	}
}

func TestChecklistPrefersTheProjectsOwn(t *testing.T) {
	dir := t.TempDir()
	if got := checklist(dir, ""); got != ci.BuiltinChecklist {
		t.Errorf("no checklist file: got %q, want the built-in one", got)
	}
	write(t, filepath.Join(dir, ".github", "orch-review.md"), "- github one\n")
	if got := checklist(dir, ""); got != "- github one\n" {
		t.Errorf("got %q, want .github/orch-review.md", got)
	}
	write(t, filepath.Join(dir, ".orch", "review.md"), "- the domain imports no adapter\n")
	if got := checklist(dir, ""); got != "- the domain imports no adapter\n" {
		t.Errorf("got %q, want .orch/review.md first", got)
	}
	write(t, filepath.Join(dir, "docs", "rules.md"), "- configured\n")
	if got := checklist(dir, "docs/rules.md"); got != "- configured\n" {
		t.Errorf("got %q, want verify.checklist", got)
	}
}

// branchWithWork is a repository whose HEAD is one commit ahead of main.
func branchWithWork(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		// #nosec G204 -- this test's own fixed git subcommands.
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.test", "-c", "user.name=t"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("symbolic-ref", "HEAD", "refs/heads/main")
	write(t, filepath.Join(dir, "README.md"), "x\n")
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	git("checkout", "-q", "-b", "orch/T-1")
	write(t, filepath.Join(dir, "store.go"), "package store\n\nfunc Load() {}\n")
	git("add", "-A")
	git("commit", "-q", "-m", "work")
	return dir
}

// fakeReviewer makes claude answer with output and exit code, through the
// engine's fake provider.
func fakeReviewer(t *testing.T, exit string, output []byte) {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "claude", ci.ReviewTaskID+".out"), string(output))
	write(t, filepath.Join(dir, "claude", ci.ReviewTaskID+".exit"), exit)
	t.Setenv(engine.FakeProviderEnv, dir)
}

func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", "providers", "testdata"}, parts...)...)) // #nosec G304 -- a fixture under testdata
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestIsTestFile(t *testing.T) {
	tests := map[string]bool{
		"internal/auth/login_test.go":  true,
		"src/auth/login.test.ts":       true,
		"web/src/Button.spec.tsx":      true,
		"tests/test_login.py":          true,
		"app/login_test.py":            true,
		"src/test/java/LoginTest.java": true,
		"Auth.Tests/LoginTests.cs":     true,
		"src/__tests__/login.js":       true,
		"crates/auth/tests/login.rs":   true,
		"spec/login_spec.rb":           true,
		"internal/auth/login.go":       false,
		"src/auth/login.ts":            false,
		"app/testing_utils.py":         false,
		"docs/testing.md":              false,
		"src/contest/entry.ts":         false,
	}
	for path, want := range tests {
		if got := IsTestFile(path); got != want {
			t.Errorf("IsTestFile(%q) = %v, want %v", path, got, want)
		}
	}
}

// With dispatch.tdd on, a change with no test in it goes back to the agent,
// before any command runs; one with a test goes on to the commands.
func TestVerifyRequiresATestWhenTDDIsOn(t *testing.T) {
	dir := branchWithWork(t) // adds store.go only
	v := Verifier{Cfg: config.Verify{Commands: []string{"touch commands-ran"}, TimeoutS: 10}, Base: "main", RequireTests: true}

	got := v.Verify(context.Background(), "T-1", model.BackendClaude, dir)
	if !strings.Contains(got, "touches no test file") || !strings.Contains(got, "store.go") {
		t.Errorf("Verify = %q, want the missing test reported with the changed files", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "commands-ran")); err == nil {
		t.Error("the commands ran on a change that was already sent back")
	}

	write(t, filepath.Join(dir, "store_test.go"), "package store\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "test")
	if got := v.Verify(context.Background(), "T-1", model.BackendClaude, dir); got != "" {
		t.Errorf("Verify = %q, want a pass once a test is in the change", got)
	}

	v.RequireTests = false
	gitIn(t, dir, "rm", "-q", "store_test.go")
	gitIn(t, dir, "commit", "-q", "-m", "no test")
	if got := v.Verify(context.Background(), "T-1", model.BackendClaude, dir); got != "" {
		t.Errorf("Verify = %q, want no test requirement with dispatch.tdd off", got)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	// #nosec G204 -- this test's own fixed git subcommands.
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.test", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
