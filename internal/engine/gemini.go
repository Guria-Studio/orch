package engine

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// GeminiStreamJSON is whether the installed gemini has `--output-format
// stream-json`, which reports a run's tokens (#543). Probed once per process
// from `gemini --help`: an older gemini rejects the flag, and a dispatch that
// cannot start is worse than one whose spend is estimated. A var so tests
// can pin it.
var GeminiStreamJSON = sync.OnceValue(probeGeminiStreamJSON)

// probeGeminiStreamJSON reads gemini's help for the stream-json choice;
// false when gemini cannot run.
func probeGeminiStreamJSON() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gemini", "--help")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	cmd.Stdout = &out
	if cmd.Run() != nil {
		return false
	}
	return strings.Contains(out.String(), "stream-json")
}

// charsPerToken is the rule of thumb an estimate divides by: about four
// characters of English or code to a token.
const charsPerToken = 4

// estimateTokens is a text size in tokens, rounded up.
func estimateTokens(chars int64) int {
	return int((chars + charsPerToken - 1) / charsPerToken)
}
