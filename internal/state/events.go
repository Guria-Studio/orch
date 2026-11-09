package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

// AllEvents returns events across every task, oldest first. n <= 0 means all;
// n > 0 returns the TAIL — the newest n, still in chronological order.
//
// `Events` answers "what happened to this task"; this answers "what happened",
// which is the question a log view and any per-task aggregate both start from.
// Doing it as one query rather than a loop over `Events` matters at both ends:
// a project with 400 tasks is 400 round trips, and a log view that fetched
// per task could not order the result without reading everything anyway.
//
// # Ordering
//
// By time, then by `id`. Python sorts its merged JSONL + SQLite rows with
// `sort(key=ts)`, which is stable, so rows sharing a timestamp keep the order
// they were read in — insertion order, which is `id`. Two events inside the
// same second are not rare: a dispatch and its block land together all the
// time. Ordering by `id` alone would be right for one project and wrong for
// one that migrated from JSONL, where ids were assigned at import.
//
// Sorted in Go on the parsed time, not with `ORDER BY ts`: migration 015 made
// every stored spelling canonical, but an older orch still running against
// the same file can write `+00:00` after it, and as text `...09:00:00Z` sorts
// after `...10:00:00+02:00`. The tail is cut after the sort, so it reads every
// row; a project's events are thousands, not millions.
func (b *SQLite) AllEvents(ctx context.Context, n int) ([]Event, error) {
	rows, err := b.db.read.QueryContext(ctx,
		`SELECT id, run_id, event_type, task_id, COALESCE(backend, ''), ts,
		        COALESCE(extra_json, '{}')
		   FROM events
		  WHERE project_id = ?
		  ORDER BY id`, b.projectID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Event
	for rows.Next() {
		var (
			e     Event
			extra string
		)
		if err := rows.Scan(&e.ID, &e.RunID, &e.EventType, &e.TaskID,
			&e.Backend, &e.TS, &extra); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		e.Extra = decodeExtra(extra)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	// Stable over id order, so equal times keep insertion order. An
	// unreadable ts parses as the zero time and sorts first, as '' did in SQL.
	slices.SortStableFunc(out, func(a, b Event) int {
		ta, _ := ParseTS(a.TS)
		tb, _ := ParseTS(b.TS)
		return ta.Compare(tb)
	})
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

// FirstEventBackend reports whether the project has any event of eventType and
// the backend of the earliest one, by id: what the onboarding progress reads
// (#378) — "has this project ever dispatched", and with which CLI.
func (b *SQLite) FirstEventBackend(ctx context.Context, eventType string) (backend string, ok bool, err error) {
	err = b.db.read.QueryRowContext(ctx,
		`SELECT COALESCE(backend, '') FROM events
		  WHERE project_id = ? AND event_type = ?
		  ORDER BY id LIMIT 1`, b.projectID, eventType).Scan(&backend)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query the first %s event: %w", eventType, err)
	}
	return backend, true, nil
}
