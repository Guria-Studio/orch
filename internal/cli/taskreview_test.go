package cli_test

import (
	"context"
	"testing"

	"github.com/hectorcanaimero/orch/internal/cli"
	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/prompt"
	"github.com/hectorcanaimero/orch/internal/state"
)

// TestTaskNoteRetryWritesTheNote: #454's Send back from the CLI. `task note
// --retry` leaves the operator note for the next attempt and reopens the
// done task to todo.
func TestTaskNoteRetryWritesTheNote(t *testing.T) {
	root, common := newTestProject(t)
	run := func(args ...string) int { return cli.Run("test", append(args, common...)) }
	if rc := run("task", "set", "--id", "F0.T1", "--status", "done"); rc != 0 {
		t.Fatalf("task set: rc = %d", rc)
	}
	if rc := run("task", "note", "--id", "F0.T1", "--retry", "rename", "the", "flag"); rc != 0 {
		t.Fatalf("task note --retry: rc = %d", rc)
	}

	ctx := context.Background()
	paths, err := config.ResolvePaths(root, "parity-project", "")
	if err != nil {
		t.Fatal(err)
	}
	db, _, err := state.Open(ctx, paths.SQLitePath(config.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	got, err := state.NewSQLite(db, "parity-project", root).Task(ctx, "F0.T1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusTodo {
		t.Errorf("status %s, want todo", got.Status)
	}
	if notes := prompt.OperatorNotes(got.Comments); len(notes) != 1 || notes[0] != "rename the flag" {
		t.Errorf("operator notes %v, want the one sent back with", notes)
	}
}
