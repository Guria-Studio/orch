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
