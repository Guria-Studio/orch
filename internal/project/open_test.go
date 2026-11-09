package project

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// Open's tasks are tasks.json's rows with the database's status: the file
// says todo, the run moved the task on, and Tasks must say so (rule 30).
func TestOpenTasksAreHydrated(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tf := model.TasksFile{Tasks: []model.Task{{ID: "T-1", Title: "t", Model: "claude/opus", Status: model.StatusTodo}}}
	if err := model.SaveTasksFile(filepath.Join(root, "tasks.json"), tf); err != nil {
		t.Fatal(err)
	}
	paths, err := config.ResolvePaths(root, "demo", "")
	if err != nil {
		t.Fatal(err)
	}

	p, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if err := p.Backend.Bootstrap(ctx, tf.Tasks); err != nil {
		t.Fatal(err)
	}
	if err := p.Backend.Transition(ctx, "T-1", model.StatusInProgress, state.Note{}); err != nil {
		t.Fatal(err)
	}

	got, err := p.Tasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != model.StatusInProgress {
		t.Fatalf("Tasks = %+v, want T-1 in_progress from the database", got)
	}
	// Definitions is the file as written: its status is the file's.
	defs, err := Definitions(paths.TasksJSON())
	if err != nil {
		t.Fatal(err)
	}
	if defs.Tasks[0].Status != model.StatusTodo {
		t.Errorf("Definitions status = %q, want the file's todo", defs.Tasks[0].Status)
	}
}

// The read models front ends use instead of naming state's filters and
// sentinels (#550): every runtime row, the status map a run's queue starts
// from, whether anything is done, and the latest run — none being no error.
func TestProjectReadModels(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	paths, err := config.ResolvePaths(root, "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })

	if _, ok, err := LatestRun(ctx, p.Backend); ok || err != nil {
		t.Errorf("LatestRun on a project never run: ok=%v err=%v, want neither", ok, err)
	}
	if done, err := AnyDone(ctx, p.Backend); done || err != nil {
		t.Errorf("AnyDone with no task: %v, %v", done, err)
	}
	if err := p.Backend.Bootstrap(ctx, []model.Task{{ID: "A", Status: model.StatusTodo}, {ID: "B", Status: model.StatusDone}}); err != nil {
		t.Fatal(err)
	}
	if err := p.Backend.StartRun(ctx, "run-1", "auto"); err != nil {
		t.Fatal(err)
	}
	if run, ok, err := LatestRun(ctx, p.Backend); !ok || err != nil || run.RunID != "run-1" {
		t.Errorf("LatestRun = %+v, %v, %v; want run-1", run, ok, err)
	}
	if done, err := AnyDone(ctx, p.Backend); !done || err != nil {
		t.Errorf("AnyDone with B done: %v, %v", done, err)
	}
	statuses, err := Statuses(ctx, p.Backend)
	if err != nil || statuses["A"] != model.StatusTodo || statuses["B"] != model.StatusDone {
		t.Errorf("Statuses = %v, %v", statuses, err)
	}
	if rows, err := Runtime(ctx, p.Backend); err != nil || len(rows) != 2 {
		t.Errorf("Runtime = %d rows, %v; want 2", len(rows), err)
	}
}

// New wraps a backend its caller owns: Close leaves it open.
func TestNewDoesNotCloseTheCallersBackend(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, _, err := state.Open(ctx, filepath.Join(root, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := New(config.Paths{Root: root, ID: "demo"}, config.Config{}, state.NewSQLite(db, "demo", root))
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Backend.Tasks(ctx, state.TaskFilter{}); err != nil {
		t.Errorf("the backend was closed with the project: %v", err)
	}
}
