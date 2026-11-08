package state

import (
	"context"
	"testing"
	"time"
)

// TestFindingQueue (#536): an agent's report waits in the queue until the
// operator sends or dismisses it; the queue keeps the decision and the URL.
func TestFindingQueue(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)
	at := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	first, err := b.QueueFinding(ctx, QueuedFinding{Title: "a", Body: "body a", Summary: "sa", TaskID: "T1", QueuedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.QueueFinding(ctx, QueuedFinding{Title: "b", Body: "body b", QueuedAt: at.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetFindingStatus(ctx, first, FindingSent, "https://github.com/o/r/issues/7"); err != nil {
		t.Fatal(err)
	}
	got, err := b.QueuedFindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != second || got[1].ID != first {
		t.Fatalf("queue = %+v, want both, newest first", got)
	}
	if got[0].Status != FindingQueued || got[1].Status != FindingSent || got[1].URL != "https://github.com/o/r/issues/7" {
		t.Errorf("statuses = %q %q (%q)", got[0].Status, got[1].Status, got[1].URL)
	}
	if got[1].Body != "body a" || got[1].TaskID != "T1" || !got[1].QueuedAt.Equal(at) {
		t.Errorf("row = %+v", got[1])
	}
	if err := b.SetFindingStatus(ctx, 999, FindingDismissed, ""); err == nil {
		t.Error("an unknown id was accepted")
	}
}
