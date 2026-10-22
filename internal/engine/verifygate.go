package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hectorcanaimero/orch/internal/model"
)

// EventVerifyFailed records a local verify that sent a task back to its agent.
const EventVerifyFailed = "verify_failed"

// defaultVerifyMaxAttempts is verify.max_attempts when it is unset or below 1.
const defaultVerifyMaxAttempts = 2

// Verifier checks an agent's work in its worktree after a clean exit and
// before the push (verify.enabled). It returns what failed, written for the
// agent's next attempt, or "" when the work passes. internal/verify is the
// implementation; it cannot live here because it reviews through internal/ci,
// which imports this package.
type Verifier interface {
	Verify(ctx context.Context, taskID string, writer model.Backend, dir string) string
}

// verifies reports whether a clean exit's work is checked before the push.
func (s *Scheduler) verifies() bool {
	return s.Verifier != nil && s.Worktree != nil && s.Opts.WorktreeMode
}

// verifyOutcome runs the local verify on a clean exit, from the dispatch's
// supervising goroutine: tests and a review take minutes, and the run loop
// must not wait on them. The work is committed first, so the review reads it
// as the diff the push would publish.
func (s *Scheduler) verifyOutcome(d Dispatch, out *Outcome) {
	if !out.Result.Success || !s.verifies() {
		return
	}
	ctx := context.Background()
	s.commitWork(ctx, d.Req.TaskID, d.Cwd)
	out.VerifyFailure = s.Verifier.Verify(ctx, d.Req.TaskID, d.Req.Route.Backend, d.Cwd)
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
	if err := writeFeedback(entry.Dispatch.Cwd, "Local verify failed — please fix", out.VerifyFailure); err != nil {
		s.logger().Error("writing the verify feedback failed; the retry runs without it", "task", id, "err", err)
	}
	s.retryWorktree[id] = entry.Dispatch.Cwd
	s.logger().Warn("the local verify failed; sending the task back to its agent",
		"task", id, "attempt", s.verifyAttempts[id], "of", s.verifyMax())

	if s.Backend != nil {
		if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, EventVerifyFailed, id,
			string(entry.Route.Backend), map[string]any{
				"attempt": s.verifyAttempts[id],
				"reason":  truncateReason(out.VerifyFailure),
			}); err != nil {
			s.logger().Error("recording the verify failure failed", "task", id, "err", err)
		}
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
