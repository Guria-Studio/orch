package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
)

// TestMetricsReadsTheAttempts (#538): `orch metrics` prints the agents'
// quality figures from the attempts table, by route and tier, and --json
// gives the same as one object.
func TestMetricsReadsTheAttempts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	if out, err := runOrch(t, "", "init", dir, "--no-permissions"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	paths, cfg, err := loadProjectConfig(&projectFlags{root: dir})
	if err != nil {
		t.Fatal(err)
	}
	// A project with no attempt yet says so instead of a table of zeroes.
	if out, err := runOrch(t, "", "--project-root", dir, "metrics"); err != nil || !strings.Contains(out, "No attempts recorded yet") {
		t.Errorf("metrics on a fresh project = %v\n%s", err, out)
	}

	ctx := context.Background()
	backend, closeDB, err := openBackend(ctx, paths, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []state.Attempt{
		{RunID: "r1", TaskID: "X-1", Attempt: 1, Backend: "claude", Model: "sonnet", Tier: "standard", Result: "success", Verify: "fail", CostUSD: 1},
		{RunID: "r1", TaskID: "X-1", Attempt: 2, Backend: "claude", Model: "sonnet", Tier: "standard", Result: "success", Verify: "pass", CostUSD: 1},
	} {
		if err := backend.RecordAttempt(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	_ = closeDB()

	out, err := runOrch(t, "", "--project-root", dir, "metrics")
	if err != nil {
		t.Fatalf("metrics: %v\n%s", err, out)
	}
	for _, want := range []string{"claude/sonnet", "standard", "SEND-BACK", "50% of 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	out, err = runOrch(t, "", "--project-root", dir, "metrics", "--json")
	if err != nil {
		t.Fatalf("metrics --json: %v\n%s", err, out)
	}
	var m project.Metrics
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if m.Total.Attempts != 2 || m.Total.SentBack != 1 || len(m.ByRoute) != 1 {
		t.Errorf("metrics = %+v", m)
	}
}
