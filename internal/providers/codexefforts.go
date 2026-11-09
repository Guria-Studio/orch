package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// CodexModelsCachePath is the model catalog codex keeps in its home:
// $CODEX_HOME/models_cache.json, ~/.codex/models_cache.json when CODEX_HOME
// is unset. codex refreshes it from its server as it runs, so it carries the
// reasoning efforts each model takes now (#576), which the catalog bundled
// with the CLI can lag. Reading it is a file read: no exec, no network.
func CodexModelsCachePath() string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".codex")
	}
	return filepath.Join(home, "models_cache.json")
}

// ParseCodexEfforts reads a codex model catalog — models_cache.json, or the
// same `models` array `codex debug models` prints — into each model slug's
// supported reasoning efforts, in the catalog's order. A catalog it cannot
// read, or one with no efforts at all, is nil: the caller falls back to the
// levels every model takes rather than trusting a partial answer.
func ParseCodexEfforts(raw []byte) map[string][]string {
	var cat struct {
		Models []struct {
			Slug   string `json:"slug"`
			Levels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &cat) != nil {
		return nil
	}
	out := map[string][]string{}
	for _, m := range cat.Models {
		for _, l := range m.Levels {
			if m.Slug != "" && l.Effort != "" {
				out[m.Slug] = append(out[m.Slug], l.Effort)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
