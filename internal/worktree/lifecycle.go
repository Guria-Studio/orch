package worktree

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// PurgeOrphanBranch best-effort deletes the local branch for taskID's
// worktree. Exported (not just inlined into Create) so a caller — or a
// test — can reproduce the F-7 cleanup (issue #71) on its own: a prior
// `git worktree add -b` may have created the branch ref before failing
// (rate limit, sqlite contention, a partial disk write), and Remove only
// cleans up the worktree directory, leaving the branch behind. Without
// purging it first, the next Create fails with "a branch named ... already
// exists" and the task blocks permanently.
//
// Unlike Remove, a failure here is not logged: "the branch doesn't exist"
// (git exits 1) is the expected outcome on every normal Create — logging
// it would mean a WARN line on every single task, training whoever reads
// the log to ignore them. The error is still returned rather than
// swallowed inside this function, so a caller with reason to care about a
// specific failure mode can inspect it.
func (m *Manager) PurgeOrphanBranch(taskID string) error {
	return m.runBestEffort(taskID, "git", "branch", "-D", m.BranchName(taskID))
}

// clearLeftoverBranch deletes orch/<taskID> when an earlier attempt left it
// behind and nothing uses it, so `git worktree add -b` can create it afresh —
// unless it holds commits that neither start nor any remote has, which are
// renamed to a salvage branch instead (#391).
//
// PurgeOrphanBranch alone was not enough (#345): when the earlier attempt's
// worktree directory vanished without `git worktree remove` — a killed run, a
// cleaned-up disk — git still lists that worktree, refuses to delete a branch
// it has checked out, and `worktree add -b` then fails with "a branch named
// ... already exists" on every dispatch. `git worktree prune` drops exactly
// those registrations (the ones whose directory is gone) and nothing else.
//
// A branch a worktree that does exist still has checked out is somebody's
// work in progress — most likely another `orch run` on the same project
// (#344) — and is left alone: Create fails, naming where it is checked out.
func (m *Manager) clearLeftoverBranch(taskID, start string) error {
	branch := m.BranchName(taskID)
	if _, err := m.run(taskID, "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		return nil // no branch: the ordinary first dispatch
	}
	prune := []string{"git", "worktree", "prune"}
	if err := m.runBestEffort(taskID, prune...); err != nil {
		logGitWarning(taskID, prune, err)
	}
	list, err := m.run(taskID, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return fmt.Errorf("worktree: listing worktrees to check who uses %s: %w", branch, err)
	}
	if at := worktreeUsing(list, branch); at != "" {
		return fmt.Errorf("worktree: branch %s is still checked out at %s; another orch run may be working on %s — "+
			"stop it, or run `git worktree remove %s` if nothing is", branch, at, taskID, at)
	}
	// Commits neither the new start nor any remote has are the only copy of
	// an earlier attempt's work — one whose task ended blocked before a push
	// (#391). They are kept under a salvage name instead of deleted.
	if out, err := m.run(taskID, "git", "rev-list", "--count", branch, "--not", start, "--remotes"); err == nil {
		if n := strings.TrimSpace(out); n != "" && n != "0" {
			salvage := branch + SalvageMarker + time.Now().UTC().Format("20060102T150405Z")
			if _, err := m.run(taskID, "git", "branch", "-m", branch, salvage); err != nil {
				return fmt.Errorf("worktree: keeping the earlier attempt's work on %s: %w", branch, err)
			}
			slog.Warn("worktree: kept an earlier attempt's unpushed commits instead of deleting them; review them with `orch task salvage`",
				"task_id", taskID, "commits", n, "branch", salvage)
			return nil
		}
	}
	if err := m.PurgeOrphanBranch(taskID); err != nil {
		return fmt.Errorf("worktree: deleting the leftover branch %s: %w", branch, err)
	}
	slog.Info("worktree: deleted a leftover branch from an earlier attempt", "task_id", taskID, "branch", branch)
	return nil
}

// worktreeUsing returns the path of the worktree in `git worktree list
// --porcelain` output that has branch checked out, or "".
func worktreeUsing(porcelain, branch string) string {
	path := ""
	for _, line := range strings.Split(porcelain, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return path
		}
	}
	return ""
}

