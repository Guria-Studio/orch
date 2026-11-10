package collide

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// AlembicRule is the unique_key rule for an Alembic versions directory
// (#522 decision 4): Alembic chains migrations by down_revision, so two new
// migrations naming the same parent are two heads, whatever their files are
// called.
func AlembicRule(versionsDir string) Rule {
	return Rule{Name: "migrations", Kind: KindUniqueKey,
		Paths:   []string{strings.TrimSuffix(versionsDir, "/") + "/*.py"},
		Pattern: `^\s*down_revision\s*(?::[^=]*)?=\s*['"]([0-9A-Za-z_]+)['"]`, Mode: ModeBlock}
}

// DefaultRules are the migration rules for the stacks a repository already
// shows, for `orch init` to write (#522 decision 4): Alembic (by
// down_revision), Django (one sequence per app), Supabase and a plain
// db/migrations directory of numbered SQL files. Empty when it shows none —
// most often a repository scaffolded before its code exists.
func DefaultRules(root string) []Rule {
	var out []Rule
	if dir := alembicVersions(root); dir != "" {
		out = append(out, AlembicRule(dir))
	}
	for _, app := range djangoMigrationDirs(root) {
		out = append(out, Rule{Name: "migrations-" + path.Base(path.Dir(app)), Kind: KindSequence,
			Paths: []string{app + "/*.py"}, Key: `^(\d{4})_`, Contiguous: true, Mode: ModeBlock})
	}
	if isDir(root, "supabase/migrations") {
		out = append(out, Rule{Name: "migrations", Kind: KindSequence,
			Paths: []string{"supabase/migrations/*.sql"}, Key: `^(\d+)_`, Mode: ModeBlock})
	}
	for _, dir := range []string{"db/migrations", "migrations"} {
		if hasNumbered(root, dir, ".sql") {
			out = append(out, Rule{Name: "migrations", Kind: KindSequence,
				Paths: []string{dir + "/*.sql"}, Key: `^(\d+)_`, Mode: ModeBlock})
			break
		}
	}
	// Names must be unique for the events to say which rule fired.
	seen := map[string]int{}
	for i := range out {
		if n := seen[out[i].Name]; n > 0 {
			out[i].Name += "-" + strconv.Itoa(n+1)
		}
		seen[out[i].Name]++
	}
	return out
}

// alembicVersions is the Alembic versions directory, repo-relative: from
// alembic.ini's script_location, else alembic/versions if it exists.
func alembicVersions(root string) string {
	loc := ""
	if f, err := os.Open(filepath.Join(root, "alembic.ini")); err == nil { // #nosec G304 -- the repository being scaffolded
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), "=")
			if ok && strings.TrimSpace(k) == "script_location" {
				loc = strings.TrimSpace(v)
				break
			}
		}
		if err := f.Close(); err != nil {
			loc = ""
		}
	}
	loc = strings.TrimPrefix(strings.TrimPrefix(loc, "%(here)s/"), "./")
	if loc == "" || strings.Contains(loc, ":") || strings.Contains(loc, "%") {
		loc = "alembic"
	}
	if dir := path.Join(filepath.ToSlash(loc), "versions"); isDir(root, dir) {
		return dir
	}
	return ""
}

// djangoMigrationDirs are the <app>/migrations directories holding a
// 0001_*.py, at most three levels down, skipping virtualenvs and
// dependencies.
func djangoMigrationDirs(root string) []string {
	var out []string
	skip := map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true, "site-packages": true, ".orch": true}
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir // an unreadable directory is skipped, not fatal to init
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() && (skip[d.Name()] || strings.Count(rel, "/") > 3) {
			return filepath.SkipDir
		}
		if d.IsDir() && d.Name() == "migrations" && rel != "migrations" && hasNumbered(root, rel, ".py") {
			out = append(out, rel)
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		// The walk itself failed (an unreadable root): no app found.
		return nil
	}
	sort.Strings(out)
	return out
}

// hasNumbered reports whether dir holds a file named <digits>_*<ext>.
func hasNumbered(root, dir, ext string) bool {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		i := strings.IndexFunc(n, func(r rune) bool { return r < '0' || r > '9' })
		if !e.IsDir() && i > 0 && n[i] == '_' && strings.HasSuffix(n, ext) {
			return true
		}
	}
	return false
}

func isDir(root, rel string) bool {
	st, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && st.IsDir()
}
