package knowledge

import (
	"context"
	"fmt"
	"time"

	"github.com/Guria-Studio/orch/internal/state"
)

// The events the project knowledge is recorded as (#453).
const (
	// EventEdited is an operator's add, edit or removal of a lesson; extra
	// names the action, the lesson and who did it.
	EventEdited = "knowledge_edited"
	// EventUsed is a dispatch whose prompt carried lessons; extra lists them.
	EventUsed = "knowledge_used"
)

// EventAppender is the slice of state.Backend an Editor records with.
type EventAppender interface {
	AppendEvent(ctx context.Context, runID string, e state.Event) error
}

// Editor is the one way the operator changes the lessons, from `orch
// knowledge add|rm` and from the dashboard alike: the file is written by Add,
// Update or Remove, then the change is recorded as an event.
type Editor struct {
	Root string
	// Events, when set, gets one EventEdited per change.
	Events EventAppender
	// Source names who edited, in the event: cli, dashboard.
	Source string
}

// Add writes a new lesson.
func (e Editor) Add(ctx context.Context, l Lesson, now time.Time) (Lesson, error) {
	l, err := Add(e.Root, l, now)
	if err != nil {
		return l, err
	}
	return l, e.record(ctx, "added", l.ID, now)
}

// Update rewrites lesson id.
func (e Editor) Update(ctx context.Context, id string, l Lesson, now time.Time) (Lesson, error) {
	l, err := Update(e.Root, id, l)
	if err != nil {
		return l, err
	}
	return l, e.record(ctx, "edited", id, now)
}

// Remove deletes lesson id.
func (e Editor) Remove(ctx context.Context, id string, now time.Time) error {
	if err := Remove(e.Root, id); err != nil {
		return err
	}
	return e.record(ctx, "removed", id, now)
}

func (e Editor) record(ctx context.Context, action, id string, now time.Time) error {
	if e.Events == nil {
		return nil
	}
	err := e.Events.AppendEvent(ctx, "", state.Event{
		EventType: EventEdited, TS: now.UTC().Format(time.RFC3339Nano),
		Extra: map[string]any{"action": action, "lesson": id, "source": e.Source},
	})
	if err != nil {
		return fmt.Errorf("the lesson is saved, but recording the edit failed: %w", err)
	}
	return nil
}

// Retries is a finished task's dispatches past the first plus its CI
// retries; 0 is a first-try success. `orch knowledge stats` and Usage count
// with it.
func Retries(t state.TaskRuntime) int {
	return max(t.Attempts-1, 0) + t.CIAttempts
}

// Use is what a lesson did: how many prompts carried it, how many of the
// tasks they were for are done, and how many of those were a first-try
// success.
type Use struct {
	Prompts  int `json:"prompts"`
	Tasks    int `json:"tasks"`
	FirstTry int `json:"first_try"`
}

// Usage reads the EventUsed events against the done tasks, by lesson id.
func Usage(events []state.Event, done []state.TaskRuntime) map[string]Use {
	doneByID := map[string]state.TaskRuntime{}
	for _, t := range done {
		doneByID[t.ID] = t
	}
	out := map[string]Use{}
	seen := map[[2]string]bool{}
	for _, e := range events {
		if e.EventType != EventUsed {
			continue
		}
		ids, _ := e.Extra["lessons"].([]any)
		for _, raw := range ids {
			id, _ := raw.(string)
			u := out[id]
			u.Prompts++
			if t, ok := doneByID[e.TaskID]; ok && !seen[[2]string{id, e.TaskID}] {
				seen[[2]string{id, e.TaskID}] = true
				u.Tasks++
				if Retries(t) == 0 {
					u.FirstTry++
				}
			}
			out[id] = u
		}
	}
	return out
}
