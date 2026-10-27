package engine

import (
	"context"
	"strings"

	"github.com/hectorcanaimero/orch/internal/guard"
)

// guardOutcome runs the pre-PR guard (#484) on a clean exit's diff, from the
// dispatch's supervising goroutine like verifyOutcome, and after it: work the
// verify sends back is guarded on the attempt that passes.
func (s *Scheduler) guardOutcome(d Dispatch, out *Outcome) {
	g := s.Opts.Cfg.Guard
	on := func(rule string) bool { m := g.Mode(rule); return m == "block" || m == "warn" }
	if !out.Result.Success || out.VerifyFailure != "" || s.Worktree == nil || !s.Opts.WorktreeMode ||
		!on(guard.RuleSecrets) && !on(guard.RuleDebug) {
		return
	}
	ctx := context.Background()
	id := d.Req.TaskID
	s.commitWork(ctx, id, d.Cwd)
	diff, err := s.Worktree.Diff(ctx, id)
	if err != nil {
		s.logger().Warn("reading the task's diff failed; the pre-PR guard did not run", "task", id, "err", err)
		return
	}
	var blocked []string
	for _, f := range guard.Scan(diff) {
		switch g.Mode(f.Rule) {
		case "block":
			blocked = append(blocked, f.String())
		case "warn":
			out.GuardWarnings = append(out.GuardWarnings, f.String())
		}
	}
	if len(out.GuardWarnings) > 0 {
		s.logger().Warn("the pre-PR guard found debug leftovers or secrets it only warns about",
			"task", id, "findings", strings.Join(out.GuardWarnings, "; "))
	}
	if len(blocked) > 0 {
		out.GuardBlock = "the pre-PR guard kept this work from being pushed (guard.* in config.yaml): " +
			strings.Join(blocked, "; ") + "; the work is committed in the task's local branch, so take them out of its history before unblocking it"
	}
}

// guardSection names, for the PR body, what the guard warned about.
func guardSection(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	return "\n\n**The pre-PR guard warns** (debug leftovers or secrets; guard.* in config.yaml):\n- " +
		strings.Join(warnings, "\n- ")
}
