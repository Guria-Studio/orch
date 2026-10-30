package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
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

// countingRanker records whether the index was asked anything.
type countingRanker struct{ calls int }

func (c *countingRanker) RankKnowledge(context.Context, []state.KnowledgeDoc, string, int) ([]string, error) {
	c.calls++
	return nil, nil
}

// n=0 or no lessons selects nothing and never touches the index.
func TestSelectWithNothingToPickSkipsTheIndex(t *testing.T) {
	r := &countingRanker{}
	if got, err := Select(context.Background(), r, []Lesson{{ID: "a", Title: "session"}}, nil, "session", 0); got != nil || err != nil {
		t.Errorf("n=0 selected %v, %v", got, err)
	}
	if got, err := Select(context.Background(), r, nil, nil, "session", 5); got != nil || err != nil {
		t.Errorf("no lessons selected %v, %v", got, err)
	}
	if r.calls != 0 {
		t.Errorf("the index was asked %d times", r.calls)
	}
}

// ForTask reads the lessons under root and matches them against the task's
// files and its spec file under specRoot, then its title and description.
func TestForTaskMatchesFilesSpecAndText(t *testing.T) {
	db, _, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r := state.NewSQLite(db, "p", t.TempDir())

	root := t.TempDir()
	for _, l := range []Lesson{
		{Title: "Spec lesson", Body: "about the spec", Area: []string{"specs/f1-auth.md"}},
		{Title: "File lesson", Body: "about login", Area: []string{"internal/auth/"}},
		{Title: "Text lesson", Body: "rate limiting needs redis"},
		{Title: "Unrelated", Body: "docker networks", Area: []string{"deploy/"}},
	} {
		if _, err := Add(root, l, now); err != nil {
			t.Fatal(err)
		}
	}
	task := model.Task{ID: "F1.T1", Title: "rate limiting", Files: []string{"internal/auth/login.go"}, SpecRef: "f1-auth.md#T1"}
	got, err := ForTask(context.Background(), r, root, task, "specs", 5)
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	var titles []string
	for _, l := range got {
		titles = append(titles, l.Title)
	}
	if want := []string{"File lesson", "Spec lesson", "Text lesson"}; !reflect.DeepEqual(titles, want) {
		t.Errorf("ForTask = %v, want %v", titles, want)
	}

	if got, err := ForTask(context.Background(), r, t.TempDir(), task, "specs", 5); got != nil || err != nil {
		t.Errorf("ForTask with no lessons = %v, %v", got, err)
	}
	bad := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bad, Dir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, Dir, "x.md"), []byte("no front matter"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ForTask(context.Background(), r, bad, task, "specs", 5); err == nil || !strings.Contains(err.Error(), "x.md") {
		t.Errorf("ForTask over a broken lesson = %v, want an error naming the file", err)
	}
}

func TestParseRefusesBrokenFrontMatter(t *testing.T) {
	for name, raw := range map[string]string{
		"no front matter":   "just a body\n",
		"no closing line":   "---\nid: a\ntitle: t\n",
		"malformed YAML":    "---\nid: [unclosed\n---\nbody\n",
		"a list, not a map": "---\n- a\n- b\n---\nbody\n",
	} {
		t.Run(name, func(t *testing.T) {
			if l, err := Parse([]byte(raw)); err == nil {
				t.Errorf("Parse accepted %q as %+v", raw, l)
			}
		})
	}
	l, err := Parse([]byte("---\r\nid: a\r\ntitle: t\r\n---"))
	if err != nil || l.ID != "a" || l.Body != "" {
		t.Errorf("CRLF front matter with no body = %+v, %v", l, err)
	}
}

func TestRemoveSaysWhatFailed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes files from a read-only directory")
	}
	root := t.TempDir()
	l, err := Add(root, Lesson{Title: "t", Body: "b"}, now)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, Dir)
	if err := os.Chmod(dir, 0o500); err != nil { // #nosec G302 -- a temp dir this test made, read-only on purpose
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) }) // #nosec G302 -- restored so TempDir can clean up
	err = Remove(root, l.ID)
	if err == nil || !strings.Contains(err.Error(), "remove lesson "+l.ID) {
		t.Errorf("Remove = %v, want it wrapped with the lesson id", err)
	}
}

type events []state.Event

func (e *events) AppendEvent(_ context.Context, _ string, ev state.Event) error {
	*e = append(*e, ev)
	return nil
}

// The operator's one write path (#453): Update keeps the lesson's id, file
// and date, and every change through the Editor is an event.
func TestEditorWritesAndRecordsEachChange(t *testing.T) {
	root := t.TempDir()
	var got events
	ed := Editor{Root: root, Events: &got, Source: "dashboard"}
	l, err := ed.Add(context.Background(), Lesson{Title: "Old title", Body: "old"}, now)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := ed.Update(context.Background(), l.ID, Lesson{Title: " New title ", Body: "new", Area: []string{"web/"}}, now.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if edited.ID != l.ID || edited.Created != l.Created || edited.Path != l.Path || edited.Title != "New title" || edited.Source != "operator" {
		t.Errorf("Update = %+v; want the same id, file and date, the new title", edited)
	}
	loaded, _ := Load(root)
	if len(loaded) != 1 || loaded[0].Body != "new" || !reflect.DeepEqual(loaded[0].Area, []string{"web/"}) {
		t.Errorf("after Update the files hold %+v", loaded)
	}
	if _, err := ed.Update(context.Background(), l.ID, Lesson{Title: "no body"}, now); err == nil {
		t.Error("Update accepted a lesson with no body")
	}
	if _, err := ed.Update(context.Background(), "nope", Lesson{Title: "x", Body: "y"}, now); !errors.Is(err, ErrNoLesson) {
		t.Errorf("Update of an unknown id = %v, want ErrNoLesson", err)
	}
	if err := ed.Remove(context.Background(), l.ID, now); err != nil {
		t.Fatal(err)
	}
	if loaded, _ := Load(root); len(loaded) != 0 {
		t.Errorf("after Remove the files hold %+v", loaded)
	}
	var actions []string
	for _, e := range got {
		if e.EventType != EventEdited || e.Extra["lesson"] != l.ID || e.Extra["source"] != "dashboard" {
			t.Errorf("event %+v", e)
		}
		actions = append(actions, e.Extra["action"].(string))
	}
	if want := []string{"added", "edited", "removed"}; !reflect.DeepEqual(actions, want) {
		t.Errorf("events %v, want %v", actions, want)
	}
}

// Usage counts the prompts that carried a lesson and, of the done tasks they
// were for, the first-try successes — each task once.
func TestUsageCountsPromptsAndFirstTries(t *testing.T) {
	used := func(task string, ids ...any) state.Event {
		return state.Event{EventType: EventUsed, TaskID: task, Extra: map[string]any{"lessons": ids}}
	}
	got := Usage([]state.Event{
		used("T-1", "a"), used("T-2", "a", "b"), used("T-2", "a"), used("T-3", "a"),
		{EventType: "dispatch", TaskID: "T-1", Extra: map[string]any{"lessons": []any{"a"}}},
	}, []state.TaskRuntime{
		{ID: "T-1", Attempts: 1},
		{ID: "T-2", Attempts: 2},
	})
	want := map[string]Use{"a": {Prompts: 4, Tasks: 2, FirstTry: 1}, "b": {Prompts: 1, Tasks: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Usage = %+v, want %+v", got, want)
	}
}
