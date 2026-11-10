package scaffold

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/collide"
	"github.com/Guria-Studio/orch/internal/config"
)

// TestInitWritesMigrationRules (#522 decision 4): orch init on a repository
// that already has migrations writes the semantic_checks rules for its stack,
// and config.Load reads back exactly those; a repository without any gets no
// block.
func TestInitWritesMigrationRules(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		want  int
	}{
		{"alembic and numbered sql", []string{"alembic/versions/a1_init.py", "db/migrations/001_init.sql"}, 2},
		{"nothing to go on", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "proj")
			for _, f := range tc.files {
				full := filepath.Join(root, filepath.FromSlash(f))
				if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte("x\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			res, err := Run(Options{Root: root, Now: fixedNow})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			loaded, err := config.Load(filepath.Join(res.Root, ".orchestrator", "config.yaml"), res.Root)
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			got := loaded.Config.SemanticChecks
			if len(got) != tc.want {
				t.Fatalf("semantic_checks = %+v, want %d rules", got, tc.want)
			}
			if want := collide.DefaultRules(res.Root); tc.want > 0 && !reflect.DeepEqual(got, want) {
				t.Errorf("semantic_checks read back as\n %+v\nwant\n %+v", got, want)
			}
			body := readFile(t, res, ".orchestrator/config.yaml")
			if n := strings.Count(body, "semantic_checks:"); (tc.want > 0) != (n == 1) {
				t.Errorf("semantic_checks blocks = %d:\n%s", n, body)
			}
		})
	}
}
