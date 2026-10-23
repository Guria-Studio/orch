package providers

import (
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
)

func agyRoute(cliModel string) model.RouteEntry {
	return model.RouteEntry{Backend: model.BackendAgy, CLIModel: cliModel}
}

func strptr(s string) *string { return &s }

func TestAgyArgv(t *testing.T) {
	argv := AgyProvider{}.Argv(Request{
		Route:      agyRoute("gemini-3.8-flash-medium"),
		PromptText: "do the thing",
	})

	// No --agent: agy 1.4 has no "executor" and refuses it (#382).
	want := []string{
		"agy",
		"--output-format", "json",
		"--model", "gemini-3.8-flash-medium",
		"--print", "do the thing",
	}
	if len(argv) != len(want) {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %q, want %q", argv, want)
		}
	}
}

// TestAgyArgvPrintIsLast is the whole reason Argv appends rather than inserts:
// --print's value is the prompt, parsed positionally, so any flag after it is
// swallowed into the prompt text and silently stops applying.
func TestAgyArgvPrintIsLast(t *testing.T) {
	argv := AgyProvider{}.Argv(Request{
		Route: model.RouteEntry{
			Backend:  model.BackendAgy,
			CLIModel: "gemini-3.7-flash",
			Agent:    strptr("reviewer"),
			Effort:   strptr("high"),
		},
		PromptText: "prompt body",
	})

	if argv[len(argv)-2] != "--print" || argv[len(argv)-1] != "prompt body" {
		t.Fatalf("argv tail = %q, want --print then the prompt", argv[len(argv)-2:])
	}
}

func TestAgyArgvRouteOverrides(t *testing.T) {
	tests := []struct {
		name       string
		route      model.RouteEntry
		wantAgent  string // "" = no --agent at all
		wantEffort bool
	}{
		{
			name:  "no agent means no --agent",
			route: agyRoute("m"),
		},
		{
			name: "an empty agent is not an agent",
			route: model.RouteEntry{
				Backend: model.BackendAgy, CLIModel: "m", Agent: strptr(""),
			},
		},
		{
			name: "the route wins when it names one",
			route: model.RouteEntry{
				Backend: model.BackendAgy, CLIModel: "m", Agent: strptr("reviewer"),
			},
			wantAgent: "reviewer",
		},
		{
			name: "effort is emitted only when set",
			route: model.RouteEntry{
				Backend: model.BackendAgy, CLIModel: "m", Effort: strptr("low"),
			},
			wantEffort: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv := AgyProvider{}.Argv(Request{Route: tt.route, PromptText: "p"})
			got := ""
			for i, a := range argv {
				if a == "--agent" {
					got = argv[i+1]
				}
			}
			if got != tt.wantAgent {
				t.Errorf("--agent %q, want %q (argv %q)", got, tt.wantAgent, argv)
			}
			if got := contains(argv, "--effort"); got != tt.wantEffort {
				t.Errorf("--effort present = %v, want %v (argv %q)",
					got, tt.wantEffort, argv)
			}
		})
	}
}

// agy 1.4.2 refuses the "executor" agent orch used to send on every dispatch
// (#382): a failure, with agy's own reason kept for the operator.
func TestAgyParseRealUnknownAgent(t *testing.T) {
	out := readFixture(t, "agy", "1.4.2", "unknown-agent.json")
	res := AgyProvider{}.Parse(1, out)
	if res.Success {
		t.Fatal("an unknown agent is not a success")
	}
	if !strings.Contains(string(out), `unknown agent \"executor\"`) {
		t.Fatalf("fixture drifted: %q", out)
	}
}

