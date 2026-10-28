package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/atomize"
	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/graph"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/project"
	"github.com/hectorcanaimero/orch/internal/router"
	"github.com/hectorcanaimero/orch/internal/scaffold"
	"github.com/hectorcanaimero/orch/internal/skills"
)

// startFirstRun is how many tasks `orch start` offers to run: enough to see
// agents work side by side, few enough to spend little.
const startFirstRun = 2

// planFromRepo is what claude is asked to plan when `orch start` gets no idea.
const planFromRepo = "plan the next work for this repository from its README and its open GitHub issues (gh issue list)"

// newStartCmd is `orch start` (#377): from one sentence to agents working.
// claude plans (the embedded orch-plan skills, in the user's own claude
// session); orch checks what it wrote, estimates it and runs the first tasks.
func newStartCmd(flags *projectFlags) *cobra.Command {
	var yes, noRun, replan, noPerms bool
	cmd := &cobra.Command{
		Use:   `start ["what you want to build"]`,
		Short: "From an idea to agents working: scaffold, plan with claude, check, estimate, run",
		Long: "Scaffold an orch project here if there is none (no questions; models from the\n" +
			"CLIs on PATH), install the planning skills, and hand the terminal to claude with\n" +
			"/orch-plan <idea> — with no idea, it plans from the README and open issues. When\n" +
			"claude exits, orch re-routes tasks whose CLI is not installed, validates the\n" +
			"plan, prints its estimate and offers to run the first " + strconv.Itoa(startFirstRun) + " tasks with the\n" +
			"dashboard open. A project that already has tasks skips planning unless --replan.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			idea := ""
			if len(args) == 1 {
				idea = strings.TrimSpace(args[0])
			}
			return runStart(cmd, flags, idea, yes, noRun, replan, noPerms)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Do not ask: allow orch's agent rules and run the first tasks")
	cmd.Flags().BoolVar(&noPerms, "no-permissions", false,
		"Do not allow orch's agent rules in the settings of the CLIs the plan uses; orch claude-permissions / orch agy-permissions add them later")
	cmd.Flags().BoolVar(&noRun, "no-run", false, "Stop after the estimate; run nothing")
	cmd.Flags().BoolVar(&replan, "replan", false, "Plan with claude even though tasks.json has tasks")
	return cmd
}

func runStart(cmd *cobra.Command, flags *projectFlags, idea string, yes, noRun, replan, noPerms bool) error {
	out := cmd.OutOrStdout()
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) }
	term := scaffold.NewWizardIO(cmd.InOrStdin(), out)
	ask := term.Confirm
	if yes {
		ask = nil
	}

	root := flags.root
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolving the current directory: %w", err)
		}
		root = cwd
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", root, err)
	}

	installed := installedCLIs()
	if len(installed) == 0 {
		return withExitCode(1, errNoCLI)
	}
	if err := exec.Command("git", "-C", root, "rev-parse", "--git-dir").Run(); err != nil { // #nosec G204 -- fixed git subcommand on the project root
		return withExitCode(1, fmt.Errorf("%s is not a git repository (%w); orch gives each task its own branch — run `git init` there first", root, err))
	}
	say("✓ %s on PATH · git repository", strings.Join(installed, ", "))

	// A project already here is used as it is; otherwise one is scaffolded
	// with every default and no question.
	_, tasksErr := os.Stat(filepath.Join(root, "tasks.json"))
	_, orchErr := os.Stat(filepath.Join(root, ".orchestrator"))
	if tasksErr != nil && orchErr != nil {
		if err := scaffoldForStart(cmd.Context(), root, say); err != nil {
			return withExitCode(1, err)
		}
	}
	scoped := *flags
	scoped.root = root

	paths, err := resolveAndValidate(&scoped)
	if err != nil {
		return withExitCode(1, err)
	}
	tf, err := model.LoadTasksFile(paths.TasksJSON())
	if err != nil {
		return withExitCode(1, err)
	}

	switch {
	// A project holding only init's sample task has nothing planned yet (#376).
	case len(tf.Tasks) == 0 || scaffold.OnlySampleTask(tf.Tasks) || replan:
		if !router.OnPath(model.BackendClaude) {
			return withExitCode(1, errors.New("planning needs claude (Claude Code) on PATH: "+
				"https://docs.anthropic.com/en/docs/claude-code — or write specs/ by hand and `orch atomize --apply`"))
		}
		if err := installPlanningSkills(out); err != nil {
			return withExitCode(1, err)
		}
		if idea == "" {
			idea = planFromRepo
		}
		say("→ claude plans it with /orch-plan. Answer and correct it there; exit claude (/exit) once tasks.json is written.")
		say("")
		plan := exec.CommandContext(cmd.Context(), "claude", "/orch-plan "+idea) // #nosec G204 -- the user's own idea, passed as one argument
		plan.Dir = root
		plan.Stdin, plan.Stdout, plan.Stderr = cmd.InOrStdin(), out, cmd.ErrOrStderr()
		if err := plan.Run(); err != nil {
			say("! claude exited with %v", err)
		}
		say("")
		if tf, err = model.LoadTasksFile(paths.TasksJSON()); err != nil {
			return withExitCode(1, err)
		}
		if len(tf.Tasks) == 0 || scaffold.OnlySampleTask(tf.Tasks) {
			return withExitCode(1, errors.New("claude finished without writing tasks to tasks.json; run `orch start` again to keep planning"))
		}
	case idea != "":
		say("tasks.json already has %d tasks: planning skipped (--replan plans again).", len(tf.Tasks))
	}

	ready, err := readyPlan(cmd.Context(), &scoped, paths, tf, ask, out, noPerms)
	if err != nil {
		return withExitCode(1, err)
	}
	if ready == 0 {
		say("No task is todo yet: promote the ones to run with `orch task-status <id> todo`, then `orch run`.")
		return nil
	}
	if noRun {
		say("Next: orch run --max-tasks %d", startFirstRun)
		return nil
	}
	if ask != nil {
		ok, err := ask(fmt.Sprintf("Run the first %d now?", startFirstRun))
		if err != nil {
			return err
		}
		if !ok {
			say("Nothing run. `orch run --max-tasks %d` when you are ready.", startFirstRun)
			return nil
		}
	}
	return runWithDashboard(cmd, root, say)
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func orBlankTier(s string) string {
	if s == "" {
		return "none installed"
	}
	return s
}

