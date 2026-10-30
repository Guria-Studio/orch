package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Guria-Studio/orch/internal/graph"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// The events a task edit is recorded as.
const (
	EventTaskAdded  = "task_added"
	EventTaskEdited = "task_edited"
)

// taskEditLockFile is the lock under the state directory that serialises
// edits to tasks.json, so the CLI and the dashboard never write over each
// other. It lives there, not beside tasks.json, so git never sees it.
const taskEditLockFile = "tasks.lock"

// ErrInvalidEdit is an edit the DAG would not survive: a missing dependency,
// a cycle, a model with no route. Nothing is written.
var ErrInvalidEdit = errors.New("the edit leaves tasks.json invalid")

// TaskEditor changes the task definitions in tasks.json — the one way the CLI
// and the dashboard do it (#390, v0.22), so both do exactly the same to a
// task. tasks.json stays the source of a task's definition, versioned with
// git; its status stays the database's, changed through state.Transition.
type TaskEditor struct {
	TasksJSON string
	StateDir  string
	// Routes are model_router.yaml's keys; a task's model must be one. Nil
	// skips the check, for a project with no router.
	Routes []string
	// Backend, when set, gets the edit at once: the definitions are synced
	// (Bootstrap upserts them) and the edit is recorded as an event.
	Backend state.Backend
	// Source names who edited, in the event: cli, dashboard.
	Source string
}

// TaskPatch is a partial change to a task's definition; nil fields stay.
type TaskPatch struct {
	Title         *string   `json:"title,omitempty"`
	Description   *string   `json:"description,omitempty"`
	Model         *string   `json:"model,omitempty"`
	Reason        *string   `json:"reason,omitempty"`
	SpecRef       *string   `json:"spec_ref,omitempty"`
	Phase         *int      `json:"phase,omitempty"`
	EstimateHours *float64  `json:"estimate_hours,omitempty"`
	Dependencies  *[]string `json:"dependencies,omitempty"`
	Files         *[]string `json:"files,omitempty"`
}

// fields names what a patch changes, for the event.
func (p TaskPatch) fields() []string {
	var out []string
	for name, set := range map[string]bool{
		"title": p.Title != nil, "description": p.Description != nil, "model": p.Model != nil,
		"reason": p.Reason != nil, "spec_ref": p.SpecRef != nil, "phase": p.Phase != nil,
		"estimate_hours": p.EstimateHours != nil, "dependencies": p.Dependencies != nil, "files": p.Files != nil,
	} {
		if set {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// Add appends a new task. It starts as todo unless it says backlog.
func (e TaskEditor) Add(ctx context.Context, t model.Task) (model.Task, error) {
	t.ID = strings.TrimSpace(t.ID)
	if t.ID == "" {
		return model.Task{}, fmt.Errorf("%w: a task needs an id", ErrInvalidEdit)
	}
	if t.Status != model.StatusBacklog {
		t.Status = model.StatusTodo
	}
	err := e.edit(ctx, EventTaskAdded, t.ID, nil, func(tf *model.TasksFile) error {
		if slices.ContainsFunc(tf.Tasks, func(o model.Task) bool { return o.ID == t.ID }) {
			return fmt.Errorf("%w: a task %s already exists", ErrInvalidEdit, t.ID)
		}
		tf.Tasks = append(tf.Tasks, t)
		return nil
	})
	return t, err
}

// Update changes the fields p sets on task id.
func (e TaskEditor) Update(ctx context.Context, id string, p TaskPatch) (model.Task, error) {
	var out model.Task
	err := e.edit(ctx, EventTaskEdited, id, p.fields(), func(tf *model.TasksFile) error {
		i := slices.IndexFunc(tf.Tasks, func(o model.Task) bool { return o.ID == id })
		if i < 0 {
			return fmt.Errorf("%s: %w", id, state.ErrTaskNotFound)
		}
		t := &tf.Tasks[i]
		set(&t.Title, p.Title)
		set(&t.Description, p.Description)
		set(&t.Model, p.Model)
		set(&t.Reason, p.Reason)
		set(&t.SpecRef, p.SpecRef)
		set(&t.Phase, p.Phase)
		set(&t.EstimateHours, p.EstimateHours)
		set(&t.Dependencies, p.Dependencies)
		set(&t.Files, p.Files)
		out = *t
		return nil
	})
	return out, err
}

func set[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

// edit is every change's one path: lock, read, change, validate, write
// atomically, sync the database, record the event.
func (e TaskEditor) edit(ctx context.Context, kind, id string, fields []string, change func(*model.TasksFile) error) error {
	unlock, err := lockFile(filepath.Join(e.StateDir, taskEditLockFile))
	if err != nil {
		return err
	}
	defer unlock()

	tf, err := model.LoadTasksFile(e.TasksJSON)
	if err != nil {
		return err
	}
	// Only the errors this edit introduces refuse it: a project that already
	// has some — a model its router lacks — can still be edited, which is
	// how it gets fixed.
	before := map[string]bool{}
	for _, msg := range validationErrors(tf.Tasks, e.Routes) {
		before[msg] = true
	}
	if err := change(&tf); err != nil {
		return err
	}
	var problems []string
	for _, msg := range validationErrors(tf.Tasks, e.Routes) {
		if !before[msg] {
			problems = append(problems, msg)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidEdit, strings.Join(problems, "; "))
	}
	if err := writeTasksFile(e.TasksJSON, tf); err != nil {
		return err
	}

	if e.Backend == nil {
		return nil
	}
	if err := e.Backend.Bootstrap(ctx, tf.Tasks); err != nil {
		return fmt.Errorf("tasks.json is saved, but syncing the database failed: %w", err)
	}
	extra := map[string]any{"source": e.Source}
	if fields != nil {
		extra["fields"] = fields
	}
	if err := e.Backend.AppendEvent(ctx, "", state.Event{
		EventType: kind, TaskID: id, TS: time.Now().UTC().Format(time.RFC3339), Extra: extra,
	}); err != nil {
		return fmt.Errorf("tasks.json is saved, but recording the edit failed: %w", err)
	}
	return nil
}

func validationErrors(tasks []model.Task, routes []string) []string {
	var out []string
	for _, p := range graph.Validate(tasks, routes) {
		if p.Severity == graph.SeverityError {
			out = append(out, p.Message)
		}
	}
	return out
}

// writeTasksFile writes tf the way model.SaveTasksFile encodes it, through a
// temp file and a rename, so a reader — orch run reloading it — never sees
// half a file. No backup: tasks.json is versioned with git, and one per
// dashboard edit would bury the project in tasks.json.bak-* files.
func writeTasksFile(path string, tf model.TasksFile) error {
	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding tasks.json: %w", err)
	}
	tmp := path + ".tmp"
	// #nosec G306 -- tasks.json is a plain, git-tracked file, as model.SaveTasksFile writes it.
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// lockFile takes an exclusive flock on path, creating it, and returns the
// release.
func lockFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- a fixed name under the project's state directory
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil { // #nosec G115 -- a file descriptor fits in an int
		_ = f.Close()
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) // #nosec G115 -- as above
		_ = f.Close()
	}, nil
}
