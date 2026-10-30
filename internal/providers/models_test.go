package providers

import (
	"slices"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
)

// #397: the CLIs that can say which models they run now, read from what each
// one really printed (testdata/README.md says how each file was captured).
func TestParseModelsFromRealCaptures(t *testing.T) {
	for _, tc := range []struct {
		backend  model.Backend
		fixture  []string
		argv     []string
		count    int
		has      []string
		hasNot   []string
		firstOut string
	}{
		{model.BackendOpencode, []string{"opencode", "1.18.32", "models.txt"}, []string{"opencode", "models"},
			39, []string{"opencode-go/kimi-k3", "opencode-go/glm-5.2", "9router/code"}, []string{"google/gemini-2.5-pro"}, "opencode/beam-free"},
		{model.BackendAgy, []string{"agy", "1.4.6", "models.txt"}, []string{"agy", "models"},
			13, []string{"gemini-4-argon-high", "gemini-3.8-flash-medium"}, []string{"Fetching", "Gemini 4 Argon (High)"}, "gemini-4-argon-medium"},
		// Only the models codex shows (visibility "list"): a hidden slug such
		// as codex-auto-review is codex's own, not one to route a task to.
		{model.BackendCodex, []string{"codex", "0.154.0", "models.json"}, []string{"codex", "debug", "models"},
			4, []string{"gpt-6-astra", "gpt-5.6-luna"}, []string{"codex-auto-review", "gpt-reserve", "gpt-5.4"}, "gpt-6-astra"},
	} {
		t.Run(string(tc.backend), func(t *testing.T) {
			p, err := Get(tc.backend)
			if err != nil {
				t.Fatal(err)
			}
			l, ok := p.(ModelLister)
			if !ok {
				t.Fatalf("%s has a model list but its adapter is no ModelLister", tc.backend)
			}
			if argv := l.ModelsArgv(); !slices.Equal(argv, tc.argv) {
				t.Errorf("ModelsArgv = %q, want %q", argv, tc.argv)
			}
			got := l.ParseModels(readFixture(t, tc.fixture...))
			if len(got) != tc.count {
				t.Fatalf("%d models, want %d: %q", len(got), tc.count, got)
			}
			if got[0] != tc.firstOut {
				t.Errorf("first model = %q, want %q (the CLI's own order)", got[0], tc.firstOut)
			}
			for _, m := range tc.has {
				if !slices.Contains(got, m) {
					t.Errorf("%q missing from %q", m, got)
				}
			}
			for _, m := range tc.hasNot {
				if slices.Contains(got, m) {
					t.Errorf("%q should not be listed: %q", m, got)
				}
			}
		})
	}
}

// Output a parser cannot read is no list at all, never a list of garbage: an
// empty result is what tells the caller to keep the packaged routes.
func TestParseModelsUnreadableIsEmpty(t *testing.T) {
	for _, b := range []model.Backend{model.BackendOpencode, model.BackendAgy, model.BackendCodex} {
		p, _ := Get(b)
		l := p.(ModelLister)
		for _, out := range []string{"", "Error: not logged in\n", "{not json"} {
			if got := l.ParseModels([]byte(out)); len(got) != 0 {
				t.Errorf("%s.ParseModels(%q) = %q, want none", b, out, got)
			}
		}
	}
}

// claude, gemini and muse have no command that lists their models (checked
// on claude 2.1.309, gemini 0.59.0, muse 1.4.12): their packaged routes stay.
func TestNoModelListForClaudeGeminiMuse(t *testing.T) {
	for _, b := range []model.Backend{model.BackendClaude, model.BackendGemini, model.BackendMuse} {
		p, _ := Get(b)
		if _, ok := p.(ModelLister); ok {
			t.Errorf("%s is a ModelLister; it has no model list to read", b)
		}
	}
}
