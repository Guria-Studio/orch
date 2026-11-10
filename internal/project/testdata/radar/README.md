# Radar report fixture

`dogfood-events.json` is the conflict radar's warnings, the conflicts and the
CI blocks recorded in orch's own dogfood project between 2026-10-23 and
2026-11-02, read out of its `orch.db` (`events` table) with the PR URLs
removed. `TestRadarEffectivenessOnRealEvents` runs the radar report over them:
real event shapes, as `orch run` wrote them, not ones a test invented.
