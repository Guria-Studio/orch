// Package verify is orch's local gate between an agent's clean exit and the
// push (verify.enabled): the repository's own checks, then a read-only review
// by another provider. What fails goes back to the agent; only work that
// passes is published.
//
// It lives outside internal/engine, which sees it only as its Verifier
// interface; the types they trade are internal/contract's, so it does not
// import the engine (#550).
package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Guria-Studio/orch/internal/ci"
	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/contract"
	"github.com/Guria-Studio/orch/internal/knowledge"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
)

// maxDiffBytes cuts the reviewed diff, file by file: `orch ci review`'s default.
const maxDiffBytes = 200_000

// outputTail is how much of a failing command's output the agent is shown.
// The end is where test runners and compilers put the failure.
const outputTail = 8_000

// Verifier is the gate. The zero Cfg verifies nothing: callers build one only
// when verify.enabled is on.
type Verifier struct {
	Cfg config.Verify
	// Base is the branch tasks start from; the review reads the diff against
	// it (origin/<Base> when there is one, as the worktree started there).
	Base string
	// RequireTests sends back a change that touches no test file
	// (dispatch.tdd).
	RequireTests bool
	// Rules has the review check the operator's rules (knowledge files of
	// kind: rule) whose area covers a changed file (#505); knowledge.enabled.
	Rules bool
	// SpecRoot is spec_root, where the task's specRef is looked for; the
	// reviewer is shown the task's section of the spec (#539).
	SpecRoot string
	// Once runs the review, normally an engine.OneShot: with its State set
	// the review's spend is
	// recorded under state.ReviewSpendID, with its Gate a reviewer over its budget
	// window is refused before it spawns, and the review is skipped (#470).
	Once contract.Runner
}

// Verify checks the work in dir, a task's worktree with the agent's work
// committed. It returns what failed, written for the agent's next attempt, or
// "" when the work passes.
//
// A check that cannot run is not the agent's fault and does not fail it: a
// detected command whose tool is not installed is skipped, so is a configured
// one sh cannot find (a warning names its config key). A review that cannot
// complete (no other provider, a provider that is down or over its budget
// window, an answer that does not parse) is asked once more, then marked
// ReviewUnavailable: the work is not failed, and not counted as reviewed
// either (#539). A gate that blocks on its own breakage teaches people to
// turn it off.
func (v Verifier) Verify(ctx context.Context, req contract.VerifyRequest) contract.VerifyResult {
	res := v.check(ctx, req)
	// What was checked, for the task's evidence (#520), passed or not.
	res.HeadSHA, res.BaseSHA, res.Diff = v.describe(ctx, req.TaskID, req.Dir)
	return res
}

// check is Verify without the description of the change.
func (v Verifier) check(ctx context.Context, req contract.VerifyRequest) contract.VerifyResult {
	timeout := time.Duration(v.Cfg.TimeoutS) * time.Second
	changed := v.changedFiles(ctx, req.TaskID, req.Dir)
	if v.RequireTests {
		if failure := requireTests(changed); failure != "" {
			return contract.VerifyResult{Failure: failure}
		}
	}
	if len(changed) > 0 {
		// A project's own verify script can scope itself to the change (#498).
		req.Env = withVar(req.Env, "ORCH_CHANGED_FILES="+strings.Join(changed, "\n"))
	}
	var ran []contract.VerifyCommand
	for _, c := range v.commands(req.TaskID, req.Dir, changed) {
		if out, err := timed(&ran, c.command, c.kind, func() (string, error) { return run(ctx, req.Dir, req.Env, c.command, timeout) }); notFound(err) {
			skipNotFound(req.TaskID, "verify.commands", c.command, out)
		} else if err != nil {
			return contract.VerifyResult{Failure: commandFailed(c.command, err, out, ""), Commands: ran}
		}
	}
	res := v.e2e(ctx, req, timeout)
	res.Commands = append(ran, res.Commands...)
	if res.Failure != "" || !v.Cfg.Review || req.SkipReview {
		return res
	}
	res.Failure = v.review(ctx, req, timeout, &res, v.rules(req.TaskID, req.Dir, changed))
	return res
}

