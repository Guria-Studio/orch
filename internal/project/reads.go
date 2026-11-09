package project

import (
	"context"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// The rows the front ends read from a project's database, under the names
// they use them by (#587).
//
// Aliases, not copies, and on purpose. Every one of these rows goes on from
// the front end into a package that already takes the state type — usage,
// estimate, budget, knowledge, receipt, publish/snapshot — so a separate
// struct here would be converted back at each of those hand-offs, and the
// JSON the dashboard and MCP serve from some of them (TaskRuntime's PR and CI
// columns, Event's extra) would then hang on two sets of tags that must stay
// identical by hand. What #550 closes is the second DOOR to the database —
// opening it, filtering it, reading a status without Hydrate — and that is
// what the front ends lose by naming these through project: the queries are
// project's (RuntimeOf, DoneRuntime, LatestRun, Hydrate), the rows are the
// database's.
type (
	// TaskRuntime is a task's runtime row: live status, PR, CI, timings.
	TaskRuntime = state.TaskRuntime
	// Event is one entry in the event log.
	Event = state.Event
	// Spend is one dispatch's cost and token row.
	Spend = state.Spend
	// Run is one `orch run`'s row.
	Run = state.Run
	// Dispatch is an agent process a run started and has not reaped.
	Dispatch = state.Dispatch
	// Attempt is one finished attempt at a task, for the agent metrics.
	Attempt = state.Attempt
	// ProviderState is whether the operator turned a provider off.
	ProviderState = state.ProviderState
	// QueuedFinding is an agent's report about orch waiting in the review
	// queue (#536).
	QueuedFinding = state.QueuedFinding
	// Backend is the type of Project.Backend, for a helper handed an opened
	// project's database rather than the whole project.
	Backend = state.Backend
)

// A queued finding's status.
const (
	FindingQueued    = state.FindingQueued
	FindingSent      = state.FindingSent
	FindingDismissed = state.FindingDismissed
)

// RuntimeOf is the runtime rows of the given task ids — fewer when the
// database has no row for one, and none for no ids (an empty filter would
// otherwise mean every task).
func RuntimeOf(ctx context.Context, r StatusReader, ids ...string) ([]TaskRuntime, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.Tasks(ctx, state.TaskFilter{IDs: ids})
}

// DoneRuntime is the runtime rows of every task that is done.
func DoneRuntime(ctx context.Context, r StatusReader) ([]TaskRuntime, error) {
	return r.Tasks(ctx, state.TaskFilter{Statuses: []model.Status{model.StatusDone}})
}

// ParseTS reads a timestamp out of the database (state.ParseTS): the one
// parser that knows every spelling an orch.db holds, so a front end never
// writes a second one.
func ParseTS(ts string) (time.Time, bool) { return state.ParseTS(ts) }

// SummarySpendID is the synthetic task id the AI summary of run runID writes
// its spend under (`orch report summary --ai`, #461).
func SummarySpendID(runID string) string { return state.SummarySpendID(runID) }

// FirstEventBackend reports whether the project has any event of eventType
// and the backend of the earliest one — the onboarding milestones (#378). ok
// is false, with no error, for a backend that cannot answer it.
func FirstEventBackend(ctx context.Context, b Backend, eventType string) (backend string, ok bool, err error) {
	r, can := b.(interface {
		FirstEventBackend(ctx context.Context, eventType string) (string, bool, error)
	})
	if !can {
		return "", false, nil
	}
	return r.FirstEventBackend(ctx, eventType)
}
