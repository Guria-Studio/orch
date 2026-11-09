package collide

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// repo is a real git repository with main holding three migrations, 001 to
// 003, as a project's db/migrations directory does.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q")
	r.git("symbolic-ref", "HEAD", "refs/heads/main")
	for _, f := range []string{"001_init.sql", "002_users.sql", "003_orders.sql"} {
		r.write("db/migrations/"+f, "-- "+f+"\n")
	}
	r.write("README.md", "hi\n")
	r.commit("initial")
	return r
}

func (r *repo) git(args ...string) {
	r.t.Helper()
	// #nosec G204 -- this test's own fixed git subcommands.
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.test", "-c", "user.name=t"}, args...)...)
	cmd.Dir = r.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r *repo) write(path, body string) {
	r.t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

// branch cuts name from main and commits files on it.
func (r *repo) branch(name string, files ...string) {
	r.t.Helper()
	r.git("checkout", "-q", "-b", name, "main")
	for _, f := range files {
		r.write(f, "-- "+name+"\n")
	}
	r.commit(name)
	r.git("checkout", "-q", "main")
}

// merge squash-merges name into main, as a forge does.
func (r *repo) merge(name string) {
	r.t.Helper()
	r.git("merge", "-q", "--squash", name)
	r.git("commit", "-q", "-m", name+" (squash)")
}

var migrations = Rule{
	Name: "migrations", Kind: KindSequence, Paths: []string{"db/migrations/*.sql"},
	Key: `^(\d+)_`, Mode: ModeBlock,
}

// TestSequenceCatchesTheV5Collision is the dogfood incident (#522): two
// tasks each add migration 004 under different file names. git merges both
// cleanly; the rule catches it against the base once one is merged, and
// against the other branch while both are open.
func TestSequenceCatchesTheV5Collision(t *testing.T) {
	r := newRepo(t)
	r.branch("orch/A", "db/migrations/004_add_a.sql")
	r.branch("orch/B", "db/migrations/004_add_b.sql")
	ctx := context.Background()

	got, err := Check(ctx, r.dir, []Rule{migrations}, Branch{Label: "main", Ref: "main"}, "orch/B", []Branch{{Label: "A", Ref: "orch/A"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Collision{{
		Rule: "migrations", Key: "004", File: "db/migrations/004_add_b.sql", With: "A",
		WithFile: "db/migrations/004_add_a.sql", Mode: ModeBlock,
		Reason: "migrations: db/migrations/004_add_b.sql adds 004, which A's db/migrations/004_add_a.sql also adds",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("both open:\n got %+v\nwant %+v", got, want)
	}

	r.merge("orch/A")
	got, err = Check(ctx, r.dir, []Rule{migrations}, Branch{Label: "main", Ref: "main"}, "orch/B", nil)
	if err != nil {
		t.Fatal(err)
	}
	want = []Collision{{
		Rule: "migrations", Key: "004", File: "db/migrations/004_add_b.sql", With: "base",
		WithFile: "db/migrations/004_add_a.sql", Mode: ModeBlock,
		Reason: "migrations: db/migrations/004_add_b.sql adds 004, which main already has as db/migrations/004_add_a.sql",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("one merged:\n got %+v\nwant %+v", got, want)
	}
}

func TestSequence(t *testing.T) {
	contiguous := migrations
	contiguous.Contiguous = true
	tests := []struct {
		name  string
		rule  Rule
		files []string
		want  []string // each collision's Key and With
	}{
		{"the next number is fine", contiguous, []string{"db/migrations/004_x.sql"}, nil},
		{"two new in a row are fine", contiguous, []string{"db/migrations/004_x.sql", "db/migrations/005_y.sql"}, nil},
		{"a number the base has collides", migrations, []string{"db/migrations/002_again.sql"}, []string{"002 base"}},
		{"a gap is caught when contiguous", contiguous, []string{"db/migrations/006_x.sql"}, []string{"006 base"}},
		{"a gap is fine when not", migrations, []string{"db/migrations/006_x.sql"}, nil},
		{"a file outside the paths is ignored", migrations, []string{"other/002_again.sql"}, nil},
		{"a name without the key is ignored", migrations, []string{"db/migrations/README.sql"}, nil},
		{"off checks nothing", Rule{Name: "m", Kind: KindSequence, Paths: migrations.Paths, Key: migrations.Key, Mode: ModeOff},
			[]string{"db/migrations/002_again.sql"}, nil},
		{"** matches any depth", Rule{Name: "m", Kind: KindSequence, Paths: []string{"db/**/*.sql"}, Key: migrations.Key, Mode: ModeWarn},
			[]string{"db/migrations/002_again.sql"}, []string{"002 base"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRepo(t)
			r.branch("orch/T", tt.files...)
			got, err := Check(context.Background(), r.dir, []Rule{tt.rule}, Branch{Label: "main", Ref: "main"}, "orch/T", nil)
			if err != nil {
				t.Fatal(err)
			}
			var keys []string
			for _, c := range got {
				keys = append(keys, c.Key+" "+c.With)
			}
			if !reflect.DeepEqual(keys, tt.want) {
				t.Errorf("collisions = %v (%+v), want %v", keys, got, tt.want)
			}
		})
	}
}

// TestSequenceIgnoresWhatTheBaseAlreadyHas: a dependent's branch carries its
// dependency's migration, merged from the dependency's branch; once the
// dependency is on the base, that file is the base's, not a collision, and
// the dependent's own next number is still checked from there.
func TestSequenceIgnoresWhatTheBaseAlreadyHas(t *testing.T) {
	r := newRepo(t)
	r.branch("orch/DEP", "db/migrations/004_dep.sql")
	r.git("checkout", "-q", "-b", "orch/T", "orch/DEP")
	r.write("db/migrations/005_t.sql", "-- t\n")
	r.commit("T")
	r.git("checkout", "-q", "main")
	r.merge("orch/DEP")

	contiguous := migrations
	contiguous.Contiguous = true
	got, err := Check(context.Background(), r.dir, []Rule{contiguous}, Branch{Label: "main", Ref: "main"}, "orch/T", []Branch{{Label: "DEP", Ref: "orch/DEP"}})
	if err != nil || len(got) != 0 {
		t.Errorf("Check = %+v, %v; want nothing", got, err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
		ok   bool
	}{
		{"a sequence rule", migrations, true},
		{"no name", Rule{Kind: KindSequence, Paths: []string{"a"}, Key: `(\d+)`}, false},
		{"an unknown kind", Rule{Name: "x", Kind: "fuzzy", Paths: []string{"a"}, Key: `(\d+)`}, false},
		{"no paths", Rule{Name: "x", Kind: KindSequence, Key: `(\d+)`}, false},
		{"a key that does not compile", Rule{Name: "x", Kind: KindSequence, Paths: []string{"a"}, Key: `(\d+`}, false},
		{"a key without a group", Rule{Name: "x", Kind: KindSequence, Paths: []string{"a"}, Key: `\d+`}, false},
		{"an unknown mode", Rule{Name: "x", Kind: KindSequence, Paths: []string{"a"}, Key: `(\d+)`, Mode: "loud"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.rule.Validate(); (err == nil) != tt.ok {
				t.Errorf("Validate() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"db/migrations/*.sql", "db/migrations/001_a.sql", true},
		{"db/migrations/*.sql", "db/migrations/sub/001_a.sql", false},
		{"db/**/*.sql", "db/001_a.sql", true},
		{"db/**/*.sql", "db/a/b/001_a.sql", true},
		{"**/migrations/*.py", "app/alembic/migrations/x.py", true},
		{"db/*.sql", "dbx/a.sql", false},
	}
	for _, tt := range tests {
		if got := match(tt.pattern, tt.path); got != tt.want {
			t.Errorf("match(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}
