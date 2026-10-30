package engine

import (
	"context"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

func TestEntriesOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"src/a.go", "src/a.go", true},
		{"src/a.go", "src/b.go", false},
		{"src/", "src/a.go", true},
		{"src/a.go", "src/", true},
		{"src/", "docs/", false},
		{"src/", "src/x/", true},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/a.ts", false},
		{"src/*.go", "docs/a.go", false},
		{"src/x/*.go", "src/", true},
		{"src/*.go", "src/x/", true}, // the approximation: same literal dir
		{"src/*.go", "lib/*.go", false},
		{"src/*.go", "src/*_test.go", true},
	}
	for _, c := range cases {
		if got := entriesOverlap(c.a, c.b); got != c.want {
			t.Errorf("entriesOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// radarFixture has B ready while A, whose files overlap B's, runs.
func radarFixture(t *testing.T, mode string) *reapFixture {
	t.Helper()
	b := task("B", 1, "claude/opus")
	b.Files = []string{"src/handlers/webhook.ts", "README.md"}
	f := newReapFixture(t, []model.Task{b}, map[string]fakeResponse{"B": okResponse()}, SchedulerOptions{})
	f.s.Opts.Cfg.Radar = mode
	a := task("A", 1, "claude/opus")
	a.Files = []string{"src/handlers/"}
	f.s.inFlight[4242] = &InFlight{Task: a, Route: model.RouteEntry{Backend: model.BackendClaude}}
	return f
}

// TestRadarSerializeHoldsAnOverlappingTask pins #482: with radar: serialize a
// task whose files overlap a running one waits, says so once, and goes once
// the other is no longer running.
func TestRadarSerializeHoldsAnOverlappingTask(t *testing.T) {
	f := radarFixture(t, "serialize")
	ctx := context.Background()
	for range 2 {
		if n, err := f.s.Refill(ctx); err != nil || n != 0 {
			t.Fatalf("Refill = %d, %v; want B held", n, err)
		}
	}
	if got := f.s.DeferReasons()["B"]; got != "files-overlap:A" {
		t.Errorf("defer reason = %q, want files-overlap:A", got)
	}
	if got := f.backend.eventTypes("B"); len(got) != 1 || got[0] != EventFilesOverlap {
		t.Fatalf("events = %v, want one files_overlap over two ticks", got)
	}
	extra := f.backend.eventExtra("B", EventFilesOverlap)
	if files, _ := extra["files"].([]string); extra["with"] != "A" || len(files) != 1 || files[0] != "src/handlers/webhook.ts" {
		t.Errorf("extra = %v, want with A on src/handlers/webhook.ts", extra)
	}

	delete(f.s.inFlight, 4242)
	f.runOnce(t)
	if got, _ := f.s.Queue.Status("B"); got != model.StatusDone {
		t.Errorf("B = %q once A finished, want it dispatched and done", got)
	}
}

// TestRadarWarnDispatchesAndRecords: warn, the default, only reports.
func TestRadarWarnDispatchesAndRecords(t *testing.T) {
	f := radarFixture(t, "warn")
	if n, err := f.s.Refill(context.Background()); err != nil || n != 1 {
		t.Fatalf("Refill = %d, %v; want B dispatched", n, err)
	}
	delete(f.s.inFlight, 4242)
	if _, err := f.s.DrainWait(context.Background(), 30*time.Second); err != nil {
		t.Fatalf("DrainWait: %v", err)
	}
	if got := f.backend.eventTypes("B"); !contains(got, EventFilesOverlap) || !contains(got, EventDispatch) {
		t.Errorf("events = %v, want files_overlap and dispatch", got)
	}
}

// TestRadarOffSaysNothing.
func TestRadarOffSaysNothing(t *testing.T) {
	f := radarFixture(t, "off")
	if n, err := f.s.Refill(context.Background()); err != nil || n != 1 {
		t.Fatalf("Refill = %d, %v; want B dispatched", n, err)
	}
	delete(f.s.inFlight, 4242)
	if _, err := f.s.DrainWait(context.Background(), 30*time.Second); err != nil {
		t.Fatalf("DrainWait: %v", err)
	}
	if got := f.backend.eventTypes("B"); contains(got, EventFilesOverlap) {
		t.Errorf("events = %v, want no files_overlap with radar off", got)
	}
}

type fakeOpenPRs []state.TaskRuntime

func (o fakeOpenPRs) TasksWithPendingCI(context.Context) ([]state.TaskRuntime, error) { return o, nil }

// TestRadarFlagsAPRThatConflictsWithAnotherOpenOne pins #482's second half:
// the PR just opened is merged against the other open task PRs, and each one
// it conflicts with is recorded with the files.
func TestRadarFlagsAPRThatConflictsWithAnotherOpenOne(t *testing.T) {
	f, wt, _, _ := prFixture(t)
	f.s.Opts.Cfg.Radar = "warn"
	wt.branchConflicts = map[string][]string{"C-9": {"src/webhook.ts"}}
	f.s.OpenPRs = fakeOpenPRs{
		{ID: "C-1", PRURL: "https://github.com/o/r/pull/7"},
		{ID: "C-8", PRURL: "https://github.com/o/r/pull/5"},
		{ID: "C-9", PRURL: "https://github.com/o/r/pull/6"},
	}

	f.runOnce(t)

	var conflicts int
	for _, e := range f.backend.eventTypes("C-1") {
		if e == EventBranchConflict {
			conflicts++
		}
	}
	if conflicts != 1 {
		t.Fatalf("events = %v, want one branch_conflict (C-9; C-8 merges cleanly)", f.backend.eventTypes("C-1"))
	}
	extra := f.backend.eventExtra("C-1", EventBranchConflict)
	if files, _ := extra["files"].([]string); extra["with"] != "C-9" || extra["pr_url"] != "https://github.com/o/r/pull/6" ||
		len(files) != 1 || files[0] != "src/webhook.ts" {
		t.Errorf("extra = %v, want C-9's PR on src/webhook.ts", extra)
	}
}
