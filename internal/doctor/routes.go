package doctor

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/router"
)

// CheckRoutes reports whether every task's model resolves to a route in r.
// Ports the models.resolve check from build_doctor_report
// (orchestrator/doctor.py), via router.Router.Validate rather than
// reimplementing the resolution loop.
func CheckRoutes(tasks []model.Task, r router.Router) Check {
	const name = "models.resolve"
	if len(tasks) == 0 {
		return Check{Name: name, Status: StatusSkip, Detail: "no tasks loaded — cannot check resolution"}
	}
	if len(r) == 0 {
		return Check{Name: name, Status: StatusSkip, Detail: "router did not load — cannot check resolution"}
	}
	if err := r.Validate(tasks); err != nil {
		return Check{
			Name: name, Status: StatusError,
			Detail:      err.Error(),
			Remediation: "Add the missing entries to model_router.yaml.",
		}
	}
	// #266: resolving is not enough when the route names a model the CLI
	// rejects — every dispatch would 404. Offline and a warning only; see
	// router.CLIModelWarning.
	if warnings := r.CLIModelWarnings(tasks); len(warnings) > 0 {
		return Check{
			Name: name, Status: StatusWarn,
			Detail: fmt.Sprintf("%d task(s) resolve, but %s",
				len(tasks), strings.Join(warnings, "; ")),
			Remediation: "Fix the cli_model of each route named above in model_router.yaml.",
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("%d task(s) resolve", len(tasks))}
}

// CheckModelsAvailable reports the routes in r whose CLI listed the models it
// runs now (lists, from scaffold.DiscoverModels) without the route's
// cli_model (#397). A warning only: "listed" is not "works" — an account may
// not reach a listed model, and a list may lag — so it is a hint to look, not
// a reason to refuse a run.
func CheckModelsAvailable(r router.Router, lists map[model.Backend][]string) Check {
	const name = "models.available"
	if len(lists) == 0 {
		return Check{Name: name, Status: StatusSkip, Detail: "no installed CLI that this router uses lists its models"}
	}
	checked := 0
	var missing []string
	for _, k := range r.Keys() {
		e := r[k]
		listed, ok := lists[e.Backend]
		if !ok {
			continue
		}
		checked++
		if !slices.Contains(listed, e.CLIModel) {
			missing = append(missing, fmt.Sprintf("%s (%s)", k, e.CLIModel))
		}
	}
	if len(missing) > 0 {
		return Check{
			Name: name, Status: StatusWarn,
			Detail: fmt.Sprintf("%d route(s) name a model their CLI does not list: %s",
				len(missing), strings.Join(missing, "; ")),
			Remediation: "Point each route at a model its CLI lists (`opencode models`, `agy models`, `codex debug models`), or remove it.",
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("%d route(s) on a CLI that lists its models, each listed", checked)}
}

// CheckDefaultModels reports tier defaults (tasks.json's
// meta.default_<tier>_model) whose CLI is not on PATH. It needs no tasks: a
// fresh project has none, and #376 found init defaulting two tiers to agy on
// a machine without it while every other check said ok. backendOf names the
// backend a model key routes to, "" when nothing knows.
func CheckDefaultModels(defaults map[string]string, backendOf func(string) string) Check {
	const name = "models.defaults_installed"
	if len(defaults) == 0 {
		return Check{Name: name, Status: StatusSkip, Detail: "tasks.json names no default models"}
	}
	var missing []string
	for _, tier := range []string{"premium", "standard", "cheap"} {
		key := defaults[tier]
		if key == "" {
			continue
		}
		backend := backendOf(key)
		if backend == "" {
			backend, _, _ = strings.Cut(key, "/")
		}
		if _, err := exec.LookPath(backend); err != nil {
			missing = append(missing, fmt.Sprintf("%s %s (%s is not on PATH)", tier, key, backend))
		}
	}
	if len(missing) > 0 {
		return Check{
			Name: name, Status: StatusWarn,
			Detail:      "default models need a CLI that is missing: " + strings.Join(missing, "; "),
			Remediation: "Install that CLI, or set meta.default_<tier>_model in tasks.json to a model of one you have.",
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("%d default model(s), each on an installed CLI", len(defaults))}
}
