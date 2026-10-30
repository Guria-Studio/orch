package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Guria-Studio/orch/internal/knowledge"
	"github.com/Guria-Studio/orch/internal/worktree"
)

// orch_learn writes the lesson into the task's worktree, where it is
// committed with the task's work, and orch_knowledge_search finds it there
// (#451).
func TestLearnWritesIntoTheTaskWorktreeAndSearchFindsIt(t *testing.T) {
	root, backend := newTestProject(t)
	wt := filepath.Join(worktree.Dir(root), "F0.T1")
	if err := os.MkdirAll(wt, 0o750); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, Options{Backend: backend, ProjectID: "proj", ProjectRoot: root, Knowledge: true})

	var learned learnOut
	res := call(t, cs, "orch_learn", map[string]any{"task_id": "F0.T1", "title": "Health checks need a JSON body",
		"lesson": "The load balancer reads `status`.", "area": []string{"app/"}, "tags": []string{"health"}}, &learned)
	if res.IsError {
		t.Fatalf("orch_learn refused: %s", learned.Message)
	}
	got, err := knowledge.Load(wt)
	if err != nil || len(got) != 1 {
		t.Fatalf("lessons in the worktree = %v, %v; want the one just learned", got, err)
	}
	if l := got[0]; l.Source != "agent" || l.Task != "F0.T1" || l.ID != learned.ID || l.Path != learned.Path {
		t.Errorf("lesson = %+v, answer = %+v", l, learned)
	}
	if lessons, _ := knowledge.Load(root); len(lessons) != 0 {
		t.Errorf("the lesson landed in the project root too: %v", lessons)
	}

	var found knowledgeSearchOut
	call(t, cs, "orch_knowledge_search", map[string]any{"task_id": "F0.T1", "query": "balancer status"}, &found)
	if len(found.Lessons) != 1 || found.Lessons[0].ID != learned.ID || !strings.Contains(found.Lessons[0].Body, "load balancer") {
		t.Errorf("search found %+v", found)
	}
	call(t, cs, "orch_knowledge_search", map[string]any{"task_id": "F0.T1", "query": "kubernetes"}, &found)
	if len(found.Lessons) != 0 || found.Message == "" {
		t.Errorf("an unrelated search found %+v", found)
	}
}

// Off, both tools are listed and say how to turn the feature on; nothing is
// written.
func TestKnowledgeToolsSayWhenTheFeatureIsOff(t *testing.T) {
	cs, root, _ := newTestSession(t)
	var out learnOut
	res := call(t, cs, "orch_learn", map[string]any{"title": "t", "lesson": "l"}, &out)
	if !res.IsError || !strings.Contains(out.Message, "knowledge.enabled: true") {
		t.Errorf("orch_learn with the feature off = %+v, %+v", res, out)
	}
	if _, err := os.Stat(filepath.Join(root, knowledge.Dir)); err == nil {
		t.Error("a lesson was written with the feature off")
	}
}

// A task id that is not one path element never picks where a lesson goes.
func TestLearnIgnoresATaskIDThatWalksOutOfTheWorktrees(t *testing.T) {
	root, backend := newTestProject(t)
	outside := filepath.Join(filepath.Dir(root), "elsewhere")
	if err := os.MkdirAll(outside, 0o750); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, Options{Backend: backend, ProjectID: "proj", ProjectRoot: root, Knowledge: true})
	var out learnOut
	call(t, cs, "orch_learn", map[string]any{"task_id": "../elsewhere", "title": "t", "lesson": "l"}, &out)
	if got, _ := knowledge.Load(outside); len(got) != 0 {
		t.Fatalf("the lesson was written outside the project: %v", got)
	}
	if got, _ := knowledge.Load(root); len(got) != 1 {
		t.Errorf("want the lesson in the project root, got %v (%s)", got, out.Message)
	}
}

// A lesson is a few lines: past 20 orch_learn refuses it and writes nothing.
func TestLearnRefusesALessonOverTwentyLines(t *testing.T) {
	root, backend := newTestProject(t)
	cs := connect(t, Options{Backend: backend, ProjectID: "proj", ProjectRoot: root, Knowledge: true})
	for _, tc := range []struct {
		lines  int
		refuse bool
	}{{21, true}, {20, false}} {
		var out learnOut
		res := call(t, cs, "orch_learn", map[string]any{"title": "long " + strings.Repeat("x", tc.lines),
			"lesson": strings.TrimSuffix(strings.Repeat("a line\n", tc.lines), "\n")}, &out)
		if res.IsError != tc.refuse {
			t.Errorf("%d lines: refused = %v (%s), want %v", tc.lines, res.IsError, out.Message, tc.refuse)
		}
		if tc.refuse && !strings.Contains(out.Message, "at most ~15") {
			t.Errorf("%d lines: message %q does not say why", tc.lines, out.Message)
		}
	}
	if got, _ := knowledge.Load(root); len(got) != 1 {
		t.Errorf("want only the 20-line lesson written, got %d", len(got))
	}
}

// A lesson orch cannot read fails the search with an error that says so.
func TestKnowledgeSearchSaysWhatItCouldNotRead(t *testing.T) {
	root, backend := newTestProject(t)
	if err := os.MkdirAll(filepath.Join(root, knowledge.Dir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, knowledge.Dir, "broken.md"), []byte("no front matter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, Options{Backend: backend, ProjectID: "proj", ProjectRoot: root, Knowledge: true})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "orch_knowledge_search",
		Arguments: map[string]any{"query": "anything"}})
	msg := ""
	if err != nil {
		msg = err.Error()
	} else {
		for _, c := range res.Content {
			if tc, ok := c.(*mcpsdk.TextContent); ok {
				msg += tc.Text
			}
		}
	}
	if !strings.Contains(msg, "reading the lessons: ") || !strings.Contains(msg, "broken.md") {
		t.Errorf("search over a broken lesson = %q, want it to say it could not read the lessons", msg)
	}
}

// orch_learn cannot write a rule: that is the operator's (#505).
func TestLearnRefusesARule(t *testing.T) {
	root, backend := newTestProject(t)
	cs := connect(t, Options{Backend: backend, ProjectID: "proj", ProjectRoot: root, Knowledge: true})
	var learned learnOut
	res := call(t, cs, "orch_learn", map[string]any{"task_id": "F0.T1", "title": "Layering",
		"lesson": "Controllers call services.", "area": []string{"app/"}, "kind": "rule"}, &learned)
	if !res.IsError || !strings.Contains(learned.Message, "only the operator writes a rule") {
		t.Errorf("orch_learn = %v %q, want the rule refused, saying why", res.IsError, learned.Message)
	}
}
