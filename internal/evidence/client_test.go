package evidence

import (
	"strings"
	"testing"
)

func clientManifest() Manifest {
	m := passedManifest()
	m.E2E.Media = append(m.E2E.Media, Media{Test: "login › entra", Kind: "screenshot", Path: "t001/shot.png", Bytes: 3})
	m.E2E.Walkthroughs = []Walkthrough{
		{Title: "cart › fails", Status: "failed", Video: "t000/video.webm"},
		{Title: "login › entra", Status: "passed", Steps: []string{"Abre la página", "Entra"}, Video: "t001/video.webm", Screenshot: "t001/shot.png"},
	}
	return m
}

// ForClient shows the first @evidence walk-through that passed, and its
// media only when the operator opted in (§9.2), the video only under the
// size limit.
func TestForClientPicksThePassedWalkthroughAndHonoursTheOptIn(t *testing.T) {
	const dir = "e2e/run-1/F2.T3/attempt-2/"
	noTagged := clientManifest()
	noTagged.E2E.Walkthroughs = nil
	for _, tc := range []struct {
		name          string
		m             Manifest
		video         bool
		max           int64
		walk          string // the walk-through's title, "" for none
		video_, place string // VideoSrc and PosterSrc, "" for none
	}{
		{"no opt-in: steps, no media", clientManifest(), false, 4 << 20, "login › entra", "", ""},
		{"opted in", clientManifest(), true, 4 << 20, "login › entra", dir + "t001/video.webm", dir + "t001/shot.png"},
		{"video over the limit: poster only", clientManifest(), true, 5, "login › entra", "", dir + "t001/shot.png"},
		{"no @evidence test", noTagged, true, 4 << 20, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ForClient(tc.m, tc.video, tc.max)
			if title := ""; got.Walkthrough != nil {
				title = got.Walkthrough.Title
				if title != tc.walk {
					t.Errorf("walk-through = %q, want %q", title, tc.walk)
				}
			} else if tc.walk != "" {
				t.Errorf("no walk-through, want %q", tc.walk)
			}
			if got.VideoSrc != tc.video_ || got.PosterSrc != tc.place {
				t.Errorf("sources = %q, %q; want %q, %q", got.VideoSrc, got.PosterSrc, tc.video_, tc.place)
			}
			if (got.Video != "") != (tc.video_ != "") || (got.Poster != "") != (tc.place != "") {
				t.Errorf("site paths = %q, %q", got.Video, got.Poster)
			}
			for _, site := range []string{got.Video, got.Poster} {
				if site == "" {
					continue
				}
				if !strings.HasPrefix(site, "evidence/") || strings.Contains(site, "F2.T3") || strings.Contains(site, "run-1") {
					t.Errorf("site path %q is not an opaque evidence/<hash>/ path", site)
				}
			}
		})
	}
}

func TestReadAllReadsEveryTasksEvidence(t *testing.T) {
	stateDir := t.TempDir()
	for _, id := range []string{"A", "B"} {
		if err := Promote(stateDir, Manifest{Version: Version, TaskID: id, RunID: "r", Attempt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Record(stateDir, Manifest{Version: Version, TaskID: "C", RunID: "r", Attempt: 1}); err != nil {
		t.Fatal(err) // history only: not pushed, not evidence
	}
	all, err := ReadAll(stateDir)
	if err != nil || len(all) != 2 || all["A"].TaskID != "A" || all["B"].TaskID != "B" {
		t.Errorf("ReadAll = %v, %v; want A and B", all, err)
	}
	if none, err := ReadAll(t.TempDir()); err != nil || len(none) != 0 {
		t.Errorf("ReadAll without evidence = %v, %v", none, err)
	}
}
