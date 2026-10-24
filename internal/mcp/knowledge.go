package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hectorcanaimero/orch/internal/knowledge"
	"github.com/hectorcanaimero/orch/internal/worktree"
)

// knowledgeOff is what both tools answer when the project keeps no memory.
const knowledgeOff = "the project knowledge is off for this project; the operator can turn it on with " +
	"`knowledge.enabled: true` in .orchestrator/config.yaml."

type learnIn struct {
	TaskID string   `json:"task_id,omitempty" jsonschema:"the task you are working on; defaults to ORCH_TASK_ID"`
	Title  string   `json:"title" jsonschema:"one line a later task would search for, e.g. 'auth tests need the redis container up'"`
	Lesson string   `json:"lesson" jsonschema:"what happened and what worked, at most ~15 lines; no secrets"`
	Area   []string `json:"area,omitempty" jsonschema:"paths or globs it applies to, e.g. internal/auth/ or web/src/**; tasks touching them get it first"`
	Tags   []string `json:"tags,omitempty" jsonschema:"a few keywords"`
}

type learnOut struct {
	ID      string `json:"id,omitempty"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// taskDir is where task's agent works: its worktree, next to the project,
// when there is one, else the project root (dispatch.worktree_mode off). A
// lesson written there is committed with the task's work and reaches the
// base through its PR, like the code it is about.
func (s *server) taskDir(task string) string {
	// A task id is one path element: an agent's "../x" must not pick the
	// directory a lesson is written to.
	if filepath.IsLocal(task) && !strings.ContainsAny(task, `/\`) && s.opts.ProjectRoot != "" {
		wt := filepath.Join(worktree.Dir(s.opts.ProjectRoot), task)
		if st, err := os.Stat(wt); err == nil && st.IsDir() { // #nosec G703 -- task is a single local path element, checked above
			return wt
		}
	}
	return s.opts.ProjectRoot
}

func (s *server) learn(_ context.Context, _ *mcpsdk.CallToolRequest, in learnIn) (*mcpsdk.CallToolResult, learnOut, error) {
	refuse := func(msg string) (*mcpsdk.CallToolResult, learnOut, error) {
		return &mcpsdk.CallToolResult{IsError: true}, learnOut{Message: msg}, nil
	}
	if !s.opts.Knowledge {
		return refuse(knowledgeOff)
	}
	task := strings.TrimSpace(in.TaskID)
	if task == "" {
		task = os.Getenv("ORCH_TASK_ID")
	}
	if n := strings.Count(strings.TrimSpace(in.Lesson), "\n") + 1; n > 20 {
		return refuse("a lesson is a few lines (at most ~15), not a document; say what happened and what worked")
	}
	l, err := knowledge.Add(s.taskDir(task), knowledge.Lesson{Title: in.Title, Body: in.Lesson,
		Area: in.Area, Tags: in.Tags, Source: "agent", Task: task}, time.Now())
	if err != nil {
		return refuse(err.Error())
	}
	return nil, learnOut{ID: l.ID, Path: l.Path,
		Message: "recorded " + l.Path + "; it is committed with your work and later tasks touching its area get it in their prompt"}, nil
}

type knowledgeSearchIn struct {
	Query  string `json:"query" jsonschema:"words to look for; rephrase and search again if nothing fits"`
	TaskID string `json:"task_id,omitempty" jsonschema:"search the lessons your task's worktree has; defaults to ORCH_TASK_ID"`
	Limit  int    `json:"limit,omitempty" jsonschema:"at most this many lessons (default 5)"`
}

type knowledgeSearchOut struct {
	Lessons []knowledge.Lesson `json:"lessons"`
	Message string             `json:"message,omitempty"`
}

func (s *server) knowledgeSearch(ctx context.Context, _ *mcpsdk.CallToolRequest, in knowledgeSearchIn) (*mcpsdk.CallToolResult, knowledgeSearchOut, error) {
	if !s.opts.Knowledge {
		return &mcpsdk.CallToolResult{IsError: true}, knowledgeSearchOut{Lessons: []knowledge.Lesson{}, Message: knowledgeOff}, nil
	}
	task := strings.TrimSpace(in.TaskID)
	if task == "" {
		task = os.Getenv("ORCH_TASK_ID")
	}
	limit := in.Limit
	if limit < 1 {
		limit = 5
	}
	lessons, err := knowledge.Load(s.taskDir(task))
	if err != nil {
		return nil, knowledgeSearchOut{}, fmt.Errorf("reading the lessons: %w", err)
	}
	ranker, _ := s.opts.Backend.(knowledge.Ranker)
	found, err := knowledge.Search(ctx, ranker, lessons, in.Query, limit)
	if err != nil {
		return nil, knowledgeSearchOut{}, fmt.Errorf("searching the lessons: %w", err)
	}
	out := knowledgeSearchOut{Lessons: append([]knowledge.Lesson{}, found...)}
	if len(found) == 0 {
		out.Message = "no lesson matches; try other words"
	}
	return nil, out, nil
}
