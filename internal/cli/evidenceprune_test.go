package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/evidence"
)

// `orch run` prunes the evidence when it ends (#520 §3.5), to
// evidence.keep_attempts, and says what it freed.
func TestRunPrunesTheEvidenceWhenItEnds(t *testing.T) {
	paths := config.Paths{Root: t.TempDir(), ID: "p"}
	stateDir := paths.StateDir()
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 2; i++ {
		m := evidence.Manifest{Version: evidence.Version, TaskID: "F1", RunID: "run-1", Attempt: i, VerifiedAt: t0.Add(time.Duration(i) * time.Hour), Verdict: evidence.VerdictPassed}
		if _, err := evidence.Record(stateDir, m); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(stateDir, "e2e", "run-1", "F1", fmt.Sprintf("attempt-%d", i))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "video.webm"), make([]byte, 2048), 0o600); err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			if err := evidence.Promote(stateDir, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	var cfg config.Config
	cfg.Evidence.KeepAttempts = 1
	var out bytes.Buffer

	pruneEvidence(&out, paths, cfg)

	if _, err := os.Stat(filepath.Join(stateDir, "e2e", "run-1", "F1", "attempt-1")); err == nil {
		t.Error("attempt 1 was kept past keep_attempts: 1")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "e2e", "run-1", "F1", "attempt-2")); err != nil {
		t.Errorf("the pushed attempt was pruned: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "evidence: pruned 1 old attempt") || !strings.Contains(got, "keep_attempts") {
		t.Errorf("output = %q, want a note of what was pruned", got)
	}
}

// publishMedia maps each evidence video and poster the snapshot shows to its
// file under the state dir (#520 §3.4); nothing without the opt-in.
func TestPublishMediaFollowsTheSnapshot(t *testing.T) {
	paths := config.Paths{Root: t.TempDir(), ID: "p"}
	m := evidence.Manifest{Version: evidence.Version, TaskID: "F1", RunID: "run-1", Attempt: 1, Verdict: evidence.VerdictPassed,
		E2E: &evidence.E2E{Passed: 1, Dir: "e2e/run-1/F1/attempt-1",
			Media:        []evidence.Media{{Test: "w", Kind: "video", Path: "t001/video.webm", Bytes: 10}},
			Walkthroughs: []evidence.Walkthrough{{Title: "w", Status: "passed", Video: "t001/video.webm"}}}}
	var cfg config.Config
	cfg.Evidence.Publish.Video = true
	all := map[string]evidence.Manifest{"F1": m}
	site := evidence.ForClient(m, true, cfg.Evidence.Publish.MaxVideo()).Video

	got := publishMedia(paths, cfg, all, []string{site})
	if want := filepath.Join(paths.StateDir(), "e2e", "run-1", "F1", "attempt-1", "t001", "video.webm"); got[site] != want || len(got) != 1 {
		t.Errorf("publishMedia = %v, want %s -> %s", got, site, want)
	}
	if got := publishMedia(paths, cfg, all, nil); len(got) != 0 {
		t.Errorf("media the snapshot does not show = %v", got)
	}
	cfg.Evidence.Publish.Video = false
	if got := publishMedia(paths, cfg, all, []string{site}); len(got) != 0 {
		t.Errorf("without the opt-in = %v", got)
	}
}
