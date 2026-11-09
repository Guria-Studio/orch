package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
)

// Issue events (#525). Go only.
const (
	// EventIssueClosed: the last task of a GitHub issue landed locally and
	// orch closed the issue with the evidence as its comment.
	EventIssueClosed = "issue_closed"
	// EventIssueCloseFailed: closing it did not work (gh missing, not
	// authenticated, no access). The task stays done; the issue stays open.
	EventIssueCloseFailed = "issue_close_failed"
)

// IssueCloser closes a GitHub issue with a closing comment; vcs's
// GitHubProvider is the real one.
type IssueCloser interface {
	CloseIssue(issueURL, comment string) error
}

// issueSection is what the PR body (or the local squash commit) says about
// the task's source issue: `Closes` when no other task of the issue is left
// unfinished, `Refs` while one is.
func (s *Scheduler) issueSection(task model.Task) string {
	return project.IssueSection(task, s.openIssueSiblings(task))
}

func (s *Scheduler) openIssueSiblings(task model.Task) []string {
	return project.OpenIssueSiblings(task, s.Queue.AllTasks(), func(id string) model.Status {
		st, _ := s.Queue.Status(id)
		return st
	})
}

// closeSourceIssue closes the issue of a task just landed on the base here
// (vcs.provider: local), once its last task is done: with no PR there is no
// closing keyword for GitHub to act on, so orch closes it, and the comment is
// the evidence — which tasks, which commit on which branch, what the verify
// said. Best effort: the work is on the base whatever gh answers, so a
// failure is an event and a log line, never a change to the task.
func (s *Scheduler) closeSourceIssue(ctx context.Context, entry *InFlight, commit string) {
	task := entry.Task
	if s.Issues == nil || task.SourceIssue == "" {
		return
	}
	if _, ok := project.IssueRef(task.SourceIssue); !ok {
		return
	}
	if open := s.openIssueSiblings(task); len(open) > 0 {
		s.logger().Info("the issue stays open until its other tasks are done",
			"task", task.ID, "issue", task.SourceIssue, "open", strings.Join(open, ", "))
		return
	}
	comment := s.issueEvidence(task, commit, entry.VerifyNote)
	event, extra := EventIssueClosed, map[string]any{"issue_url": task.SourceIssue, "commit": commit}
	if err := s.Issues.CloseIssue(task.SourceIssue, comment); err != nil {
		s.logger().Warn("closing the task's GitHub issue failed; close it by hand",
			"task", task.ID, "issue", task.SourceIssue, "err", err)
		event, extra["reason"] = EventIssueCloseFailed, truncateReason(err.Error())
	} else {
		s.logger().Info("closed the task's GitHub issue", "task", task.ID, "issue", task.SourceIssue)
	}
	if s.Backend == nil {
		return
	}
	if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, event, task.ID,
		string(entry.Route.Backend), extra); err != nil {
		s.logger().Error("recording the issue close failed", "task", task.ID, "err", err)
	}
}

// issueEvidence is the closing comment: every task of the issue, the commit
// the last one landed as, and the verify's verdict on it.
func (s *Scheduler) issueEvidence(task model.Task, commit, verifyNote string) string {
	var b strings.Builder
	short := commit
	if len(short) > 7 {
		short = short[:7]
	}
	if short == "" {
		short = "no new commit (the base already had the work)"
	}
	fmt.Fprintf(&b, "Done by orch: the work is merged into `%s` (%s), with no pull request (vcs.provider: local).\n\n", s.baseBranch(), short)
	b.WriteString("Tasks from this issue:\n")
	for _, t := range s.Queue.AllTasks() {
		if t.SourceIssue != task.SourceIssue {
			continue
		}
		fmt.Fprintf(&b, "- `%s` %s", t.ID, t.Title)
		if t.ID == task.ID {
			b.WriteString(" — landed last")
		}
		b.WriteString("\n")
	}
	if verifyNote == "" {
		verifyNote = "not run (verify.enabled is off)"
	}
	fmt.Fprintf(&b, "\nVerify on the last task: %s.\n", verifyNote)
	return b.String()
}

// verifyNote is the verify's verdict in one line, for the issue's closing
// comment; "" when there was no verify.
func verifyNote(res *VerifyResult) string {
	if res == nil {
		return ""
	}
	if res.Failure != "" {
		return "failed"
	}
	note := fmt.Sprintf("passed (%d check(s)", len(res.Commands))
	switch {
	case res.Reviewer != "":
		note += ", reviewed by " + res.Reviewer
	case res.ReviewUnavailable != "":
		note += ", review unavailable: " + res.ReviewUnavailable
	}
	return note + ")"
}
