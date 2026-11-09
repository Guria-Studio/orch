package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Guria-Studio/orch/internal/budget"
	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/knowledge"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/notify"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/providers"
	"github.com/Guria-Studio/orch/internal/router"
	"github.com/Guria-Studio/orch/internal/vcs"
	"github.com/Guria-Studio/orch/internal/verify"
	"github.com/Guria-Studio/orch/internal/worktree"
)

// ErrNoTasks: the project has no task to run. The CLI words it with the
// planning steps that make some.
var ErrNoTasks = errors.New("no tasks")

// RunOptions is what one run asks for on top of the project's config: `orch
// run`'s flags, and what `orch bench` and `orch knowledge curate` change.
type RunOptions struct {
	Mode Mode
	// MaxTasks starts at most this many tasks; 0 means no limit.
	MaxTasks int
	// Only dispatches only the tasks whose id matches this glob.
	Only string
	// TaskLocks takes a per-task lock, for several instances on one project.
	TaskLocks bool
	// WorktreeMode turns on dispatch.worktree_mode for this run.
	WorktreeMode bool
	// NoPush skips pushing task branches.
	NoPush bool
	// RunID names the run; empty means a fresh session id.
	RunID string
	// Isolated is `orch bench`'s run: no worktrees, no pushes, no PRs, no
	// CI polling and no notifications, whatever the project's config says.
	Isolated bool
	// Tasks, when set, is the whole DAG instead of tasks.json, which is then
	// neither read nor reloaded: `orch knowledge curate`'s one task (#452).
	Tasks []model.Task
	// Verifier, when set, is the only gate a task's work passes before it is
	// published, whatever verify.enabled says: curate's scope check.
	Verifier Verifier
	// In and Out are the terminal the semi-mode gate asks on.
	In  io.Reader
	Out io.Writer
	// Stderr takes the run's notes and warnings; nil is os.Stderr.
	Stderr io.Writer
}

// ProjectRun is a run wired from an opened project (#550): FromProject builds
// it, the caller may print what it likes about it, and Start runs it.
type ProjectRun struct {
	Project *project.Project
	// Tasks is the DAG the run walks, as defined (statuses are the Queue's).
	Tasks []model.Task
	// Queue starts from the database's statuses.
	Queue *TaskQueue
	// Router is model_router.yaml, empty when it does not load.
	Router router.Router
	RunID  string
	// Notifier posts to the configured webhooks: a no-op without any, and
	// always on an isolated run.
	Notifier *notify.Notifier

	opts         RunOptions
	stderr       io.Writer
	notifyCfg    config.Config
	worktreeMode bool
	autoPR       bool
	localMerge   bool
	noPush       bool
}

