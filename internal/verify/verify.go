// Package verify is orch's local gate between an agent's clean exit and the
// push (verify.enabled): the repository's own checks, then a read-only review
// by another provider. What fails goes back to the agent; only work that
// passes is published.
//
// It lives outside internal/engine because it drives the review through
// internal/ci, which imports the engine; the engine sees it only as its
// Verifier interface.
package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hectorcanaimero/orch/internal/ci"
	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/model"
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
}

// Verify checks the work in dir, a task's worktree with the agent's work
// committed. It returns what failed, written for the agent's next attempt, or
// "" when the work passes.
//
// A check that cannot run is not the agent's fault and does not fail it: a
// detected command whose tool is not installed is skipped, and a review that
// cannot complete (no other provider, a provider that is down, an answer that
// does not parse) is logged and passes, as `orch ci review` without
// --blocking does. A gate that blocks on its own breakage teaches people to
// turn it off.
func (v Verifier) Verify(ctx context.Context, taskID string, writer model.Backend, dir string) string {
	timeout := time.Duration(v.Cfg.TimeoutS) * time.Second
	for _, c := range v.commands(taskID, dir) {
		if out, err := run(ctx, dir, c, timeout); err != nil {
			return fmt.Sprintf("orch's local verify ran `%s` in your working dir and it failed (%v):\n\n```\n%s\n```\n",
				c, err, tail(out))
		}
	}
	if !v.Cfg.Review {
		return ""
	}
	return v.review(ctx, taskID, writer, dir, timeout)
}

// commands are the configured ones, or what `orch ci setup` detects.
func (v Verifier) commands(taskID, dir string) []string {
	if len(v.Cfg.Commands) > 0 {
		return v.Cfg.Commands
	}
	st, err := ci.Detect(dir)
	if err != nil {
		slog.Warn("verify: detecting the repository's checks failed; running none", "task", taskID, "err", err)
		return nil
	}
	var out []string
	for _, p := range st.Packages {
		for _, c := range []string{p.Install, p.Lint, p.Typecheck, p.Test, p.Build} {
			if c == "" {
				continue
			}
			if tool, _, _ := strings.Cut(c, " "); !onPath(tool) {
				slog.Warn("verify: skipping a check whose tool is not installed", "task", taskID, "command", c)
				continue
			}
			if p.Dir != "." {
				c = "cd " + shellQuote(p.Dir) + " && " + c
			}
			out = append(out, c)
		}
	}
	return out
}

// run is `sh -c command` in dir, bounded by timeout (checklist rule 18).
func run(ctx context.Context, dir, command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// #nosec G204 -- the operator's own verify.commands, or commands detected from their repository.
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ctx.Err() != nil {
		err = fmt.Errorf("timed out after %s", timeout)
	}
	return out.String(), err
}

// review asks another provider to review the diff, and returns its blocking
// findings for the agent, or "".
func (v Verifier) review(ctx context.Context, taskID string, writer model.Backend, dir string, timeout time.Duration) string {
	reviewer := v.reviewer(writer)
	if reviewer == "" {
		slog.Warn("verify: no other provider is installed to review; skipping the review", "task", taskID, "writer", writer)
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
	answer, err := ci.Ask(ctx, reviewer, v.Cfg.ReviewModel, ci.BuildPrompt(checklist(dir, v.Cfg.Checklist), diff, cut), timeout)
	if err != nil {
		slog.Warn("verify: the review did not complete; passing the work", "task", taskID, "reviewer", reviewer, "err", err)
		return ""
	}
	verdict, err := ci.ParseVerdict(answer)
	if err != nil {
		slog.Warn("verify: the review's answer did not parse; passing the work", "task", taskID, "reviewer", reviewer, "err", err)
		return ""
	}
	if !verdict.Blocks() {
		return ""
	}
	return "orch's local verify had your work reviewed and the review asks for changes. " +
		"Fix the blocking findings:\n\n" + verdict.Markdown(string(reviewer), nil)
}

// reviewer is verify.reviewer, or the first installed review backend that did
// not write the task: a model reviewing its own work misses what it missed.
func (v Verifier) reviewer(writer model.Backend) model.Backend {
	if v.Cfg.Reviewer != "" {
		return model.Backend(v.Cfg.Reviewer)
	}
	for _, b := range ci.ReviewBackends {
		if b != writer && onPath(string(b)) {
			return b
		}
	}
	return ""
}

// diff is what the task changed, against origin/<Base> when the remote has
// it and <Base> otherwise.
func (v Verifier) diff(ctx context.Context, dir string) (string, error) {
	base := v.Base
	if base == "" {
		base = "main"
	}
	d, err := ci.Diff(ctx, dir, "origin/"+base)
	if err == nil {
		return d, nil
	}
	d, err2 := ci.Diff(ctx, dir, base)
	if err2 != nil {
		return "", errors.Join(err, err2)
	}
	return d, nil
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
