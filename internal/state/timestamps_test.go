package state

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
)

var canonicalRe = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$`)

// The spellings Python and older orch binaries left in an orch.db, each with
// the instant it means. Text order of the left column is 08:30, 08:45, 09:00,
// 10:00+02:00; time order is 08:00, 08:30, 08:45, 09:00.
var mixedSpellings = []struct{ raw, want string }{
	{"2026-09-04T10:00:00+02:00", "2026-09-04T08:00:00Z"},
	{"2026-09-04T09:00:00Z", "2026-09-04T09:00:00Z"},
	{"2026-09-04T08:30:00.123456+00:00", "2026-09-04T08:30:00Z"},
	{"2026-09-04T08:45:00", "2026-09-04T08:45:00Z"},
}

var wantOrder = []string{
	"2026-09-04T08:00:00Z", "2026-09-04T08:30:00Z",
	"2026-09-04T08:45:00Z", "2026-09-04T09:00:00Z",
}

// Migration 015 rewrites every timestamp column to one spelling: UTC, second
// precision, `Z`. The Python fixture holds `+00:00` and `Z` side by side; the
// rows inserted raw add an offset, fractional seconds and a naive time.
func TestMigrationNormalizesEveryTimestamp(t *testing.T) {
	ctx := context.Background()
	path := copyPythonFixture(t)

	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	for _, s := range mixedSpellings {
		if _, err := raw.ExecContext(ctx,
			`INSERT INTO events (project_id, run_id, event_type, task_id, ts)
			 VALUES ('billing-api', 'r', 'dispatch', 'MIX', ?)`, s.raw); err != nil {
			t.Fatalf("insert %q: %v", s.raw, err)
		}
	}
	_ = raw.Close()

	db, _, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cols := map[string][]string{
		"projects":         {"created_at", "migrated_at"},
		"tasks_runtime":    {"started_at", "finished_at", "updated_at"},
		"runs":             {"started_at", "updated_at"},
		"dispatches":       {"started_at"},
		"events":           {"ts"},
		"spend":            {"ts"},
		"findings":         {"created_at"},
		"tasks_definition": {"updated_at"},
		"milestones":       {"created_at"},
	}
	checked := 0
	for table, cs := range cols {
		for _, c := range cs {
			// #nosec G202 -- table and column come from the fixed map above.
			rows, err := db.read.QueryContext(ctx,
				"SELECT "+c+" FROM "+table+" WHERE COALESCE("+c+", '') != ''")
			if err != nil {
				t.Fatalf("query %s.%s: %v", table, c, err)
			}
			for rows.Next() {
				var v string
				if err := rows.Scan(&v); err != nil {
					t.Fatalf("scan: %v", err)
				}
				checked++
				if !canonicalRe.MatchString(v) {
					t.Errorf("%s.%s = %q, not canonical", table, c, v)
				}
			}
			_ = rows.Close()
		}
	}
	if checked < 20 {
		t.Errorf("checked only %d timestamps; the fixture holds more", checked)
	}

	b := NewSQLite(db, "billing-api", t.TempDir())
	all, err := b.AllEvents(ctx, 0)
	if err != nil {
		t.Fatalf("AllEvents: %v", err)
	}
	var got []string
	for _, e := range all {
		if e.TaskID == "MIX" {
			got = append(got, e.TS)
		}
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("got %d MIX events, want %d", len(got), len(wantOrder))
	}
	for i := range got {
		if got[i] != wantOrder[i] {
			t.Errorf("event %d = %q, want %q (all: %q)", i, got[i], wantOrder[i], got)
		}
	}
}

// An older orch still running against an upgraded database can write a
// non-canonical row after 015 ran. AllEvents orders by time, not by text, and
// AppendEvent stores the canonical spelling whatever it is handed.
func TestAllEventsOrdersByTimeNotText(t *testing.T) {
	ctx := context.Background()
	b := seeded(t, "T1")

	// The first row goes in raw, as an older binary would write it.
	if _, err := b.db.write.ExecContext(ctx,
		`INSERT INTO events (project_id, run_id, event_type, task_id, ts)
		 VALUES (?, 'r', 'dispatch', 'T1', ?)`, b.projectID, mixedSpellings[0].raw); err != nil {
		t.Fatalf("raw insert: %v", err)
	}
	for i, s := range mixedSpellings[1:] {
		if err := b.AppendEvent(ctx, "r", Event{
			EventType: "dispatch", TaskID: "T1", TS: s.raw, Extra: map[string]any{"pid": i},
		}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	all, err := b.AllEvents(ctx, 0)
	if err != nil {
		t.Fatalf("AllEvents: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("got %d events, want 4", len(all))
	}
	if all[0].TS != mixedSpellings[0].raw {
		t.Errorf("first event = %q, want the raw %q first", all[0].TS, mixedSpellings[0].raw)
	}
	for i, e := range all[1:] {
		if e.TS != wantOrder[i+1] {
			t.Errorf("event %d = %q, want %q", i+1, e.TS, wantOrder[i+1])
		}
	}

	tail, err := b.AllEvents(ctx, 2)
	if err != nil {
		t.Fatalf("AllEvents tail: %v", err)
	}
	if len(tail) != 2 || tail[0].TS != wantOrder[2] || tail[1].TS != wantOrder[3] {
		t.Errorf("tail = %+v, want the 08:45 and 09:00 events", tail)
	}
}
