package evidence

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// attempt leaves one verified attempt of task in stateDir the way the engine
// does: its manifest in history/ and its E2E directory with a file of size
// bytes.
func attempt(t *testing.T, stateDir, task, run string, n int, at time.Time, bytes int) Manifest {
	t.Helper()
	dir := filepath.ToSlash(filepath.Join("e2e", run, task, fmt.Sprintf("attempt-%d", n)))
	writeFile(t, filepath.Join(stateDir, filepath.FromSlash(dir), "t001", "video.webm"), string(make([]byte, bytes)))
	m := Manifest{Version: Version, TaskID: task, RunID: run, Attempt: n, VerifiedAt: at, Verdict: VerdictPassed,
		E2E: &E2E{Dir: dir}}
	if _, err := Record(stateDir, m); err != nil {
		t.Fatal(err)
	}
	return m
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestPruneKeepsThePushedAttempt is phase 4's acceptance (#520): with
// keep_attempts 1 a task keeps only its manifest and the pushed attempt's
// E2E directory, even when the pushed one is not the newest.
func TestPruneKeepsThePushedAttempt(t *testing.T) {
	stateDir := t.TempDir()
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	a1 := attempt(t, stateDir, "F1", "run-1", 1, t0, 10)
	a2 := attempt(t, stateDir, "F1", "run-1", 2, t0.Add(time.Hour), 10)
	a3 := attempt(t, stateDir, "F1", "run-2", 1, t0.Add(2*time.Hour), 10) // failed later, never pushed
	if err := Promote(stateDir, a2); err != nil {
		t.Fatal(err)
	}
	// An E2E directory of the task with no manifest (from before #520).
	writeFile(t, filepath.Join(stateDir, "e2e", "run-0", "F1", "attempt-1", "report.json"), "{}")
	// Another task's attempts are its own business.
	other := attempt(t, stateDir, "F2", "run-1", 1, t0, 10)

	got, err := Prune(stateDir, 1, 0)
	if err != nil {
		t.Fatal(err)
	}

	if !exists(filepath.Join(stateDir, "evidence", "F1", ManifestFile)) || !exists(filepath.Join(stateDir, a2.E2E.Dir)) {
		t.Error("the pushed attempt's manifest or E2E directory was pruned")
	}
	for _, gone := range []string{a1.E2E.Dir, a3.E2E.Dir, "e2e/run-0/F1/attempt-1",
		"evidence/F1/history/" + HistoryName("run-1", 1), "evidence/F1/history/" + HistoryName("run-2", 1)} {
		if exists(filepath.Join(stateDir, filepath.FromSlash(gone))) {
			t.Errorf("%s was kept", gone)
		}
	}
	if !exists(filepath.Join(stateDir, "evidence", "F1", "history", HistoryName("run-1", 2))) {
		t.Error("the pushed attempt's history entry was pruned")
	}
	if !exists(filepath.Join(stateDir, other.E2E.Dir)) {
		t.Error("F2's only attempt was pruned")
	}
	if got.Attempts != 3 || got.Bytes < 20 {
		t.Errorf("Prune = %+v, want 3 attempts and their bytes", got)
	}
}

// TestPruneHoldsTheSizeLimit: past max_bytes_per_task the oldest attempts
// go first, and the pushed one stays even when it alone is over.
func TestPruneHoldsTheSizeLimit(t *testing.T) {
	stateDir := t.TempDir()
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var all []Manifest
	for i := 1; i <= 3; i++ {
		all = append(all, attempt(t, stateDir, "F1", "run-1", i, t0.Add(time.Duration(i)*time.Hour), 1000))
	}
	if err := Promote(stateDir, all[0]); err != nil { // the oldest is the pushed one
		t.Fatal(err)
	}

	if _, err := Prune(stateDir, 3, 2500); err != nil {
		t.Fatal(err)
	}
	var kept []int
	for _, m := range all {
		if exists(filepath.Join(stateDir, m.E2E.Dir)) {
			kept = append(kept, m.Attempt)
		}
	}
	if !slices.Equal(kept, []int{1, 3}) {
		t.Errorf("kept attempts %v, want the pushed 1 and the newest 3 under 2500 bytes", kept)
	}

	if _, err := Prune(stateDir, 3, 10); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(stateDir, all[0].E2E.Dir)) {
		t.Error("the pushed attempt was pruned for size")
	}
}

func TestPruneWithoutEvidenceIsANoOp(t *testing.T) {
	if got, err := Prune(t.TempDir(), 1, 0); err != nil || got.Attempts != 0 {
		t.Errorf("Prune = %+v, %v; want nothing", got, err)
	}
}
