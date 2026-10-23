package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/doctor"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/router"
	"github.com/hectorcanaimero/orch/internal/scaffold"
)

// permissionCommands are the CLIs orch writes rules for, and the command
// that writes them. The others need none (codex is granted per dispatch,
// opencode and muse run without approvals) or orch does not know (gemini):
// `orch doctor` reports each one.
var permissionCommands = map[string]string{
	"claude": doctor.ClaudePermissionsCommand,
	"agy":    doctor.AgyPermissionsCommand,
}

// usedBackends is every backend the project routes to: its tasks' models and
// the default models init stamped, which new tasks get. Unlike
// doctor.ReferencedBackends there is no fallback: a CLI nothing routes to is
// not used.
func usedBackends(tasks []model.Task, rtr router.Router, tasksJSON string) []string {
	used := map[string]bool{}
	for _, t := range tasks {
		if b := routeBackend(rtr, t.Model); b != "" {
			used[b] = true
		}
	}
	for _, key := range tierDefaults(tasksJSON) {
		if b := routeBackend(rtr, key); b != "" {
			used[b] = true
		}
	}
	out := make([]string, 0, len(used))
	for b := range used {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// projectBackends is usedBackends for the project at root, read from its
// files; a file that does not load contributes nothing.
func projectBackends(root string) []string {
	paths, err := config.ResolvePaths(root, "", "")
	if err != nil {
		return nil
	}
	return usedBackends(loadDAG(paths), loadRouterTolerant(paths), paths.TasksJSON())
}

// routeBackend is the backend a model key routes to — the project's router,
// else the packaged one — or "" when neither knows it.
func routeBackend(rtr router.Router, key string) string {
	if e, ok := rtr[key]; ok {
		return string(e.Backend)
	}
	if packaged, err := scaffold.PackagedRouter(); err == nil {
		if e, ok := packaged[key]; ok {
			return string(e.Backend)
		}
	}
	return ""
}

// grantPermissions is what init and start do (#400): add orch's rules for
// each used CLI that is on PATH and lacks them, printing the file and the
// rules. ask nil writes without asking. With skip set it writes nothing and
// names the commands that would.
func grantPermissions(ask func(question string) (bool, error), out io.Writer, root string, used []string, skip bool) error {
	for _, b := range used {
		cmd, ok := permissionCommands[b]
		if !ok || !router.OnPath(model.Backend(b)) || !missingPermissions(b, root) {
			continue
		}
		_, _ = fmt.Fprintln(out)
		if skip {
			_, _ = fmt.Fprintf(out, "%s agents cannot report a task done yet (--no-permissions); `%s` adds the rules.\n", b, cmd)
			continue
		}
		var err error
		if b == "claude" {
			err = offerClaudePermissions(ask, out, root, false)
		} else {
			err = offerAgyPermissions(ask, out, false, false)
		}
		if err != nil {
			return fmt.Errorf("writing %s's settings failed: %w", b, err)
		}
	}
	return nil
}

func missingPermissions(backend, root string) bool {
	if backend == "claude" {
		return len(doctor.MissingOrchRules(root)) > 0
	}
	path := agySettingsPath()
	return path != "" && len(doctor.MissingAgyRules(path, doctor.AgyToolRules)) > 0
}

// warnMissingPermissions is `orch run`'s guard (#400): a used CLI on PATH
// whose rules are missing is named before the first dispatch, not found out
// after the attempt fails.
func warnMissingPermissions(w io.Writer, root string, used []string) {
	for _, b := range used {
		if cmd, ok := permissionCommands[b]; ok && router.OnPath(model.Backend(b)) && missingPermissions(b, root) {
			_, _ = fmt.Fprintf(w, "note: %s agents cannot report a task done or blocked: orch's rules are missing; `%s` adds them.\n", b, cmd)
		}
	}
}
