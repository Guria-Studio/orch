package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/providers"
	"github.com/hectorcanaimero/orch/internal/state"
)

func tuiSample() tuiView {
	return tuiView{
		Project:   "demo",
		RunStatus: "running",
		Rows: []tuiRow{
			{ID: "T1", Status: model.StatusDone, Backend: "claude", Model: "sonnet", Phase: 1, Elapsed: 90 * time.Second},
			{ID: "T2", Status: model.StatusInProgress, Backend: "codex", Model: "gpt-5", Phase: 1, Elapsed: 2*time.Hour + 5*time.Minute},
			{ID: "T3", Status: model.StatusTodo, Backend: "claude", Model: "sonnet", Phase: 2},
		},
		Usage:      []tuiUsage{{Backend: "claude", CostUSD: 1.5, TokensIn: 12_300, TokensOut: 800}},
		SelectedID: "T2",
		Steps: []providers.Step{
			{Kind: providers.StepStart},
			{Kind: providers.StepText, Detail: "from the first attempt"},
			{Kind: providers.StepStart},
			{Kind: providers.StepTool, Tool: "Read", Detail: "main.go"},
			{Kind: providers.StepText, Detail: "looks\nfine"},
		},
		Width:  70,
		Height: 16,
	}
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// The frame fills the terminal exactly — no line wider than it, no more lines
// than it has — or the redraw scrolls and the screen tears.
func TestRenderTUIFitsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{70, 16}, {20, 6}, {200, 50}} {
		v := tuiSample()
		v.Width, v.Height = size[0], size[1]
		lines := strings.Split(renderTUI(v), "\n")
		if len(lines) != v.Height {
			t.Errorf("%v: %d lines, want %d", size, len(lines), v.Height)
		}
		for _, l := range lines {
			if n := utf8.RuneCountInString(ansi.ReplaceAllString(l, "")); n > v.Width {
				t.Errorf("%v: line %q is %d wide", size, l, n)
			}
		}
	}
}

func TestRenderTUIShowsTheRun(t *testing.T) {
	out := renderTUI(tuiSample())
	for _, want := range []string{
		"demo · run running · filter: all",
		"P1 1/2  P2 0/1  done 1/3",
		"claude $1.50 (12.3k in / 800 out)",
		"\x1b[7mT2             in-progress  codex/gpt-5                  1     2h05m",
		"T1             done         claude/sonnet                1     1m30s",
		"── log T2 ",
		"• Read main.go",
		"  looks fine",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("frame lacks %q:\n%s", want, out)
		}
	}
	// Only the log's last attempt is shown.
	if strings.Contains(out, "first attempt") {
		t.Errorf("frame shows an earlier attempt:\n%s", out)
	}
}

func TestRenderTUIFilters(t *testing.T) {
	v := tuiSample()
	v.Filter = model.StatusTodo
	out := renderTUI(v)
	if strings.Contains(out, "T1 ") || strings.Contains(out, "T2 ") {
		t.Errorf("todo filter shows other tasks:\n%s", out)
	}
	// T2 left the filter, so the cursor lands on the first row that is left.
	if !strings.Contains(out, "\x1b[7mT3") || !strings.Contains(out, "filter: todo") {
		t.Errorf("todo filter does not select T3:\n%s", out)
	}
	v.Filter = model.StatusBlocked
	if out := renderTUI(v); !strings.Contains(out, "no tasks match the filter") {
		t.Errorf("empty filter says nothing:\n%s", out)
	}
}

func TestTUISelectionAndFilterKeys(t *testing.T) {
	rows := tuiSample().Rows
	if got := moveSelection(rows, "", "T2", +1); got != "T3" {
		t.Errorf("down from T2 = %s, want T3", got)
	}
	if got := moveSelection(rows, "", "T3", +1); got != "T3" {
		t.Errorf("down from the last row = %s, want it to stay", got)
	}
	if got := moveSelection(rows, "", "T1", -1); got != "T1" {
		t.Errorf("up from the first row = %s, want it to stay", got)
	}
	if got := moveSelection(rows, model.StatusTodo, "T2", -1); got != "T3" {
		t.Errorf("up inside a filter = %s, want T3", got)
	}
	f := model.Status("")
	for range tuiFilters {
		f = nextFilter(f)
	}
	if f != "" {
		t.Errorf("cycling every filter ends on %q, want all", f)
	}
}

func TestElapsedOf(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rt := state.TaskRuntime{StartedAt: "2026-01-01T11:00:00Z", FinishedAt: "2026-01-01T11:30:00+00:00", UpdatedAt: "2026-01-01T11:45:00Z"}
	for _, c := range []struct {
		status model.Status
		rt     state.TaskRuntime
		want   time.Duration
	}{
		{model.StatusInProgress, rt, time.Hour},
		{model.StatusDone, rt, 30 * time.Minute},
		{model.StatusBlocked, state.TaskRuntime{StartedAt: rt.StartedAt, UpdatedAt: rt.UpdatedAt}, 45 * time.Minute},
		{model.StatusTodo, state.TaskRuntime{}, 0},
	} {
		if got := elapsedOf(c.rt, c.status, now); got != c.want {
			t.Errorf("%s: elapsed = %v, want %v", c.status, got, c.want)
		}
	}
}

