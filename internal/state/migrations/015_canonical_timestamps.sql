-- 015_canonical_timestamps.sql — one spelling for every timestamp (#550).
--
-- An orch.db held the same instant written several ways: Python wrote
-- `2026-09-01T09:00:00+00:00` (sometimes with microseconds), Go writes
-- `2026-09-01T09:00:00Z`, and a naive `2026-09-01T09:00:00` turns up too.
-- Compared as text they sort wrong (`...09:00:00Z` > `...10:00:00+02:00`),
-- which is why every reader had to parse in Go. This rewrites each timestamp
-- column to the one form Go writes: UTC, second precision, `Z`
-- (state.FormatTS).
--
-- SQLite's own date parser does the conversion: it reads `Z`, `[+-]HH:MM`,
-- fractional seconds and a missing zone (taken as UTC, as state.ParseTS does)
-- and applies the offset. The GLOB keeps it to values that start like an
-- ISO date, so '' and NULL stay as they are and nothing numeric is read as a
-- Julian day; a value strftime cannot parse is kept unchanged (COALESCE).
-- Fractional seconds are truncated, matching every Go writer.
--
-- dedup_hash columns are not touched: they keep the hash of the spelling the
-- row was first written with, which is what a replayed import recomputes.
-- milestones.target_date is a date, not a timestamp, and is left alone.
--
-- Go-only, like 006 to 014.
--
-- Compatibility: SQLite >= 3.32. No RETURNING, STRICT, or GENERATED.

PRAGMA user_version = 15;

UPDATE projects SET created_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', created_at), created_at)
 WHERE created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE projects SET migrated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', migrated_at), migrated_at)
 WHERE migrated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE tasks_runtime SET started_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', started_at), started_at)
 WHERE started_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE tasks_runtime SET finished_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', finished_at), finished_at)
 WHERE finished_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE tasks_runtime SET updated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', updated_at), updated_at)
 WHERE updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE runs SET started_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', started_at), started_at)
 WHERE started_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE runs SET updated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', updated_at), updated_at)
 WHERE updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE dispatches SET started_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', started_at), started_at)
 WHERE started_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE events SET ts = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', ts), ts)
 WHERE ts GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE spend SET ts = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', ts), ts)
 WHERE ts GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE findings SET created_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', created_at), created_at)
 WHERE created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE tasks_definition SET updated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', updated_at), updated_at)
 WHERE updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE milestones SET created_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', created_at), created_at)
 WHERE created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE stakeholder_tokens SET rotated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', rotated_at), rotated_at)
 WHERE rotated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE provider_state SET updated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', updated_at), updated_at)
 WHERE updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE filed_findings SET filed_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', filed_at), filed_at)
 WHERE filed_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE finding_queue SET queued_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', queued_at), queued_at)
 WHERE queued_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
UPDATE attempts SET finished_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', finished_at), finished_at)
 WHERE finished_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*';
