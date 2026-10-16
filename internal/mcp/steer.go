package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hectorcanaimero/orch/internal/project"
)

// The operator's steering tools (#356). They go through project.Steer, the
// same path as `orch task note`.

func (s *server) steer() project.Steer {
	return project.Steer{Backend: s.opts.Backend}
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
