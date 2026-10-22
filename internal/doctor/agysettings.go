package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// AgyToolRules are the agy allow rules the dispatch prompt's fallback
// channel needs. agy run headless (`--print`) refuses every shell command it
// has no rule for, so an agent that did the work could not run
// scripts/task-finish.sh to say so, and the attempt failed (#383). A
// `command(<cmd>)` rule matches that command with any arguments; `*` and
// `:*` forms match nothing (checked against agy 1.4.2).
var AgyToolRules = []string{"command(scripts/task-finish.sh)", "command(scripts/task-block.sh)"}

// AgyAllCommandsRule lets agy run any command. agy run headless ends the
// agent's turn at the first command it refuses — an `ls` or a `go test`
// before the report is enough — so the two rules above keep an agent that
// only edits files working, and this one is what a task that runs its tests
// needs. Never added without `--all-commands`.
const AgyAllCommandsRule = "command(*)"

// AgyPermissionsCommand adds AgyToolRules to agy's settings.
const AgyPermissionsCommand = "orch agy-permissions"

// AgySettingsPath is agy's own settings file. agy reads permissions only
// from it: a project-local .agy/, .gemini/ or .antigravity/ settings.json is
// ignored, so unlike claude's, these rules are the user's, not the project's.
func AgySettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
}

// MissingAgyRules is the part of rules the settings file at path does not
// allow, in rules' order; command(*) allows them all. A missing or
// unreadable file allows nothing.
func MissingAgyRules(path string, rules []string) []string {
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if raw, err := os.ReadFile(path); err == nil { // #nosec G304 -- agy's own settings file
		_ = json.Unmarshal(raw, &settings)
	}
	allowed := map[string]bool{}
	for _, rule := range settings.Permissions.Allow {
		allowed[rule] = true
	}
	var missing []string
	for _, rule := range rules {
		if !allowed[rule] && !allowed[AgyAllCommandsRule] {
			missing = append(missing, rule)
		}
	}
	return missing
}

// CheckAgyPermissions warns when a project whose tasks route to agy runs
// with an agy that would refuse the scripts that report a task done.
func CheckAgyPermissions(path string, usesAgy bool) Check {
	const name = "agy.permissions"
	if !usesAgy {
		return Check{Name: name, Status: StatusSkip, Detail: "no task routes to agy"}
	}
	missing := MissingAgyRules(path, AgyToolRules)
	if len(missing) == 0 {
		if len(MissingAgyRules(path, []string{AgyAllCommandsRule})) == 0 {
			return Check{Name: name, Status: StatusOK, Detail: "agy settings allow every command"}
		}
		return Check{Name: name, Status: StatusOK,
			Detail: "agy settings allow task-finish.sh and task-block.sh; any other command ends the agent's turn " +
				"(`" + AgyPermissionsCommand + " --all-commands` allows them all)"}
	}
	return Check{
		Name: name, Status: StatusWarn,
		Detail: "agy agents cannot report a task done or blocked — not in " + path + ": " + strings.Join(missing, ", "),
		Remediation: "Run `" + AgyPermissionsCommand + "` (it shows the rules and asks before writing), or add them to " +
			"permissions.allow by hand. agy reads them only from that file, so they apply to every agy session.",
	}
}
