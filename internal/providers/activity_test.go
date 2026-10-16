package providers

import (
	"strings"
	"testing"
)

// activityOf runs every line of a captured log through ActivityOf, the way
// the dashboard reads a log it is tailing.
func activityOf(log []byte) []Step {
	var steps []Step
	for _, line := range strings.Split(string(log), "\n") {
		steps = append(steps, ActivityOf([]byte(line))...)
	}
	return steps
}

func assertSteps(t *testing.T, got, want []Step) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d:\n got  %+v\n want %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Each test below reads a real run that read a file, ran one command and
// answered (testdata/README.md): the same three things, in each CLI's words.

func TestActivityClaude2_1_301(t *testing.T) {
	assertSteps(t, activityOf(readFixture(t, "claude", "2.1.301", "stream-success.jsonl")), []Step{
		{Kind: StepStart},
		{Kind: StepTool, Tool: "Read", Detail: "/work/project/note.txt"},
		{Kind: StepTool, Tool: "Bash", Detail: "wc -l note.txt"},
		{Kind: StepText, Detail: `note.txt is a one-line plain-text file containing the greeting "hello from orch".`},
		{Kind: StepEnd, Detail: "success"},
	})
}

func TestActivityCodex0_154_0(t *testing.T) {
	assertSteps(t, activityOf(readFixture(t, "codex", "0.154.0", "tools.jsonl")), []Step{
		{Kind: StepStart},
		{Kind: StepText, Detail: "I’ll read `note.txt` and count its lines."},
		{Kind: StepTool, Tool: "shell", Detail: "cat note.txt"},
		{Kind: StepTool, Tool: "shell", Detail: "wc -l note.txt"},
		{Kind: StepText, Detail: "The file contains one line: “hello from orch”."},
		{Kind: StepEnd},
	})
}

func TestActivityOpencode1_18_32(t *testing.T) {
	assertSteps(t, activityOf(readFixture(t, "opencode", "1.18.32", "tools.json")), []Step{
		{Kind: StepTool, Tool: "read", Detail: "/work/project/note.txt"},
		{Kind: StepTool, Tool: "bash", Detail: "wc -l note.txt"},
		{Kind: StepText, Detail: `The file contains a single line of text: "hello from orch".`},
		{Kind: StepEnd, Detail: "stop"},
	})
}

// A failure reads as one: codex's API error, opencode's unknown model, and a
// non-JSON line (stderr, a partial line the tail started in) is no step.
func TestActivityErrorsAndNoise(t *testing.T) {
	codex := activityOf(readFixture(t, "codex", "0.154.0", "unknown-model.jsonl"))
	if !hasKind(codex, StepError) {
		t.Errorf("codex unknown-model gave no error step: %+v", codex)
	}
	opencode := activityOf(readFixture(t, "opencode", "1.18.30", "unknown-model.json"))
	if !hasKind(opencode, StepError) {
		t.Errorf("opencode unknown-model gave no error step: %+v", opencode)
	}
	for _, line := range []string{"Reading prompt from stdin...", `ype":"assistant"}`, "", "[]"} {
		if got := ActivityOf([]byte(line)); len(got) != 0 {
			t.Errorf("ActivityOf(%q) = %+v, want none", line, got)
		}
	}
}

func TestActivityDetailIsBounded(t *testing.T) {
	long := strings.Repeat("x", 5000)
	got := ActivityOf([]byte(`{"type":"item.completed","item":{"type":"agent_message","text":"` + long + `"}}`))
	if len(got) != 1 || len([]rune(got[0].Detail)) > maxStepDetail+1 {
		t.Fatalf("detail not bounded: %d runes", len([]rune(got[0].Detail)))
	}
}

func hasKind(steps []Step, kind string) bool {
	for _, s := range steps {
		if s.Kind == kind {
			return true
		}
	}
	return false
}
