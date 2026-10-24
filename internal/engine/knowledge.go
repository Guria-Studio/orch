package engine

import (
	"context"
	"fmt"
	"slices"
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

// recordLessonsUsed records which lessons a dispatch's prompt carried
// (#453), so the Knowledge page can tell how often each one was used and how
// the tasks it went to did.
func (s *Scheduler) recordLessonsUsed(ctx context.Context, taskID string, backend model.Backend, lessons []knowledge.Lesson) {
	if len(lessons) == 0 || s.Backend == nil {
		return
	}
	ids := make([]string, 0, len(lessons))
	for _, l := range lessons {
		ids = append(ids, l.ID)
	}
	if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, knowledge.EventUsed, taskID, string(backend),
		map[string]any{"lessons": ids}); err != nil {
		s.logger().Warn("recording the lessons a prompt carried failed", "task", taskID, "err", err)
	}
}

// captureLessons writes the draft lessons a clean, accepted attempt leaves
// (knowledge.auto_capture): what the local verify rejected before this
// attempt fixed it, and the conflict this attempt resolved. They go into the
// task's worktree before its work is committed, so they land in its PR,
// where the reviewer keeps, edits or drops them.
func (s *Scheduler) captureLessons(ctx context.Context, entry *InFlight) {
	id := entry.Task.ID
	rejected, findings := s.verifyRejected[id], s.verifyFindings[id]
	delete(s.verifyRejected, id)
	delete(s.verifyFindings, id)
	if !s.Opts.Cfg.Knowledge.Captures() || s.Worktree == nil {
		return
	}
	note := s.agentNote(ctx, id)
	if rejected != "" {
		l := verifyLesson(id, rejected, findings, entry.Task.Files)
		if note != "" {
			l.Body += "\n\nThe attempt that passed: " + note
		}
		s.writeLesson(entry, l)
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

// keepRejection keeps what the local verify rejected — the reason, and the
// review's findings when it had any — until an attempt passes.
func (s *Scheduler) keepRejection(id string, out Outcome) {
	s.verifyRejected[id] = out.VerifyFailure
	if out.Verify != nil {
		s.verifyFindings[id] = out.Verify.Findings
	} else {
		delete(s.verifyFindings, id)
	}
}

// verifyLesson is the draft a rejection leaves: one line per blocking finding
// (file:line title) under the findings' files (#475), or, with none, the
// reason's first lines under the task's files.
func verifyLesson(id, rejected string, findings []VerifyFinding, taskFiles []string) knowledge.Lesson {
	var lines, area []string
	for _, f := range findings {
		if f.Severity != "blocking" { // internal/ci.SeverityBlocking; ci imports engine
			continue
		}
		where := f.File
		if where != "" && f.Line > 0 {
			where += fmt.Sprintf(":%d", f.Line)
		}
		lines = append(lines, strings.TrimSpace(where+" "+f.Title))
		if f.File != "" && !slices.Contains(area, f.File) {
			area = append(area, f.File)
		}
	}
	l := knowledge.Lesson{Tags: []string{"verify"}, Source: "verify", Area: taskFiles}
	if len(lines) == 0 {
		l.Title = "Verify rejected " + id + ": " + firstLines(rejected, 1)
		l.Body = "The local verify rejected an attempt at " + id + ":\n" + firstLines(rejected, 6)
		return l
	}
	if len(area) > 0 {
		l.Area = area
	}
	l.Title = "Verify rejected " + id + ": " + lines[0]
	l.Body = "The local verify rejected an attempt at " + id + ":\n- " + strings.Join(lines, "\n- ")
	return l
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
		s.logger().Warn("reading the agent's note for a draft lesson failed; drafting without it", "task", taskID, "err", err)
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
