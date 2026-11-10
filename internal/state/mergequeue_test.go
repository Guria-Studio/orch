package state

import (
	"context"
	"testing"
	"time"
)

// TestMergeQueueRows (#522, migration 017): a task enters once while queued,
// leaves as merged or ejected (an ejection counts a requeue), and enters
// again at the back; rows come back oldest first.
func TestMergeQueueRows(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	for _, step := range []struct {
		id   string
		at   time.Time
		want bool
	}{
		{"T-2", t0, true},
		{"T-1", t0.Add(time.Minute), true},
		{"T-2", t0.Add(2 * time.Minute), false}, // already queued: keeps its place
	} {
		got, err := b.Enqueue(ctx, step.id, "https://example.test/pr/"+step.id, step.at)
		if err != nil || got != step.want {
			t.Fatalf("Enqueue(%s) = %v, %v; want %v", step.id, got, err, step.want)
		}
	}
	entries, err := b.QueueEntries(ctx)
	if err != nil || len(entries) != 2 || entries[0].TaskID != "T-2" || !entries[0].EnqueuedAt.Equal(t0) {
		t.Fatalf("entries = %+v, %v; want T-2 first, entered at %s", entries, err, t0)
	}

	if err := b.SetQueueState(ctx, "T-2", QueueEjected, "sent back to its agent", t0.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := b.SetQueueState(ctx, "T-1", QueueMerged, "", t0.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if again, err := b.Enqueue(ctx, "T-2", "https://example.test/pr/T-2", t0.Add(4*time.Minute)); err != nil || !again {
		t.Fatalf("re-entering an ejected task = %v, %v; want it back", again, err)
	}
	entries, _ = b.QueueEntries(ctx)
	got := map[string]QueueEntry{}
	for _, e := range entries {
		got[e.TaskID] = e
	}
	if e := got["T-2"]; e.State != QueueQueued || e.Requeues != 1 || e.Reason != "" || !e.EnqueuedAt.Equal(t0.Add(4*time.Minute)) {
		t.Errorf("T-2 = %+v, want queued again at the back with one requeue", e)
	}
	if e := got["T-1"]; e.State != QueueMerged || e.Requeues != 0 {
		t.Errorf("T-1 = %+v, want merged", e)
	}
	if entries[0].TaskID != "T-1" {
		t.Errorf("order = %+v, want T-1 (entered earlier) first now", entries)
	}
}
