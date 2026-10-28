package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/atomize"
	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/dashboard"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/providers"
	"github.com/hectorcanaimero/orch/internal/router"
	"github.com/hectorcanaimero/orch/internal/scaffold"
	"github.com/hectorcanaimero/orch/internal/state"
)

// headlessPlan is appended to the idea when the welcome page plans: claude
// runs with `-p`, so nobody can answer it, and the apply gate moves to the
// page, where the operator sees the tasks before they are written.
const headlessPlan = "\n\nYou run headless from orch's welcome page: nobody can answer you. " +
	"Ask nothing; take reasonable defaults and write each one down as an assumption in the PRD. " +
	"The user asked you to run straight through gates 1 and 2. Write the specs and check them " +
	"with `orch atomize --list` until they have no warnings, then STOP before `orch atomize --apply`: " +
	"the user reviews the tasks on the page and applies them there. Never run `orch run`."

// planTools is what the headless planner may run besides reading and editing
// files: the read-only orch commands orch-plan's stages use.
var planTools = []string{"Bash(orch atomize:*)", "Bash(orch doctor:*)", "Bash(orch validate:*)", "Bash(orch explain:*)", "Bash(git status:*)", "Bash(git log:*)"}

// planTimeout bounds a headless plan: a stuck claude must not hold the page.
const planTimeout = 30 * time.Minute

// maxWelcomeSteps bounds the planner's progress the page shows.
const maxWelcomeSteps = 200

// welcomeFlow is dashboard.WelcomeFlow: `orch start`'s steps, one per click.
type welcomeFlow struct {
	ctx  context.Context
	self string

	mu       sync.Mutex
	cwd      string
	root     string
	log      []string
	planning bool
	planDone chan struct{} // closed when the current plan ends
	steps    []providers.Step
	planErr  string
	estimate string
	ready    int
	applied  bool
	url      string
	dash     *exec.Cmd
}

type welcomeCLI struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

type welcomeTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Phase int    `json:"phase"`
	Model string `json:"model"`
}

type welcomeState struct {
	CLIs      []welcomeCLI     `json:"clis"`
	Cwd       string           `json:"cwd"`
	Root      string           `json:"root"`
	Log       []string         `json:"log"`
	Tasks     []welcomeTask    `json:"tasks"`
	Planning  bool             `json:"planning"`
	Steps     []providers.Step `json:"steps"`
	PlanError string           `json:"plan_error,omitempty"`
	Diff      string           `json:"diff,omitempty"`
	Applied   bool             `json:"applied"`
	Estimate  string           `json:"estimate,omitempty"`
	Ready     int              `json:"ready"`
	URL       string           `json:"dashboard_url,omitempty"`
}

func (f *welcomeFlow) say(format string, a ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, fmt.Sprintf(format, a...))
}

func (f *welcomeFlow) State() any {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := welcomeState{
		Cwd: f.cwd, Root: f.root, Log: append([]string{}, f.log...), Planning: f.planning,
		Steps: append([]providers.Step{}, f.steps...), PlanError: f.planErr, Applied: f.applied,
		Estimate: f.estimate, Ready: f.ready, URL: f.url, Tasks: []welcomeTask{},
	}
	for _, b := range startBackends {
		s.CLIs = append(s.CLIs, welcomeCLI{Name: string(b), Installed: router.OnPath(b)})
	}
	if f.root == "" {
		return s
	}
	// The tasks the page shows are the specs' when they would change
	// tasks.json (planned, not applied yet), else tasks.json's own.
	if paths, cfg, err := loadProjectConfig(&projectFlags{root: f.root}); err == nil {
		if merged, diff, parse, err := previewSpecs(paths, cfg); err == nil && len(diff.NewTasks)+len(diff.Updated) > 0 {
			s.Diff = atomize.RenderDiff(diff, parse)
			s.Tasks = welcomeTasks(merged.Tasks)
		} else if tf, err := model.LoadTasksFile(paths.TasksJSON()); err == nil && !scaffold.OnlySampleTask(tf.Tasks) {
			s.Tasks = welcomeTasks(tf.Tasks)
		}
	}
	return s
}

func welcomeTasks(tasks []model.Task) []welcomeTask {
	out := make([]welcomeTask, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, welcomeTask{ID: t.ID, Title: t.Title, Phase: t.Phase, Model: t.Model})
	}
	return out
}

// previewSpecs is `orch atomize` without --apply: tasks.json merged with
// what the specs say, and the diff between them.
func previewSpecs(paths config.Paths, cfg config.Config) (model.TasksFile, atomize.MergeDiff, atomize.ParseResult, error) {
	existing, err := atomize.LoadExisting(paths.TasksJSON())
	if err != nil {
		return model.TasksFile{}, atomize.MergeDiff{}, atomize.ParseResult{}, err
	}
	return atomize.AtomizeDir(filepath.Join(paths.Root, cfg.SpecRoot), paths.ID, existing)
}

