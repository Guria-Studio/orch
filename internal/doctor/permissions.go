package doctor

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"syscall"
)

// CheckPermissions is one <cli>.permissions check per backend in used: can
// that CLI's agent, run headless by orch, reach the channels that report a
// task done or blocked (#400, #392). A CLI the project does not use gets no
// check. codexHelp returns `codex exec --help`, "" when it cannot run.
func CheckPermissions(used []string, root, agySettings string, codexHelp func() string) []Check {
	out := make([]Check, 0, len(used))
	for _, b := range used {
		name := b + ".permissions"
		switch b {
		case "claude":
			out = append(out, CheckClaudePermissions(root, true))
		case "agy":
			out = append(out, CheckAgyPermissions(agySettings, true))
		case "codex":
			out = append(out, checkCodexPermissions(codexHelp()))
		case "opencode":
			out = append(out, Check{Name: name, Status: StatusOK,
				Detail: "orch runs `opencode run --auto`, which approves every permission opencode's config does not deny"})
		case "muse":
			out = append(out, Check{Name: name, Status: StatusOK,
				Detail: "orch runs `muse exec --approval-mode never --disable-sandbox`: no rule needed"})
		default:
			// gemini: orch runs `gemini -p` in its default approval mode, and
			// nothing in this repository shows whether that mode lets the
			// agent run scripts/task-finish.sh. Saying so beats guessing.
			out = append(out, Check{Name: name, Status: StatusSkip,
				Detail: "unknown: orch has not verified whether a headless " + b + " agent may run scripts/task-finish.sh; " +
					"if its tasks fail after the work is done, that is the first thing to check"})
		}
	}
	return out
}

// checkCodexPermissions: codex's workspace-write sandbox lets the agent write
// its worktree only, so orch grants the state directory with --add-dir on
// every dispatch (#387, #409). A codex without that flag cannot be granted it.
func checkCodexPermissions(help string) Check {
	const name = "codex.permissions"
	switch {
	case help == "":
		return Check{Name: name, Status: StatusSkip, Detail: "could not run `codex exec --help` — cannot check"}
	case strings.Contains(help, "--add-dir"):
		return Check{Name: name, Status: StatusOK,
			Detail: "orch grants codex's sandbox the project's state directory (--add-dir) on every dispatch"}
	}
	return Check{Name: name, Status: StatusWarn,
		Detail:      "this codex has no --add-dir: its agents cannot write the project's database from their sandbox, so they cannot report a task done (#387)",
		Remediation: "Upgrade codex (`npm i -g @openai/codex`)."}
}

// CodexExecHelp is `codex exec --help`, "" when it cannot run.
func CodexExecHelp() string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "exec", "--help")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if cmd.Run() != nil {
		return ""
	}
	return stdout.String()
}
