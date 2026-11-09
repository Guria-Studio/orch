package providers

import (
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
)

func geminiRoute(cliModel string) model.RouteEntry {
	return model.RouteEntry{Backend: model.BackendGemini, CLIModel: cliModel}
}

func TestGeminiArgv(t *testing.T) {
	argv := GeminiProvider{}.Argv(Request{
		Route:      geminiRoute("gemini-2.5-flash"),
		PromptText: "do the thing",
		Cwd:        "/abs/worktree",
	})

	want := []string{"gemini", "-p", "do the thing", "--model", "gemini-2.5-flash"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %q, want %q", argv, want)
		}
	}
	// No directory flag exists: the engine sets the child's working directory
	// and PWD. A --dir here would be invented, not ported.
	if contains(argv, "--dir") {
		t.Error("argv carries a --dir flag gemini does not have")
	}
}

func TestGeminiPromptDelivery(t *testing.T) {
	if (GeminiProvider{}).PromptDelivery() != PromptArg {
		t.Error("gemini takes the prompt as an argument, not on stdin")
	}
}

func TestGeminiParseRealSuccess(t *testing.T) {
	out := readFixture(t, "gemini", "0.59.0", "success.log")
	res := GeminiProvider{}.Parse(0, out)

	if !res.Success {
		t.Fatalf("want success, got %q", res.ErrorMessage)
	}
	if res.ErrorMessage != "" {
		t.Errorf("ErrorMessage = %q, want empty", res.ErrorMessage)
	}
	// gemini prints no envelope, so there is nothing to read a number out of.
	// Spend for a gemini route is estimated downstream from pricing.yaml.
	if res.CostUSD != 0 || res.TokensIn != 0 || res.TokensOut != 0 {
		t.Errorf("cost/tokens = (%v,%d,%d), want all zero",
			res.CostUSD, res.TokensIn, res.TokensOut)
	}
	// Those zeros are "not reported", not "free": the spend row must say so,
	// or the budget and cost views read a real run as costing nothing.
	if !res.Estimated {
		t.Error("Estimated = false; gemini reports no usage, so its zeros are unknown")
	}
	// The real capture opens with a tool notice ("Ripgrep is not available…")
	// and only then the answer. A hand-written fixture would have been the
	// answer alone, and would have hidden that gemini's stdout is a
	// conversation rather than a value — which is why the failure path reads
	// the LAST non-empty line and not the first.
	if !strings.Contains(string(out), "Ripgrep is not available") {
		t.Error("the preamble is gone from the fixture; this test now proves less")
	}
}

// TestGeminiParseUntrustedDirectory is bug 30, and it is the one finding in
// this batch that stops gemini working from orch at all.
//
// gemini refuses to run in a directory it has not been told to trust, exiting
// 55 before contacting any model. orch dispatches every task into a fresh git
// worktree, which is never a trusted directory, so every gemini dispatch ends
// here — in Python too, since GeminiBackend.build_cmd passes no --skip-trust
// and sets no GEMINI_CLI_TRUST_WORKSPACE (docs/brainstorm/go-migration-notes/
// sonnet-2.md). orch now sets that variable on every gemini dispatch
// (HeadlessEnv, engine's TestSpawnTrustsTheWorktreeForGemini); this test still
// pins what the output means when an operator sets it to false.
func TestGeminiParseUntrustedDirectory(t *testing.T) {
	out := readFixture(t, "gemini", "0.59.0", "untrusted-directory.log")
	res := GeminiProvider{}.Parse(55, out)

	if res.Success {
		t.Fatal("want failure")
	}
	if !strings.Contains(res.ErrorMessage, "not running in a trusted directory") {
		t.Errorf("ErrorMessage = %q", res.ErrorMessage)
	}
	// Nothing marks this as unretryable, so the reaper will try again and get
	// exit 55 again. Pinned so that changing it is deliberate.
	if got := Classify(res); got != FailureOther {
		t.Errorf("Classify = %q, want %q — if this changed, bug 30 moved",
			got, FailureOther)
	}
}

// TestGeminiParseRealAuthError reads a real gemini 0.59.0 run with no
// credentials configured. Its exit code is 41, not 1, which is why success is
// read from "exit != 0" rather than from any particular code.
func TestGeminiParseRealAuthError(t *testing.T) {
	out := readFixture(t, "gemini", "0.59.0", "auth-error.log")
	res := GeminiProvider{}.Parse(41, out)

	if res.Success {
		t.Fatal("want failure")
	}
	if res.ExitCode != 41 {
		t.Errorf("ExitCode = %d, want 41", res.ExitCode)
	}
	if !strings.Contains(res.ErrorMessage, "Please set an Auth method") {
		t.Errorf("ErrorMessage = %q, want the CLI's own line", res.ErrorMessage)
	}

	// Pinned, not endorsed. The sentence gemini prints — "Please set an Auth
	// method ... or specify one of the following environment variables" —
	// contains none of the permission markers, so a missing credential is
	// classified as FailureOther and retried like a transient blip. It cannot
	// succeed on retry. Adding a marker is a behaviour change for every
	// backend at once, so it is logged in
	// docs/brainstorm/go-migration-notes/sonnet-2.md rather than slipped in
	// here; if this assertion starts failing, that note is what to update.
	if got := Classify(res); got != FailureOther {
		t.Errorf("Classify = %q, want %q — if this now says permission, a "+
			"marker was added and the note needs closing", got, FailureOther)
	}
}

