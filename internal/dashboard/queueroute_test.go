package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/project"
)

// queueState is a fakeState that also keeps a merge queue.
type queueState struct {
	*fakeState
	entries []project.QueueEntry
}

func (q queueState) QueueEntries(context.Context) ([]project.QueueEntry, error) {
	return append([]project.QueueEntry(nil), q.entries...), nil
}

// TestCIShowsTheMergeQueue (#522 phase 5): /api/ci carries the queue's
// settings and its entries — the queued ones in the order the engine merges
// them (phase, then dependents, then when they entered), numbered, then the
// ones that left it, newest first.
func TestCIShowsTheMergeQueue(t *testing.T) {
	tasksJSON := `{"meta":{},"phases":[],"tasks":[
 {"id":"A","phase":2,"title":"a","model":"m","dependencies":[]},
 {"id":"B","phase":1,"title":"b","model":"m","dependencies":[]},
 {"id":"C","phase":1,"title":"c","model":"m","dependencies":[]},
 {"id":"D","phase":3,"title":"d","model":"m","dependencies":["C"]}]}`
	root := writeProject(t, "github:\n  auto_merge: true\nmerge_queue:\n  enabled: true\n", tasksJSON)
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o750); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	st := queueState{fakeState: &fakeState{}, entries: []project.QueueEntry{
		{TaskID: "A", State: project.QueueQueued, EnqueuedAt: t0, UpdatedAt: t0},
		{TaskID: "B", State: project.QueueQueued, EnqueuedAt: t0, UpdatedAt: t0},
		{TaskID: "C", State: project.QueueQueued, EnqueuedAt: t0.Add(time.Minute), UpdatedAt: t0},
		{TaskID: "X", State: project.QueueMerged, EnqueuedAt: t0, UpdatedAt: t0.Add(-time.Hour)},
		{TaskID: "Y", State: project.QueueEjected, Reason: "sent back to its agent", Requeues: 1, EnqueuedAt: t0, UpdatedAt: t0.Add(-time.Minute)},
	}}
	s, err := New(cfg(ProfileOperator, ""), Options{
		Static: spaHandler(t),
		State:  st,
		Paths:  config.Paths{Root: root, ID: "demo", ConfigYAML: filepath.Join(root, "config.yaml")},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var got ciPayload
	decode(t, get(t, s, "/api/ci"), &got)
	q := got.MergeQueue
	if !q.Enabled || q.Reverify != "when_base_moved" {
		t.Errorf("merge_queue = %+v, want enabled, when_base_moved", q)
	}
	var order []string
	for _, e := range q.Entries {
		order = append(order, e.TaskID+":"+e.State)
	}
	want := "C:queued B:queued A:queued Y:ejected X:merged"
	if strings.Join(order, " ") != want {
		t.Errorf("entries = %v, want %s", order, want)
	}
	if q.Entries[0].Position != 1 || q.Entries[2].Position != 3 || q.Entries[3].Position != 0 || q.Entries[3].Reason == "" {
		t.Errorf("entries = %+v, want positions 1..3 for the queued, none for the rest", q.Entries)
	}
}

// TestCIWarnsAQueueWithoutAutoMerge: the queue only orders orch's own merges.
func TestCIWarnsAQueueWithoutAutoMerge(t *testing.T) {
	s := ciServer(t, "vcs:\n  auto_pr: true\n  worktree_mode: true\nmerge_queue:\n  enabled: true\n", map[string]string{"orch-ci.yml": orchCIWorkflow})
	var got ciPayload
	decode(t, get(t, s, "/api/ci"), &got)
	found := false
	for _, w := range got.Warnings {
		found = found || strings.Contains(w, "merge_queue.enabled")
	}
	if !found || got.MergeQueue.Entries == nil {
		t.Errorf("warnings = %v, entries = %v; want the merge_queue warning and an empty list", got.Warnings, got.MergeQueue.Entries)
	}
}
