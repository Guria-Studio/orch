package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hectorcanaimero/orch/internal/project"
)

// The operator's two steering tools (#356). They go through project.Steer,
// the same path as `orch task note|stop` and the dashboard.

func (s *server) steer() project.Steer {
	return project.Steer{Backend: s.opts.Backend, StateDir: s.opts.StateDir, Source: "mcp"}
}

type taskNoteIn struct {
	TaskID string `json:"task_id" jsonschema:"the task's id, e.g. F1.T3"`
	Note   string `json:"note" jsonschema:"what the task's agent should know or do differently"`
}

type taskNoteOut struct {
	TaskID  string `json:"task_id"`
	Message string `json:"message"`
}

func (s *server) taskNote(ctx context.Context, _ *mcpsdk.CallToolRequest, in taskNoteIn) (*mcpsdk.CallToolResult, taskNoteOut, error) {
	if strings.TrimSpace(in.TaskID) == "" {
		return nil, taskNoteOut{}, fmt.Errorf("task_id is required")
	}
	if err := s.steer().Note(ctx, in.TaskID, in.Note); err != nil {
		return nil, taskNoteOut{}, fmt.Errorf("%s: %w", in.TaskID, err)
	}
	return nil, taskNoteOut{TaskID: in.TaskID, Message: "note added; the task's next attempt's prompt carries it"}, nil
}

type taskStopIn struct {
	TaskID string `json:"task_id" jsonschema:"the task's id, e.g. F1.T3"`
	Hold   bool   `json:"hold,omitempty" jsonschema:"keep the task from being dispatched again (blocked) instead of dispatching it again"`
	Note   string `json:"note,omitempty" jsonschema:"a note for the task's next attempt"`
}

func (s *server) taskStop(ctx context.Context, _ *mcpsdk.CallToolRequest, in taskStopIn) (*mcpsdk.CallToolResult, project.StopResult, error) {
	if strings.TrimSpace(in.TaskID) == "" {
		return nil, project.StopResult{}, fmt.Errorf("task_id is required")
	}
	res, err := s.steer().Stop(ctx, in.TaskID, in.Hold, in.Note)
	if err != nil {
		return nil, project.StopResult{}, fmt.Errorf("%s: %w", in.TaskID, err)
	}
	return nil, res, nil
}
