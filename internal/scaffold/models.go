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

// DiscoverySummary is what init and start say about the discovery, a line per
// installed CLI that listed its models or failed to; a CLI with no list says
// nothing, its routes being the packaged ones as before.
func DiscoverySummary(installed func(model.Backend) bool, lists ModelLists, failed []model.Backend) []string {
	packaged, err := PackagedRouter()
	if err != nil {
		return nil
	}
	var lines []string
	for _, b := range router.BackendPreference {
		if !installed(b) {
			continue
		}
		if slices.Contains(failed, b) {
			lines = append(lines, fmt.Sprintf("%s: could not list its models; its packaged routes are kept", b))
			continue
		}
		listed, ok := lists[b]
		if !ok {
			continue
		}
		one := ModelLists{b: listed}
		_, dropped := one.Available(packaged)
		line := fmt.Sprintf("%s lists %d models", b, len(listed))
		if len(dropped) > 0 {
			line += fmt.Sprintf("; packaged routes left out, not listed: %s", strings.Join(dropped, ", "))
		}
		lines = append(lines, line)
		if un := one.Unrouted(packaged)[b]; len(un) > 0 {
			lines = append(lines, fmt.Sprintf("  listed but not routed: %s", strings.Join(un, ", ")))
		}
	}
	return lines
}
