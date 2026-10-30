package providers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/pyfmt"
)

// ClaudeProvider adapts the `claude` CLI. Port of dispatcher.py's
// ClaudeBackend.
//
// The CLI streams one JSON event per line under `--output-format stream-json`
// — what the agent says and every tool it calls, as it happens, which is what
// lets the dashboard follow a running task — and ends with the envelope it
// used to print alone under `--output-format json`: `type: "result"`, same
// fields. A run counts as a success only when the process exited 0 AND the envelope
// says `is_error: false` AND `subtype: "success"` — all three, because 2.1.269
// will happily exit non-zero with `subtype: "success"` on an API error (see
// testdata/claude/2.1.269/unrecognized-model.log).
type ClaudeProvider struct{}

func (ClaudeProvider) Name() model.Backend { return model.BackendClaude }

func (ClaudeProvider) PromptDelivery() PromptDelivery { return PromptStdin }

// Argv returns the argv for `claude -p --output-format stream-json --verbose ...`.
//
// The order is the Python's, flag for flag, except the output format: Python
// asked for `json`, which writes nothing until the run ends, so a dispatch
// could not be followed while it ran. `stream-json` requires `--verbose`
// under `-p`. `--add-dir .` is the literal dot:
// the engine passes the real directory as the child's working directory, and
// the flag grants explicit write access to it as defence in depth.
//
// Two departures from build_cmd, both because Argv is pure:
//
//   - The session id comes from the Request instead of being minted here and
//     dug back out of the argv afterwards. An empty SessionID omits the flag
//     and lets the CLI pick its own, so a caller that forgets loses retry
//     correlation rather than emitting a broken `--session-id ""`.
//   - The budget comes from the Request instead of a config dict stored on the
//     adapter. Python looks for `claude_max_budget_usd` and then
//     `budget.per_dispatch_usd`; resolving which one wins is the engine's job
//     now, and this package never sees internal/config.
func (ClaudeProvider) Argv(req Request) []string {
	argv := []string{
		"claude",
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--model", req.Route.CLIModel,
	}
	if req.SessionID != "" {
		argv = append(argv, "--session-id", req.SessionID)
	}
	argv = append(argv,
		"--add-dir", ".",
		"--permission-mode", "acceptEdits",
	)
	if req.Settings != "" {
		argv = append(argv, "--settings", req.Settings)
	}
	if req.BudgetUSD != nil {
		argv = append(argv, "--max-budget-usd", pyfmt.Float(*req.BudgetUSD))
	}
	return argv
}

// Parse reads the JSON envelope out of a finished run's captured output.
func (c ClaudeProvider) Parse(exitCode int, output []byte) Result {
	text := string(output)

	env, ok := parseClaudeEnvelope(output)
	if !ok {
		res := Result{
			ExitCode:     exitCode,
			Success:      false,
			Stdout:       withoutStreamEvents(text),
			ErrorMessage: "could not parse claude JSON envelope",
			Quota:        claudeQuota(text),
		}
		res.QuotaExhausted, res.QuotaResetsAt = claudeExhausted(text)
		return res
	}

	isError := pyTruthy(env["is_error"])
	subtype := asString(env["subtype"])
	success := exitCode == 0 && !isError && subtype == "success"

	cost, tokensIn, tokensOut := claudeCost(env)

	var errMsg string
	if !success {
		errMsg = claudeErrorMessage(env, isError)
	}

	res := Result{
		ExitCode:            exitCode,
		Success:             success,
		CostUSD:             cost,
		TokensIn:            tokensIn,
		CacheReadTokens:     usageInt(env, "cache_read_input_tokens"),
		CacheCreationTokens: usageInt(env, "cache_creation_input_tokens"),
		TokensOut:           tokensOut,
		Stdout:              withoutStreamEvents(text),
		Text:                asString(env["result"]),
		ErrorMessage:        errMsg,
		PermissionDenials:   claudeDenials(env),
		Quota:               claudeQuota(text),
	}
	res.QuotaExhausted, res.QuotaResetsAt = claudeExhausted(text)
	return res
}

