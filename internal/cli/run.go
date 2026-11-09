package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/budget"
	"github.com/Guria-Studio/orch/internal/ci"
	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/doctor"
	"github.com/Guria-Studio/orch/internal/engine"
	"github.com/Guria-Studio/orch/internal/findings"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/notify"
	"github.com/Guria-Studio/orch/internal/pricing"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/receipt"
	"github.com/Guria-Studio/orch/internal/sandbox"
	"github.com/Guria-Studio/orch/internal/scaffold"
)

// newRunCmd ports `orch run` (orchestrator/orch.py's main loop). The loop
// itself lives in internal/engine, and so does its wiring (engine.FromProject,
// #550); this opens the project, says what the operator should know before
// the run starts, and translates the outcome into an exit code.
//
// Flags are the subset the engine actually honours today. Python's `run` also
// takes `--budgets-preset`, `--dry-run` and a few more; they are deliberately
// not registered rather than accepted and ignored, which is the failure mode
// this project has now found four times (`concurrency.per_file` among them) —
// a flag or key that promises a behaviour nothing implements. They arrive in
// the PR that makes them do something. `docs/CLI.md` records the gap.
func newRunCmd(flags *projectFlags) *cobra.Command {
	var opts runOptions

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Walk the DAG, dispatching ready tasks to their CLI agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runProject(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), flags, opts); err != nil {
				return err
			}
			// knowledge.curate_every (#452), once the run's lock is released.
			return curateIfDue(cmd.Context(), cmd.OutOrStdout(), flags, opts.noPush)
		},
	}

	cmd.Flags().StringVar(&opts.mode, "mode", string(engine.ModeAuto),
		"auto dispatches everything ready; semi asks before each critical task")
	cmd.Flags().IntVar(&opts.maxTasks, "max-tasks", 0,
		"Start at most this many tasks (their retries do not count); 0 means no limit")
	cmd.Flags().StringVar(&opts.only, "only", "",
		"Only dispatch tasks whose id matches this glob (dependencies still resolve across the whole DAG)")
	cmd.Flags().BoolVar(&opts.taskLocks, "task-locks", false,
		"Take a per-task lock, so several orch instances can share one project")
	cmd.Flags().BoolVar(&opts.worktreeMode, "worktree-mode", false,
		"Give each task its own git worktree and branch (also settable as dispatch.worktree_mode)")
	cmd.Flags().BoolVar(&opts.noPush, "no-push", false,
		"Skip pushing task branches — for a project with no remote")

	return cmd
}

// runOptions are `orch run`'s flags, as runProject takes them.
type runOptions struct {
	mode         string
	maxTasks     int
	only         string
	taskLocks    bool
	worktreeMode bool
	noPush       bool
	// runID names the run; empty means a fresh session id. `orch bench`
	// picks it up front so it can read the run's receipt while it is going.
	runID string
	// isolated is `orch bench`'s run: no worktrees, no pushes, no PRs, no CI
	// polling and no notifications, whatever the project's config says. A
	// benchmark copy has no git history of its own, must never open a PR on
	// the real remote, and is not news for the project's Slack channel.
	isolated bool
	// project, when set, is the project the run writes through instead of
	// opening its own; the caller closes it. `orch bench` reads the run's
	// spend while it goes, and doing that through the run's own backend keeps
	// one writer on the file (CHECKLIST rule 17).
	project *project.Project
	// tasks, when set, is the whole DAG this run dispatches instead of
	// tasks.json, which is then neither read nor reloaded: `orch knowledge
	// curate`'s one task (#452).
	tasks []model.Task
	// verifier, when set, is the only gate a task's work passes before it is
	// published, whatever verify.enabled says: curate's scope check.
	verifier engine.Verifier
}

// sandboxAvailable is sandbox.Available, swapped by the test that needs a
// kernel without Landlock.
var sandboxAvailable = sandbox.Available

