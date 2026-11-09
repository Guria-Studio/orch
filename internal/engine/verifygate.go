package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Guria-Studio/orch/internal/contract"
	bundle "github.com/Guria-Studio/orch/internal/evidence"
	"github.com/Guria-Studio/orch/internal/model"
)

// EventVerifyFailed records a local verify that failed an attempt: the
// reason the agent reads, and the review's findings uncut (#463).
const EventVerifyFailed = "verify_failed"

// EventVerifyPassed records a local verify that passed an attempt: the
// commands it ran and the reviewer that read the diff (#463).
const EventVerifyPassed = "verify_passed"

// EventE2EReport records the E2E results of one verified attempt: the counts,
// and the report the dashboard opens.
const EventE2EReport = "e2e_report"

// defaultVerifyMaxAttempts is verify.max_attempts when it is unset or below 1.
const defaultVerifyMaxAttempts = 2

// Verifier checks an agent's work in its worktree after a clean exit and
// before the push (verify.enabled). It returns what failed, written for the
// agent's next attempt, or "" when the work passes. internal/verify is the
// implementation; its request and result types live in internal/contract so
// it does not import this package.
type Verifier interface {
	Verify(ctx context.Context, req VerifyRequest) VerifyResult
}

// VerifyResult is the verdict on one attempt's work.
type VerifyResult = contract.VerifyResult

// VerifyCommand is one check the verify ran.
type VerifyCommand = contract.VerifyCommand

// VerifyFinding is one review finding, as internal/ci's Finding has it.
type VerifyFinding = contract.VerifyFinding

// E2ESummary is an E2E run in counts, and where its full report is.
type E2ESummary = contract.E2ESummary

// VerifyRequest is one clean exit's work to verify.
type VerifyRequest = contract.VerifyRequest

// verifies reports whether a clean exit's work is checked before the push.
func (s *Scheduler) verifies() bool {
	return s.Verifier != nil && s.Worktree != nil && s.Opts.WorktreeMode
}

// verifyOutcome runs the local verify on a clean exit, from the dispatch's
// supervising goroutine: tests and a review take minutes, and the run loop
// must not wait on them. The work is committed first, so the review reads it
// as the diff the push would publish. A task landed here (vcs.provider:
// local) has the base merged into its worktree first when it moved and
// merges cleanly, so what is verified is what would land (#522). skipReview
// is the re-verify after the base moved again: the checks run, the LLM
// review that already read the agent's diff does not.
func (s *Scheduler) verifyOutcome(task model.Task, d Dispatch, attempt int, out *Outcome, skipReview bool) {
	if !out.Result.Success || !s.verifies() {
		return
	}
	ctx := context.Background()
	s.commitWork(ctx, d.Req.TaskID, d.Cwd)
	baseMoved := s.Opts.LocalMerge && s.catchUpBase(ctx, d.Req.TaskID)
	res := s.Verifier.Verify(ctx, VerifyRequest{
		TaskID: d.Req.TaskID, Attempt: attempt, Writer: d.Req.Route.Backend, Dir: d.Cwd, Env: d.Env,
		Title: task.Title, Description: task.Description, SpecRef: task.SpecRef,
		ArtifactsDir: E2EArtifactsDir(s.Opts.StateDir, s.Opts.RunID, d.Req.TaskID, attempt),
		SkipReview:   skipReview,
	})
	out.VerifyFailure, out.E2E, out.Verify = res.Failure, res.E2E, &res
	out.VerifyBaseMoved = out.VerifyBaseMoved || baseMoved
}

// baseChecker is the worktree manager's view of the base (#522):
// managerAdapter has it, test doubles that do not are treated as never
// behind, which is how every run behaved before.
type baseChecker interface {
	BehindBase(ctx context.Context, taskID, baseBranch string) (behind bool, conflicts []string, err error)
}

// baseMovedCleanly reports whether the base has commits the task's branch
// lacks and would merge into it without a conflict. A conflict is left to
// the land, which hands it to the agent (#438).
func (s *Scheduler) baseMovedCleanly(ctx context.Context, taskID string) bool {
	bc, ok := s.Worktree.(baseChecker)
	if !ok {
		return false
	}
	behind, conflicts, err := bc.BehindBase(ctx, taskID, s.baseBranch())
	if err != nil {
		s.logger().Warn("checking whether the base moved failed; verifying the work as it is", "task", taskID, "err", err)
		return false
	}
	return behind && len(conflicts) == 0
}

