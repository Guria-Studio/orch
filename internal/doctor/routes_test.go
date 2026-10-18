package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/router"
)

func TestCheckRoutesSkipsWithNoTasks(t *testing.T) {
	c := CheckRoutes(nil, router.Router{"x": {}})
	if c.Status != StatusSkip {
		t.Errorf("c = %+v, want skip", c)
	}
}

func TestCheckRoutesSkipsWithNoRouter(t *testing.T) {
	c := CheckRoutes([]model.Task{{ID: "T1", Model: "x"}}, nil)
	if c.Status != StatusSkip {
		t.Errorf("c = %+v, want skip", c)
	}
}

func TestCheckRoutesOKWhenAllResolve(t *testing.T) {
	tasks := []model.Task{{ID: "T1", Model: "claude/opus"}}
	r := router.Router{"claude/opus": model.RouteEntry{Backend: model.BackendClaude, CLIModel: "opus"}}
	c := CheckRoutes(tasks, r)
	if c.Status != StatusOK {
		t.Errorf("c = %+v, want ok", c)
	}
}

// #266: a route that resolves but names a model the claude CLI rejects is a
// warning, so preflight is not green while every dispatch 404s.
func TestCheckRoutesWarnsOnAnUnknownClaudeModel(t *testing.T) {
	tasks := []model.Task{{ID: "T1", Model: "claude/sonnet-5"}, {ID: "T2", Model: "codex/gpt-x"}}
	r := router.Router{
		"claude/sonnet-5": model.RouteEntry{Backend: model.BackendClaude, CLIModel: "sonnet-5"},
		"codex/gpt-x":     model.RouteEntry{Backend: model.BackendCodex, CLIModel: "gpt-x"},
	}
	c := CheckRoutes(tasks, r)
	if c.Status != StatusWarn {
		t.Fatalf("c = %+v, want warn", c)
	}
	for _, want := range []string{"'claude/sonnet-5'", "'sonnet-5'", "'claude-sonnet-5'"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("detail %q does not name %s", c.Detail, want)
		}
	}
	if strings.Contains(c.Detail, "gpt-x") {
		t.Errorf("detail %q warns about codex, whose ids orch does not know", c.Detail)
	}
}

func TestCheckRoutesErrorsOnUnresolved(t *testing.T) {
	tasks := []model.Task{{ID: "T1", Model: "nope/nope"}}
	r := router.Router{"claude/opus": model.RouteEntry{Backend: model.BackendClaude}}
	c := CheckRoutes(tasks, r)
	if c.Status != StatusError {
		t.Errorf("c = %+v, want error", c)
	}
	if c.Remediation == "" {
		t.Errorf("expected a remediation hint")
	}
}

// #376: a default model whose CLI is missing is flagged with no tasks at all,
// which is the state a fresh `orch init` leaves.
func TestCheckDefaultModelsFlagsAMissingCLI(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o700); err != nil { // #nosec G306 -- an executable fake
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	backendOf := func(key string) string {
		if key == "opencode-go/kimi-k3" {
			return "opencode"
		}
		return ""
	}

	got := CheckDefaultModels(map[string]string{"standard": "claude/claude-sonnet-4-6", "cheap": "agy/flash"}, backendOf)
	if got.Status != StatusWarn || !strings.Contains(got.Detail, "cheap agy/flash (agy is not on PATH)") {
		t.Errorf("got %+v, want a warning naming agy", got)
	}
	got = CheckDefaultModels(map[string]string{"cheap": "opencode-go/kimi-k3"}, backendOf)
	if got.Status != StatusWarn || !strings.Contains(got.Detail, "opencode is not on PATH") {
		t.Errorf("got %+v, want the router's backend, not the key's prefix", got)
	}
	if got := CheckDefaultModels(map[string]string{"standard": "claude/claude-sonnet-4-6"}, backendOf); got.Status != StatusOK {
		t.Errorf("got %+v, want ok", got)
	}
	if got := CheckDefaultModels(nil, backendOf); got.Status != StatusSkip {
		t.Errorf("got %+v, want skip", got)
	}
}