// Project is step 1: the folder, a git repository (made one when asked, the
// folder too), and an orch project in it, scaffolded as `orch start` does.
func (f *welcomeFlow) Project(ctx context.Context, path string, gitInit bool) error {
	if f.busy() {
		return errors.New("claude is still planning; wait for it to finish")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("choose a folder")
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if len(installedCLIs()) == 0 {
		return errNoCLI
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		if !gitInit {
			return fmt.Errorf("%s is not a folder; tick \"create it\" to have it made", root)
		}
		if err := os.MkdirAll(root, 0o750); err != nil {
			return err
		}
	}
	if exec.Command("git", "-C", root, "rev-parse", "--git-dir").Run() != nil { // #nosec G204 -- fixed git subcommand on the chosen folder
		if !gitInit {
			return fmt.Errorf("%s is not a git repository; orch gives each task its own branch. Tick \"git init\" to make it one", root)
		}
		if out, err := exec.CommandContext(ctx, "git", "-C", root, "init").CombinedOutput(); err != nil { // #nosec G204 -- fixed git subcommand on the chosen folder
			return fmt.Errorf("git init: %w: %s", err, out)
		}
		f.say("✓ git init %s", root)
	}
	_, tasksErr := os.Stat(filepath.Join(root, "tasks.json"))
	_, orchErr := os.Stat(filepath.Join(root, ".orchestrator"))
	if tasksErr != nil && orchErr != nil {
		if err := scaffoldForStart(ctx, root, f.say); err != nil {
			return err
		}
	} else {
		f.say("✓ %s is already an orch project", root)
	}
	if _, err := resolveAndValidate(&projectFlags{root: root}); err != nil {
		return err
	}
	f.mu.Lock()
	f.root, f.applied, f.estimate, f.planErr, f.steps = root, false, "", "", nil
	f.mu.Unlock()
	return nil
}

func (f *welcomeFlow) busy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.planning
}

// Plan is step 2: claude plans the idea headless with orch-plan, its progress
// read off its stream-json the way a running task's is. It returns once
// claude has started.
func (f *welcomeFlow) Plan(idea string) error {
	f.mu.Lock()
	root, planning := f.root, f.planning
	f.mu.Unlock()
	switch {
	case root == "":
		return errors.New("choose the project's folder first")
	case planning:
		return errors.New("claude is already planning")
	case !router.OnPath(model.BackendClaude):
		return errors.New("planning needs claude (Claude Code) on PATH: https://docs.anthropic.com/en/docs/claude-code")
	}
	idea = strings.TrimSpace(idea)
	if idea == "" {
		idea = planFromRepo
	}
	var skillsOut bytes.Buffer
	if err := installPlanningSkills(&skillsOut); err != nil {
		return err
	}
	for _, l := range strings.Split(strings.TrimSpace(skillsOut.String()), "\n") {
		if l != "" {
			f.say("%s", l)
		}
	}
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits",
		"--allowedTools", strings.Join(planTools, ",")}
	ctx, cancel := context.WithTimeout(f.ctx, planTimeout)
	cmd := exec.CommandContext(ctx, "claude", args...) // #nosec G204 -- fixed flags; the idea goes on stdin
	cmd.Dir = root
	// Its own process group, killed whole on timeout or Ctrl-C: claude forks
	// the tools it runs.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdin = strings.NewReader("/orch-plan " + idea + headlessPlan)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting claude: %w", err)
	}
	done := make(chan struct{})
	f.mu.Lock()
	f.planning, f.planErr, f.steps, f.applied, f.estimate, f.planDone = true, "", nil, false, "", done
	f.mu.Unlock()
	f.say("→ claude plans it with /orch-plan")
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
		for sc.Scan() {
			steps := providers.ActivityOf(sc.Bytes())
			if len(steps) == 0 {
				continue
			}
			f.mu.Lock()
			f.steps = append(f.steps, steps...)
			if n := len(f.steps) - maxWelcomeSteps; n > 0 {
				f.steps = f.steps[n:]
			}
			f.mu.Unlock()
		}
		err := cmd.Wait()
		cancel()
		defer close(done)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.planning = false
		if err != nil {
			f.planErr = strings.TrimSpace(fmt.Sprintf("claude exited with %v\n%s", err, stderr.String()))
		}
	}()
	return nil
}

// Apply is step 3: the specs written into tasks.json, the new tasks promoted
// to todo, and the plan made dispatchable and estimated by `orch start`'s
// own readyPlan.
func (f *welcomeFlow) Apply(ctx context.Context) error {
	f.mu.Lock()
	root, planning := f.root, f.planning
	f.mu.Unlock()
	if root == "" || planning {
		return errors.New("nothing to apply yet")
	}
	flags := &projectFlags{root: root}
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		return err
	}
	merged, diff, _, err := previewSpecs(paths, cfg)
	if err != nil {
		return err
	}
	if len(diff.NewTasks)+len(diff.Updated) > 0 {
		if _, err := atomize.Apply(paths.TasksJSON(), merged, true); err != nil {
			return err
		}
		f.say("✓ tasks.json: %s added, %d updated", plural(len(diff.NewTasks), "task"), len(diff.Updated))
	}
	tf, err := model.LoadTasksFile(paths.TasksJSON())
	if err != nil {
		return err
	}
	if len(tf.Tasks) == 0 || scaffold.OnlySampleTask(tf.Tasks) {
		return errors.New("there are no tasks yet: plan first, or write specs/ by hand")
	}
	if err := promoteToTodo(ctx, paths, cfg, tf.Tasks, diff.NewTasks); err != nil {
		return err
	}
	var out bytes.Buffer
	ready, err := readyPlan(ctx, flags, paths, tf, nil, &out, false)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, out.String())
	}
	f.mu.Lock()
	f.applied, f.estimate, f.ready = true, strings.TrimSpace(out.String()), ready
	f.mu.Unlock()
	return nil
}