// startPoint is what a new task branch starts from: the remote's base when
// there is a remote to push to, freshly fetched, else the local base.
//
// A task dispatched right after its dependency's PR merged would otherwise
// branch from a local base that never saw the merge; its PR then conflicts,
// and GitHub runs no CI on a conflicting PR (#236). A fetch that fails (no
// network, no such branch on origin) falls back to the local base with a
// warning — a stale start is a conflict to resolve later, not a reason to
// block the task now.
func (m *Manager) startPoint(taskID, baseBranch string) string {
	if !m.pushEnabled {
		return baseBranch
	}
	if _, err := m.run(taskID, "git", "fetch", "origin", baseBranch); err != nil {
		slog.Warn("worktree: fetching the base failed; branching from the local base",
			"task_id", taskID, "base", baseBranch, "error", err)
		return baseBranch
	}
	return "origin/" + baseBranch
}

// Create makes an isolated worktree for taskID branched off baseBranch, with
// the branch of every task in deps merged in when the base does not already
// contain it (see mergeDependency). A stale worktree directory left by a
// crashed prior run is removed first; clearLeftoverBranch then deletes the
// branch an earlier attempt left behind (see its doc comment) before
// `git worktree add -b` runs, or refuses when a live worktree still uses it.
//
// Removing the stale directory is best-effort — Create can succeed despite it
// failing — but its error is not dropped (rule 19): Remove logs it at WARN,
// and if Create goes on to fail anyway it is folded into the returned error
// via errors.Join so nothing gets lost on the path that actually matters.
func (m *Manager) Create(taskID, baseBranch string, deps ...string) (string, error) {
	return m.CreateWithEnv(taskID, baseBranch, nil, deps...)
}

// CreateWithEnv is Create with setupEnv added to dispatch.worktree_setup's
// environment, on top of orch's own and replacing any variable of the same
// name: the engine hands it the task's COMPOSE_PROJECT_NAME and
// ORCH_TASK_SLOT (#343), so a setup that starts containers starts the
// task's own rather than a sibling's.
func (m *Manager) CreateWithEnv(taskID, baseBranch string, setupEnv []string, deps ...string) (string, error) {
	wtPath := m.WorktreePath(taskID)
	var cleanupErr error
	if pathExists(wtPath) || pathExists(filepath.Join(LegacyDir(m.root), taskID)) {
		cleanupErr = m.Remove(taskID)
	}
	if pathExists(wtPath) {
		// This only fires if Remove silently failed to clear a directory it
		// saw as present, which "git worktree remove --force" essentially
		// never does — kept as a hard stop rather than letting `add -b`
		// collide with it.
		return "", errors.Join(
			fmt.Errorf("worktree: stale worktree at %s could not be removed", wtPath),
			cleanupErr,
		)
	}

	if _, err := m.worktreesDir(); err != nil {
		return "", errors.Join(err, cleanupErr)
	}

	start := m.startPoint(taskID, baseBranch)
	if err := m.clearLeftoverBranch(taskID, start); err != nil {
		return "", errors.Join(err, cleanupErr)
	}

	if _, err := m.run(taskID, "git", "worktree", "add", wtPath, "-b", m.BranchName(taskID), start); err != nil {
		return "", errors.Join(err, cleanupErr)
	}
	for _, dep := range deps {
		if err := m.mergeDependency(taskID, wtPath, dep); err != nil {
			// Leave nothing half-merged behind: the task blocks with the
			// reason, and its next dispatch starts from a clean slate.
			return "", errors.Join(err, m.Remove(taskID), cleanupErr)
		}
	}
	if err := m.runSetup(taskID, wtPath, setupEnv); err != nil {
		return "", errors.Join(err, m.Remove(taskID), cleanupErr)
	}
	// Where the agent's own work starts: after the dependency merges, so
	// ChangedFiles does not count their files as the task's.
	head, err := m.run(taskID, "git", "-C", wtPath, "rev-parse", "HEAD")
	if err != nil {
		return "", errors.Join(err, m.Remove(taskID), cleanupErr)
	}

	m.mu.Lock()
	m.active[taskID] = wtPath
	m.start[taskID] = strings.TrimSpace(head)
	m.mu.Unlock()
	return wtPath, nil
}

