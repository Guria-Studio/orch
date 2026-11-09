package providers

import (
	"path/filepath"
	"slices"
	"testing"
)

// #576: codex's model catalog says which reasoning efforts each model takes,
// read from the models_cache.json codex 0.166 wrote (testdata/README.md).
func TestParseCodexEffortsFromModelsCache(t *testing.T) {
	got := ParseCodexEfforts(readFixture(t, "codex", "0.166.0", "models_cache.json"))
	for slug, want := range map[string][]string{
		"gpt-6-astra":  {"low", "medium", "high", "xhigh", "max", "ultra"},
		"gpt-5.6-luna": {"low", "medium", "high", "xhigh", "max"},
	} {
		if !slices.Equal(got[slug], want) {
			t.Errorf("%s efforts = %q, want %q (the catalog's order)", slug, got[slug], want)
		}
	}
	if _, ok := got["no-such-model"]; ok {
		t.Error("a model the catalog does not have has efforts")
	}
}

// A catalog that cannot be read is no catalog: the router falls back to
// low | medium | high rather than trusting garbage.
func TestParseCodexEffortsUnreadableIsNil(t *testing.T) {
	for _, out := range []string{"", "{not json", `{"models": []}`} {
		if got := ParseCodexEfforts([]byte(out)); got != nil {
			t.Errorf("ParseCodexEfforts(%q) = %v, want nil", out, got)
		}
	}
}

// codex keeps its state under CODEX_HOME, ~/.codex when unset.
func TestCodexModelsCachePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	if got, want := CodexModelsCachePath(), filepath.Join(dir, "models_cache.json"); got != want {
		t.Errorf("with CODEX_HOME: %q, want %q", got, want)
	}
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HOME", dir)
	if got, want := CodexModelsCachePath(), filepath.Join(dir, ".codex", "models_cache.json"); got != want {
		t.Errorf("without CODEX_HOME: %q, want %q", got, want)
	}
}

// A level above high reaches codex exactly as written (#576).
func TestCodexArgvForwardsAnEffortAboveHigh(t *testing.T) {
	xhigh := "xhigh"
	route := codexRoute("gpt-6-astra")
	route.Effort = &xhigh
	argv := CodexProvider{}.Argv(Request{Route: route})
	if !slices.Contains(argv, `model_reasoning_effort="xhigh"`) {
		t.Errorf("argv does not set model_reasoning_effort=\"xhigh\": %v", argv)
	}
}
