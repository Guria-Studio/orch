package project_test

import (
	"context"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
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