// mergeDependency brings dep's branch into taskID's fresh worktree.
//
// A dependency is "done" the moment its agent says so, which is before its PR
// is merged — and its dependents are ready right then. Branching them from the
// base alone handed the agent a tree without the work it was told to build on,
// and a sandboxed agent cannot fetch or merge it in (#322). So the dependency's
// branch is merged here: origin's copy when there is a remote and the branch
// is still there (a task's Push may have raced this dispatch), else the local
// one when there is no remote.
//
// Not merged, because the work is already in the base: a branch the base
// contains, and — with a remote — a branch the remote no longer has, which is
// what a forge deleting it after the merge looks like (#345). Its local copy
// is not merged instead: after a squash merge it conflicts with the squashed
// commit that already holds the same work. A squash-merged branch still on
// the remote is the engine's to leave out of deps; git cannot tell.
//
// A merge that conflicts is returned as an error naming the dependency: the
// dependency's own PR conflicts with the base too, and that is for a person
// to resolve before the dependent can meaningfully run.
func (m *Manager) mergeDependency(taskID, wtPath, dep string) error {
	ref, why := m.dependencyRef(taskID, dep)
	if ref == "" {
		slog.Info("worktree: not merging the dependency's branch; its work is taken to be in the base",
			"task_id", taskID, "dependency", dep, "reason", why)
		return nil
	}
	if _, err := m.run(taskID, "git", "-C", wtPath, "merge-base", "--is-ancestor", ref, "HEAD"); err == nil {
		slog.Debug("worktree: the base already contains the dependency's branch",
			"task_id", taskID, "dependency", dep, "ref", ref)
		return nil
	}
	if _, err := m.run(taskID, "git", "-C", wtPath,
		"-c", "user.email=orch@local", "-c", "user.name=orch",
		"merge", "--no-edit", ref,
	); err != nil {
		abort := []string{"git", "-C", wtPath, "merge", "--abort"}
		if abortErr := m.runBestEffort(taskID, abort...); abortErr != nil {
			logGitWarning(taskID, abort, abortErr)
		}
		return fmt.Errorf("worktree: merging dependency %s (%s) into the worktree of %s: %w", dep, ref, taskID, err)
	}
	return nil
}

// dependencyRef is the freshest ref holding dep's work, or "" and why there
// is none to merge.
//
// With a remote: origin's copy of its branch when the fetch finds it; nothing
// when origin answers that it has no such branch (deleted after its PR
// merged, #345); the local branch when origin cannot be asked at all, so a
// network blip does not drop work the base may lack. Without a remote: the
// local branch, if any.
func (m *Manager) dependencyRef(taskID, dep string) (ref, why string) {
	branch := m.BranchName(dep)
	if m.pushEnabled {
		if _, err := m.run(taskID, "git", "fetch", "origin", branch); err == nil {
			return "origin/" + branch, ""
		}
		heads, err := m.run(taskID, "git", "ls-remote", "--heads", "origin", "refs/heads/"+branch)
		if err == nil && strings.TrimSpace(heads) == "" {
			return "", "the branch is gone from the remote"
		}
	}
	if _, err := m.run(taskID, "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return branch, ""
	}
	return "", "the dependency has no branch"
}

