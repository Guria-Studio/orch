package verify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/budget"
	"github.com/Guria-Studio/orch/internal/ci"
	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/engine"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
	"github.com/Guria-Studio/orch/internal/verify"
)

func TestVerifyPassesWhenTheCommandsPass(t *testing.T) {
	v := verify.Verifier{Cfg: config.Verify{Commands: []string{"true", "test -d .", "no-such-tool-463"}, TimeoutS: 10}}
	res := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: t.TempDir()})
	if res.Failure != "" {
		t.Errorf("Verify = %q, want a pass", res.Failure)
	}
	// #463: the pass says what ran, and the command sh could not find did not.
	if len(res.Commands) != 2 || res.Commands[0].Command != "true" || res.Commands[1].Command != "test -d ." || res.Commands[0].DurationS < 0 {
		t.Errorf("Commands = %+v, want true and test -d . with their durations", res.Commands)
	}
}

// The agent is shown the command and the end of its output: that is where a
// test runner or a compiler says what broke.
func TestVerifyReportsTheFailingCommandWithItsOutput(t *testing.T) {
	v := verify.Verifier{Cfg: config.Verify{Commands: []string{"true", "echo FAIL TestLogin: want 401; exit 3", "touch never-ran"}, TimeoutS: 10}}
	dir := t.TempDir()
	got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir}).Failure
	for _, want := range []string{"`echo FAIL TestLogin: want 401; exit 3`", "exit status 3", "FAIL TestLogin: want 401"} {
		if !strings.Contains(got, want) {
			t.Errorf("Verify = %q, want it to contain %q", got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "never-ran")); err == nil {
		t.Error("a command after the failing one ran")
	}
}

// A command that fails without printing anything still tells the agent so,
// rather than handing it an empty code block to puzzle over (#598).
func TestVerifySaysWhenTheFailingCommandPrintedNothing(t *testing.T) {
	v := verify.Verifier{Cfg: config.Verify{Commands: []string{"exit 4"}, TimeoutS: 10}}
	got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: t.TempDir()}).Failure
	if strings.Contains(got, "```\n\n```") {
		t.Errorf("Verify = %q, an empty code block", got)
	}
	for _, want := range []string{"`exit 4`", "exit status 4", "printed nothing"} {
		if !strings.Contains(got, want) {
			t.Errorf("Verify = %q, want it to contain %q", got, want)
		}
	}
}

// A configured command that is not found (sh's exit 127, a typo such as
// `tue`) is the operator's config, not the agent's work: it is skipped, and
// so is the E2E an app that cannot be started was for (#433).
func TestVerifySkipsAConfiguredCommandThatIsNotFound(t *testing.T) {
	v := verify.Verifier{Cfg: config.Verify{
		Commands: []string{"orch-no-such-command-tue", "touch commands-ran"},
		App:      config.VerifyApp{Start: "orch-no-such-command-tue"},
		E2E:      []string{"orch-no-such-command-tue", "touch e2e-ran"},
		TimeoutS: 10,
	}}
	dir := t.TempDir()
	if got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir}).Failure; got != "" {
		t.Errorf("Verify = %q, want a pass", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "commands-ran")); err != nil {
		t.Errorf("the commands after the missing one did not run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "e2e-ran")); err == nil {
		t.Error("the E2E ran without its app")
	}

	v.Cfg.App.Start = ""
	if got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir}).Failure; got != "" {
		t.Errorf("Verify with a missing E2E command = %q, want a pass", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "e2e-ran")); err != nil {
		t.Errorf("the E2E commands after the missing one did not run: %v", err)
	}
}

func TestVerifyBoundsEachCommand(t *testing.T) {
	v := verify.Verifier{Cfg: config.Verify{Commands: []string{"sleep 30"}, TimeoutS: 1}}
	if got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: t.TempDir()}).Failure; !strings.Contains(got, "timed out after 1s") {
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
	v := verify.Verifier{Cfg: config.Verify{TimeoutS: 10}}
	if got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir}).Failure; !strings.Contains(got, "detected-make-test") {
		t.Errorf("Verify = %q, want the detected make test to have run and failed", got)
	}
}

