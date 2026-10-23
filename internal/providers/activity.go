package providers

import (
	"strings"
)

// A Step is one thing an agent did, read off one line of its streamed log:
// what the dashboard shows under a running task. It is a reading aid, not a
// record — nothing decides anything on it, so a line it does not recognise is
// simply no step.
type Step struct {
	Kind string `json:"kind"`
	// Tool is the tool's name in the CLI's own words (claude's `Read`,
	// opencode's `read`, codex's `shell`); empty unless Kind is StepTool.
	Tool string `json:"tool,omitempty"`
	// Detail is what the step was about — the file, the command, the text —
	// cut to maxStepDetail runes.
	Detail string `json:"detail,omitempty"`
}

// The kinds of Step. StepStart opens an attempt (claude's `init`, codex's
// `thread.started`; opencode has none), so a reader of a log that several
// attempts appended to can show the last one alone.
const (
	StepStart = "start"
	StepText  = "text"
	StepTool  = "tool"
	StepError = "error"
	StepEnd   = "end"
)

const maxStepDetail = 600

// ActivityOf reads one line of a dispatch log — claude's stream-json, codex's
// `--json`, opencode's `--format json` — into the steps it records. The
// formats do not share a `type` value that means different things, so the
// line says which CLI wrote it; gemini and agy write one object at the end
// and yield nothing until then. A line that is not a JSON object yields
// nothing: stderr, or the partial line a tail started in.
func ActivityOf(line []byte) []Step {
	obj, ok := decodeObject([]byte(strings.TrimSpace(string(line))))
	if !ok {
		return nil
	}
	if pt := asString(obj["payload_type"]); pt != "" {
		return museSteps(pt, obj)
	}
	switch asString(obj["type"]) {
	// claude
	case "system":
		if asString(obj["subtype"]) == "init" {
			return []Step{{Kind: StepStart}}
		}
	case "assistant":
		return claudeSteps(obj)
	case "user":
		return claudeToolErrors(obj)
	case "result":
		if pyTruthy(obj["is_error"]) {
			return []Step{{Kind: StepError, Detail: bound(asString(obj["result"]))}}
		}
		return []Step{{Kind: StepEnd, Detail: asString(obj["subtype"])}}
	// codex
	case "thread.started":
		return []Step{{Kind: StepStart}}
	case "item.started", "item.completed":
		return codexSteps(asString(obj["type"]) == "item.started", obj)
	case "turn.completed":
		return []Step{{Kind: StepEnd}}
	case "turn.failed":
		if e, ok := asObject(obj["error"]); ok {
			return []Step{{Kind: StepError, Detail: bound(asString(e["message"]))}}
		}
	// opencode
	case "text":
		if part, ok := asObject(obj["part"]); ok {
			return textStep(asString(part["text"]))
		}
	case "tool_use":
		return opencodeTool(obj)
	case "step_finish":
		if part, ok := asObject(obj["part"]); ok && asString(part["reason"]) == "stop" {
			return []Step{{Kind: StepEnd, Detail: "stop"}}
		}
	// codex and opencode both
	case "error":
		return []Step{{Kind: StepError, Detail: bound(errorText(obj))}}
	}
	return nil
}

func claudeSteps(obj map[string]any) []Step {
	msg, _ := asObject(obj["message"])
	blocks, _ := msg["content"].([]any)
	var steps []Step
	for _, b := range blocks {
		block, ok := asObject(b)
		if !ok {
			continue
		}
		switch asString(block["type"]) {
		case "text":
			steps = append(steps, textStep(asString(block["text"]))...)
		case "tool_use":
			input, _ := asObject(block["input"])
			steps = append(steps, Step{Kind: StepTool, Tool: asString(block["name"]), Detail: bound(toolTarget(input))})
		}
	}
	return steps
}

// claudeToolErrors reports the tool results that failed; a result that
// worked is the agent's business and would only bury the next step.
func claudeToolErrors(obj map[string]any) []Step {
	msg, _ := asObject(obj["message"])
	blocks, _ := msg["content"].([]any)
	var steps []Step
	for _, b := range blocks {
		block, ok := asObject(b)
		if !ok || asString(block["type"]) != "tool_result" || !pyTruthy(block["is_error"]) {
			continue
		}
		detail := asString(block["content"])
		if detail == "" {
			detail = "tool failed"
		}
		steps = append(steps, Step{Kind: StepError, Detail: bound(detail)})
	}
	return steps
}