// promoteToTodo moves the tasks atomize just added from backlog to todo, as
// orch-tasks' "promote what should run" does — on the page, the operator
// promoted them by applying.
func promoteToTodo(ctx context.Context, paths config.Paths, cfg config.Config, all, added []model.Task) error {
	if len(added) == 0 {
		return nil
	}
	backend, closeDB, err := openBackend(ctx, paths, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()
	if err := backend.Bootstrap(ctx, all); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	note := state.Note{Author: "operator", Body: "applied from the welcome page"}
	for _, t := range added {
		if err := backend.Transition(ctx, t.ID, model.StatusTodo, note); err != nil && !errors.Is(err, state.ErrIllegalTransition) {
			return fmt.Errorf("%s: %w", t.ID, err)
		}
	}
	return nil
}

// Run is step 4: the project's own dashboard, and `orch run` on the first
// tasks, both as children of this process; the page moves to the dashboard.
func (f *welcomeFlow) Run() error {
	f.mu.Lock()
	root, applied, running := f.root, f.applied, f.dash != nil
	f.mu.Unlock()
	switch {
	case !applied:
		return errors.New("apply the plan first")
	case running:
		return errors.New("already running")
	}
	dash := exec.Command(f.self, "dashboard", "--port", "0", "--project-root", root) // #nosec G204 G702 -- this orch, as it was invoked, with fixed arguments
	stdout, err := dash.StdoutPipe()
	if err != nil {
		return err
	}
	if err := dash.Start(); err != nil {
		return fmt.Errorf("starting the dashboard: %w", err)
	}
	url := dashboardURL(stdout, 10*time.Second)
	if url == "" {
		_ = dash.Process.Kill()
		_ = dash.Wait()
		return errors.New("the dashboard did not say where it listens")
	}
	run := exec.Command(f.self, "run", "--max-tasks", strconv.Itoa(startFirstRun), "--project-root", root) // #nosec G204 G702 -- this orch, as it was invoked, with fixed arguments
	run.Stdout, run.Stderr = os.Stdout, os.Stderr
	if err := run.Start(); err != nil {
		_ = dash.Process.Kill()
		_ = dash.Wait()
		return fmt.Errorf("starting orch run: %w", err)
	}
	go func() {
		if err := run.Wait(); err != nil {
			f.say("! orch run: %v", err)
		}
	}()
	f.mu.Lock()
	f.dash, f.url = dash, url
	f.mu.Unlock()
	f.say("→ orch run --max-tasks %d · dashboard %s", startFirstRun, url)
	return nil
}

// stop ends the project dashboard this flow started; `orch run` finishes
// what it dispatched on its own.
func (f *welcomeFlow) stop() {
	f.mu.Lock()
	dash := f.dash
	f.mu.Unlock()
	if dash != nil {
		_ = dash.Process.Kill()
		_ = dash.Wait()
	}
}

// wantsWelcome reports whether `orch dashboard` should serve the welcome page:
// no project was named and the working directory is not one.
func wantsWelcome(flags *projectFlags) bool {
	if flags.root != "" || os.Getenv("ORCH_PROJECT_ROOT") != "" {
		return false
	}
	paths, err := config.ResolvePaths("", flags.id, flags.configPath)
	if err != nil {
		return false
	}
	_, tasksErr := os.Stat(paths.TasksJSON())
	_, orchErr := os.Stat(filepath.Join(paths.Root, ".orchestrator"))
	return tasksErr != nil && orchErr != nil
}

// runWelcome serves the welcome page until Ctrl-C.
func runWelcome(ctx context.Context, cmd *cobra.Command, cfg dashboard.Config) error {
	spa, err := dashboard.SPA()
	if err != nil {
		return fmt.Errorf("reading the embedded SPA: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	flow := &welcomeFlow{ctx: ctx, self: os.Args[0], cwd: cwd}
	defer flow.stop()
	w, err := dashboard.NewWelcome(cfg, dashboard.SPAHandler(spa), flow)
	if err != nil {
		return err
	}
	go func() {
		<-w.Ready()
		if w.BoundAddr() == "" {
			return
		}
		out := cmd.OutOrStdout()
		_, _ = fmt.Fprintf(out, "No orch project here: the welcome page sets one up.\n")
		_, _ = fmt.Fprintf(out, "Orch dashboard running on http://%s/welcome\n", w.BoundAddr())
	}()
	return w.Serve(ctx)
}
