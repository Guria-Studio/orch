package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/atomize"
	"github.com/Guria-Studio/orch/internal/estimate"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/pyfmt"
)

// newAtomizeCmd ports `orch atomize` (orchestrator/atomize.py's `main`) —
// parse markdown specs and merge them into tasks.json. Read-only by
// default (prints a diff); `--apply` writes.
//
// Flags, defaults, and exit codes match Python: `--specs-dir` defaults to
// `<project-root>/<cfg.spec_root>` (bug 18 — see docs/brainstorm/go-
// migration-notes/sonnet.md; `spec_root` defaults to "specs", the same
// config key `prompt_builder` uses to compose a dispatched task's
// read-first path, not the `docs` this command used to hardcode),
// `--tasks-json` to `<project-root>/tasks.json`, `--file` bypasses the
// directory walk for a single spec. Exit 2 only for `--file` naming a path
// that doesn't exist; 0 otherwise, including "no spec files found" and
// "--apply passed but nothing to apply" — Python treats both as
// successful no-ops, not errors.
//
// One thing Python's `--apply` does that this does not: after writing
// tasks.json, `main` best-effort syncs the parsed tasks into SQLite's
// `tasks_definition` table via `state_backend.upsert_task_definition`.
// `state.Backend` has no method shaped for that write yet — the same gap
// `task set --model/--backend/--milestone` already documents — so this
// step is skipped rather than faked. See docs/brainstorm/go-migration-
// notes/sonnet.md.
func newAtomizeCmd(flags *projectFlags) *cobra.Command {
	var specsDir, file, tasksJSONPath string
	var listMode, apply, noBackup bool
	var fromIssues, issuesModel string
	var allAuthors bool
	cmd := &cobra.Command{
		Use:   "atomize",
		Short: "Parse markdown specs and merge them into tasks.json (read-only unless --apply)",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, cfg, err := loadProjectConfig(flags)
			if err != nil {
				return err
			}

			specsRoot := filepath.Join(paths.Root, cfg.SpecRoot)
			if specsDir != "" {
				specsRoot, err = filepath.Abs(specsDir)
				if err != nil {
					return fmt.Errorf("resolve --specs-dir: %w", err)
				}
			}
			tasksPath := paths.TasksJSON()
			if tasksJSONPath != "" {
				tasksPath, err = filepath.Abs(tasksJSONPath)
				if err != nil {
					return fmt.Errorf("resolve --tasks-json: %w", err)
				}
			}

			if fromIssues != "" {
				if file != "" {
					return withExitCode(2, errFromIssuesWithFile)
				}
				return runAtomizeFromIssues(cmd, flags, atomizeIssuesOpts{
					query: fromIssues, model: issuesModel, allAuthors: allAuthors,
					specsRoot: specsRoot, tasksPath: tasksPath,
					apply: apply, noBackup: noBackup, list: listMode,
				})
			}
			if issuesModel != "" || allAuthors {
				return withExitCode(2, errors.New("--model and --all-authors only apply with --from-issues"))
			}

			var specFiles []string
			if file != "" {
				abs, aerr := filepath.Abs(file)
				if aerr != nil {
					return fmt.Errorf("resolve --file: %w", aerr)
				}
				if _, serr := os.Stat(abs); serr != nil {
					return withExitCode(2, fmt.Errorf("spec file not found: %s", abs))
				}
				if outsideRoot(abs, specsRoot) {
					if _, werr := fmt.Fprintf(cmd.ErrOrStderr(),
						"warning: %s is outside %s, so its specRef is just %s — any subdirectory "+
							"is lost. Pass --specs-dir, or move the spec under the project's spec root.\n",
						abs, specsRoot, pyfmt.Quote(filepath.Base(abs))); werr != nil {
						return werr
					}
				}
				specFiles = []string{abs}
			} else {
				specFiles, err = atomize.WalkSpecFiles(specsRoot)
				if err != nil {
					return fmt.Errorf("walk specs under %s: %w", specsRoot, err)
				}
			}

			out := cmd.OutOrStdout()
			if len(specFiles) == 0 {
				_, err := fmt.Fprintf(out,
					"No hay specs .md bajo %s (y no se pasó --file). Nada que hacer.\n", specsRoot)
				return err
			}

			parse, err := atomize.ParseFiles(specFiles, specsRoot, paths.ID)
			if err != nil {
				return fmt.Errorf("parse specs under %s: %w", specsRoot, err)
			}

			// Warn, never fail: the operator may be about to commit.
			for _, w := range specGitWarnings(paths.Root, cfg.Dispatch.BaseBranch, parse) {
				if _, err := fmt.Fprintln(cmd.ErrOrStderr(), w); err != nil {
					return err
				}
			}

			if listMode {
				return renderAtomizeList(cmd, parse)
			}

			existing, err := atomize.LoadExisting(tasksPath)
			if err != nil {
				return fmt.Errorf("load %s: %w", tasksPath, err)
			}

			_, diff := atomize.MergeTasks(existing, parse.Tasks)
			if _, err := fmt.Fprint(out, atomize.RenderDiff(diff, parse)); err != nil {
				return err
			}

			if !apply {
				// Where they will land, and what that means for a run (#579).
				if len(diff.NewTasks) > 0 {
					_, err := fmt.Fprintln(out, "New tasks are written in backlog, which orch run does not dispatch: "+
						"after --apply, promote the ones to run with orch task set --id <ID> --status todo.")
					return err
				}
				return nil
			}
			if len(diff.NewTasks) == 0 && len(diff.Updated) == 0 {
				_, err := fmt.Fprintln(out, "--apply pasado pero no hay cambios que aplicar.")
				return err
			}

			// Merged again under the task editor's lock, onto tasks.json as it is
			// now: an edit that landed since the preview is kept (#531).
			var written atomize.MergeDiff
			backupPath, err := project.MutateTasks(paths.StateDir(), tasksPath, !noBackup, func(tf *model.TasksFile) error {
				*tf, written = atomize.MergeTasks(*tf, parse.Tasks)
				return nil
			})
			if err != nil {
				return fmt.Errorf("write %s: %w", tasksPath, err)
			}
			if _, err := fmt.Fprintf(out, "✓ Escrito: %s\n", tasksPath); err != nil {
				return err
			}
			if backupPath != "" {
				if _, err := fmt.Fprintf(out, "  backup: %s\n", backupPath); err != nil {
					return err
				}
			}
			return noteNewInBacklog(out, written.NewTasks)
		},
	}
	cmd.Flags().StringVar(&specsDir, "specs-dir", "",
		"Directorio de specs .md (default: <project-root>/<spec_root>, \"specs\")")
	cmd.Flags().StringVar(&file, "file", "",
		"Sólo un archivo markdown (bypass del walk de --specs-dir)")
	cmd.Flags().StringVar(&tasksJSONPath, "tasks-json", "",
		"Path a tasks.json (default: <project-root>/tasks.json)")
	cmd.Flags().BoolVar(&listMode, "list", false,
		"Modo listar: sólo imprime lo parseado, sin merge/diff")
	cmd.Flags().BoolVar(&apply, "apply", false,
		"Escribí tasks.json (con backup). Sin este flag es read-only.")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false,
		"No crear backup .bak-<ts> al escribir (default: sí crea)")
	cmd.Flags().StringVar(&fromIssues, "from-issues", "",
		"GitHub issue search (e.g. label:orch): each open issue becomes specs/issue-<n>-<slug>.md, then tasks")
	cmd.Flags().StringVar(&issuesModel, "model", "",
		"With --from-issues: model for the imported tasks; must be one model_router.yaml resolves (required)")
	cmd.Flags().BoolVar(&allAuthors, "all-authors", false,
		"With --from-issues: import issues by anyone, not only the repo's collaborators")
	return cmd
}

