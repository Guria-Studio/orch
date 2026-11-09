package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// QuotaWindow is one window of a provider's subscription as a dispatch's
// outcome event recorded it (extra.quota): the shape providers.QuotaWindow
// marshals to, read back without importing the providers package.
type QuotaWindow struct {
	Window string `json:"window"`
	// Utilization is the used fraction, 0 to 1.
	Utilization float64 `json:"utilization"`
	// ResetsAt is when the window resets, Unix seconds; 0 when not given.
	ResetsAt int64 `json:"resets_at"`
}

// ProviderQuota is the newest report of one backend's subscription windows.
// It counts every use of the account, inside orch or not, and is only as
// fresh as the last dispatch to that backend: SeenAt.
type ProviderQuota struct {
	Windows []QuotaWindow
	SeenAt  time.Time
}

// quotaScan bounds how many quota-carrying rows LatestQuota looks through
// for one it can read; a row it cannot is skipped, not trusted.
const quotaScan = 20

// LatestQuota is the newest quota a backend reported in this project, from
// the extra.quota of its outcome events (#417, #577). ok is false when it
// never reported one.
func (b *SQLite) LatestQuota(ctx context.Context, backend string) (ProviderQuota, bool, error) {
	rows, err := b.db.read.QueryContext(ctx,
		`SELECT ts, extra_json FROM events
		  WHERE project_id = ? AND backend = ? AND extra_json LIKE '%"quota":%'
		  ORDER BY ts DESC, id DESC LIMIT ?`,
		b.projectID, backend, quotaScan)
	if err != nil {
		return ProviderQuota{}, false, fmt.Errorf("query quota of %s: %w", backend, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ts, extra string
		if err := rows.Scan(&ts, &extra); err != nil {
			return ProviderQuota{}, false, fmt.Errorf("scan quota row: %w", err)
		}
		var e struct {
			Quota []QuotaWindow `json:"quota"`
		}
		at, okTS := ParseTS(ts)
		if json.Unmarshal([]byte(extra), &e) != nil || len(e.Quota) == 0 || !okTS {
			continue
		}
		return ProviderQuota{Windows: e.Quota, SeenAt: at}, true, nil
	}
	if err := rows.Err(); err != nil {
		return ProviderQuota{}, false, fmt.Errorf("iterate quota rows: %w", err)
	}
	return ProviderQuota{}, false, nil
}