// Detected checks run only for the packages the change touches (#498): an
// app-only task is not sent back because the firmware's toolchain is missing
// on this machine. A file outside every package may affect them all.
func TestVerifyRunsOnlyTheTouchedPackagesChecks(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	dir := branchWithWork(t)
	write(t, filepath.Join(dir, "apps", "app", "Makefile"), "test:\n\t@true\n")
	write(t, filepath.Join(dir, "apps", "fw", "Makefile"), "test:\n\t@echo fw-toolchain-missing; exit 2\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "packages")
	gitIn(t, dir, "branch", "-f", "main")
	write(t, filepath.Join(dir, "apps", "app", "main.go"), "package main\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "app only")

	v := verify.Verifier{Cfg: config.Verify{TimeoutS: 10}, Base: "main"}
	req := engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir}
	if got := v.Verify(context.Background(), req).Failure; got != "" {
		t.Errorf("Verify = %q, want the app-only change to skip the firmware's checks", got)
	}

	write(t, filepath.Join(dir, "README.md"), "shared\n")
	gitIn(t, dir, "commit", "-q", "-am", "root file")
	if got := v.Verify(context.Background(), req).Failure; !strings.Contains(got, "fw-toolchain-missing") {
		t.Errorf("Verify = %q, want a file outside every package to run them all", got)
	}
}

// A configured command is told what changed, so a project's own verify script
// can scope itself (#498).
func TestVerifyTellsTheCommandsWhatChanged(t *testing.T) {
	dir := branchWithWork(t) // adds store.go only
	v := verify.Verifier{Cfg: config.Verify{Commands: []string{`test "$ORCH_CHANGED_FILES" = store.go`}, TimeoutS: 10}, Base: "main"}
	if got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir}).Failure; got != "" {
		t.Errorf("Verify = %q, want ORCH_CHANGED_FILES to list the change", got)
	}
}

func TestVerifySendsBackABlockingReview(t *testing.T) {
	dir := branchWithWork(t)
	fakeReviewer(t, "0", readFixture(t, "claude", "2.1.272", "review-verdict.json"))
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main", Once: engine.OneShot{}}

	got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendCodex, Dir: dir}).Failure
	for _, want := range []string{"the review asks for changes", "Tests: new behaviour comes with a test that exercises it"} {
		if !strings.Contains(got, want) {
			t.Errorf("Verify = %q, want it to contain %q", got, want)
		}
	}
}

// #463: every finding comes back structured, however long the reason the
// agent reads gets, with the reviewer that found them.
func TestVerifyReturnsEveryFindingStructured(t *testing.T) {
	dir := branchWithWork(t)
	var findings []ci.Finding
	for i := 1; i <= 12; i++ {
		findings = append(findings, ci.Finding{Severity: ci.SeverityBlocking, File: "store.go", Line: i,
			Title: "finding " + strconv.Itoa(i), Detail: strings.TrimSpace(strings.Repeat("why it matters ", 10))})
	}
	verdict, _ := json.Marshal(ci.Verdict{Verdict: ci.VerdictRequestChanges, Summary: "twelve problems", Findings: findings})
	answer, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": string(verdict)})
	fakeReviewer(t, "0", answer)
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", ReviewModel: "opus", TimeoutS: 30}, Base: "main", Once: engine.OneShot{}}

	res := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendCodex, Dir: dir})
	if !strings.Contains(res.Failure, "the review asks for changes") {
		t.Errorf("Failure = %q, want the review's request", res.Failure)
	}
	if len(res.Findings) != 12 || res.Findings[11] != engine.VerifyFinding(findings[11]) {
		t.Errorf("Findings = %+v, want all 12", res.Findings)
	}
	if res.Reviewer != "claude" || res.ReviewModel != "opus" {
		t.Errorf("reviewer = %q/%q, want claude/opus", res.Reviewer, res.ReviewModel)
	}
}

func TestVerifyPassesAnApprovingReview(t *testing.T) {
	dir := branchWithWork(t)
	fakeReviewer(t, "0", []byte(`{"type":"result","subtype":"success","is_error":false,"result":"{\"verdict\":\"approve\",\"summary\":\"fine\",\"findings\":[]}"}`))
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main", Once: engine.OneShot{}}

	if got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendCodex, Dir: dir}).Failure; got != "" {
		t.Errorf("Verify = %q, want a pass", got)
	}
}

