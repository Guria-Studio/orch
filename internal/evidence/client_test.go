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
	m := clientManifest()

	off := ForClient(m, false, 4<<20)
	if off.Walkthrough == nil || off.Walkthrough.Title != "login › entra" || off.Video != "" || off.Poster != "" {
		t.Errorf("without the opt-in = %+v, want the steps and no media", off)
	}

	on := ForClient(m, true, 4<<20)
	if !strings.HasPrefix(on.Video, "evidence/") || !strings.HasSuffix(on.Video, "/video.webm") ||
		!strings.HasSuffix(on.Poster, "/poster.png") || on.VideoSrc != "e2e/run-1/F2.T3/attempt-2/t001/video.webm" ||
		on.PosterSrc != "e2e/run-1/F2.T3/attempt-2/t001/shot.png" {
		t.Errorf("with the opt-in = %+v", on)
	}
	for _, leak := range []string{"F2.T3", "run-1"} {
		if strings.Contains(on.Video+on.Poster, leak) {
			t.Errorf("the site path %q names %q", on.Video, leak)
		}
	}

	small := ForClient(m, true, 5)
	if small.Video != "" || small.Poster == "" {
		t.Errorf("a video over the limit = %+v, want only the poster", small)
	}

	m.E2E.Walkthroughs = nil
	if none := ForClient(m, true, 4<<20); none.Walkthrough != nil || none.Video != "" {
		t.Errorf("no @evidence test = %+v, want nothing", none)
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
