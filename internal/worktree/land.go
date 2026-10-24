package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
)

// Landing is what LandLocally did: the squash commit now at the tip of the
// base, or the files that conflict with it. Both empty: the task's branch held
// nothing the base lacks.
type Landing struct {
	Commit string
	// Conflicts are the paths the task's work and the base both changed;
	// nothing was changed, and the task's branch and worktree are kept.
	Conflicts []string
	// Commits are the base's commits that touched them, "<hash> <subject>".
	Commits []string
}

// landMu makes the lands of one process take turns.
//
// ponytail: a process lock. Another process landing at the same moment (a
// dashboard Accept next to `orch run`) is caught by the compare-and-swap on
// the base below and gets an error to retry, not a lost commit; a file lock
// would make it wait instead.
var landMu sync.Mutex

// LandLocally squash-merges orch/<taskID> into baseBranch in this repository,
// pushing nothing (vcs.provider: local, #455): one commit on the base with
// message, whose tree is the base with the task's work merged in. It is what a
// forge's squash merge does, done here, and it is the one function both the
// engine and the dashboard's Accept land a task with.
//
// The merge is computed without touching any checkout (`git merge-tree`), so a
// conflict changes nothing: Landing.Conflicts names the files and the task's
// branch and worktree stay for the conflict retry (#438). A clean one moves
// the base: through `merge --ff-only` in the worktree that has it checked out
// — the project root, usually — so its files follow; with `update-ref` and the
// old value when none has. Either refuses if the base moved meanwhile. Then the
// task's worktree and branch are removed, as a forge deletes a merged branch,
// so no dependent merges the unsquashed branch on top of its own squash.
func (m *Manager) LandLocally(taskID, baseBranch, message string) (Landing, error) {
	landMu.Lock()
	defer landMu.Unlock()

	branch := m.BranchName(taskID)
	base, err := m.run(taskID, "git", "rev-parse", "--verify", "refs/heads/"+baseBranch+"^{commit}")
	if err != nil {
		return Landing{}, fmt.Errorf("worktree: reading the base %s: %w", baseBranch, err)
	}
	base = strings.TrimSpace(base)
	ahead, err := m.run(taskID, "git", "rev-list", "--count", base+".."+branch)
	if err != nil {
		return Landing{}, fmt.Errorf("worktree: reading %s: %w", branch, err)
	}
	if strings.TrimSpace(ahead) == "0" {
		return Landing{}, m.dropLanded(taskID)
	}

	tree, conflicts, err := m.mergeTree(taskID, base, branch)
	if err != nil {
		return Landing{}, err
	}
	if len(conflicts) > 0 {
		var commits []string
		logArgs := append([]string{"git", "log", "--format=%h %s", branch + ".." + base, "--"}, conflicts...)
		if log, err := m.run(taskID, logArgs...); err == nil {
			commits = strings.FieldsFunc(log, func(r rune) bool { return r == '\n' })
		}
		return Landing{Conflicts: conflicts, Commits: commits}, nil
	}

	commit, err := m.run(taskID, "git", "-c", "user.email=orch@local", "-c", "user.name=orch",
		"commit-tree", tree, "-p", base, "-m", message)
	if err != nil {
		return Landing{}, fmt.Errorf("worktree: committing %s's squash: %w", taskID, err)
	}
	commit = strings.TrimSpace(commit)

	list, err := m.run(taskID, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return Landing{}, fmt.Errorf("worktree: finding where %s is checked out: %w", baseBranch, err)
	}
	if at := worktreeUsing(list, baseBranch); at != "" {
		_, err = m.run(taskID, "git", "-C", at, "merge", "--ff-only", "--quiet", commit)
		if err != nil {
			return Landing{}, fmt.Errorf("worktree: moving %s in %s to %s's work (uncommitted changes to the same files?): %w", baseBranch, at, taskID, err)
		}
	} else if _, err := m.run(taskID, "git", "update-ref", "refs/heads/"+baseBranch, commit, base); err != nil {
		return Landing{}, fmt.Errorf("worktree: moving %s to %s's work: %w", baseBranch, taskID, err)
	}
	if err := m.dropLanded(taskID); err != nil {
		slog.Warn("worktree: the task landed, but removing its branch failed", "task_id", taskID, "error", err)
	}
	return Landing{Commit: commit}, nil
}

// mergeTree merges branch into base without a checkout, returning the merged
// tree, or the conflicting paths.
func (m *Manager) mergeTree(taskID, base, branch string) (tree string, conflicts []string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	// #nosec G204 -- fixed git subcommand; base is a hash, branch orch/<task id>.
	cmd := exec.CommandContext(ctx, "git", "merge-tree", "--write-tree", "--name-only", "--no-messages", "-z", base, branch)
	cmd.Dir = m.root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	fields := strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 })
	var exit *exec.ExitError
	switch {
	case runErr == nil && len(fields) > 0:
		return fields[0], nil, nil
	case errors.As(runErr, &exit) && exit.ExitCode() == 1 && len(fields) > 1:
		return "", fields[1:], nil
	}
	return "", nil, fmt.Errorf("worktree: merging %s into the base: %w: %s", branch, runErr, strings.TrimSpace(stderr.String()))
}

// dropLanded removes a landed task's worktree and branch.
func (m *Manager) dropLanded(taskID string) error {
	return errors.Join(m.Remove(taskID), m.runBestEffort(taskID, "git", "branch", "-D", m.BranchName(taskID)))
}
