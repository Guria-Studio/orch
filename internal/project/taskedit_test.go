package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/state"
)

const editTasksJSON = `{
  "meta": {"project": "demo"},
  "phases": [],
  "tasks": [
    {"id": "T1", "phase": 1, "title": "first", "model": "claude/sonnet", "status": "todo", "dependencies": [], "estimateHours": 1, "files": [], "specRef": "", "reason": "", "owner": "keep me"},
    {"id": "T2", "phase": 1, "title": "second", "model": "claude/sonnet", "status": "todo", "dependencies": ["T1"], "estimateHours": 2, "files": [], "specRef": "", "reason": ""}
  ]
}
`

// editor is a TaskEditor over a real tasks.json and database.
func editor(t *testing.T) (TaskEditor, state.Backend) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "tasks.json")
	if err := os.WriteFile(path, []byte(editTasksJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	db, _, err := state.Open(context.Background(), filepath.Join(root, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "demo", root)
	tf, err := model.LoadTasksFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Bootstrap(context.Background(), tf.Tasks); err != nil {
		t.Fatal(err)
	}
	return TaskEditor{
		TasksJSON: path, StateDir: filepath.Join(root, "state"),
		Routes: []string{"claude/sonnet", "codex/gpt-5.6-codex"}, Backend: backend, Source: "test",
	}, backend
}

func load(t *testing.T, e TaskEditor) model.TasksFile {
	t.Helper()
	tf, err := model.LoadTasksFile(e.TasksJSON)
	if err != nil {
		t.Fatal(err)
	}
	return tf
}

// #390: a task's model changes in tasks.json and in the database at once;
// the rest of the file — other tasks, keys orch does not know — stays, and so
// does the task's status. The edit is an event naming what changed.
func TestUpdateChangesTasksJSONAndTheDatabase(t *testing.T) {
	ctx := context.Background()
	e, backend := editor(t)
	if err := backend.Transition(ctx, "T1", model.StatusInProgress, state.Note{}); err != nil {
		t.Fatal(err)
	}
	m := "codex/gpt-5.6-codex"
	got, err := e.Update(ctx, "T1", TaskPatch{Model: &m})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != m || got.Title != "first" {
		t.Errorf("Update returned %+v", got)
	}

	raw, _ := os.ReadFile(e.TasksJSON) // #nosec G304 -- a temp file this test wrote
	tf := load(t, e)
	if tf.Tasks[0].Model != m || tf.Tasks[1].Model != "claude/sonnet" || !strings.Contains(string(raw), `"owner": "keep me"`) {
		t.Errorf("tasks.json after the edit:\n%s", raw)
	}
	if task, _ := backend.Task(ctx, "T1"); task.Status != model.StatusInProgress {
		t.Errorf("status = %q; an edit touched the runtime row", task.Status)
	}
	events, err := backend.Events(ctx, "T1", 0)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.EventType != EventTaskEdited || fmt.Sprint(last.Extra["fields"]) != "[model]" || last.Extra["source"] != "test" {
		t.Errorf("event = %+v", last)
	}
}

// An edit the DAG would not survive writes nothing.
func TestAnInvalidEditWritesNothing(t *testing.T) {
	ctx := context.Background()
	e, _ := editor(t)
	before, _ := os.ReadFile(e.TasksJSON) // #nosec G304 -- a temp file this test wrote

	unrouted := "gemini/nope"
	cycle := []string{"T2"}
	for name, run := range map[string]func() error{
		"model with no route": func() error { _, err := e.Update(ctx, "T1", TaskPatch{Model: &unrouted}); return err },
		"cycle":               func() error { _, err := e.Update(ctx, "T1", TaskPatch{Dependencies: &cycle}); return err },
		"missing dependency": func() error {
			_, err := e.Add(ctx, model.Task{ID: "T3", Title: "x", Model: "claude/sonnet", Dependencies: []string{"T9"}})
			return err
		},
		"duplicate id": func() error {
			_, err := e.Add(ctx, model.Task{ID: "T1", Title: "x", Model: "claude/sonnet"})
			return err
		},
	} {
		if err := run(); !errors.Is(err, ErrInvalidEdit) {
			t.Errorf("%s: err = %v, want ErrInvalidEdit", name, err)
		}
	}
	if after, _ := os.ReadFile(e.TasksJSON); string(after) != string(before) { // #nosec G304 -- as above
		t.Errorf("an invalid edit changed tasks.json:\n%s", after)
	}
	if _, err := e.Update(ctx, "T9", TaskPatch{}); !errors.Is(err, state.ErrTaskNotFound) {
		t.Errorf("unknown task: err = %v, want ErrTaskNotFound", err)
	}
}

// A new task is todo in the database at once, ready for the next run.
func TestAddSeedsTheDatabase(t *testing.T) {
	ctx := context.Background()
	e, backend := editor(t)
	if _, err := e.Add(ctx, model.Task{ID: "T3", Phase: 2, Title: "third", Model: "claude/sonnet", Dependencies: []string{"T2"}, Status: model.StatusDone}); err != nil {
		t.Fatal(err)
	}
	task, err := backend.Task(ctx, "T3")
	if err != nil || task.Status != model.StatusTodo {
		t.Errorf("T3 = %+v, %v; want todo whatever the request said", task, err)
	}
	if tf := load(t, e); len(tf.Tasks) != 3 || tf.Tasks[2].ID != "T3" {
		t.Errorf("tasks.json has %d tasks", len(tf.Tasks))
	}
}

// Two edits at once do not write over each other: the lock serialises the
// read-modify-write.
func TestConcurrentEditsAreSerialised(t *testing.T) {
	ctx := context.Background()
	e, _ := editor(t)
	e.Backend = nil // the database has its own locking; this is about the file
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.Add(ctx, model.Task{ID: fmt.Sprintf("N%d", i), Title: "n", Model: "claude/sonnet"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if tf := load(t, e); len(tf.Tasks) != 12 {
		t.Errorf("tasks.json has %d tasks after 10 concurrent adds, want 12", len(tf.Tasks))
	}
}

// A project that already has errors (here, a model its router lacks) can
// still be edited: only what the edit itself breaks refuses it.
func TestAnEditIsJudgedOnWhatItBreaks(t *testing.T) {
	ctx := context.Background()
	e, _ := editor(t)
	e.Routes = []string{"codex/gpt-5.6-codex"} // neither task's claude/sonnet is routed
	title := "renamed"
	if _, err := e.Update(ctx, "T2", TaskPatch{Title: &title}); err != nil {
		t.Errorf("a title edit was refused for errors it did not cause: %v", err)
	}
	unrouted := "gemini/nope"
	if _, err := e.Update(ctx, "T2", TaskPatch{Model: &unrouted}); !errors.Is(err, ErrInvalidEdit) {
		t.Errorf("an edit that adds an unrouted model: err = %v, want ErrInvalidEdit", err)
	}
}