// installPlanningSkills puts the embedded skills in ~/.claude/skills, saying
// so only when something was written; a copy the user edited is kept.
func installPlanningSkills(out io.Writer) error {
	all, err := skills.List()
	if err != nil {
		return err
	}
	var installed, kept []string
	for _, s := range all {
		res, err := skills.Install(s, skills.TargetClaude, skills.Options{})
		if err != nil {
			return fmt.Errorf("installing the %s skill: %w", s.Name, err)
		}
		switch res.Action {
		case skills.ActionInstalled, skills.ActionUpdated:
			installed = append(installed, s.Name)
		case skills.ActionSkipped:
			kept = append(kept, s.Name)
		}
	}
	if len(installed) > 0 {
		_, _ = fmt.Fprintf(out, "✓ installed claude skills: %s\n", strings.Join(installed, ", "))
	}
	if len(kept) > 0 {
		_, _ = fmt.Fprintf(out, "  kept your edited copy of: %s (`orch install-skills --force` replaces it)\n", strings.Join(kept, ", "))
	}
	return nil
}

// rerouteToInstalled moves every task whose model needs a CLI that is not
// installed to the same tier's route on one that is, saying each move, and
// returns how many moved. A model the project's router lacks is judged by the
// packaged route of that name, else by its `backend/` prefix at the standard
// tier. A tier no installed CLI serves is reported and left as it is.
func rerouteToInstalled(tf *model.TasksFile, rtr, packaged router.Router, installed func(model.Backend) bool, say func(string, ...any)) int {
	moved := 0
	for i, t := range tf.Tasks {
		entry, ok := rtr[t.Model]
		if !ok {
			if entry, ok = packaged[t.Model]; !ok {
				var err error
				if entry, err = router.InferEntry(t.Model, model.TierStandard); err != nil {
					continue // validation names it
				}
			}
		}
		if installed(entry.Backend) {
			continue
		}
		to := rtr.ForTier(entry.Tier, installed)
		if to == "" {
			say("! %s: %s needs %s, which is not on PATH, and no installed CLI has a %s route", t.ID, t.Model, entry.Backend, entry.Tier)
			continue
		}
		say("↪ %s: %s → %s (%s is not on PATH)", t.ID, t.Model, to, entry.Backend)
		tf.Tasks[i].Model = to
		moved++
	}
	return moved
}

