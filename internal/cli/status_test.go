package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/evidence"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/pricing"
	"github.com/Guria-Studio/orch/internal/router"
	"github.com/Guria-Studio/orch/internal/state"
)

// A todo task the budget gate deferred says so in `defer_reason`, which had
// been hard-coded to null: the reason lived only in the run's memory, so
// `orch status` showed a ready task sitting still with no explanation. A
// reset already past is not waiting any more.
func TestStatusShowsTheBudgetDeferral(t *testing.T) {
	ctx := context.Background()
	db, _, err := state.Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", t.TempDir())

	tasks := []model.Task{
		{ID: "A", Title: "waits", Status: model.StatusTodo, Model: "claude/opus"},
		{ID: "B", Title: "waited", Status: model.StatusTodo, Model: "claude/opus"},
	}
	if err := backend.Bootstrap(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	past := time.Now().Add(-time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	for id, reset := range map[string]string{"A": future, "B": past} {
		if err := backend.AppendEvent(ctx, "run-1", state.Event{
			EventType: "budget_skip", TaskID: id, Backend: "claude",
			Extra: map[string]any{"reset_at": reset, "reason": "claude over threshold"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := buildStatusRows(ctx, backend, "p", tasks, router.Router{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "blocked-by-budget:claude until " + future
	if rows[0].DeferReason == nil || *rows[0].DeferReason != want {
		t.Errorf("A defer_reason = %v, want %q", rows[0].DeferReason, want)
	}
	if rows[1].DeferReason != nil {
		t.Errorf("B defer_reason = %q, want null once the reset has passed", *rows[1].DeferReason)
	}
}

func TestParseStatusList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]bool
	}{
		{"empty", "", nil},
		{"single", "todo", map[string]bool{"todo": true}},
		{"multiple", "todo,in-progress", map[string]bool{"todo": true, "in-progress": true}},
		{"whitespace and blanks collapse", " todo , , done ", map[string]bool{"todo": true, "done": true}},
		{"only blanks", " , ,", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStatusList(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseStatusList(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCostForMatchesPythonsSumTypeQuirk(t *testing.T) {
	// Python: sum(cost_by_task.values()) over an empty dict is the int 0
	// (json "0"); sum(r["cost_usd"] for r in filtered) over a non-empty
	// list of floats is a float (json "0.0"), even when every term is
	// zero — but over an EMPTY filtered list, sum() of an empty generator
	// is int 0 again. See the costJSON doc comment in status.go.
	cases := []struct {
		name          string
		costByTask    map[string]pricing.Sum
		filteredSum   float64
		filteredCount int
		wantProject   string
		wantFiltered  string
	}{
		{"no spend at all", nil, 0, 0, "0", "0"},
		{"spend exists but filtered set is empty", map[string]pricing.Sum{"F0.T1": {Rows: 1, Reported: 0.42}}, 0, 0, "0.42", "0"},
		{"single zero-cost task still filtered in", map[string]pricing.Sum{"F0.T1": {Rows: 1}}, 0, 1, "0.0", "0.0"},
		{"real spend, all filtered in", map[string]pricing.Sum{"F0.T1": {Rows: 1, Reported: 0.42}, "F1.T1": {Rows: 1, Reported: 1.0}}, 1.42, 5, "1.42", "1.42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := costFor(tc.costByTask, pricing.Sum{Reported: tc.filteredSum}, tc.filteredCount)
			if string(got.ProjectTotalUSD) != tc.wantProject {
				t.Errorf("ProjectTotalUSD = %s, want %s", got.ProjectTotalUSD, tc.wantProject)
			}
			if string(got.FilteredTotalUSD) != tc.wantFiltered {
				t.Errorf("FilteredTotalUSD = %s, want %s", got.FilteredTotalUSD, tc.wantFiltered)
			}
		})
	}
}

// A codex task on a subscription records cost_usd 0 with real tokens, and
// `orch status` printed `$0.0` for it — a measured-looking zero next to an
// `orch run` estimate in dollars (#578). The column now says where the number
// comes from: billed as is, priced from pricing.yaml with a `~`, `N/A` when a
// run left nothing to price, `—` when the task never ran. The estimate is the
// dashboard's (pricing.Table), not a second price list.
func TestStatusMarksUnbilledCostAsEstimateOrNA(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, _, err := state.Open(ctx, filepath.Join(root, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", root)

	tasks := []model.Task{
		{ID: "BILLED", Title: "claude billed", Status: model.StatusDone, Model: "claude/sonnet"},
		{ID: "SUB", Title: "codex subscription", Status: model.StatusDone, Model: "codex/gpt-5"},
		{ID: "MIXED", Title: "billed then unbilled", Status: model.StatusDone, Model: "codex/gpt-5"},
		{ID: "SILENT", Title: "no tokens, no dollars", Status: model.StatusDone, Model: "codex/gpt-5"},
		{ID: "NEVER", Title: "never ran", Status: model.StatusTodo, Model: "codex/gpt-5"},
	}
	if err := backend.Bootstrap(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	ts := "2026-11-08T10:00:0%dZ"
	for i, sp := range []state.Spend{
		{TaskID: "BILLED", Backend: "claude", Model: "claude-sonnet-4-6", TokensIn: 1000, TokensOut: 100, CostUSD: 0.42},
		{TaskID: "SUB", Backend: "codex", Model: "gpt-5", TokensIn: 100_000, TokensOut: 10_000},
		{TaskID: "MIXED", Backend: "claude", Model: "claude-sonnet-4-6", TokensIn: 1000, TokensOut: 100, CostUSD: 0.5},
		{TaskID: "MIXED", Backend: "codex", Model: "gpt-5", TokensIn: 100_000, TokensOut: 10_000},
		{TaskID: "SILENT", Backend: "codex", Model: "gpt-5"},
	} {
		sp.TS = fmt.Sprintf(ts, i)
		if err := backend.RecordSpend(ctx, sp); err != nil {
			t.Fatal(err)
		}
	}

	snap, err := buildSnapshot(ctx, config.Paths{Root: root, ID: "p"}, backend, tasks, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// gpt-5 at 2.50/10.00 per million: 100k in + 10k out = $0.35.
	if want := pricing.Load(root).EstimateCost("gpt-5", pricing.Tokens{In: 100_000, Out: 10_000}); want != 0.35 {
		t.Fatalf("fixture assumes gpt-5 prices 100k/10k at $0.35, pricing.yaml says %v", want)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := renderStatusTable(cmd, snap); err != nil {
		t.Fatal(err)
	}
	table := out.String()
	for id, want := range map[string]string{
		"BILLED": `\$0\.42`,
		"SUB":    `~\$0\.35`,
		"MIXED":  `~\$0\.85`,
		"SILENT": `N/A`,
		"NEVER":  `—`,
	} {
		re := regexp.MustCompile(`(?m)^` + id + `\s.*\s` + want + `\s+\d+\s*$`)
		if !re.MatchString(table) {
			t.Errorf("%s: want COST %s in\n%s", id, want, table)
		}
	}
	// The total under the table says how much of it is an estimate.
	if want := "Cost: ~$1.62 ($0.92 billed + ~$0.7 estimated from pricing.yaml"; !strings.Contains(table, want) {
		t.Errorf("want total line %q in\n%s", want, table)
	}
	if strings.Contains(table, "$0.0 ") {
		t.Errorf("no row ran for free, yet the table shows $0.0:\n%s", table)
	}

	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	js := string(body)
	for _, want := range []string{
		// cost_usd stays what the CLI billed: scripts reading it keep working.
		`"id":"BILLED"[^}]*"cost_usd":0.42,"cost_source":"reported","estimated_cost_usd":0.0`,
		`"id":"SUB"[^}]*"cost_usd":0.0,"cost_source":"estimated","estimated_cost_usd":0.35`,
		`"id":"MIXED"[^}]*"cost_usd":0.5,"cost_source":"reported","estimated_cost_usd":0.35`,
		`"id":"SILENT"[^}]*"cost_usd":0.0,"cost_source":"no_data","estimated_cost_usd":0.0`,
		`"id":"NEVER"[^}]*"cost_usd":0.0,"cost_source":"none","estimated_cost_usd":0.0`,
		`"cost":\{"project_total_usd":0.92,"filtered_total_usd":0.92,"project_estimated_usd":0.7,"filtered_estimated_usd":0.7\}`,
	} {
		if !regexp.MustCompile(want).MatchString(js) {
			t.Errorf("status --json: want %s in\n%s", want, js)
		}
	}
}

// `status --json` shows, per task, what its latest attempt's CLI said it ran
// beside the declared cli_model, and whether that differs (#580). A task
// whose attempts reported nothing has no `effective` at all: unknown is not
// the declared value.
func TestStatusShowsTheEffectiveRoute(t *testing.T) {
	ctx := context.Background()
	db, _, err := state.Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", t.TempDir())
	tasks := []model.Task{
		{ID: "A", Title: "aliased", Status: model.StatusDone, Model: "codex/luna"},
		{ID: "B", Title: "silent", Status: model.StatusDone, Model: "codex/luna"},
	}
	if err := backend.Bootstrap(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	for _, a := range []state.Attempt{
		{RunID: "r", TaskID: "A", Attempt: 1, Backend: "codex", Model: "gpt-6-luna", Result: "fail", EffectiveModel: "gpt-6-luna"},
		{RunID: "r", TaskID: "A", Attempt: 2, Backend: "codex", Model: "gpt-6-luna", ServiceTier: "flex", Result: "success",
			EffectiveModel: "gpt-6-astra", EffectiveEffort: "high", EffectiveServiceTier: "default"},
		{RunID: "r", TaskID: "B", Attempt: 1, Backend: "codex", Model: "gpt-6-luna", Result: "success"},
	} {
		if err := backend.RecordAttempt(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := buildStatusRows(ctx, backend, "p", tasks, router.Router{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	want := `"effective":{"attempt":2,"model":"gpt-6-astra","effort":"high","service_tier":"default","mismatch":["model","service_tier"]}`
	if !strings.Contains(string(body), want) {
		t.Errorf("A = %s\nwant it to contain %s", body, want)
	}
	if rows[1].Effective != nil {
		t.Errorf("B effective = %+v, want none", rows[1].Effective)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := renderStatusTable(cmd, statusSnapshot{Totals: newOrderedCount(), Tasks: rows}); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^A\s+done\s+\?/codex/luna \(ran gpt-6-astra\)\s`).MatchString(out.String()) {
		t.Errorf("table:\n%s", out.String())
	}
	if !regexp.MustCompile(`(?m)^B\s+done\s+\?/codex/luna\s`).MatchString(out.String()) {
		t.Errorf("B shows a ran-model it never reported:\n%s", out.String())
	}
}

// TestStatusJSONCarriesEachTasksEvidence is #520's reading side: a task with
// an evidence manifest has it under `evidence` in `status --json`; one
// without has no key at all, so the shape scripts read is unchanged.
func TestStatusJSONCarriesEachTasksEvidence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, _, err := state.Open(ctx, filepath.Join(root, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", root)
	paths := config.Paths{Root: root, ID: "p"}
	tasks := []model.Task{
		{ID: "F2.T3", Title: "Login", Status: model.StatusDone, Model: "claude/sonnet"},
		{ID: "F2.T4", Title: "Logout", Status: model.StatusTodo, Model: "claude/sonnet"},
	}
	m := evidence.Manifest{Version: evidence.Version, TaskID: "F2.T3", RunID: "run-1", Attempt: 1,
		Verdict: evidence.VerdictPassed, Checks: []evidence.Check{{Command: "go test ./...", Kind: "tests"}}}
	if err := evidence.Promote(paths.StateDir(), m); err != nil {
		t.Fatal(err)
	}

	snap, err := buildSnapshot(ctx, paths, backend, tasks, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(snap.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	if got := string(rows[0]["evidence"]); !strings.Contains(got, `"verdict":"passed"`) || !strings.Contains(got, `"kind":"tests"`) {
		t.Errorf("F2.T3 evidence = %s, want its manifest", got)
	}
	if _, ok := rows[1]["evidence"]; ok {
		t.Errorf("F2.T4 has an evidence key with no manifest: %s", rows[1]["evidence"])
	}
}
