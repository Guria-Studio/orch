package collide

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var endpoints = Rule{
	Name: "endpoints", Kind: KindUniqueKey, Paths: []string{"internal/**/*.go"},
	Pattern: `"(?:GET|POST|PUT|PATCH|DELETE) (/[^"]*)"`, Mode: ModeWarn,
}

// endpointRepo is a repository whose main serves GET /api/health.
func endpointRepo(t *testing.T) *repo {
	t.Helper()
	r := newRepo(t)
	r.write("internal/server/routes.go", "package server\n\nvar routes = []string{\n\t\"GET /api/health\",\n}\n")
	r.commit("routes")
	return r
}

// branchWith cuts name from main, writes the files given as path → body and
// commits them.
func (r *repo) branchWith(name string, files map[string]string) {
	r.t.Helper()
	r.git("checkout", "-q", "-b", name, "main")
	for f, body := range files {
		r.write(f, body)
	}
	r.commit(name)
	r.git("checkout", "-q", "main")
}

// TestUniqueKeyCatchesADuplicateEndpoint (#522 §6.3): two tasks each add GET
// /api/tests in different files. Both open, it collides with the other
// branch; once one is merged, with the base.
func TestUniqueKeyCatchesADuplicateEndpoint(t *testing.T) {
	r := endpointRepo(t)
	r.branchWith("orch/A", map[string]string{"internal/tests/a.go": "package tests\n\nconst route = \"GET /api/tests\"\n"})
	r.branchWith("orch/B", map[string]string{"internal/runs/b.go": "package runs\n\nconst route = \"GET /api/tests\"\n"})
	ctx := context.Background()

	got, err := Check(ctx, r.dir, []Rule{endpoints}, Branch{Label: "main", Ref: "main"}, "orch/B", []Branch{{Label: "A", Ref: "orch/A"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Collision{{Rule: "endpoints", Key: "/api/tests", File: "internal/runs/b.go", With: "A",
		WithFile: "internal/tests/a.go", Mode: ModeWarn,
		Reason: "endpoints: internal/runs/b.go adds /api/tests, which A's internal/tests/a.go also adds"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("both open:\n got %+v\nwant %+v", got, want)
	}

	r.merge("orch/A")
	got, err = Check(ctx, r.dir, []Rule{endpoints}, Branch{Label: "main", Ref: "main"}, "orch/B", nil)
	if err != nil {
		t.Fatal(err)
	}
	want = []Collision{{Rule: "endpoints", Key: "/api/tests", File: "internal/runs/b.go", With: "base",
		WithFile: "internal/tests/a.go", Mode: ModeWarn,
		Reason: "endpoints: internal/runs/b.go adds /api/tests, which main already has in internal/tests/a.go"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("one merged:\n got %+v\nwant %+v", got, want)
	}
}

func TestUniqueKey(t *testing.T) {
	routes := "package server\n\nvar routes = []string{\n\t\"GET /api/health\",\n"
	tests := []struct {
		name  string
		rule  Rule
		files map[string]string
		want  []string // each collision's Key and With
	}{
		{"a new endpoint is fine", endpoints,
			map[string]string{"internal/server/routes.go": routes + "\t\"GET /api/runs\",\n}\n"}, nil},
		{"a key the base has but the branch does not add stays quiet", endpoints,
			map[string]string{"internal/server/routes.go": routes + "\t// a comment\n}\n"}, nil},
		{"a key the base has and the branch adds again collides", endpoints,
			map[string]string{"internal/other/dup.go": "package other\n\nconst r = \"POST /api/health\"\n"}, []string{"/api/health base"}},
		{"a line moved to another file is not new", endpoints,
			map[string]string{"internal/server/routes.go": "package server\n\nvar routes = []string{\n}\n",
				"internal/server/moved.go": "package server\n\nconst health = \"GET /api/health\"\n"}, nil},
		{"a file outside the paths is ignored", endpoints,
			map[string]string{"docs/api.md": "\"GET /api/health\"\n"}, nil},
		{"off checks nothing", Rule{Name: "e", Kind: KindUniqueKey, Paths: endpoints.Paths, Pattern: endpoints.Pattern, Mode: ModeOff},
			map[string]string{"internal/other/dup.go": "const r = \"GET /api/health\"\n"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := endpointRepo(t)
			r.branchWith("orch/T", tt.files)
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

// TestUniqueKeyCatchesTwoAlembicHeads: Alembic chains migrations by
// down_revision, not by number. A branch whose new migration points at a
// revision another migration on main already follows makes two heads.
func TestUniqueKeyCatchesTwoAlembicHeads(t *testing.T) {
	r := newRepo(t)
	r.write("alembic/versions/a1_init.py", "revision = 'a1'\ndown_revision = None\n")
	r.write("alembic/versions/b2_users.py", "revision = 'b2'\ndown_revision = 'a1'\n")
	r.commit("alembic")
	r.branchWith("orch/T", map[string]string{"alembic/versions/c3_orders.py": "revision = 'c3'\ndown_revision = 'a1'\n"})
	r.branchWith("orch/OK", map[string]string{"alembic/versions/d4_items.py": "revision = 'd4'\ndown_revision = 'b2'\n"})
	rule := AlembicRule("alembic/versions")
	ctx := context.Background()

	got, err := Check(ctx, r.dir, []Rule{rule}, Branch{Label: "main", Ref: "main"}, "orch/T", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "a1" || got[0].WithFile != "alembic/versions/b2_users.py" {
		t.Errorf("two heads: %+v, want a1 against b2_users.py", got)
	}
	if got, err := Check(ctx, r.dir, []Rule{rule}, Branch{Label: "main", Ref: "main"}, "orch/OK", nil); err != nil || len(got) != 0 {
		t.Errorf("a migration on the head: %+v, %v; want nothing", got, err)
	}
}

// TestCommandRule: the script sees the three SHAs; a non-zero exit is one
// collision carrying what it printed, a zero exit is none.
func TestCommandRule(t *testing.T) {
	r := newRepo(t)
	r.branch("orch/T", "db/migrations/004_t.sql")
	r.branch("orch/O", "db/migrations/004_o.sql")
	seen := filepath.Join(t.TempDir(), "env")
	fail := Rule{Name: "custom", Kind: KindCommand, Mode: ModeBlock,
		Run: `echo "$ORCH_BASE_SHA $ORCH_HEAD_SHA $ORCH_OTHER_HEADS" > ` + seen + `; echo "schema drift in users"; exit 3`}
	ctx := context.Background()

	got, err := Check(ctx, r.dir, []Rule{fail}, Branch{Label: "main", Ref: "main"}, "orch/T", []Branch{{Label: "O", Ref: "orch/O"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Collision{{Rule: "custom", With: "base", Mode: ModeBlock, Reason: "custom: schema drift in users"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("failing command:\n got %+v\nwant %+v", got, want)
	}
	env, err := os.ReadFile(seen) // #nosec G304 -- t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if f := strings.Fields(string(env)); len(f) != 3 || len(f[0]) != 40 || f[1] == f[0] || f[2] == f[1] {
		t.Errorf("the command saw %q, want base, head and the other head's SHAs", env)
	}

	pass := Rule{Name: "custom", Kind: KindCommand, Run: "exit 0"}
	if got, err := Check(ctx, r.dir, []Rule{pass}, Branch{Label: "main", Ref: "main"}, "orch/T", nil); err != nil || len(got) != 0 {
		t.Errorf("passing command: %+v, %v; want nothing", got, err)
	}
}

func TestValidateNewKinds(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
		ok   bool
	}{
		{"a unique_key rule", endpoints, true},
		{"unique_key without a pattern", Rule{Name: "x", Kind: KindUniqueKey, Paths: []string{"a"}}, false},
		{"unique_key with a pattern without a group", Rule{Name: "x", Kind: KindUniqueKey, Paths: []string{"a"}, Pattern: `GET /x`}, false},
		{"unique_key without paths", Rule{Name: "x", Kind: KindUniqueKey, Pattern: `(x)`}, false},
		{"a command rule needs no paths", Rule{Name: "x", Kind: KindCommand, Run: "true"}, true},
		{"a command rule without run", Rule{Name: "x", Kind: KindCommand}, false},
		{"the alembic default", AlembicRule("alembic/versions"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.rule.Validate(); (err == nil) != tt.ok {
				t.Errorf("Validate() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}
