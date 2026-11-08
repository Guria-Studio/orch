package scaffold

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
	"github.com/Guria-Studio/orch/internal/router"
)

// ModelLists is, per backend, the models its CLI said it runs now (#397). A
// backend missing from it is unknown — no model list, or the CLI could not
// give one — and keeps every packaged route.
type ModelLists map[model.Backend][]string

// discoverTimeout bounds each CLI's list; a variable so a test can shorten it.
var discoverTimeout = 5 * time.Second

// DiscoverModels asks each installed CLI that has a model list (opencode,
// agy, codex; see providers.ModelLister) for it, all at once, each cut off at
// discoverTimeout. A CLI that fails, hangs or prints nothing readable is left
// out of lists and named in failed, by router.BackendPreference: init goes on
// with its packaged routes and says so.
func DiscoverModels(ctx context.Context, installed func(model.Backend) bool) (lists ModelLists, failed []model.Backend) {
	got := make([][]string, len(router.BackendPreference))
	asked := make([]bool, len(router.BackendPreference))
	var wg sync.WaitGroup
	for i, b := range router.BackendPreference {
		p, err := providers.Get(b)
		if err != nil || !installed(b) {
			continue
		}
		l, ok := p.(providers.ModelLister)
		if !ok {
			continue
		}
		asked[i] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = listModels(ctx, l)
		}()
	}
	wg.Wait()
	for i, b := range router.BackendPreference {
		switch {
		case !asked[i]:
		case len(got[i]) == 0:
			failed = append(failed, b)
		default:
			if lists == nil {
				lists = ModelLists{}
			}
			lists[b] = got[i]
		}
	}
	return lists, failed
}

// listModels runs one CLI's model list; nil on any failure.
func listModels(ctx context.Context, l providers.ModelLister) []string {
	ctx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()
	argv := l.ModelsArgv()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- a fixed argv from the adapter
	// A child that leaves a grandchild holding stdout must not hold init.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return l.ParseModels(out)
}

// Available is r without the routes whose CLI listed its models but not the
// route's cli_model; dropped names those, sorted. Tier, effort and fallbacks
// still come from r: a list only says what is there.
func (l ModelLists) Available(r router.Router) (kept router.Router, dropped []string) {
	kept = router.Router{}
	for k, e := range r {
		if listed, ok := l[e.Backend]; ok && !slices.Contains(listed, e.CLIModel) {
			dropped = append(dropped, k)
			continue
		}
		kept[k] = e
	}
	// An escalation to a dropped route goes with it: router.Load refuses an
	// escalation_model the file does not route.
	for k, e := range kept {
		if e.EscalationModel != nil && slices.Contains(dropped, *e.EscalationModel) {
			e.EscalationModel = nil
			kept[k] = e
		}
	}
	slices.Sort(dropped)
	return kept, dropped
}

// Unrouted is, per backend that listed its models, the ones no route in r
// runs, in the CLI's order.
func (l ModelLists) Unrouted(r router.Router) map[model.Backend][]string {
	out := map[model.Backend][]string{}
	for b, listed := range l {
		for _, m := range listed {
			routed := false
			for _, e := range r {
				if e.Backend == b && e.CLIModel == m {
					routed = true
					break
				}
			}
			if !routed {
				out[b] = append(out[b], m)
			}
		}
	}
	return out
}

// DiscoverySummary is what init and start say about the discovery (#547):
// one line, per installed CLI the routes r (the project's router) gives it,
// how many of the models it lists no route runs, or that its list failed.
// The models themselves are `orch router refresh`'s to show. Empty when no
// CLI is installed.
func DiscoverySummary(installed func(model.Backend) bool, lists ModelLists, failed []model.Backend, r router.Router) string {
	var parts []string
	detail := false
	for _, b := range router.BackendPreference {
		if !installed(b) {
			continue
		}
		routes := 0
		for _, e := range r {
			if e.Backend == b {
				routes++
			}
		}
		if slices.Contains(failed, b) {
			parts = append(parts, fmt.Sprintf("%s %s (could not list its models)", b, plural(routes, "packaged route")))
			detail = true
			continue
		}
		unrouted := len(ModelLists{b: lists[b]}.Unrouted(r)[b])
		switch {
		case unrouted > 0:
			parts = append(parts, fmt.Sprintf("%s %s, %s not routed", b, plural(routes, "route"), plural(unrouted, "listed model")))
			detail = true
		case routes > 0:
			// A CLI with no route and nothing listed has nothing to say.
			parts = append(parts, fmt.Sprintf("%s %s", b, plural(routes, "route")))
		}
	}
	line := strings.Join(parts, " · ")
	if detail {
		line += " — see `orch router refresh`"
	}
	return line
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
