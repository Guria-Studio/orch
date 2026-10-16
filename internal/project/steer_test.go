package project_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/engine"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/project"
	"github.com/hectorcanaimero/orch/internal/state"
)

// TestTheRunsAnswerToAStopIsNotDroppedAsADuplicate: #356. The run answers a
// request in the same second, in the same run, for the same task and with the
// same event type; the event dedup hash must still tell the two rows apart,
// or `orch task stop` waits for an answer that was thrown away.
func TestTheRunsAnswerToAStopIsNotDroppedAsADuplicate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, _, err := state.Open(ctx, filepath.Join(dir, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", dir)
	tk := model.Task{ID: "T-1", Title: "t", Model: "claude/opus", Status: model.StatusTodo}
	if err := backend.Bootstrap(ctx, []model.Task{tk}); err != nil {
		t.Fatal(err)
	}
	if err := backend.StartRun(ctx, "r1", "auto"); err != nil {
		t.Fatal(err)
	}

	// A run holds the lock.
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	lock, err := engine.AcquireRunLock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	steer := project.Steer{Backend: backend, StateDir: stateDir, Source: "cli"}
	res, err := steer.Stop(ctx, "T-1", false, "")
	if err != nil || !res.Requested {
		t.Fatalf("Stop = %+v, %v; want a request", res, err)
	}

	q, err := engine.NewTaskQueue([]model.Task{tk})
	if err != nil {
		t.Fatal(err)
	}
	s := engine.NewScheduler(q, nil, engine.SchedulerOptions{RunID: "r1"})
	s.Backend = engine.NewStateRecorder(backend)
	s.StopTask(ctx, "T-1", false)

	ev, ok := steer.WaitStopped(ctx, res, 2*time.Second)
	if !ok || ev.Extra["stopped"] != "not_running" {
		t.Fatalf("WaitStopped = %+v, %v; want the run's not_running answer", ev, ok)
	}
}

// TestStopWithNoRunIsAPlainChange, and a note that does not move the task.
func TestStopWithNoRunIsAPlainChange(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, _, err := state.Open(ctx, filepath.Join(dir, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", dir)
	if err := backend.Bootstrap(ctx, []model.Task{{ID: "T-1", Title: "t", Status: model.StatusInProgress}}); err != nil {
		t.Fatal(err)
	}
	// A lock file nobody holds is no run.
	if err := os.WriteFile(filepath.Join(dir, project.RunLockFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	steer := project.Steer{Backend: backend, StateDir: dir}
	res, err := steer.Stop(ctx, "T-1", true, "wait for the API")
	if err != nil || res.Requested || res.To != model.StatusBlocked {
		t.Fatalf("Stop = %+v, %v", res, err)
	}
	if res.Message != "no orch run is running: T-1 in-progress -> blocked" {
		t.Errorf("message = %q", res.Message)
	}
	if err := steer.Note(ctx, "T-1", " "); err == nil {
		t.Error("an empty note was accepted")
	}
	// The flock this test never took is still free.
	f, _ := os.Open(filepath.Join(dir, project.RunLockFile)) // #nosec G304 -- a temp dir
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Errorf("Stop left the run lock taken: %v", err)
	}
}
