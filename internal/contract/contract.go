// Package contract holds the types the engine trades with the packages it
// drives or is driven by — internal/ci and internal/verify — so they can name
// them without importing the engine (#550). It is a leaf: besides model and
// providers it imports nothing of orch's. The engine re-exports each type
// under its old name (engine.Dispatch is contract.Dispatch).
package contract

import (
	"context"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
)

// Dispatch is one task's run: which provider, with what, and for how long.
type Dispatch struct {
	// Provider runs the task. Required.
	Provider providers.Provider
	// Req is handed to the provider's Argv.
	Req providers.Request
	// PromptPath, LogPath and Cwd are as in engine.SpawnRequest.
	PromptPath string
	LogPath    string
	Cwd        string
	// Timeout is the wall-clock budget, normally from engine.TimeoutFor.
	Timeout time.Duration
	// Env, when non-nil, replaces the inherited environment.
	Env []string
}

// Outcome is everything one completed dispatch produced.
type Outcome struct {
	// Result is the provider's verdict, with the engine's own reasons
	// stamped on top where it owns them (a timeout overrides whatever the
	// CLI managed to print).
	Result providers.Result
	// Failure is the retry-policy class. Empty on success.
	Failure providers.Failure
	// PID is the child's pid, which is also its process-group id.
	PID int
	// StartedAt and Duration feed the spend row.
	StartedAt time.Time
	Duration  time.Duration
	// TimedOut is true when the engine killed the child rather than the
	// child exiting on its own.
	TimedOut bool
	// ActiveAtTimeout is true when a timed-out child's log was still growing
	// as the timeout fired: it was working, not hung (#529).
	ActiveAtTimeout bool
	// LogPath is where the captured output landed.
	LogPath string
	// Stopped is true when the operator stopped the attempt (#356), and
	// Held when they also held the task. A stopped attempt is cancelled, not
	// failed: it is neither classified nor retried by the retry policy.
	Stopped, Held bool
	// VerifyFailure is what the local verify found wrong with a clean exit's
	// work (verify.enabled), for the agent's next attempt. Empty when the
	// work passed or was not verified.
	VerifyFailure string
	// E2E is the attempt's E2E run, when the verify collected one.
	E2E *E2ESummary
	// Verify is the local verify's whole result, nil when it did not run.
	Verify *VerifyResult
	// GuardBlock is why the pre-PR guard (#484) refused the work, and
	// GuardWarnings what it let through for the PR body to name. Neither
	// ever holds a matched secret.
	GuardBlock    string
	GuardWarnings []string
}

// ShouldRetryWithFallback reports whether the reap loop should swap in the
// route's fallback_cli_model before retrying (FR-D-7). Port of the
// `should_retry_with_fallback` flag Python sets on a VERSION_DRIFT failure.
//
// Derived rather than stored: Python keeps it as a field on DispatchResult,
// and a field that must agree with Classify is a field that eventually does
// not.
func (o Outcome) ShouldRetryWithFallback(route model.RouteEntry) bool {
	return o.Failure == providers.FailureVersionDrift && route.FallbackCLIModel != nil
}

// Runner runs one dispatch outside the task loop. engine.OneShot is the
// implementation: the engine's spawn and supervision, its spend row and its
// budget gate.
type Runner interface {
	Run(ctx context.Context, d Dispatch) (Outcome, error)
}

// VerifyResult is the verdict on one attempt's work.
type VerifyResult struct {
	// Failure is what failed, written for the agent's next attempt; empty
	// when the work passes.
	Failure string
	// E2E summarises the E2E run, when verify.e2e_report collected one.
	E2E *E2ESummary
	// Commands are the checks that ran, in order, with how long each took.
	Commands []VerifyCommand
	// Reviewer and ReviewModel name who reviewed the diff; empty when no
	// review completed.
	Reviewer, ReviewModel string
	// Findings are the review's, blocking and minor.
	Findings []VerifyFinding
	// ReviewUnavailable says why a review that was asked for did not happen
	// (no other provider, or no answer that parsed after a retry). The work
	// is not failed for it, and not counted as reviewed either (#539).
	ReviewUnavailable string
	// HeadSHA and BaseSHA are the commit verified and where its branch
	// left the base; Diff is the change between them in numbers. Empty
	// when git could not say (#520).
	HeadSHA, BaseSHA string
	Diff             *DiffStat
}

// VerifyCommand is one check the verify ran.
type VerifyCommand struct {
	Command   string  `json:"command"`
	DurationS float64 `json:"duration_s"`
	// Kind is what sort of check it is: tests, typecheck, lint, build,
	// install, e2e, or "" when the command does not say (#520).
	Kind string `json:"kind,omitempty"`
}

// DiffStat is a change in numbers: files touched, lines added and removed,
// and the files that changed most, biggest first.
type DiffStat struct {
	Files      int      `json:"files"`
	Insertions int      `json:"insertions"`
	Deletions  int      `json:"deletions"`
	Top        []string `json:"top,omitempty"`
}

// VerifyFinding is one review finding, as internal/ci's Finding has it.
type VerifyFinding struct {
	Severity string `json:"severity"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
}

// E2ESummary is an E2E run in counts, and where its full report is.
type E2ESummary struct {
	Passed, Failed, Flaky, Skipped int
	// Report is the report file, under ArtifactsDir.
	Report string
}

// VerifyRequest is one clean exit's work to verify.
type VerifyRequest struct {
	TaskID string
	// Attempt is the task's dispatch attempt; the review's spend row is named
	// after it (#470).
	Attempt int
	// Writer is the backend that did the work; the review picks another.
	Writer model.Backend
	// Title, Description and SpecRef are the task's, for the reviewer to
	// check the change against (#539).
	Title, Description, SpecRef string
	// Dir is the task's worktree, with the work committed.
	Dir string
	// Env is the agent's own environment: the task's ORCH_TASK_SLOT and
	// COMPOSE_PROJECT_NAME, so the checks and the app run as the task's.
	Env []string
	// ArtifactsDir is where this attempt's E2E report and the files its
	// tests left are kept, under the state directory.
	ArtifactsDir string
}
