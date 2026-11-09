package evidence

import (
	"path/filepath"
	"strings"
	"testing"
)

func passedManifest() Manifest {
	return Manifest{
		Version: Version, TaskID: "F2.T3", Title: "Login", RunID: "run-1", Attempt: 2, HeadSHA: "abc1234def",
		Verdict: VerdictPassed,
		Checks: []Check{
			{Command: "go test ./...", DurationS: 41.2, Kind: "tests"},
			{Command: "golangci-lint run", DurationS: 10, Kind: "lint"},
			{Command: "go build ./...", DurationS: 6.8, Kind: "build"},
			{Command: "pnpm exec playwright test", DurationS: 30, Kind: "e2e"},
		},
		Probes: []Probe{{Path: "/", Status: 200, OK: true}, {Path: "/dashboard", Status: 200, OK: true}},
		Review: &Review{Reviewer: "gemini", Findings: []Finding{{Severity: "minor", Title: "naming"}, {Severity: "minor", Title: "docs"}}},
		Diff:   &Diff{Files: 7, Insertions: 120, Deletions: 14},
		E2E: &E2E{Passed: 5, Dir: "e2e/run-1/F2.T3/attempt-2",
			Media:        []Media{{Test: "login › entra", Kind: "video", Path: "t001/video.webm", Bytes: 10}},
			Walkthroughs: []Walkthrough{{Title: "login › entra", Status: "passed", Steps: []string{"Abre la página", "Entra"}, Video: "t001/video.webm"}}},
	}
}

// TestCommentSummarisesTheEvidence is the PR comment of §3.2: one marked
// comment, in English, with a row per kind of check, and the media said to be
// kept locally (§9: no media leaves the machine yet).
func TestCommentSummarisesTheEvidence(t *testing.T) {
	got := Comment(passedManifest(), "")
	for _, want := range []string{
		CommentMarker,
		"### Evidence — F2.T3 (attempt 2)",
		"| Check | Result |",
		"| tests · lint · build (3 commands, 58 s) | ✅ |",
		"| app probes (2) | ✅ `/` 200, `/dashboard` 200 |",
		"| E2E (1 command, 30 s) | ✅ 5 passed · 0 failed |",
		"| review (gemini) | ✅ no blocking findings · 2 minor |",
		"Diff: 7 files, +120 −14 · commit `abc1234`.",
		"Walk-through: login › entra (2 steps, video)",
		"kept locally",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment is missing %q:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(got, CommentMarker+"\n") {
		t.Errorf("the marker does not open the comment:\n%s", got)
	}
}

func TestCommentSaysWhatWasNotReviewed(t *testing.T) {
	m := passedManifest()
	m.Verdict, m.Review = VerdictReviewUnavailable, &Review{Unavailable: "no provider other than claude is installed"}
	m.E2E, m.Probes = nil, nil
	got := Comment(m, "")
	if !strings.Contains(got, "| review | ⚠️ not reviewed: no provider other than claude is installed |") {
		t.Errorf("comment = \n%s", got)
	}
	if strings.Contains(got, "E2E") || strings.Contains(got, "probes") {
		t.Errorf("rows for what did not run:\n%s", got)
	}
}

// TestCommentKeepsSecretsOut is phase 3's acceptance: text the comment
// carries out of the machine (finding titles, step titles, commands) is
// dropped whole when the guard sees a secret in it, and redacted otherwise;
// the project's path never shows.
func TestCommentKeepsSecretsOut(t *testing.T) {
	m := passedManifest()
	// Shapes the redaction knows, assembled here so no source line holds
	// one: a GitHub token and a password assignment.
	token := "gh" + "p_" + strings.Repeat("Zq7", 12)
	secret := strings.Repeat("q", 9)
	assignment := "pass" + "word=" + secret
	root := filepath.Join(t.TempDir(), "proj")
	m.Review.Findings = append(m.Review.Findings, Finding{Severity: "minor", Title: "token " + token + " is hard-coded"})
	m.Review.Findings = append(m.Review.Findings, Finding{Severity: "minor", Title: "reads " + root + "/.env"})
	m.E2E.Walkthroughs[0].Steps = []string{"Entra con " + token}
	m.E2E.Walkthroughs[0].Title = "login with " + assignment
	got := Comment(m, root)
	for _, leak := range []string{token, secret, root} {
		if strings.Contains(got, leak) {
			t.Errorf("the comment leaks %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "4 minor") {
		t.Errorf("the findings are not counted any more:\n%s", got)
	}
}
