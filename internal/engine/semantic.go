package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/Guria-Studio/orch/internal/collide"
	"github.com/Guria-Studio/orch/internal/prompt"
	"github.com/Guria-Studio/orch/internal/state"
	"github.com/Guria-Studio/orch/internal/worktree"
)

// EventSemanticCollision records an identity a task's branch adds that the
// base or another open branch already claims (#522): two migrations 005
// under different file names, which git merges without a conflict.
const EventSemanticCollision = "semantic_collision"

// Where a semantic check ran, as the event's `at`.
const (
	collideAtPROpen = "pr_open"
	collideAtMerge  = "merge"
	collideAtLand   = "land"
)

// collisionChecker is the worktree manager's semantic check (#522);
// managerAdapter has it. A test double without it checks nothing.
type collisionChecker interface {
	Collisions(ctx context.Context, taskID, baseBranch string, rules []collide.Rule, others []string) ([]collide.Collision, error)
}

// collisions runs semantic_checks over the task's branch, against the base
// and the branches of the other tasks named. Nil when no rule is configured;
// a check that cannot run is logged and finds nothing — a gate that blocks on
// its own breakage teaches people to turn it off.
func (s *Scheduler) collisions(ctx context.Context, taskID string, others []string) []collide.Collision {
	rules := s.Opts.Cfg.SemanticChecks
	cc, ok := s.Worktree.(collisionChecker)
	if len(rules) == 0 || !ok {
		return nil
	}
	found, err := cc.Collisions(ctx, taskID, s.baseBranch(), rules, others)
	if err != nil {
		s.logger().Warn("the semantic collision check could not run; skipping it", "task", taskID, "err", err)
		return nil
	}
	return found
}

// recordCollisions writes one semantic_collision event per collision; blocked
// says whether it stopped the merge or land.
func (s *Scheduler) recordCollisions(ctx context.Context, taskID, backend, at string, found []collide.Collision, blocked bool, prURLs map[string]string) {
	if s.Backend == nil {
		return
	}
	for _, c := range found {
		extra := map[string]any{
			"rule": c.Rule, "key": c.Key, "file": c.File, "with": c.With, "mode": c.Mode,
			"reason": c.Reason, "at": at, "blocked": blocked,
		}
		if c.WithFile != "" {
			extra["with_file"] = c.WithFile
		}
		if url := prURLs[c.With]; url != "" {
			extra["pr_url"] = url
		}
		if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, EventSemanticCollision, taskID, backend, extra); err != nil {
			s.logger().Error("recording the semantic collision failed", "task", taskID, "err", err)
		}
	}
}

// splitCollisions separates the collisions a block rule raised.
func splitCollisions(found []collide.Collision) (block, warn []collide.Collision) {
	for _, c := range found {
		if c.Mode == collide.ModeBlock {
			block = append(block, c)
		} else {
			warn = append(warn, c)
		}
	}
	return block, warn
}

// collisionReason is the block reason for the collisions that stopped a merge.
func collisionReason(block []collide.Collision) string {
	reason := "semantic collision: " + block[0].Reason
	if len(block) > 1 {
		reason += fmt.Sprintf(" (and %d more)", len(block)-1)
	}
	return truncateReason(reason)
}

// collisionFeedback is the feedback file's body: each collision, and what to
// do about it.
func collisionFeedback(base string, block []collide.Collision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Your branch merges into %s without a git conflict, but orch's semantic_checks found these collisions. "+
		"%s has been merged into your working dir: change your side (renumber the migration, rename the key) so each is unique, and keep the tests passing.\n\n", base, base)
	for _, c := range block {
		fmt.Fprintf(&b, "- %s\n", c.Reason)
	}
	return b.String()
}

// semanticPRs checks the PR task just opened against the other open task PRs
// and the base, and records what collides (#522). Always a warning here: the
// merge, not the PR, is where a block rule stops.
func (s *Scheduler) semanticPRs(ctx context.Context, entry *InFlight) {
	if len(s.Opts.Cfg.SemanticChecks) == 0 {
		return
	}
	id := entry.Task.ID
	var others []string
	urls := map[string]string{}
	if s.OpenPRs != nil {
		open, err := s.OpenPRs.TasksWithPendingCI(ctx)
		if err != nil {
			s.logger().Warn("listing the open PRs for the semantic check failed", "task", id, "err", err)
		}
		for _, row := range open {
			if row.ID != id {
				others = append(others, row.ID)
				urls[row.ID] = row.PRURL
			}
		}
	}
	found := s.collisions(ctx, id, others)
	for _, c := range found {
		s.logger().Warn("semantic collision", "task", id, "with", c.With, "reason", c.Reason)
	}
	s.recordCollisions(ctx, id, string(entry.Route.Backend), collideAtPROpen, found, false, urls)
}

