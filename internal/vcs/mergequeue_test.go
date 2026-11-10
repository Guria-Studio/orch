package vcs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitHubMergeQueueActive (#522): orch asks GitHub whether the base branch
// has its merge queue on, and reads the answer real gh printed for a
// repository with one and one without.
func TestGitHubMergeQueueActive(t *testing.T) {
	for _, tt := range []struct {
		fixture string
		want    bool
	}{
		{"graphql-merge-queue-active.json", true},
		{"graphql-merge-queue-none.json", false},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			withFakeBin(t)
			body, err := os.ReadFile(filepath.Join("testdata", "gh", "2.101.0", tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(t.TempDir(), "gh.log")
			t.Setenv("FAKE_GH_LOG", log)
			t.Setenv("FAKE_GH_API_STDOUT", string(body))
			got, err := NewGitHubProvider().MergeQueueActive("https://github.com/acme/shop/pull/12", "main")
			if err != nil || got != tt.want {
				t.Fatalf("MergeQueueActive = %v, %v; want %v", got, err, tt.want)
			}
			args, _ := os.ReadFile(log) // #nosec G304 -- t.TempDir()
			for _, want := range []string{"api graphql", "owner=acme", "name=shop", "branch=main"} {
				if !strings.Contains(string(args), want) {
					t.Errorf("gh was called as %q, want %q in it", args, want)
				}
			}
		})
	}
	withFakeBin(t)
	if _, err := NewGitHubProvider().MergeQueueActive("not a PR", "main"); err == nil {
		t.Error("a non-PR URL was accepted")
	}
}
