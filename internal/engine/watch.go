package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/Guria-Studio/orch/internal/providers"
)

// Events the agent watch records about a running attempt (#487). Neither
// stops it: they say what the operator would otherwise learn at the timeout.
const (
	EventAgentStalled = "agent_stalled"
	EventAgentLooping = "agent_looping"
	// EventAgentResumed follows an agent_stalled when the log grows again, so
	// the last event says whether the agent is still stuck (#533).
	EventAgentResumed = "agent_resumed"
)

const (
	// watchInterval is how often the run loop reads the running logs.
	watchInterval = 30 * time.Second
	// watchTail is how much of a log one read looks at.
	watchTail = 64 << 10
	// loopWindow and loopRepeats: an attempt is looping when one tool call
	// (tool and target) fills loopRepeats of its last loopWindow calls.
	loopWindow, loopRepeats = 20, 5
)

// agentWatch is what the watch remembers about one running attempt.
type agentWatch struct {
	size  int64
	since time.Time // when size last changed
	// lastActive is when the agent last showed work: its log grew, or its
	// processes or files moved (#524).
	lastActive time.Time
	// base is the liveness sample when the log went quiet, the start of the
	// evidence; prev the last one, which the next is compared with.
	base, prev *liveSample
	stalled    bool      // reported since the agent last showed work
	waitingAt  time.Time // when agent_waiting was last recorded; zero when not waiting
	looped     bool      // reported for this attempt
}

// WatchAgents reads every running attempt's log, at most every watchInterval,
// and records agent_looping when the agent keeps making the same tool call
// (once per attempt) and, when the log has been silent for
// watch.stall_after_s, one of:
//
//   - agent_waiting: with watch.liveness on, its process group still uses CPU
//     or moves data, or files grow in its worktree or watch.data_dirs. The
//     event carries the evidence ("downloading data: +163 MB in 1h0m0s") and
//     is refreshed every stall_after_s while the wait lasts.
//   - agent_stalled: no sign of life for stall_after_s. Recorded once, again
//     only after the agent showed work and then stopped.
//
// agent_resumed closes either when the log grows again. Called from the run
// loop, which owns inFlight.
//
// ponytail: a 64 KiB tail read per attempt every 30 s, and for a silent one a
// /proc scan and a walk of its worktree; at a few parallel tasks that is
// nothing. Watch the files if runs get much wider.
func (s *Scheduler) WatchAgents(ctx context.Context, now time.Time) {
	cfg := s.Opts.Cfg.Watch
	if !cfg.Enabled || (!s.lastWatch.IsZero() && now.Sub(s.lastWatch) < watchInterval) {
		return
	}
	s.lastWatch = now
	if s.watches == nil {
		s.watches = map[int]*agentWatch{}
	}
	for pid := range s.watches {
		if _, ok := s.inFlight[pid]; !ok {
			delete(s.watches, pid)
		}
	}
	stallAfter := time.Duration(cfg.StallAfterS) * time.Second
	for pid, e := range s.inFlight {
		w := s.watches[pid]
		if w == nil {
			w = &agentWatch{since: now, lastActive: now}
			s.watches[pid] = w
		}
		tail, size, err := readTail(e.Dispatch.LogPath, watchTail)
		if err != nil {
			continue
		}
		if size != w.size {
			if w.stalled || !w.waitingAt.IsZero() {
				s.recordResumed(ctx, e, now.Sub(w.since))
			}
			w.size, w.since, w.lastActive = size, now, now
			w.stalled, w.waitingAt, w.base, w.prev = false, time.Time{}, nil, nil
		} else if cfg.Liveness && cfg.StallAfterS > 0 {
			cur := s.sampleLiveness(pid, e, now)
			if w.base == nil {
				w.base = &cur
			} else if active(*w.prev, cur, cfg.MinCPUPct) {
				w.lastActive = now
			}
			w.prev = &cur
		}
		if silent := now.Sub(w.since); cfg.StallAfterS > 0 && silent >= stallAfter {
			s.judgeSilence(ctx, e, w, now, silent, stallAfter)
		}
		if call, n := repeatedCall(tail); !w.looped && n >= loopRepeats {
			w.looped = true
			s.recordWatch(ctx, e, EventAgentLooping,
				fmt.Sprintf("it made the same call %d times in its last %d: %s", n, loopWindow, call),
				map[string]any{"call": call, "repeats": n})
		}
	}
}

