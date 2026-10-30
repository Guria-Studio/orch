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
	size    int64
	since   time.Time // when size last changed
	stalled bool      // reported since the log last grew
	looped  bool      // reported for this attempt
}

// WatchAgents reads every running attempt's log, at most every watchInterval,
// and records agent_stalled when it has been silent for watch.stall_after_s
// and agent_looping when the agent keeps making the same tool call. Each is
// recorded once: a stall again only after the log grew, a loop once per
// attempt. Called from the run loop, which owns inFlight.
//
// ponytail: a 64 KiB tail read per attempt every 30 s, no inotify; at a few
// parallel tasks that is nothing. Watch the files if runs get much wider.
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
	for pid, e := range s.inFlight {
		w := s.watches[pid]
		if w == nil {
			w = &agentWatch{since: now}
			s.watches[pid] = w
		}
		tail, size, err := readTail(e.Dispatch.LogPath, watchTail)
		if err != nil {
			continue
		}
		if size != w.size {
			w.size, w.since, w.stalled = size, now, false
		}
		stallAfter := time.Duration(cfg.StallAfterS) * time.Second
		if silent := now.Sub(w.since); cfg.StallAfterS > 0 && !w.stalled && silent >= stallAfter {
			w.stalled = true
			s.recordWatch(ctx, e, EventAgentStalled,
				fmt.Sprintf("its log has been silent for %s", silent.Round(time.Second)),
				map[string]any{"silent_s": int(silent.Seconds())})
		}
		if call, n := repeatedCall(tail); !w.looped && n >= loopRepeats {
			w.looped = true
			s.recordWatch(ctx, e, EventAgentLooping,
				fmt.Sprintf("it made the same call %d times in its last %d: %s", n, loopWindow, call),
				map[string]any{"call": call, "repeats": n})
		}
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
