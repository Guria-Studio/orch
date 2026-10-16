package engine

import (
	"strconv"
	"strings"
	"sync"

	"github.com/hectorcanaimero/orch/internal/config"
)

// Parallel tasks run in sibling worktrees of one repository, and docker
// compose knows nothing of worktrees: a compose file with a fixed top-level
// `name:` puts every task's containers in one project, and one task's
// `docker compose up` recreates — or its `down` removes — the containers
// another task is testing against (#343). These are what orch gives each task
// so that does not happen by default:
//
//   - COMPOSE_PROJECT_NAME, `<project>-<task id>` (dispatch.isolate_compose,
//     on by default). It beats a top-level `name:`; only `-p` beats it, which
//     is why the prompt asks the agent not to pass one.
//   - ORCH_TASK_SLOT, a small integer no other running task holds, for the
//     host ports a compose file publishes: `"${API_PORT:-3001}:3001"` with
//     API_PORT derived from the slot. orch never rewrites a compose file.
//   - ORCH_TASK_ID, the task's id.
const (
	envComposeProject = "COMPOSE_PROJECT_NAME"
	envTaskSlot       = "ORCH_TASK_SLOT"
	envTaskID         = "ORCH_TASK_ID"
)

// TaskSlots hands each running task a small integer no other running task in
// this run holds. A task's slot is the lowest free one, starting at 1, and
// goes back to the pool when the task finishes, so a project can derive
// per-task host ports from it (`API_PORT=$((3000 + ORCH_TASK_SLOT))`) and two
// parallel tasks never publish the same one.
//
// 1, not 0: base + 0 is the port the operator's own stack in the main
// checkout already publishes.
type TaskSlots struct {
	mu     sync.Mutex
	byTask map[string]int
	taken  map[int]bool
}

// NewTaskSlots returns an empty pool.
func NewTaskSlots() *TaskSlots {
	return &TaskSlots{byTask: map[string]int{}, taken: map[int]bool{}}
}

// Acquire returns taskID's slot, taking the lowest free one if it holds none.
func (t *TaskSlots) Acquire(taskID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if slot, ok := t.byTask[taskID]; ok {
		return slot
	}
	slot := 1
	for t.taken[slot] {
		slot++
	}
	t.taken[slot] = true
	t.byTask[taskID] = slot
	return slot
}

// Release gives taskID's slot back. A task holding none is a no-op.
func (t *TaskSlots) Release(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if slot, ok := t.byTask[taskID]; ok {
		delete(t.taken, slot)
		delete(t.byTask, taskID)
	}
}

// Slot reports taskID's slot, if it holds one.
func (t *TaskSlots) Slot(taskID string) (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	slot, ok := t.byTask[taskID]
	return slot, ok
}

// ComposeProjectName is the docker compose project a task's containers run
// under: `<project>-<task id>`, lowercased, with every run of characters
// compose refuses in a project name (anything but a-z, 0-9, `-` and `_`)
// turned into one `-`, and starting with a letter or digit as compose
// requires. An empty half is left out; both empty is "orch".
func ComposeProjectName(project, taskID string) string {
	var parts []string
	for _, s := range []string{project, taskID} {
		if c := composeSafe(s); c != "" {
			parts = append(parts, c)
		}
	}
	if len(parts) == 0 {
		return "orch"
	}
	return strings.Join(parts, "-")
}

// composeSafe lowercases s, maps every run of characters compose does not
// accept to one `-`, and trims `-` and `_` from both ends.
func composeSafe(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
			dash = r == '-'
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-_")
}

// composeProject is taskID's compose project, or "" with
// dispatch.isolate_compose off.
func (s *Scheduler) composeProject(taskID string) string {
	if !s.Opts.Cfg.Dispatch.IsolateCompose {
		return ""
	}
	project := s.Opts.ProjectID
	if project == "" {
		project = config.DefaultProjectID(s.Opts.Cwd)
	}
	return ComposeProjectName(project, taskID)
}

// taskEnv is what a task's agent and its worktree_setup are told on top of
// the inherited environment: its id, its slot while it holds one, and its
// compose project when isolation is on.
func (s *Scheduler) taskEnv(taskID string) []string {
	env := []string{envTaskID + "=" + taskID}
	if slot, ok := s.slots.Slot(taskID); ok {
		env = append(env, envTaskSlot+"="+strconv.Itoa(slot))
	}
	if name := s.composeProject(taskID); name != "" {
		env = append(env, envComposeProject+"="+name)
	}
	return env
}

// withEnv is env with every variable vars sets replaced by vars' value, so a
// COMPOSE_PROJECT_NAME the operator's shell exported does not sit next to the
// task's own with the child left to pick one.
func withEnv(env, vars []string) []string {
	set := make(map[string]bool, len(vars))
	for _, kv := range vars {
		key, _, _ := strings.Cut(kv, "=")
		set[key] = true
	}
	out := make([]string, 0, len(env)+len(vars))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if !set[key] {
			out = append(out, kv)
		}
	}
	return append(out, vars...)
}