// Spend is summed per provider from the rows the dispatches recorded, and the
// task table comes from tasks.json joined with the database, as in `status`.
func TestLoadTUIView(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tasksJSON := `{"tasks":[{"id":"A","title":"a","phase":1,"model":"m","status":"todo"},{"id":"B","title":"b","phase":2,"model":"m","status":"todo"}]}`
	if err := os.WriteFile(filepath.Join(root, "tasks.json"), []byte(tasksJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{Root: root, ID: "p"}
	db, _, err := state.Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", root)
	if err := backend.Bootstrap(ctx, loadDAG(paths)); err != nil {
		t.Fatal(err)
	}
	for i, s := range []state.Spend{
		{TaskID: "A", Backend: "codex", CostUSD: 0.25, TokensIn: 100, TokensOut: 10},
		{TaskID: "A", Backend: "claude", CostUSD: 1, TokensIn: 1000, TokensOut: 50},
		{TaskID: "B", Backend: "claude", CostUSD: 0.5, TokensIn: 500, TokensOut: 25},
	} {
		s.TS = time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC).Format(time.RFC3339)
		if err := backend.RecordSpend(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	v, err := loadTUIView(ctx, paths, backend, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := []tuiUsage{
		{Backend: "claude", CostUSD: 1.5, TokensIn: 1500, TokensOut: 75},
		{Backend: "codex", CostUSD: 0.25, TokensIn: 100, TokensOut: 10},
	}
	if len(v.Usage) != len(want) || v.Usage[0] != want[0] || v.Usage[1] != want[1] {
		t.Errorf("usage = %+v, want %+v", v.Usage, want)
	}
	if len(v.Rows) != 2 || v.Rows[0].ID != "A" || v.Rows[1].Phase != 2 || v.RunStatus != "" {
		t.Errorf("view = %+v", v)
	}
}

// Without a terminal there is nothing to draw on: the command says so and
// points at the snapshot command instead of garbling a pipe.
func TestTUIRefusesWithoutATerminal(t *testing.T) {
	cmd := newTUICmd(&projectFlags{})
	cmd.SetArgs(nil)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "orch status") {
		t.Fatalf("err = %v, want one pointing at orch status", err)
	}
}

// `status` bootstraps the task definitions before it reads; a view polling
// beside `orch run` must not, so a project the database has never seen is
// still shown (from tasks.json) and is still absent from the database after.
func TestLoadTUIViewWritesNothing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tasks.json"), []byte(`{"tasks":[{"id":"A","title":"a","phase":1,"model":"m","status":"todo"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	db, _, err := state.Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", root)

	v, err := loadTUIView(ctx, config.Paths{Root: root, ID: "p"}, backend, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Rows) != 1 || v.Rows[0].Status != model.StatusTodo {
		t.Errorf("rows = %+v, want A from tasks.json", v.Rows)
	}
	if rt, err := backend.Tasks(ctx, state.TaskFilter{}); err != nil || len(rt) != 0 {
		t.Errorf("database tasks after a refresh = %v (err %v), want none", rt, err)
	}
}

func TestTUIKeys(t *testing.T) {
	rows := tuiSample().Rows
	for _, c := range []struct {
		name  string
		start tuiKeys
		keys  string
		want  tuiKeys
		quit  bool
	}{
		{"down arrow", tuiKeys{selID: "T1"}, "\x1b[B", tuiKeys{selID: "T2"}, false},
		{"up arrow", tuiKeys{selID: "T2"}, "\x1b[A", tuiKeys{selID: "T1"}, false},
		{"j", tuiKeys{selID: "T1"}, "jj", tuiKeys{selID: "T3"}, false},
		{"k", tuiKeys{selID: "T3"}, "k", tuiKeys{selID: "T2"}, false},
		{"half an arrow waits", tuiKeys{selID: "T1"}, "\x1b[", tuiKeys{selID: "T1", esc: 2}, false},
		{"f", tuiKeys{selID: "T1"}, "f", tuiKeys{selID: "T1", filter: model.StatusInProgress}, false},
		{"other key", tuiKeys{selID: "T1"}, "x", tuiKeys{selID: "T1"}, false},
		{"q", tuiKeys{selID: "T1"}, "q", tuiKeys{selID: "T1"}, true},
		{"ctrl+c", tuiKeys{selID: "T1"}, "\x03", tuiKeys{selID: "T1"}, true},
	} {
		k, quit := c.start, false
		for i := 0; i < len(c.keys) && !quit; i++ {
			k, quit = k.press(rows, c.keys[i])
		}
		if k != c.want || quit != c.quit {
			t.Errorf("%s: got %+v quit=%v, want %+v quit=%v", c.name, k, quit, c.want, c.quit)
		}
	}
}

// A log that cannot be read says so in the pane instead of looking empty.
func TestLogStepsShowsAReadError(t *testing.T) {
	dir := t.TempDir()
	// A directory where the log should be: it opens, but cannot be read.
	if err := os.MkdirAll(filepath.Join(dir, "logs", "T1.log"), 0o750); err != nil {
		t.Fatal(err)
	}
	steps := logSteps(dir, "T1")
	if len(steps) != 1 || steps[0].Kind != providers.StepError || !strings.Contains(steps[0].Detail, "reading the log") {
		t.Errorf("steps = %+v, want one error step", steps)
	}
	if steps := logSteps(dir, "missing"); len(steps) != 0 {
		t.Errorf("no log yet = %+v, want no steps", steps)
	}
}
