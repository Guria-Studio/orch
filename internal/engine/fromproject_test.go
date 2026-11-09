package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
)

const fromProjectTask = `{"id":"T-1","phase":1,"title":"t","model":"claude/haiku","status":"todo","dependencies":[],"estimateHours":1,"files":[],"specRef":""}`

// openFixtureProject writes a one-task project answered by the fake claude
// and opens it the way `orch run` does.
func openFixtureProject(t *testing.T, tasksJSON, configYAML string) *project.Project {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{
		"tasks.json":                      tasksJSON,
		".orchestrator/config.yaml":       configYAML,
		".orchestrator/model_router.yaml": "claude/haiku:\n  backend: claude\n  cli_model: claude-haiku-4-5\n  tier: cheap\n",
		"fake/claude/_default.out":        `{"type":"result","subtype":"success","is_error":false,"total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}`,
		"scripts/task-start.sh":           "#!/bin/sh\nexit 0\n",
		"scripts/task-finish.sh":          "#!/bin/sh\nexit 0\n",
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		// #nosec G306 -- the fixture's task scripts must be executable.
		if err := os.WriteFile(p, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(FakeProviderEnv, filepath.Join(root, "fake"))
	paths, err := config.ResolvePaths(root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Open(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// #550: engine.FromProject is the run's wiring. From an opened project it
// seeds the database, starts the queue from the database's statuses, and
// Start walks the DAG through the project's backend to the end.
func TestFromProjectRunsTheDAG(t *testing.T) {
	ctx := context.Background()
	p := openFixtureProject(t, `{"tasks":[`+fromProjectTask+`]}`, "concurrency:\n  global_max: 1\n")
	var stderr bytes.Buffer
	r, err := FromProject(ctx, p, RunOptions{Mode: ModeAuto, Isolated: true, RunID: "run-fp", Stderr: &stderr})
	if err != nil {
		t.Fatalf("FromProject: %v", err)
	}
	if r.RunID != "run-fp" || r.Notifier == nil || r.Queue == nil || len(r.Tasks) != 1 {
		t.Fatalf("run = %+v, want run-fp with a notifier, a queue and the one task", r)
	}
	code, err := r.Start(ctx)
	if err != nil || code != 0 {
		t.Fatalf("Start = %d, %v; stderr:\n%s", code, err, stderr.String())
	}
	tasks, err := p.Tasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Status != model.StatusDone {
		t.Errorf("T-1 = %s after the run, want done", tasks[0].Status)
	}
	run, err := p.Backend.LatestRun(ctx)
	if err != nil || run.RunID != "run-fp" {
		t.Errorf("latest run = %+v, %v; want run-fp", run, err)
	}
}

// The queue starts from what the database says, not from tasks.json's seed:
// a task the database has as done is not dispatched again.
func TestFromProjectHydratesTheQueue(t *testing.T) {
	ctx := context.Background()
	p := openFixtureProject(t, `{"tasks":[`+fromProjectTask+`]}`, "concurrency:\n  global_max: 1\n")
	if err := p.Backend.Bootstrap(ctx, []model.Task{{ID: "T-1", Status: model.StatusDone}}); err != nil {
		t.Fatal(err)
	}
	r, err := FromProject(ctx, p, RunOptions{Mode: ModeAuto, Isolated: true, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Queue.Status("T-1"); got != model.StatusDone {
		t.Errorf("queue has T-1 as %s, want the database's done", got)
	}
}

// No tasks is ErrNoTasks, for the CLI to word with its planning steps.
func TestFromProjectWithNoTasks(t *testing.T) {
	p := openFixtureProject(t, `{"tasks":[]}`, "concurrency:\n  global_max: 1\n")
	if _, err := FromProject(context.Background(), p, RunOptions{Mode: ModeAuto}); !errors.Is(err, ErrNoTasks) {
		t.Errorf("err = %v, want ErrNoTasks", err)
	}
}

// Worktree mode needs a base to branch from: refused before anything starts.
func TestFromProjectRefusesWorktreesWithoutARepository(t *testing.T) {
	p := openFixtureProject(t, `{"tasks":[`+fromProjectTask+`]}`, "dispatch:\n  worktree_mode: true\n")
	_, err := FromProject(context.Background(), p, RunOptions{Mode: ModeAuto, Stderr: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "cannot start: each task works on its own git branch") {
		t.Errorf("err = %v, want the base-branch refusal", err)
	}
}

// vcs.auto_pr without worktrees has no branch to open a PR from. As before
// FromProject, the refusal comes once the run has started.
func TestFromProjectRefusesAutoPRWithoutWorktrees(t *testing.T) {
	ctx := context.Background()
	p := openFixtureProject(t, `{"tasks":[`+fromProjectTask+`]}`, "vcs:\n  auto_pr: true\n")
	r, err := FromProject(ctx, p, RunOptions{Mode: ModeAuto, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start(ctx); err == nil || !strings.Contains(err.Error(), "vcs.auto_pr needs dispatch.worktree_mode") {
		t.Errorf("Start err = %v, want the auto_pr refusal", err)
	}
}
