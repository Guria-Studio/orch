package snapshot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/evidence"
)

// technicalEvidence is a manifest full of what a client must never read:
// ids, run, models, commands, finding text, the state dir's paths.
func technicalEvidence() evidence.Manifest {
	return evidence.Manifest{
		Version: evidence.Version, TaskID: "F0.1.T1", Title: "Setup monorepo", RunID: "run-20261109-abc", Attempt: 2,
		HeadSHA: "deadbeefcafe", Verdict: evidence.VerdictPassed,
		Checks: []evidence.Check{
			{Command: "go test ./internal/secretpkg/...", Kind: "tests"},
			{Command: "golangci-lint run", Kind: "lint"},
			{Command: "pnpm install", Kind: "install"},
			{Command: "pnpm exec playwright test", Kind: "e2e"},
		},
		Review: &evidence.Review{Reviewer: "gemini", Model: "gemini-3-pro",
			Findings: []evidence.Finding{{Severity: "minor", File: "internal/x.go", Title: "rename the handler"}}},
		Diff: &evidence.Diff{Files: 3, Insertions: 40, Deletions: 2, Top: []string{"internal/x.go"}},
		E2E: &evidence.E2E{Passed: 4, Failed: 0, Flaky: 1, Dir: "e2e/run-20261109-abc/F0.1.T1/attempt-2",
			Media: []evidence.Media{{Test: "login", Kind: "video", Path: "t001/video.webm", Bytes: 1000}},
			Walkthroughs: []evidence.Walkthrough{{Title: "login › entra", Status: "passed",
				Steps: []string{"Abre la página de inicio", "Entra con su cuenta"}, Video: "t001/video.webm"}}},
	}
}

func evidenceInput(media bool) Input {
	in := fixtureInput(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC))
	in.Evidence = map[string]evidence.Manifest{"F0.1.T1": technicalEvidence()}
	in.EvidenceMedia, in.MaxVideoBytes = media, 4<<20
	return in
}

func evidenceOfSetup(t *testing.T, snap Snapshot) *DeliverableEvidence {
	t.Helper()
	for _, m := range snap.Milestones {
		for _, p := range m.Packages {
			for _, d := range p.Deliverables {
				if d.Title == "Setup monorepo" {
					return d.Evidence
				}
			}
		}
	}
	t.Fatal("the done deliverable is not in the snapshot")
	return nil
}

// TestDeliverableCarriesItsEvidence is #520 phase 5: a done deliverable
// says how it was checked — the kinds of check, reviewed, the E2E count and
// the walk-through's steps — and, opted in, where its video is in the site.
func TestDeliverableCarriesItsEvidence(t *testing.T) {
	e := evidenceOfSetup(t, Build(evidenceInput(true)))
	if e == nil {
		t.Fatal("no evidence on the done deliverable")
	}
	if strings.Join(e.Checked, ",") != "tests,lint,e2e" || !e.Reviewed || e.E2E == nil || e.E2E.Passed != 4 || e.E2E.Total != 5 {
		t.Errorf("evidence = %+v", e)
	}
	if strings.Join(e.Steps, "|") != "Abre la página de inicio|Entra con su cuenta" {
		t.Errorf("steps = %q, want the @evidence test's, as written", e.Steps)
	}
	if !strings.HasPrefix(e.Video, "evidence/") || !strings.HasSuffix(e.Video, "/video.webm") {
		t.Errorf("video = %q, want a site path", e.Video)
	}

	if off := evidenceOfSetup(t, Build(evidenceInput(false))); off == nil || off.Video != "" || len(off.Steps) != 2 {
		t.Errorf("without evidence.publish.video = %+v, want the steps and no video", off)
	}
}

// TestEvidenceKeepsOperatorFactsOut: none of the manifest's ids, models,
// commands, finding text or paths reach the snapshot.
func TestEvidenceKeepsOperatorFactsOut(t *testing.T) {
	raw, err := json.Marshal(Build(evidenceInput(true)))
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	walkKeys(t, doc, "$")
	for _, leak := range []string{"F0.1.T1", "run-20261109-abc", "gemini", "secretpkg", "golangci", "playwright",
		"rename the handler", "internal/x.go", "deadbeef", "t001", "e2e/"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the snapshot carries %q", leak)
		}
	}
}

// A deliverable that is not done shows no evidence, and neither does one
// whose last verify failed.
func TestEvidenceOnlyOnDoneWork(t *testing.T) {
	in := evidenceInput(true)
	failed := technicalEvidence()
	failed.TaskID, failed.Verdict = "F0.1.T2", evidence.VerdictFailed
	in.Evidence["F0.1.T2"] = failed
	snap := Build(in)
	for _, m := range snap.Milestones {
		for _, p := range m.Packages {
			for _, d := range p.Deliverables {
				if d.Status != "done" && d.Evidence != nil {
					t.Errorf("%q (%s) has evidence", d.Title, d.Status)
				}
			}
		}
	}
}

// StripEvidenceMedia is what `--to cloud` publishes (#520 §9: the media stay
// local for now): the steps, without video or poster.
func TestStripEvidenceMedia(t *testing.T) {
	snap := StripEvidenceMedia(Build(evidenceInput(true)))
	if e := evidenceOfSetup(t, snap); e == nil || e.Video != "" || e.Poster != "" || len(e.Steps) == 0 {
		t.Errorf("stripped = %+v", e)
	}
	if got := MediaPaths(Build(evidenceInput(true))); len(got) != 1 {
		t.Errorf("MediaPaths = %v, want the one video", got)
	}
}
