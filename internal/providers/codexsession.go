package providers

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// codex's session rollout (#577, #580).
//
// `codex exec --json` streams the run's events to stdout and nothing else
// about the account: no rate limits, no model. Both are in the session
// rollout codex writes beside it, `$CODEX_HOME/sessions/YYYY/MM/DD/
// rollout-<local time>-<thread id>.jsonl` (CODEX_HOME defaults to ~/.codex),
// where every `event_msg` of type `token_count` carries the plan's
// `rate_limits`. The thread id is the one thread.started names in the exec
// log (Result.SessionID).
//
// This file stays pure like the rest of the package: it says where the file
// is and parses its bytes. Reading it is the engine's job.

// CodexSession is what codex wrote about one run in its session rollout.
type CodexSession struct {
	// Quota is the newest token_count's rate_limits: the plan's 5-hour
	// (primary) and weekly (secondary) windows, which count every use of the
	// account, not only orch's. nil when the rollout reported none.
	Quota []QuotaWindow
	// Model and Effort are what the last turn ran with, from its
	// turn_context: the model codex resolved and the reasoning effort,
	// "" when codex recorded none (no effort was set). exec --json names
	// neither (#580).
	Model, Effort string
}

// ParseCodexRollout reads a rollout's JSONL. Lines that do not parse are
// skipped, as in the exec log: codex may still be appending to it.
func ParseCodexRollout(b []byte) CodexSession {
	var s CodexSession
	for _, ev := range jsonlEvents(b) {
		payload, ok := asObject(ev.obj["payload"])
		if !ok {
			continue
		}
		if ev.eventType() == "turn_context" {
			s.Model, s.Effort = asString(payload["model"]), asString(payload["effort"])
		}
		if ev.eventType() == "event_msg" && asString(payload["type"]) == "token_count" {
			if q := codexQuota(payload["rate_limits"]); q != nil {
				s.Quota = q
			}
		}
	}
	return s
}

// codexWindowNames maps a rate-limit window's length to the name claude uses
// for the same window, so the dashboard labels both alike.
var codexWindowNames = map[int]string{300: "five_hour", 10080: "seven_day"}

// codexQuota reads one rate_limits object: primary then secondary, each with
// used_percent (0-100), window_minutes and resets_at (Unix seconds). nil when
// neither is there.
func codexQuota(v any) []QuotaWindow {
	limits, ok := asObject(v)
	if !ok {
		return nil
	}
	var out []QuotaWindow
	for _, key := range []string{"primary", "secondary"} {
		w, ok := asObject(limits[key])
		if !ok {
			continue
		}
		minutes := toInt(w["window_minutes"])
		name, known := codexWindowNames[minutes]
		if !known {
			name = fmt.Sprintf("%dm", minutes)
		}
		out = append(out, QuotaWindow{
			Window:      name,
			Utilization: toFloat(w["used_percent"]) / 100,
			ResetsAt:    int64(toFloat(w["resets_at"])),
		})
	}
	return out
}

// codexThreadID is the shape of a thread id codex prints: a UUID, but any
// run of letters, digits and dashes is accepted. Nothing else is, because
// the id is spliced into a glob.
var codexThreadID = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// CodexRolloutPattern is the glob that finds a thread's rollout under
// sessionsDir, whatever day codex filed it under (the directory is the local
// date the session started, which need not be the date orch saw). "" for an
// id that is not a plain id.
func CodexRolloutPattern(sessionsDir, threadID string) string {
	if !codexThreadID.MatchString(threadID) {
		return ""
	}
	return filepath.Join(sessionsDir, "*", "*", "*", "rollout-*-"+threadID+".jsonl")
}

// CodexSessionsDir is where codex keeps its rollouts: $CODEX_HOME/sessions,
// else ~/.codex/sessions. "" when neither variable is set.
func CodexSessionsDir(getenv func(string) string) string {
	if h := getenv("CODEX_HOME"); h != "" {
		return filepath.Join(h, "sessions")
	}
	if h := getenv("HOME"); h != "" {
		return filepath.Join(h, ".codex", "sessions")
	}
	return ""
}

// codexDroppedTier reports whether codex said it would omit the configured
// service tier, after which the run is on the default tier (#416, #580).
func codexDroppedTier(events []event) bool {
	for _, ev := range events {
		msg := pyStr(ev.obj["message"])
		if item, ok := asObject(ev.obj["item"]); ok {
			msg = pyStr(item["message"])
		}
		if strings.Contains(msg, codexTierNotAdvertised) {
			return true
		}
	}
	return false
}

// codexSessionID is the thread id thread.started names; "" when the log has
// no such event.
func codexSessionID(events []event) string {
	for _, ev := range events {
		if ev.eventType() == "thread.started" {
			return asString(ev.obj["thread_id"])
		}
	}
	return ""
}
