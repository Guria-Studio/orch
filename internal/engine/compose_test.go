package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/model"
)

// composeNameRE is what docker compose accepts as a project name: lowercase
// letters, digits, dashes and underscores, starting with a letter or digit.
var composeNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func TestComposeProjectNameIsComposeValid(t *testing.T) {
	cases := []struct {
		project, taskID, want string
	}{
		{"usebot", "F1.T2", "usebot-f1-t2"},
		{"UseBot", "A-1", "usebot-a-1"},
		{"my app", "a_1", "my-app-a_1"},
		{"--weird..", "X", "weird-x"},
		{"Ñandú", "T/1", "and-t-1"},
		{"", "F1.T1", "f1-t1"},
		{"", "", "orch"},
	}
	for _, c := range cases {
		t.Run(c.project+"/"+c.taskID, func(t *testing.T) {
			got := ComposeProjectName(c.project, c.taskID)
			if got != c.want {
				t.Errorf("ComposeProjectName(%q, %q) = %q, want %q", c.project, c.taskID, got, c.want)
			}
			if !composeNameRE.MatchString(got) {
				t.Errorf("%q is not a valid compose project name", got)
			}
		})
	}
}

// Two tasks whose ids differ only by what sanitising folds away must not end
// up in one project — that is the collision the whole feature removes.
func TestComposeProjectNameKeepsDistinctTasksApart(t *testing.T) {
	a, b := ComposeProjectName("p", "F1.T1"), ComposeProjectName("p", "F1.T2")
	if a == b {
		t.Errorf("F1.T1 and F1.T2 share the compose project %q", a)
	}
}

func TestTaskSlotsTakeTheLowestFreeAndReleaseIt(t *testing.T) {
	s := NewTaskSlots()
	for i, id := range []string{"A", "B", "C"} {
		if got := s.Acquire(id); got != i+1 {
			t.Fatalf("Acquire(%s) = %d, want %d", id, got, i+1)
		}
	}
	if got := s.Acquire("A"); got != 1 {
		t.Errorf("a second Acquire for A = %d, want its own slot 1 back", got)
	}
	s.Release("B")
	if _, held := s.Slot("B"); held {
		t.Error("B still holds a slot after Release")
	}
	if got := s.Acquire("D"); got != 2 {
		t.Errorf("Acquire(D) after B finished = %d, want B's freed slot 2", got)
	}
	if got := s.Acquire("E"); got != 4 {
		t.Errorf("Acquire(E) = %d, want 4", got)
	}
	s.Release("never-acquired") // a no-op, not a panic
	if got, held := s.Slot("C"); !held || got != 3 {
		t.Errorf("Slot(C) = %d, %v, want 3, true", got, held)
	}
}

// The scheduler acquires on its own loop today, but the pool is shared with
// whatever reads it, so it must hold under concurrent use: every running
// task gets its own slot, and a full release hands slot 1 out again.
func TestTaskSlotsAreUniqueUnderConcurrency(t *testing.T) {
	const n = 64
	s := NewTaskSlots()
	got := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = s.Acquire(fmt.Sprintf("T-%d", i))
		}(i)
	}
	wg.Wait()
	sort.Ints(got)
	for i, slot := range got {
		if slot != i+1 {
			t.Fatalf("slots = %v, want exactly 1..%d once each", got, n)
		}
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Release(fmt.Sprintf("T-%d", i))
		}(i)
	}
	wg.Wait()
	if slot := s.Acquire("next"); slot != 1 {
		t.Errorf("Acquire after every release = %d, want 1", slot)
	}
}

func envValue(env []string, key string) (string, bool) {
	val, found := "", false
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			if found {
				return "<duplicate>", true
			}
			val, found = v, true
		}
	}
	return val, found
}

