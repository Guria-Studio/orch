// Package project is what the CLI and the dashboard share about a project:
// tasks.json hydrated with the live status from the state backend, plus the
// per-task figures that can only be derived from the event log.
//
// It exists because both consumers were about to answer the same question in
// two places. `tasks.json` carries a task's status as it was when the file was
// written; the database carries what it is now. Every view that ships task
// rows has to overlay the second on the first, and Python learned that the
// hard way — `_load_tasks_hydrated` was extracted in sprint F-8 (issue #72)
// after three endpoints shipped stale statuses from the file.
//
// Open is the front ends' door to a project: its config, its database and
// its tasks, hydrated (#550).
//
// Imports config, model, graph and state; imported by cli, dashboard and mcp. Nothing here
// knows about HTTP or about a terminal.
package project

import (
	"context"
	"fmt"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// StatusReader is the slice of the backend Hydrate needs: one query for every
// task's current runtime row.
type StatusReader interface {
	Tasks(ctx context.Context, filter state.TaskFilter) ([]state.TaskRuntime, error)
}

// Hydrate overlays the live status AND comments on tasks.json's rows.
//
// One query, not one per task. The per-task form is a round trip each and
// answers the same question; on a project of any size it is the difference
// between a page that loads and one that does not.
//
// # Why comments and not only status
//
// Since F-12 the comments a task accumulates live in
// `tasks_runtime.comments_json` — every `Transition` appends one — while
// tasks.json keeps whatever it was seeded with, which for a scaffolded project
// is nothing. Overlaying only the status meant the dashboard's task detail
// served an empty `comments` array for a project orch had actually run, with
// the notes sitting in the database the same request had already opened.
//
// Nothing is lost by overlaying: `Bootstrap` seeds `comments_json` FROM the
// file, so a runtime row's comments start as the file's and only grow.
//
// (Python has the same hole for the same reason — `_load_tasks_hydrated`
// replaces `status` and nothing else. It is the second half of bug 24, and
// opus-2's notes carry the first.)
//
// A task with no row in the database keeps what tasks.json gave it. That is
// the honest answer before `Bootstrap` has seeded the project — the file is
// the only thing that knows about the task at all.
//
// Order, and everything else about the rows, is left alone: the caller's task
// order is the file's order, which is what every view renders in.
//
// One caveat for a future caller: a hydrated Task is for READING. `model.Task`
// remembers which optional keys its source file had, so saving one back to
// tasks.json would not write comments the file never declared — this overlay
// is not a way to migrate them into the file, and nothing should treat it as
// one.
func Hydrate(ctx context.Context, backend StatusReader, tasks []model.Task) ([]model.Task, error) {
	if len(tasks) == 0 {
		return tasks, nil
	}
	runtime, err := backend.Tasks(ctx, state.TaskFilter{})
	if err != nil {
		return nil, fmt.Errorf("read runtime task status: %w", err)
	}
	if len(runtime) == 0 {
		return tasks, nil
	}

	byID := make(map[string]state.TaskRuntime, len(runtime))
	for _, r := range runtime {
		byID[r.ID] = r
	}

	out := make([]model.Task, len(tasks))
	copy(out, tasks)
	for i := range out {
		row, ok := byID[out[i].ID]
		if !ok {
			continue
		}
		out[i].Status = row.Status
		out[i].Comments = row.Comments
	}
	return out, nil
}

// Load reads tasks.json and hydrates it in one call, for a caller that already
// holds the backend (the dashboard's server, the MCP server). Everything else
// opens the project with Open and asks it for Tasks.
func Load(ctx context.Context, backend StatusReader, tasksJSON string) ([]model.Task, error) {
	f, err := Definitions(tasksJSON)
	if err != nil {
		return nil, err
	}
	return Hydrate(ctx, backend, f.Tasks)
}

// Definitions reads tasks.json exactly as written: the DAG's shape, with the
// status and comments the file was last saved with — stale as soon as a run
// moves a task. It is for callers that want definitions only and no database
// near them (validate, doctor, the router, a rewrite of the file, Bootstrap's
// seed). Anything that shows or decides on a status reads Tasks or Load
// instead (review checklist rule 30).
//
// It is the front ends' only way to read the file (#550,
// TestFrontEndsGoThroughProject).
func Definitions(tasksJSON string) (model.TasksFile, error) {
	return model.LoadTasksFile(tasksJSON)
}

// Project is an opened project: where it lives, its config, and its database.
// Open is the door to it; Close releases the database.
type Project struct {
	Paths   config.Paths
	Config  config.Config
	Backend state.Backend
	db      *state.DB
}

// Open loads the project's config.yaml and opens its database (creating and
// migrating it if needed). paths come from config.ResolvePaths; checking that
// they are a real project is the caller's (the CLI words that error itself).
func Open(ctx context.Context, paths config.Paths) (*Project, error) {
	res, err := config.Load(paths.ConfigYAML, paths.Root)
	if err != nil {
		return nil, fmt.Errorf("config load failed: %w", err)
	}
	dbPath := paths.SQLitePath(res.Config)
	db, _, err := state.Open(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	return &Project{Paths: paths, Config: res.Config, Backend: state.NewSQLite(db, paths.ID, paths.Root), db: db}, nil
}

// Tasks is tasks.json hydrated with the database's live status and comments.
func (p *Project) Tasks(ctx context.Context) ([]model.Task, error) {
	return Load(ctx, p.Backend, p.Paths.TasksJSON())
}

// Close closes the project's database.
func (p *Project) Close() error {
	return p.db.Close()
}

// FinishedAt maps each task id to when it first reached done, for the tasks
// that have. An id with no finish time is absent.
func FinishedAt(ctx context.Context, backend StatusReader) (map[string]string, error) {
	runtime, err := backend.Tasks(ctx, state.TaskFilter{})
	if err != nil {
		return nil, fmt.Errorf("read task finish times: %w", err)
	}
	out := make(map[string]string, len(runtime))
	for _, r := range runtime {
		if r.FinishedAt != "" {
			out[r.ID] = r.FinishedAt
		}
	}
	return out, nil
}
