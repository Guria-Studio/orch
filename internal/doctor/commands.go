package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Guria-Studio/orch/internal/config"
)

// UnresolvedCommand is a shell command in config.yaml whose program is not
// on this machine: Key is its config key (`verify.commands[1]`), Command the
// value, Detail what `orch doctor` and `orch run` say about it.
type UnresolvedCommand struct {
	Key     string `json:"key"`
	Command string `json:"command"`
	Detail  string `json:"detail"`
}

// UnresolvedCommands resolves the first word of every shell command the
// config holds — verify.commands, verify.app.start/stop and verify.e2e when
// verify is on, dispatch.worktree_setup when set — and returns those whose
// program is not on PATH, or, for a path, not in the repository (#440). The
// run would find them only mid-task, as an exit 127 verify skips (#436).
//
// Only the first word is checked: `cd web && pnpm test` resolves `cd`. Shell
// builtins and words the shell expands (`$TOOL`, quotes) are taken as found.
func UnresolvedCommands(root string, cfg config.Config) []UnresolvedCommand {
	type entry struct{ key, command string }
	var all []entry
	if cfg.Dispatch.WorktreeSetup != "" {
		all = append(all, entry{"dispatch.worktree_setup", cfg.Dispatch.WorktreeSetup})
	}
	if v := cfg.Verify; v.Enabled {
		for i, c := range v.Commands {
			all = append(all, entry{fmt.Sprintf("verify.commands[%d]", i), c})
		}
		all = append(all, entry{"verify.app.start", v.App.Start}, entry{"verify.app.stop", v.App.Stop})
		for i, c := range v.E2E {
			all = append(all, entry{fmt.Sprintf("verify.e2e[%d]", i), c})
		}
	}
	var out []UnresolvedCommand
	for _, e := range all {
		word := firstWord(e.command)
		if word == "" || shellBuiltins[word] || strings.ContainsAny(word, "$`'\"\\*?") {
			continue
		}
		var where string
		if strings.Contains(word, "/") {
			p := word
			if !filepath.IsAbs(p) {
				p = filepath.Join(root, p)
			}
			if _, err := os.Stat(p); err == nil {
				continue
			}
			where = "not found in the repository"
		} else {
			if _, err := exec.LookPath(word); err == nil {
				continue
			}
			where = "not found on PATH"
		}
		out = append(out, UnresolvedCommand{Key: e.key, Command: e.command,
			Detail: fmt.Sprintf("%s: %q %s", e.key, word, where)})
	}
	return out
}

// firstWord is the program a shell command runs: its first word after any
// leading `VAR=value` assignments.
func firstWord(command string) string {
	for _, w := range strings.Fields(command) {
		// A subshell or group, `(cd web && make)`, runs its own first word.
		if w = strings.TrimLeft(w, "({"); w == "" {
			continue
		}
		if name, _, ok := strings.Cut(w, "="); ok && name != "" && !strings.ContainsAny(name, "/$") {
			continue
		}
		return w
	}
	return ""
}

// shellBuiltins are the words sh runs itself, plus its reserved words.
var shellBuiltins = map[string]bool{
	".": true, ":": true, "[": true, "alias": true, "bg": true, "break": true, "cd": true, "command": true,
	"continue": true, "echo": true, "eval": true, "exec": true, "exit": true, "export": true, "false": true,
	"fg": true, "getopts": true, "hash": true, "jobs": true, "kill": true, "printf": true, "pwd": true,
	"read": true, "readonly": true, "return": true, "set": true, "shift": true, "source": true, "test": true,
	"times": true, "trap": true, "true": true, "type": true, "ulimit": true, "umask": true, "unalias": true,
	"unset": true, "wait": true, "if": true, "for": true, "while": true, "until": true, "case": true,
	"!": true, "{": true, "(": true,
}

// CheckConfiguredCommands is UnresolvedCommands as doctor checks: one error
// per command, or one ok check when every program resolves.
func CheckConfiguredCommands(root string, cfg config.Config) []Check {
	const name = "config.commands"
	missing := UnresolvedCommands(root, cfg)
	if len(missing) == 0 {
		return []Check{{Name: name, Status: StatusOK, Detail: "every configured shell command's program is installed"}}
	}
	out := make([]Check, 0, len(missing))
	for _, m := range missing {
		out = append(out, Check{Name: name, Status: StatusError, Detail: m.Detail,
			Remediation: "Fix the command in .orchestrator/config.yaml or install the program; verify skips a command sh cannot find, so its check never runs."})
	}
	return out
}
