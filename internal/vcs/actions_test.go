package vcs

import (
	"strings"
	"testing"
	"time"
)

func fakeGH(t *testing.T, replies map[string]string) {
	t.Helper()
	old := ghIn
	t.Cleanup(func() { ghIn = old })
	ghIn = func(_ string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		for key, out := range replies {
			if strings.Contains(joined, key) {
				return out, nil
			}
		}
		t.Fatalf("unexpected gh %s", joined)
		return "", nil
	}
}

// Jobs bill rounded up to the minute each; a run's minutes go to its
// workflow, and a pull request's runs to its branch.
func TestActionsMinutes(t *testing.T) {
	fakeGH(t, map[string]string{
		"actions/runs?": "11\torch-ci\tpull_request\torch/F1.T1\n" +
			"12\torch-ci\tpull_request\torch/F1.T1\n" +
			"13\torch-ci\tpull_request\torch/F1.T2\n" +
			"14\torch-ci\tpush\tmain\n",
		// two jobs: 1m01s → 2, 30s → 1
		"runs/11/jobs": "2026-10-01T10:00:00Z\t2026-10-01T10:01:01Z\n2026-10-01T10:00:00Z\t2026-10-01T10:00:30Z\n",
		"runs/12/jobs": "2026-10-02T10:00:00Z\t2026-10-02T10:04:00Z\n",
		// a job still running bills nothing yet
		"runs/13/jobs": "2026-10-03T10:00:00Z\t2026-10-03T10:02:10Z\n2026-10-03T10:00:00Z\t\n",
		"runs/14/jobs": "2026-10-03T11:00:00Z\t2026-10-03T11:05:00Z\n",
	})
	u, err := ActionsMinutes(".", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// 3 + 4 + 3 + 5
	if u.Runs != 4 || u.Minutes != 15 || u.ByWorkflow["orch-ci"] != 15 {
		t.Errorf("usage = %+v, want 4 runs, 15 minutes", u)
	}
	// F1.T1: 7, F1.T2: 3 → 5 per PR; the push run is not a PR's.
	if u.PullRequests != 2 || u.MinutesPerPR != 5 {
		t.Errorf("per PR = %d PRs, %.1f min; want 2, 5", u.PullRequests, u.MinutesPerPR)
	}
}

func TestRepoVisibility(t *testing.T) {
	fakeGH(t, map[string]string{"repo view": "PRIVATE\n"})
	if v, err := RepoVisibility("."); err != nil || v != "private" {
		t.Errorf("visibility = %q, %v", v, err)
	}
}