// landCollisions checks a task about to land here against the base: a block
// rule's collision keeps it off the base and is returned as the block reason.
func (s *Scheduler) landCollisions(ctx context.Context, entry *InFlight) string {
	found := s.collisions(ctx, entry.Task.ID, nil)
	if len(found) == 0 {
		return ""
	}
	block, warn := splitCollisions(found)
	backend := string(entry.Route.Backend)
	s.recordCollisions(ctx, entry.Task.ID, backend, collideAtLand, warn, false, nil)
	if len(block) == 0 {
		return ""
	}
	s.recordCollisions(ctx, entry.Task.ID, backend, collideAtLand, block, true, nil)
	return collisionReason(block) + " (kept on " + s.Worktree.BranchName(entry.Task.ID) + ")"
}

// semanticBlocked runs the semantic checks on a green PR before orch merges
// it, against the base as it is now (#522). Warnings are recorded once per
// task; a block rule's collision keeps the PR open and sends the task back to
// its agent — the base merged into its kept work, the collisions in its
// feedback file — within retry.conflict, after which the task is blocked.
// Reports whether it stopped the merge.
func (p *CIPoller) semanticBlocked(ctx context.Context, s *Scheduler, row state.TaskRuntime) bool {
	found := s.collisions(ctx, row.ID, nil)
	if len(found) == 0 {
		return false
	}
	block, warn := splitCollisions(found)
	if !p.AutoMerge {
		// A person merges: tell them, stop nothing.
		block, warn = nil, found
	}
	if len(warn) > 0 && !p.semanticWarned[row.ID] {
		if p.semanticWarned == nil {
			p.semanticWarned = map[string]bool{}
		}
		p.semanticWarned[row.ID] = true
		s.recordCollisions(ctx, row.ID, row.LastBackend, collideAtMerge, warn, false, nil)
	}
	if len(block) == 0 {
		return false
	}
	s.recordCollisions(ctx, row.ID, row.LastBackend, collideAtMerge, block, true, nil)
	reason := collisionReason(block)
	s.logger().Warn("semantic collision with the base; not merging", "task", row.ID, "pr", row.PRURL, "reason", reason)
	if p.Worktree == nil || p.conflictAttempts[row.ID] >= p.ConflictRetries {
		p.block(ctx, s, row, reason, false)
		return true
	}
	task, ok := s.Queue.Task(row.ID)
	route, routed := s.Routes[task.Model]
	if !ok || !routed {
		s.logger().Warn("semantic collision: the task or its route is not in this run; blocking", "task", row.ID)
		p.block(ctx, s, row, reason, false)
		return true
	}
	base := s.baseBranch()
	wtPath, err := p.Worktree.Recreate(ctx, row.ID)
	var merge worktree.MergeResult
	if err == nil {
		merge, err = p.Worktree.MergeBase(ctx, row.ID, base)
	}
	if err != nil {
		s.logger().Warn("semantic collision: merging the base for the agent failed; blocking", "task", row.ID, "err", err)
		p.dropWorktree(ctx, s, row.ID)
		p.block(ctx, s, row, reason, false)
		return true
	}
	if err := writeFeedback(wtPath, "Semantic collision with "+base+" — please fix", collisionFeedback(base, block)); err != nil {
		s.logger().Error("writing the collision feedback failed; the retry runs without it", "task", row.ID, "err", err)
	}
	if !requeueOnPR(ctx, s, row, task, route, wtPath, "semantic collision with the base; sent to the agent") {
		p.dropWorktree(ctx, s, row.ID)
		return false
	}
	if len(merge.Conflicts) > 0 {
		s.conflictRetry[row.ID] = &prompt.Conflict{Base: base, Files: merge.Conflicts, Commits: merge.Commits}
	}
	if p.conflictAttempts == nil {
		p.conflictAttempts = map[string]int{}
	}
	p.conflictAttempts[row.ID]++
	return true
}
