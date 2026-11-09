package project

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
)

// IssueRef turns a GitHub issue URL into the `owner/repo#n` form GitHub's
// closing keywords take. The long form, not `#n`: it closes the issue
// whether the PR is in the issue's repository or another one.
func IssueRef(issueURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(issueURL))
	if err != nil || u.Host != "github.com" {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "issues" || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n <= 0 {
		return "", false
	}
	return fmt.Sprintf("%s/%s#%d", parts[0], parts[1], n), true
}

// IssueSection is what a task's PR body says about the GitHub issue it came
// from (`sourceIssue`, #525), or "" for a task with none.
//
// `Closes owner/repo#n` when no other task of the issue is unfinished, so
// GitHub closes the issue when the PR merges, with the PR as the evidence.
// While others are (openSiblings, the ids of the issue's other tasks not
// done), `Refs`: closing on the first of several PRs would mark the issue
// done with most of it still to do. An issue URL that is not GitHub's is
// only linked — no keyword would act on it.
func IssueSection(task model.Task, openSiblings []string) string {
	if task.SourceIssue == "" {
		return ""
	}
	ref, ok := IssueRef(task.SourceIssue)
	if !ok {
		return "\n\nIssue: " + task.SourceIssue
	}
	if len(openSiblings) == 0 {
		return "\n\nCloses " + ref
	}
	return fmt.Sprintf("\n\nRefs %s — %d other task(s) of this issue are not done yet (%s); the last one's PR closes it.",
		ref, len(openSiblings), strings.Join(openSiblings, ", "))
}

// OpenIssueSiblings lists, in the order given, the ids of the tasks other
// than task that come from the same issue and are not done. status answers a
// task's current status.
func OpenIssueSiblings(task model.Task, tasks []model.Task, status func(id string) model.Status) []string {
	if task.SourceIssue == "" {
		return nil
	}
	var open []string
	for _, t := range tasks {
		if t.ID == task.ID || t.SourceIssue != task.SourceIssue {
			continue
		}
		if status(t.ID) != model.StatusDone {
			open = append(open, t.ID)
		}
	}
	return open
}
