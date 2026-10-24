package state

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

// The index is FTS5, which modernc.org/sqlite has to be built with; this is
// the test that says it is, and that a rebuild replaces what an earlier one indexed.
func TestRankKnowledgeRanksByBM25AndRebuilds(t *testing.T) {
	ctx := context.Background()
	db, _, err := Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	b := NewSQLite(db, "p", t.TempDir())

	docs := []KnowledgeDoc{
		{ID: "a", Title: "Migrations", Body: "run the migration before the tests"},
		{ID: "b", Title: "Tokens", Body: "tokens expire; refresh tokens rotate tokens on use"},
		{ID: "c", Title: "Docker", Body: "compose needs the network"},
	}
	got, err := b.RankKnowledge(ctx, docs, `"tokens" OR "migration"`, 5)
	if err != nil {
		t.Fatalf("RankKnowledge: %v", err)
	}
	if want := []string{"b", "a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ranked %v, want %v (most mentions first)", got, want)
	}

	got, err = b.RankKnowledge(ctx, docs[2:], `"tokens"`, 5)
	if err != nil {
		t.Fatalf("RankKnowledge after a rebuild: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a removed lesson is still found: %v", got)
	}
}
