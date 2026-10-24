package engine

import (
	"context"
	"fmt"
	"strings"

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

// captureLessons writes the draft lessons a clean, accepted attempt leaves
// (knowledge.auto_capture): what the local verify rejected before this
// attempt fixed it, and the conflict this attempt resolved. They go into the
// task's worktree before its work is committed, so they land in its PR,
// where the reviewer keeps, edits or drops them.
func (s *Scheduler) captureLessons(ctx context.Context, entry *InFlight) {
	id := entry.Task.ID
	rejected := s.verifyRejected[id]
	delete(s.verifyRejected, id)
	if !s.Opts.Cfg.Knowledge.Captures() || s.Worktree == nil {
		return
	}
	note := s.agentNote(ctx, id)
	if rejected != "" {
		body := "The local verify rejected an attempt at " + id + ":\n" + firstLines(rejected, 6)
		if note != "" {
			body += "\n\nThe attempt that passed: " + note
		}
		s.writeLesson(entry, knowledge.Lesson{Title: "Verify rejected " + id + ": " + firstLines(rejected, 1),
			Area: entry.Task.Files, Tags: []string{"verify"}, Source: "verify", Body: body})
	}
	if c := entry.Conflict; c != nil {
		body := fmt.Sprintf("%s's pull request conflicted with %s in %s.", id, c.Base, strings.Join(c.Files, ", "))
		if len(c.Commits) > 0 {
			body += "\nThe base commits that touched them:\n- " + strings.Join(c.Commits, "\n- ")
		}
		if note != "" {
			body += "\n\nHow the agent resolved it: " + note
		}
		s.writeLesson(entry, knowledge.Lesson{Title: "Conflict with " + c.Base + " in " + strings.Join(c.Files, ", "),
			Area: c.Files, Tags: []string{"conflict"}, Source: "conflict", Body: body})
	}
}

func (s *Scheduler) writeLesson(entry *InFlight, l knowledge.Lesson) {
	l.Task = entry.Task.ID
	if r := []rune(l.Title); len(r) > 100 {
		l.Title = string(r[:100]) + "…"
	}
	written, err := knowledge.Add(entry.Dispatch.Cwd, l, s.now())
	if err != nil {
		s.logger().Warn("writing a draft lesson failed", "task", entry.Task.ID, "source", l.Source, "err", err)
		return
	}
	s.logger().Info("drafted a lesson in the task's branch", "task", entry.Task.ID, "lesson", written.Path)
}

// agentNote is what the agent reported, "" when it said nothing or there is
// no trail to read.
func (s *Scheduler) agentNote(ctx context.Context, taskID string) string {
	if s.Comments == nil {
		return ""
	}
	comments, err := s.Comments.TaskComments(ctx, taskID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(prompt.AgentComment(comments))
}

// firstLines is text's first n non-empty lines.
func firstLines(text string, n int) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
			if len(out) == n {
				break
			}
		}
	}
	return strings.Join(out, "\n")
}
