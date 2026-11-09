package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Guria-Studio/orch/internal/contract"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
)

// FallbackTimeout is what a task with no usable estimate gets. Python's
// _timeout_for gives 60s to a task whose estimate_hours is 0 (a dry seed, or a
// typo) so the reap loop can still catch a child that hangs without emitting.
const FallbackTimeout = 60 * time.Second

// TimeoutFor computes a dispatch's timeout: estimateHours × multiplier, in
// hours. Port of orchestrator/orch.py::_timeout_for.
//
// A non-positive result — no estimate, or a multiplier someone zeroed in
// config — falls back to FallbackTimeout rather than to "no timeout at all",
// which would let one hung child hold a slot forever.
func TimeoutFor(estimateHours, multiplier float64) time.Duration {
	if multiplier <= 0 {
		// Python's `float(cfg.get(...) or 1.5)` only catches a missing or
		// falsy value, so a config that says 0 gets 0 there and hits the
		// same `seconds <= 0` fallback below. Reaching the default earlier
		// gives an estimate-bearing task its real timeout instead of 60s.
		multiplier = 1.5
	}
	seconds := estimateHours * multiplier * 3600
	if seconds <= 0 {
		return FallbackTimeout
	}
	return time.Duration(seconds * float64(time.Second))
}

// Dispatch is one task's run: which provider, with what, and for how long.
type Dispatch = contract.Dispatch

// Outcome is everything one completed dispatch produced.
type Outcome = contract.Outcome

// Run spawns a dispatch, supervises it, and parses what it produced.
//
// Recording the outcome in state is the caller's job: Run stays free of a
// state.Backend so it can be exercised without a database, and so the single
// writer stays single (checklist rule 17).
//
// The ordering matches the Python reap loop: a timeout's reason wins over
// anything the CLI printed, because a killed child's output is whatever
// happened to be flushed before the signal — and TimeoutReason is what
// providers.Classify keys FailureTimeout on.
func Run(ctx context.Context, d Dispatch) (Outcome, error) {
	if d.Provider == nil {
		return Outcome{}, fmt.Errorf("dispatch: no provider for task %q", d.Req.TaskID)
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = FallbackTimeout
	}

	sp, err := Spawn(ctx, SpawnRequest{
		Provider:   d.Provider,
		Req:        d.Req,
		PromptPath: d.PromptPath,
		LogPath:    d.LogPath,
		Cwd:        d.Cwd,
		Env:        d.Env,
	})
	if err != nil {
		return Outcome{}, err
	}

	exitCode, reason, timedOut := sp.Wait(timeout)
	duration := time.Since(sp.StartedAt)

	out := Outcome{
		PID:       sp.PID,
		StartedAt: sp.StartedAt,
		Duration:  duration,
		TimedOut:  timedOut,
		LogPath:   d.LogPath,
	}

	// Parse with the real provider even under the fake hook: the parser is
	// the thing being exercised, and the fake replaces only the argv.
	out.Result = d.Provider.Parse(exitCode, []byte(ReadLog(d.LogPath)))
	if timedOut {
		// Python: `result.success = False; result.error_message = err_msg`.
		out.Result.Success = false
		out.Result.ErrorMessage = reason
	}

	if !out.Result.Success {
		// The reap loop forces TIMEOUT rather than asking Classify, because
		// a killed CLI's output can say anything while the semantics are
		// fixed: no retry. Classify would agree via TimeoutReason; forcing
		// it means a future edit to the marker table cannot change it.
		if timedOut {
			out.Failure = providers.FailureTimeout
		} else {
			out.Failure = providers.Classify(out.Result)
		}
	}

	return out, nil
}

// LogPathFor is where a task's captured output goes: `<stateDir>/logs/<id>.log`,
// the same layout Python's _ensure_logs_dir builds.
func LogPathFor(stateDir, taskID string) string {
	return filepath.Join(stateDir, "logs", taskID+".log")
}

// defaultUnestimatedTimeout is a task's timeout when it has no estimate and
// dispatch.unestimated_timeout_m is not set (#529).
const defaultUnestimatedTimeout = time.Hour

// timeoutFor is a task's dispatch timeout. An estimate drives it through
// TimeoutFor; a task with none — every one `orch sync issues` creates — gets
// dispatch.unestimated_timeout_m rather than TimeoutFor's 60-second floor,
// which killed it in its first minute and, a timeout not being retried,
// blocked it there (#529).
func (s *Scheduler) timeoutFor(task model.Task) time.Duration {
	d := defaultUnestimatedTimeout
	switch m := s.Opts.Cfg.Dispatch.UnestimatedTimeoutM; {
	case task.EstimateHours > 0:
		d = TimeoutFor(task.EstimateHours, s.Opts.TimeoutMultiplier)
	case m > 0:
		d = time.Duration(m) * time.Minute
	}
	if scale := s.timeoutScale[task.ID]; scale > 0 {
		d = time.Duration(float64(d) * scale)
	}
	return d
}

// logActiveAt reports whether a log was written in the last stretch before
// deadline — a quarter of the timeout, at most two minutes. Its mtime is the
// cheapest witness that the agent was still producing output when it was
// killed.
func logActiveAt(logPath string, deadline time.Time, timeout time.Duration) bool {
	fi, err := os.Stat(logPath)
	if err != nil {
		return false
	}
	return !fi.ModTime().Before(deadline.Add(-min(timeout/4, 2*time.Minute)))
}