// catchUpBase merges a base that moved cleanly into the task's worktree, and
// reports whether it did.
func (s *Scheduler) catchUpBase(ctx context.Context, taskID string) bool {
	if !s.baseMovedCleanly(ctx, taskID) {
		return false
	}
	res, err := s.Worktree.MergeBase(ctx, taskID, s.baseBranch())
	if err != nil || len(res.Conflicts) > 0 {
		s.logger().Warn("merging the moved base into the worktree failed; verifying the work as it is",
			"task", taskID, "conflicts", res.Conflicts, "err", err)
		return false
	}
	s.logger().Info("the base moved while the task worked; merged it in before the verify", "task", taskID, "base", s.baseBranch())
	return res.Moved
}

// reverifyBeforeLand catches a local land whose verify passed while the base
// moved on (#522): another task landed during the verify, and the verified
// tree is no longer the one that would land. The dispatch stays in flight —
// its slot and lock held, as through the first verify — while a goroutine
// merges the base in, runs the checks again without the review, and posts
// the outcome back for the reaper. Reports whether it did; the reaper then
// leaves the completion alone.
//
// Each round needs another land in between, and lands take turns in this
// reaper, so the rounds end when the other tasks do.
func (s *Scheduler) reverifyBeforeLand(ctx context.Context, c completion, entry *InFlight) bool {
	out := c.outcome
	id := entry.Task.ID
	if !s.Opts.LocalMerge || !s.verifies() || !out.Result.Success || out.Verify == nil ||
		out.VerifyFailure != "" || out.GuardBlock != "" {
		return false
	}
	if _, stopping := s.stopping[c.pid]; stopping || s.taskStatusIs(ctx, id, model.StatusBlocked) {
		return false
	}
	if !s.baseMovedCleanly(ctx, id) {
		return false
	}
	s.logger().Info("the base moved during the verify; verifying again with it merged in", "task", id, "base", s.baseBranch())
	reviewed := *out.Verify
	go func() {
		s.verifyOutcome(entry.Task, entry.Dispatch, entry.Attempt, &out, true)
		if v := out.Verify; v != nil && v.Failure == "" {
			// The pass stands on the attempt's own review.
			v.Reviewer, v.ReviewModel, v.ReviewUnavailable, v.Findings =
				reviewed.Reviewer, reviewed.ReviewModel, reviewed.ReviewUnavailable, reviewed.Findings
		}
		s.done <- completion{pid: c.pid, outcome: out}
	}()
	return true
}

// recordVerify records a verified attempt's verdict: verify_passed, or
// verify_failed with the reason and the findings, sent back or not.
func (s *Scheduler) recordVerify(ctx context.Context, entry *InFlight, res *VerifyResult, baseMoved, sentBack bool) {
	if res == nil {
		return
	}
	manifest := s.recordEvidence(entry, res)
	if s.Backend == nil {
		return
	}
	id := entry.Task.ID
	event, extra := EventVerifyPassed, map[string]any{
		"attempt": s.verifyAttempts[id] + 1, "commands": res.Commands,
		"reviewer": res.Reviewer, "review_model": res.ReviewModel,
	}
	if res.Failure != "" {
		event, extra = EventVerifyFailed, map[string]any{
			"attempt": s.verifyAttempts[id] + 1, "reason": truncateReason(res.Failure), "sent_back": sentBack,
		}
	}
	if len(res.Findings) > 0 {
		extra["findings"] = res.Findings
	}
	if baseMoved {
		extra["base_moved"] = true
	}
	if res.ReviewUnavailable != "" {
		extra["review_unavailable"] = res.ReviewUnavailable
	}
	if manifest != "" {
		extra["evidence"] = manifest
	}
	if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, event, id, string(entry.Route.Backend), extra); err != nil {
		s.logger().Error("recording the verify result failed", "task", id, "err", err)
	}
}

// recordEvidence writes a verified attempt's evidence manifest into the
// task's history (#520) and returns its path under the state directory, ""
// when it could not be written. Work that passed is kept on entry, for
// promoteEvidence once it is pushed.
func (s *Scheduler) recordEvidence(entry *InFlight, res *VerifyResult) string {
	if s.Opts.StateDir == "" {
		return ""
	}
	id := entry.Task.ID
	m := bundle.Build(s.Opts.StateDir, bundle.Attempt{
		TaskID: id, Title: entry.Task.Title, RunID: s.Opts.RunID, Attempt: entry.Attempt, At: s.now(),
		ArtifactsDir: E2EArtifactsDir(s.Opts.StateDir, s.Opts.RunID, id, entry.Attempt),
	}, *res)
	rel, err := bundle.Record(s.Opts.StateDir, m)
	if err != nil {
		s.logger().Error("writing the task's evidence failed", "task", id, "err", err)
		return ""
	}
	if m.Verdict != bundle.VerdictFailed {
		entry.Evidence = &m
	}
	return rel
}