// printStartEstimate prints the estimate of the whole plan and returns how
// many tasks a run would dispatch. Status comes from the database: tasks.json
// does not know it.
func printStartEstimate(ctx context.Context, flags *projectFlags, out io.Writer) (ready int, err error) {
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		return 0, err
	}
	backend, closeDB, err := openBackend(ctx, paths, cfg)
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := closeDB(); cerr != nil && err == nil {
			err = fmt.Errorf("closing the project database: %w", cerr)
		}
	}()
	tasks, err := project.Load(ctx, backend, paths.TasksJSON())
	if err != nil {
		return 0, err
	}
	e, err := projectEstimate(ctx, backend, paths, cfg, tasks, "", 0)
	if err != nil {
		return 0, err
	}
	printEstimate(out, e)
	return len(e.Tasks), nil
}

// runWithDashboard starts `orch dashboard` in the background, prints where it
// is, then runs the first tasks in the foreground. When the run ends the
// dashboard keeps serving until Ctrl-C — on a terminal; a script gets its
// exit code instead of a process that never ends.
func runWithDashboard(cmd *cobra.Command, root string, say func(string, ...any)) error {
	// os.Args[0], not os.Executable: it is how this orch was invoked, found
	// on PATH like any command, and also what a test harness named it.
	self := os.Args[0]
	dash := exec.Command(self, "dashboard", "--port", "0", "--project-root", root) // #nosec G204 G702 -- this orch, as it was invoked, with fixed arguments
	stdout, err := dash.StdoutPipe()
	if err != nil {
		return fmt.Errorf("starting the dashboard: %w", err)
	}
	if err := dash.Start(); err != nil {
		say("! the dashboard did not start (%v); the run goes on without it", err)
		dash = nil
	} else if url := dashboardURL(stdout, 10*time.Second); url != "" {
		say("→ dashboard: %s", url)
	} else {
		say("! the dashboard did not say where it listens; `orch dashboard` opens it")
	}

	run := exec.CommandContext(cmd.Context(), self, "run", "--max-tasks", strconv.Itoa(startFirstRun), "--project-root", root) // #nosec G204 G702 -- this orch, as it was invoked, with fixed arguments
	run.Stdin, run.Stdout, run.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
	runErr := run.Run()
	if dash != nil {
		if onTerminal(cmd.OutOrStdout()) {
			say("The dashboard keeps serving; Ctrl-C stops it.")
		} else if err := dash.Process.Kill(); err != nil {
			say("! stopping the dashboard: %v", err)
		}
		// Ctrl-C or the kill ends it with a signal, which is how it is meant
		// to end; only a wait that could not happen is worth saying.
		var exited *exec.ExitError
		if err := dash.Wait(); err != nil && !errors.As(err, &exited) {
			say("! waiting for the dashboard: %v", err)
		}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return withExitCode(exitErr.ExitCode(), errors.New("orch run failed"))
	}
	if runErr != nil {
		return fmt.Errorf("running the first tasks: %w", runErr)
	}
	return nil
}

func onTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// dashboardURL reads the dashboard's banner for its URL, then drains the rest
// so the child never blocks on a full pipe.
func dashboardURL(r io.Reader, wait time.Duration) string {
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(r)
		sent := false
		for sc.Scan() {
			if _, url, ok := strings.Cut(sc.Text(), "running on "); ok && !sent {
				found <- strings.TrimSpace(url)
				sent = true
			}
		}
		if !sent {
			close(found)
		}
	}()
	select {
	case url := <-found:
		return url
	case <-time.After(wait):
		return ""
	}
}

// errNoCLI is what `orch start` and the welcome page say when no AI CLI is
// installed: claude first, since it is the one that plans.
var errNoCLI = errors.New("no AI CLI on PATH (claude, codex, gemini, opencode, agy or muse); " +
	"install one — claude plans the work: https://docs.anthropic.com/en/docs/claude-code")

