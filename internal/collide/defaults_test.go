package collide

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func touchAll(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDefaultRules (#522 decision 4): the migration rules orch init writes
// for the stacks a repository shows, and none for one that shows none.
func TestDefaultRules(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		ini   string
		want  []string // each rule's name, kind and first path
	}{
		{"an empty repository", nil, "", nil},
		{"alembic at its default place", []string{"alembic/versions/a1_init.py"}, "",
			[]string{"migrations unique_key alembic/versions/*.py"}},
		{"alembic where alembic.ini says", []string{"db/alembic/versions/a1_init.py"}, "[alembic]\nscript_location = db/alembic\n",
			[]string{"migrations unique_key db/alembic/versions/*.py"}},
		{"django, one rule per app", []string{"shop/migrations/0001_initial.py", "users/migrations/0001_initial.py", "shop/migrations/__init__.py"}, "",
			[]string{"migrations-shop sequence shop/migrations/*.py", "migrations-users sequence users/migrations/*.py"}},
		{"supabase", []string{"supabase/migrations/20261101120000_init.sql"}, "",
			[]string{"migrations sequence supabase/migrations/*.sql"}},
		{"numbered sql files", []string{"db/migrations/001_init.sql"}, "",
			[]string{"migrations sequence db/migrations/*.sql"}},
		{"a migrations dir with no numbered files", []string{"db/migrations/README.md"}, "", nil},
		{"dependencies are not apps", []string{".venv/lib/django/contrib/auth/migrations/0001_initial.py"}, "", nil},
		{"two stacks get distinct names", []string{"alembic/versions/a.py", "db/migrations/001_init.sql"}, "",
			[]string{"migrations unique_key alembic/versions/*.py", "migrations-2 sequence db/migrations/*.sql"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			touchAll(t, root, tt.files...)
			if tt.ini != "" {
				if err := os.WriteFile(filepath.Join(root, "alembic.ini"), []byte(tt.ini), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var got []string
			for _, r := range DefaultRules(root) {
				if err := r.Validate(); err != nil {
					t.Errorf("rule %s is invalid: %v", r.Name, err)
				}
				got = append(got, r.Name+" "+r.Kind+" "+r.Paths[0])
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rules = %v, want %v", got, tt.want)
			}
		})
	}
}
