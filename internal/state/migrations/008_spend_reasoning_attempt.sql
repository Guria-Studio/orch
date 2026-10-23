-- 008_spend_reasoning_attempt.sql — a dispatch's reasoning tokens and attempt.
--
-- `tokens_out` keeps every output token, reasoning included (codex and agy
-- already count it there; opencode reports it apart, and its parser adds it
-- in). `reasoning_tokens` says how much of it was reasoning, 0 when the CLI
-- does not say. `attempt` is the task's attempt the row paid for, so usage
-- can be split per attempt and retries seen (#417); 0 on rows written before
-- this migration, which no one can date to an attempt now.
--
-- Go-only, like 006 and 007.
--
-- Compatibility: SQLite >= 3.32. No RETURNING, STRICT, or GENERATED.

PRAGMA user_version = 8;

ALTER TABLE spend ADD COLUMN reasoning_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE spend ADD COLUMN attempt INTEGER NOT NULL DEFAULT 0;
