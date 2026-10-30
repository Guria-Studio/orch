package providers

import (
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
)

func TestMuseArgv(t *testing.T) {
	argv := MuseProvider{}.Argv(Request{
		Route:      model.RouteEntry{Backend: model.BackendMuse, CLIModel: "muse-wonder-1.0", Effort: strptr("low")},
		Cwd:        "/work/project",
		PromptPath: "/state/prompts/T1.md",
	})
	assertArgv(t, argv, []string{
		"muse", "exec", "--json",
		"--prompt-file", "/state/prompts/T1.md",
		"--workspace", "/work/project",
		"--worktree", "off",
		"--approval-mode", "never",
		"--disable-request-user-input",
		"--disable-sandbox",
		"--model", "muse-wonder-1.0",
		"--reasoning-effort", "low",
	})
	assertNoShellMetacharacters(t, argv)
}

// An empty cli_model leaves muse on the one its settings name.
func TestMuseArgvNoModelNoEffort(t *testing.T) {
	argv := MuseProvider{}.Argv(Request{Route: model.RouteEntry{Backend: model.BackendMuse}, Cwd: "/w", PromptPath: "/p"})
	for _, a := range argv {
		if a == "--model" || a == "--reasoning-effort" {
			t.Fatalf("argv = %q, want neither --model nor --reasoning-effort", argv)
		}
	}
}

func TestMusePromptDelivery(t *testing.T) {
	if got := (MuseProvider{}).PromptDelivery(); got != PromptFile {
		t.Errorf("PromptDelivery = %v, want PromptFile", got)
	}
}

func TestMuse1_4_12Success(t *testing.T) {
	res := MuseProvider{}.Parse(0, readFixture(t, "muse", "1.4.12", "success.jsonl"))
	if !res.Success || res.ErrorMessage != "" {
		t.Fatalf("Success = %v, ErrorMessage = %q", res.Success, res.ErrorMessage)
	}
	if want := "Command ran once with exit code 0. Output:\n\n```\nreported\n```"; res.Text != want {
		t.Errorf("Text = %q, want %q", res.Text, want)
	}
	if res.TokensIn != 59477 || res.CacheReadTokens != 29556 || res.TokensOut != 141 || res.ReasoningTokens != 22 {
		t.Errorf("tokens in=%d cache=%d out=%d reasoning=%d", res.TokensIn, res.CacheReadTokens, res.TokensOut, res.ReasoningTokens)
	}
	want := []QuotaWindow{
		{Window: "session", Utilization: 0.24, ResetsAt: 1792777976},
		{Window: "seven_day", Utilization: 0.10, ResetsAt: 1792972800},
	}
	if len(res.Quota) != len(want) {
		t.Fatalf("Quota = %+v, want %+v", res.Quota, want)
	}
	for i := range want {
		if res.Quota[i] != want[i] {
			t.Errorf("Quota[%d] = %+v, want %+v", i, res.Quota[i], want[i])
		}
	}
}

func TestMuse1_4_12UnknownModel(t *testing.T) {
	res := MuseProvider{}.Parse(1, readFixture(t, "muse", "1.4.12", "unknown-model.jsonl"))
	if res.Success {
		t.Fatal("Success = true for a model muse refused")
	}
	const want = "model `nope-model` does not exist or you lack access [request_id=838c9861-4722-40aa-9a05-4f09a3bf568b]"
	if res.ErrorMessage != want {
		t.Errorf("ErrorMessage = %q, want %q", res.ErrorMessage, want)
	}
	if res.Quota != nil {
		t.Errorf("Quota = %+v, want nil: the failed run reports allowance null", res.Quota)
	}
}

// The sandboxed run muse finished "completed" even though every shell call
// died in bwrap: the run itself is a success, and the task stays undone
// because task-finish.sh never ran — which is the engine's to notice.
func TestMuse1_4_12SandboxDeniedStillCompletes(t *testing.T) {
	res := MuseProvider{}.Parse(0, readFixture(t, "muse", "1.4.12", "sandbox-denied.jsonl"))
	if !res.Success {
		t.Fatalf("Success = false: %q", res.ErrorMessage)
	}
	if res.Text == "" {
		t.Error("Text is empty, want muse's answer about the failed command")
	}
}

// A background workflow can wake the session and end it again: the last
// run_terminal is the verdict.
func TestMuseLastTerminalWins(t *testing.T) {
	out := `{"payload_type":"run.terminal.failed","payload":{"terminal":"failed","reason":"first"}}
{"payload_type":"run.terminal.completed","payload":{"terminal":"completed","text":"second"}}
`
	res := MuseProvider{}.Parse(0, []byte(out))
	if !res.Success || res.Text != "second" {
		t.Errorf("Success = %v, Text = %q, want the second terminal", res.Success, res.Text)
	}
}

func TestMuseNoTerminal(t *testing.T) {
	res := MuseProvider{}.Parse(0, []byte(`{"payload_type":"run.lifecycle.started","payload":{}}`+"\n"))
	if res.Success || res.ErrorMessage != "muse did not emit a run_terminal event (exit=0)" {
		t.Errorf("Success = %v, ErrorMessage = %q", res.Success, res.ErrorMessage)
	}
}
