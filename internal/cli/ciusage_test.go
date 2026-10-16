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
	if out, _ = runRoot(t, "dev", "ci", "usage", "--project-root", dir); !strings.Contains(out, "public: GitHub does not bill") {
		t.Errorf("public:\n%s", out)
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
