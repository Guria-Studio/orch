package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// The statuses of a queued finding.
const (
	FindingQueued    = "queued"
	FindingSent      = "sent"
	FindingDismissed = "dismissed"
)

// QueuedFinding is an agent's report about orch waiting for the operator
// (#536). Title and Body are redacted and are exactly what sending files.
type QueuedFinding struct {
	ID       int64
	Title    string
	Body     string
	Summary  string
	TaskID   string
	QueuedAt time.Time
	Status   string
	URL      string
}

// QueueFinding stores a report for the operator to review and returns its id.
func (b *SQLite) QueueFinding(ctx context.Context, f QueuedFinding) (int64, error) {
	var id int64
	err := b.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO finding_queue (project_id, title, body, summary, task_id, queued_at, status) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			b.projectID, f.Title, f.Body, f.Summary, f.TaskID, b.ts(f.QueuedAt), FindingQueued)
		if err != nil {
			return fmt.Errorf("queue finding for %q: %w", b.projectID, err)
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// QueuedFindings lists this project's queued reports in every status, newest
// first.
func (b *SQLite) QueuedFindings(ctx context.Context) ([]QueuedFinding, error) {
	rows, err := b.db.read.QueryContext(ctx,
		`SELECT id, title, body, summary, task_id, queued_at, status, url FROM finding_queue WHERE project_id = ? ORDER BY id DESC`,
		b.projectID)
	if err != nil {
		return nil, fmt.Errorf("query queued findings for %q: %w", b.projectID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []QueuedFinding
	for rows.Next() {
		var f QueuedFinding
		var at string
		if err := rows.Scan(&f.ID, &f.Title, &f.Body, &f.Summary, &f.TaskID, &at, &f.Status, &f.URL); err != nil {
			return nil, fmt.Errorf("scan queued finding: %w", err)
		}
		var ok bool
		if f.QueuedAt, ok = ParseTS(at); !ok {
			return nil, fmt.Errorf("queued finding %d: unreadable queued_at %q", f.ID, at)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetFindingStatus records the operator's decision on a queued report: sent
// (with the issue's URL) or dismissed.
func (b *SQLite) SetFindingStatus(ctx context.Context, id int64, status, url string) error {
	return b.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE finding_queue SET status = ?, url = ? WHERE project_id = ? AND id = ?`,
			status, url, b.projectID, id)
		if err != nil {
			return fmt.Errorf("set finding %d to %s: %w", id, status, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("no queued finding %d in project %q", id, b.projectID)
		}
		return nil
	})
}