// FromProject wires a run of p: it seeds the database with the DAG, starts
// the queue from the database's statuses, and settles how the work is
// isolated and published (worktrees, pushes, PRs) from the config and opts.
// It refuses a worktree run whose base branch has no commit to branch from.
// Nothing is dispatched and no run is recorded until Start.
func FromProject(ctx context.Context, p *project.Project, opts RunOptions) (*ProjectRun, error) {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	paths, cfg := p.Paths, p.Config

	tasks := opts.Tasks
	if tasks == nil {
		// Tolerant, as `orch status` is: a tasks.json that does not load
		// has no tasks to run.
		if tf, err := project.Definitions(paths.TasksJSON()); err == nil {
			tasks = tf.Tasks
		}
	}
	if len(tasks) == 0 {
		return nil, ErrNoTasks
	}
	if err := p.Backend.Bootstrap(ctx, tasks); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	queue, err := NewTaskQueue(tasks)
	if err != nil {
		return nil, err
	}
	// The database owns runtime status since F-12, so the queue starts from
	// what it says rather than from tasks.json's seed.
	if runtime, rerr := project.Statuses(ctx, p.Backend); rerr != nil {
		_, _ = fmt.Fprintf(stderr, "reading task status; starting from tasks.json: %v\n", rerr)
	} else {
		queue.Hydrate(runtime)
	}

	r := &ProjectRun{Project: p, Tasks: tasks, Queue: queue, opts: opts, stderr: stderr, noPush: opts.NoPush}
	r.worktreeMode = (opts.WorktreeMode || cfg.Dispatch.WorktreeMode) && !opts.Isolated
	r.autoPR = cfg.VCS.AutoPR && !opts.Isolated
	// vcs.provider: local (#455): each finished task is squash-merged into
	// the base here; nothing is pushed and no PR is opened.
	r.localMerge = r.worktreeMode && cfg.VCS.Provider == config.VCSProviderLocal
	if r.worktreeMode {
		if problem := worktree.BaseProblem(paths.Root, cfg.Dispatch.BaseBranch); problem != "" {
			return nil, errors.New("cannot start: " + problem)
		}
		if r.localMerge {
			r.noPush, r.autoPR = true, false
		}
		// A repository with no remote has nowhere to push or open a PR
		// (#376): the work stays on local branches, as doctor's vcs.remote
		// says, instead of every task blocking on a push that cannot work.
		if !worktree.HasRemote(paths.Root) {
			r.noPush = true
			if r.autoPR {
				r.autoPR = false
				_, _ = fmt.Fprintln(stderr, "note: this repository has no git remote — each task's work stays on its local orch/<task-id> branch, with no push and no pull request.")
			}
		}
	}

	// A broken or missing router degrades to no routes, as `orch status`
	// does, rather than stopping the run here.
	r.Router = router.Router{}
	if rtr, err := router.Load(paths.RouterYAML()); err == nil {
		r.Router = rtr
	}
	r.RunID = opts.RunID
	if r.RunID == "" {
		r.RunID = providers.NewSessionID()
	}
	// Built unconditionally: with no webhook configured it is a working
	// no-op, so the engine never has to ask whether the operator wanted
	// notifications.
	r.notifyCfg = cfg
	if opts.Isolated {
		r.notifyCfg.Notifications.SlackWebhook, r.notifyCfg.Notifications.DiscordWebhook = "", ""
	}
	r.Notifier = NewNotifier(r.notifyCfg)
	return r, nil
}