// judgeSilence decides what a log silent for stallAfter or more means: the
// agent is waiting on work that shows (#524), or it is stuck.
func (s *Scheduler) judgeSilence(ctx context.Context, e *InFlight, w *agentWatch, now time.Time, silent, stallAfter time.Duration) {
	if idle := now.Sub(w.lastActive); idle >= stallAfter {
		if w.stalled {
			return
		}
		w.stalled, w.waitingAt = true, time.Time{}
		what := fmt.Sprintf("its log has been silent for %s", silent.Round(time.Second))
		if s.Opts.Cfg.Watch.Liveness {
			what += fmt.Sprintf(", and its processes and files for %s", idle.Round(time.Second))
		}
		s.recordWatch(ctx, e, EventAgentStalled, what, map[string]any{"silent_s": int(silent.Seconds())})
		return
	}
	if !w.waitingAt.IsZero() && now.Sub(w.waitingAt) < stallAfter {
		return
	}
	w.stalled, w.waitingAt = false, now
	ev := evidence(*w.base, *w.prev)
	s.logger().Info("the agent is silent but working: "+ev.String(), "task", e.Task.ID)
	if s.Backend == nil {
		return
	}
	extra := ev.extra()
	extra["attempt"], extra["reason"], extra["silent_s"] = e.Attempt, ev.String(), int(silent.Seconds())
	if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, EventAgentWaiting, e.Task.ID, string(e.Route.Backend), extra); err != nil {
		s.logger().Error("recording the agent watch event failed", "task", e.Task.ID, "err", err)
	}
}

func (s *Scheduler) recordWatch(ctx context.Context, e *InFlight, event, what string, extra map[string]any) {
	s.logger().Warn("the agent may be stuck: "+what+"; `orch task stop --id ID` ends the attempt", "task", e.Task.ID)
	if s.Backend == nil {
		return
	}
	extra["attempt"], extra["reason"] = e.Attempt, what
	if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, event, e.Task.ID, string(e.Route.Backend), extra); err != nil {
		s.logger().Error("recording the agent watch event failed", "task", e.Task.ID, "err", err)
	}
}

// repeatedCall is the tool call that comes up most often among the last
// loopWindow calls of the log's last attempt, and how often.
func repeatedCall(tail []byte) (string, int) {
	var calls []string
	for _, line := range strings.Split(string(tail), "\n") {
		for _, st := range providers.ActivityOf([]byte(line)) {
			switch st.Kind {
			case providers.StepStart:
				calls = calls[:0]
			case providers.StepTool:
				calls = append(calls, st.Tool+" "+st.Detail)
			}
		}
	}
	if len(calls) > loopWindow {
		calls = calls[len(calls)-loopWindow:]
	}
	counts := map[string]int{}
	best, most := "", 0
	for _, c := range calls {
		if counts[c]++; counts[c] > most {
			best, most = c, counts[c]
		}
	}
	return best, most
}

// readTail is the last n bytes of path and its size. A log not written yet
// is empty, not an error.
func readTail(path string, n int64) ([]byte, int64, error) {
	f, err := os.Open(path) // #nosec G304 -- the engine's own log path
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	off := max(info.Size()-n, 0)
	buf := make([]byte, info.Size()-off)
	_, err = f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, err
	}
	return buf, info.Size(), nil
}

// recordResumed notes that a stalled agent's log grew again. It is not a
// warning: the stall it closes was the warning.
func (s *Scheduler) recordResumed(ctx context.Context, e *InFlight, silent time.Duration) {
	s.logger().Info("the agent is writing again after a silence", "task", e.Task.ID, "silent", silent.Round(time.Second))
	if s.Backend == nil {
		return
	}
	if err := s.Backend.AppendEngineEvent(ctx, s.Opts.RunID, EventAgentResumed, e.Task.ID, string(e.Route.Backend),
		map[string]any{"attempt": e.Attempt, "silent_s": int(silent.Seconds())}); err != nil {
		s.logger().Error("recording the agent watch event failed", "task", e.Task.ID, "err", err)
	}
}