// timed runs f, the check command, and adds it to ran with its kind and how
// long it took unless sh could not find it.
func timed(ran *[]contract.VerifyCommand, command, kind string, f func() (string, error)) (string, error) {
	start := time.Now()
	out, err := f()
	if !notFound(err) {
		*ran = append(*ran, contract.VerifyCommand{Command: command, DurationS: time.Since(start).Round(time.Millisecond).Seconds(), Kind: kind})
	}
	return out, err
}

// kindE2E is the kind of a verify.e2e command.
const kindE2E = "e2e"

func commandFailed(command string, err error, out, context string) string {
	if strings.TrimSpace(out) == "" {
		// No empty code block for the agent to puzzle over (#598).
		return fmt.Sprintf("orch's local verify ran `%s` in your working dir%s and it failed (%v); the command printed nothing.\n",
			command, context, err)
	}
	return fmt.Sprintf("orch's local verify ran `%s` in your working dir%s and it failed (%v):\n\n```\n%s\n```\n",
		command, context, err, tail(out))
}

// e2e brings the task's app up (verify.app), waits for it to answer, runs
// verify.e2e against it with ORCH_APP_URL set, and takes it down again,
// whatever happened. An app that does not start or never answers is sent
// back like a failing test: most often it is the change that broke it, and
// when it is the machine instead, the agent can block the task and say so.
func (v Verifier) e2e(ctx context.Context, req contract.VerifyRequest, timeout time.Duration) contract.VerifyResult {
	app := v.Cfg.App
	env := req.Env
	appURL := ""
	if app.Start != "" {
		if app.Stop != "" {
			defer func() {
				// A fresh context: the app comes down even when ctx is done.
				if out, err := run(context.Background(), req.Dir, req.Env, app.Stop, timeout); err != nil {
					slog.Warn("verify: stopping the app failed", "task", req.TaskID, "err", err, "output", tail(out))
				}
			}()
		}
		if out, err := run(ctx, req.Dir, req.Env, app.Start, timeout); notFound(err) {
			// The E2E is for the app: without it there is nothing to run.
			skipNotFound(req.TaskID, "verify.app.start", app.Start, out)
			return contract.VerifyResult{}
		} else if err != nil {
			return contract.VerifyResult{Failure: "The task's app did not start. " + commandFailed(app.Start, err, out, "")}
		}
		if app.URL != "" {
			url, err := expandURL(ctx, req.Dir, req.Env, app.URL)
			if err != nil {
				slog.Warn("verify: expanding verify.app.url failed; not waiting for the app", "task", req.TaskID, "err", err)
			} else {
				ready := time.Duration(app.ReadyTimeoutS) * time.Second
				if err := waitReady(ctx, url, ready); err != nil {
					return contract.VerifyResult{Failure: fmt.Sprintf("orch's local verify started the task's app with `%s`, and it never answered at %s (%v). "+
						"Check that the change still builds and serves.\n", app.Start, url, err)}
				}
				env = withVar(env, "ORCH_APP_URL="+url)
				appURL = url
			}
		}
	}
	var probes []contract.VerifyProbe
	if len(app.Probes) > 0 {
		if appURL == "" {
			slog.Warn("verify: verify.app.probes need verify.app.start and a url that answers; not probing", "task", req.TaskID)
		} else if failure := probe(ctx, appURL, app.Probes, &probes); failure != "" {
			return contract.VerifyResult{Failure: failure, Probes: probes}
		}
	}
	report := ""
	if v.Cfg.E2EReport != "" {
		// A report left by an earlier attempt must not pass for this one's.
		report = filepath.Join(req.Dir, filepath.FromSlash(v.Cfg.E2EReport))
		_ = os.Remove(report)
	}
	res := contract.VerifyResult{Probes: probes}
	for _, c := range v.Cfg.E2E {
		if out, err := timed(&res.Commands, c, kindE2E, func() (string, error) { return run(ctx, req.Dir, env, c, timeout) }); notFound(err) {
			skipNotFound(req.TaskID, "verify.e2e", c, out)
		} else if err != nil {
			res.Failure = commandFailed(c, err, out, " against the running app")
			break
		}
	}
	if report == "" || len(v.Cfg.E2E) == 0 {
		return res
	}
	rep, err := collectE2E(report, req.Dir, req.ArtifactsDir, req.TaskID, appURL)
	if err != nil {
		slog.Warn("verify: the E2E report could not be read; keeping the command's verdict", "task", req.TaskID, "err", err)
		return res
	}
	res.E2E = &contract.E2ESummary{
		Passed: rep.Stats.Passed, Failed: rep.Stats.Failed, Flaky: rep.Stats.Flaky, Skipped: rep.Stats.Skipped,
		Report: filepath.Join(req.ArtifactsDir, ReportFile),
	}
	if res.Failure != "" && (rep.Stats.Failed > 0 || len(rep.Errors) > 0) {
		// The report says it better than the command's output.
		res.Failure = "orch's local verify ran the E2E tests. " + rep.failureSummary()
	}
	return res
}

