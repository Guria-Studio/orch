package engine

import (
	"context"

	"github.com/hectorcanaimero/orch/internal/knowledge"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/prompt"
)

// lessonsFor picks the project lessons task's prompt carries (#451), from the
// directory the agent will run in: its worktree holds the base branch's
// lessons and those of the dependencies merged into it. Nil when the project
// keeps no memory, or nothing matches; an unreadable lesson costs the block,
// never the dispatch.
func (s *Scheduler) lessonsFor(ctx context.Context, task model.Task, workdir string) []knowledge.Lesson {
	if s.Knowledge == nil || !s.Opts.Cfg.Knowledge.Enabled {
		return nil
	}
	specRoot := s.Opts.Cfg.SpecRoot
	if specRoot == "" {
		specRoot = prompt.DefaultSpecRoot
	}
	lessons, err := knowledge.ForTask(ctx, s.Knowledge, workdir, task, specRoot, s.Opts.Cfg.Knowledge.Prompt())
	if err != nil {
		s.logger().Warn("reading the project knowledge for the prompt failed; dispatching without it", "task", task.ID, "err", err)
		return nil
	}
	return lessons
}
