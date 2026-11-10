package engine

import (
	"context"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
	"github.com/Guria-Studio/orch/internal/worktree"
)

// Events the merge queue records (#522). Go only.
const (
	// EventQueueEnqueued: a green PR entered the queue.
	EventQueueEnqueued = "queue_enqueued"
	// EventQueueMerged: the queue merged it.
	EventQueueMerged = "queue_merged"
	// EventQueueEjected: it left the queue without merging — sent back to
	// its agent (a conflict, a semantic collision, a verify that failed on
	// the current base) or blocked; it enters again once green.
	EventQueueEjected = "queue_ejected"
)

// MergeQueueStore keeps the merge queue (migration 017). state.SQLite is one.
type MergeQueueStore interface {
	Enqueue(ctx context.Context, taskID, prURL string, at time.Time) (bool, error)
	SetQueueState(ctx context.Context, taskID, state, reason string, at time.Time) error
	QueueEntries(ctx context.Context) ([]state.QueueEntry, error)
}

// mergeQueueDetector is a provider that can tell whether a branch has the
// forge's own merge queue on; vcs.GitHubProvider is one.
type mergeQueueDetector interface {
	MergeQueueActive(prURL, baseBranch string) (bool, error)
}

// queueOn reports whether orch's merge queue runs this poll.
func (p *CIPoller) queueOn() bool {
	return p.MergeQueue && p.AutoMerge && p.Queue != nil && (p.queueDelegated == nil || !*p.queueDelegated)
}

// queueing reports whether a green PR waits for its turn in orch's queue
// rather than merging now. Asks the forge once per run whether the base has
// its own merge queue: if it does, orch leaves the order to it (#522
// decision 3) — `gh pr merge --auto` puts the PR in GitHub's queue — and
// keeps only its semantic checks, which mergeGreen runs first.
func (p *CIPoller) queueing(ctx context.Context, s *Scheduler, row state.TaskRuntime) bool {
	if !p.MergeQueue || !p.AutoMerge || p.Queue == nil {
		return false
	}
	if p.queueDelegated == nil {
		active := false
		if d, ok := p.Provider.(mergeQueueDetector); ok {
			var err error
			if active, err = d.MergeQueueActive(row.PRURL, s.baseBranch()); err != nil {
				// Not cached: ask again on the next green PR.
				s.logger().Warn("merge queue: could not tell whether the base has GitHub's merge queue; using orch's",
					"base", s.baseBranch(), "err", err)
				return true
			}
		}
		p.queueDelegated = &active
		if active {
			s.logger().Info("merge queue: the base branch has GitHub's merge queue on; leaving the order to it",
				"base", s.baseBranch())
		}
	}
	return !*p.queueDelegated
}

// enqueue puts a green PR in the queue, once.
func (p *CIPoller) enqueue(ctx context.Context, s *Scheduler, row state.TaskRuntime) {
	entered, err := p.Queue.Enqueue(ctx, row.ID, row.PRURL, s.now())
	if err != nil {
		s.logger().Error("merge queue: enqueueing failed", "task", row.ID, "err", err)
		return
	}
	if entered {
		s.logger().Info("merge queue: the PR is green; queued", "task", row.ID, "pr", row.PRURL)
		p.emit(ctx, s, EventQueueEnqueued, row, map[string]any{"pr_url": row.PRURL})
	}
}

