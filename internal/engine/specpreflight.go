package engine

import (
	"context"
	"fmt"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/prompt"
)

// EventSpecRefWarning records a dispatch whose spec ref did not resolve where
// the agent runs: the file is not there at all, or it has no heading for the
// ref's anchor. The task is dispatched anyway, with a prompt that says so.
// Go only (#341, #342).
const EventSpecRefWarning = "spec_ref_warning"

// checkSpec looks for task's spec ref in workdir, the directory the agent is
// about to run in, before its prompt tells it to READ FIRST.
//
// Three outcomes, by how much the operator can do about them:
//
//   - The file is in the project but not in the task's worktree. That is a
//     spec written and never committed and pushed: the worktree starts from
//     the base branch, `orch atomize` read the working tree (#341). The fix
//     is one push, and an agent dispatched now would build the task from its
//     one-line description, so the task blocks with a reason that says what
//     to do. Returned as blockReason.
//   - The file is nowhere (#342), or it is there without a heading for the
//     anchor. Blocking would stop every task of a scaffolded project, whose
//     template specRefs name specs the operator has not written yet, so the
//     task dispatches with a prompt that tells the agent the truth and a
//     spec_ref_warning event the operator can find.
//   - The spec is where it should be, or there is no spec ref: nothing.
func (s *Scheduler) checkSpec(ctx context.Context, task model.Task, route model.RouteEntry, workdir string) (prompt.SpecState, string) {
	if task.SpecRef == "" {
		return prompt.SpecUnchecked, ""
	}
	specRoot := s.Opts.Cfg.SpecRoot
	if specRoot == "" {
		specRoot = prompt.DefaultSpecRoot
	}
	got, err := project.CheckSpecRef(workdir, specRoot, task.SpecRef)
	if err != nil {
		// A spec that exists and cannot be read is the agent's problem to
		// hit, not a reason to hold the task back: it gets the plain line.
		s.logger().Warn("checking the spec ref failed", "task", task.ID, "err", err)
		return prompt.SpecUnchecked, ""
	}
	if got.OK() {
		return prompt.SpecUnchecked, ""
	}

	inWorktree := s.Worktree != nil && s.Opts.WorktreeMode && workdir != s.Opts.Cwd
	if inWorktree {
		local, err := project.CheckSpecRef(s.Opts.Cwd, specRoot, task.SpecRef)
		switch {
		case err != nil:
		case !got.FileFound && local.FileFound:
			return prompt.SpecUnchecked, fmt.Sprintf(
				"spec_missing: %s is not in origin/%s, which the task's worktree starts from; commit and push it, then `orch task-status %s todo`",
				local.File, s.baseBranch(), task.ID)
		case local.OK():
			return prompt.SpecUnchecked, fmt.Sprintf(
				"spec_missing: %s in origin/%s has no heading for %s, the project's copy does; commit and push it, then `orch task-status %s todo`",
				local.File, s.baseBranch(), local.Anchor, task.ID)
		}
	}

	state, problem := prompt.SpecAnchorMissing, "anchor_missing"
	if !got.FileFound {
		state, problem = prompt.SpecFileMissing, "file_missing"
	}
	if s.Backend != nil {
		if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, EventSpecRefWarning, task.ID,
			string(route.Backend), map[string]any{
				"spec_ref": task.SpecRef, "file": got.File, "problem": problem,
			}); err != nil {
			s.logger().Error("recording the spec ref warning failed", "task", task.ID, "err", err)
		}
	}
	return state, ""
}
