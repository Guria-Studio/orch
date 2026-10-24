package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// FiledFinding is an issue orch_report_finding filed for this project.
type FiledFinding struct {
	Number  int
	URL     string
	Title   string
	Summary string
	FiledAt time.Time
}

// FiledFindings lists the issues this project's agents filed, newest first.
func (b *SQLite) FiledFindings(ctx context.Context) ([]FiledFinding, error) {
	rows, err := b.db.read.QueryContext(ctx,
		`SELECT issue_number, url, title, summary, filed_at FROM filed_findings WHERE project_id = ? ORDER BY issue_number DESC`,
		b.projectID)
	if err != nil {
		return nil, fmt.Errorf("query filed findings for %q: %w", b.projectID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []FiledFinding
	for rows.Next() {
		var f FiledFinding
		var at string
		if err := rows.Scan(&f.Number, &f.URL, &f.Title, &f.Summary, &at); err != nil {
			return nil, fmt.Errorf("scan filed finding: %w", err)
		}
		f.FiledAt, _ = ParseTS(at)
		out = append(out, f)
	}
	return out, rows.Err()
}

// RecordFiledFinding remembers an issue orch_report_finding filed.
func (b *SQLite) RecordFiledFinding(ctx context.Context, f FiledFinding) error {
	return b.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO filed_findings (project_id, issue_number, url, title, summary, filed_at) VALUES (?, ?, ?, ?, ?, ?)`,
			b.projectID, f.Number, f.URL, f.Title, f.Summary, b.ts(f.FiledAt)); err != nil {
			return fmt.Errorf("record filed finding #%d for %q: %w", f.Number, b.projectID, err)
		}
		return nil
	})
}
