package router

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
	"github.com/Guria-Studio/orch/internal/pyfmt"
)

// baseEfforts are the reasoning efforts every backend takes, and every codex
// model whatever its catalog says.
var baseEfforts = []string{"low", "medium", "high"}

// validateEffort checks a route's `effort` (#576). low | medium | high go to
// any backend. A level beyond them (xhigh, max, ultra…) is codex's, and only
// for a model codex's own catalog lists it for: orch does not keep a table of
// which model takes which level, because codex's server changes that between
// releases. No catalog, or a model it does not have, falls back to the base
// levels, and the error names where the catalog was looked for.
func validateEffort(path, key string, e model.RouteEntry) error {
	if e.Effort == nil || slices.Contains(baseEfforts, *e.Effort) {
		return nil
	}
	if e.Backend != model.BackendCodex {
		return &FormatError{Path: path, Msg: fmt.Sprintf(
			"entry %s field `effort` must be one of %s, got %s "+
				"(levels above high are codex's, for a model its catalog lists them for)",
			pyfmt.Quote(key), effortList(baseEfforts), pyfmt.Quote(*e.Effort))}
	}
	catalog := providers.CodexModelsCachePath()
	listed := codexEfforts(catalog)[e.CLIModel]
	if slices.Contains(listed, *e.Effort) {
		return nil
	}
	if len(listed) == 0 {
		return &FormatError{Path: path, Msg: fmt.Sprintf(
			"entry %s field `effort` must be one of %s, got %s: codex's model catalog "+
				"(%s) does not list efforts for %s — run codex once so it writes the catalog, "+
				"or use one of those",
			pyfmt.Quote(key), effortList(baseEfforts), pyfmt.Quote(*e.Effort),
			catalog, pyfmt.Quote(e.CLIModel))}
	}
	allowed := slices.Clone(baseEfforts)
	for _, l := range listed {
		if !slices.Contains(allowed, l) {
			allowed = append(allowed, l)
		}
	}
	return &FormatError{Path: path, Msg: fmt.Sprintf(
		"entry %s field `effort` must be one of %s for codex model %s (codex's model catalog, %s), got %s",
		pyfmt.Quote(key), effortList(allowed), pyfmt.Quote(e.CLIModel), catalog, pyfmt.Quote(*e.Effort))}
}

// codexEfforts reads codex's catalog at path; a missing or unreadable one is
// none. Read per call: only a route asking for more than high gets here.
func codexEfforts(path string) map[string][]string {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- codex's own catalog under CODEX_HOME
	if err != nil {
		return nil
	}
	return providers.ParseCodexEfforts(raw)
}

func effortList(levels []string) string {
	quoted := make([]string, len(levels))
	for i, l := range levels {
		quoted[i] = pyfmt.Quote(l)
	}
	return strings.Join(quoted, " | ")
}
