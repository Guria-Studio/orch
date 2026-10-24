package state

import (
	"context"
	"database/sql"
	"fmt"
)

// KnowledgeDoc is one lesson as the search index sees it (#451).
type KnowledgeDoc struct {
	ID, Title, Body, Tags, Area string
}

// RankKnowledge rebuilds the lesson index from docs and returns
// the ids of the lessons matching the FTS5 query, best BM25 first, at most
// limit. The files under .orch/knowledge/ are the truth and the index is
// their cache: rebuilding and querying in one transaction means a reader
// never ranks against another worktree's set of lessons.
//
// ponytail: a full rebuild per search, fine for the tens to hundreds of
// lessons a project has; index incrementally (by file mtime) past that.
func (b *SQLite) RankKnowledge(ctx context.Context, docs []KnowledgeDoc, query string, limit int) ([]string, error) {
	var ids []string
	err := b.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_fts`); err != nil {
			return fmt.Errorf("clear the knowledge index: %w", err)
		}
		for _, d := range docs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO knowledge_fts (lesson_id, title, body, tags, area) VALUES (?, ?, ?, ?, ?)`,
				d.ID, d.Title, d.Body, d.Tags, d.Area); err != nil {
				return fmt.Errorf("index lesson %s: %w", d.ID, err)
			}
		}
		if query == "" {
			return nil
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT lesson_id FROM knowledge_fts WHERE knowledge_fts MATCH ? ORDER BY bm25(knowledge_fts) LIMIT ?`,
			query, limit)
		if err != nil {
			return fmt.Errorf("search the knowledge index: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("scan a knowledge hit: %w", err)
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	return ids, err
}
