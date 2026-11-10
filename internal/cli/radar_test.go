package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// radarProject is a project whose orch.db holds the spec's acceptance case
// (#522 §6.0): two radar warnings and three collisions, the first of them
// predicted, all within the last week; and one older collision.
func radarProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfgDir := filepath.Join(root, ".orchestrator")
	for path, body := range map[string]string{
		filepath.Join(root, "tasks.json"):               `{"meta":{},"phases":[],"tasks":[]}`,
		filepath.Join(cfgDir, "config.yaml"):            "state:\n  backend: sqlite\nradar: warn\n",
		filepath.Join(root, "scripts", "task-start.sh"): "#!/usr/bin/env bash\nexit 0\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	db, _, err := state.Open(ctx, filepath.Join(cfgDir, "state", "p", "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	b := state.NewSQLite(db, "p", root)
	var defs []model.Task
	for _, id := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		defs = append(defs, model.Task{ID: id, Title: id, Model: "m", Status: model.StatusTodo})
	}
	if err := b.Bootstrap(ctx, defs); err != nil {
		t.Fatal(err)
	}
	if err := b.StartRun(ctx, "r1", "auto"); err != nil {
		t.Fatal(err)
	}
	day := time.Now().UTC().Add(-2 * 24 * time.Hour).Truncate(time.Hour)
	ts := func(h int) string { return day.Add(time.Duration(h) * time.Hour).Format(time.RFC3339) }
	for _, e := range []state.Event{
		{EventType: "ci_conflict_retry", TaskID: "G", TS: day.Add(-40 * 24 * time.Hour).Format(time.RFC3339),
			Extra: map[string]any{"agent": true, "attempt": 1, "files": []string{"old.go"}}},
		{EventType: "files_overlap", TaskID: "A", TS: ts(0), Extra: map[string]any{"with": "B", "files": []string{"src/"}, "mode": "warn"}},
		{EventType: "branch_conflict", TaskID: "C", TS: ts(1), Extra: map[string]any{"with": "D", "files": []string{"web/x.ts"}}},
		{EventType: "ci_conflict_retry", TaskID: "A", TS: ts(2), Extra: map[string]any{"agent": true, "attempt": 1, "files": []string{"src/a.go"}}},
		{EventType: "ci_conflict_retry", TaskID: "E", TS: ts(3), Extra: map[string]any{"agent": true, "attempt": 1, "files": []string{"lib/z.go"}}},
		{EventType: "ci_blocked", TaskID: "F", TS: ts(4), Extra: map[string]any{
			"reason": "the PR conflicts with its base branch, so CI will not run (conflicts in docs/y.md)"}},
	} {
		if err := b.AppendEvent(ctx, "r1", e); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRadarReport(t *testing.T) {
	root := radarProject(t)
	common := []string{"radar", "report", "--project-root", root, "--project-id", "p"}

	out, err := runOrch(t, "", common...)
	if err != nil {
		t.Fatalf("radar report: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Conflict radar (radar: warn), since ",
		"Warnings:   2 (files_overlap 1, branch_conflict 1) about 2 pair(s) of tasks; 1 pair(s) collided later",
		"Collisions: 3 (pr_conflict 2, pr_conflict_blocked 1); 1 predicted, 2 escaped",
		"Recall:     33% of the collisions had a warning first",
		"Precision:  50% of the warned pairs collided",
		"A ~ B (files)",
		"docs/y.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "old.go") {
		t.Errorf("a collision older than --since 30d is in the report:\n%s", out)
	}

	out, err = runOrch(t, "", append(common, "--since", "all")...)
	if err != nil || !strings.Contains(out, "Collisions: 4") || !strings.Contains(out, "all of history") {
		t.Errorf("--since all: err=%v\n%s", err, out)
	}

	out, err = runOrch(t, "", append(common, "--json")...)
	if err != nil {
		t.Fatalf("--json: %v\n%s", err, out)
	}
	// The JSON's shape is the contract: keys and order are fixed.
	for _, want := range []string{
		`{"since":"30d","radar":"warn","from":"`,
		`"predictions":{"files_overlap":1,"branch_conflict":1,"pairs":2,"pairs_collided":1},` +
			`"collisions":{"total":3,"predicted":1,"escaped":2,"by_kind":{"pr_conflict":2,"pr_conflict_blocked":1}},` +
			`"recall":0.3333333333333333,"precision":0.5,"pairs":[{"tasks":["A","B"],"kinds":["files_overlap"],"files":["src/"],"first_at":"`,
		`"collided":true},{"tasks":["C","D"],"kinds":["branch_conflict"],"files":["web/x.ts"],"first_at":"`,
		`{"task_id":"A","kind":"pr_conflict","event_type":"ci_conflict_retry","at":"`,
		`"files":["src/a.go"],"with":[],"predicted":true,"predicted_by":["A ~ B"],"match":"files"}`,
		`{"task_id":"F","kind":"pr_conflict_blocked","event_type":"ci_blocked","at":"`,
		`"files":["docs/y.md"],"with":[],"predicted":false,"predicted_by":[],"match":""}]}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--json lacks %q:\n%s", want, out)
		}
	}

	if _, err := runOrch(t, "", append(common, "--since", "soon")...); err == nil || !strings.Contains(err.Error(), "--since") {
		t.Errorf("--since soon: err = %v, want a usage error", err)
	}
}

// TestRadarReportWithTheRadarOff: no warning can exist, and the report says
// why instead of a bare 0%.
func TestRadarReportWithTheRadarOff(t *testing.T) {
	root := radarProject(t)
	cfg := filepath.Join(root, ".orchestrator", "config.yaml")
	if err := os.WriteFile(cfg, []byte("state:\n  backend: sqlite\nradar: \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runOrch(t, "", "radar", "report", "--project-root", root, "--project-id", "p", "--since", "1h")
	if err != nil || !strings.Contains(out, "The radar is off in config.yaml") || !strings.Contains(out, "Precision:  —") {
		t.Errorf("err=%v\n%s", err, out)
	}
}