// agy 1.4.2 headless refuses a shell command it has no allow rule for, and
// answers SUCCESS with an empty response (#383). The attempt fails, and the
// reason names the refusal, not thinking-mode absorption.
func TestAgyParseRealCommandDenied(t *testing.T) {
	out := readFixture(t, "agy", "1.4.2", "command-denied.json")
	res := AgyProvider{}.Parse(0, out)
	if res.Success {
		t.Fatal("a run whose command was refused is not a success")
	}
	if len(res.PermissionDenials) != 1 || res.PermissionDenials[0] != "RunCommand" {
		t.Errorf("PermissionDenials = %q, want [RunCommand]", res.PermissionDenials)
	}
	if !strings.Contains(res.ErrorMessage, "agy-permissions") || strings.Contains(res.ErrorMessage, "thinking") {
		t.Errorf("ErrorMessage = %q, want it to name the refusal and orch agy-permissions", res.ErrorMessage)
	}
}

func TestAgyParseRealSuccess142(t *testing.T) {
	out := readFixture(t, "agy", "1.4.2", "success.json")
	res := AgyProvider{}.Parse(0, out)
	if !res.Success || strings.TrimSpace(res.Stdout) == "" && res.TokensIn == 0 {
		t.Fatalf("agy 1.4.2 success not read: %+v", res)
	}
	if res.TokensIn == 0 {
		t.Errorf("TokensIn = 0, want the envelope's input_tokens")
	}
}

func TestAgyParseRealSuccess(t *testing.T) {
	out := readFixture(t, "agy", "1.2.1", "success.json")
	res := AgyProvider{}.Parse(0, out)

	if !res.Success {
		t.Fatalf("want success, got %q", res.ErrorMessage)
	}
	if res.TokensIn != 13255 || res.TokensOut != 25 {
		t.Errorf("tokens = (%d,%d), want (13255,25)", res.TokensIn, res.TokensOut)
	}
	// agy reports tokens but never a price; the dashboard's pricing.yaml
	// turns those tokens into USD.
	if res.CostUSD != 0 {
		t.Errorf("CostUSD = %v, want 0", res.CostUSD)
	}
	// The real run spent 24 thinking tokens and still answered. That is the
	// case the issue-86 carve-out must NOT catch: thinking tokens alone are
	// not absorption, an empty response alongside them is.
	if !strings.Contains(string(out), `"thinking_tokens":24`) {
		t.Error("fixture no longer has non-zero thinking tokens; this stops " +
			"being the counter-example to TestAgyParseThinkingModeAbsorption")
	}
}

// TestAgyParseRealUnknownModel: agy answers a bad model name with the list of
// models it does accept — and Parse throws that list away, because Python
// renders a non-SUCCESS status as "agy status=<X>" and never looks at the
// `error` field. Ported as-is; recorded because the discarded text is exactly
// what an operator needs.
func TestAgyParseRealUnknownModel(t *testing.T) {
	out := readFixture(t, "agy", "1.2.1", "unknown-model.json")
	res := AgyProvider{}.Parse(1, out)

	if res.Success {
		t.Fatal("want failure")
	}
	if res.ErrorMessage != "agy status=ERROR" {
		t.Errorf("ErrorMessage = %q, want the ported status line", res.ErrorMessage)
	}
	if !strings.Contains(string(out), "Gemini 3.7 Flash (Medium)") {
		t.Error("the fixture no longer carries the model list this test is about")
	}
	// An unusable model name is unretryable, and here the classifier gets
	// that right off the raw output rather than off the message.
	if got := Classify(res); got != FailureVersionDrift {
		t.Errorf("Classify = %q, want %q", got, FailureVersionDrift)
	}
}

// TestAgyParseRealAuthError is the regression for bug 29. The fixture is a
// real agy 1.2.1 run that printed an OAuth prompt before its envelope;
// Python's json.loads over the whole log fails on it, losing the status and
// the usage numbers. Reading the last JSON object line recovers both.
func TestAgyParseRealAuthError(t *testing.T) {
	out := readFixture(t, "agy", "1.2.1", "auth-error.json")

	// The preamble is what makes this fixture worth having: assert it is
	// really there, so a future re-capture without it does not quietly turn
	// this into a test of nothing.
	if !strings.HasPrefix(string(out), "Authentication required") {
		t.Fatal("fixture no longer starts with the OAuth preamble — " +
			"this test exists to prove the envelope survives one")
	}

	res := AgyProvider{}.Parse(1, out)
	if res.Success {
		t.Fatal("want failure")
	}
	if res.ErrorMessage != "agy status=ERROR" {
		t.Errorf("ErrorMessage = %q, want the status from the envelope — "+
			"if this is the raw last line, the envelope was not parsed",
			res.ErrorMessage)
	}
}

