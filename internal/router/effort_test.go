package router

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/providers"
)

// withCodexCatalog points CODEX_HOME at a directory holding the
// models_cache.json codex 0.166 wrote, or at an empty one when catalog is
// false, so no test reads the operator's own ~/.codex.
func withCodexCatalog(t *testing.T, catalog bool) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	if catalog {
		raw, err := os.ReadFile(filepath.Join("..", "providers", "testdata", "codex", "0.166.0", "models_cache.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), raw, 0o600); err != nil { // #nosec G703 -- t.TempDir()
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "models_cache.json")
}

func codexRouteYAML(cliModel, effort string) string {
	return "codex/" + cliModel + ":\n  backend: codex\n  cli_model: " + cliModel +
		"\n  tier: premium\n  effort: " + effort + "\n"
}

// #576: codex's catalog lists xhigh, max and ultra for gpt-6-astra, so a
// route asking for one loads, and the dispatch hands it to codex as written.
func TestCodexEffortAboveHighLoadsWhenTheCatalogListsIt(t *testing.T) {
	withCodexCatalog(t, true)
	for _, effort := range []string{"xhigh", "max", "ultra"} {
		rtr, err := Load(writeRouter(t, codexRouteYAML("gpt-6-astra", effort)))
		if err != nil {
			t.Fatalf("effort %s: %v", effort, err)
		}
		argv := providers.CodexProvider{}.Argv(providers.Request{Route: rtr["codex/gpt-6-astra"]})
		if want := `model_reasoning_effort="` + effort + `"`; !slices.Contains(argv, want) {
			t.Errorf("argv lacks %s: %v", want, argv)
		}
	}
}

// A level the model does not list is refused, naming the levels it does.
func TestCodexEffortTheModelDoesNotListNamesItsLevels(t *testing.T) {
	withCodexCatalog(t, true)
	_, err := Load(writeRouter(t, codexRouteYAML("gpt-5.6-luna", "ultra")))
	if err == nil {
		t.Fatal("gpt-5.6-luna does not list ultra, and the route loaded")
	}
	for _, want := range []string{"'codex/gpt-5.6-luna'", "'low' | 'medium' | 'high' | 'xhigh' | 'max'", "got 'ultra'"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %s: %v", want, err)
		}
	}
}

// No catalog (codex never ran here, or a model it does not know): the levels
// every codex model takes, and the error says where the catalog was looked for.
func TestCodexEffortAboveHighWithoutACatalogFallsBack(t *testing.T) {
	path := withCodexCatalog(t, false)
	_, err := Load(writeRouter(t, codexRouteYAML("gpt-6-astra", "xhigh")))
	if err == nil {
		t.Fatal("xhigh loaded with no catalog to say the model takes it")
	}
	for _, want := range []string{"'low' | 'medium' | 'high'", "got 'xhigh'", path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %s: %v", want, err)
		}
	}

	withCodexCatalog(t, true)
	if _, err := Load(writeRouter(t, codexRouteYAML("gpt-9-unknown", "xhigh"))); err == nil ||
		!strings.Contains(err.Error(), "'low' | 'medium' | 'high'") {
		t.Errorf("a model the catalog lacks took xhigh: %v", err)
	}
	// low | medium | high never needed the catalog and still do not.
	withCodexCatalog(t, false)
	if _, err := Load(writeRouter(t, codexRouteYAML("gpt-6-astra", "high"))); err != nil {
		t.Errorf("high without a catalog: %v", err)
	}
}

// Levels above high are codex's: claude, agy, gemini and the rest keep
// low | medium | high even when codex's catalog lists more.
func TestEffortAboveHighIsCodexOnly(t *testing.T) {
	withCodexCatalog(t, true)
	for _, backend := range []string{"claude", "agy", "gemini", "muse", "opencode"} {
		_, err := Load(writeRouter(t, "k:\n  backend: "+backend+"\n  cli_model: gpt-6-astra\n  tier: standard\n  effort: xhigh\n"))
		if err == nil {
			t.Errorf("%s route took effort xhigh", backend)
			continue
		}
		for _, want := range []string{"'low' | 'medium' | 'high'", "got 'xhigh'", "codex"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error lacks %s: %v", backend, want, err)
			}
		}
	}
}
