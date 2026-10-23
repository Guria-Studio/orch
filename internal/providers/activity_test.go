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

// Every fixture is a real run (testdata/README.md). `tools` read a file, ran
// one command and answered; `tool-errors` asked for a file that does not
// exist and ran `ls` on it, so each CLI's own way of reporting a failed tool
// is pinned: claude's `is_error` tool result, codex's non-zero `exit_code`,
// opencode's `status: "error"`.
func TestActivityOf(t *testing.T) {
	tests := []struct {
		name    string
		fixture []string
		want    []Step
	}{
		{
			name:    "claude 2.1.301 tools",
			fixture: []string{"claude", "2.1.301", "stream-success.jsonl"},
			want: []Step{
				{Kind: StepStart},
				{Kind: StepTool, Tool: "Read", Detail: "/work/project/note.txt"},
				{Kind: StepTool, Tool: "Bash", Detail: "wc -l note.txt"},
				{Kind: StepText, Detail: `note.txt is a one-line plain-text file containing the greeting "hello from orch".`},
				{Kind: StepEnd, Detail: "success"},
			},
		},
		{
			name:    "claude 2.1.301 tool errors",
			fixture: []string{"claude", "2.1.301", "tool-errors.jsonl"},
			want: []Step{
				{Kind: StepStart},
				{Kind: StepTool, Tool: "Read", Detail: "/work/project/missing.txt"},
				{Kind: StepError, Detail: "File does not exist. Note: your current working directory is /work/project."},
				{Kind: StepTool, Tool: "Bash", Detail: "ls missing.txt"},
				{Kind: StepError, Detail: "Exit code 2\nls: no se puede acceder a 'missing.txt': No existe el archivo o directorio"},
				{Kind: StepText, Detail: "`missing.txt` does not exist in the working directory: the Read failed with \"File does not exist,\" and `ls missing.txt` returned \"No existe el archivo o directorio\" (exit code 2)."},
				{Kind: StepEnd, Detail: "success"},
			},
		},
		{
			name:    "muse 1.4.12 success",
			fixture: []string{"muse", "1.4.12", "success.jsonl"},
			want: []Step{
				{Kind: StepStart},
				{Kind: StepTool, Tool: "bash", Detail: "scripts/task-finish.sh T1"},
				{Kind: StepText, Detail: "Command ran once with exit code 0. Output:\n\n```\nreported\n```"},
				{Kind: StepEnd, Detail: "completed"},
			},
		},
		{
			name:    "muse 1.4.12 sandbox denied",
			fixture: []string{"muse", "1.4.12", "sandbox-denied.jsonl"},
			want: []Step{
				{Kind: StepStart},
				{Kind: StepError, Detail: "scripts/task-finish.sh T1: bwrap: setting up uid map: Permission denied"},
				{Kind: StepText, Detail: "Command failed before the script ran. Output:\n\n```\nbwrap: setting up uid map: Permission denied\n```\n\nExit code: 1"},
				{Kind: StepEnd, Detail: "completed"},
			},
		},
		{
			name:    "muse 1.4.12 unknown model",
			fixture: []string{"muse", "1.4.12", "unknown-model.jsonl"},
			want: []Step{
				{Kind: StepStart},
				{Kind: StepError, Detail: "model `nope-model` does not exist or you lack access [request_id=838c9861-4722-40aa-9a05-4f09a3bf568b]"},
			},
		},
		{
			name:    "codex 0.154.0 tools",
			fixture: []string{"codex", "0.154.0", "tools.jsonl"},
			want: []Step{
				{Kind: StepStart},
				{Kind: StepText, Detail: "I’ll read `note.txt` and count its lines."},
				{Kind: StepTool, Tool: "shell", Detail: "cat note.txt"},
				{Kind: StepTool, Tool: "shell", Detail: "wc -l note.txt"},
				{Kind: StepText, Detail: "The file contains one line: “hello from orch”."},
				{Kind: StepEnd},
			},
		},
		{
			// codex reports its own warnings as `error` items: the first
			// one here is about the operator's codex skills, not the task.
			name:    "codex 0.154.0 tool errors",
			fixture: []string{"codex", "0.154.0", "tool-errors.jsonl"},
			want: []Step{
				{Kind: StepStart},
				{Kind: StepError, Detail: "Skill descriptions were shortened to fit the skills context budget. Codex can still see every skill, but some descriptions are shorter. Disable unused skills or plugins to leave more room for the rest."},
				{Kind: StepText, Detail: "I’ll read `missing.txt`, then run `ls missing.txt`."},
				{Kind: StepTool, Tool: "shell", Detail: "cat missing.txt"},
				{Kind: StepError, Detail: "cat missing.txt exited 1"},
				{Kind: StepTool, Tool: "shell", Detail: "ls missing.txt"},
				{Kind: StepError, Detail: "ls missing.txt exited 2"},
				{Kind: StepText, Detail: "Both reading `missing.txt` and running `ls missing.txt` failed because the file does not exist."},
				{Kind: StepEnd},
			},
		},
		{
			name:    "opencode 1.18.32 tools",
			fixture: []string{"opencode", "1.18.32", "tools.json"},
			want: []Step{
				{Kind: StepTool, Tool: "read", Detail: "/work/project/note.txt"},
				{Kind: StepTool, Tool: "bash", Detail: "wc -l note.txt"},
				{Kind: StepText, Detail: `The file contains a single line of text: "hello from orch".`},
				{Kind: StepEnd, Detail: "stop"},
			},
		},
		{
			// opencode's `bash` succeeds as a call whatever the command's
			// exit code; only the `read` of a missing file is an error.
			name:    "opencode 1.18.32 tool errors",
			fixture: []string{"opencode", "1.18.32", "tool-errors.json"},
			want: []Step{
				{Kind: StepError, Detail: "read: File not found: /work/project/missing.txt"},
				{Kind: StepTool, Tool: "bash", Detail: "ls missing.txt"},
				{Kind: StepText, Detail: "missing.txt no existe, tanto la lectura como el `ls` fallaron por archivo inexistente."},
				{Kind: StepEnd, Detail: "stop"},
			},
		},
		{
			name:    "codex 0.154.0 unknown model",
			fixture: []string{"codex", "0.154.0", "unknown-model.jsonl"},
			want: []Step{
				{Kind: StepStart},
				{Kind: StepError, Detail: "Model metadata for `no-such-model-xyz` not found. Defaulting to fallback metadata; this can degrade performance and cause issues."},
				{Kind: StepError, Detail: `{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The 'no-such-model-xyz' model is not supported when using Codex with a ChatGPT account."}}`},
				{Kind: StepError, Detail: `{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The 'no-such-model-xyz' model is not supported when using Codex with a ChatGPT account."}}`},
			},
		},
		{
			name:    "opencode 1.18.30 unknown model",
			fixture: []string{"opencode", "1.18.30", "unknown-model.json"},
			want: []Step{
				{Kind: StepError, Detail: "Unexpected server error. Check server logs for details."},
				{Kind: StepError, Detail: "Model not found: no-such-model-xyz/."},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := activityOf(readFixture(t, tt.fixture...))
			if len(got) != len(tt.want) {
				t.Fatalf("got %d steps, want %d:\n got  %#v\n want %#v", len(got), len(tt.want), got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("step %d = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// A line that is not a JSON object is no step: stderr, the partial line a
// tail starts in, an empty line, a JSON array.
func TestActivityOfIgnoresNoise(t *testing.T) {
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
