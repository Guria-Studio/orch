package providers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
)

// GeminiProvider adapts the `gemini` CLI. Port of dispatcher.py's
// GeminiBackend.
//
// A gemini with `--output-format stream-json` (0.59 has it) is run with it
// when the engine's probe finds it, and reports its real tokens (#543). An
// older one prints plain text with no usage at all: its tokens are zero and
// Estimated, and the engine estimates them from the prompt and output size
// (engine.spendRow). Either way pricing.yaml prices the tokens; gemini
// reports no dollar figure.
type GeminiProvider struct{}

func (GeminiProvider) Name() model.Backend { return model.BackendGemini }

// PromptDelivery is PromptArg: gemini takes the prompt as the value of `-p`,
// and the engine closes the child's stdin.
func (GeminiProvider) PromptDelivery() PromptDelivery { return PromptArg }

// HeadlessEnv is the environment a backend's CLI needs, on top of the
// operator's, to run unattended in a task's worktree. Only gemini needs any.
//
// gemini 0.59 refuses a directory it was not told to trust (exit 55,
// testdata/gemini/0.59.0/untrusted-directory.log), and every dispatch runs in
// a fresh worktree beside the repo, never inside a folder the operator
// trusted. GEMINI_CLI_TRUST_WORKSPACE=true lifts that gate the way claude's
// acceptEdits and codex's approval flags already open the same door; it is an
// environment variable rather than `--skip-trust` because a gemini that
// predates it ignores the variable but would reject the flag.
func HeadlessEnv(b model.Backend) []string {
	if b == model.BackendGemini {
		return []string{"GEMINI_CLI_TRUST_WORKSPACE=true"}
	}
	return nil
}

// Argv returns the argv for `gemini -p <prompt> --model <model>`.
//
// There is no working-directory flag. Python sets `PWD` in the child's
// environment on top of the usual working directory; that is the engine's
// half of the contract, not the argv's.
func (GeminiProvider) Argv(req Request) []string {
	argv := []string{
		"gemini",
		"-p", req.PromptText,
		"--model", req.Route.CLIModel,
	}
	if req.StreamJSON {
		argv = append(argv, "--output-format", "stream-json")
	}
	return argv
}

// Parse reports success purely from the exit code, and quotes the last
// non-empty line as the failure reason.
//
// That last line is what carries the diagnosis in practice: an
// unauthenticated gemini 0.59.0 exits 41 with a single line naming the three
// environment variables it would accept
// (testdata/gemini/0.59.0/auth-error.log).
func (GeminiProvider) Parse(exitCode int, output []byte) Result {
	text := string(output)
	success := exitCode == 0
	if r, ok := parseGeminiStream(exitCode, text); ok {
		return r
	}

	var errMsg, answer string
	if success {
		answer = strings.TrimSpace(text)
	} else if last := lastNonEmptyLine(text); last != "" {
		errMsg = last
	} else {
		errMsg = fmt.Sprintf("gemini exited with code %d", exitCode)
	}

	return Result{
		ExitCode:     exitCode,
		Success:      success,
		Stdout:       text,
		Text:         answer,
		ErrorMessage: errMsg,
		// The zeros above are "not reported", not "free": the spend row says
		// so, and the budget gate weighs it as a typical dispatch.
		Estimated: true,
	}
}

// geminiEvent is the part of a stream-json line Parse reads.
type geminiEvent struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Status  string `json:"status"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
	Stats *struct {
		Total  int `json:"total_tokens"`
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
		Cached int `json:"cached"`
	} `json:"stats"`
}

// parseGeminiStream reads a `--output-format stream-json` run
// (testdata/gemini/0.59.0/stream-*.jsonl), ok false when output holds no
// result event — a plain-text run, or one killed before it finished.
//
// Lines that are not JSON are skipped: the engine merges stderr into the log,
// and gemini prints notices and stack traces there.
//
// The tokens are the result's stats. input_tokens counts the cached ones too
// (`input` is the rest), which is TokensIn's meaning. The stats name no
// thoughts, but total_tokens counts them: what the total holds beyond input
// and output is the thinking gemini bills as output, so it is both added to
// TokensOut and reported as ReasoningTokens.
func parseGeminiStream(exitCode int, text string) (Result, bool) {
	var answer strings.Builder
	var result *geminiEvent
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev geminiEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "message" && ev.Role == "assistant":
			answer.WriteString(ev.Content)
		case ev.Type == "result":
			result = &ev
		}
	}
	if result == nil {
		return Result{}, false
	}
	r := Result{
		ExitCode: exitCode,
		Success:  exitCode == 0 && result.Status == "success",
		Stdout:   text,
		Text:     strings.TrimSpace(answer.String()),
	}
	if s := result.Stats; s != nil {
		r.TokensIn, r.CacheReadTokens = s.Input, s.Cached
		r.ReasoningTokens = max(s.Total-s.Input-s.Output, 0)
		r.TokensOut = s.Output + r.ReasoningTokens
	} else {
		r.Estimated = true
	}
	if !r.Success {
		switch {
		case result.Error != nil && result.Error.Message != "":
			r.ErrorMessage = result.Error.Message
		case exitCode != 0:
			r.ErrorMessage = fmt.Sprintf("gemini exited with code %d", exitCode)
		default:
			r.ErrorMessage = "gemini reported status " + result.Status
		}
		r.Text = ""
	}
	return r, true
}

// ExtractCost reports the token counts of a stream-json run; the USD figure is
// always zero, since gemini reports none and pricing.yaml prices the tokens.
func (GeminiProvider) ExtractCost(output []byte) (cost float64, tokensIn, tokensOut int) {
	r, _ := parseGeminiStream(0, string(output))
	return 0, r.TokensIn, r.TokensOut
}

// lastNonEmptyLine returns the final line of s that is not blank once
// trimmed, mirroring Python's `[ln.strip() for ln in text.splitlines() if
// ln.strip()][-1]`.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
