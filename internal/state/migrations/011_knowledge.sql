-- 011_knowledge.sql — the search index over the project's lessons (#451).
--
-- The lessons themselves are markdown files under .orch/knowledge/, versioned
-- with the code; this table is a cache rebuilt from them, never the source of
-- truth. FTS5 ranks them by BM25 for the prompt's "Project knowledge" block
-- and for orch_knowledge_search.
--
-- Go-only, like 006 to 010.

PRAGMA user_version = 11;

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(
  lesson_id UNINDEXED,
  title,
  body,
  tags,
  area
);