// expandURL has the shell expand verify.app.url with the task's environment,
// so `$((3000 + ORCH_TASK_SLOT))` gives each task its own port.
func expandURL(ctx context.Context, dir string, env []string, url string) (string, error) {
	out, err := run(ctx, dir, env, `printf '%s' "`+strings.ReplaceAll(url, `"`, `\"`)+`"`, time.Minute)
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, tail(out))
	}
	return strings.TrimSpace(out), nil
}

// readyPoll is how often waitReady asks.
var readyPoll = time.Second

// waitReady polls url until it answers below 500, or ready runs out. A 404 is
// ready: the server is up, and the E2E tests say what is missing.
func waitReady(ctx context.Context, url string, ready time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, ready)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second}
	var last error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req) // #nosec G704 -- the operator's own verify.app.url
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			err = fmt.Errorf("it answered %s", resp.Status)
		}
		last = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("gave up after %s: %w", ready, last)
		case <-time.After(readyPoll):
		}
	}
}

// maxProbeBody is how much of an answer a probe reads.
const maxProbeBody = 1 << 20

// probe GETs each of probes at base and checks its status and body, in
// order, adding each result to out; it stops at the first that fails and
// returns what failed, written for the agent, or "" when all hold. Neither
// the result nor the failure names base: the URL is the operator's.
func probe(ctx context.Context, base string, probes []config.VerifyProbe, out *[]contract.VerifyProbe) string {
	client := &http.Client{Timeout: 30 * time.Second}
	for _, p := range probes {
		path := p.Path
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		res := contract.VerifyProbe{Path: path}
		res.Status, res.Detail = probeOnce(ctx, client, strings.TrimRight(base, "/")+path, p)
		res.OK = res.Detail == ""
		*out = append(*out, res)
		if !res.OK {
			return fmt.Sprintf("orch's local verify started the task's app and probed what it serves (verify.app.probes): "+
				"GET %s %s. The app answers, but not with what it should; check that the change builds and serves the page.\n", path, res.Detail)
		}
	}
	return ""
}

