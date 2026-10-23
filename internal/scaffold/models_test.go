package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/router"
)

// What `opencode models` listed on a machine with only the opencode-go
// provider configured: two of the packaged routes' models, and one no route
// runs.
var opencodeListed = ModelLists{model.BackendOpencode: {"opencode-go/kimi-k3", "opencode-go/glm-5.2", "opencode-go/hy3"}}

func onlyInstalled(bs ...model.Backend) func(model.Backend) bool {
	return func(b model.Backend) bool { return slices.Contains(bs, b) }
}

// #397: a CLI that listed its models keeps only the packaged routes it runs;
// every other backend keeps all of its routes.
func TestModelListsAvailableDropsUnlistedRoutes(t *testing.T) {
	packaged, err := PackagedRouter()
	if err != nil {
		t.Fatal(err)
	}
	kept, dropped := opencodeListed.Available(packaged)
	for _, k := range []string{"opencode/kimi-k3", "opencode-go/kimi-k3", "opencode/glm-5.2", "claude/claude-opus-4-7", "agy/pro"} {
		if _, ok := kept[k]; !ok {
			t.Errorf("%s dropped; its model is listed, or its CLI listed nothing", k)
		}
	}
	for _, k := range []string{"opencode/deepseek-v4-pro", "opencode/gemini-2.5-pro", "opencode-go/glm-5.1"} {
		if _, ok := kept[k]; ok {
			t.Errorf("%s kept; opencode does not list its model", k)
		}
		if !slices.Contains(dropped, k) {
			t.Errorf("%s not reported as dropped: %q", k, dropped)
		}
	}
	if !slices.IsSorted(dropped) {
		t.Errorf("dropped not sorted: %q", dropped)
	}
	if len(kept)+len(dropped) != len(packaged) {
		t.Errorf("%d kept + %d dropped != %d packaged", len(kept), len(dropped), len(packaged))
	}
	// No lists at all is today's behaviour: everything kept.
	if all, none := ModelLists(nil).Available(packaged); len(all) != len(packaged) || len(none) != 0 {
		t.Errorf("nil lists: kept %d of %d, dropped %q", len(all), len(packaged), none)
	}
}

func TestModelListsUnrouted(t *testing.T) {
	packaged, err := PackagedRouter()
	if err != nil {
		t.Fatal(err)
	}
	got := opencodeListed.Unrouted(packaged)
	if want := []string{"opencode-go/hy3"}; !slices.Equal(got[model.BackendOpencode], want) {
		t.Errorf("unrouted = %q, want %q", got[model.BackendOpencode], want)
	}
}

// Acceptance: with opencode installed, the tier defaults are models opencode
// lists. Without the list, premium was opencode/deepseek-v4-pro, whose
// deepseek/ provider this machine has not configured.
func TestInstalledTierDefaultsUseListedModels(t *testing.T) {
	got, err := InstalledTierDefaults(onlyInstalled(model.BackendOpencode), opencodeListed)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"premium": "opencode/kimi-k3", "standard": "opencode/glm-5.2", "cheap": "opencode-go/kimi-k3"}
	for tier, k := range want {
		if got[tier] != k {
			t.Errorf("%s = %q, want %q (all: %v)", tier, got[tier], k, got)
		}
	}
}