// CommitPending stages and commits any uncommitted changes inside taskID's
// worktree, using an inline git identity so it works even when the
// project's own git config has none set. Returns false (no error) when the
// tree was already clean. Ports commit_pending (Sprint F-6, fix #60): the
// dispatched agent writes files but is never asked to commit them, so
// without this step Push would send an empty branch and Remove would
// silently discard the agent's output.
func (m *Manager) CommitPending(taskID, message string) (bool, error) {
	wt := m.WorktreePath(taskID)

	if err := m.verifyWorktree(taskID, wt); err != nil {
		return false, err
	}

	if _, err := m.run(taskID, "git", "-C", wt, "add", "-A"); err != nil {
		return false, err
	}
	status, err := m.run(taskID, "git", "-C", wt, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(status) == "" {
		return false, nil
	}
	if _, err := m.run(taskID, "git", "-C", wt,
		"-c", "user.email=orch@local", "-c", "user.name=orch",
		"commit", "-m", message,
	); err != nil {
		return false, err
	}
	return true, nil
}

// verifyWorktree confirms wt is still git's registered worktree for taskID,
// not a directory that outlived `git worktree remove` and got recreated by
// a still-running agent (#253). `git -C <dir>` has no notion of "this isn't
// a worktree anymore" — without a `.git` file inside wt to anchor it, it
// walks up the filesystem and resolves to the main checkout's `.git`
// instead, so CommitPending's add/commit would silently run there. Comparing
// `git -C wt rev-parse --show-toplevel` against wt itself catches exactly
// that: for a real worktree it echoes wt back; for a plain directory it
// prints the main checkout's root.
func (m *Manager) verifyWorktree(taskID, wt string) error {
	top, err := m.run(taskID, "git", "-C", wt, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("worktree: the worktree for %s no longer exists: %w", taskID, err)
	}

	top = strings.TrimSpace(top)
	wantReal, resolveErr := filepath.EvalSymlinks(wt)
	if resolveErr != nil {
		wantReal = wt
	}
	gotReal, resolveErr := filepath.EvalSymlinks(top)
	if resolveErr != nil {
		gotReal = top
	}
	if gotReal != wantReal {
		return fmt.Errorf("worktree: the worktree for %s no longer exists (resolved to %s instead of %s)", taskID, top, wt)
	}
	return nil
}

// Push pushes taskID's branch to origin with --force-with-lease, so a
// retried task overwrites its own previous attempt's branch without
// clobbering unrelated remote changes. A no-op when the Manager was built
// with pushEnabled=false (no remote configured).
func (m *Manager) Push(taskID string) error {
	if !m.pushEnabled {
		return nil
	}
	_, err := m.run(taskID, "git", "push", "--force-with-lease", "-u", "origin", m.BranchName(taskID))
	return err
}

// Remove deletes taskID's worktree directory, and the one an orch from
// before #249 left inside the checkout for the same task, which would
// otherwise keep orch/<taskID> checked out and make every later Create fail.
// A no-op if both are absent. Uses --force so a dirty tree (untracked files
// left by a failed agent) is removed without complaint. A git failure here is
// best-effort — matching Python, removal must never block a caller cleaning
// up after a task that already failed for its own reason — but is logged
// (never silenced, rule 19) and returned: a non-nil error means "removed with
// warnings" (the active-worktree bookkeeping is cleared either way), not
// that the task itself should be treated as failed.
func (m *Manager) Remove(taskID string) error {
	var errs []error
	for _, wtPath := range []string{m.WorktreePath(taskID), filepath.Join(LegacyDir(m.root), taskID)} {
		if !pathExists(wtPath) {
			continue
		}
		args := []string{"git", "worktree", "remove", "--force", wtPath}
		if err := m.runBestEffort(taskID, args...); err != nil {
			logGitWarning(taskID, args, err)
			errs = append(errs, err)
		}
	}
	err := errors.Join(errs...)
	m.mu.Lock()
	delete(m.active, taskID)
	delete(m.start, taskID)
	m.mu.Unlock()
	return err
}

// ChangedFiles lists the files committed in taskID's worktree since Create
// handed it to the agent — the task's own work, not its dependencies'. Nil
// for a worktree Create did not make (a CI retry's Recreate), since there is
// no starting point to compare against.
func (m *Manager) ChangedFiles(taskID string) ([]string, error) {
	m.mu.Lock()
	wtPath, start := m.active[taskID], m.start[taskID]
	m.mu.Unlock()
	if wtPath == "" || start == "" {
		return nil, nil
	}
	// -z: paths verbatim, not quoted or split on the spaces they may hold.
	out, err := m.run(taskID, "git", "-C", wtPath, "diff", "--name-only", "-z", start, "HEAD")
	if err != nil {
		return nil, err
	}
	return strings.FieldsFunc(out, func(r rune) bool { return r == 0 }), nil
}

// Recreate checks out the already-pushed branch orch/<taskID> into a fresh
// worktree at the usual path, for CI re-dispatch. Unlike Create, it does
// not branch off a base — it fetches and reuses the remote branch a prior
// run already pushed, so downstream code (context file injection, the
// per-attempt spawn) works unchanged regardless of whether the worktree is
// fresh or recreated.
//
// The fetch only moves origin/orch/<taskID>; checking out the local
// orch/<taskID> ref afterwards would silently ignore it whenever the local
// ref is stale or missing commits an operator (or another orch instance)
// pushed straight to origin (#254). `worktree add -B` resets the local
// branch to match the freshly fetched remote ref before checkout, so the
// re-dispatch always starts from what's actually on origin. If origin has
// no such branch, the fetch fails and Recreate returns an error instead of
// falling back to a possibly-stale local branch.
func (m *Manager) Recreate(taskID string) (string, error) {
	// Clean up any stale dir first. Best-effort — Remove already logs a
	// genuine failure at WARN — but folded into the returned error via
	// errors.Join below if Recreate goes on to fail anyway (rule 19).
	cleanupErr := m.Remove(taskID)
	branch := m.BranchName(taskID)
	wtPath := m.WorktreePath(taskID)
	remoteBranch := "origin/" + branch

	if _, err := m.worktreesDir(); err != nil {
		return "", errors.Join(err, cleanupErr)
	}
	if _, err := m.run(taskID, "git", "fetch", "origin", branch); err != nil {
		return "", errors.Join(err, cleanupErr)
	}
	if _, err := m.run(taskID, "git", "worktree", "add", "-B", branch, wtPath, remoteBranch); err != nil {
		return "", errors.Join(err, cleanupErr)
	}
	if err := m.runSetup(taskID, wtPath, nil); err != nil {
		return "", errors.Join(err, m.Remove(taskID), cleanupErr)
	}

	m.mu.Lock()
	m.active[taskID] = wtPath
	m.mu.Unlock()
	return wtPath, nil
}

// MergeResult is what merging the base into a task's worktree left there.
type MergeResult struct {
	// Moved reports whether the merge brought anything in: false when the
	// branch already contained the base.
	Moved bool
	// Conflicts are the paths the merge stopped on, with the merge left in
	// progress; empty when it went through.
	Conflicts []string
	// Commits are the base's commits that touched them, "<hash> <subject>".
	Commits []string
}

// MergeBase merges the freshly fetched base into taskID's worktree (#438).
// A merge, not a rebase: orch squash-merges its PRs, so the branch's history
// is flattened anyway, and a merge stops once with every conflict at once
// where a rebase stops per commit. It also keeps the push a fast-forward.
//
// A clean merge is committed. One that conflicts is left in progress, with
// the markers in the files, for the agent to resolve; committing what it
// leaves (CommitPending) concludes it. Any other failure is aborted.
func (m *Manager) MergeBase(taskID, baseBranch string) (MergeResult, error) {
	wt := m.WorktreePath(taskID)
	ref := baseBranch
	if m.pushEnabled {
		if _, err := m.run(taskID, "git", "fetch", "origin", baseBranch); err != nil {
			return MergeResult{}, fmt.Errorf("worktree: fetching %s to merge into %s: %w", baseBranch, taskID, err)
		}
		ref = "origin/" + baseBranch
	}
	before, err := m.run(taskID, "git", "-C", wt, "rev-parse", "HEAD")
	if err != nil {
		return MergeResult{}, fmt.Errorf("worktree: reading the head of %s: %w", taskID, err)
	}
	_, mergeErr := m.run(taskID, "git", "-C", wt,
		"-c", "user.email=orch@local", "-c", "user.name=orch",
		"merge", "--no-edit", ref)
	if mergeErr == nil {
		after, err := m.run(taskID, "git", "-C", wt, "rev-parse", "HEAD")
		if err != nil {
			return MergeResult{}, fmt.Errorf("worktree: reading the head of %s after the merge: %w", taskID, err)
		}
		return MergeResult{Moved: after != before}, nil
	}
	out, err := m.run(taskID, "git", "-C", wt, "diff", "--name-only", "-z", "--diff-filter=U")
	files := strings.FieldsFunc(out, func(r rune) bool { return r == 0 })
	if err != nil || len(files) == 0 {
		abort := []string{"git", "-C", wt, "merge", "--abort"}
		if abortErr := m.runBestEffort(taskID, abort...); abortErr != nil {
			logGitWarning(taskID, abort, abortErr)
		}
		return MergeResult{}, fmt.Errorf("worktree: merging %s into %s: %w", ref, taskID, errors.Join(mergeErr, err))
	}
	logArgs := append([]string{"git", "-C", wt, "log", "--format=%h %s", "HEAD..MERGE_HEAD", "--"}, files...)
	log, err := m.run(taskID, logArgs...)
	if err != nil {
		// The files are what the agent needs; the commits only say why.
		slog.Warn("worktree: listing the base commits behind a conflict failed",
			"task_id", taskID, "error", err)
	}
	var commits []string
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		if line != "" {
			commits = append(commits, line)
		}
	}
	return MergeResult{Moved: true, Conflicts: files, Commits: commits}, nil
}

