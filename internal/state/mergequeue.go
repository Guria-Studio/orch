package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Merge queue states (migration 017, #522).
const (
	QueueQueued  = "queued"
	QueueMerged  = "merged"
	QueueEjected = "ejected"
)

// QueueEntry is one task's place in the merge queue.
type QueueEntry struct {
	TaskID     string    `json:"task_id"`
	PRURL      string    `json:"pr_url"`
	EnqueuedAt time.Time `json:"enqueued_at"`
	State      string    `json:"state"`
	Requeues   int       `json:"requeues"`
	Reason     string    `json:"reason"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// QueueEntries lists this project's merge queue rows, oldest first.
func (b *SQLite) QueueEntries(ctx context.Context) ([]QueueEntry, error) {
	rows, err := b.db.read.QueryContext(ctx,
		`SELECT task_id, pr_url, enqueued_at, state, requeues, reason, updated_at
		 FROM merge_queue WHERE project_id = ? ORDER BY enqueued_at, task_id`, b.projectID)
	if err != nil {
		return nil, fmt.Errorf("query the merge queue for %q: %w", b.projectID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []QueueEntry
	for rows.Next() {
		var e QueueEntry
		var enq, upd string
		if err := rows.Scan(&e.TaskID, &e.PRURL, &enq, &e.State, &e.Requeues, &e.Reason, &upd); err != nil {
			return nil, fmt.Errorf("scan a merge queue row: %w", err)
		}
		e.EnqueuedAt, _ = ParseTS(enq)
		e.UpdatedAt, _ = ParseTS(upd)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Enqueue puts taskID in the merge queue at `at`, unless it is already
// queued; an entry merged or ejected before enters again, at the back.
// Reports whether it entered now.
func (b *SQLite) Enqueue(ctx context.Context, taskID, prURL string, at time.Time) (bool, error) {
	entered := false
	err := b.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO merge_queue (project_id, task_id, pr_url, enqueued_at, state, updated_at)
			 VALUES (?, ?, ?, ?, 'queued', ?)
			 ON CONFLICT(project_id, task_id) DO UPDATE
			   SET pr_url = excluded.pr_url, enqueued_at = excluded.enqueued_at, state = 'queued',
			       reason = '', updated_at = excluded.updated_at
			   WHERE merge_queue.state <> 'queued'`,
			b.projectID, taskID, prURL, b.ts(at), b.ts(at))
		if err != nil {
			return fmt.Errorf("enqueue %q/%s: %w", b.projectID, taskID, err)
		}
		n, err := res.RowsAffected()
		entered = err == nil && n > 0
		return err
	})
	return entered, err
}

// SetQueueState moves taskID's queue entry to state (merged or ejected),
// with the reason; ejected also counts a requeue.
func (b *SQLite) SetQueueState(ctx context.Context, taskID, state, reason string, at time.Time) error {
	return b.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE merge_queue SET state = ?, reason = ?, updated_at = ?,
			   requeues = requeues + CASE WHEN ? = 'ejected' THEN 1 ELSE 0 END
			 WHERE project_id = ? AND task_id = ?`,
			state, reason, b.ts(at), state, b.projectID, taskID); err != nil {
			return fmt.Errorf("set the queue state of %q/%s: %w", b.projectID, taskID, err)
		}
		return nil
	})
}
