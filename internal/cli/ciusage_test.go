package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/vcs"
)

func fakeActions(t *testing.T, visibility string, usage vcs.ActionsUsage) {
	t.Helper()
	oldV, oldM := repoVisibility, actionsMinutes
	t.Cleanup(func() { repoVisibility, actionsMinutes = oldV, oldM })
	repoVisibility = func(string) (string, error) {
		if visibility == "" {
			return "", errors.New("no GitHub remote")
		}
		return visibility, nil
	}
	actionsMinutes = func(_ string, since time.Time) (vcs.ActionsUsage, error) {
		usage.Since = since
		return usage, nil
	}
}

// #363: the month's minutes, per workflow and per PR, and what the tasks not
// done would add — and whether any of it is billed.
func TestCIUsage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	if out, err := runRoot(t, "dev", "init", dir, "--template", "python-api"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	fakeActions(t, "private", vcs.ActionsUsage{Runs: 6, Minutes: 30, ByWorkflow: map[string]int{"orch-ci": 30}, PullRequests: 5, MinutesPerPR: 4.5})

	out, err := runRoot(t, "dev", "ci", "usage", "--project-root", dir)
	if err != nil {
		t.Fatalf("ci usage: %v\n%s", err, out)
	}
	// python-api ships 2 tasks, none done: 2 × 4.5 = 9.
	for _, want := range []string{"30 minutes over 6 runs", "orch-ci", "4.5 minutes each", "The 2 tasks still to do: about 9 more minutes", "private", "2,000"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}

	out, err = runRoot(t, "dev", "ci", "usage", "--json", "--project-root", dir)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Minutes          int  `json:"minutes"`
		Billed           bool `json:"billed"`
		RemainingTasks   int  `json:"remaining_tasks"`
		EstimatedMinutes int  `json:"estimated_minutes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Minutes != 30 || !got.Billed || got.RemainingTasks != 2 || got.EstimatedMinutes != 9 {
		t.Errorf("--json = %+v (%v):\n%s", got, err, out)
	}

	fakeActions(t, "public", vcs.ActionsUsage{ByWorkflow: map[string]int{}})
	if out, err = runRoot(t, "dev", "ci", "usage", "--project-root", dir); err != nil || !strings.Contains(out, "public: GitHub does not bill") {
		t.Errorf("public: %v\n%s", err, out)
	}
}

// Outside an orch project there are no tasks to estimate: the minutes alone,
// and remaining_tasks -1. A repository gh cannot read is exit 1.
func TestCIUsageOutsideAProject(t *testing.T) {
	dir := t.TempDir()
	fakeActions(t, "private", vcs.ActionsUsage{Runs: 2, Minutes: 8, ByWorkflow: map[string]int{"ci": 8}, PullRequests: 2, MinutesPerPR: 4})
	out, err := runRoot(t, "dev", "ci", "usage", "--json", "--project-root", dir)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		RemainingTasks   int `json:"remaining_tasks"`
		EstimatedMinutes int `json:"estimated_minutes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.RemainingTasks != -1 || got.EstimatedMinutes != 0 {
		t.Errorf("--json = %+v (%v):\n%s", got, err, out)
	}
	if out, err = runRoot(t, "dev", "ci", "usage", "--project-root", dir); err != nil || strings.Contains(out, "still to do") {
		t.Errorf("text: %v\n%s", err, out)
	}

	fakeActions(t, "", vcs.ActionsUsage{})
	var ee *exitError
	if _, err := runRoot(t, "dev", "ci", "usage", "--project-root", dir); !errors.As(err, &ee) || ee.code != 1 {
		t.Errorf("no GitHub remote: err = %v, want exit 1", err)
	}
}

// What orch run prints when its loop ends: the note only when init's
// placeholder has been outgrown.
func TestNoteOutgrownCI(t *testing.T) {
	root := t.TempDir()
	wf := filepath.Join(root, ".github", "workflows", "orch-ci.yml")
	if err := os.MkdirAll(filepath.Dir(wf), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wf, []byte("jobs:\n  test:\n    steps:\n      - name: Placeholder - no stack detected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if noteOutgrownCI(&b, root); b.Len() != 0 {
		t.Errorf("nothing to check yet, but: %q", b.String())
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if noteOutgrownCI(&b, root); !strings.Contains(b.String(), "orch ci setup --force") {
		t.Errorf("outgrown placeholder, note = %q", b.String())
	}
}

// A private repository's workflow leaves out the push run and setup says
// why; unknown visibility keeps it.
func TestCISetupOnAPrivateRepository(t *testing.T) {
	for _, tc := range []struct {
		visibility string
		push       bool
	}{{"private", false}, {"", true}, {"public", true}} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(".PHONY: test\ntest:\n\t./t.sh\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		fakeActions(t, tc.visibility, vcs.ActionsUsage{})
		out, err := runRoot(t, "dev", "ci", "setup", "--yes", "--project-root", root)
		if err != nil {
			t.Fatalf("%q: %v\n%s", tc.visibility, err, out)
		}
		wf, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "orch-ci.yml")) // #nosec G304 -- under t.TempDir()
		if got := strings.Contains(string(wf), "  push:\n"); got != tc.push {
			t.Errorf("%q: push run = %v, want %v\n%s", tc.visibility, got, tc.push, wf)
		}
		if said := strings.Contains(out, "bills its Actions minutes"); said == tc.push {
			t.Errorf("%q: setup said it is billed = %v:\n%s", tc.visibility, said, out)
		}
		if !strings.Contains(string(wf), "cancel-in-progress") {
			t.Errorf("%q: no concurrency block", tc.visibility)
		}
	}
}
