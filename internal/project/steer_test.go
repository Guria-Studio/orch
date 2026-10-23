package project_test

import (
	"context"
	"errors"
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

	ev, ok, err := steer.WaitStopped(ctx, res, 2*time.Second)
	if err != nil || !ok || ev.Extra["stopped"] != "not_running" {
		t.Fatalf("WaitStopped = %+v, %v, %v; want the run's not_running answer", ev, ok, err)
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
	// Blocked now, so a stop without --hold has nothing to do.
	res, err = steer.Stop(ctx, "T-1", false, "")
	if err != nil || res.To != model.StatusBlocked || res.Message != "no orch run is running and T-1 is blocked: nothing to stop" {
		t.Errorf("Stop on a blocked task = %+v, %v", res, err)
	}
	// The flock this test never took is still free.
	f, err := os.Open(filepath.Join(dir, project.RunLockFile)) // #nosec G304 -- a temp dir
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Errorf("Stop left the run lock taken: %v", err)
	}
}

// Move (v0.22, the board's drag): with a run live, a running task moved to
// blocked is a stop request, not a status change behind the run's back; and
// only orch run starts a task or finishes a running one.
func TestMoveLeavesARunningTaskToItsRun(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, _, err := state.Open(ctx, filepath.Join(dir, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", dir)
	if err := backend.Bootstrap(ctx, []model.Task{
		{ID: "T-1", Title: "t", Status: model.StatusInProgress},
		{ID: "T-2", Title: "u", Status: model.StatusTodo},
	}); err != nil {
		t.Fatal(err)
	}
	// A run holds its lock.
	f, err := os.Create(filepath.Join(dir, project.RunLockFile)) // #nosec G304 -- a temp dir
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	steer := project.Steer{Backend: backend, StateDir: dir, Source: "dashboard"}

	if _, err := steer.Move(ctx, "T-1", model.StatusBlocked, ""); err == nil {
		t.Error("blocking with no reason was accepted")
	}
	res, err := steer.Move(ctx, "T-1", model.StatusBlocked, "wrong approach")
	if err != nil || !res.Requested || !res.Hold {
		t.Fatalf("Move to blocked = %+v, %v; want a hold request to the run", res, err)
	}
	if rt, _ := backend.Task(ctx, "T-1"); rt.Status != model.StatusInProgress {
		t.Errorf("T-1 = %s: moved behind the run's back", rt.Status)
	}
	for _, c := range []struct {
		id string
		to model.Status
	}{{"T-1", model.StatusDone}, {"T-2", model.StatusInProgress}} {
		if _, err := steer.Move(ctx, c.id, c.to, ""); !errors.Is(err, project.ErrRunOwnsTask) {
			t.Errorf("Move %s to %s = %v, want ErrRunOwnsTask", c.id, c.to, err)
		}
	}
	if res, err := steer.Move(ctx, "T-2", model.StatusDone, ""); err != nil || res.To != model.StatusDone {
		t.Errorf("Move T-2 to done = %+v, %v", res, err)
	}
}

// One function turns a provider off and on for `orch provider` and the
// dashboard alike; a name orch has no adapter for is refused, not stored.
func TestSetProviderTurnsABackendOffAndOn(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, _, err := state.Open(ctx, filepath.Join(dir, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := project.Steer{Backend: state.NewSQLite(db, "p", dir)}

	if err := s.SetProvider(ctx, "codex", true, "  quota spent  "); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, _ := s.Backend.ProviderStates(ctx)
	if len(got) != 1 || !got[0].Disabled || got[0].Reason != "quota spent" {
		t.Fatalf("states = %+v, want codex disabled, reason trimmed", got)
	}
	if err := s.SetProvider(ctx, "codex", false, ""); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if got, _ := s.Backend.ProviderStates(ctx); got[0].Disabled {
		t.Errorf("codex still disabled: %+v", got)
	}
	if err := s.SetProvider(ctx, "ngrok", true, ""); err == nil {
		t.Error("an unknown provider was accepted")
	}
}