// processQueue works the head of the queue, one step per poll: the queue is
// serial, so while the head waits on CI nothing behind it merges. The head
// is the first queued task whose PR is still waiting here, by phase, then
// the number of tasks that depend on it (more first: it unblocks more work),
// then when it entered.
//
// A head whose base moved since its branch was last brought up to date gets
// the base merged in, the local verify run again on the result without the
// LLM review (merge_queue.reverify: when_base_moved, #522 decision 5) and is
// pushed; its CI runs again and it merges at a later poll, against the base
// it was verified on. A merge that conflicts goes the conflict retry's way
// (#438), a failing verify or a blocking semantic collision back to the
// agent; either takes it out of the queue until it is green again.
//
// ponytail: a merge made outside orch between the check and `gh pr merge`
// is not seen; GitHub's "require branches to be up to date" closes that.
func (p *CIPoller) processQueue(ctx context.Context, s *Scheduler, pending []state.TaskRuntime, green map[string]state.TaskRuntime) int {
	entries, err := p.Queue.QueueEntries(ctx)
	if err != nil {
		s.logger().Error("merge queue: reading the queue failed", "err", err)
		return 0
	}
	rows := map[string]state.TaskRuntime{}
	for _, r := range pending {
		rows[r.ID] = r
	}
	busy := map[string]bool{}
	for id := range s.inFlightIDs() {
		busy[id] = true
	}
	for id := range s.retryQueueIDs() {
		busy[id] = true
	}
	var queued []state.QueueEntry
	for _, e := range entries {
		if _, waiting := rows[e.TaskID]; e.State == state.QueueQueued && waiting && !busy[e.TaskID] {
			queued = append(queued, e)
		}
	}
	if len(queued) == 0 {
		return 0
	}
	sortQueue(queued, s.Queue)
	head := queued[0]
	id := head.TaskID

	if _, running := p.reverifying[id]; running {
		acted := p.collectReverify(ctx, s, rows[id])
		p.ejectIfSentBack(ctx, s, rows[id])
		return boolInt(acted)
	}
	row, isGreen := green[id]
	if !isGreen {
		return 0 // its CI runs again, or is red: nothing behind it merges
	}
	if s.Queue != nil {
		if st, _ := s.Queue.Status(id); st == model.StatusDone || st == model.StatusBlocked {
			// The sweep above already settled it (a semantic block).
			p.ejectIfSentBack(ctx, s, row)
			return 0
		}
	}
	if p.Worktree == nil {
		acted, merged := p.mergeGreen(ctx, s, row)
		p.afterMerge(ctx, s, row, acted, merged)
		return boolInt(acted)
	}

	base := s.baseBranch()
	wtPath, err := p.Worktree.Recreate(ctx, id)
	var merge worktree.MergeResult
	if err == nil {
		merge, err = p.Worktree.MergeBase(ctx, id, base)
	}
	if err != nil {
		s.logger().Warn("merge queue: bringing the head up to date failed; trying again next poll", "task", id, "err", err)
		p.dropWorktree(ctx, s, id)
		return 0
	}
	switch {
	case len(merge.Conflicts) > 0:
		p.dropWorktree(ctx, s, id)
		acted := p.ciConflict(ctx, s, row)
		p.ejectIfSentBack(ctx, s, row)
		return boolInt(acted)
	case merge.Moved && s.verifies() && p.Reverify != "never":
		p.startReverify(ctx, s, row, wtPath, base)
		p.reverifying[id].queue = true
		acted := p.collectReverify(ctx, s, row)
		p.ejectIfSentBack(ctx, s, row)
		return boolInt(acted)
	case merge.Moved:
		return boolInt(p.pushMerged(ctx, s, row, base, false, true))
	}
	p.dropWorktree(ctx, s, id)
	acted, merged := p.mergeGreen(ctx, s, row)
	p.afterMerge(ctx, s, row, acted, merged)
	return boolInt(acted)
}

// afterMerge records what mergeGreen did to the queue's head.
func (p *CIPoller) afterMerge(ctx context.Context, s *Scheduler, row state.TaskRuntime, acted, merged bool) {
	switch {
	case merged:
		if err := p.Queue.SetQueueState(ctx, row.ID, state.QueueMerged, "", s.now()); err != nil {
			s.logger().Error("merge queue: recording the merge failed", "task", row.ID, "err", err)
		}
		p.emit(ctx, s, EventQueueMerged, row, map[string]any{"pr_url": row.PRURL})
	case acted:
		// A semantic block, or a merge that would not go through: the task
		// left the queue one way or another.
		p.eject(ctx, s, row, "not merged: "+p.lastReason(s, row.ID))
	}
}

// lastReason is a short word on why a merge did not happen.
func (p *CIPoller) lastReason(s *Scheduler, id string) string {
	if s.retryQueueIDs()[id] || s.inFlightIDs()[id] {
		return "sent back to its agent"
	}
	if s.Queue != nil {
		if st, _ := s.Queue.Status(id); st == model.StatusBlocked {
			return "blocked"
		}
	}
	return "auto-merge failed, the PR is still open"
}

// ejectIfSentBack takes the head out of the queue when what was just done
// sent its task back to the agent or blocked it.
func (p *CIPoller) ejectIfSentBack(ctx context.Context, s *Scheduler, row state.TaskRuntime) {
	switch {
	case s.retryQueueIDs()[row.ID] || s.inFlightIDs()[row.ID]:
		p.eject(ctx, s, row, "sent back to its agent")
	case s.Queue != nil:
		if st, _ := s.Queue.Status(row.ID); st == model.StatusBlocked {
			p.eject(ctx, s, row, "blocked")
		}
	}
}

func (p *CIPoller) eject(ctx context.Context, s *Scheduler, row state.TaskRuntime, reason string) {
	if err := p.Queue.SetQueueState(ctx, row.ID, state.QueueEjected, reason, s.now()); err != nil {
		s.logger().Error("merge queue: recording the ejection failed", "task", row.ID, "err", err)
	}
	s.logger().Info("merge queue: the PR left the queue", "task", row.ID, "reason", reason)
	p.emit(ctx, s, EventQueueEjected, row, map[string]any{"pr_url": row.PRURL, "reason": reason})
}

// sortQueue orders the queue as project.SortQueue does.
func sortQueue(entries []state.QueueEntry, q *TaskQueue) {
	var tasks []model.Task
	if q != nil {
		tasks = q.AllTasks()
	}
	project.SortQueue(entries, tasks)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
