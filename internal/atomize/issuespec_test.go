package atomize

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
)

// A body shaped like the issues orch's own reporter files: bold "field"
// bullets, headings, a fence and a line that looks like an atomizer header.
// Each one is a line the spec parser would otherwise act on.
const trickyIssueBody = "- **Type**: feature\n" +
	"**About**: orch\n\n" +
	"## Context\n" +
	"# F1 — not a phase\n" +
	"### F9.9.T9 — not a task\n" +
	"```go\nfunc x() {}\n```\n" +
	"Last line."

func sampleIssue() SourceIssue {
	return SourceIssue{
		Number: 12,
		Title:  "Export the report as CSV: totals & per-task rows!",
		Body:   trickyIssueBody,
		URL:    "https://github.com/acme/shop/issues/12",
	}
}

func TestIssueSpecFileName(t *testing.T) {
	tests := []struct {
		number int
		title  string
		want   string
	}{
		{12, "Export the report as CSV: totals & per-task rows!", "issue-12-export-the-report-as-csv-totals-per-task-rows.md"},
		{3, "  Ünïcode — títulos  ", "issue-3-n-code-t-tulos.md"},
		{7, "!!!", "issue-7.md"},
		{8, strings.Repeat("word ", 30), "issue-8-word-word-word-word-word-word-word-word-word-word.md"},
	}
	for _, tt := range tests {
		if got := IssueSpecFileName(tt.number, tt.title); got != tt.want {
			t.Errorf("IssueSpecFileName(%d, %q) = %q, want %q", tt.number, tt.title, got, tt.want)
		}
	}
}

// The generated spec goes through the normal parser and comes out as one task
// that carries the issue: its URL first in the description, the body intact
// (no field, header or fence in it acted on), and sourceIssue set.
func TestIssueSpecParsesToOneTaskThatCarriesTheIssue(t *testing.T) {
	issue := sampleIssue()
	text := IssueSpec(issue, "shop", "claude-sonnet-4-6")
	specs := t.TempDir()
	path := filepath.Join(specs, IssueSpecFileName(issue.Number, issue.Title))

	res := ParseText(text, path, specs, "shop")

	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none: the body must not reach the parser", res.Warnings)
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("parsed %d tasks, want 1: %+v", len(res.Tasks), res.Tasks)
	}
	got := res.Tasks[0]
	want := ParsedTask{
		ID: "F0.12.T1", Phase: 0, Package: 12, TaskNum: 1,
		Title:        issue.Title,
		Model:        "claude-sonnet-4-6",
		Dependencies: []string{}, Files: []string{},
		SpecRef:     filepath.Base(path) + "#F0.12.T1",
		SourceIssue: issue.URL,
		SourceFile:  path,
	}
	// Line is where the header sits in a generated file, and the description
	// is checked line by line below, so both are left out of the comparison.
	gotDesc := got.Description
	got.Line, got.Description = 0, ""
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("task =\n%s\nwant\n%s", gotJSON, wantJSON)
	}

	first, rest, _ := strings.Cut(gotDesc, "\n")
	if first != issue.URL {
		t.Errorf("description's first line = %q, want the issue URL", first)
	}
	for _, line := range strings.Split(issue.Body, "\n") {
		if !strings.Contains(rest, line) {
			t.Errorf("description lost the body line %q:\n%s", line, rest)
		}
	}
}

func TestIssueSpecWithAnEmptyBody(t *testing.T) {
	issue := SourceIssue{Number: 4, Title: "t", URL: "https://github.com/acme/shop/issues/4"}
	res := ParseText(IssueSpec(issue, "shop", "m"), "/s/issue-4-t.md", "/s", "shop")
	if len(res.Tasks) != 1 || res.Tasks[0].Description != issue.URL {
		t.Fatalf("tasks = %+v, want one whose description is just the URL", res.Tasks)
	}
}

// A spec the operator split by hand keeps the issue on every task it defines:
// the issue lives in the frontmatter, not in a task.
func TestSourceIssueFrontmatterReachesEveryTask(t *testing.T) {
	text := "---\ntype: spec\nsource_issue: https://github.com/acme/shop/issues/12\n---\n\n" +
		"# F0 — Issues\n\n## F0.12 — CSV\n\n### F0.12.T1 — totals\n\n- **Model**: m\n\n" +
		"### F0.12.T2 — rows\n\n- **Model**: m\n"
	res := ParseText(text, "/s/issue-12-csv.md", "/s", "")
	if len(res.Tasks) != 2 {
		t.Fatalf("parsed %d tasks, want 2", len(res.Tasks))
	}
	for _, task := range res.Tasks {
		if task.SourceIssue != "https://github.com/acme/shop/issues/12" {
			t.Errorf("%s SourceIssue = %q", task.ID, task.SourceIssue)
		}
	}
}

// MergeTasks writes sourceIssue on a new task and onto an existing one, and
// a spec without it leaves an existing value alone.
func TestMergeCarriesSourceIssue(t *testing.T) {
	const url = "https://github.com/acme/shop/issues/12"
	parsed := []ParsedTask{
		{ID: "F0.12.T1", Title: "a", Model: "m", SourceIssue: url},
		{ID: "F0.12.T2", Title: "b", Model: "m", SourceIssue: url},
		{ID: "F1.1.T1", Title: "c", Model: "m"},
	}
	existing := model.TasksFile{Tasks: []model.Task{
		{ID: "F0.12.T2", Title: "b", Model: "m"},
		{ID: "F1.1.T1", Title: "c", Model: "m", SourceIssue: "https://github.com/acme/shop/issues/1"},
	}}

	merged, diff := MergeTasks(existing, parsed)

	got := map[string]string{}
	for _, task := range merged.Tasks {
		got[task.ID] = task.SourceIssue
	}
	want := map[string]string{
		"F0.12.T1": url,
		"F0.12.T2": url,
		"F1.1.T1":  "https://github.com/acme/shop/issues/1",
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s sourceIssue = %q, want %q", id, got[id], w)
		}
	}
	if len(diff.Updated) != 1 || diff.Updated[0].New.ID != "F0.12.T2" ||
		strings.Join(diff.Updated[0].Changed, ",") != "sourceIssue" {
		t.Errorf("updated = %+v, want F0.12.T2 changed in sourceIssue only", diff.Updated)
	}
}

// A source_issue that is not a string is dropped with a warning, never
// written onto a task.
func TestSourceIssueFrontmatterMustBeAString(t *testing.T) {
	text := "---\ntype: spec\nsource_issue: [1, 2]\n---\n\n# F0 — x\n\n### F0.1.T1 — t\n\n- **Model**: m\n"
	res := ParseText(text, "/s/x.md", "/s", "")
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "source_issue debe ser string") {
		t.Errorf("warnings = %v, want the source_issue one", res.Warnings)
	}
	if len(res.Tasks) != 1 || res.Tasks[0].SourceIssue != "" {
		t.Errorf("tasks = %+v, want one with no sourceIssue", res.Tasks)
	}
}
