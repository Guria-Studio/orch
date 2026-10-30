package providers

import (
	"fmt"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
)

// MuseProvider adapts the `muse` CLI (Muse Code, #395).
//
// `muse exec --json` streams JSONL envelopes keyed by `payload_type` rather
// than `type`. The run's verdict is the last `run.terminal.*` envelope — a
// background workflow can wake the session and end it more than once — whose
// payload says `terminal: "completed"` or `"failed"` with a `reason`, and
// carries the final answer in `text`. Usage and the subscription allowance
// come in `run.usage`. muse reports no USD figure, so pricing.yaml estimates
// spend from the tokens, as for agy.
type MuseProvider struct{}

func (MuseProvider) Name() model.Backend { return model.BackendMuse }

// PromptDelivery is PromptFile: muse 1.4.12 reads no prompt from stdin
// ("missing prompt") and wants a regular file for --prompt-file, so
// /dev/stdin is refused too.
func (MuseProvider) PromptDelivery() PromptDelivery { return PromptFile }

// Argv returns the argv for `muse exec --json --prompt-file <p> --workspace
// <cwd> --worktree off ...`.
//
// orch owns the worktree, hence `--worktree off`. Headless, the default
// approval mode hangs on the first shell call, so approvals are off and the
// agent may not ask the user anything. `--disable-sandbox` because muse's
// bwrap sandbox fails where unprivileged user namespaces are restricted
// (Ubuntu's apparmor_restrict_unprivileged_userns, testdata
// sandbox-denied.jsonl), and would keep the agent from writing the project's
// orch.db outside the workspace even where it works.
func (MuseProvider) Argv(req Request) []string {
	argv := []string{
		"muse", "exec", "--json",
		"--prompt-file", req.PromptPath,
		"--workspace", req.Cwd,
		"--worktree", "off",
		"--approval-mode", "never",
		"--disable-request-user-input",
		"--disable-sandbox",
	}
	if req.Route.CLIModel != "" {
		argv = append(argv, "--model", req.Route.CLIModel)
	}
	if req.Route.Effort != nil && *req.Route.Effort != "" {
		argv = append(argv, "--reasoning-effort", *req.Route.Effort)
	}
	return argv
}

// Parse reads muse's JSONL stream. A run succeeds when the process exited 0
// and the last run_terminal says "completed".
func (MuseProvider) Parse(exitCode int, output []byte) Result {
	var terminal, usage map[string]any
	n := 0
	for _, ev := range jsonlEvents(output) {
		pt := asString(ev.obj["payload_type"])
		if pt == "" {
			continue
		}
		n++
		payload, _ := asObject(ev.obj["payload"])
		switch {
		case strings.HasPrefix(pt, "run.terminal."):
			terminal = payload
		case pt == "run.usage":
			usage = payload
		}
	}

	status := asString(terminal["terminal"])
	success := exitCode == 0 && status == "completed"

	var errMsg string
	if !success {
		switch {
		case n == 0:
			errMsg = "muse produced no JSONL events"
		case terminal == nil:
			errMsg = fmt.Sprintf("muse did not emit a run_terminal event (exit=%d)", exitCode)
		case asString(terminal["reason"]) != "":
			errMsg = asString(terminal["reason"])
		default:
			errMsg = fmt.Sprintf("muse run ended %q (exit=%d)", status, exitCode)
		}
	}

	r := Result{
		ExitCode:     exitCode,
		Success:      success,
		Stdout:       string(output),
		Text:         asString(terminal["text"]),
		ErrorMessage: errMsg,
		Quota:        museQuota(usage),
	}
	models, _ := usage["models"].([]any)
	for _, m := range models {
		u, _ := asObject(m)
		r.TokensIn += toInt(u["input_tokens"])
		r.CacheReadTokens += toInt(u["cached_tokens"])
		// ponytail: output_tokens taken to include reasoning, as codex's do;
		// the 1.4.12 captures cannot tell. Revisit if spend looks low.
		r.TokensOut += toInt(u["output_tokens"])
		r.ReasoningTokens += toInt(u["reasoning_tokens"])
	}
	return r
}

// museWindows maps run.usage's allowance keys to QuotaWindow names: `weekly`
// is claude's seven_day; `window` is a rolling session window whose length
// muse does not state.
var museWindows = [][2]string{{"window", "session"}, {"weekly", "seven_day"}}

// museQuota reads run.usage's allowance, each window with used_percent
// (0-100) and resets_at_ms.
func museQuota(usage map[string]any) []QuotaWindow {
	allowance, ok := asObject(usage["allowance"])
	if !ok {
		return nil
	}
	var out []QuotaWindow
	for _, names := range museWindows {
		w, ok := asObject(allowance[names[0]])
		if !ok {
			continue
		}
		out = append(out, QuotaWindow{
			Window:      names[1],
			Utilization: toFloat(w["used_percent"]) / 100,
			ResetsAt:    int64(toFloat(w["resets_at_ms"]) / 1000),
		})
	}
	return out
}
