-- 017_merge_queue.sql — the merge queue (#522).
--
-- With merge_queue.enabled, a task whose PR went green waits here and orch
-- merges the queue one PR at a time, in order (phase, then how many tasks
-- depend on it, then enqueued_at), bringing each up to date with the base and
-- re-verifying it first when the base moved. A row outlives `orch run`, so a
-- restarted run keeps the order. state: queued (waiting), merged, ejected
-- (sent back to its agent or blocked; reason says why). requeues counts the
-- times it was sent back.
--
-- Go-only, like 006 to 016.
--
-- Compatibility: SQLite >= 3.32. No RETURNING, STRICT, or GENERATED.

PRAGMA user_version = 17;

CREATE TABLE IF NOT EXISTS merge_queue (
  project_id   TEXT NOT NULL,
  task_id      TEXT NOT NULL,
  pr_url       TEXT NOT NULL DEFAULT '',
  enqueued_at  TEXT NOT NULL,
  state        TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'merged', 'ejected')),
  requeues     INTEGER NOT NULL DEFAULT 0,
  reason       TEXT NOT NULL DEFAULT '',
  updated_at   TEXT NOT NULL,
  PRIMARY KEY (project_id, task_id)
);
