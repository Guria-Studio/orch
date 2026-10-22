package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/state"
	"github.com/hectorcanaimero/orch/internal/worktree"
)

// eventSalvageAccepted records work accepted with `orch task salvage --accept`.
const eventSalvageAccepted = "salvage_accepted"

// salvageGitTimeout bounds each git command `orch task salvage` runs.
const salvageGitTimeout = time.Minute

// newTaskSalvageCmd is `orch task salvage` (#391): the work an earlier attempt
// committed and never published — on orch/<id>, or kept by Create under
// orch/<id>-salvage-<time> instead of being deleted — reviewed and, with
// --accept, taken as the task's result without running its agent again.
func newTaskSalvageCmd(flags *projectFlags) *cobra.Command {
	var id, accept string
	cmd := &cobra.Command{
		Use:   "salvage --id ID [--accept BRANCH]",
		Short: "Review the work an earlier attempt committed but never published, and accept it without running the agent again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, cfg, err := loadProjectConfig(flags)
			if err != nil {
				return err
			}
			ctx, out := cmd.Context(), cmd.OutOrStdout()
			base := baseRef(ctx, paths.Root, cfg.Dispatch.BaseBranch)
			found, err := salvageable(ctx, paths.Root, id, base)
			if err != nil {
				return err
			}

			if accept == "" {
				if len(found) == 0 {
					_, err := fmt.Fprintf(out, "%s: no committed work outside %s on orch/%s or a salvage branch\n", id, base, id)
					return err
				}
				for _, b := range found {
					if _, err := fmt.Fprintf(out, "%s — %d commit(s) not on %s:\n%s\n", b.name, commitCount(b.commits), base, indent(b.commits)); err != nil {
						return err
					}
				}
				_, err := fmt.Fprintf(out, "Accept one as %s's result, without running its agent again:\n    orch task salvage --id %s --accept %s\n", id, id, found[0].name)
				return err
			}

			i := slices.IndexFunc(found, func(b salvageBranch) bool { return b.name == accept })
			if i < 0 {
				return withExitCode(1, fmt.Errorf("%s has no committed work outside %s for %s; run `orch task salvage --id %s` to list what there is", accept, base, id, id))
			}
			backend, closeDB, err := openBackend(ctx, paths, cfg)
			if err != nil {
				return err
			}
			defer func() { _ = closeDB() }()
			if err := backend.Bootstrap(ctx, loadDAG(paths)); err != nil {
				return fmt.Errorf("bootstrap: %w", err)
			}
			if _, err := backend.Task(ctx, id); err != nil {
				return steerErr(id, err)
			}
			taskBranch := "orch/" + id
			// Under its task's own name, the work is what a dependent's
			// worktree merges in (#322), as if the attempt had finished.
			if accept != taskBranch {
				if _, err := gitOut(ctx, paths.Root, "rev-parse", "--verify", "--quiet", "refs/heads/"+taskBranch); err == nil {
					return withExitCode(1, fmt.Errorf("%s already exists; delete it or accept it instead (`git branch -D %s` drops what it holds)", taskBranch, taskBranch))
				}
				if _, err := gitOut(ctx, paths.Root, "branch", "-m", accept, taskBranch); err != nil {
					return fmt.Errorf("renaming %s to %s: %w", accept, taskBranch, err)
				}
			}

			note := state.Note{Author: "orch-salvage", Body: fmt.Sprintf("salvaged: %d commit(s) an earlier attempt left on %s, accepted without running the agent again", commitCount(found[i].commits), accept)}
			if err := backend.Transition(ctx, id, model.StatusDone, note); errors.Is(err, state.ErrIllegalTransition) {
				if err = backend.Transition(ctx, id, model.StatusTodo, note); err == nil {
					err = backend.Transition(ctx, id, model.StatusDone, note)
				}
				if err != nil {
					return steerErr(id, err)
				}
			} else if err != nil {
				return steerErr(id, err)
			}
			if err := backend.AppendEvent(ctx, "", state.Event{
				EventType: eventSalvageAccepted, TaskID: id, TS: time.Now().UTC().Format(time.RFC3339),
				Extra: map[string]any{"branch": taskBranch, "from": accept, "commits": commitCount(found[i].commits), "base": base},
			}); err != nil {
				return fmt.Errorf("recording the salvage: %w", err)
			}
			_, err = fmt.Fprintf(out, "%s: done, with the %d commit(s) now on %s. Publish them as a task's work goes out:\n"+
				"    git push -u origin %s\n    gh pr create --head %s --base %s\n",
				id, commitCount(found[i].commits), taskBranch, taskBranch, taskBranch, strings.TrimPrefix(base, "origin/"))
			return err
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Task ID, e.g. F0.1.T5")
	cmd.Flags().StringVar(&accept, "accept", "", "Branch whose commits become the task's result (one the listing names)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

type salvageBranch struct {
	name    string
	commits string // `git log --oneline` of what base lacks, newest first
}

// salvageable lists orch/<id> and its salvage branches that hold commits
// base does not.
func salvageable(ctx context.Context, root, id, base string) ([]salvageBranch, error) {
	refs, err := gitOut(ctx, root, "for-each-ref", "--format=%(refname:short)",
		"refs/heads/orch/"+id, "refs/heads/orch/"+id+worktree.SalvageMarker+"*")
	if err != nil {
		return nil, fmt.Errorf("listing %s's branches: %w", id, err)
	}
	var out []salvageBranch
	for _, name := range strings.Fields(refs) {
		log, err := gitOut(ctx, root, "log", "--oneline", base+".."+name)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		if strings.TrimSpace(log) != "" {
			out = append(out, salvageBranch{name: name, commits: strings.TrimSpace(log)})
		}
	}
	return out, nil
}

// baseRef is origin/<base> when the clone has it, as a task's branch starts
// there, else the local base.
func baseRef(ctx context.Context, root, base string) string {
	if base == "" {
		base = "main"
	}
	if _, err := gitOut(ctx, root, "rev-parse", "--verify", "--quiet", "origin/"+base+"^{commit}"); err == nil {
		return "origin/" + base
	}
	return base
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, salvageGitTimeout)
	defer cancel()
	// #nosec G204 -- fixed git subcommands; branch names come from git itself or the operator's flag, one argument each.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}

func commitCount(log string) int { return len(strings.Split(log, "\n")) }
