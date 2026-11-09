package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
)

// readSessionFacts adds to a finished run's result what its CLI kept outside
// the log orch captured. Today that is codex's session rollout (#577): the
// thread id the exec log names leads to
// `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*-<id>.jsonl`, whose token_count
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
