package project_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
)

// openTransitionProject is a project over a fresh database seeded with one
// task per status given, ids T-1, T-2, ...
func openTransitionProject(t *testing.T, statuses ...model.Status) (*project.Project, state.Backend) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, _, err := state.Open(ctx, filepath.Join(dir, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", dir)
	var tasks []model.Task
	for i, s := range statuses {
		tasks = append(tasks, model.Task{ID: "T-" + string(rune('1'+i)), Title: "t", Status: s})
	}
	if err := backend.Bootstrap(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	return project.New(config.Paths{Root: dir}, config.Config{}, backend), backend
}

// The project's sentinels are what a front end tests for: they have to match
// whatever the backend returned, through any wrapping on the way.
func TestSetStatus(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		to      model.Status
		wantErr error
		want    model.Status // T-1's status afterwards
	}{
		{"legal move", "T-1", model.StatusInProgress, nil, model.StatusInProgress},
		{"illegal move", "T-1", model.StatusBacklog, project.ErrIllegalTransition, model.StatusTodo},
		{"unknown task", "NOPE", model.StatusDone, project.ErrTaskNotFound, model.StatusTodo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			p, backend := openTransitionProject(t, model.StatusTodo)
			err := p.SetStatus(ctx, tc.id, tc.to, project.Note{Author: "operator", Body: "by hand"})
			if tc.wantErr == nil && err != nil {
				t.Fatalf("SetStatus = %v, want nil", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("SetStatus = %v, want %v", err, tc.wantErr)
			}
			rt, err := backend.Task(ctx, "T-1")
			if err != nil {
				t.Fatal(err)
			}
			if rt.Status != tc.want {
				t.Errorf("T-1 is %s, want %s", rt.Status, tc.want)
			}
		})
	}
}

// The note is the task's comment, author and body as given: `orch task set`
// and the task scripts are told apart by it (prompt.AgentComment, bug 24).
func TestSetStatusRecordsTheNote(t *testing.T) {
	ctx := context.Background()
	p, backend := openTransitionProject(t, model.StatusTodo)
	if err := project.SetStatus(ctx, backend, "T-1", model.StatusInProgress, project.Note{Author: "claude-opus", Body: "starting"}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetStatus(ctx, "T-1", model.StatusDone, project.Note{Author: "operator", Body: "manual"}); err != nil {
		t.Fatal(err)
	}
	rt, err := backend.Task(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	for _, raw := range rt.Comments {
		var c map[string]any
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		got = append(got, map[string]any{"author": c["author"], "body": c["body"]})
	}
	want := []map[string]any{
		{"author": "claude-opus", "body": "starting"},
		{"author": "operator", "body": "manual"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("comments = %v, want %v", got, want)
	}
}

// The illegal-move error keeps the backend's message, which names the legal
// destinations: `orch task set` prints it as is.
func TestSetStatusKeepsTheBackendsMessage(t *testing.T) {
	ctx := context.Background()
	p, _ := openTransitionProject(t, model.StatusDone)
	err := p.SetStatus(ctx, "T-1", model.StatusBlocked, project.Note{Author: "operator"})
	want := `"T-1": illegal transition done -> blocked (legal from done: todo, done)`
	if err == nil || err.Error() != want {
		t.Errorf("SetStatus = %v, want %q", err, want)
	}
}

func TestTaskStatus(t *testing.T) {
	ctx := context.Background()
	p, backend := openTransitionProject(t, model.StatusBlocked)
	got, err := p.TaskStatus(ctx, "T-1")
	if err != nil || got != model.StatusBlocked {
		t.Errorf("TaskStatus(T-1) = %q, %v; want blocked", got, err)
	}
	if _, err := project.TaskStatus(ctx, backend, "NOPE"); !errors.Is(err, project.ErrTaskNotFound) {
		t.Errorf("TaskStatus(NOPE) = %v, want ErrTaskNotFound", err)
	}
}

// TaskIDsIn is what `orch reset` lists: the database's status, never the
// file's (rule 30), in task-id order.
func TestTaskIDsIn(t *testing.T) {
	ctx := context.Background()
	_, backend := openTransitionProject(t, model.StatusInProgress, model.StatusTodo, model.StatusInProgress)
	got, err := project.TaskIDsIn(ctx, backend, model.StatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"T-1", "T-3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("TaskIDsIn(in-progress) = %v, want %v", got, want)
	}
	got, err = project.TaskIDsIn(ctx, backend, model.StatusDone)
	if err != nil || len(got) != 0 {
		t.Errorf("TaskIDsIn(done) = %v, %v; want none", got, err)
	}
}

func TestRecordEvent(t *testing.T) {
	ctx := context.Background()
	p, backend := openTransitionProject(t, model.StatusTodo)
	if err := p.RecordEvent(ctx, "T-1", "salvage_accepted", map[string]any{"branch": "orch/T-1"}); err != nil {
		t.Fatal(err)
	}
	evs, err := backend.Events(ctx, "T-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("events = %+v, want one", evs)
	}
	e := evs[0]
	if e.EventType != "salvage_accepted" || e.TaskID != "T-1" || e.RunID != "" || e.Extra["branch"] != "orch/T-1" || !strings.HasPrefix(e.TS, "20") {
		t.Errorf("event = %+v", e)
	}
	if err := p.RecordEvent(ctx, "T-1", "no_such_type", nil); err == nil {
		t.Error("an unknown event type was recorded")
	}
}