// probeOnce is one probe's status and, when it fails, why.
func probeOnce(ctx context.Context, client *http.Client, url string, p config.VerifyProbe) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Sprintf("could not be asked (%v)", err)
	}
	resp, err := client.Do(req) // #nosec G704 -- the operator's own verify.app.url
	if err != nil {
		return 0, fmt.Sprintf("got no answer (%v)", errors.Unwrap(err))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeBody))
	if err != nil {
		return resp.StatusCode, fmt.Sprintf("answered %d and its body could not be read (%v)", resp.StatusCode, err)
	}
	switch {
	case p.Status != 0 && resp.StatusCode != p.Status:
		return resp.StatusCode, fmt.Sprintf("answered %d, want %d", resp.StatusCode, p.Status)
	case p.Status == 0 && resp.StatusCode >= 400:
		return resp.StatusCode, fmt.Sprintf("answered %d, want a status below 400", resp.StatusCode)
	}
	for _, want := range p.Contains {
		if !strings.Contains(string(body), want) {
			return resp.StatusCode, fmt.Sprintf("answered %d without %q in the page", resp.StatusCode, want)
		}
	}
	for _, bad := range p.NotContains {
		if strings.Contains(string(body), bad) {
			return resp.StatusCode, fmt.Sprintf("answered %d with %q in the page", resp.StatusCode, bad)
		}
	}
	return resp.StatusCode, ""
}

// withVar is env with kv added, starting from this process's environment
// when env is nil (which exec reads as "inherit").
func withVar(env []string, kv string) []string {
	if env == nil {
		env = os.Environ()
	}
	return append(slices.Clone(env), kv)
}

// check is one command to run and its kind (tests, lint, install, …).
type check struct{ command, kind string }

// commands are the configured ones, their kind told from their words, or
// what `orch ci setup` detects for the packages the change touches.
func (v Verifier) commands(taskID, dir string, changed []string) []check {
	if len(v.Cfg.Commands) > 0 {
		out := make([]check, 0, len(v.Cfg.Commands))
		for _, c := range v.Cfg.Commands {
			out = append(out, check{c, ci.GateOf(c)})
		}
		return out
	}
	st, err := ci.Detect(dir)
	if err != nil {
		slog.Warn("verify: detecting the repository's checks failed; running none", "task", taskID, "err", err)
		return nil
	}
	var out []check
	for _, p := range scope(st.Packages, changed) {
		for _, c := range []check{{p.Install, "install"}, {p.Lint, ci.GateLint}, {p.Typecheck, ci.GateTypecheck}, {p.Test, ci.GateTests}, {p.Build, ci.GateBuild}} {
			if c.command == "" {
				continue
			}
			if tool, _, _ := strings.Cut(c.command, " "); !onPath(tool) {
				slog.Warn("verify: skipping a check whose tool is not installed", "task", taskID, "command", c.command)
				continue
			}
			if p.Dir != "." {
				c.command = "cd " + shellQuote(p.Dir) + " && " + c.command
			}
			out = append(out, c)
		}
	}
	return out
}

// run is `sh -c command` in dir with env (nil inherits this process's),
// bounded by timeout (checklist rule 18).
func run(ctx context.Context, dir string, env []string, command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// #nosec G204 -- the operator's own verify.commands, or commands detected from their repository.
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = env
	// Its own process group, killed whole on a timeout or cancel (#541):
	// killing only sh leaves the app a start command ran and a test runner's
	// workers alive, holding ports and the output pipe. Only then — a server
	// app.start left running on purpose stays up for app.stop.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ctx.Err() != nil {
		err = fmt.Errorf("timed out after %s", timeout)
	}
	return out.String(), err
}

// notFound reports whether sh could not find the command (exit 127): a typo
// or a tool missing from the machine, which the agent cannot fix (#433).
func notFound(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 127
}

func skipNotFound(taskID, key, command, out string) {
	slog.Warn("verify: a configured command was not found; skipping it, check "+key+" in config.yaml",
		"task", taskID, "command", command, "output", tail(out))
}