// TestAgyParseThinkingModeAbsorption covers issue #86: SUCCESS with an empty
// response and thinking tokens spent is a failure, because retrying it burns
// the same tokens again and can never produce a reply.
func TestAgyParseThinkingModeAbsorption(t *testing.T) {
	out := readFixture(t, "agy", "synthetic", "empty-thinking.json")
	res := AgyProvider{}.Parse(0, out)

	if res.Success {
		t.Fatal("want failure: SUCCESS with an empty response is not success")
	}
	for _, want := range []string{"1533 thinking token", "issue #86"} {
		if !strings.Contains(res.ErrorMessage, want) {
			t.Errorf("ErrorMessage = %q, want it to mention %q",
				res.ErrorMessage, want)
		}
	}
	// The tokens were spent and must still be recorded.
	if res.TokensIn != 1200 {
		t.Errorf("TokensIn = %d, want 1200", res.TokensIn)
	}
}

func TestAgyParseEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		output   string
		wantErr  string
	}{
		{
			name:     "exit 0 with unparseable output is still a failure",
			exitCode: 0,
			output:   "agy: something went sideways",
			wantErr:  "agy: something went sideways",
		},
		{
			name:     "no output at all",
			exitCode: 0,
			output:   "",
			wantErr:  "agy produced no parseable JSON output",
		},
		{
			name:     "exit 0 but the status is not SUCCESS",
			exitCode: 0,
			output:   `{"status":"TIMEOUT","response":"","usage":{}}`,
			wantErr:  "agy status=TIMEOUT",
		},
		{
			name:     "SUCCESS with a non-zero exit is not success",
			exitCode: 2,
			output:   `{"status":"SUCCESS","response":"ok","usage":{}}`,
			wantErr:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := AgyProvider{}.Parse(tt.exitCode, []byte(tt.output))
			if res.Success {
				t.Fatal("want failure")
			}
			if tt.wantErr != "" && !strings.Contains(res.ErrorMessage, tt.wantErr) {
				t.Errorf("ErrorMessage = %q, want it to contain %q",
					res.ErrorMessage, tt.wantErr)
			}
		})
	}
}

func TestAgyExtractCost(t *testing.T) {
	out := readFixture(t, "agy", "1.2.1", "success.json")
	cost, in, outTok := AgyProvider{}.ExtractCost(out)
	if cost != 0 || in != 13255 || outTok != 25 {
		t.Errorf("ExtractCost = (%v,%d,%d), want (0,13255,25)", cost, in, outTok)
	}
}

func TestAgyEnvelopePrefersWholeBody(t *testing.T) {
	// A clean body parses whole; nothing should depend on the line fallback
	// when there is no preamble to skip.
	obj, ok := agyEnvelope([]byte("{\n  \"status\": \"SUCCESS\"\n}\n"))
	if !ok || asString(obj["status"]) != "SUCCESS" {
		t.Fatalf("agyEnvelope on a pretty-printed body = %v, %v", obj, ok)
	}
}

// #417: agy's thinking_tokens are part of output_tokens (total = input +
// output); the result says how many.
func TestAgyThinkingIsTheReasoning(t *testing.T) {
	res := AgyProvider{}.Parse(0, readFixture(t, "agy", "1.4.2", "command-denied.json"))
	if res.TokensOut != 249 || res.ReasoningTokens != 119 {
		t.Errorf("out %d, reasoning %d; want 249, 119", res.TokensOut, res.ReasoningTokens)
	}
}
