package engine

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/knowledge"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/state"
)

// noRanks is an index that finds nothing: the lessons below are picked by
// their area alone.
type noRanks struct{}

func (noRanks) RankKnowledge(context.Context, []state.KnowledgeDoc, string, int) ([]string, error) {
	return nil, nil
}

// The prompt carries the lessons whose area covers the task's files (#451);
// a lesson orch cannot read costs the block and a warning, never the
// dispatch.
func TestPromptKnowledgeAndAnUnreadableLesson(t *testing.T) {
	for _, tc := range []struct {
		name   string
		broken bool
	}{{"readable", false}, {"one lesson unreadable", true}} {
		t.Run(tc.name, func(t *testing.T) {
			tk := task("C-1", 1, "claude/opus")
			tk.Files = []string{"internal/auth/login.go"}
			f := newReapFixture(t, []model.Task{tk}, map[string]fakeResponse{"C-1": okResponse()}, SchedulerOptions{})
			f.s.Opts.Cfg.Knowledge.Enabled = true
			f.s.Knowledge = noRanks{}
			var logs bytes.Buffer
			f.s.Log = slog.New(slog.NewTextHandler(&logs, nil))
			lesson, err := knowledge.Add(f.s.Opts.Cwd, knowledge.Lesson{Title: "Refresh tokens first",
				Body: "A 401 means expired.", Area: []string{"internal/auth/"}}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if tc.broken {
				write(t, filepath.Join(f.s.Opts.Cwd, knowledge.Dir, "broken.md"), "no front matter\n")
			}

			f.runOnce(t)

			body, err := os.ReadFile(filepath.Join(f.s.Opts.StateDir, "prompts", f.s.Opts.RunID, "C-1.txt")) // #nosec G304 -- a temp dir this test made
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := f.s.Queue.Status("C-1"); got != model.StatusDone {
				t.Errorf("status = %q, want the task dispatched and done", got)
			}
			hasBlock := strings.Contains(string(body), "  - Refresh tokens first [internal/auth/]\n")
			warned := strings.Contains(logs.String(), "reading the project knowledge for the prompt failed") &&
				strings.Contains(logs.String(), "broken.md")
			if hasBlock == tc.broken || warned != tc.broken {
				t.Errorf("block in the prompt = %v, warned = %v; want block %v, warning %v\n%s\n%s",
					hasBlock, warned, !tc.broken, tc.broken, body, logs.String())
			}
			// The lessons the prompt carried are recorded for the Knowledge
			// page (#453); a prompt without them records nothing.
			var used []any
			for _, e := range f.backend.events {
				if e.eventType == knowledge.EventUsed && e.taskID == "C-1" {
					used = append(used, e.extra["lessons"])
				}
			}
			if want := []any{[]string{lesson.ID}}; tc.broken && len(used) != 0 || !tc.broken && !reflect.DeepEqual(used, want) {
				t.Errorf("knowledge_used events = %v; want %v when the block is there, none otherwise", used, want)
			}
		})
	}
}
