package engine

import (
	"context"
	"syscall"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
)

// StopRequests is where the loop reads the operator's stop requests (#356):
// the event log, which `orch task stop`, orch_task_stop and the dashboard
// append to from their own processes. state.Backend satisfies it.
type StopRequests interface {
	LatestEventID(ctx context.Context) (int64, error)
	EventsSince(ctx context.Context, afterID int64, limit int) ([]state.Event, error)
}

// stopRequestPage is how many events one read takes.
const stopRequestPage = 200

// startStopRequests sets the cursor at the newest event: a request written
// before this run started was answered by whoever wrote it, since no run was
// there to act on it.
func (r *Runner) startStopRequests(ctx context.Context) {
	if r.Requests == nil {
		return
	}
	id, err := r.Requests.LatestEventID(ctx)
	if err != nil {
		r.Scheduler.logger().Warn("cannot read the event log; stop requests are not watched this run", "err", err)
		r.Requests = nil
		return
	}
	r.requestCursor = id
}

// takeStopRequests acts on the stop requests written since the last tick.
// A read that fails is retried on the next tick: being a tick late is better
// than a request lost.
func (r *Runner) takeStopRequests(ctx context.Context) {
	if r.Requests == nil {
		return
	}
	s := r.Scheduler
	for {
		events, err := r.Requests.EventsSince(ctx, r.requestCursor, stopRequestPage)
		if err != nil {
			s.logger().Warn("reading stop requests failed; trying again next tick", "err", err)
			return
		}
		for _, e := range events {
			r.requestCursor = e.ID
			// The run's own answers carry `stopped`; requests carry who
			// asked.
			if e.EventType != project.EventOperatorStop || e.Extra["requested_by"] == nil {
				continue
			}
			if _, ours := s.Queue.Task(e.TaskID); !ours {
				continue
			}
			hold, _ := e.Extra["hold"].(bool)
			s.StopTask(ctx, e.TaskID, hold)
		}
		if len(events) < stopRequestPage {
			return
		}
	}
}

// StopTask ends a task's running attempt at the operator's request, and
// holds it or dispatches it again (#356). A task that is not running is held,
// or, waiting out a retry's backoff, dispatched now.
func (s *Scheduler) StopTask(ctx context.Context, taskID string, hold bool) {
	for pid, entry := range s.inFlight {
		if entry.Task.ID != taskID {
			continue
		}
		if _, asked := s.stopping[pid]; !asked {
			s.logger().Info("the operator stopped the task; ending its attempt",
				"task", taskID, "pid", pid, "hold", hold)
			entry.Spawned.terminate()
		}
		// A second request before the child exits decides hold.
		s.stopping[pid] = hold
		return
	}

	if st, _ := s.Queue.Status(taskID); st == model.StatusDone {
		s.answerStop(ctx, taskID, hold, "already_done")
		return
	}
	waiting := false
	keep := s.retryQueue[:0]
	for _, item := range s.retryQueue {
		if item.Task.ID == taskID {
			waiting = true
			if hold {
				continue
			}
			item.EarliestAt = s.now()
		}
		keep = append(keep, item)
	}
	s.retryQueue = keep

	answer := "not_running"
	switch {
	case hold:
		// ponytail: a task waiting on CI is held here and the poller can
		// still finish it when its CI passes; a CI-aware hold if that bites.
		s.holdTask(ctx, taskID)
		answer = "held"
	case waiting:
		answer = "retry_now"
	}
	s.answerStop(ctx, taskID, hold, answer)
}

// answerStop records the run's answer to a request for a task that was not
// running: held, retry_now, not_running or already_done.
func (s *Scheduler) answerStop(ctx context.Context, taskID string, hold bool, answer string) {
	s.logger().Info("the operator stopped a task that is not running", "task", taskID, "hold", hold, "answer", answer)
	if s.Backend == nil {
		return
	}
	if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, project.EventOperatorStop, taskID, "",
		map[string]any{"stopped": answer, "hold": hold}); err != nil {
		s.logger().Error("recording the answer to a stop request failed", "task", taskID, "err", err)
	}
}

// finishStopped ends an attempt the operator stopped: held, or back in line
// at once with the same attempt number — the operator's stop does not count
// against max_attempts. The outcome event RecordFinish wrote is the answer.
func (s *Scheduler) finishStopped(ctx context.Context, entry *InFlight, hold bool) error {
	id := entry.Task.ID
	if hold {
		s.holdTask(ctx, id)
		return nil
	}
	s.logger().Info("dispatching the stopped task again", "task", id, "attempt", entry.Attempt)
	if err := s.Queue.MarkTodo(id); err != nil {
		s.logger().Error("resetting to todo failed", "task", id, "err", err)
	}
	if err := s.transition(ctx, id, model.StatusTodo, "stopped by the operator; dispatching it again"); err != nil {
		s.logger().Error("recording todo failed", "task", id, "err", err)
	}
	s.retryQueue = append(s.retryQueue, RetryItem{
		Task: entry.Task, Route: entry.Route, Attempt: entry.Attempt, EarliestAt: s.now(),
	})
	return nil
}

// holdTask blocks a task the operator held. Unblocking it is the existing
// `orch task set --status todo`.
func (s *Scheduler) holdTask(ctx context.Context, taskID string) {
	if err := s.Queue.MarkBlocked(taskID); err != nil {
		s.logger().Error("mark blocked failed", "task", taskID, "err", err)
	}
	if s.Backend == nil {
		return
	}
	if err := transitionThroughTodo(ctx, s.Backend, taskID, model.StatusBlocked, project.HeldReason); err != nil {
		s.logger().Error("recording the hold failed", "task", taskID, "err", err)
	}
}

// terminate asks the child's process group to stop, and kills it if it has
// not after TermToKillGrace — the timeout's escalation, for one child.
func (s *Spawned) terminate() {
	s.signalGroup(syscall.SIGTERM)
	go func() {
		t := time.NewTimer(TermToKillGrace)
		defer t.Stop()
		select {
		case <-t.C:
			s.signalGroup(syscall.SIGKILL)
		case <-s.watchStop:
		}
	}()
}
