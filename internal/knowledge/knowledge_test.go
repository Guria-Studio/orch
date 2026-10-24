package knowledge

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/state"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func TestAddWritesAFileLoadReadsBack(t *testing.T) {
	root := t.TempDir()
	l, err := Add(root, Lesson{Title: "Auth tokens: refresh first!", Body: "Refresh before retrying.\n",
		Area: []string{"internal/auth/", " "}, Tags: []string{"auth"}, Source: "agent", Task: "F1.T2"}, now)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if l.ID != "20261024-auth-tokens-refresh-first" || l.Path != ".orch/knowledge/20261024-auth-tokens-refresh-first.md" {
		t.Errorf("id %q, path %q", l.ID, l.Path)
	}
	again, err := Add(root, Lesson{Title: "Auth tokens: refresh first!", Body: "other"}, now)
	if err != nil || again.ID != l.ID+"-2" || again.Source != "operator" {
		t.Errorf("a second lesson with the same title = %+v, %v; want its own id, source operator", again, err)
	}
	if _, err := Add(root, Lesson{Title: "x", Body: "y", Source: "robot"}, now); err == nil {
		t.Error("an unknown source was accepted")
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Lesson{ID: l.ID, Title: "Auth tokens: refresh first!", Area: []string{"internal/auth/"}, Tags: []string{"auth"},
		Source: "agent", Task: "F1.T2", Created: "2026-10-24", Body: "Refresh before retrying.", Path: l.Path}
	if len(got) != 2 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("Load = %+v\nwant %+v first", got, want)
	}

	if err := Remove(root, again.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got, _ := Load(root); len(got) != 1 {
		t.Errorf("after Remove, %d lessons", len(got))
	}
	if err := Remove(root, "nope"); err == nil {
		t.Error("removing a lesson that is not there succeeded")
	}
}

func TestLoadWithoutTheDirectoryIsNoLessons(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil || got != nil {
		t.Errorf("Load = %v, %v", got, err)
	}
}

func TestAreaMatches(t *testing.T) {
	for _, c := range []struct {
		area string
		path string
		want bool
	}{
		{"internal/auth", "internal/auth/login.go", true},
		{"internal/auth/", "internal/auth/login.go", true},
		{"internal/auth/**", "internal/auth/x/y.go", true},
		{"internal/*/login.go", "internal/auth/login.go", true},
		{"internal/auth/login.go", "internal/auth", true},
		{"internal/auth", "internal/authz/x.go", false},
		{"web/", "internal/auth/login.go", false},
	} {
		if got := AreaMatches(Lesson{Area: []string{c.area}}, []string{c.path}); got != c.want {
			t.Errorf("area %q, path %q: %v, want %v", c.area, c.path, got, c.want)
		}
	}
}

func TestSelectPutsTheAreaFirstThenBM25(t *testing.T) {
	db, _, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r := state.NewSQLite(db, "p", t.TempDir())

	lessons := []Lesson{
		{ID: "a", Title: "Docker", Body: "compose needs the network"},
		{ID: "b", Title: "Billing", Body: "invoices round half even", Area: []string{"internal/billing"}},
		{ID: "c", Title: "Sessions", Body: "session cookies are httponly; the session store is redis"},
		{ID: "d", Title: "Login", Body: "login rate limit", Area: []string{"internal/auth"}},
	}
	got, err := Select(context.Background(), r, lessons, []string{"internal/auth/login.go"}, "add a session timeout", 5)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	var ids []string
	for _, l := range got {
		ids = append(ids, l.ID)
	}
	if want := []string{"d", "c"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("selected %v, want %v", ids, want)
	}
	if got, _ := Select(context.Background(), r, lessons, nil, "session", 0); got != nil {
		t.Errorf("n=0 selected %v", got)
	}
}

func TestQueryQuotesWordsAndDropsNoise(t *testing.T) {
	if got, want := Query(`The "auth" token's AND-expiry, for x`), `"auth" OR "token" OR "expiry"`; got != want {
		t.Errorf("Query = %s, want %s", got, want)
	}
	if got := Query("a an to"); got != "" {
		t.Errorf("Query of noise = %q", got)
	}
	if strings.Count(Query(strings.Repeat("word ", 3)), "word") != 1 {
		t.Error("a repeated word is searched twice")
	}
}
