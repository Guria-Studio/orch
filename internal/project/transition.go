package project

import (
	"context"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// The status writes the front ends make by hand — `orch task set`,
// `orch task-status`, `orch reset`, `orch task salvage`, the welcome page,
// the MCP server's orch_set_status — go through here, so cli, dashboard and
// mcp never name the state package to move a task (#587).
//
// Legality stays where it is: the backend's Transition checks the move
// against model.CanTransition inside its own transaction, and that is the
// single writer path (review checklist rules 16 and 17). Nothing here reads
// a status to decide whether a move is allowed.

// The sentinels a front end tests a status write's error against. They are
// the backend's own values, as io/fs.ErrNotExist is os.ErrNotExist: so
// errors.Is matches every error that carries one, whichever path wrapped it
// — SetStatus, Steer, TaskEditor or the engine — and the message, which
// `orch task set` prints as is, stays the backend's.
var (
	// ErrTaskNotFound means the id has no runtime row in the project.
	ErrTaskNotFound = state.ErrTaskNotFound
	// ErrIllegalTransition means the move is not in the transition table.
	// The error's message names the legal destinations.
	ErrIllegalTransition = state.ErrIllegalTransition
)

// Note is why a task moved: recorded in its comments with the author.
// An empty Body records the status name; an empty Author is "orch".
type Note struct {
	Author string
	Body   string
}

// StatusWriter is the slice of the backend SetStatus needs.
type StatusWriter interface {
	Transition(ctx context.Context, id string, to model.Status, note state.Note) error
}

// SetStatus moves a task to `to`, recording note: ErrTaskNotFound when the
// task has no row, ErrIllegalTransition when the table forbids the move. For
// a caller that holds a backend (the MCP server); the rest call
// Project.SetStatus.
func SetStatus(ctx context.Context, w StatusWriter, taskID string, to model.Status, note Note) error {
	return w.Transition(ctx, taskID, to, state.Note{Author: note.Author, Body: note.Body})
}

// SetStatus moves one of the project's tasks; see the package function.
func (p *Project) SetStatus(ctx context.Context, taskID string, to model.Status, note Note) error {
	return SetStatus(ctx, p.Backend, taskID, to, note)
}

// TaskReader is the slice of the backend TaskStatus needs.
type TaskReader interface {
	Task(ctx context.Context, id string) (state.TaskRuntime, error)
}

// TaskStatus is a task's status in the database, which is its status
// (rule 30); ErrTaskNotFound when it has no row.
func TaskStatus(ctx context.Context, r TaskReader, taskID string) (model.Status, error) {
	rt, err := r.Task(ctx, taskID)
	if err != nil {
		return "", err
	}
	return rt.Status, nil
}

// TaskStatus is one of the project's tasks' status; see the package function.
func (p *Project) TaskStatus(ctx context.Context, taskID string) (model.Status, error) {
	return TaskStatus(ctx, p.Backend, taskID)
}

// TaskIDsIn lists the ids of the tasks the database has at status, in task
// id order: what `orch reset` reverts.
func TaskIDsIn(ctx context.Context, r StatusReader, status model.Status) ([]string, error) {
	rows, err := r.Tasks(ctx, state.TaskFilter{Statuses: []model.Status{status}})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// EventWriter is the slice of the backend RecordEvent needs.
type EventWriter interface {
	AppendEvent(ctx context.Context, runID string, e state.Event) error
}

// RecordEvent appends an event about a task from outside any run, stamped
// now. The type must be one the event log knows. The error is the
// backend's, for the caller to say what it was recording.
func RecordEvent(ctx context.Context, w EventWriter, taskID, eventType string, extra map[string]any) error {
	return w.AppendEvent(ctx, "", state.Event{
		EventType: eventType, TaskID: taskID, TS: state.FormatTS(time.Now()), Extra: extra,
	})
}

// RecordEvent appends an event about one of the project's tasks; see the
// package function.
func (p *Project) RecordEvent(ctx context.Context, taskID, eventType string, extra map[string]any) error {
	return RecordEvent(ctx, p.Backend, taskID, eventType, extra)
}