// claudeExhausted reads whether the last rate_limit_event said the
// subscription is out of quota — `status: "rejected"`, or a window at full
// utilization — and when it resets (Unix seconds, 0 when not given). The
// event is filtered out of Stdout, so the classifier never sees it (#439).
func claudeExhausted(output string) (exhausted bool, resetsAt int64) {
	for _, line := range strings.Split(output, "\n") {
		obj, ok := decodeObject([]byte(strings.TrimSpace(line)))
		if !ok || asString(obj["type"]) != "rate_limit_event" {
			continue
		}
		info, _ := obj["rate_limit_info"].(map[string]any)
		exhausted, resetsAt = asString(info["status"]) == "rejected", int64(toFloat(info["resetsAt"]))
		windows, _ := info["unifiedWindows"].(map[string]any)
		for _, w := range windows {
			w, _ := w.(map[string]any)
			if toFloat(w["utilization"]) >= 1 {
				exhausted = true
				resetsAt = max(resetsAt, int64(toFloat(w["resetsAt"])))
			}
		}
	}
	return exhausted, resetsAt
}

// claudeQuota reads the last rate_limit_event of a stream-json run: claude
// emits one per run with the subscription's windows (five_hour, seven_day),
// each with its utilization (0–1) and reset time. Windows come sorted by
// name, so the result does not depend on map order. nil when the run printed
// none — a `json` run, or one killed before the event.
func claudeQuota(output string) []QuotaWindow {
	var windows map[string]any
	for _, line := range strings.Split(output, "\n") {
		obj, ok := decodeObject([]byte(strings.TrimSpace(line)))
		if !ok || asString(obj["type"]) != "rate_limit_event" {
			continue
		}
		info, _ := obj["rate_limit_info"].(map[string]any)
		if w, ok := info["unifiedWindows"].(map[string]any); ok {
			windows = w
		}
	}
	if len(windows) == 0 {
		return nil
	}
	names := make([]string, 0, len(windows))
	for name := range windows {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]QuotaWindow, 0, len(names))
	for _, name := range names {
		w, _ := windows[name].(map[string]any)
		out = append(out, QuotaWindow{Window: name, Utilization: toFloat(w["utilization"]), ResetsAt: int64(toFloat(w["resetsAt"]))})
	}
	return out
}

// claudeDenials reads `permission_denials`: one object per refused call, with
// `tool_name` and the `tool_input` it was refused. A Bash call names its
// command, because "Bash" alone does not say what to allow.
func claudeDenials(env map[string]any) []string {
	list, _ := env["permission_denials"].([]any)
	var out []string
	for _, item := range list {
		d, ok := asObject(item)
		if !ok {
			continue
		}
		name := asString(d["tool_name"])
		if input, ok := asObject(d["tool_input"]); ok {
			if cmd := asString(input["command"]); cmd != "" {
				name += "(" + cmd + ")"
			}
		}
		out = append(out, name)
	}
	return out
}

// claudeErrorMessage ports the `err = api_error_status or terminal_reason or ""`
// chain. api_error_status is an object on some failures (a code/message pair)
// and a bare HTTP status number on others — 2.1.269 sends `404` for an
// unrecognised model, and the long-standing auth fixture sends an object.
func claudeErrorMessage(env map[string]any, isError bool) string {
	var err any
	switch {
	case pyTruthy(env["api_error_status"]):
		err = env["api_error_status"]
	case pyTruthy(env["terminal_reason"]):
		err = env["terminal_reason"]
	default:
		err = ""
	}

	if obj, ok := asObject(err); ok {
		if msg := obj["message"]; pyTruthy(msg) {
			return pyStr(msg)
		}
		return pyStr(obj)
	}

	if s := pyStr(err); s != "" {
		return s
	}
	// Python formats the bool here, so the string reads "is_error=True".
	return fmt.Sprintf("claude reported is_error=%s", pyStr(isError))
}

