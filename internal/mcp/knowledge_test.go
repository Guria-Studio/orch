package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/knowledge"
	"github.com/hectorcanaimero/orch/internal/worktree"
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
