-- 009_provider_state.sql — providers the operator turned off.
--
-- `orch provider disable <backend>` (and the dashboard's switch, which runs
-- the same function) writes a row here; `orch run` leaves the tasks routed to
-- a disabled backend waiting, the way a capped budget does, until
-- `orch provider enable` turns it back on. Runtime state, so it lives in the
-- database rather than in config.yaml. A backend with no row is enabled.
--
-- Go-only, like 006 to 008.
--
-- Compatibility: SQLite >= 3.32. No RETURNING, STRICT, or GENERATED.

PRAGMA user_version = 9;

CREATE TABLE IF NOT EXISTS provider_state (
  project_id   TEXT NOT NULL,
  backend      TEXT NOT NULL,
  disabled     INTEGER NOT NULL DEFAULT 0,
  reason       TEXT NOT NULL DEFAULT '',
  updated_at   TEXT NOT NULL,
  PRIMARY KEY (project_id, backend)
);
