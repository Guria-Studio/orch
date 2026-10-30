package prompt

import (
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
)

// TestSpecLineSaysWhenTheSpecIsNotThere: #341 and #342 were agents told to
// "READ FIRST" a file that was not in their working dir. The line now says
// what the dispatcher found, and the unchecked line stays the one the goldens
// pin.
func TestSpecLineSaysWhenTheSpecIsNotThere(t *testing.T) {
	tests := []struct {
		name        string
		state       SpecState
		wantLine    string
		wantWarning string
	}{
		{
			name:     "found, or not checked",
			state:    SpecUnchecked,
			wantLine: "Spec ref (READ FIRST): specs/f11.md#F11.1.T1",
		},
		{
			name:        "the file is not there",
			state:       SpecFileMissing,
			wantLine:    "Spec ref (READ FIRST): specs/f11.md#F11.1.T1 (NOT FOUND in your working dir — do not look for it; take the acceptance criteria from the description)",
			wantWarning: "spec file of f11.md#F11.1.T1 is not in the working dir — the agent proceeds with the description only",
		},
		{
			name:        "the file has no heading for the anchor",
			state:       SpecAnchorMissing,
			wantLine:    "Spec ref (READ FIRST): specs/f11.md#F11.1.T1 (the file has no heading for F11.1.T1 — read it for context, and take the acceptance criteria from the description)",
			wantWarning: "spec ref f11.md#F11.1.T1: the file has no heading for F11.1.T1 — the agent relies on the description",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := model.Task{ID: "F11.1.T1", Title: "Load", Model: "claude/opus"}
			body, warnings := Render(task, nil, "f11.md#F11.1.T1", Options{ProjectRoot: "/p", Spec: tt.state})
			var line string
			for _, l := range strings.Split(body, "\n") {
				if strings.HasPrefix(l, "Spec ref (READ FIRST):") {
					line = l
				}
			}
			if line != tt.wantLine {
				t.Errorf("spec line = %q\nwant        %q", line, tt.wantLine)
			}
			var want []string
			if tt.wantWarning != "" {
				want = []string{tt.wantWarning}
			}
			if strings.Join(warnings, "|") != strings.Join(want, "|") {
				t.Errorf("warnings = %q, want %q", warnings, want)
			}
		})
	}
}

// TestNoSpecRefIgnoresTheSpecState: with no spec ref there is nothing to have
// found, and the existing "no spec ref" line wins whatever the dispatcher says.
func TestNoSpecRefIgnoresTheSpecState(t *testing.T) {
	task := model.Task{ID: "X", Title: "x", Model: "m"}
	plain, _ := Render(task, nil, "", Options{ProjectRoot: "/p"})
	got, _ := Render(task, nil, "", Options{ProjectRoot: "/p", Spec: SpecFileMissing})
	if got != plain {
		t.Errorf("a spec state changed the no-spec-ref prompt:\n%s", got)
	}
}
