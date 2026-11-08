-- 013_attempts.sql — one row per finished attempt, for `orch metrics` (#538).
--
-- `dispatches` is keyed (run_id, task_id) and is cleared once the process is
-- reaped, so a retry in the same run overwrote the attempt before it and
-- nothing kept how each one ended. `attempts` keeps them all. It has its own
-- id rather than a (run, task, attempt) key: a CI retry starts again at
-- attempt 1 in the same run.
--
-- result is the dispatch's outcome (success | fail | timeout | stopped);
-- verify is the local verify's verdict on a clean exit ('' when it did not
-- run, pass, fail); review is the reviewer's ('' when no review completed,
-- pass, blocks); findings_json the review's findings. ci is how the PR's CI
-- went for the attempt that pushed ('' | success | failure), set by the CI
-- poller's writes.
--
-- Go-only, like 006 to 012.
--
-- Compatibility: SQLite >= 3.32. No RETURNING, STRICT, or GENERATED.

PRAGMA user_version = 13;

CREATE TABLE IF NOT EXISTS attempts (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id    TEXT NOT NULL,
  run_id        TEXT NOT NULL,
  task_id       TEXT NOT NULL,
  attempt       INTEGER NOT NULL,
  backend       TEXT NOT NULL DEFAULT '',
  model         TEXT NOT NULL DEFAULT '',
  tier          TEXT NOT NULL DEFAULT '',
  result        TEXT NOT NULL,
  failure_class TEXT NOT NULL DEFAULT '',
  reason        TEXT NOT NULL DEFAULT '',
  tokens_in     INTEGER NOT NULL DEFAULT 0,
  tokens_out    INTEGER NOT NULL DEFAULT 0,
  cost_usd      REAL NOT NULL DEFAULT 0.0,
  duration_s    REAL NOT NULL DEFAULT 0.0,
  verify        TEXT NOT NULL DEFAULT '',
  review        TEXT NOT NULL DEFAULT '',
  reviewer      TEXT NOT NULL DEFAULT '',
  findings_json TEXT NOT NULL DEFAULT '[]',
  ci            TEXT NOT NULL DEFAULT '',
  finished_at   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_attempts_task ON attempts(project_id, task_id, id);