// codexSteps reads one item. A command shows when it starts — a long build is
// worth seeing before it ends — and again only if it failed.
func codexSteps(started bool, obj map[string]any) []Step {
	item, ok := asObject(obj["item"])
	if !ok {
		return nil
	}
	switch asString(item["type"]) {
	case "command_execution":
		if started {
			return []Step{{Kind: StepTool, Tool: "shell", Detail: bound(unwrapShell(asString(item["command"])))}}
		}
		if code := toInt(item["exit_code"]); code != 0 {
			return []Step{{Kind: StepError, Detail: bound(unwrapShell(asString(item["command"])) + " exited " + pyStr(code))}}
		}
	case "agent_message":
		if !started {
			return textStep(asString(item["text"]))
		}
	case "file_change":
		if started {
			return nil
		}
		changes, _ := item["changes"].([]any)
		var paths []string
		for _, c := range changes {
			if change, ok := asObject(c); ok {
				paths = append(paths, asString(change["path"]))
			}
		}
		return []Step{{Kind: StepTool, Tool: "edit", Detail: bound(strings.Join(paths, ", "))}}
	case "error":
		if !started {
			return []Step{{Kind: StepError, Detail: bound(asString(item["message"]))}}
		}
	}
	return nil
}

// museSteps reads one muse envelope, keyed by payload_type. A tool shows once
// it has a result, since muse streams the call itself only as task lifecycle
// noise; the result's text is a JSON object naming the command.
func museSteps(pt string, obj map[string]any) []Step {
	payload, _ := asObject(obj["payload"])
	switch {
	case pt == "run.lifecycle.started":
		return []Step{{Kind: StepStart}}
	case pt == "tool.result":
		facts, _ := asObject(payload["correlation_facts"])
		result, _ := decodeObject([]byte(asString(payload["text"])))
		target := toolTarget(result)
		if asString(facts["outcome"]) == "failure" {
			return []Step{{Kind: StepError, Detail: bound(strings.TrimSpace(target + ": " + asString(result["output"])))}}
		}
		return []Step{{Kind: StepTool, Tool: asString(facts["tool_name"]), Detail: bound(target)}}
	case strings.HasPrefix(pt, "run.terminal."):
		if asString(payload["terminal"]) != "completed" {
			return []Step{{Kind: StepError, Detail: bound(asString(payload["reason"]))}}
		}
		return append(textStep(asString(payload["text"])), Step{Kind: StepEnd, Detail: "completed"})
	}
	return nil
}

func opencodeTool(obj map[string]any) []Step {
	part, ok := asObject(obj["part"])
	if !ok {
		return nil
	}
	st, _ := asObject(part["state"])
	input, _ := asObject(st["input"])
	if asString(st["status"]) == "error" {
		return []Step{{Kind: StepError, Detail: bound(asString(part["tool"]) + ": " + asString(st["error"]))}}
	}
	return []Step{{Kind: StepTool, Tool: asString(part["tool"]), Detail: bound(toolTarget(input))}}
}

// toolTarget is the one input field that says what a tool call was about,
// in the order the CLIs name them.
func toolTarget(input map[string]any) string {
	for _, key := range []string{"command", "file_path", "filePath", "path", "pattern", "url", "query", "description"} {
		if v := asString(input[key]); v != "" {
			return v
		}
	}
	return ""
}

func textStep(text string) []Step {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return []Step{{Kind: StepText, Detail: bound(text)}}
}

// errorText finds the message in codex's `{"message": ...}` and opencode's
// `{"error": {"data": {"message": ...}}}`.
func errorText(obj map[string]any) string {
	if msg := asString(obj["message"]); msg != "" {
		return msg
	}
	e, _ := asObject(obj["error"])
	if data, ok := asObject(e["data"]); ok && asString(data["message"]) != "" {
		return asString(data["message"])
	}
	if msg := asString(e["message"]); msg != "" {
		return msg
	}
	return "error"
}

// unwrapShell drops the `/bin/bash -lc '…'` codex wraps every command in.
func unwrapShell(cmd string) string {
	for _, prefix := range []string{"/bin/bash -lc ", "bash -lc ", "/bin/sh -c "} {
		if rest, ok := strings.CutPrefix(cmd, prefix); ok {
			if len(rest) >= 2 && (rest[0] == '\'' || rest[0] == '"') && rest[len(rest)-1] == rest[0] {
				return rest[1 : len(rest)-1]
			}
			return rest
		}
	}
	return cmd
}

func bound(s string) string {
	r := []rune(s)
	if len(r) <= maxStepDetail {
		return s
	}
	return string(r[:maxStepDetail]) + "…"
}
