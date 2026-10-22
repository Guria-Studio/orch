package mcp

import (
	"context"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hectorcanaimero/orch/internal/state"
)

// ---- orch_run_status -------------------------------------------------------

// runStatusIn takes no arguments: the tool answers for the latest run.
type runStatusIn struct{}

// runStatusRun is the header of the latest run, the same fields `orch status`
// reads from the runs table.
type runStatusRun struct {
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	Mode      string `json:"mode"`
	StartedAt string `json:"started_at"`
	UpdatedAt string `json:"updated_at"`
}

// runningTask is one dispatch of the run that is still in flight: the task,
// the provider it went to, and which attempt it is on.
type runningTask struct {
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	Backend   string `json:"backend"`
	Attempt   int    `json:"attempt"`
	StartedAt string `json:"started_at"`
}

type runStatusOut struct {
	// Run is nil when the project has never recorded a run.
	Run *runStatusRun `json:"run"`
	// Working is the latest run's dispatches still in flight, in task order.
	Working []runningTask `json:"working"`
	// Counts is every task by status, zeroes included, so a caller can
	// iterate it without a nil check.
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
}

func (s *server) runStatus(ctx context.Context, _ *mcpsdk.CallToolRequest, _ runStatusIn) (*mcpsdk.CallToolResult, runStatusOut, error) {
	tasks, _, err := s.load(ctx)
	if err != nil {
		return nil, runStatusOut{}, err
	}
	out := runStatusOut{Working: []runningTask{}, Counts: map[string]int{}, Total: len(tasks)}
	for _, t := range tasks {
		out.Counts[string(t.Status)]++
	}

	run, err := s.opts.Backend.LatestRun(ctx)
	if errors.Is(err, state.ErrNoRuns) {
		return nil, out, nil
	}
	if err != nil {
		return nil, runStatusOut{}, fmt.Errorf("read the latest run: %w", err)
	}
	out.Run = &runStatusRun{
		RunID: run.RunID, Status: run.Status, Mode: run.Mode,
		StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt,
	}

	// In-flight rows span every run; a dispatch left over from an earlier
	// run is the reconciler's business, not this run's.
	flight, err := s.opts.Backend.InFlightDispatches(ctx)
	if err != nil {
		return nil, runStatusOut{}, fmt.Errorf("read in-flight dispatches: %w", err)
	}
	for _, d := range flight {
		if d.RunID != run.RunID {
			continue
		}
		title := ""
		if t, ok := findTask(tasks, d.TaskID); ok {
			title = t.Title
		}
		out.Working = append(out.Working, runningTask{
			TaskID: d.TaskID, Title: title, Backend: d.Backend,
			Attempt: d.Attempt, StartedAt: d.StartedAt,
		})
	}
	return nil, out, nil
}