// A reviewer that is down is not the agent's fault: the work passes.
func TestVerifyPassesWhenTheReviewCannotComplete(t *testing.T) {
	dir := branchWithWork(t)
	fakeReviewer(t, "1", []byte(`{"type":"result","subtype":"error","is_error":true,"terminal_reason":"api_error"}`))
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main", Once: engine.OneShot{}}

	if got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendCodex, Dir: dir}).Failure; got != "" {
		t.Errorf("Verify = %q, want a pass", got)
	}
}

// recordReviews has the fake reviewer copy each prompt it is given to
// <dir>/prompt-N.md, and returns dir. With swapTo set, the answer becomes
// swapTo's content after the first review.
func recordReviews(t *testing.T, swapTo string) string {
	t.Helper()
	seen := t.TempDir()
	fake := os.Getenv(engine.FakeProviderEnv)
	out := filepath.Join(fake, "claude", state.ReviewSpendID("T-1", 0)+".out")
	script := `n=$(ls "` + seen + `" | wc -l); cp ../prompt.md "` + seen + `/prompt-$((n+1)).md"` + "\n"
	if swapTo != "" {
		script += `cp "` + swapTo + `" "` + out + `"` + "\n"
	}
	write(t, filepath.Join(fake, "claude", state.ReviewSpendID("T-1", 0)+".sh"), script)
	return seen
}

func reviews(t *testing.T, seen string) []string {
	t.Helper()
	entries, err := os.ReadDir(seen)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(seen, e.Name())) // #nosec G304 -- the test's own temp dir
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	return out
}

const approveAnswer = `{"type":"result","subtype":"success","is_error":false,"result":"{\"verdict\":\"approve\",\"summary\":\"fine\",\"findings\":[]}"}`

// #539: the reviewer reads what the task asked for — its title, description
// and its section of the spec, Done when included — not only the diff.
func TestVerifyReviewSeesTheTask(t *testing.T) {
	dir := branchWithWork(t)
	write(t, filepath.Join(dir, "specs", "f1.md"), "# F1\n\n### F1.1.T1 — Store\n\nDone when: Load reads the saved rows.\n\n### F1.1.T2 — Other\n\nnot this task\n")
	fakeReviewer(t, "0", []byte(approveAnswer))
	seen := recordReviews(t, "")
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main", SpecRoot: "specs", Once: engine.OneShot{}}

	res := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendCodex, Dir: dir,
		Title: "Add the store", Description: "Persist the rows between runs", SpecRef: "f1.md#F1.1.T1"})
	if res.Failure != "" {
		t.Fatalf("Verify = %q, want a pass", res.Failure)
	}
	got := reviews(t, seen)
	if len(got) != 1 {
		t.Fatalf("reviews = %d, want 1", len(got))
	}
	for _, want := range []string{"Add the store", "Persist the rows between runs", "Done when: Load reads the saved rows."} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the review's prompt lacks %q:\n%s", want, got[0])
		}
	}
	if strings.Contains(got[0], "not this task") {
		t.Errorf("the review's prompt carries another task's section:\n%s", got[0])
	}
}

// #539: an answer that does not parse is asked again once; a second one that
// does not either is "review unavailable", said in the result, never an
// approval.
func TestVerifyUnparseableReviewIsUnavailableNotApproved(t *testing.T) {
	dir := branchWithWork(t)
	fakeReviewer(t, "0", []byte(`{"type":"result","subtype":"success","is_error":false,"result":"looks good to me!"}`))
	seen := recordReviews(t, "")
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main", Once: engine.OneShot{}}

	res := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendCodex, Dir: dir})
	if n := len(reviews(t, seen)); n != 2 {
		t.Errorf("reviews = %d, want a retry (2)", n)
	}
	if res.ReviewUnavailable == "" || res.Reviewer != "" {
		t.Errorf("Verify = %+v, want the review marked unavailable and no reviewer credited", res)
	}
}

