package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Attempt is one finished attempt at a task (#538): how it ended, what it
// cost, and what the local verify, its review and CI said about it.
type Attempt struct {
	ID      int64
	RunID   string
	TaskID  string
	Attempt int
	// Backend, Model and Tier are the route it ran on.
	Backend, Model, Tier string
	// Result is the dispatch's outcome: success, fail, timeout or stopped.
	Result       string
	FailureClass string
	Reason       string
	TokensIn     int
	TokensOut    int
	CostUSD      float64
	DurationS    float64
	// Verify is the local verify's verdict: "" when it did not run, pass or
	// fail. Review is the reviewer's: "" when none completed, pass or blocks.
	Verify, Review, Reviewer string
	Findings                 []AttemptFinding
	// CI is how the PR's CI went, on the attempt that pushed: "", success or
	// failure.
	CI         string
	FinishedAt string
}

// AttemptFinding is one review finding, as engine.VerifyFinding has it minus
// the detail.
type AttemptFinding struct {
	Severity string `json:"severity"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Title    string `json:"title"`
}

// RecordAttempt appends one finished attempt.
func (b *SQLite) RecordAttempt(ctx context.Context, a Attempt) error {
	findings := a.Findings
	if findings == nil {
		findings = []AttemptFinding{}
	}
	fj, err := json.Marshal(findings)
	if err != nil {
		return fmt.Errorf("encode findings of %q: %w", a.TaskID, err)
	}
	_, err = b.db.write.ExecContext(ctx,
		`INSERT INTO attempts
		   (project_id, run_id, task_id, attempt, backend, model, tier, result,
		    failure_class, reason, tokens_in, tokens_out, cost_usd, duration_s,
		    verify, review, reviewer, findings_json, finished_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.projectID, a.RunID, a.TaskID, a.Attempt, a.Backend, a.Model, a.Tier, a.Result,
		a.FailureClass, a.Reason, a.TokensIn, a.TokensOut, a.CostUSD, a.DurationS,
		a.Verify, a.Review, a.Reviewer, string(fj), a.FinishedAt)
	if err != nil {
		return fmt.Errorf("record attempt of %q: %w", a.TaskID, err)
	}
	return nil
}

// Attempts returns every attempt of this project, oldest first.
func (b *SQLite) Attempts(ctx context.Context) ([]Attempt, error) {
	rows, err := b.db.read.QueryContext(ctx,
		`SELECT id, run_id, task_id, attempt, backend, model, tier, result,
		        failure_class, reason, tokens_in, tokens_out, cost_usd, duration_s,
		        verify, review, reviewer, findings_json, ci, finished_at
		   FROM attempts WHERE project_id = ? ORDER BY id`, b.projectID)
	if err != nil {
		return nil, fmt.Errorf("query attempts for %q: %w", b.projectID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []Attempt
	for rows.Next() {
		var a Attempt
		var fj string
		if err := rows.Scan(&a.ID, &a.RunID, &a.TaskID, &a.Attempt, &a.Backend, &a.Model, &a.Tier, &a.Result,
			&a.FailureClass, &a.Reason, &a.TokensIn, &a.TokensOut, &a.CostUSD, &a.DurationS,
			&a.Verify, &a.Review, &a.Reviewer, &fj, &a.CI, &a.FinishedAt); err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}
		if err := json.Unmarshal([]byte(fj), &a.Findings); err != nil {
			return nil, fmt.Errorf("attempt %d: unreadable findings: %w", a.ID, err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// markLatestAttemptCI records a CI verdict on the task's latest attempt: the
// one that pushed to the PR whose CI just finished.
func markLatestAttemptCI(ctx context.Context, tx *sql.Tx, projectID, taskID, ci string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE attempts SET ci = ?
		  WHERE id = (SELECT MAX(id) FROM attempts WHERE project_id = ? AND task_id = ?)`,
		ci, projectID, taskID)
	if err != nil {
		return fmt.Errorf("record CI %s on %q's latest attempt: %w", ci, taskID, err)
	}
	return nil
}
