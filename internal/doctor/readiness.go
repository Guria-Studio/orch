package doctor

import (
	"fmt"
	"strings"

	"github.com/hectorcanaimero/orch/internal/graph"
	"github.com/hectorcanaimero/orch/internal/model"
)

// CheckReadiness says what `orch run` would dispatch now (tasks.ready) and
// which backlog tasks only wait to be promoted (tasks.backlog) — #392, where
// tasks sat in backlog with their dependencies done and nothing said so.
// tasks must carry the database's status (project.Hydrate), not tasks.json's.
func CheckReadiness(tasks []model.Task) []Check {
	ready := graph.Ready(tasks)
	var promotable []string
	for _, t := range graph.Parallelizable(tasks) {
		if t.Status == model.StatusBacklog {
			promotable = append(promotable, t.ID)
		}
	}
	counts := map[model.Status]int{}
	for _, t := range tasks {
		counts[t.Status]++
	}

	readyCheck := Check{Name: "tasks.ready", Status: StatusOK}
	switch {
	case len(tasks) == 0:
		readyCheck.Status = StatusWarn
		readyCheck.Detail = "tasks.json has no tasks"
		readyCheck.Remediation = `Plan them: orch start "<what you want to build>".`
	case len(ready) > 0:
		readyCheck.Detail = fmt.Sprintf("%d task(s) ready to dispatch: %s", len(ready), idList(ready))
	case counts[model.StatusDone] == len(tasks):
		readyCheck.Detail = fmt.Sprintf("all %d task(s) are done", len(tasks))
	case counts[model.StatusInProgress] > 0:
		readyCheck.Detail = fmt.Sprintf("none ready now; %d in progress", counts[model.StatusInProgress])
	default:
		readyCheck.Status = StatusWarn
		readyCheck.Detail = "no todo task has its dependencies done: `orch run` would dispatch nothing"
		readyCheck.Remediation = "Promote a backlog task (see tasks.backlog) or unblock one; `orch explain` shows where the project stands."
	}

	backlog := Check{Name: "tasks.backlog", Status: StatusSkip, Detail: "no backlog task has its dependencies done"}
	if len(promotable) > 0 {
		backlog.Status = StatusOK
		if len(ready) == 0 {
			backlog.Status = StatusWarn
		}
		backlog.Detail = fmt.Sprintf("%d backlog task(s) have their dependencies done and wait to be promoted: %s",
			len(promotable), strings.Join(promotable, ", "))
		cmds := make([]string, len(promotable))
		for i, id := range promotable {
			cmds[i] = "orch task-status " + id + " todo"
		}
		backlog.Remediation = "Promote the ones to run: " + strings.Join(cmds, "; ")
	}
	return []Check{readyCheck, backlog}
}

func idList(tasks []model.Task) string {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return strings.Join(ids, ", ")
}

// Sections are the readiness report's groups, in the order it prints them
// and the verdict looks for a blocker.
var Sections = []string{"config", "backends", "tasks", "permissions", "environment"}

// SetSections fills each check's Section from its name.
func SetSections(checks []Check) {
	for i := range checks {
		checks[i].Section = sectionOf(checks[i].Name)
	}
}

func sectionOf(name string) string {
	switch {
	case name == "models.available": // what a backend serves (#397)
		return "backends"
	case strings.HasPrefix(name, "config."), strings.HasPrefix(name, "router."), strings.HasPrefix(name, "models."),
		name == "tasks.parse", name == "tasks.valid":
		return "config"
	case strings.HasPrefix(name, "backend."):
		return "backends"
	case strings.HasPrefix(name, "tasks."):
		return "tasks"
	case strings.HasSuffix(name, ".permissions"), name == "claude.done_channel":
		return "permissions"
	}
	return "environment"
}

// Verdict is whether `orch run` can go ahead, and if not, the first thing in
// the way.
type Verdict struct {
	Ready   bool   `json:"ready"`
	Blocker *Check `json:"blocker,omitempty"`
}

// ReadinessVerdict blocks on any error, and on a warning about tasks or
// permissions — an agent that cannot report done fails its attempt after the
// work (#387). The first blocker in Sections order wins. Checks must have
// their Section set.
func ReadinessVerdict(checks []Check) Verdict {
	for _, section := range Sections {
		for i, c := range checks {
			if c.Section != section {
				continue
			}
			if c.Status == StatusError || (c.Status == StatusWarn && (section == "tasks" || section == "permissions")) {
				return Verdict{Blocker: &checks[i]}
			}
		}
	}
	return Verdict{Ready: true}
}
