package engine

import (
	"context"
	"sort"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// Events the conflict radar records (#482). Go only.
const (
	// EventFilesOverlap: a task about to dispatch declares files a running
	// task also declares. With radar: serialize it waits for that one.
	EventFilesOverlap = "files_overlap"
	// EventBranchConflict: the PR just opened conflicts with another open task
	// PR, on the files named. Not pr_*: the timeline credits those to CI, and
	// this is orch merging two branches.
	EventBranchConflict = "branch_conflict"
)

// OpenPRLister lists the tasks whose PR is open and still waiting on CI, the
// ones a new PR is checked against. state.SQLite is one.
type OpenPRLister interface {
	TasksWithPendingCI(ctx context.Context) ([]state.TaskRuntime, error)
}

// radarMode is the radar config key, read as off when unset: a Scheduler
// built without a loaded config (the tests, mostly) gets no radar.
func (s *Scheduler) radarMode() string {
	if m := s.Opts.Cfg.Radar; m == "warn" || m == "serialize" {
		return m
	}
	return "off"
}

// overlapHolds reports whether task must wait because its files overlap a
// running task's (radar: serialize); warn lets it go. Either mode records a
// files_overlap event and a log line once per (task, running task) until the
// task dispatches, so a task held, or skipped for a full slot, for ten ticks
// says so once.
func (s *Scheduler) overlapHolds(ctx context.Context, task model.Task, backend string) bool {
	mode := s.radarMode()
	if mode == "off" || len(task.Files) == 0 {
		return false
	}
	other, files := s.runningOverlap(task)
	if other == "" {
		return false
	}
	if s.radarSeen == nil {
		s.radarSeen = map[string]string{}
	}
	if s.radarSeen[task.ID] != other {
		s.radarSeen[task.ID] = other
		s.logger().Warn("the task's files overlap a running task's", "task", task.ID, "with", other,
			"files", strings.Join(files, ", "), "radar", mode)
		if s.Backend != nil {
			if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, EventFilesOverlap, task.ID, backend,
				map[string]any{"with": other, "files": files, "mode": mode,
					"reason": "shares " + strings.Join(files, ", ") + " with " + other + ", still running"}); err != nil {
				s.logger().Error("recording the files overlap failed", "task", task.ID, "err", err)
			}
		}
	}
	if mode != "serialize" {
		return false
	}
	s.deferReasons[task.ID] = "files-overlap:" + other
	return true
}

// runningOverlap is the first running task, by id, whose files overlap
// task's, and task's entries that do.
func (s *Scheduler) runningOverlap(task model.Task) (string, []string) {
	running := make([]*InFlight, 0, len(s.inFlight))
	for _, e := range s.inFlight {
		if e.Task.ID != task.ID {
			running = append(running, e)
		}
	}
	sort.Slice(running, func(i, j int) bool { return running[i].Task.ID < running[j].Task.ID })
	for _, e := range running {
		var files []string
		for _, mine := range task.Files {
			for _, theirs := range e.Task.Files {
				if entriesOverlap(mine, theirs) {
					files = append(files, mine)
					break
				}
			}
		}
		if len(files) > 0 {
			return e.Task.ID, files
		}
	}
	return "", nil
}

// entriesOverlap reports whether two `files` entries (a path, a directory
// ending in "/", or a glob) can name the same file. A path against anything
// is exact, through inFileList.
//
// ponytail: two globs, or a glob and a directory, overlap when one's literal
// directory before the first wildcard contains the other's, so "src/*.go"
// and "src/*.ts" count as overlapping and two root globs always do. It errs
// towards a warning; expand the globs against `git ls-files` if that is too
// noisy.
func entriesOverlap(a, b string) bool {
	if inFileList([]string{a}, b) || inFileList([]string{b}, a) {
		return true
	}
	da, okA := globDir(a)
	db, okB := globDir(b)
	return okA && okB && (strings.HasPrefix(da, db) || strings.HasPrefix(db, da))
}

// globDir is the directory a glob or a directory entry stays inside, and
// false for a plain path.
func globDir(entry string) (string, bool) {
	i := strings.IndexAny(entry, "*?[")
	if i < 0 {
		return entry, strings.HasSuffix(entry, "/")
	}
	return entry[:strings.LastIndex(entry[:i], "/")+1], true
}

// radarPRs checks the PR task just opened against every other open task PR,
// merging the two branches without a checkout (git merge-tree), and records
// a branch_conflict event naming the files for each one it conflicts with. The
// PR is open either way: this is a heads-up before the operator merges, not
// a gate. A branch that cannot be read (deleted, from another clone) is
// skipped with a log line.
//
// ponytail: compares only against PRs whose CI is pending, the ones
// TasksWithPendingCI lists; a PR whose CI went green and that waits on a
// person to merge it is missed. A list of open PRs from the forge covers it.
func (s *Scheduler) radarPRs(ctx context.Context, entry *InFlight) {
	if s.radarMode() == "off" || s.OpenPRs == nil {
		return
	}
	id := entry.Task.ID
	open, err := s.OpenPRs.TasksWithPendingCI(ctx)
	if err != nil {
		s.logger().Warn("listing the open PRs for the conflict radar failed", "task", id, "err", err)
		return
	}
	for _, row := range open {
		if row.ID == id {
			continue
		}
		files, err := s.Worktree.BranchConflicts(ctx, id, row.ID)
		if err != nil {
			s.logger().Warn("checking the PR against another open one failed", "task", id, "with", row.ID, "err", err)
			continue
		}
		if len(files) == 0 {
			continue
		}
		s.logger().Warn("the PR conflicts with another open task PR; whichever merges second needs a rebase",
			"task", id, "with", row.ID, "pr", row.PRURL, "files", strings.Join(files, ", "))
		if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, EventBranchConflict, id, string(entry.Route.Backend),
			map[string]any{"with": row.ID, "pr_url": row.PRURL, "files": files,
				"reason": "conflicts with " + row.ID + "'s PR on " + strings.Join(files, ", ")}); err != nil {
			s.logger().Error("recording the PR conflict failed", "task", id, "err", err)
		}
	}
}
