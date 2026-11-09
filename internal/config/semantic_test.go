package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/collide"
)

// semantic_checks (#522) is empty by default; a rule loads as written, mode
// lower-cased and warn when absent; an unusable one is refused at load.
func TestSemanticChecks(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       []collide.Rule
		wantErr    string
	}{
		{"default", "", nil, ""},
		{"a sequence rule", `semantic_checks:
  - name: migrations
    kind: sequence
    paths: ["db/migrations/*.sql"]
    key: '^(\d+)_'
    contiguous: true
    mode: Block
  - name: alembic-ish
    kind: sequence
    paths: ["migrations/*.sql"]
    key: '^(\d+)'
`, []collide.Rule{
			{Name: "migrations", Kind: "sequence", Paths: []string{"db/migrations/*.sql"}, Key: `^(\d+)_`, Contiguous: true, Mode: "block"},
			{Name: "alembic-ish", Kind: "sequence", Paths: []string{"migrations/*.sql"}, Key: `^(\d+)`, Mode: "warn"},
		}, ""},
		{"a key without a group", "semantic_checks:\n  - {name: m, kind: sequence, paths: [a], key: 'x'}\n", nil, "semantic_checks"},
		{"an unknown kind", "semantic_checks:\n  - {name: m, kind: fuzzy, paths: [a], key: '(x)'}\n", nil, "semantic_checks"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			write(t, path, c.yaml)
			res, err := Load(path, filepath.Dir(path))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("Load error = %v, want one naming %s", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := res.Config.SemanticChecks; len(got) != len(c.want) || (len(got) > 0 && !equalRules(got, c.want)) {
				t.Errorf("semantic_checks = %+v, want %+v", got, c.want)
			}
			for _, w := range res.Warnings {
				if strings.Contains(w, "semantic_checks") {
					t.Errorf("semantic_checks warned as unknown: %s", w)
				}
			}
		})
	}
}

func equalRules(a, b []collide.Rule) bool {
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Kind != b[i].Kind || strings.Join(a[i].Paths, ",") != strings.Join(b[i].Paths, ",") ||
			a[i].Key != b[i].Key || a[i].Contiguous != b[i].Contiguous || a[i].Mode != b[i].Mode {
			return false
		}
	}
	return true
}