// RemoveAll removes every tracked worktree. Called from the run loop's
// shutdown path (SIGTERM/drain), after — not during — the drain wait: see
// the package doc comment's caller-contract section. Returns the task IDs
// that were removed with warnings (Remove's error was non-nil — already
// logged by runBestEffort), if a caller wants to report that; a shutdown
// path that doesn't care can ignore the return.
func (m *Manager) RemoveAll() []string {
	m.mu.Lock()
	ids := make([]string, 0, len(m.active))
	for id := range m.active {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	var withWarnings []string
	for _, id := range ids {
		if err := m.Remove(id); err != nil {
			withWarnings = append(withWarnings, id)
		}
	}
	return withWarnings
}

// runSetup runs m.Setup inside a fresh worktree (#324). A worktree is a clean
// checkout — no node_modules, no .venv — so without it every agent's first
// test run failed, and the agent had to decide whether installing from the
// lockfile broke its "do not touch dependencies" rule. The error carries the
// tail of the command's output, which is where an installer says why. env,
// when non-empty, is added to the inherited environment (see CreateWithEnv).
func (m *Manager) runSetup(taskID, wtPath string, env []string) error {
	if strings.TrimSpace(m.Setup) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	// #nosec G204 -- dispatch.worktree_setup is the project owner's own command.
	cmd := exec.CommandContext(ctx, "sh", "-c", m.Setup)
	cmd.Dir = wtPath
	if len(env) > 0 {
		cmd.Env = overrideEnv(os.Environ(), env)
	}
	// Its own process group, killed whole on timeout: an installer forks.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	tail := strings.TrimSpace(string(out))
	if len(tail) > 500 {
		tail = "…" + tail[len(tail)-500:]
	}
	return fmt.Errorf("worktree: dispatch.worktree_setup %q failed for %s: %w: %s", m.Setup, taskID, err, tail)
}

// overrideEnv is base with every variable vars sets replaced by vars' value.
func overrideEnv(base, vars []string) []string {
	set := make(map[string]bool, len(vars))
	for _, kv := range vars {
		key, _, _ := strings.Cut(kv, "=")
		set[key] = true
	}
	out := make([]string, 0, len(base)+len(vars))
	for _, kv := range base {
		if key, _, _ := strings.Cut(kv, "="); !set[key] {
			out = append(out, kv)
		}
	}
	return append(out, vars...)
}

// SalvageMarker sits between a task's branch name and a timestamp on a branch
// that keeps an earlier attempt's unpushed work (#391):
// orch/<id>-salvage-20261022T213000Z.
const SalvageMarker = "-salvage-"

// SalvageTaskID is the task a salvage branch belongs to, and whether branch
// is one. It takes the short name (`orch/F0.1.T5-salvage-…`).
func SalvageTaskID(branch string) (string, bool) {
	rest, ok := strings.CutPrefix(branch, "orch/")
	if !ok {
		return "", false
	}
	id, _, found := strings.Cut(rest, SalvageMarker)
	return id, found && id != ""
}
