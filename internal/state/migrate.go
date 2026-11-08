package state

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// Migrations 001-005 are byte-for-byte copies of
// `orchestrator/state/sqlite_migrations/` (ADR-G3), written while that tree
// was still live: an orch.db written by the Python implementation had to
// open here with nothing pending, and one written here had to open there.
// The Python line froze at v0.11.0-py (ADR-G0) before 006 existed, so that
// mirror ends at 005 — 006 onward is Go-only, with no Python counterpart to
// keep in step, and TestEmbeddedMigrationsMatchThePythonTree only compares
// the five that predate the freeze. Either way these files are append-only:
// superseding a migration means adding the next number, never editing an
// earlier one.
//
// 004's `milestones` table and `tasks_definition.milestone_id` column are
// unused: nothing ever wrote them, and every "milestone" orch shows is a
// phase (snapshot.PhaseMilestones). They stay because a migration is never
// edited, and a database Python wrote may still carry rows in them.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads the embedded files and orders them by the numeric
// prefix. A file that does not start with digits is a packaging mistake and
// is reported rather than skipped: silently ignoring a migration is how a
// schema drifts.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %q has no NNN_ prefix", e.Name())
		}
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %q has a non-numeric prefix: %w", e.Name(), err)
		}
		body, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", e.Name(), err)
		}
		out = append(out, migration{version: v, name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })

	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf(
				"migrations are not a contiguous 1..N run: expected %03d, found %s",
				i+1, m.name)
		}
	}
	return out, nil
}

// schemaVersion reads `PRAGMA user_version`, the same gate Python uses
// (`sqlite_backend._init_schema`). There is no separate migrations table.
func schemaVersion(ctx context.Context, q queryer) (int, error) {
	var v int
	if err := q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return v, nil
}

// migrate applies every migration above the database's current user_version,
// in order, and returns how many ran. Zero means the database was already
// current — which is what opening the Python-written fixture must report.
//
// Each .sql file ends with its own `PRAGMA user_version = N`, so the version
// advances as a side effect of the file rather than from a separate write.
// The final defensive set mirrors Python: it also repairs a database whose
// files were applied by hand without the pragma.
func migrate(ctx context.Context, db *sql.DB) (applied int, err error) {
	migrations, err := loadMigrations()
	if err != nil {
		return 0, err
	}
	return applyMigrations(ctx, db, migrations)
}

// applyMigrations runs the given set; migrate passes the embedded one, and a
// test passes one with a deliberately broken file.
func applyMigrations(ctx context.Context, db *sql.DB, migrations []migration) (applied int, err error) {
	current, err := schemaVersion(ctx, db)
	if err != nil {
		return 0, err
	}
	if len(migrations) == 0 {
		return 0, nil
	}
	maxV := migrations[len(migrations)-1].version
	// A database a newer orch migrated has columns and tables this binary
	// knows nothing about; writing its own max back would also lower
	// user_version, and the next newer binary would re-run migrations that
	// are already applied (#530).
	if current > maxV {
		return 0, fmt.Errorf("orch.db is at schema version %d, newer than this orch knows (%d): "+
			"upgrade orch (`orch upgrade`) before using this project", current, maxV)
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		ran, err := applyOne(ctx, db, m)
		if err != nil {
			return applied, err
		}
		if ran {
			applied++
		}
	}
	// The defensive set only ever raises the version, for a database whose
	// files were applied by hand without their pragma.
	if v, err := schemaVersion(ctx, db); err != nil {
		return applied, err
	} else if v < maxV {
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf("PRAGMA user_version = %d", maxV)); err != nil {
			return applied, fmt.Errorf("set user_version: %w", err)
		}
	}
	return applied, nil
}

// applyOne runs one migration file in a transaction, so a statement failing
// partway leaves the database exactly as it was.
//
// The files set `PRAGMA user_version = N` before their statements, and 001–006
// are released and never edited. Run outside a transaction, a second ALTER
// failing left user_version at N with the first column already added: the
// next open skipped the migration, and re-running it would fail on the
// duplicate column (SQLite has no ADD COLUMN IF NOT EXISTS). user_version is
// transactional in SQLite, so the rollback undoes it with the schema.
//
// The version is read again inside the transaction, which the writer opens
// IMMEDIATE (`_txlock=immediate`): a run and a dashboard that both read the
// old version before either migrated would otherwise both apply the file, and
// the second would fail on the duplicate column (#530). ran is false when
// another process got there first.
func applyOne(ctx context.Context, db *sql.DB, m migration) (ran bool, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("apply migration %s: begin: %w", m.name, err)
	}
	current, err := schemaVersion(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if current >= m.version {
		return false, tx.Rollback()
	}
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		_ = tx.Rollback()
		return false, fmt.Errorf("apply migration %s: %w", m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("apply migration %s: commit: %w", m.name, err)
	}
	return true, nil
}