// Each dispatched agent is told its compose project, its task id and a slot
// no other running task holds, replacing whatever the operator's shell set.
func TestDispatchEnvGivesEachTaskItsOwnComposeProjectAndSlot(t *testing.T) {
	routes := map[string]model.RouteEntry{"claude/opus": route(model.BackendClaude, "opus", false)}
	t.Setenv("COMPOSE_PROJECT_NAME", "shared")
	t.Setenv("ORCH_TASK_SLOT", "99")
	cfg := config.Defaults()
	f := newSchedulerFixture(t, []model.Task{task("A-1", 1, "claude/opus"), task("A-2", 1, "claude/opus")}, routes, SchedulerOptions{
		Mode: ModeAuto, GlobalMax: 2, PerProvider: map[string]int{"claude": 2}, ProjectID: "UseBot", Cfg: cfg,
	})

	if _, err := f.s.Refill(context.Background()); err != nil {
		t.Fatalf("Refill: %v", err)
	}
	if len(f.s.InFlight()) != 2 {
		t.Fatalf("in flight = %d, want 2", len(f.s.InFlight()))
	}
	slots := map[string]bool{}
	for _, e := range f.s.InFlight() {
		env := e.Dispatch.Env
		want := map[string]string{
			"COMPOSE_PROJECT_NAME": "usebot-" + strings.ToLower(e.Task.ID),
			"ORCH_TASK_ID":         e.Task.ID,
		}
		for k, v := range want {
			if got, _ := envValue(env, k); got != v {
				t.Errorf("%s: %s = %q, want %q", e.Task.ID, k, got, v)
			}
		}
		slot, ok := envValue(env, "ORCH_TASK_SLOT")
		if !ok || slot == "99" || slot == "<duplicate>" {
			t.Errorf("%s: ORCH_TASK_SLOT = %q (set %v), want the task's own", e.Task.ID, slot, ok)
		}
		slots[slot] = true
	}
	if !slots["1"] || !slots["2"] {
		t.Errorf("slots = %v, want 1 and 2", slots)
	}
}

// With isolation off orch leaves COMPOSE_PROJECT_NAME alone — the operator's
// own value, if any, is what the agent gets — and says nothing about compose
// in the prompt. The slot is still handed out: ports are not only compose's.
func TestDispatchEnvLeavesComposeAloneWhenIsolationIsOff(t *testing.T) {
	routes := map[string]model.RouteEntry{"claude/opus": route(model.BackendClaude, "opus", false)}
	t.Setenv("COMPOSE_PROJECT_NAME", "mine")
	cfg := config.Defaults()
	cfg.Dispatch.IsolateCompose = false
	f := newSchedulerFixture(t, []model.Task{task("A-1", 1, "claude/opus")}, routes, SchedulerOptions{
		Mode: ModeAuto, GlobalMax: 1, PerProvider: map[string]int{"claude": 1}, ProjectID: "usebot", Cfg: cfg,
	})
	if _, err := f.s.Refill(context.Background()); err != nil {
		t.Fatalf("Refill: %v", err)
	}
	for _, e := range f.s.InFlight() {
		if got, _ := envValue(e.Dispatch.Env, "COMPOSE_PROJECT_NAME"); got != "mine" {
			t.Errorf("COMPOSE_PROJECT_NAME = %q, want the operator's own %q", got, "mine")
		}
		if got, _ := envValue(e.Dispatch.Env, "ORCH_TASK_SLOT"); got != "1" {
			t.Errorf("ORCH_TASK_SLOT = %q, want 1", got)
		}
	}
}

