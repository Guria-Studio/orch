-- 012_finding_queue.sql — agents' reports about orch, waiting for the
-- operator (#536).
--
-- orch_report_finding no longer files a public issue under the operator's
-- gh login: it queues the report here, redacted. The operator previews it
-- and sends or dismisses it (`orch findings`, or the dashboard). `body` is
-- the issue body exactly as it would be filed. Sent rows also land in
-- filed_findings (010), which de-duplication reads.
--
-- Go-only, like 006 to 011.
--
-- Compatibility: SQLite >= 3.32. No RETURNING, STRICT, or GENERATED.

PRAGMA user_version = 12;

CREATE TABLE IF NOT EXISTS finding_queue (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,
  summary    TEXT NOT NULL DEFAULT '',
  task_id    TEXT NOT NULL DEFAULT '',
  queued_at  TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'queued',  -- queued | sent | dismissed
  url        TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_finding_queue_project ON finding_queue(project_id, status);
