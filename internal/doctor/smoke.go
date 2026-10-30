package doctor

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/state"
)

// SmokeEvidence is what one backend's `orch doctor --smoke` dispatch left
// behind (#437): what orch sent, what the database recorded, and what the CLI
// answered. Every adapter bug of v0.2x was found by a real run (#382, #383,
// #387, #388, #389); these are the traces each of them would have left.
type SmokeEvidence struct {
	// Nonce is the token the smoke task's description carries. Only an agent
	// that got the prompt can write it back.
	Nonce string
	// Route is what the smoke routed the task to.
	Route model.RouteEntry
	// TakesEffort is whether the backend's CLI is given a route's effort.
	TakesEffort bool
	// MCPExpected is whether the project itself configures orch's MCP server
	// for this CLI (claude reads the project's .mcp.json); other CLIs read
	// their own user config, and fall back to the scripts without it.
	MCPExpected bool
	// Dispatch is the dispatch event's extra; nil when nothing was dispatched.
	Dispatch map[string]any
	// Argv is the provider's argv rebuilt from the recorded route.
	Argv []string
	// Text is the CLI's final answer, parsed from its log.
	Text string
	// Status and Comments are the task's row after the run.
	Status   model.Status
	Comments []json.RawMessage
	// Usage sums the task's spend rows; SpendRows counts them.
	Usage     state.Spend
	SpendRows int
	// CostUSD is the spend, as reported or else estimated from pricing.yaml.
	CostUSD float64
}

type smokeComment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	Kind   string `json:"kind"`
}

// SmokeChecks reads the evidence into the smoke's five checks: the prompt
// arrived, the agent reached orch's MCP tools, it reported the task done, its
// usage was parsed and priced, and the route's model and effort reached the
// CLI.
func SmokeChecks(e SmokeEvidence) []Check {
	var comments []smokeComment
	for _, raw := range e.Comments {
		var c smokeComment
		if json.Unmarshal(raw, &c) == nil {
			comments = append(comments, c)
		}
	}
	agentWrote := func(pred func(smokeComment) bool) (smokeComment, bool) {
		for _, c := range comments {
			if c.Author != "orch" && pred(c) {
				return c, true
			}
		}
		return smokeComment{}, false
	}
	withNonce := func(c smokeComment) bool { return strings.Contains(c.Body, e.Nonce) }

	prompt := Check{Name: "smoke.prompt", Status: StatusOK, Detail: "the agent wrote back the token its prompt carried"}
	if _, ok := agentWrote(withNonce); !ok && !strings.Contains(e.Text, e.Nonce) {
		prompt.Status, prompt.Detail = StatusError, "the agent never wrote back the token its prompt carried: the prompt did not reach it (#382)"
		switch {
		case e.Dispatch == nil:
			prompt.Detail = "nothing was dispatched"
		case strings.TrimSpace(e.Text) == "":
			prompt.Detail = "the CLI gave no answer (smoke.done says why)"
		}
	}

	mcp := Check{Name: "smoke.mcp", Status: StatusOK, Detail: "the agent called orch_task_note"}
	if _, ok := agentWrote(func(c smokeComment) bool { return c.Kind == state.NoteKindOperator && withNonce(c) }); !ok {
		mcp.Status, mcp.Detail = StatusWarn, "the agent did not reach orch's MCP tools; it can still report through scripts/task-finish.sh"
		mcp.Remediation = "Give this CLI orch's MCP server (docs/MCP.md)."
		if e.MCPExpected {
			mcp.Status, mcp.Detail = StatusError, "the agent did not reach orch's MCP tools, which the project's .mcp.json configures"
			mcp.Remediation = "`orch claude-permissions` allows them; check that `orch mcp` starts."
		}
	}

	done := Check{Name: "smoke.done", Status: StatusOK}
	reported, ok := agentWrote(func(c smokeComment) bool { return c.Kind == "" })
	switch {
	case e.Status != model.StatusDone:
		last := ""
		if len(comments) > 0 {
			last = ": " + comments[len(comments)-1].Body
		}
		done.Status, done.Detail = StatusError, fmt.Sprintf("the task ended %s%s", e.Status, last)
	case !ok:
		done.Status, done.Detail = StatusError, "orch recorded the task done when the CLI exited: the agent could not report it through task-finish.sh or orch_set_status (#383, #387)"
	default:
		done.Detail = "the agent reported done as " + reported.Author
	}

	u := e.Usage
	usage := Check{Name: "smoke.usage", Status: StatusOK, Detail: fmt.Sprintf("in %d (cached %d) · out %d · $%.4f",
		u.TokensIn, u.CacheReadTokens+u.CacheCreationTokens, u.TokensOut, e.CostUSD)}
	switch {
	case e.SpendRows == 0:
		usage.Status, usage.Detail = StatusError, "no spend row was recorded"
	case u.Estimated:
		usage.Status, usage.Detail = StatusError, "the CLI reported no token usage; orch estimated it"
	case u.TokensIn <= 0 || u.TokensOut <= 0:
		usage.Status, usage.Detail = StatusError, "usage was not parsed: "+usage.Detail
	case e.CostUSD <= 0:
		usage.Status, usage.Detail = StatusError, "usage was not priced: "+usage.Detail
	case u.CacheReadTokens+u.CacheCreationTokens == 0:
		usage.Status, usage.Detail = StatusWarn, "no cached input tokens were counted (#389): "+usage.Detail
	}

	return []Check{prompt, mcp, done, usage, smokeRoute(e)}
}

// smokeRoute is the fifth check: the recorded dispatch names the route's
// model and effort, and the argv built from it carries both.
func smokeRoute(e SmokeEvidence) Check {
	c := Check{Name: "smoke.route"}
	if e.Dispatch == nil {
		c.Status, c.Detail = StatusError, "no dispatch was recorded"
		return c
	}
	cliModel, _ := e.Dispatch["cli_model"].(string)
	if cliModel != e.Route.CLIModel || !slices.Contains(e.Argv, cliModel) {
		c.Status, c.Detail = StatusError, fmt.Sprintf("the route's model %s did not reach the CLI (recorded %q, argv %v)", e.Route.CLIModel, cliModel, e.Argv)
		return c
	}
	if !e.TakesEffort {
		c.Status, c.Detail = StatusOK, "model "+cliModel+" reached the CLI; it takes no effort"
		return c
	}
	want := ""
	if e.Route.Effort != nil {
		want = *e.Route.Effort
	}
	effort, _ := e.Dispatch["effort"].(string)
	inArgv := slices.ContainsFunc(e.Argv, func(a string) bool {
		return a == effort || strings.Contains(a, "effort") && strings.Contains(a, effort)
	})
	if effort != want || effort == "" || !inArgv {
		c.Status, c.Detail = StatusError, fmt.Sprintf("the route's effort %q did not reach the CLI (recorded %q, argv %v) (#388)", want, effort, e.Argv)
		return c
	}
	c.Status, c.Detail = StatusOK, "model "+cliModel+" and effort "+effort+" reached the CLI"
	return c
}
