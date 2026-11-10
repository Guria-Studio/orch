package engine

import (
	"context"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/worktree"
)

// TestConflictCausedBy (#522): the base commits behind a conflict name the
// tasks whose work they are — by the PR number a squash merge appends, the
// task title a local land commits, or an "<id>:" prefix — for the radar
// report to match against the radar's warnings.
func TestConflictCausedBy(t *testing.T) {
	f := newReapFixture(t,
		[]model.Task{task("C-1", 1, "claude/opus"), task("C-2", 1, "claude/opus"),
			task("C-3", 1, "claude/opus"), task("C-4", 1, "claude/opus")},
		nil, SchedulerOptions{})
	f.s.PRs = prMap{"C-1": "https://github.com/o/r/pull/7", "C-2": "https://github.com/o/r/pull/8"}
	ctx := context.Background()
	tests := []struct {
		name    string
		commits []string
		want    []string
	}{
		{"a squash merge's PR number", []string{"abc1234 Add the login form (#7)"}, []string{"C-1"}},
		{"a local land's task title", []string{"abc1234 C-3 title"}, []string{"C-3"}},
		{"a PR number no task has, then its title", []string{"abc1234 C-4 title (#99)"}, []string{"C-4"}},
		{"an id prefix", []string{"abc1234 C-4: tweak the parser"}, []string{"C-4"}},
		{"the task itself is left out", []string{"abc1234 Mine (#8)", "def5678 Theirs (#7)"}, []string{"C-1"}},
		{"several, deduplicated and sorted", []string{"a C-3 title", "b x (#7)", "c C-3 title"}, []string{"C-1", "C-3"}},
		{"a commit no task made", []string{"abc1234 bump deps", "def5678 X-9: other"}, nil},
		{"no commits", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.s.conflictCausedBy(ctx, "C-2", tt.commits); !equalStrings(got, tt.want) {
				t.Errorf("caused_by = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestConflictRetryNamesWhatItConflictsWith: the ci_conflict_retry a real
// conflicting PR records carries caused_by, the sibling task whose squash
// merge it conflicts with.
func TestConflictRetryNamesWhatItConflictsWith(t *testing.T) {
	f, _, _ := conflictFixture(t, true)
	if _, err := f.s.Queue.Reload([]model.Task{task("C-1", 1, "claude/opus"), task("C-2", 1, "claude/opus")}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.poller.Poll(context.Background(), f.s); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	extra := f.backend.eventExtra("C-2", EventCIConflictRetry)
	if got, _ := extra["caused_by"].([]string); !equalStrings(got, []string{"C-1"}) {
		t.Errorf("caused_by = %v, want [C-1] (extra %v)", extra["caused_by"], extra)
	}
}

// TestLocalLandConflictNamesWhatItConflictsWith: the same for a local land,
// whose base commits carry the landed task's title.
func TestLocalLandConflictNamesWhatItConflictsWith(t *testing.T) {
	f := newReapFixture(t,
		[]model.Task{task("C-0", 1, "claude/opus"), task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": okResponse()},
		SchedulerOptions{WorktreeMode: true, LocalMerge: true, ConflictRetries: 1})
	if err := f.s.Queue.MarkDone("C-0"); err != nil {
		t.Fatal(err)
	}
	f.s.Worktree = &recordingWorktree{dir: t.TempDir(),
		land: worktree.Landing{Conflicts: []string{"a.go"}, Commits: []string{"abc1234 C-0 title"}}}

	f.runOnce(t)

	extra := f.backend.eventExtra("C-1", EventCIConflictRetry)
	if got, _ := extra["caused_by"].([]string); !equalStrings(got, []string{"C-0"}) {
		t.Errorf("caused_by = %v, want [C-0] (extra %v)", extra["caused_by"], extra)
	}
}

// TestRadarReportReadsTheEngineReasons: `orch radar report` recognises a
// conflict that blocked by the start of the engine's reason; it must not
// drift from what the engine writes.
func TestRadarReportReadsTheEngineReasons(t *testing.T) {
	if got := conflictBlockReason; len(got) < len(project.RadarPRConflictReason) ||
		got[:len(project.RadarPRConflictReason)] != project.RadarPRConflictReason {
		t.Errorf("conflictBlockReason %q no longer starts with %q", got, project.RadarPRConflictReason)
	}
	if landConflictPrefix != project.RadarLandConflictReason {
		t.Errorf("the land conflict reason starts with %q, the report reads %q", landConflictPrefix, project.RadarLandConflictReason)
	}
}