// Start records the run, wires its scheduler and loop over the project's
// backend, and walks the DAG to the end. code is the loop's exit code.
func (r *ProjectRun) Start(ctx context.Context) (code int, err error) {
	opts := r.opts
	paths, cfg, backend := r.Project.Paths, r.Project.Config, r.Project.Backend
	if err := backend.StartRun(ctx, r.RunID, string(opts.Mode)); err != nil {
		return 0, fmt.Errorf("starting the run: %w", err)
	}

	scheduler := NewScheduler(r.Queue, r.Router, SchedulerOptions{
		Mode:              opts.Mode,
		GlobalMax:         cfg.Concurrency.GlobalMax,
		PerProvider:       cfg.Concurrency.PerProvider,
		TimeoutMultiplier: cfg.DefaultTimeoutMult,
		Cfg:               cfg,
		UseTaskLocks:      opts.TaskLocks,
		WorktreeMode:      r.worktreeMode,
		BaseBranch:        cfg.Dispatch.BaseBranch,
		AutoPR:            r.autoPR,
		LocalMerge:        r.localMerge,
		ConflictRetries:   conflictRetries(cfg),
		Only:              opts.Only,
		MaxTasks:          opts.MaxTasks,
		StateDir:          paths.StateDir(),
		DBDir:             filepath.Dir(paths.SQLitePath(cfg)),
		Cwd:               paths.Root,
		ProjectID:         paths.ID,
		RunID:             r.RunID,
		BudgetUSD:         perDispatchCap(cfg),
	})
	scheduler.Backend = NewStateRecorder(backend)
	// The same adapter, named separately because it answers a different
	// question: what a finished dependency reported, for the prompt's
	// context block.
	scheduler.Comments = NewStateRecorder(backend)
	// And a dependency's PR, so one already merged is not merged into its
	// dependent's worktree a second time (#345).
	scheduler.PRs = NewStateRecorder(backend)
	// What `orch provider disable` turned off, read every tick so a switch
	// flipped mid-run takes effect without a restart.
	scheduler.Providers = backend
	// The search index the prompt's project knowledge is ranked with (#451).
	if kr, ok := backend.(knowledge.Ranker); ok && cfg.Knowledge.Enabled {
		scheduler.Knowledge = kr
	}
	scheduler.Notify = r.Notifier
	if opts.Mode == ModeSemi {
		scheduler.Gate = TerminalGate{In: opts.In, Out: opts.Out}
	}
	// One gate, shared: the scheduler asks it per dispatch, the loop whether
	// every provider is capped, and the verify's review whether its reviewer
	// may run (#470). Building two would read the same window twice and let
	// them disagree inside a single tick.
	budgetGate := NewBudgetGate(paths, cfg, backend, r.stderr)
	// Worktree isolation, and the auto-PR + CI chain that rides on it. All
	// three are off unless the project asked: a project with worktree_mode
	// off runs exactly as it did before, in its own root.
	var wtManager WorktreeManager
	if r.worktreeMode {
		wm := worktree.NewManager(paths.Root, !r.noPush)
		wm.Setup = cfg.Dispatch.WorktreeSetup
		wtManager = NewWorktreeManager(wm)
		scheduler.Worktree = wtManager
		if opts.Verifier != nil {
			scheduler.Verifier = opts.Verifier
		} else if cfg.Verify.Enabled {
			scheduler.Verifier = verify.Verifier{Cfg: cfg.Verify, Base: cfg.Dispatch.BaseBranch, RequireTests: cfg.Dispatch.TDD, Rules: cfg.Knowledge.Enabled, SpecRoot: cfg.SpecRoot,
				Once: OneShot{State: backend, Gate: budgetGate}}
		}
	} else if cfg.Verify.Enabled {
		_, _ = fmt.Fprintln(r.stderr, "note: verify.enabled needs dispatch.worktree_mode; the work is not verified before it is published")
	}

	// With no PR, nothing carries a `Closes` keyword to GitHub: the last
	// task of an imported issue to land closes it through gh (#525).
	if r.localMerge {
		scheduler.Issues = vcs.NewGitHubProvider()
	}

	var provider vcs.Provider
	if r.autoPR {
		if wtManager == nil {
			// Python's main() builds the provider only when BOTH are on,
			// for the good reason that there is no branch to open a PR from
			// without a worktree.
			return 0, errors.New("vcs.auto_pr needs dispatch.worktree_mode: there is no branch to open a PR from")
		}
		provider = vcs.NewProvider(vcs.Config{
			Provider: cfg.VCS.Provider,
			Host:     cfg.VCS.Host,
		})
		scheduler.VCS = provider
		scheduler.CIRecorder = backend
		scheduler.OpenPRs = backend
	}

	runner := NewRunner(scheduler)
	// Resume: the same backend answers which dispatches a previous run left
	// behind, so a crashed orch's rows do not hold tasks in-progress forever.
	runner.Dispatches = backend
	// And adopts statuses a person or a failed write changed in the
	// database while this run held another view (#255).
	runner.Statuses = StateRecorder{Backend: backend}
	// And acts on the operator's stop requests (#356).
	runner.Requests = backend
	// And takes tasks.json again when it changes (v0.22): a task added or
	// edited — by `orch task add|edit`, the dashboard or a hand edit — joins
	// this run. Checked by modification time, so an unchanged file costs a
	// stat per tick.
	if opts.Tasks == nil {
		runner.ReloadTasks = reloadTasks(paths.TasksJSON(), backend)
	}

	// The CI poller only has something to watch when a PR can exist.
	if provider != nil {
		runner.CI = &CIPoller{
			Provider:        provider,
			Backend:         backend,
			Worktree:        wtManager,
			PollInterval:    time.Duration(cfg.VCS.CIPollIntervalS) * time.Second,
			MaxRetries:      cfg.VCS.CIMaxRetries,
			ConflictRetries: conflictRetries(cfg),
			AutoMerge:       cfg.GitHub.AutoMerge,
			// Python's `gh pr merge --squash --auto`. Without a method gh
			// refuses to merge non-interactively (#232).
			Squash:           true,
			MergeWhenPassing: true,
		}
	}
	if budgetGate != nil {
		scheduler.Budget = budgetGate
		runner.Budget = budgetGate
		runner.Alerts = newBudgetAlerts(r.notifyCfg, r.Notifier, budgetGate, backend, r.RunID)
	}
	return runner.Run(ctx)
}

