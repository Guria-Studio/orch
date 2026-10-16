package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRunLockExcludesASecondRun: #344. Two `orch run` on one project raced
// over the same tasks, each adopting the other's writes and re-dispatching
// work already being retried. The second one now stops, naming the first.
func TestRunLockExcludesASecondRun(t *testing.T) {
	dir := t.TempDir()

	first, err := AcquireRunLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	second, err := AcquireRunLock(dir)
	if second != nil {
		second.Release()
		t.Fatal("a second run took the lock the first one holds")
	}
	var held *RunLockHeldError
	if !errors.As(err, &held) {
		t.Fatalf("second acquire: err = %v, want a *RunLockHeldError", err)
	}
	if held.PID != os.Getpid() {
		t.Errorf("holder pid = %d, want %d", held.PID, os.Getpid())
	}
	msg := err.Error()
	for _, want := range []string{"pid " + strconv.Itoa(os.Getpid()), "kill " + strconv.Itoa(os.Getpid())} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not say %q: %s", want, msg)
		}
	}

	first.Release()
	third, err := AcquireRunLock(dir)
	if err != nil || third == nil {
		t.Fatalf("after the first run released it: lock=%v err=%v", third, err)
	}
	third.Release()
}

// A run that died (SIGKILL, a crash) leaves its lock file behind with its pid
// in it, but the kernel released the flock with the process. The next run
// takes it over and writes its own pid.
func TestRunLockTakesOverAStaleLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RunLockFile)
	if err := os.WriteFile(path, []byte("pid=999999999\nstarted=2026-10-12T09:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	lock, err := AcquireRunLock(dir)
	if err != nil || lock == nil {
		t.Fatalf("acquire over a stale lock: lock=%v err=%v", lock, err)
	}
	defer lock.Release()

	b, err := os.ReadFile(path) // #nosec G304 -- a temp dir this test made
	if err != nil {
		t.Fatal(err)
	}
	if want := "pid=" + strconv.Itoa(os.Getpid()) + "\n"; !strings.HasPrefix(string(b), want) {
		t.Errorf("lock file = %q, want it to start with %q", b, want)
	}
	if !strings.Contains(string(b), "started=") {
		t.Errorf("lock file = %q, want the start time", b)
	}
}

// Release is safe on nil and twice, like TaskLock's.
func TestRunLockReleaseIsIdempotent(t *testing.T) {
	var nilLock *RunLock
	nilLock.Release()

	lock, err := AcquireRunLock(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lock.Release()
	lock.Release()
}

// A run that ends cleanly empties the file, so the next run's takeover notice
// only ever names a run that died holding the lock.
func TestRunLockReleaseEmptiesTheFile(t *testing.T) {
	dir := t.TempDir()
	lock, err := AcquireRunLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	lock.Release()
	b, err := os.ReadFile(filepath.Join(dir, RunLockFile)) // #nosec G304 -- a temp dir this test made
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 0 {
		t.Errorf("lock file after Release = %q, want empty", b)
	}
}