// #539: a review that fails once and answers on the retry counts.
func TestVerifyRetriesAFailedReview(t *testing.T) {
	dir := branchWithWork(t)
	fakeReviewer(t, "1", []byte(`{"type":"result","subtype":"error","is_error":true,"terminal_reason":"api_error"}`))
	good := filepath.Join(t.TempDir(), "approve.out")
	write(t, good, approveAnswer)
	fake := os.Getenv(engine.FakeProviderEnv)
	write(t, filepath.Join(fake, "claude", state.ReviewSpendID("T-1", 0)+".exit"), "1")
	seen := recordReviews(t, good)
	// The exit code file is read before the script runs, so the retry's
	// exit is set by a second canned file the script swaps in too.
	write(t, filepath.Join(fake, "claude", state.ReviewSpendID("T-1", 0)+".sh"),
		readFile(t, filepath.Join(fake, "claude", state.ReviewSpendID("T-1", 0)+".sh"))+
			`echo 0 > "`+filepath.Join(fake, "claude", state.ReviewSpendID("T-1", 0)+".exit")+`"`+"\n")
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main", Once: engine.OneShot{}}

	res := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendCodex, Dir: dir})
	if n := len(reviews(t, seen)); n != 2 {
		t.Errorf("reviews = %d, want 2", n)
	}
	if res.ReviewUnavailable != "" || res.Reviewer != "claude" || res.Failure != "" {
		t.Errorf("Verify = %+v, want the retry's approval", res)
	}
}

