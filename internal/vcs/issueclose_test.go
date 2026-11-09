package vcs

import (
	"errors"
	"strings"
	"testing"
)

// `orch atomize --from-issues <query>` asks with a search query and no label:
// the query is GitHub's issue search (`label:orch`, `milestone:v1`, ...).
func TestListIssuesWithASearchNeedsNoLabel(t *testing.T) {
	withFakeBin(t)
	log := ghLog(t)
	t.Setenv("FAKE_GH_ISSUES_STDOUT", "[]")

	if _, err := NewGitHubProvider().ListIssues(IssueQuery{Search: "label:orch -label:wontfix"}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	argv := readLog(t, log)
	for _, want := range []string{"issue list --search label:orch -label:wontfix --state open --limit 500 --json " + issueListFields} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv = %s, want it to contain %q", argv, want)
		}
	}
	if strings.Contains(argv, "--label") {
		t.Errorf("argv = %s, want no --label when none was asked for", argv)
	}
}

func TestListIssuesWithASearchReportsTheQueryOnFailure(t *testing.T) {
	withFakeBin(t)
	t.Setenv("FAKE_GH_ISSUES_EXIT", "1")
	_, err := NewGitHubProvider().ListIssues(IssueQuery{Search: "label:orch"})
	if err == nil || !strings.Contains(err.Error(), `--search "label:orch"`) {
		t.Fatalf("err = %v, want it to name the search", err)
	}
}

// CloseIssue is one `gh issue close <url> --comment <evidence>`: the comment
// and the close land together, and the URL names the repository, so gh's
// working directory does not matter.
func TestCloseIssue(t *testing.T) {
	withFakeBin(t)
	log := ghLog(t)

	err := NewGitHubProvider().CloseIssue("https://github.com/acme/shop/issues/12", "Done by orch: F0.12.T1")
	if err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if got := readLog(t, log); got != "gh issue close https://github.com/acme/shop/issues/12 --comment Done by orch: F0.12.T1\n" {
		t.Errorf("argv = %q", got)
	}
}

func TestCloseIssueFailures(t *testing.T) {
	t.Run("gh refuses", func(t *testing.T) {
		withFakeBin(t)
		t.Setenv("FAKE_GH_ISSUE_CLOSE_EXIT", "1")
		t.Setenv("FAKE_GH_ISSUE_CLOSE_STDERR", "GraphQL: Could not resolve to an issue")
		err := NewGitHubProvider().CloseIssue("https://github.com/acme/shop/issues/12", "x")
		if err == nil || !strings.Contains(err.Error(), "issues/12") || !strings.Contains(err.Error(), "Could not resolve") {
			t.Fatalf("err = %v, want the issue and gh's own words", err)
		}
	})
	t.Run("no gh", func(t *testing.T) {
		withNoBin(t)
		var cliErr *CLIError
		if err := NewGitHubProvider().CloseIssue("https://github.com/acme/shop/issues/12", "x"); !errors.As(err, &cliErr) {
			t.Fatalf("err = %v, want a *CLIError", err)
		}
	})
}