// review asks another provider to review the diff, and returns its blocking
// findings for the agent, or "". A review that completes names its reviewer
// and all its findings in res.
func (v Verifier) review(ctx context.Context, req contract.VerifyRequest, timeout time.Duration, res *contract.VerifyResult, rules []knowledge.Lesson) string {
	taskID, writer, dir := req.TaskID, req.Writer, req.Dir
	reviewer := v.reviewer(writer)
	if reviewer == "" {
		res.ReviewUnavailable = "no provider other than " + string(writer) + ", which wrote the task, is installed to review it"
		slog.Warn("verify: review unavailable; the work is not reviewed", "task", taskID, "reason", res.ReviewUnavailable)
		return ""
	}
	diff, err := v.diff(ctx, dir)
	if err != nil {
		slog.Warn("verify: reading the diff failed; skipping the review", "task", taskID, "err", err)
		return ""
	}
	if strings.TrimSpace(diff) == "" {
		return ""
	}
	diff, cut := ci.TruncateDiff(diff, maxDiffBytes)
	prompt := ci.BuildPromptFor(v.taskBrief(req), withRules(checklist(dir, v.Cfg.Checklist), rules), diff, cut)
	// A review that fails or does not parse is asked once more; a second
	// failure is "review unavailable", never an approval (#539).
	var verdict ci.Verdict
	for try := 1; ; try++ {
		answer, err := ci.AskOnce(ctx, v.Once, state.ReviewSpendID(taskID, req.Attempt),
			model.RouteEntry{Backend: reviewer, CLIModel: v.Cfg.ReviewModel}, prompt, timeout)
		if err == nil {
			if verdict, err = ci.ParseVerdict(answer); err == nil {
				break
			}
			err = fmt.Errorf("the answer did not parse: %w", err)
		}
		if try == 2 {
			res.ReviewUnavailable = fmt.Sprintf("%s's review failed twice: %v", reviewer, err)
			slog.Warn("verify: review unavailable; the work is not reviewed", "task", taskID, "reason", res.ReviewUnavailable)
			return ""
		}
		slog.Warn("verify: the review failed; asking once more", "task", taskID, "reviewer", reviewer, "err", err)
	}
	res.Reviewer, res.ReviewModel = string(reviewer), v.Cfg.ReviewModel
	for _, f := range verdict.Findings {
		res.Findings = append(res.Findings, contract.VerifyFinding(f))
	}
	if !verdict.Blocks() {
		return ""
	}
	return "orch's local verify had your work reviewed and the review asks for changes. " +
		"Fix the blocking findings:\n\n" + verdict.Markdown(string(reviewer), nil)
}

// reviewer is verify.reviewer, or the first installed review backend that did
// not write the task: a model reviewing its own work misses what it missed.
// A configured reviewer that wrote the task gives way to another (#539); ""
// when there is none.
func (v Verifier) reviewer(writer model.Backend) model.Backend {
	if v.Cfg.Reviewer != "" && model.Backend(v.Cfg.Reviewer) != writer {
		return model.Backend(v.Cfg.Reviewer)
	}
	if v.Cfg.Reviewer != "" {
		slog.Warn("verify: verify.reviewer wrote this task, and does not review its own work; looking for another provider",
			"reviewer", v.Cfg.Reviewer)
	}
	for _, b := range ci.ReviewBackends {
		if b != writer && onPath(string(b)) {
			return b
		}
	}
	return ""
}

// taskBrief is what the reviewer checks the change against: the task's title,
// description and its section of the spec, read from the task's worktree.
func (v Verifier) taskBrief(req contract.VerifyRequest) string {
	var b strings.Builder
	if req.Title != "" {
		fmt.Fprintf(&b, "**%s** (%s)\n\n", req.Title, req.TaskID)
	}
	if d := strings.TrimSpace(req.Description); d != "" {
		b.WriteString(d + "\n\n")
	}
	section, err := project.SpecSection(req.Dir, v.SpecRoot, req.SpecRef)
	if err != nil {
		slog.Warn("verify: reading the task's spec failed; reviewing without it", "task", req.TaskID, "err", err)
	}
	if section = strings.TrimSpace(section); section != "" {
		if len(section) > maxSpecBytes {
			section = section[:maxSpecBytes] + "\n…(cut)"
		}
		fmt.Fprintf(&b, "From the spec (%s):\n\n%s\n", req.SpecRef, section)
	}
	return b.String()
}