// runProject is `orch run` minus cobra: the one code path that walks a
// project's DAG. `orch bench` calls it too, so a benchmark measures exactly
// what `orch run` does. The run's wiring is engine.FromProject's (#550); this
// decides whether the run may start, says what the operator should know
// before it does, and turns the outcome into an exit code.
func runProject(ctx context.Context, in io.Reader, out io.Writer, flags *projectFlags, opts runOptions) error {
	switch engine.Mode(opts.mode) {
	case engine.ModeAuto, engine.ModeSemi:
	default:
		return withExitCode(1, fmt.Errorf(
			"--mode %q: expected %q or %q", opts.mode, engine.ModeAuto, engine.ModeSemi))
	}

	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		return withExitCode(1, err)
	}
	// Refused rather than run unconfined: the operator turned the sandbox on
	// to keep agents out of their home directory, and a warning scrolling by
	// at the start of a long run would not (#481).
	if cfg.Dispatch.Sandbox {
		if err := sandboxAvailable(); err != nil {
			return withExitCode(1, fmt.Errorf("cannot start: dispatch.sandbox is on but %w — turn it off or run on a kernel with Landlock (see `orch doctor`)", err))
		}
	}

	runLock, err := takeRunLock(opts, paths.StateDir())
	if err != nil {
		return withExitCode(1, err)
	}
	defer runLock.Release()

	p := opts.project
	if p == nil {
		opened, err := project.OpenWith(ctx, paths, cfg)
		if err != nil {
			return withExitCode(1, err)
		}
		defer func() {
			if cerr := opened.Close(); cerr != nil {
				fmt.Fprintf(os.Stderr, "closing the state backend: %v\n", cerr)
			}
		}()
		p = opened
	}

	run, err := engine.FromProject(ctx, p, engine.RunOptions{
		Mode:         engine.Mode(opts.mode),
		MaxTasks:     opts.maxTasks,
		Only:         opts.only,
		TaskLocks:    opts.taskLocks,
		WorktreeMode: opts.worktreeMode,
		NoPush:       opts.noPush,
		RunID:        opts.runID,
		Isolated:     opts.isolated,
		Tasks:        opts.tasks,
		Verifier:     opts.verifier,
		In:           in,
		Out:          out,
	})
	if errors.Is(err, engine.ErrNoTasks) {
		return withExitCode(1, fmt.Errorf("no tasks in %s — plan them with: %s", paths.TasksJSON(), strings.Join(planSteps(), "  then  ")))
	}
	if err != nil {
		return withExitCode(1, err)
	}

	// A typo in a configured command is found here, not mid-task as an exit
	// 127 verify skips (#440). A warning: the run goes ahead.
	for _, m := range doctor.UnresolvedCommands(paths.Root, cfg) {
		_, _ = fmt.Fprintln(os.Stderr, "warning: "+m.Detail+" — fix it in .orchestrator/config.yaml; until then verify skips it")
	}
	noteEstimate(ctx, os.Stderr, p, run.Queue, opts)
	used := usedBackends(run.Tasks, run.Router, paths.TasksJSON())
	warnMissingPermissions(os.Stderr, paths.Root, used)
	if !opts.isolated {
		noteOutdatedCLIs(ctx, os.Stderr, used, cliLatest(cfg))
	}

	code, err := run.Start(ctx)
	if err != nil {
		return withExitCode(1, err)
	}
	reportRunSummary(ctx, os.Stderr, p, run.Notifier, run.RunID)
	noteOutgrownCI(os.Stderr, paths.Root)
	offerReportFindings(ctx, os.Stderr, p)
	noteQueuedFindings(ctx, os.Stderr, p)
	if code != 0 {
		// The loop already said what it was doing on its way out;
		// a second line here would be noise.
		return withSilentExitCode(code)
	}
	return nil
}