// tasksSeeder is the slice of the backend a reload needs.
type tasksSeeder interface {
	Bootstrap(ctx context.Context, tasks []model.Task) error
}

// reloadTasks is the loop's ReloadTasks over tasksJSON: nothing while the
// file's modification time stands still; its tasks, seeded into the
// database, once it moves. The clock starts when it is built.
func reloadTasks(tasksJSON string, backend tasksSeeder) func(ctx context.Context) ([]model.Task, bool, error) {
	tasksMod := fileModTime(tasksJSON)
	return func(ctx context.Context) ([]model.Task, bool, error) {
		mod := fileModTime(tasksJSON)
		if mod.Equal(tasksMod) {
			return nil, false, nil
		}
		// Before reading: a file broken mid-edit is reported once, and read
		// again when it next changes.
		tasksMod = mod
		tf, err := project.Definitions(tasksJSON)
		if err != nil {
			return nil, false, err
		}
		if err := backend.Bootstrap(ctx, tf.Tasks); err != nil {
			return nil, false, err
		}
		return tf.Tasks, true, nil
	}
}

// NewNotifier is the webhooks cfg names, a working no-op when it names none.
func NewNotifier(cfg config.Config) *notify.Notifier {
	return notify.New(
		cfg.Notifications.SlackWebhook,
		cfg.Notifications.DiscordWebhook,
		float64(cfg.Notifications.TimeoutS),
	)
}

// NewBudgetGate loads the provider guardrails, or returns nil when the
// project has none — which is every project that never opted in, and is why
// a missing budgets.yaml is not an error.
//
// Read failures are reported on warn and treated as "no gate" rather than
// failing the run: a guardrail that cannot be loaded must not be able to
// stop work, the same fail-open the per-dispatch check takes when the window
// is unreadable. The path is budget.ResolvePath's, the one doctor and the
// dashboard read too.
func NewBudgetGate(paths config.Paths, cfg config.Config, spend budget.SpendReader, warn io.Writer) *budget.Gate {
	path := budget.ResolvePath(paths.Root, paths.ConfigYAML, cfg.BudgetsConfig)
	if path == "" {
		return nil
	}
	bcfg, err := budget.LoadConfig(path, cfg.BudgetsPreset)
	if err != nil {
		_, _ = fmt.Fprintf(warn, "budget guardrails disabled: %v\n", err)
		return nil
	}
	if bcfg == nil {
		return nil
	}
	bcfg.UnreportedDispatchTokens = cfg.TypicalDispatchToken
	return budget.NewGate(spend, bcfg)
}

// perDispatchCap is budget.per_dispatch_usd as the per-attempt cap claude
// receives as `--max-budget-usd` — only when the project wrote the key. The
// default 5.0 keeps its older meaning (it limits the attempt-3 escalation) and
// caps nothing by itself. nil (no flag) when unwritten, zero or less.
func perDispatchCap(cfg config.Config) *float64 {
	if !cfg.Budget.PerDispatchExplicit || cfg.Budget.PerDispatchUSD <= 0 {
		return nil
	}
	v := cfg.Budget.PerDispatchUSD
	return &v
}

// newBudgetAlerts builds the budget-window alerts, or nil when they would
// have nowhere to go: notifications.budget_alerts off, or no webhook set.
func newBudgetAlerts(cfg config.Config, notifier *notify.Notifier, usage BudgetUsage,
	events AlertEvents, runID string) *BudgetAlerts {
	if !cfg.Notifications.BudgetAlerts || !notifier.Enabled() {
		return nil
	}
	return &BudgetAlerts{
		Usage:  usage,
		Notify: notifier,
		Events: events,
		RunID:  runID,
		Pct:    cfg.Notifications.BudgetAlertPct,
	}
}

// fileModTime is path's modification time, zero when it cannot be read.
func fileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// conflictRetries is retry.conflict.max_attempts as written (#438). A config
// without it is off, so upgrading never starts pushing to PRs and paying for
// agents unasked; `orch init` and `orch start` write 1 into a new project.
func conflictRetries(cfg config.Config) int {
	return cfg.Retry.Conflict.Attempts(0)
}
