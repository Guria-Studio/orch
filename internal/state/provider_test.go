package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// A provider the operator turned off stays off across opens until turned
// back on, with the reason it was given; one never touched is not listed.
func TestProviderStatesPersistAndReEnable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orch.db")
	db, _, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	b := NewSQLite(db, "demo", "")
	at := time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC)
	if err := b.SetProviderState(ctx, "codex", true, "quota spent", at); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := b.ProviderStates(ctx)
	if err != nil || len(got) != 1 || got[0].Backend != "codex" || !got[0].Disabled || got[0].Reason != "quota spent" || !got[0].UpdatedAt.Equal(at) {
		t.Fatalf("ProviderStates = %+v, %v; want codex disabled with its reason", got, err)
	}
	if err := b.SetProviderState(ctx, "codex", false, "", at.Add(time.Hour)); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if got, _ := b.ProviderStates(ctx); len(got) != 1 || got[0].Disabled || got[0].Reason != "" {
		t.Errorf("after enabling: %+v, want codex enabled with no reason", got)
	}
	if other, _ := NewSQLite(db, "other", "").ProviderStates(ctx); len(other) != 0 {
		t.Errorf("another project sees %+v, want nothing", other)
	}
}