// The prompt names the project only when orch set it.
func TestDispatchPromptNamesTheComposeProjectOnlyWhenIsolated(t *testing.T) {
	routes := map[string]model.RouteEntry{"claude/opus": route(model.BackendClaude, "opus", false)}
	for _, on := range []bool{true, false} {
		t.Run(fmt.Sprintf("isolate=%v", on), func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Dispatch.IsolateCompose = on
			f := newSchedulerFixture(t, []model.Task{task("A-1", 1, "claude/opus")}, routes, SchedulerOptions{
				Mode: ModeAuto, GlobalMax: 1, PerProvider: map[string]int{"claude": 1}, ProjectID: "usebot", Cfg: cfg,
			})
			if _, err := f.s.Refill(context.Background()); err != nil {
				t.Fatalf("Refill: %v", err)
			}
			for _, e := range f.s.InFlight() {
				raw, err := os.ReadFile(e.Dispatch.PromptPath)
				if err != nil {
					t.Fatalf("reading the prompt: %v", err)
				}
				body := string(raw)
				if has := strings.Contains(body, "usebot-a-1"); has != on {
					t.Errorf("prompt names usebot-a-1 = %v, want %v:\n%s", has, on, body)
				}
			}
		})
	}
}

// worktree_setup runs with the same compose project and slot the agent gets,
// so containers it starts are the task's own.
func TestWorktreeSetupGetsTheTasksComposeEnv(t *testing.T) {
	cfg := config.Defaults()
	cfg.Retry = config.Retry{MaxAttempts: 2, BackoffSeconds: 5, RateLimitBackoffSeconds: 60}
	f := newReapFixture(t,
		[]model.Task{task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": okResponse()},
		SchedulerOptions{ProjectID: "usebot", Cfg: cfg, WorktreeMode: true})
	wt := &recordingWorktree{dir: t.TempDir()}
	f.s.Worktree = wt

	if _, err := f.s.Refill(context.Background()); err != nil {
		t.Fatalf("Refill: %v", err)
	}
	wt.mu.Lock()
	env := wt.setupEnv
	wt.mu.Unlock()
	for k, v := range map[string]string{
		"COMPOSE_PROJECT_NAME": "usebot-c-1", "ORCH_TASK_ID": "C-1", "ORCH_TASK_SLOT": "1",
	} {
		if got, _ := envValue(env, k); got != v {
			t.Errorf("setup env %s = %q, want %q (env %v)", k, got, v, env)
		}
	}
	if _, err := f.s.DrainWait(context.Background(), 30*time.Second); err != nil {
		t.Fatalf("DrainWait: %v", err)
	}
}

// A finished task gives its slot back, so the next task to start takes it.
func TestReapReleasesTheTasksSlot(t *testing.T) {
	f := newReapFixture(t,
		[]model.Task{task("C-1", 1, "claude/opus"), task("C-2", 1, "claude/opus", "C-1")},
		map[string]fakeResponse{"C-1": okResponse(), "C-2": okResponse()},
		SchedulerOptions{})

	f.runOnce(t)
	if _, held := f.s.slots.Slot("C-1"); held {
		t.Fatal("C-1 still holds its slot after it was reaped")
	}
	if _, err := f.s.Refill(context.Background()); err != nil {
		t.Fatalf("Refill: %v", err)
	}
	if slot, held := f.s.slots.Slot("C-2"); !held || slot != 1 {
		t.Errorf("C-2 slot = %d (held %v), want C-1's freed slot 1", slot, held)
	}
	if _, err := f.s.DrainWait(context.Background(), 30*time.Second); err != nil {
		t.Fatalf("DrainWait: %v", err)
	}
}

// A dispatch that fails before the agent starts gives its slot back too, or
// the pool leaks one per failed worktree.
func TestFailedWorktreeCreateReleasesTheSlot(t *testing.T) {
	f := newReapFixture(t,
		[]model.Task{task("C-1", 1, "claude/opus")},
		map[string]fakeResponse{"C-1": okResponse()},
		SchedulerOptions{WorktreeMode: true})
	f.s.Worktree = &recordingWorktree{dir: t.TempDir(), createErr: errors.New("disk full")}

	if _, err := f.s.Refill(context.Background()); err != nil {
		t.Fatalf("Refill: %v", err)
	}
	if slot, held := f.s.slots.Slot("C-1"); held {
		t.Errorf("C-1 holds slot %d after its worktree failed", slot)
	}
}
