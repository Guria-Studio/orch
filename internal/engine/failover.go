package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
)

// Provider failover (#439): a route's `failover` lists other route keys, in
// order. When its provider runs out of quota the retry goes to the next one
// at once, in the same worktree, and the provider is remembered as exhausted
// until its window resets so later tasks skip it. A route without `failover`
// never reaches any of this.

// rateLimited reports whether out says its provider is out of quota: a
// rate-limit failure, or claude's rate_limit_event saying so whatever the
// run's own error was.
func rateLimited(out Outcome) bool {
	return out.Result.QuotaExhausted || (!out.Result.Success && out.Failure == providers.FailureRateLimit)
}

// noteExhausted remembers backend as exhausted until its reset: the time the
// CLI gave, else retry.rate_limit's backoff from now.
func (s *Scheduler) noteExhausted(backend model.Backend, out Outcome) {
	if !rateLimited(out) {
		return
	}
	until := s.now().Add(retryBackoff(s.Opts.Cfg.Retry, providers.FailureRateLimit, false, s.Opts.Cfg.Retry.RateLimit))
	if out.Result.QuotaResetsAt > 0 {
		until = time.Unix(out.Result.QuotaResetsAt, 0)
	}
	if until.After(s.exhausted[backend]) {
		s.exhausted[backend] = until
	}
}

func (s *Scheduler) isExhausted(b model.Backend) bool {
	until, ok := s.exhausted[b]
	return ok && s.now().Before(until)
}

// nextFailover is the first of route's failover routes whose provider is not
// exhausted. A target with no failover of its own carries the rest of the
// list, so a chain claude -> codex -> gemini keeps going.
func (s *Scheduler) nextFailover(route model.RouteEntry) (model.RouteEntry, bool) {
	for i, key := range route.Failover {
		next, ok := s.Routes[key]
		if !ok || s.isExhausted(next.Backend) {
			continue
		}
		if len(next.Failover) == 0 {
			next.Failover = route.Failover[i+1:]
		}
		return next, true
	}
	return route, false
}

// routeFor is route, or its failover while route's provider is exhausted.
func (s *Scheduler) routeFor(route model.RouteEntry) model.RouteEntry {
	if !s.isExhausted(route.Backend) {
		return route
	}
	next, _ := s.nextFailover(route)
	return next
}

// failoverFor reports where a finished dispatch fails over to, if it does:
// only a failure that says the provider is out of quota, on a route with a
// failover left, and not a task the agent reported done anyway.
func (s *Scheduler) failoverFor(ctx context.Context, entry *InFlight, out Outcome) (model.RouteEntry, bool) {
	if out.Stopped || out.Result.Success || !rateLimited(out) || len(entry.Route.Failover) == 0 {
		return model.RouteEntry{}, false
	}
	if s.taskStatusIs(ctx, entry.Task.ID, model.StatusDone) {
		return model.RouteEntry{}, false
	}
	return s.nextFailover(entry.Route)
}

// failOver queues the retry on the failover route with no backoff, in the
// worktree the failed attempt left, so its work carries over.
func (s *Scheduler) failOver(ctx context.Context, entry *InFlight, out Outcome, next model.RouteEntry) error {
	if s.Worktree != nil && s.Opts.WorktreeMode {
		s.retryWorktree[entry.Task.ID] = entry.Dispatch.Cwd
	}
	reason := fmt.Sprintf("rate-limit failover: %s -> %s (%s)",
		entry.Route.Backend, next.Backend, truncateReason(out.Result.ErrorMessage))
	s.logger().Warn("the provider is out of quota; failing over", "task", entry.Task.ID,
		"from", string(entry.Route.Backend), "to", string(next.Backend))
	return s.queueRetry(ctx, entry, RetryDecision{
		Retry: true, Route: next, Attempt: entry.Attempt + 1, Reason: reason,
	}, out)
}
