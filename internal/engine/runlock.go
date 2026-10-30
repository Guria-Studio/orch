package engine

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Guria-Studio/orch/internal/project"
)

// RunLockFile is the file under a project's state directory that one `orch
// run` holds for as long as it runs.
const RunLockFile = project.RunLockFile

// RunLock is the exclusive, per-project lock `orch run` holds (#344).
//
// Two runs on one project raced over the same tasks: each adopted the other's
// status writes as "changed outside this run", re-dispatched work the other
// was already retrying, and one retried a task whose PR had just merged. A
// run that outlived its wrapper is the usual way to get there.
//
// It is a flock, like TaskLock, so the kernel drops it when the process dies
// however it dies: a lock file left by a killed run holds no lock, and the
// next run takes it over. The pid and start time written into the file are
// for the error the second run prints, not for deciding anything. Go opens
// files close-on-exec, so the agents a run forks do not inherit the lock and
// cannot keep it alive after the run is gone.
//
// Only `orch run` takes it. The dashboard and every read-only command read
// the database as before.
type RunLock struct {
	f    *os.File
	path string
}

// RunLockHeldError is AcquireRunLock's answer when another run holds the lock.
type RunLockHeldError struct {
	Path string
	// PID and Started are what the holder wrote; zero and "" when the file
	// could not be read or predates them.
	PID     int
	Started string
}

func (e *RunLockHeldError) Error() string {
	if e.PID == 0 {
		return fmt.Sprintf("another orch run is already running on this project (lock %s); "+
			"wait for it to finish or stop it before starting another", e.Path)
	}
	started := ""
	if e.Started != "" {
		started = ", started " + e.Started
	}
	return fmt.Sprintf("another orch run is already running on this project (pid %d%s); "+
		"wait for it to finish, or stop it with `kill %d` (it drains its running agents; "+
		"signal it again to kill them)", e.PID, started, e.PID)
}

// AcquireRunLock takes the run lock in stateDir, or returns a
// *RunLockHeldError naming the run that holds it. Any other error is a
// problem with the state directory itself.
func AcquireRunLock(stateDir string) (*RunLock, error) {
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return nil, fmt.Errorf("run lock: create %s: %w", stateDir, err)
	}
	path := filepath.Join(stateDir, RunLockFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- the project's own state dir
	if err != nil {
		return nil, fmt.Errorf("run lock: open %s: %w", path, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		held := readRunLock(f)
		if cerr := f.Close(); cerr != nil {
			slog.Debug("run lock: closing the file after a refused flock", "path", path, "err", cerr)
		}
		if errors.Is(err, syscall.EWOULDBLOCK) {
			held.Path = path
			return nil, held
		}
		return nil, fmt.Errorf("run lock: flock %s: %w", path, err)
	}

	if prev := readRunLock(f); prev.PID != 0 && prev.PID != os.Getpid() {
		slog.Info("taking over the run lock of an orch run that is no longer running",
			"pid", prev.PID, "started", prev.Started)
	}
	body := fmt.Sprintf("pid=%d\nstarted=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if err := f.Truncate(0); err != nil {
		// The lock is held; failing to annotate it only costs the second
		// run's message its pid, so it is reported and kept.
		slog.Warn("run lock: could not record this run's pid", "path", path, "err", err)
	} else if _, err := f.WriteAt([]byte(body), 0); err != nil {
		slog.Warn("run lock: could not record this run's pid", "path", path, "err", err)
	}
	return &RunLock{f: f, path: path}, nil
}

// readRunLock parses what a holder wrote into the lock file.
func readRunLock(f *os.File) *RunLockHeldError {
	held := &RunLockHeldError{}
	buf := make([]byte, 256)
	n, err := f.ReadAt(buf, 0)
	if n == 0 && err != nil {
		return held
	}
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "pid":
			if pid, err := strconv.Atoi(value); err == nil {
				held.PID = pid
			}
		case "started":
			held.Started = value
		}
	}
	return held
}

// Release drops the lock. Safe on a nil lock and safe to call twice. The
// file stays behind, emptied: deleting it would race a run that has just
// opened it, and emptying it is what tells the next run this one ended
// cleanly rather than died holding it.
func (l *RunLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	if err := l.f.Truncate(0); err != nil {
		slog.Debug("run lock: emptying the lock file", "path", l.path, "err", err)
	}
	if err := l.f.Close(); err != nil {
		slog.Debug("run lock: closing the lock file", "path", l.path, "err", err)
	}
	l.f = nil
}