// promoteEvidence makes the pushed attempt's manifest the task's evidence,
// at <state>/evidence/<task>/evidence.json (#520).
func (s *Scheduler) promoteEvidence(entry *InFlight) {
	if entry.Evidence == nil || s.Opts.StateDir == "" {
		return
	}
	if err := bundle.Promote(s.Opts.StateDir, *entry.Evidence); err != nil {
		s.logger().Error("writing the task's evidence failed", "task", entry.Task.ID, "err", err)
	}
}

// E2EArtifactsDir is where one attempt's E2E report and files are kept:
// <state>/e2e/<run>/<task>/attempt-<n>.
func E2EArtifactsDir(stateDir, runID, taskID string, attempt int) string {
	return filepath.Join(stateDir, "e2e", runID, taskID, fmt.Sprintf("attempt-%d", attempt))
}

// recordE2E records an attempt's E2E results, whatever its verdict.
func (s *Scheduler) recordE2E(ctx context.Context, entry *InFlight, e *E2ESummary) {
	if e == nil || s.Backend == nil {
		return
	}
	if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, EventE2EReport, entry.Task.ID,
		string(entry.Route.Backend), map[string]any{
			"attempt": entry.Attempt,
			"passed":  e.Passed, "failed": e.Failed, "flaky": e.Flaky, "skipped": e.Skipped,
			"report": e.Report,
		}); err != nil {
		s.logger().Error("recording the E2E report failed", "task", entry.Task.ID, "err", err)
	}
}

// commitWork commits what the agent left in its worktree, minus the feedback
// file it was given: that is for the agent, not the branch.
func (s *Scheduler) commitWork(ctx context.Context, taskID, workdir string) {
	if err := os.Remove(filepath.Join(workdir, CIFeedbackFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.logger().Error("worktree: removing the feedback file failed", "task", taskID, "err", err)
	}
	if err := s.Worktree.CommitPending(ctx, taskID); err != nil {
		s.logger().Error("worktree: commit failed", "task", taskID, "err", err)
	}
}

// verifyMax is how many times a task is sent back before it is blocked.
func (s *Scheduler) verifyMax() int {
	if s.Opts.Cfg.Verify.MaxAttempts < 1 {
		return defaultVerifyMaxAttempts
	}
	return s.Opts.Cfg.Verify.MaxAttempts
}

// sendBack returns a task whose work failed the local verify to its agent:
// the worktree is kept with the work committed in it, the failure is left in
// the feedback file the prompt points at, and the retry runs there at once.
// Nothing was pushed.
func (s *Scheduler) sendBack(ctx context.Context, entry *InFlight, out Outcome) error {
	id := entry.Task.ID
	s.verifyAttempts[id]++
	s.keepRejection(id, out)
	if err := writeFeedback(entry.Dispatch.Cwd, "Local verify failed — please fix", out.VerifyFailure); err != nil {
		s.logger().Error("writing the verify feedback failed; the retry runs without it", "task", id, "err", err)
	}
	s.retryWorktree[id] = entry.Dispatch.Cwd
	s.logger().Warn("the local verify failed; sending the task back to its agent",
		"task", id, "attempt", s.verifyAttempts[id], "of", s.verifyMax())

	if s.Backend != nil {
		// The agent has most likely reported the task done already, and done
		// only reopens to todo.
		if err := transitionThroughTodo(ctx, s.Backend, id, model.StatusTodo, "local verify failed; sent back to its agent"); err != nil {
			s.logger().Error("reopening the task failed", "task", id, "err", err)
		}
	}
	if err := s.Queue.MarkTodo(id); err != nil {
		s.logger().Error("resetting to todo failed", "task", id, "err", err)
	}
	s.retryQueue = append(s.retryQueue, RetryItem{
		Task:       entry.Task,
		Route:      entry.Route,
		Attempt:    entry.Attempt + 1,
		EarliestAt: s.now(),
	})
	return nil
}

// reviewUnavailableSection tells the PR's reader the local review did not
// run, so the PR is not mistaken for reviewed work (#539).
func reviewUnavailableSection(reason string) string {
	if reason == "" {
		return ""
	}
	return "\n\n**Not reviewed by orch's local verify**: " + reason + ". Review this change yourself."
}

// verifyBlockReason is the block reason once the send-backs are used up.
func verifyBlockReason(failure string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(failure), "\n")
	return truncateReason(fmt.Sprintf("local verify still failing after the agent's retries: %s", first))
}

// writeFeedback leaves what failed in workdir for the agent's next attempt,
// under the file name the prompt's feedback block points at.
func writeFeedback(workdir, heading, body string) error {
	path := filepath.Join(workdir, CIFeedbackFile)
	if err := os.WriteFile(path, []byte("# "+heading+"\n\n"+body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