// maxSpecBytes bounds the spec section the reviewer is shown.
const maxSpecBytes = 20_000

// diff is what the task changed, against its base.
func (v Verifier) diff(ctx context.Context, dir string) (string, error) {
	return ci.Diff(ctx, dir, v.baseRef(ctx, dir))
}

// baseRef is origin/<Base> when the clone has it, as the worktree started
// there, and <Base> otherwise.
func (v Verifier) baseRef(ctx context.Context, dir string) string {
	base := v.Base
	if base == "" {
		base = "main"
	}
	if _, err := run(ctx, dir, nil, "git rev-parse --verify --quiet "+shellQuote("origin/"+base+"^{commit}"), time.Minute); err == nil {
		return "origin/" + base
	}
	return base
}

// scope keeps the packages a change touches (#498): each changed file
// belongs to the deepest package that holds it, so an app-only change does not
// run, and fail on, the firmware's toolchain. A file outside every package
// (shared code, a root config) may affect any of them and keeps them all, as
// a change that could not be listed does.
func scope(pkgs []ci.Package, changed []string) []ci.Package {
	if len(changed) == 0 {
		return pkgs
	}
	keep := map[string]bool{}
	for _, f := range changed {
		owner, depth := "", -1
		for _, p := range pkgs {
			n := 0
			if p.Dir != "." {
				if !strings.HasPrefix(f, p.Dir+"/") {
					continue
				}
				n = len(p.Dir)
			}
			if n > depth {
				owner, depth = p.Dir, n
			}
		}
		if depth < 0 {
			return pkgs
		}
		keep[owner] = true
	}
	return slices.DeleteFunc(slices.Clone(pkgs), func(p ci.Package) bool { return !keep[p.Dir] })
}

// maxTopFiles is how many of the most-changed files describe a change.
const maxTopFiles = 5

// describe is the commit verified, the base commit its branch left from,
// and the change between them in numbers; empty when git cannot say (not a
// repository, no base).
func (v Verifier) describe(ctx context.Context, taskID, dir string) (head, base string, diff *contract.DiffStat) {
	ref := v.baseRef(ctx, dir)
	if out, err := run(ctx, dir, nil, "git rev-parse HEAD", time.Minute); err == nil {
		head = strings.TrimSpace(out)
	}
	if out, err := run(ctx, dir, nil, "git merge-base HEAD "+shellQuote(ref), time.Minute); err == nil {
		base = strings.TrimSpace(out)
	}
	out, err := run(ctx, dir, nil, "git diff --numstat --merge-base "+shellQuote(ref)+" --", time.Minute)
	if err != nil {
		slog.Debug("verify: the change could not be counted", "task", taskID, "err", err)
		return head, base, nil
	}
	type file struct {
		name    string
		changed int
	}
	var files []file
	diff = &contract.DiffStat{}
	for _, line := range strings.Split(out, "\n") {
		// added<TAB>deleted<TAB>path; a binary file counts "-" for both.
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 {
			continue
		}
		added, _ := strconv.Atoi(f[0])
		deleted, _ := strconv.Atoi(f[1])
		diff.Files++
		diff.Insertions += added
		diff.Deletions += deleted
		files = append(files, file{f[2], added + deleted})
	}
	slices.SortStableFunc(files, func(a, b file) int { return b.changed - a.changed })
	for i := 0; i < len(files) && i < maxTopFiles; i++ {
		diff.Top = append(diff.Top, files[i].name)
	}
	return head, base, diff
}

