-- 010_filed_findings.sql — what this project's agents reported about orch.
--
-- orch_report_finding writes a row for each issue it files, and reads them
-- before filing the next: a finding several tasks hit within minutes is one
-- issue with comments, not one issue each (#441). GitHub's search alone
-- missed those, as it indexes a new issue only after a while.
--
-- Go-only, like 006 to 009. Not 002's `findings` table: that was the Python
-- line's capture queue, with other columns and type values.
--
-- Compatibility: SQLite >= 3.32. No RETURNING, STRICT, or GENERATED.

PRAGMA user_version = 10;

CREATE TABLE IF NOT EXISTS filed_findings (
  project_id   TEXT NOT NULL,
  issue_number INTEGER NOT NULL,
  url          TEXT NOT NULL,
  title        TEXT NOT NULL,
  summary      TEXT NOT NULL DEFAULT '',
  filed_at     TEXT NOT NULL,
  PRIMARY KEY (project_id, issue_number)
);
