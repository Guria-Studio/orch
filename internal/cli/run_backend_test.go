package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/engine"
	"github.com/hectorcanaimero/orch/internal/state"
)

// countingBackend records whether the run wrote through it.
type countingBackend struct {
	state.Backend
	started int
}

func (c *countingBackend) StartRun(ctx context.Context, runID, mode string) error {
	c.started++
	return c.Backend.StartRun(ctx, runID, mode)
}

// A run handed a backend writes through it and opens no database of its
// own: `orch bench` reads the run's spend through that same backend while
// the run goes, and a second open would be a second writer on the file
// (CHECKLIST rule 17). The passed backend's file is not the project's default
// database path, so a run that opened its own would create that file.
func TestRunProjectUsesThePassedBackend(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"tasks.json":                      `{"tasks":[{"id":"T-1","phase":1,"title":"t","model":"claude/haiku","status":"todo","dependencies":[],"estimateHours":1,"files":[],"specRef":""}]}`,
		".orchestrator/config.yaml":       "concurrency:\n  global_max: 1\n",
		".orchestrator/model_router.yaml": "claude/haiku:\n  backend: claude\n  cli_model: claude-haiku-4-5\n  tier: cheap\n",
		"fake/claude/_default.out":        `{"type":"result","subtype":"success","is_error":false,"total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}`,
		"scripts/task-start.sh":           "#!/bin/sh\nexit 0\n",
		"scripts/task-finish.sh":          "#!/bin/sh\nexit 0\n",
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		// #nosec G306 -- the fixture's task scripts must be executable.
		if err := os.WriteFile(p, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(engine.FakeProviderEnv, filepath.Join(root, "fake"))

	flags := &projectFlags{root: root}
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		t.Fatal(err)
	}
	// The passed backend lives in a different file than the project's
	// default path, so any database the run opened itself would show up.
	db, _, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "passed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	passed := &countingBackend{Backend: state.NewSQLite(db, paths.ID, paths.Root)}

	err = runProject(context.Background(), strings.NewReader(""), io.Discard, flags,
		runOptions{mode: string(engine.ModeAuto), isolated: true, backend: passed})
	if err != nil {
		t.Fatalf("runProject: %v", err)
	}
	if passed.started != 1 {
		t.Errorf("the run started %d times through the passed backend, want 1", passed.started)
	}
	if _, err := os.Stat(paths.SQLitePath(cfg)); !os.IsNotExist(err) {
		t.Errorf("the run opened its own database at %s (stat err %v); it must use the passed backend only",
			paths.SQLitePath(cfg), err)
	}
}

// #344: a second `orch run` on a project another one is running stops before
// it opens the database or dispatches anything, and says which pid holds it.
func TestRunProjectRefusesASecondRun(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"tasks.json":                      `{"tasks":[{"id":"T-1","phase":1,"title":"t","model":"claude/haiku","status":"todo","dependencies":[],"estimateHours":1,"files":[],"specRef":""}]}`,
		".orchestrator/config.yaml":       "concurrency:\n  global_max: 1\n",
		".orchestrator/model_router.yaml": "claude/haiku:\n  backend: claude\n  cli_model: claude-haiku-4-5\n  tier: cheap\n",
		"scripts/task-start.sh":           "#!/bin/sh\nexit 0\n",
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Should the lock not hold, the run must not reach a real CLI.
	t.Setenv(engine.FakeProviderEnv, t.TempDir())
	flags := &projectFlags{root: root}
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		t.Fatal(err)
	}
	held, err := engine.AcquireRunLock(paths.StateDir())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	err = runProject(context.Background(), strings.NewReader(""), io.Discard, flags,
		runOptions{mode: string(engine.ModeAuto)})
	if err == nil {
		t.Fatal("a second run started while another holds the project")
	}
	if want := fmt.Sprintf("pid %d", os.Getpid()); !strings.Contains(err.Error(), want) {
		t.Errorf("the error does not name the holder (%s): %v", want, err)
	}
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 1 {
		t.Errorf("err = %v, want exit code 1", err)
	}
	if _, statErr := os.Stat(paths.SQLitePath(cfg)); !os.IsNotExist(statErr) {
		t.Errorf("the refused run opened the database at %s", paths.SQLitePath(cfg))
	}
}

// --task-locks is the opt-in for several orch instances on one project; the
// run lock must not take that away (CHECKLIST rule 4).
func TestTaskLocksRunsSkipTheRunLock(t *testing.T) {
	dir := t.TempDir()
	held, err := engine.AcquireRunLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	lock, err := takeRunLock(runOptions{taskLocks: true}, dir)
	if err != nil || lock != nil {
		t.Errorf("--task-locks: lock=%v err=%v, want neither", lock, err)
	}
	if _, err := takeRunLock(runOptions{}, dir); err == nil {
		t.Error("without --task-locks a second run got past a held run lock")
	}
}