// #539: a configured reviewer that wrote the task does not review it: another
// installed provider does, and with none the review is unavailable.
func TestConfiguredReviewerNeverReviewsItsOwnWork(t *testing.T) {
	dir := branchWithWork(t)
	bin := t.TempDir()
	for _, b := range []string{"claude", "gemini"} {
		write(t, filepath.Join(bin, b), "#!/bin/sh\n")
		if err := os.Chmod(filepath.Join(bin, b), 0o700); err != nil { // #nosec G302 -- an executable stub in the test's temp dir
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main", Once: engine.OneShot{}}
	if got := verify.Reviewer(v, model.BackendClaude); got != model.BackendGemini {
		t.Errorf("reviewer of claude's work with verify.reviewer claude = %q, want gemini", got)
	}
	if err := os.Remove(filepath.Join(bin, "gemini")); err != nil {
		t.Fatal(err)
	}
	if got := verify.Reviewer(v, model.BackendClaude); got != "" {
		t.Errorf("reviewer with no other provider = %q, want none", got)
	}

	res := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir})
	if res.ReviewUnavailable == "" {
		t.Errorf("Verify = %+v, want the review unavailable", res)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 G703 -- the test's own temp dir
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// approveWithUsage is claude approving, having spent 1200+300 tokens.
const approveWithUsage = `{"type":"result","subtype":"success","is_error":false,` +
	`"result":"{\"verdict\":\"approve\",\"summary\":\"fine\",\"findings\":[]}",` +
	`"usage":{"input_tokens":1200,"output_tokens":300}}`

func openState(t *testing.T) *state.SQLite {
	t.Helper()
	root := t.TempDir()
	db, _, err := state.Open(context.Background(), filepath.Join(root, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return state.NewSQLite(db, "p", root)
}

// #470: the review's dispatch writes one spend row, named after the task and
// its attempt, so the cost views and the reviewer's budget window see it.
func TestVerifyRecordsTheReviewsSpend(t *testing.T) {
	ctx := context.Background()
	dir := branchWithWork(t)
	b := openState(t)
	fakeReviewerFor(t, "T-1-verify-2", "0", []byte(approveWithUsage))
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main",
		Once: engine.OneShot{State: b}}

	if got := v.Verify(ctx, engine.VerifyRequest{TaskID: "T-1", Attempt: 2, Writer: model.BackendCodex, Dir: dir}).Failure; got != "" {
		t.Fatalf("Verify = %q, want a pass", got)
	}
	rows, err := b.AllSpend(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TaskID != "T-1-verify-2" || rows[0].Backend != "claude" || rows[0].TokensIn != 1200 {
		t.Errorf("spend = %+v, want one claude row for T-1-verify-2 with 1200 tokens in", rows)
	}
}

// #470: a reviewer over its budget window is not spawned; the review is
// skipped like one that could not run, and the work passes.
func TestVerifySkipsAReviewerOverItsBudget(t *testing.T) {
	ctx := context.Background()
	dir := branchWithWork(t)
	b := openState(t)
	if err := b.RecordSpend(ctx, state.Spend{TS: time.Now().UTC().Format("2006-01-02T15:04:05Z"), TaskID: "B-1", Backend: "claude", TokensIn: 100, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	fakeReviewer(t, "0", readFixture(t, "claude", "2.1.272", "review-verdict.json"))
	gate := budget.NewGate(b, &budget.Config{Providers: map[string]budget.ProviderBudget{
		"claude": {WindowHours: 5, TokenBudget: 10, ThresholdPct: 100},
	}})
	v := verify.Verifier{Cfg: config.Verify{Review: true, Reviewer: "claude", TimeoutS: 30}, Base: "main",
		Once: engine.OneShot{State: b, Gate: gate}}

	res := v.Verify(ctx, engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendCodex, Dir: dir})
	if res.Failure != "" || res.Reviewer != "" {
		t.Errorf("Verify = %+v, want a pass with no review", res)
	}
	if rows, err := b.AllSpend(ctx, time.Time{}); err != nil || len(rows) != 1 {
		t.Errorf("spend = %+v (%v), want only the row already there", rows, err)
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
	v := verify.Verifier{}
	if got := verify.Reviewer(v, model.BackendClaude); got != model.BackendGemini {
		t.Errorf("reviewer for claude's work = %q, want gemini", got)
	}
	if got := verify.Reviewer(v, model.BackendCodex); got != model.BackendClaude {
		t.Errorf("reviewer for codex's work = %q, want claude", got)
	}
	v.Cfg.Reviewer = "claude"
	if got := verify.Reviewer(v, model.BackendCodex); got != model.BackendClaude {
		t.Errorf("configured reviewer = %q, want claude", got)
	}
}

func TestChecklistPrefersTheProjectsOwn(t *testing.T) {
	dir := t.TempDir()
	if got := verify.Checklist(dir, ""); got != ci.BuiltinChecklist {
		t.Errorf("no checklist file: got %q, want the built-in one", got)
	}
	write(t, filepath.Join(dir, ".github", "orch-review.md"), "- github one\n")
	if got := verify.Checklist(dir, ""); got != "- github one\n" {
		t.Errorf("got %q, want .github/orch-review.md", got)
	}
	write(t, filepath.Join(dir, ".orch", "review.md"), "- the domain imports no adapter\n")
	if got := verify.Checklist(dir, ""); got != "- the domain imports no adapter\n" {
		t.Errorf("got %q, want .orch/review.md first", got)
	}
	write(t, filepath.Join(dir, "docs", "rules.md"), "- configured\n")
	if got := verify.Checklist(dir, "docs/rules.md"); got != "- configured\n" {
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
	fakeReviewerFor(t, state.ReviewSpendID("T-1", 0), exit, output)
}

// fakeReviewerFor is fakeReviewer for the review named id.
func fakeReviewerFor(t *testing.T, id, exit string, output []byte) {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "claude", id+".out"), string(output))
	write(t, filepath.Join(dir, "claude", id+".exit"), exit)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { // #nosec G703 -- paths under the test's own temp dirs
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil { // #nosec G703 -- paths under the test's own temp dirs
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
		if got := verify.IsTestFile(path); got != want {
			t.Errorf("verify.IsTestFile(%q) = %v, want %v", path, got, want)
		}
	}
}

// With dispatch.tdd on, a change with no test in it goes back to the agent,
// before any command runs; one with a test goes on to the commands.
func TestVerifyRequiresATestWhenTDDIsOn(t *testing.T) {
	dir := branchWithWork(t) // adds store.go only
	v := verify.Verifier{Cfg: config.Verify{Commands: []string{"touch commands-ran"}, TimeoutS: 10}, Base: "main", RequireTests: true}

	got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir}).Failure
	if !strings.Contains(got, "touches no test file") || !strings.Contains(got, "store.go") {
		t.Errorf("Verify = %q, want the missing test reported with the changed files", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "commands-ran")); err == nil {
		t.Error("the commands ran on a change that was already sent back")
	}

	write(t, filepath.Join(dir, "store_test.go"), "package store\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "test")
	if got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir}).Failure; got != "" {
		t.Errorf("Verify = %q, want a pass once a test is in the change", got)
	}

	v.RequireTests = false
	gitIn(t, dir, "rm", "-q", "store_test.go")
	gitIn(t, dir, "commit", "-q", "-m", "no test")
	if got := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir}).Failure; got != "" {
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

// appFixture is a server standing in for the task's app, already up, and a
// worktree whose start/stop commands leave a mark. The URL is written the
// way a project writes it, from the task's slot.
func appFixture(t *testing.T, status int) (verify.Verifier, engine.VerifyRequest, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	old := *verify.ReadyPoll
	*verify.ReadyPoll = 10 * time.Millisecond
	t.Cleanup(func() { *verify.ReadyPoll = old })

	v := verify.Verifier{Cfg: config.Verify{TimeoutS: 10, App: config.VerifyApp{
		Start:         "touch started",
		URL:           "http://127.0.0.1:$((BASE_PORT + ORCH_TASK_SLOT))",
		Stop:          "touch stopped",
		ReadyTimeoutS: 30, // room for a loaded machine; only a test that wants the timeout shortens it
	}}}
	req := engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: t.TempDir(),
		Env: append(os.Environ(), "ORCH_TASK_SLOT=2", "BASE_PORT="+strconv.Itoa(port-2))}
	return v, req, srv.URL
}

func TestVerifyRunsE2EAgainstTheTasksApp(t *testing.T) {
	v, req, want := appFixture(t, http.StatusNotFound) // up, if not serving / yet
	v.Cfg.E2E = []string{`test "$ORCH_APP_URL" = "` + want + `" && touch e2e-ran`}

	if got := v.Verify(context.Background(), req).Failure; got != "" {
		t.Fatalf("Verify = %q, want a pass", got)
	}
	for _, mark := range []string{"started", "e2e-ran", "stopped"} {
		if _, err := os.Stat(filepath.Join(req.Dir, mark)); err != nil {
			t.Errorf("%s: %v", mark, err)
		}
	}
}

func TestVerifySendsBackAnAppThatNeverAnswers(t *testing.T) {
	v, req, want := appFixture(t, http.StatusInternalServerError)
	v.Cfg.App.ReadyTimeoutS = 1
	v.Cfg.E2E = []string{"touch e2e-ran"}

	got := v.Verify(context.Background(), req).Failure
	// The last error is the 500, or, on a loaded machine, the deadline of a
	// request still waiting: either way the app was never ready, which is
	// what this checks. A 500 taken as ready would have passed the work.
	if !strings.Contains(got, "never answered at "+want) {
		t.Errorf("Verify = %q, want the app reported as never ready", got)
	}
	if _, err := os.Stat(filepath.Join(req.Dir, "e2e-ran")); err == nil {
		t.Error("the E2E ran against an app that was not ready")
	}
	if _, err := os.Stat(filepath.Join(req.Dir, "stopped")); err != nil {
		t.Errorf("the app was not stopped: %v", err)
	}
}

func TestVerifySendsBackAnAppThatDoesNotStart(t *testing.T) {
	v, req, _ := appFixture(t, http.StatusOK)
	v.Cfg.App.Start = "echo 'service web: build failed'; exit 1"

	got := v.Verify(context.Background(), req).Failure
	if !strings.Contains(got, "did not start") || !strings.Contains(got, "service web: build failed") {
		t.Errorf("Verify = %q, want the start failure and its output", got)
	}
	if _, err := os.Stat(filepath.Join(req.Dir, "stopped")); err != nil {
		t.Errorf("a half-started app was not stopped: %v", err)
	}
}

func TestVerifySendsBackAFailingE2E(t *testing.T) {
	v, req, _ := appFixture(t, http.StatusOK)
	v.Cfg.E2E = []string{"echo '1 failed: checkout pays with a card'; exit 1"}

	got := v.Verify(context.Background(), req).Failure
	if !strings.Contains(got, "against the running app") || !strings.Contains(got, "checkout pays with a card") {
		t.Errorf("Verify = %q, want the E2E failure", got)
	}
	if _, err := os.Stat(filepath.Join(req.Dir, "stopped")); err != nil {
		t.Errorf("the app was not stopped: %v", err)
	}
}

// TestVerifyCollectsARealPlaywrightReport feeds the captured report through
// an E2E command that writes it and fails, as `playwright test` does.
func TestVerifyCollectsARealPlaywrightReport(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("testdata", "playwright", "1.64.1", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "captured.json"), strings.ReplaceAll(string(raw), "/capture", dir))
	shots := filepath.Join(dir, "test-results", "shop-checkout-pays-with-a-card")
	for _, f := range []string{"test-failed-1.png", "video.webm", "error-context.md", "trace.zip"} {
		write(t, filepath.Join(shots, f), "bytes of "+f)
	}
	artifacts := filepath.Join(t.TempDir(), "attempt-1")
	v := verify.Verifier{Cfg: config.Verify{TimeoutS: 10, E2E: []string{"cp captured.json e2e-report.json; exit 1"}, E2EReport: "e2e-report.json"}}

	res := v.Verify(context.Background(), engine.VerifyRequest{TaskID: "T-1", Writer: model.BackendClaude, Dir: dir, ArtifactsDir: artifacts})

	for _, want := range []string{
		"1 of 2 E2E tests failed",
		"- checkout › pays with a card (tests/shop.spec.ts:13)",
		"last step: click Pagar",
		`Expected: "Pago aprobado"`,
		`Received: "Error 500"`,
		"screenshot: test-results/shop-checkout-pays-with-a-card/test-failed-1.png",
	} {
		if !strings.Contains(res.Failure, want) {
			t.Errorf("failure is missing %q:\n%s", want, res.Failure)
		}
	}
	if strings.Contains(res.Failure, "\x1b[") {
		t.Errorf("the failure kept terminal colours:\n%q", res.Failure)
	}
	if res.E2E == nil || res.E2E.Passed != 1 || res.E2E.Failed != 1 || res.E2E.Report != filepath.Join(artifacts, verify.ReportFile) {
		t.Fatalf("E2E = %+v, want 1 passed, 1 failed, the report under the artifacts dir", res.E2E)
	}

	b, err := os.ReadFile(res.E2E.Report) // #nosec G304 -- a temp dir this test made
	if err != nil {
		t.Fatal(err)
	}
	var rep verify.E2EReport
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Tests) != 2 || rep.Tests[0].Status != "passed" || rep.Tests[1].Status != "failed" {
		t.Fatalf("tests = %+v", rep.Tests)
	}
	failed := rep.Tests[1]
	if len(failed.Attachments) != 4 {
		t.Fatalf("attachments = %+v, want the four the test left", failed.Attachments)
	}
	for _, a := range failed.Attachments {
		if a.Path == "" {
			t.Errorf("%s was not copied", a.Name)
			continue
		}
		if _, err := os.Stat(filepath.Join(artifacts, filepath.FromSlash(a.Path))); err != nil {
			t.Errorf("%s: %v", a.Name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "e2e-report.json")); err == nil {
		t.Error("the Playwright report was left in the worktree, where the commit would pick it up")
	}
}

// The review checks the operator's rules whose area covers a changed file,
// and only those (#505).
func TestVerifyReviewCarriesTheRulesForTheChange(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".orch", "knowledge", "layering.md"),
		"---\nid: layering\ntitle: Banking layering\nkind: rule\nsource: operator\narea: [\"src/app/**\"]\n---\nNever call a provider from a controller.\n")
	v := verify.Verifier{Rules: true}
	in := verify.WithRules("checklist", verify.Rules(v, "T-1", dir, []string{"src/app/pay.ts"}))
	if !strings.Contains(in, "## Project rules") || !strings.Contains(in, "Never call a provider from a controller.") {
		t.Errorf("checklist for a change in the area:\n%s", in)
	}
	if out := verify.WithRules("checklist", verify.Rules(v, "T-1", dir, []string{"docs/x.md"})); out != "checklist" {
		t.Errorf("checklist for a change outside the area:\n%s", out)
	}
	if off := verify.Rules(verify.Verifier{}, "T-1", dir, []string{"src/app/pay.ts"}); off != nil {
		t.Errorf("rules with knowledge off = %v", off)
	}
}

// TestRunKillsTheWholeGroupOnTimeout (#541): a check that times out takes
// its children with it — a server verify.app.start left running, a test
// runner's workers — not only the sh orch started.
func TestRunKillsTheWholeGroupOnTimeout(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	_, err := verify.Run(context.Background(), dir, nil, `sleep 30 & echo $! > child.pid; wait`, 500*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("run = %v, want a timeout", err)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("run took %s: it waited on the orphaned child's output", took)
	}
	raw, rerr := os.ReadFile(filepath.Join(dir, "child.pid")) // #nosec G304 -- the test's own temp dir
	if rerr != nil {
		t.Fatal(rerr)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if perr != nil {
		t.Fatal(perr)
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the check's child %d outlived the timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