// noteNewInBacklog says that the tasks --apply just added are in backlog,
// which `orch run` never dispatches, and the command that promotes them
// (#579): a run right after used to report only that nothing was ready.
func noteNewInBacklog(w io.Writer, added []model.Task) error {
	if len(added) == 0 {
		return nil
	}
	ids := make([]string, len(added))
	for i, t := range added {
		ids[i] = t.ID
	}
	noun, pronoun := "task is", "it"
	if len(ids) > 1 {
		noun, pronoun = "tasks are", "them"
	}
	_, err := fmt.Fprintf(w, "%d new %s in backlog, which orch run does not dispatch — promote %s with: %s\n",
		len(ids), noun, pronoun, estimate.PromoteCommand(ids))
	return err
}

// renderAtomizeList is `--list`'s renderer, porting `render_list`'s content
// (scanned files, warnings, a task table) as plain tabular text rather than
// rich's colored table — same convention every human-mode renderer in this
// package follows.
func renderAtomizeList(cmd *cobra.Command, parse atomize.ParseResult) error {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "Specs escaneados: %d archivos\n", len(parse.FilesScanned)); err != nil {
		return err
	}
	for _, p := range parse.FilesScanned {
		if _, err := fmt.Fprintf(out, "  · %s\n", p); err != nil {
			return err
		}
	}
	if len(parse.Warnings) > 0 {
		if _, err := fmt.Fprintf(out, "Warnings: %d\n", len(parse.Warnings)); err != nil {
			return err
		}
		for _, w := range parse.Warnings {
			if _, err := fmt.Fprintf(out, "  · %s\n", w); err != nil {
				return err
			}
		}
	}
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tPHASE\tTITLE\tMODEL\tEST\tDEPS"); err != nil {
		return err
	}
	for _, t := range parse.Tasks {
		model := t.Model
		if model == "" {
			model = "—"
		}
		deps := "—"
		if len(t.Dependencies) > 0 {
			deps = strings.Join(t.Dependencies, ", ")
		}
		if _, err := fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%gh\t%s\n",
			t.ID, t.Phase, t.Title, model, t.EstimateHours, deps); err != nil {
			return err
		}
	}
	return w.Flush()
}

// outsideRoot reports whether abs does not live under root — the same
// out-of-bounds check `internal/atomize`'s own (unexported)
// relpathForSpecRef makes when it falls back to a bare filename for
// `specRef`. Duplicated here (rather than exported from that package)
// because it exists for a different reason on this side: not to compute a
// path, but to warn an operator their `--file` won't get a specRef
// prompt_builder can resolve.
func outsideRoot(abs, root string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