// reportRunSummary prints the summary of the run that just ended, and posts
// it to the webhooks when notifications are on (#456). Only a finished run
// has one: a run stopped before its loop ended wrote no sprint_done.
func reportRunSummary(ctx context.Context, w io.Writer, p *project.Project, notifier *notify.Notifier, runID string) {
	tf, _ := project.Definitions(p.Paths.TasksJSON())
	rec, err := receipt.Load(ctx, p.Backend, runID, receipt.Titles(tf.Tasks), pricing.Load(p.Paths.Root))
	if err != nil || rec == nil || !rec.Finished {
		return
	}
	text := rec.Summary(p.Config.Dashboard.SummaryLanguage, p.Config.GitHub.AutoMerge).Text()
	_, _ = fmt.Fprint(w, "\n"+text)
	notifier.RunSummary(ctx, text)
}

// noteEstimate prints `orch estimate`'s first line before anything is
// dispatched (#357). An estimate that cannot be made is not a reason to stop.
func noteEstimate(ctx context.Context, w io.Writer, p *project.Project, queue *engine.TaskQueue, opts runOptions) {
	tasks := queue.AllTasks()
	for i := range tasks {
		tasks[i].Status, _ = queue.Status(tasks[i].ID)
	}
	e, err := projectEstimate(ctx, p, tasks, opts.only, opts.maxTasks)
	if err != nil {
		return
	}
	lines := e.Lines()
	_, _ = fmt.Fprintf(w, "estimate: %s\n", lines[0])
}

// offerReportFindings is report_findings asked at the right moment (#376):
// once, after a run in which a project `orch init` left the offer marker in
// has a finished task. Anyone who has seen orch work can decide whether its
// agents may file public issues about it.
func offerReportFindings(ctx context.Context, w io.Writer, p *project.Project) {
	marker := filepath.Join(p.Paths.Root, ".orchestrator", scaffold.FindingsOfferName)
	if _, err := os.Stat(marker); err != nil {
		return
	}
	done, err := project.AnyDone(ctx, p.Backend)
	if err != nil || !done {
		return
	}
	_, _ = fmt.Fprintln(w, "note: your agents can report problems with orch itself as public GitHub issues, under your gh login, "+
		"to help fix them — set report_findings.enabled: true in .orchestrator/config.yaml to allow it. Asked once.")
	_ = os.Remove(marker)
}

// noteQueuedFindings says, once a run ends, how many reports about orch its
// agents queued for the operator to review and send (#536).
func noteQueuedFindings(ctx context.Context, w io.Writer, p *project.Project) {
	rows, err := p.Backend.QueuedFindings(ctx)
	if err != nil {
		return
	}
	if n := findings.Pending(rows); n > 0 {
		_, _ = fmt.Fprintf(w, "orch found %d bug(s) of its own: review exactly what would be sent, and send or dismiss each, "+
			"with `orch findings` or on the dashboard's Findings page. Nothing is public until you send it.\n", n)
	}
}

// noteOutgrownCI says, once a run ends, that the tasks may have just built
// the code init's placeholder CI was waiting for (#363).
func noteOutgrownCI(w io.Writer, root string) {
	if ci.PlaceholderOutgrown(root) {
		_, _ = fmt.Fprintf(w, "note: %s still checks nothing, and the repository now has a stack — `orch ci setup --force` proposes its real CI.\n", ci.WorkflowPath)
	}
}

// takeRunLock is the one-run-per-project lock, taken before anything is
// opened or dispatched: two runs on a project adopt each other's writes and
// dispatch the same tasks (#344).
//
// Not with --task-locks, whose whole promise is several orch instances
// sharing one project, each taking a distinct set of tasks; the per-task
// locks are what keeps those apart. A nil lock is safe to Release.
func takeRunLock(opts runOptions, stateDir string) (*engine.RunLock, error) {
	if opts.taskLocks {
		return nil, nil
	}
	return engine.AcquireRunLock(stateDir)
}

// newBudgetGate is engine.NewBudgetGate warning on stderr: the provider
// guardrails, nil when the project has none.
func newBudgetGate(paths config.Paths, cfg config.Config, spend budget.SpendReader) *budget.Gate {
	return engine.NewBudgetGate(paths, cfg, spend, os.Stderr)
}
