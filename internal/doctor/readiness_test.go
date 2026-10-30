package doctor

import (
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
)

func task(id string, st model.Status, deps ...string) model.Task {
	return model.Task{ID: id, Status: st, Dependencies: deps}
}

// #392: backlog tasks whose dependencies are done are named, with the command
// that promotes them, and they are the blocker when nothing is todo.
func TestCheckReadiness(t *testing.T) {
	checks := CheckReadiness([]model.Task{
		task("T1", model.StatusDone),
		task("T2", model.StatusBacklog, "T1"),
		task("T3", model.StatusBacklog, "T2"),
	})
	ready, backlog := checks[0], checks[1]
	if ready.Name != "tasks.ready" || ready.Status != StatusWarn {
		t.Errorf("ready = %+v, want a warning", ready)
	}
	if backlog.Name != "tasks.backlog" || backlog.Status != StatusWarn ||
		!strings.Contains(backlog.Detail, "T2") || strings.Contains(backlog.Detail, "T3") ||
		!strings.Contains(backlog.Remediation, "orch task-status T2 todo") {
		t.Errorf("backlog = %+v, want T2 alone, with its promote command", backlog)
	}

	checks = CheckReadiness([]model.Task{task("T1", model.StatusTodo), task("T2", model.StatusBacklog)})
	if checks[0].Status != StatusOK || !strings.Contains(checks[0].Detail, "T1") {
		t.Errorf("ready = %+v, want ok naming T1", checks[0])
	}
	if checks[1].Status != StatusOK {
		t.Errorf("backlog with a task ready = %+v, want ok (not a blocker)", checks[1])
	}

	checks = CheckReadiness([]model.Task{task("T1", model.StatusDone)})
	if checks[0].Status != StatusOK || checks[1].Status != StatusSkip {
		t.Errorf("all done = %+v", checks)
	}
	if checks = CheckReadiness(nil); checks[0].Status != StatusWarn || !strings.Contains(checks[0].Remediation, "orch start") {
		t.Errorf("no tasks = %+v", checks[0])
	}
}

// The verdict names the first blocker in section order: config, backends,
// tasks, permissions. A warning elsewhere does not block.
func TestVerdict(t *testing.T) {
	checks := []Check{
		{Name: "mcp.config", Status: StatusWarn},
		{Name: "codex.permissions", Status: StatusWarn, Detail: "no --add-dir"},
		{Name: "tasks.ready", Status: StatusOK},
		{Name: "backend.claude", Status: StatusOK},
	}
	SetSections(checks)
	if checks[1].Section != "permissions" || checks[0].Section != "environment" || checks[3].Section != "backends" {
		t.Errorf("sections = %+v", checks)
	}
	v := ReadinessVerdict(checks)
	if v.Ready || v.Blocker == nil || v.Blocker.Name != "codex.permissions" {
		t.Errorf("verdict = %+v, want blocked on codex.permissions", v)
	}
	checks = append(checks, Check{Name: "router.parse", Status: StatusError})
	SetSections(checks)
	if v := ReadinessVerdict(checks); v.Blocker == nil || v.Blocker.Name != "router.parse" {
		t.Errorf("verdict = %+v, want the config error first", v)
	}
	if v := ReadinessVerdict(checks[:1]); !v.Ready || v.Blocker != nil {
		t.Errorf("only an environment warning: %+v, want ready", v)
	}
	// models.available is about what a backend serves, and a hint that never
	// fails a run (#397): Backends, and its warning does not block.
	hint := []Check{{Name: "models.available", Status: StatusWarn}}
	SetSections(hint)
	if hint[0].Section != "backends" || !ReadinessVerdict(hint).Ready {
		t.Errorf("models.available = %+v, verdict %+v; want backends, ready", hint[0], ReadinessVerdict(hint))
	}
}
