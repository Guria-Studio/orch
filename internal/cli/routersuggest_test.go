package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// suggestProject is a project whose "work" tasks ran 12 times on codex/gpt-5
// (the route now, 10 first-try successes) and 12 on claude-sonnet at half the
// cost (11), within the last week.
func suggestProject(t *testing.T) (root, routerPath string) {
	t.Helper()
	root = t.TempDir()
	cfgDir := filepath.Join(root, ".orchestrator")
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o750); err != nil {
		t.Fatal(err)
	}
	var tasks []string
	var defs []model.Task
	for i := range 24 {
		id := fmt.Sprintf("T%d", i)
		tasks = append(tasks, fmt.Sprintf(`{"id":%q,"phase":0,"title":"t","model":"work","dependencies":[]}`, id))
		defs = append(defs, model.Task{ID: id, Title: "t", Model: "work", Status: model.StatusTodo})
	}
	routerPath = filepath.Join(cfgDir, "model_router.yaml")
	for path, body := range map[string]string{
		filepath.Join(root, "tasks.json"):               `{"meta":{},"phases":[],"tasks":[` + strings.Join(tasks, ",") + `]}`,
		filepath.Join(cfgDir, "config.yaml"):            "state:\n  backend: sqlite\n",
		routerPath:                                      "# hand-written\nwork:\n  backend: codex\n  cli_model: gpt-5\n  tier: standard\n",
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
	if err := b.Bootstrap(ctx, defs); err != nil {
		t.Fatal(err)
	}
	if err := b.StartRun(ctx, "r1", "auto"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-7 * 24 * time.Hour).UTC()
	for i, d := range defs {
		backend, cli, cost, ok := "codex", "gpt-5", 1.0, i%12 < 10
		if i >= 12 {
			backend, cli, cost, ok = "claude", "claude-sonnet-4-6", 0.5, i%12 < 11
		}
		at = at.Add(time.Minute)
		ts := at.Format("2006-01-02T15:04:05Z")
		if err := b.RecordSpend(ctx, state.Spend{TS: ts, TaskID: d.ID, Backend: backend, Model: cli,
			Attempt: 1, CostUSD: cost, DurationS: 120}); err != nil {
			t.Fatal(err)
		}
		typ := "fail"
		if ok {
			typ = "success"
		}
		// Python wrote `+00:00`; the match must not depend on the spelling.
		if err := b.AppendEvent(ctx, "r1", state.Event{EventType: typ, TaskID: d.ID, Backend: backend,
			TS: at.Format("2006-01-02T15:04:05+00:00")}); err != nil {
			t.Fatal(err)
		}
	}
	return root, routerPath
}

func TestRouterSuggest(t *testing.T) {
	root, routerPath := suggestProject(t)
	common := []string{"router", "suggest", "--project-root", root, "--project-id", "p"}
	before, _ := os.ReadFile(routerPath) // #nosec G304 -- t.TempDir()

	out, err := runOrch(t, "", common...)
	if err != nil || !strings.Contains(out, "→ suggest claude/claude-sonnet-4-6: first-try success as good or within 5 points (92% vs 83%) at 50% of the cost") ||
		!strings.Contains(out, "Dry run") {
		t.Fatalf("dry run: err=%v\n%s", err, out)
	}

	out, err = runOrch(t, "", append(common, "--json")...)
	var p routerSuggestPayload
	if err != nil || json.Unmarshal([]byte(out), &p) != nil || len(p.Kinds) != 1 ||
		p.Kinds[0].Suggestion == nil || p.Kinds[0].Routes[0].Tasks != 12 {
		t.Fatalf("--json: err=%v\n%s", err, out)
	}

	// A window that starts after the history has nothing to compare.
	if out, err = runOrch(t, "", append(common, "--since", "1d")...); err != nil || !strings.Contains(out, "No changes") {
		t.Fatalf("--since 1d: err=%v\n%s", err, out)
	}

	if _, err = runOrch(t, "n\n", append(common, "--apply")...); err == nil {
		t.Fatal("a no at the prompt should exit non-zero")
	}
	if after, _ := os.ReadFile(routerPath); string(after) != string(before) { // #nosec G304 -- t.TempDir()
		t.Fatalf("a no wrote the router:\n%s", after)
	}

	out, err = runOrch(t, "y\n", append(common, "--apply")...)
	if err != nil || !strings.Contains(out, "-   backend: codex") || !strings.Contains(out, "+   cli_model: claude-sonnet-4-6") {
		t.Fatalf("--apply: err=%v\n%s", err, out)
	}
	want := "# hand-written\nwork:\n  backend: claude\n  cli_model: claude-sonnet-4-6\n  tier: standard\n"
	if after, _ := os.ReadFile(routerPath); string(after) != want { // #nosec G304 -- t.TempDir()
		t.Errorf("router after --apply:\n%s", after)
	}
}
