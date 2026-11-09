-- 016_attempt_effective_route.sql — what an attempt asked for and what ran (#580).
--
-- attempts (013) kept the route's backend, model and router tier. A route can
-- also set a reasoning effort and a codex service tier, and the CLI need not
-- run what it was asked: codex drops a service tier the model does not offer,
-- and its session rollout names the model it resolved. effort and
-- service_tier are the route's ('' when it set none); effective_model,
-- effective_effort and effective_service_tier are what the CLI reported
-- ('' when it reported nothing — unknown, never the declared value).
--
-- Go-only, like 006 to 015.
--
-- Compatibility: SQLite >= 3.32. No RETURNING, STRICT, or GENERATED.

PRAGMA user_version = 16;

ALTER TABLE attempts ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE attempts ADD COLUMN service_tier TEXT NOT NULL DEFAULT '';
ALTER TABLE attempts ADD COLUMN effective_model TEXT NOT NULL DEFAULT '';
ALTER TABLE attempts ADD COLUMN effective_effort TEXT NOT NULL DEFAULT '';
ALTER TABLE attempts ADD COLUMN effective_service_tier TEXT NOT NULL DEFAULT '';
