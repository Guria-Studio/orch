package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/state"
	"github.com/hectorcanaimero/orch/internal/vcs"
)

// The operator's review of a task's work (#454): accept it, or send it back
// to its agent. Blocking is Move's. The CLI (`orch task accept`, `orch task
// note --retry`) and the dashboard's Changes tab go through these.

// EventPRMerged is the event an accepted task's merge is written as: the
// engine's own name for a PR merged outside the run.
const EventPRMerged = "pr_merged"

// ErrNoPR is Accept's answer for a task with no PR to merge.
var ErrNoPR = errors.New("the task has no PR to merge")

// Accept merges the task's work. With a forge (vcs.provider github or
// gitlab) that is its PR, squashed, now: the operator has read the change.
// Which merge it is depends on the provider, and only here.
func (s Steer) Accept(ctx context.Context, taskID string) (string, error) {
	t, err := s.Backend.Task(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", taskID, err)
	}
	if s.VCS.Provider == "local" {
		// TODO(#455): local mode has no PR; merge orch/<id> into the base
		// branch here, with the same git the worktree package uses.
		return "", errors.New("accepting a task in local mode (vcs.provider: local) is not implemented yet: see #455")
	}
	if t.PRURL == "" {
		return "", fmt.Errorf("%s: %w", taskID, ErrNoPR)
	}
	p := s.Provider
	if p == nil {
		p = vcs.NewProvider(s.VCS)
	}
	if err := p.MergePR(t.PRURL, true, false); err != nil {
		return "", fmt.Errorf("merging %s: %w", t.PRURL, err)
	}
	if err := s.Backend.AppendEvent(ctx, "", state.Event{
		EventType: EventPRMerged,
		TaskID:    taskID,
		TS:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		Extra:     map[string]any{"pr_url": t.PRURL, "accepted_by": s.Source, "pid": os.Getpid()},
	}); err != nil {
		return "", fmt.Errorf("the PR is merged, but recording it failed: %w", err)
	}
	return fmt.Sprintf("%s accepted: %s merged", taskID, t.PRURL), nil
}

// SendBack returns a finished or blocked task to its agent: the note reaches
// its next attempt's prompt, and the task goes back to todo for `orch run` to
// dispatch again.
func (s Steer) SendBack(ctx context.Context, taskID, note string) (StopResult, error) {
	if err := s.Note(ctx, taskID, note); err != nil {
		return StopResult{}, err
	}
	return s.Move(ctx, taskID, model.StatusTodo, "sent back by the operator")
}
