package engine

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
)

// prNumberSuffix is the " (#N)" a forge's squash merge appends to the PR
// title it uses as the commit subject.
var prNumberSuffix = regexp.MustCompile(`\s*\(#(\d+)\)$`)

// conflictCausedBy names the tasks whose work, already on the base, a
// conflict of self's is with (#522): ci_conflict_retry's caused_by, which
// the radar report (`orch radar report`) matches against the radar's
// warnings. commits are the base's commits that touched the conflicting
// files, "<hash> <subject>" (worktree.MergeResult.Commits). A subject is a
// task's when it ends in the number of the task's PR (a squash merge on the
// forge), equals the task's title (a local land, which commits the PR title)
// or starts with "<id>:". Commits matching no task are left out; nil when
// none matches.
func (s *Scheduler) conflictCausedBy(ctx context.Context, self string, commits []string) []string {
	if len(commits) == 0 || s.Queue == nil {
		return nil
	}
	tasks := s.Queue.AllTasks()
	byTitle := map[string][]string{}
	for _, t := range tasks {
		title := t.Title
		if title == "" {
			title = t.ID
		}
		byTitle[title] = append(byTitle[title], t.ID)
	}
	var prs map[string]string // PR number -> task id, read on first need
	found := map[string]bool{}
	for _, line := range commits {
		_, subject, _ := strings.Cut(strings.TrimSpace(line), " ")
		subject = strings.TrimSpace(subject)
		if m := prNumberSuffix.FindStringSubmatch(subject); m != nil {
			if prs == nil {
				prs = s.taskPRNumbers(ctx, tasks)
			}
			if id := prs[m[1]]; id != "" {
				found[id] = true
				continue
			}
			subject = strings.TrimSpace(subject[:len(subject)-len(m[0])])
		}
		if ids := byTitle[subject]; len(ids) == 1 {
			found[ids[0]] = true
			continue
		}
		if id, _, ok := strings.Cut(subject, ":"); ok {
			if _, known := s.Queue.Task(strings.TrimSpace(id)); known {
				found[strings.TrimSpace(id)] = true
			}
		}
	}
	delete(found, self)
	if len(found) == 0 {
		return nil
	}
	out := make([]string, 0, len(found))
	for id := range found {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// taskPRNumbers maps each task's PR number (the URL's last segment) to the
// task. Empty without a PR reader.
func (s *Scheduler) taskPRNumbers(ctx context.Context, tasks []model.Task) map[string]string {
	out := map[string]string{}
	if s.PRs == nil {
		return out
	}
	for _, t := range tasks {
		url, err := s.PRs.TaskPR(ctx, t.ID)
		if err != nil || url == "" {
			continue
		}
		if i := strings.LastIndex(strings.TrimRight(url, "/"), "/"); i >= 0 {
			out[strings.TrimRight(url, "/")[i+1:]] = t.ID
		}
	}
	return out
}