// startBackends are the CLIs orch dispatches to, in the order it names them.
var startBackends = []model.Backend{model.BackendClaude, model.BackendCodex, model.BackendGemini, model.BackendOpencode, model.BackendAgy, model.BackendMuse}

// installedCLIs names the backends on PATH.
func installedCLIs() []string {
	var installed []string
	for _, b := range startBackends {
		if router.OnPath(b) {
			installed = append(installed, string(b))
		}
	}
	return installed
}

// scaffoldForStart creates a project at root with every default and no
// question: models from the CLIs on PATH, no sample task, and agents in the
// checkout when there is nowhere to open a pull request.
func scaffoldForStart(ctx context.Context, root string, say func(string, ...any)) error {
	lists, failed := scaffold.DiscoverModels(ctx, router.OnPath)
	tiers, err := scaffold.InstalledTierDefaults(router.OnPath, lists)
	if err != nil {
		return err
	}
	res, err := scaffold.Run(scaffold.Options{Root: root, TierDefaults: tiers, NoSampleTask: true})
	if err != nil {
		return fmt.Errorf("scaffolding %s: %w", root, err)
	}
	say("✓ orch project created in %s (premium %s · standard %s · cheap %s)",
		res.Root, orBlankTier(tiers["premium"]), orBlankTier(tiers["standard"]), orBlankTier(tiers["cheap"]))
	routes, err := scaffold.SeedInstalledRoutes(root, router.OnPath, lists)
	if err != nil {
		return fmt.Errorf("adding the routes of the installed CLIs: %w", err)
	}
	say("✓ model_router.yaml: %d routes for the CLIs on PATH", len(routes))
	for _, line := range scaffold.DiscoverySummary(router.OnPath, lists, failed) {
		say("· models: %s", line)
	}
	say("%s", reportFindingsNotice)
	return runLocallyWhenNoForge(root, true, say)
}

// readyPlan takes a planned tasks.json to dispatchable: tasks moved to an
// installed CLI, missing routes added, the DAG validated, orch's agent rules
// granted (ask nil grants without asking), and the estimate printed. It
// returns how many tasks a run would dispatch.
func readyPlan(ctx context.Context, scoped *projectFlags, paths config.Paths, tf model.TasksFile,
	ask func(string) (bool, error), out io.Writer, noPerms bool) (int, error) {
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) }
	rtr, err := router.Load(paths.RouterYAML())
	if err != nil {
		return 0, err
	}
	packaged, err := scaffold.PackagedRouter()
	if err != nil {
		return 0, err
	}
	if moved := rerouteToInstalled(&tf, rtr, packaged, router.OnPath, say); moved > 0 {
		if _, err := atomize.Apply(paths.TasksJSON(), tf, true); err != nil {
			return 0, err
		}
	}
	// A model on an installed CLI that the router lacks gets its route, as
	// `orch router add-missing` would add it.
	if missing := rtr.MissingModels(tf.Tasks); len(missing) > 0 {
		added, _, err := router.AddMissing(paths.RouterYAML(), missing, model.TierStandard)
		if err != nil {
			return 0, err
		}
		if len(added) > 0 {
			say("✓ model_router.yaml: added %s (tier standard; review it)", strings.Join(added, ", "))
		}
		if rtr, err = router.Load(paths.RouterYAML()); err != nil {
			return 0, err
		}
	}
	var problems []string
	for _, p := range graph.Validate(tf.Tasks, rtr.Keys()) {
		if p.Severity == graph.SeverityError {
			problems = append(problems, p.Message)
		}
	}
	if len(problems) > 0 {
		for _, p := range problems {
			say("✗ %s", p)
		}
		return 0, fmt.Errorf("tasks.json has %d problem(s); fix them (`orch validate` lists them) and run `orch start` again", len(problems))
	}
	say("✓ tasks.json: %s, DAG valid, every model routed", plural(len(tf.Tasks), "task"))
	// orch's rules for the CLIs the plan routes to, now that it is routed
	// (#400). --yes writes them, agy's in the user's home included.
	if err := grantPermissions(ask, out, paths.Root, usedBackends(tf.Tasks, rtr, paths.TasksJSON()), noPerms); err != nil {
		return 0, err
	}
	return printStartEstimate(ctx, scoped, out)
}
