package budget

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const setFixture = `# Rolling windows per provider. Tune token_budget to your plan.
presets:
  conservative:
    claude:
      window_hours: 5
      token_budget: 800000 # Max 5x
      threshold_pct: 60
    codex:
      window_hours: 5
      token_budget: 1000000
      threshold_pct: 60
  aggressive:
    claude:
      window_hours: 5
      token_budget: 800000
      threshold_pct: 90
`

func intp(n int) *int           { return &n }
func floatp(f float64) *float64 { return &f }

// The edit changes only what it names, in the preset it names, and keeps the
// operator's comments and order.
func TestSetLimitsEditsOnlyWhatItNames(t *testing.T) {
	path := writeYAML(t, setFixture)
	got, err := SetLimits(path, "conservative", "claude", Limits{TokenBudget: intp(1_200_000), ThresholdPct: floatp(75)})
	if err != nil {
		t.Fatal(err)
	}
	if got != (ProviderBudget{WindowHours: 5, TokenBudget: 1_200_000, ThresholdPct: 75}) {
		t.Errorf("returned %+v", got)
	}
	raw, _ := os.ReadFile(path) // #nosec G304 -- path is under t.TempDir()
	text := string(raw)
	for _, keep := range []string{"# Rolling windows per provider", "# Max 5x"} {
		if !strings.Contains(text, keep) {
			t.Errorf("lost the comment %q:\n%s", keep, text)
		}
	}
	if strings.Index(text, "conservative") > strings.Index(text, "aggressive") {
		t.Errorf("presets reordered:\n%s", text)
	}
	cons, _ := LoadConfig(path, "conservative")
	aggr, _ := LoadConfig(path, "aggressive")
	if cons.Providers["claude"].TokenBudget != 1_200_000 || cons.Providers["codex"].TokenBudget != 1_000_000 {
		t.Errorf("conservative = %+v", cons.Providers)
	}
	if aggr.Providers["claude"].ThresholdPct != 90 {
		t.Errorf("another preset changed: %+v", aggr.Providers["claude"])
	}
}

// A provider the preset does not have is added only when every field is given.
func TestSetLimitsAddsAProviderWithEveryField(t *testing.T) {
	path := writeYAML(t, setFixture)
	if _, err := SetLimits(path, "conservative", "gemini", Limits{TokenBudget: intp(500_000)}); !errors.Is(err, ErrInvalidLimits) || !strings.Contains(err.Error(), "window_hours") {
		t.Errorf("partial new provider: err = %v", err)
	}
	got, err := SetLimits(path, "conservative", "gemini", Limits{TokenBudget: intp(500_000), WindowHours: floatp(24), ThresholdPct: floatp(80)})
	if err != nil || got.WindowHours != 24 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSetLimitsRefusesNonsense(t *testing.T) {
	path := writeYAML(t, setFixture)
	before, _ := os.ReadFile(path) // #nosec G304 -- path is under t.TempDir()
	for name, l := range map[string]Limits{
		"nothing":      {},
		"zero tokens":  {TokenBudget: intp(0)},
		"no window":    {WindowHours: floatp(0)},
		"over 100%":    {ThresholdPct: floatp(120)},
		"zero percent": {ThresholdPct: floatp(0)},
	} {
		if _, err := SetLimits(path, "conservative", "claude", l); !errors.Is(err, ErrInvalidLimits) {
			t.Errorf("%s: err = %v, want ErrInvalidLimits", name, err)
		}
	}
	var notFound *PresetNotFoundError
	if _, err := SetLimits(path, "nope", "claude", Limits{TokenBudget: intp(1)}); !errors.As(err, &notFound) {
		t.Errorf("unknown preset: err = %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) { // #nosec G304 -- path is under t.TempDir()
		t.Error("a refused edit changed the file")
	}
	if _, err := SetLimits(path+".missing", "conservative", "claude", Limits{TokenBudget: intp(1)}); err == nil {
		t.Error("a missing budgets.yaml was accepted")
	}
}