func TestSeedInstalledRoutesSkipsUnlisted(t *testing.T) {
	root := t.TempDir()
	if _, err := Run(Options{Root: root, NoSampleTask: true}); err != nil {
		t.Fatal(err)
	}
	keys, err := SeedInstalledRoutes(root, onlyInstalled(model.BackendOpencode), opencodeListed)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	if want := []string{"opencode-go/kimi-k3", "opencode/glm-5.2", "opencode/kimi-k3"}; !slices.Equal(keys, want) {
		t.Errorf("seeded %q, want %q", keys, want)
	}
	rtr, err := router.Load(filepath.Join(root, ".orchestrator", "model_router.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rtr["opencode/deepseek-v4-pro"]; ok {
		t.Error("seeded a route opencode does not list")
	}
}

// fakeCLIs puts shell scripts named after CLIs on PATH, and nothing else.
func fakeCLIs(t *testing.T, scripts map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { // #nosec G306 -- a test's fake CLI must be executable
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

// Never block init: a CLI that hangs is cut off at the timeout, one that fails
// or prints nothing readable is reported, and neither stops the others.
func TestDiscoverModelsFallsBackOnFailure(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs /bin/sh")
	}
	fakeCLIs(t, map[string]string{
		"opencode": `printf 'opencode-go/kimi-k3\nopencode-go/hy3\n'`,
		"agy":      `exec /bin/sleep 30`,
		"codex":    `echo 'not logged in'; exit 1`,
		"claude":   `exit 0`,
	})
	prev := discoverTimeout
	discoverTimeout = 300 * time.Millisecond
	t.Cleanup(func() { discoverTimeout = prev })

	start := time.Now()
	lists, failed := DiscoverModels(context.Background(), router.OnPath)
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("discovery took %v; a hung CLI must not hold init", d)
	}
	if want := []string{"opencode-go/kimi-k3", "opencode-go/hy3"}; !slices.Equal(lists[model.BackendOpencode], want) {
		t.Errorf("opencode = %q, want %q", lists[model.BackendOpencode], want)
	}
	if len(lists) != 1 {
		t.Errorf("lists = %v, want opencode's only", lists)
	}
	if want := []model.Backend{model.BackendCodex, model.BackendAgy}; !slices.Equal(failed, want) {
		t.Errorf("failed = %q, want %q (claude has no list, so it never fails)", failed, want)
	}
}

func TestDiscoverySummary(t *testing.T) {
	lines := strings.Join(DiscoverySummary(onlyInstalled(model.BackendOpencode, model.BackendCodex, model.BackendClaude),
		opencodeListed, []model.Backend{model.BackendCodex}), "\n")
	for _, want := range []string{
		"opencode lists 3 models",
		"opencode/deepseek-v4-pro",
		"listed but not routed: opencode-go/hy3",
		"codex: could not list its models; its packaged routes are kept",
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("summary lacks %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "claude") {
		t.Errorf("claude has no model list and nothing to report:\n%s", lines)
	}
	if got := DiscoverySummary(onlyInstalled(model.BackendClaude), nil, nil); len(got) != 0 {
		t.Errorf("nothing listed or failed, summary = %q, want none", got)
	}
}

// A kept route whose escalation_model is a dropped route loses the
// escalation, not the route: router.Load refuses an escalation to a key the
// file lacks, and seeding one would break the project's router.
func TestAvailableClearsEscalationToADroppedRoute(t *testing.T) {
	root := t.TempDir()
	if _, err := Run(Options{Root: root, NoSampleTask: true}); err != nil {
		t.Fatal(err)
	}
	// opencode-go/mimo-v2.5 escalates to opencode-go/qwen3.7-plus, not listed.
	lists := ModelLists{model.BackendOpencode: {"opencode-go/mimo-v2.5"}}
	if _, err := SeedInstalledRoutes(root, onlyInstalled(model.BackendOpencode), lists); err != nil {
		t.Fatal(err)
	}
	rtr, err := router.Load(filepath.Join(root, ".orchestrator", "model_router.yaml"))
	if err != nil {
		t.Fatalf("the seeded router does not load: %v", err)
	}
	e, ok := rtr["opencode-go/mimo-v2.5"]
	if !ok {
		t.Fatal("opencode-go/mimo-v2.5 not seeded")
	}
	if e.EscalationModel != nil {
		t.Errorf("escalation_model = %q, a route that was not seeded", *e.EscalationModel)
	}
	if e.FallbackCLIModel == nil || *e.FallbackCLIModel != "opencode-go/mimo-v2.5-pro" {
		t.Errorf("fallback_cli_model lost: %v", e.FallbackCLIModel)
	}
}
