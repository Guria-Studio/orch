package state

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// The newest outcome event carrying a quota is the backend's report; events
// without one, other backends' and other projects' do not count (#577).
func TestLatestQuotaIsTheNewestReport(t *testing.T) {
	ctx := context.Background()
	db, _, err := Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	b := NewSQLite(db, "demo", "")

	if _, ok, err := b.LatestQuota(ctx, "codex"); err != nil || ok {
		t.Fatalf("empty log: ok = %v, err = %v; want no report", ok, err)
	}

	window := func(used float64, resets int64) []map[string]any {
		return []map[string]any{{"window": "five_hour", "utilization": used, "resets_at": resets}}
	}
	for _, e := range []Event{
		{EventType: "success", TaskID: "T1", Backend: "codex", TS: "2026-10-23T00:00:00Z", Extra: map[string]any{"quota": window(0.10, 100)}},
		{EventType: "success", TaskID: "T2", Backend: "codex", TS: "2026-10-23T01:00:00Z", Extra: map[string]any{"quota": window(0.40, 200)}},
		// Newer, but says nothing about the quota.
		{EventType: "fail", TaskID: "T3", Backend: "codex", TS: "2026-10-23T02:00:00Z", Extra: map[string]any{"reason": "x"}},
		// Newer, another backend.
		{EventType: "success", TaskID: "T4", Backend: "claude", TS: "2026-10-23T03:00:00Z", Extra: map[string]any{"quota": window(0.90, 300)}},
	} {
		if err := b.AppendEvent(ctx, "run-1", e); err != nil {
			t.Fatal(err)
		}
	}
	if err := NewSQLite(db, "other", "").AppendEvent(ctx, "run-9", Event{EventType: "success", TaskID: "T9", Backend: "codex", TS: "2026-10-23T04:00:00Z", Extra: map[string]any{"quota": window(0.99, 400)}}); err != nil {
		t.Fatal(err)
	}

	got, ok, err := b.LatestQuota(ctx, "codex")
	if err != nil || !ok {
		t.Fatalf("LatestQuota: ok = %v, err = %v", ok, err)
	}
	want := ProviderQuota{
		Windows: []QuotaWindow{{Window: "five_hour", Utilization: 0.40, ResetsAt: 200}},
		SeenAt:  time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LatestQuota = %+v, want %+v", got, want)
	}
}
