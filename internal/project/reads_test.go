package project_test

import (
	"context"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
)

func runtimeIDs(rows []project.TaskRuntime) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// RuntimeOf answers for the ids asked and nothing else — in particular not
// every task when asked for none, which is what an empty filter means to the
// backend.
func TestRuntimeOf(t *testing.T) {
	ctx := context.Background()
	_, backend := openTransitionProject(t, model.StatusTodo, model.StatusDone, model.StatusTodo)
	for _, tc := range []struct {
		name string
		ids  []string
		want int
	}{
		{"one", []string{"T-2"}, 1},
		{"two", []string{"T-1", "T-3"}, 2},
		{"unknown", []string{"NOPE"}, 0},
		{"none", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := project.RuntimeOf(ctx, backend, tc.ids...)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.want {
				t.Fatalf("RuntimeOf(%v) = %v, want %d rows", tc.ids, runtimeIDs(rows), tc.want)
			}
		})
	}
}

func TestDoneRuntime(t *testing.T) {
	ctx := context.Background()
	_, backend := openTransitionProject(t, model.StatusTodo, model.StatusDone, model.StatusInProgress)
	rows, err := project.DoneRuntime(ctx, backend)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtimeIDs(rows); len(got) != 1 || got[0] != "T-2" {
		t.Fatalf("DoneRuntime = %v, want [T-2]", got)
	}
}

// onlyBackend hides every method of the SQLite backend but state.Backend's.
type onlyBackend struct{ state.Backend }

// FirstEventBackend answers from the database, and says "never" — not an
// error — for a backend that cannot answer, which is what the onboarding
// read did with its type assertion before (#378).
func TestFirstEventBackend(t *testing.T) {
	ctx := context.Background()
	_, backend := openTransitionProject(t, model.StatusTodo)
	if _, ok, err := project.FirstEventBackend(ctx, backend, "dispatch"); ok || err != nil {
		t.Fatalf("before any dispatch: ok=%v err=%v, want neither", ok, err)
	}
	for _, b := range []string{"codex", "claude"} {
		if err := backend.AppendEvent(ctx, "run-1", state.Event{EventType: "dispatch", TaskID: "T-1", Backend: b}); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := project.FirstEventBackend(ctx, backend, "dispatch")
	if err != nil || !ok || got != "codex" {
		t.Fatalf("FirstEventBackend = %q, %v, %v; want the first one, codex", got, ok, err)
	}
	if got, ok, err := project.FirstEventBackend(ctx, onlyBackend{backend}, "dispatch"); got != "" || ok || err != nil {
		t.Fatalf("a backend without the read: %q, %v, %v; want nothing", got, ok, err)
	}
}

func TestParseTSAndSummarySpendIDAreTheDatabases(t *testing.T) {
	for _, ts := range []string{"2026-11-09T12:00:00Z", "2026-11-09T12:00:00+00:00", "2026-11-09T12:00:00", "", "nope"} {
		got, ok := project.ParseTS(ts)
		want, wantOK := state.ParseTS(ts)
		if !got.Equal(want) || ok != wantOK {
			t.Errorf("ParseTS(%q) = %v, %v; state says %v, %v", ts, got, ok, want, wantOK)
		}
	}
	if got, want := project.SummarySpendID("run-7"), state.SummarySpendID("run-7"); got != want {
		t.Errorf("SummarySpendID = %q, want %q", got, want)
	}
}
