package project

import (
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
)

// IssueSection is what a task's PR body says about the issue it came from
// (#525): `Closes` when it is the issue's last unfinished task, so GitHub
// closes the issue on merge; `Refs` while other tasks of the same issue are
// still open, so the first of several PRs does not close it early.
func TestIssueSection(t *testing.T) {
	const url = "https://github.com/acme/shop/issues/12"
	tests := []struct {
		name     string
		task     model.Task
		siblings []string
		want     string
	}{
		{"no issue", model.Task{ID: "A"}, nil, ""},
		{"last task closes", model.Task{ID: "F0.12.T1", SourceIssue: url}, nil,
			"\n\nCloses acme/shop#12"},
		{"others still open refer", model.Task{ID: "F0.12.T1", SourceIssue: url}, []string{"F0.12.T2", "F0.12.T3"},
			"\n\nRefs acme/shop#12 — 2 other task(s) of this issue are not done yet (F0.12.T2, F0.12.T3); the last one's PR closes it."},
		{"not a GitHub issue URL is only linked", model.Task{ID: "A", SourceIssue: "https://gitlab.com/a/b/-/issues/3"}, nil,
			"\n\nIssue: https://gitlab.com/a/b/-/issues/3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IssueSection(tt.task, tt.siblings); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestIssueRef(t *testing.T) {
	tests := []struct {
		url, want string
		ok        bool
	}{
		{"https://github.com/acme/shop/issues/12", "acme/shop#12", true},
		{"https://github.com/acme/shop/issues/12/", "acme/shop#12", true},
		{"https://github.com/acme/shop/pull/12", "", false},
		{"https://github.com/acme/shop/issues/x", "", false},
		{"https://example.com/acme/shop/issues/12", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := IssueRef(tt.url)
		if got != tt.want || ok != tt.ok {
			t.Errorf("IssueRef(%q) = %q, %v; want %q, %v", tt.url, got, ok, tt.want, tt.ok)
		}
	}
}
