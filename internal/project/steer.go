package project

import (
	"context"
	"errors"
	"strings"

	"github.com/hectorcanaimero/orch/internal/state"
)

// OperatorAuthor is the author of everything the operator writes through
// Steer.
const OperatorAuthor = "operator"

// Steer is how the operator steers a task (#356): a note for its agent. The
// CLI and the MCP server both go through it.
type Steer struct {
	Backend state.Backend
}

// Note records a note for the task's agent. It reaches the prompt of the
// task's next attempt, not the one running now.
func (s Steer) Note(ctx context.Context, taskID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("the note is empty")
	}
	return s.Backend.AddNote(ctx, taskID, state.Note{
		Author: OperatorAuthor, Body: body, Kind: state.NoteKindOperator,
	})
}
