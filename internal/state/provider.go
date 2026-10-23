package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ProviderState is one backend the operator turned off or back on. A
// backend with no row is enabled.
type ProviderState struct {
	Backend   string
	Disabled  bool
	Reason    string
	UpdatedAt time.Time
}

// ProviderStates lists this project's provider rows, by backend name.
func (b *SQLite) ProviderStates(ctx context.Context) ([]ProviderState, error) {
	rows, err := b.db.read.QueryContext(ctx,
		`SELECT backend, disabled, reason, updated_at FROM provider_state WHERE project_id = ? ORDER BY backend`,
		b.projectID)
	if err != nil {
		return nil, fmt.Errorf("query provider state for %q: %w", b.projectID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []ProviderState
	for rows.Next() {
		var p ProviderState
		var at string
		if err := rows.Scan(&p.Backend, &p.Disabled, &p.Reason, &at); err != nil {
			return nil, fmt.Errorf("scan provider state: %w", err)
		}
		p.UpdatedAt, _ = ParseTS(at)
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetProviderState turns backend off (with the reason the operator gave) or
// back on for this project.
func (b *SQLite) SetProviderState(ctx context.Context, backend string, disabled bool, reason string, at time.Time) error {
	return b.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO provider_state (project_id, backend, disabled, reason, updated_at)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(project_id, backend) DO UPDATE
			   SET disabled = excluded.disabled, reason = excluded.reason, updated_at = excluded.updated_at`,
			b.projectID, backend, disabled, reason, b.ts(at)); err != nil {
			return fmt.Errorf("set provider state for %q/%s: %w", b.projectID, backend, err)
		}
		return nil
	})
}