// claudeCost pulls `total_cost_usd` and the `usage` token counts out of the
// envelope. Port of ClaudeBackend.extract_cost, which re-parses the text;
// taking the already-decoded envelope saves a second pass and cannot disagree
// with the one Parse used.
//
// # Divergence: cache tokens are input
//
// Python read `usage.input_tokens` alone. With prompt caching that is the
// handful of tokens after the last cache breakpoint: the 2.1.269 capture
// reports 19 of the 60,132 input tokens the request processed, the rest being
// `cache_creation_input_tokens` and `cache_read_input_tokens`. The budget
// window sums these rows, so it saw a few hundred tokens per dispatch and the
// claude cap practically never tripped. All three count.
func claudeCost(env map[string]any) (cost float64, tokensIn, tokensOut int) {
	cost = toFloat(env["total_cost_usd"])
	usage, ok := asObject(env["usage"])
	if !ok {
		return cost, 0, 0
	}
	tokensIn = toInt(usage["input_tokens"]) +
		toInt(usage["cache_creation_input_tokens"]) +
		toInt(usage["cache_read_input_tokens"])
	return cost, tokensIn, toInt(usage["output_tokens"])
}

// usageInt reads one integer out of the envelope's `usage` object, 0 when
// either is missing.
func usageInt(env map[string]any, key string) int {
	usage, ok := asObject(env["usage"])
	if !ok {
		return 0
	}
	return toInt(usage[key])
}

// ExtractCost reports the spend and token counts in a captured claude log,
// without deciding whether the run succeeded. Port of extract_cost, kept
// exported because the engine's budget gate reads a log it did not parse.
func (c ClaudeProvider) ExtractCost(output []byte) (cost float64, tokensIn, tokensOut int) {
	env, ok := parseClaudeEnvelope(output)
	if !ok {
		return 0, 0, 0
	}
	return claudeCost(env)
}

// parseClaudeEnvelope extracts the top-level JSON object from claude's output.
//
// The whole capture is the envelope on a clean `json` run. When the CLI also
// wrote to stderr — which the engine merges into the same stream — the blob as
// a whole is not JSON, and the envelope is the last line that parses. Port of
// _parse_claude_envelope, including that fallback, which is also what reads a
// `stream-json` run: its envelope is the last line. A stream event is never
// the envelope, so a run killed before its `result` line has none.
func parseClaudeEnvelope(output []byte) (map[string]any, bool) {
	stripped := strings.TrimSpace(string(output))
	if stripped == "" {
		return nil, false
	}
	if obj, ok := decodeObject([]byte(stripped)); ok {
		return obj, true
	}
	lines := strings.Split(stripped, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if obj, ok := decodeObject([]byte(line)); ok && !isStreamEvent(obj) {
			return obj, true
		}
	}
	return nil, false
}

// isStreamEvent reports a `stream-json` line other than the envelope. The
// envelope says `type: "result"` in both output formats; agy's, which goes
// through parseClaudeEnvelope too, has no `type` at all.
func isStreamEvent(obj map[string]any) bool {
	typ, ok := obj["type"].(string)
	return ok && typ != "result"
}

// withoutStreamEvents is output minus its stream events: the envelope and any
// line that is not JSON (stderr) stay. Result.Stdout feeds the classifier's
// marker scan, and the events carry text that is not claude's verdict — a
// `rate_limit_event` on every run, whatever the agent's tools printed.
func withoutStreamEvents(output string) string {
	if !strings.Contains(output, "\n") {
		return output
	}
	lines := strings.SplitAfter(output, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if obj, ok := decodeObject([]byte(strings.TrimSpace(line))); ok && isStreamEvent(obj) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}
