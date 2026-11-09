package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
	"github.com/Guria-Studio/orch/internal/state"
)

// readSessionFacts adds to a finished run's result what its CLI kept outside
// the log orch captured. Today that is codex's session rollout (#577, #580):
// the model and effort its last turn_context names, and — the
// thread id the exec log names leads to
// `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*-<id>.jsonl` — whose token_count
// events carry the plan's real 5-hour and weekly windows. They land on
// Result.Quota, which outcomeEvent keeps as extra.quota — the same road
// claude's rate_limit_event takes to /api/quota and the budget gate.
//
// Nothing to read is not an error: no thread id, no rollout, a backend that
// keeps none. The error is for a rollout that is there and cannot be read.
func readSessionFacts(d Dispatch, res *providers.Result) error {
	if d.Req.Route.Backend != model.BackendCodex || res.SessionID == "" {
		return nil
	}
	dir := providers.CodexSessionsDir(dispatchGetenv(d.Env))
	pattern := providers.CodexRolloutPattern(dir, res.SessionID)
	if dir == "" || pattern == "" {
		return nil
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("find codex rollout for thread %s: %w", res.SessionID, err)
	}
	if len(matches) == 0 {
		return nil
	}
	// One thread, one file; were there two, the later name is the later day.
	sort.Strings(matches)
	path := matches[len(matches)-1]
	b, err := os.ReadFile(path) // #nosec G304 -- a codex rollout under the operator's own CODEX_HOME, found by a validated thread id
	if err != nil {
		return fmt.Errorf("read codex rollout %s: %w", path, err)
	}
	s := providers.ParseCodexRollout(b)
	if len(res.Quota) == 0 {
		res.Quota = s.Quota
	}
	// What codex ran, not what it was asked (#580); "" stays unknown.
	if res.EffectiveModel == "" {
		res.EffectiveModel = s.Model
	}
	if res.EffectiveEffort == "" {
		res.EffectiveEffort = s.Effort
	}
	return nil
}

// dispatchGetenv looks a variable up in the child's environment, which is
// what the CLI saw; the last assignment wins, as for the child itself.
func dispatchGetenv(env []string) func(string) string {
	return func(key string) string {
		val := ""
		for _, kv := range env {
			if k, v, ok := strings.Cut(kv, "="); ok && k == key {
				val = v
			}
		}
		return val
	}
}

// routeMismatch names what ran differently from what the route asked for:
// "model", "effort", "service_tier", in that order. A field counts only when
// both sides are known — the route set it and the CLI reported it — so an
// unknown never reads as a difference (#580).
func routeMismatch(route model.RouteEntry, res providers.Result) []string {
	f := routeFields(route, res)
	return state.Attempt{
		Model: f[0].declared, Effort: f[1].declared, ServiceTier: f[2].declared,
		EffectiveModel: f[0].effective, EffectiveEffort: f[1].effective, EffectiveServiceTier: f[2].effective,
	}.Mismatch()
}

// routeField is one field of a route beside what the CLI ran.
type routeField struct{ name, label, declared, effective string }

func routeFields(route model.RouteEntry, res providers.Result) []routeField {
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return []routeField{
		{"model", "model", route.CLIModel, res.EffectiveModel},
		{"effort", "effort", deref(route.Effort), res.EffectiveEffort},
		{"service_tier", "service tier", deref(route.ServiceTier), res.EffectiveServiceTier},
	}
}

// mismatchEvent is the warning for an attempt whose CLI ran something other
// than its route asked for, or nil when nothing known differs.
func mismatchEvent(runID string, d Dispatch, o Outcome, ts string, attempt int) *state.Event {
	fields := routeMismatch(d.Req.Route, o.Result)
	if len(fields) == 0 {
		return nil
	}
	declared, effective := map[string]any{}, map[string]any{}
	var detail []string
	for _, f := range routeFields(d.Req.Route, o.Result) {
		declared[f.name], effective[f.name] = f.declared, f.effective
		if slices.Contains(fields, f.name) {
			detail = append(detail, fmt.Sprintf("%s %s asked, %s ran", f.label, f.declared, f.effective))
		}
	}
	return &state.Event{
		RunID: runID, TaskID: d.Req.TaskID, Backend: string(d.Req.Route.Backend), TS: ts,
		EventType: EventModelMismatch,
		Extra: map[string]any{
			"attempt": attempt, "fields": fields, "declared": declared, "effective": effective,
			"detail": strings.Join(detail, "; "),
		},
	}
}