func TestGeminiParseEmptyOutput(t *testing.T) {
	res := GeminiProvider{}.Parse(2, nil)
	if res.Success {
		t.Fatal("want failure")
	}
	if res.ErrorMessage != "gemini exited with code 2" {
		t.Errorf("ErrorMessage = %q", res.ErrorMessage)
	}
}

func TestGeminiExtractCostIsAlwaysZero(t *testing.T) {
	out := readFixture(t, "gemini", "0.59.0", "auth-error.log")
	cost, in, outTok := GeminiProvider{}.ExtractCost(out)
	if cost != 0 || in != 0 || outTok != 0 {
		t.Errorf("ExtractCost = (%v,%d,%d), want zeros", cost, in, outTok)
	}
}

// With StreamJSON (a gemini that has --output-format stream-json, which the
// engine probes for), the argv asks for it; without, it stays the plain-text
// argv an older gemini accepts (#543).
func TestGeminiArgvStreamJSON(t *testing.T) {
	argv := GeminiProvider{}.Argv(Request{Route: geminiRoute("gemini-2.5-flash"), PromptText: "p", StreamJSON: true})
	want := "gemini -p p --model gemini-2.5-flash --output-format stream-json"
	if got := strings.Join(argv, " "); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// A real gemini 0.59.0 stream-json run, stdout and stderr merged as the engine
// captures them: the tokens are the CLI's own, the cache reads are counted
// apart, and the thoughts gemini bills as output are the reasoning (#543).
func TestGeminiParseRealStreamSuccess(t *testing.T) {
	out := readFixture(t, "gemini", "0.59.0", "stream-success.jsonl")
	res := GeminiProvider{}.Parse(0, out)

	if !res.Success || res.ErrorMessage != "" {
		t.Fatalf("want success, got %q", res.ErrorMessage)
	}
	// stats: total 14244, input_tokens 13844 (cached 11773 of them), output 170.
	if res.TokensIn != 13844 || res.CacheReadTokens != 11773 {
		t.Errorf("in = %d (cache read %d), want 13844 (11773)", res.TokensIn, res.CacheReadTokens)
	}
	if res.TokensOut != 400 || res.ReasoningTokens != 230 {
		t.Errorf("out = %d (reasoning %d), want 400 (230): the total less the input", res.TokensOut, res.ReasoningTokens)
	}
	if res.Estimated {
		t.Error("Estimated = true; the run reported its usage")
	}
	if !strings.HasPrefix(res.Text, "1\n2\n3\n") || !strings.HasSuffix(res.Text, "59\n60") {
		t.Errorf("Text = %q, want the assistant's deltas joined", res.Text)
	}
	// The merged stderr is in the capture; it must not break the parse.
	if !strings.Contains(string(out), "Ripgrep is not available") {
		t.Error("the stderr preamble is gone from the fixture; this test now proves less")
	}
}

// A failed stream-json run reports its reason in the result event; the merged
// stderr after it is a stack trace whose last line says nothing useful.
func TestGeminiParseRealStreamUnknownModel(t *testing.T) {
	out := readFixture(t, "gemini", "0.59.0", "stream-unknown-model.jsonl")
	res := GeminiProvider{}.Parse(1, out)

	if res.Success {
		t.Fatal("want failure")
	}
	if !strings.Contains(res.ErrorMessage, "models/no-such-model-xyz is not found") {
		t.Errorf("ErrorMessage = %q, want the result event's error", res.ErrorMessage)
	}
	if res.Estimated || res.TokensIn != 0 {
		t.Errorf("Estimated = %v, in = %d; the run reported zero usage, which is a report", res.Estimated, res.TokensIn)
	}
}

func TestLastNonEmptyLine(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"\n\n  \n", ""},
		{"one", "one"},
		{"one\ntwo\n", "two"},
		{"one\ntwo\n\n   \n", "two"},
		{"one\r\ntwo\r\n", "two"},
	}
	for _, tt := range tests {
		if got := lastNonEmptyLine(tt.in); got != tt.want {
			t.Errorf("lastNonEmptyLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