// changedFiles is what the task changed against its base, as git prints the
// paths; nil when nothing changed or the change cannot be listed.
func (v Verifier) changedFiles(ctx context.Context, taskID, dir string) []string {
	out, err := run(ctx, dir, nil, "git diff --name-only --merge-base "+shellQuote(v.baseRef(ctx, dir))+" --", time.Minute)
	if err != nil {
		slog.Warn("verify: listing the changed files failed; checking every package, and not for tests", "task", taskID, "err", err)
		return nil
	}
	var changed []string
	for _, f := range strings.Split(out, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			changed = append(changed, f)
		}
	}
	return changed
}

// requireTests sends back a change that touches no test file. An empty
// change passes here: the other checks decide what nothing means.
func requireTests(changed []string) string {
	if len(changed) == 0 || slices.ContainsFunc(changed, IsTestFile) {
		return ""
	}
	if len(changed) > 10 {
		changed = append(slices.Clone(changed[:10]), "…")
	}
	return "dispatch.tdd is on, and your change touches no test file (it changes " + strings.Join(changed, ", ") + "). " +
		"Write a test for each acceptance criterion with the framework the repository already uses, " +
		"see it fail for the expected reason, then make it pass.\n"
}

// IsTestFile reports whether a path, as git prints it, is a test by the
// naming conventions of the ecosystems orch detects: Go's _test.go, the
// .test./.spec. of JavaScript and TypeScript, pytest's test_*.py and
// *_test.py, the *Test/*Tests classes of the JVM and .NET, and any file under
// a tests/, test/, __tests__/ or spec/ directory.
func IsTestFile(p string) bool {
	base := path.Base(p)
	stem := strings.TrimSuffix(base, path.Ext(base))
	switch {
	case strings.HasSuffix(base, "_test.go"),
		strings.Contains(base, ".test."), strings.Contains(base, ".spec."),
		strings.HasSuffix(base, ".py") && (strings.HasPrefix(base, "test_") || strings.HasSuffix(stem, "_test")),
		strings.HasSuffix(stem, "Test"), strings.HasSuffix(stem, "Tests"):
		return true
	}
	for _, dir := range strings.Split(path.Dir(p), "/") {
		switch dir {
		case "tests", "test", "__tests__", "spec":
			return true
		}
	}
	return false
}

// checklist is the configured file, else .orch/review.md, else
// .github/orch-review.md, read from the task's worktree so it is the one on
// the task's branch; else the built-in checklist.
func checklist(dir, configured string) string {
	candidates := []string{".orch/review.md", ".github/orch-review.md"}
	if configured != "" {
		candidates = []string{configured}
	}
	for _, c := range candidates {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path.Clean(c)))) // #nosec G304 -- a checklist path in the operator's own repository
		if err == nil && strings.TrimSpace(string(b)) != "" {
			return string(b)
		}
	}
	return ci.BuiltinChecklist
}

// rules are the operator's rules whose area covers one of changed, read from
// the task's worktree; none when Rules is off or they cannot be read.
func (v Verifier) rules(taskID, dir string, changed []string) []knowledge.Lesson {
	if !v.Rules || len(changed) == 0 {
		return nil
	}
	all, err := knowledge.Load(dir)
	if err != nil {
		slog.Warn("verify: reading the project's rules failed; reviewing without them", "task", taskID, "err", err)
		return nil
	}
	return knowledge.Rules(all, changed)
}

// withRules appends rules to the review's checklist (#505): the reviewer is
// who checks a change against them.
func withRules(list string, rules []knowledge.Lesson) string {
	if len(rules) == 0 {
		return list
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(list, "\n"))
	b.WriteString("\n\n## Project rules\n\nThe operator's rules for the files this change touches (.orch/knowledge/). " +
		"A change that breaks one is a blocking finding.\n")
	for _, r := range rules {
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", r.Title, strings.TrimSpace(r.Body))
	}
	return b.String()
}

func onPath(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

func tail(s string) string {
	if len(s) <= outputTail {
		return strings.TrimRight(s, "\n")
	}
	return "…" + strings.TrimRight(s[len(s)-outputTail:], "\n")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
