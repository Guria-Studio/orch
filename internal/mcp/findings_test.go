package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/state"
	"github.com/Guria-Studio/orch/internal/vcs"
)

// queued is what the tool left in the project's review queue.
func queued(t *testing.T, b state.Backend) []state.QueuedFinding {
	t.Helper()
	q, err := b.QueuedFindings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// fakeFindings records what the tool asked GitHub for.
type fakeFindings struct {
	existing []vcs.Issue
	searched []string
	created  []struct {
		repo, title, body string
		labels            []string
	}
	comments []string
}

func (f *fakeFindings) SearchIssues(repo, label, query string, _ int) ([]vcs.Issue, error) {
	f.searched = append(f.searched, repo+"|"+label+"|"+query)
	return f.existing, nil
}

func (f *fakeFindings) CreateIssue(repo, title, body string, labels []string) (string, error) {
	f.created = append(f.created, struct {
		repo, title, body string
		labels            []string
	}{repo, title, body, labels})
	return fmt.Sprintf("https://github.com/Guria-Studio/orch/issues/%d", 998+len(f.created)), nil
}

func (f *fakeFindings) CommentIssue(repo string, number int, body string) (string, error) {
	f.comments = append(f.comments, fmt.Sprintf("%s#%d: %s", repo, number, body))
	return fmt.Sprintf("https://github.com/Guria-Studio/orch/issues/%d#issuecomment-%d", number, len(f.comments)), nil
}

// orch_report_finding is how an agent in any orch project reports a problem
// with orch itself. Since #536 it queues the report for the operator and
// files nothing: each case is what the tool did NOT do as much as what it
// returned — nothing may be queued while it is off, invalid, a duplicate, or
// unconfirmed next to similar reports, and nothing is ever filed.
func TestReportFinding(t *testing.T) {
	valid := map[string]any{
		"type": "bug", "title": "orch explain lists backlog tasks as ready",
		"summary": "explain says ready, run never dispatches them",
	}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range valid {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	similar := []vcs.Issue{{Number: 12, Title: "explain shows the wrong ready set", State: "OPEN", URL: "u12"}}

	tests := []struct {
		name        string
		disabled    bool
		existing    []vcs.Issue
		args        map[string]any
		wantError   bool
		wantQueued  bool
		wantDup     int
		wantSimilar int
		wantMessage string
	}{
		{name: "off for the project", disabled: true, args: valid, wantError: true, wantMessage: "report_findings.enabled"},
		{name: "unknown type", args: with(map[string]any{"type": "rant"}), wantError: true, wantMessage: "bug, improvement, feature"},
		{name: "no summary", args: with(map[string]any{"summary": " "}), wantError: true, wantMessage: "required"},
		{
			name:     "same title in other words of case and punctuation",
			existing: []vcs.Issue{{Number: 235, Title: "Orch explain lists backlog tasks as ready!", State: "CLOSED", URL: "u235"}},
			args:     valid, wantDup: 235, wantMessage: "#235",
		},
		{name: "similar, not confirmed", existing: similar, args: valid, wantSimilar: 1, wantMessage: "confirm_new"},
		{name: "similar, confirmed", existing: similar, args: with(map[string]any{"confirm_new": true}), wantQueued: true, wantSimilar: 1},
		{name: "nothing like it", args: valid, wantQueued: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, backend := newTestProject(t)
			fake := &fakeFindings{existing: tt.existing}
			opts := Options{Backend: backend, ProjectRoot: root, Version: "v-test"}
			if !tt.disabled {
				opts.Findings = fake
			}
			cs := connect(t, opts)

			var out reportFindingOut
			res := call(t, cs, "orch_report_finding", tt.args, &out)
			if res.IsError != tt.wantError {
				t.Errorf("IsError = %v, want %v (%q)", res.IsError, tt.wantError, out.Message)
			}
			if out.Queued != tt.wantQueued || (len(queued(t, backend)) == 1) != tt.wantQueued {
				t.Errorf("queued = %v with %d rows, want %v", out.Queued, len(queued(t, backend)), tt.wantQueued)
			}
			if len(fake.created) != 0 || len(fake.comments) != 0 {
				t.Errorf("the tool published: created %d, comments %v", len(fake.created), fake.comments)
			}
			if tt.wantError && len(fake.searched) != 0 {
				t.Errorf("searched GitHub for a refused report: %v", fake.searched)
			}
			if tt.wantDup != 0 && (out.Duplicate == nil || out.Duplicate.Number != tt.wantDup) {
				t.Errorf("duplicate = %+v, want #%d", out.Duplicate, tt.wantDup)
			}
			if len(out.Similar) != tt.wantSimilar {
				t.Errorf("similar = %+v, want %d", out.Similar, tt.wantSimilar)
			}
			if !strings.Contains(out.Message, tt.wantMessage) {
				t.Errorf("message = %q, want it to mention %q", out.Message, tt.wantMessage)
			}
		})
	}
}

// What waits for the operator: the report's sections, and none of the
// operator's paths or secrets — the body sending would file, as is.
func TestReportFindingQueuesARedactedReport(t *testing.T) {
	root, backend := newTestProject(t)
	fake := &fakeFindings{}
	cs := connect(t, Options{Backend: backend, ProjectRoot: root, Version: "v0.12.0", Findings: fake})
	home, _ := os.UserHomeDir()

	call(t, cs, "orch_report_finding", map[string]any{
		"type": "improvement", "title": "doctor should flag an ignored .claude/",
		"summary":       "agents in " + root + " ran with no allow-list",
		"evidence":      "log at " + filepath.Join(home, "notes.txt") + " with GH_TOKEN=ghp_" + strings.Repeat("x", 36),
		"suggested_fix": "a check in internal/doctor",
		"confidence":    "medium",
	}, &reportFindingOut{})

	q := queued(t, backend)
	if len(q) != 1 || len(fake.created) != 0 {
		t.Fatalf("queued %d, created %d; want 1 queued and nothing filed", len(q), len(fake.created))
	}
	got := struct{ body string }{q[0].Body}
	if strings.Contains(got.body, "ghp_") {
		t.Errorf("body leaks a token:\n%s", got.body)
	}
	for _, want := range []string{"**Type**: improvement", "**Confidence**: medium", "orch `v0.12.0`",
		"## Summary", "agents in <project> ran", "## Evidence", "~/notes.txt", "## Suggested fix"} {
		if !strings.Contains(got.body, want) {
			t.Errorf("body is missing %q:\n%s", want, got.body)
		}
	}
	if strings.Contains(got.body, root) || strings.Contains(got.body, home) {
		t.Errorf("body leaks a local path:\n%s", got.body)
	}
	if strings.Contains(got.body, "## Repro") {
		t.Errorf("an empty section was rendered:\n%s", got.body)
	}
}

// #433, #434 and #435 were one finding — verify ran `tue` — reported three
// times by agents of one project within a minute, too soon for GitHub's
// search to see the first (#441). The queue is read first: the first call
// queues, the next two find it waiting and queue nothing.
func TestReportFindingDoesNotQueueTwice(t *testing.T) {
	root, backend := newTestProject(t)
	fake := &fakeFindings{} // search finds nothing, as it did then
	cs := connect(t, Options{Backend: backend, ProjectRoot: root, Version: "v0.24.0", Findings: fake})
	reports := []map[string]any{
		{"type": "bug", "title": `Local verify ran a nonexistent command "tue"`,
			"summary": "A task was rejected because local verify ran `\"tue\"` (sh: tue: not found, exit 127)."},
		{"type": "bug", "title": `Local verify ran a nonexistent command ("tue") and rejected the task`,
			"summary": `The orch local verify step executed the command "tue", failed with exit 127 (not found), and rejected a task whose tests and lint pass.`},
		{"type": "bug", "title": `Local verify ran a bogus command ("tue") and rejected a task`,
			"summary": "Local verify executed `\"tue\"` (exit 127, \"not found\") as the app start command and rejected a task whose tests and lint pass."},
	}
	for i, task := range []string{"F1.T1", "F1.T2", "F1.T3"} {
		t.Setenv("ORCH_TASK_ID", task)
		var out reportFindingOut
		call(t, cs, "orch_report_finding", reports[i], &out)
		if out.Queued != (i == 0) {
			t.Errorf("call %d: queued = %v (%s)", i+1, out.Queued, out.Message)
		}
		if i > 0 && (out.Duplicate == nil || out.Duplicate.State != "QUEUED") {
			t.Errorf("call %d: duplicate = %+v, want the queued report", i+1, out.Duplicate)
		}
	}
	if q := queued(t, backend); len(q) != 1 || q[0].TaskID != "F1.T1" {
		t.Errorf("queue = %+v, want one report from F1.T1", q)
	}
}

// An open report GitHub's search finds is a duplicate: its URL comes back,
// nothing is queued, and nothing is commented under the operator's login.
func TestReportFindingAnswersAnOpenDuplicate(t *testing.T) {
	root, backend := newTestProject(t)
	fake := &fakeFindings{existing: []vcs.Issue{{Number: 433, Title: `Local verify ran a nonexistent command "tue"`, State: "OPEN",
		URL: "https://github.com/Guria-Studio/orch/issues/433"}}}
	cs := connect(t, Options{Backend: backend, ProjectRoot: root, Findings: fake})
	var out reportFindingOut
	call(t, cs, "orch_report_finding", map[string]any{"type": "bug",
		"title": `Local verify ran a bogus command ("tue") and rejected a task`, "summary": "exit 127"}, &out)
	if len(queued(t, backend)) != 0 || len(fake.comments) != 0 || out.URL != "https://github.com/Guria-Studio/orch/issues/433" {
		t.Errorf("queued %d, comments %v, url %q: want #433's URL and nothing else", len(queued(t, backend)), fake.comments, out.URL)
	}
}

// The second test of likelyDuplicate: titles that share fewer than half
// their key words are one finding when they quote the same command or error.
func TestLikelyDuplicateOnTheSameQuotedCommand(t *testing.T) {
	title := "verify rejected a task over a missing binary"
	other := "agent saw verify fail on a typo in config.yaml, task sent back"
	for _, tt := range []struct {
		text, otherText string
		want            bool
	}{
		{"it ran `tue`", `the command "tue" exited 127`, true},
		{"it ran `tue`", "the command `pnpm e2e` exited 1", false},
		{"no quotes here", "none here either", false},
	} {
		if got := likelyDuplicate(title, title+"\n"+tt.text, other, other+"\n"+tt.otherText); got != tt.want {
			t.Errorf("%q vs %q: likelyDuplicate = %v, want %v", tt.text, tt.otherText, got, tt.want)
		}
	}
}
