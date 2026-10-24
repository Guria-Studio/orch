package doctor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/state"
)

const nonce = "smoke-1234abcd"

func comment(author, body, kind string) json.RawMessage {
	m := map[string]string{"author": author, "body": body}
	if kind != "" {
		m["kind"] = kind
	}
	raw, _ := json.Marshal(m)
	return raw
}

func strp(s string) *string { return &s }

// passing is a codex dispatch that did everything the smoke asks.
func passing() SmokeEvidence {
	return SmokeEvidence{
		Nonce:       nonce,
		Route:       model.RouteEntry{Backend: model.BackendCodex, CLIModel: "gpt-5.4", Effort: strp("low")},
		TakesEffort: true,
		Dispatch:    map[string]any{"cli_model": "gpt-5.4", "effort": "low"},
		Argv:        []string{"codex", "exec", "-m", "gpt-5.4", "-c", `model_reasoning_effort="low"`},
		Text:        "done. " + nonce,
		Status:      model.StatusDone,
		Comments: []json.RawMessage{
			comment("orch", "dispatched", ""),
			comment("operator", nonce, state.NoteKindOperator),
			comment("codex/gpt-5.4", "smoke "+nonce, ""),
		},
		Usage:     state.Spend{TokensIn: 900, TokensOut: 40, CacheReadTokens: 600},
		SpendRows: 1,
		CostUSD:   0.0012,
	}
}

func statuses(checks []Check) map[string]Status {
	out := map[string]Status{}
	for _, c := range checks {
		out[c.Name] = c.Status
	}
	return out
}

func TestSmokeChecksPassing(t *testing.T) {
	got := statuses(SmokeChecks(passing()))
	for _, name := range []string{"smoke.prompt", "smoke.mcp", "smoke.done", "smoke.usage", "smoke.route"} {
		if got[name] != StatusOK {
			t.Errorf("%s = %q, want ok (all: %v)", name, got[name], got)
		}
	}
}

// Each case is one of the adapter bugs a real run found (#382, #383, #387,
// #388, #389): the row it breaks must not pass.
func TestSmokeChecksCatchTheAdapterBugs(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*SmokeEvidence)
		check string
		want  Status
	}{
		{"empty prompt (#382)", func(e *SmokeEvidence) {
			e.Text = "What would you like me to do?"
			e.Comments = []json.RawMessage{comment("orch", "dispatch succeeded", "")}
		}, "smoke.prompt", StatusError},
		{"agent could not report done (#383, #387)", func(e *SmokeEvidence) {
			e.Comments = []json.RawMessage{comment("orch", "dispatch succeeded", "")}
		}, "smoke.done", StatusError},
		{"task blocked", func(e *SmokeEvidence) {
			e.Status = model.StatusBlocked
		}, "smoke.done", StatusError},
		{"effort never reached codex (#388)", func(e *SmokeEvidence) {
			e.Argv = []string{"codex", "exec", "-m", "gpt-5.4"}
		}, "smoke.route", StatusError},
		{"dispatch recorded another model", func(e *SmokeEvidence) {
			e.Dispatch["cli_model"] = "gpt-5"
		}, "smoke.route", StatusError},
		{"never dispatched", func(e *SmokeEvidence) {
			e.Dispatch = nil
		}, "smoke.route", StatusError},
		{"cached tokens dropped (#389)", func(e *SmokeEvidence) {
			e.Usage.CacheReadTokens = 0
		}, "smoke.usage", StatusWarn},
		{"usage estimated, not reported", func(e *SmokeEvidence) {
			e.Usage.Estimated = true
		}, "smoke.usage", StatusError},
		{"not priced", func(e *SmokeEvidence) {
			e.CostUSD = 0
		}, "smoke.usage", StatusError},
		{"no MCP on a CLI that may lack it", func(e *SmokeEvidence) {
			e.Comments = e.Comments[2:]
		}, "smoke.mcp", StatusWarn},
		{"no MCP where the project configures it", func(e *SmokeEvidence) {
			e.Comments = e.Comments[2:]
			e.MCPExpected = true
		}, "smoke.mcp", StatusError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := passing()
			c.edit(&e)
			checks := SmokeChecks(e)
			if got := statuses(checks)[c.check]; got != c.want {
				t.Errorf("%s = %q, want %q: %+v", c.check, got, c.want, checks)
			}
		})
	}
}

// A CLI that failed before answering is not read as a lost prompt.
func TestSmokeChecksNoAnswerIsNotALostPrompt(t *testing.T) {
	e := passing()
	e.Text, e.Status = "", model.StatusBlocked
	e.Comments = []json.RawMessage{comment("orch", "codex produced no JSONL events", "")}
	c := SmokeChecks(e)[0]
	if c.Status != StatusError || strings.Contains(c.Detail, "#382") || !strings.Contains(c.Detail, "no answer") {
		t.Errorf("smoke.prompt = %+v", c)
	}
}

func TestSmokeChecksEffortNotApplicable(t *testing.T) {
	e := passing()
	e.Route = model.RouteEntry{Backend: model.BackendClaude, CLIModel: "claude-haiku-4-5"}
	e.TakesEffort = false
	e.Dispatch = map[string]any{"cli_model": "claude-haiku-4-5"}
	e.Argv = []string{"claude", "--model", "claude-haiku-4-5"}
	for _, c := range SmokeChecks(e) {
		if c.Name == "smoke.route" && (c.Status != StatusOK || !strings.Contains(c.Detail, "takes no effort")) {
			t.Errorf("smoke.route = %+v", c)
		}
	}
}
