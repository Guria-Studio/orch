package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/cli"
)

// newTestProject copies the committed parity fixture into a fresh temp dir
// and returns its path plus the --project-root/--project-id args every
// call needs — the same fixture scripts/parity.sh uses, reused here for
// exact exit-code assertions ported from test_task_status_cmd.py and
// test_task_set_cmd.py (those check `rc == N` directly; testscript's `!
// exec` only checks success/failure, not the specific code).
func newTestProject(t *testing.T) (root string, common []string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "proj")
	if err := copyProjectFixture(fixtureDir(t), root); err != nil {
		t.Fatal(err)
	}
	return root, []string{"--project-root", root, "--project-id", "parity-project"}
}

// TestTaskStatusAndTaskSetExitCodes is a table over both commands' exit
// codes — ported directly from test_task_status_cmd.py / test_task_set_cmd.py
// assertions on `rc == N`. `pre` runs before the assertion (ignoring its own
// exit code) to set up state the case needs, e.g. bootstrapping via
// `status`, or moving a task somewhere first so the real command under test
// can attempt an illegal transition out of it.
func TestTaskStatusAndTaskSetExitCodes(t *testing.T) {
	cases := []struct {
		name string
		pre  [][]string
		args []string
		want int
	}{
		{
			name: "task-status happy path",
			args: []string{"task-status", "F2.T2", "done", "--author", "test", "--note", "finished"},
			want: 0,
		},
		{
			name: "task-status unknown task",
			args: []string{"task-status", "NOPE", "done"},
			want: 2,
		},
		{
			name: "task-status invalid status",
			args: []string{"task-status", "F0.T1", "bogus-status"},
			want: 2,
		},
		{
			// F0.T1 starts `done` in the fixture; done -> in-progress is
			// illegal (must reset to todo first). `pre` bootstraps via
			// `status` so the row exists before task-status is called.
			name: "task-status illegal transition",
			pre:  [][]string{{"status", "--json"}},
			args: []string{"task-status", "F0.T1", "in-progress"},
			want: 3,
		},
		{
			name: "task set with no mutation flag",
			args: []string{"task", "set", "--id", "F0.T1"},
			want: 1,
		},
		{
			name: "task set --status",
			args: []string{"task", "set", "--id", "F2.T2", "--status", "done"},
			want: 0,
		},
		{
			// F0.T1 is `done`; done -> blocked is illegal.
			name: "task set illegal transition",
			args: []string{"task", "set", "--id", "F0.T1", "--status", "blocked"},
			want: 3,
		},
		{
			// #356: no run, so F2.T2 (in-progress) just goes back to todo.
			name: "task stop with no run",
			args: []string{"task", "stop", "--id", "F2.T2", "--note", "try the other API"},
			want: 0,
		},
		{
			// done -> blocked is illegal, held or not.
			name: "task stop --hold on a done task",
			args: []string{"task", "stop", "--id", "F0.T1", "--hold"},
			want: 3,
		},
		{
			name: "task stop unknown task",
			args: []string{"task", "stop", "--id", "NOPE"},
			want: 1,
		},
		{
			name: "task note",
			args: []string{"task", "note", "--id", "F2.T2", "do", "not", "touch", "the", "migration"},
			want: 0,
		},
		{
			name: "task note unknown task",
			args: []string{"task", "note", "--id", "NOPE", "hi"},
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, common := newTestProject(t)
			for _, p := range tc.pre {
				_ = cli.Run("test", append(append([]string{}, p...), common...))
			}
			args := append(append([]string{}, tc.args...), common...)
			if rc := cli.Run("test", args); rc != tc.want {
				t.Fatalf("cli.Run(%v) = %d, want %d", tc.args, rc, tc.want)
			}
		})
	}
}

// #390: `task set --model` changes the task's model in tasks.json and the
// database; `task add` and `task edit` go through the same TaskEditor. A
// model with no route is refused and writes nothing, and --backend is gone
// (the route names the backend).
func TestTaskSetModelAddAndEdit(t *testing.T) {
	root, common := newTestProject(t)
	router := filepath.Join(root, ".orchestrator", "model_router.yaml")
	if err := os.WriteFile(router, []byte("codex/gpt-5.6-codex:\n  backend: codex\n  cli_model: gpt-5.6-codex\n  tier: standard\n  is_premium: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) int { return cli.Run("test", append(args, common...)) }
	tasksJSON := func() string {
		b, err := os.ReadFile(filepath.Join(root, "tasks.json")) // #nosec G304 -- a temp copy of the fixture
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	if rc := run("task", "set", "--id", "F0.T1", "--model", "codex/gpt-5.6-codex"); rc != 0 {
		t.Fatalf("task set --model: rc = %d", rc)
	}
	if !strings.Contains(tasksJSON(), `"model": "codex/gpt-5.6-codex"`) {
		t.Errorf("tasks.json does not carry the new model:\n%s", tasksJSON())
	}
	before := tasksJSON()
	if rc := run("task", "set", "--id", "F0.T1", "--model", "gemini/nope"); rc != 1 {
		t.Errorf("an unrouted model: rc = %d, want 1", rc)
	}
	if tasksJSON() != before {
		t.Error("a refused model changed tasks.json")
	}
	if rc := run("task", "set", "--id", "F0.T1", "--backend", "codex"); rc == 0 {
		t.Error("--backend is still accepted")
	}

	if rc := run("task", "add", "--id", "F9.T1", "--title", "new work", "--model", "codex/gpt-5.6-codex", "--deps", "F0.T1", "--estimate", "2"); rc != 0 {
		t.Fatalf("task add: rc = %d", rc)
	}
	if rc := run("task", "edit", "--id", "F9.T1", "--title", "renamed work"); rc != 0 {
		t.Fatalf("task edit: rc = %d", rc)
	}
	if got := tasksJSON(); !strings.Contains(got, `"id": "F9.T1"`) || !strings.Contains(got, `"title": "renamed work"`) {
		t.Errorf("tasks.json after add and edit:\n%s", got)
	}
	if rc := run("task", "edit", "--id", "F9.T1"); rc != 1 {
		t.Errorf("an edit with nothing to change: rc = %d, want 1", rc)
	}
}

func TestResetDryRunThenRequeue(t *testing.T) {
	_, common := newTestProject(t)
	// Bootstrap so tasks_runtime exists (F2.T2 ships in-progress).
	statusArgs := append([]string{"status", "--json"}, common...)
	if rc := cli.Run("test", statusArgs); rc != 0 {
		t.Fatalf("bootstrap via status: rc = %d, want 0", rc)
	}

	dryRun := append([]string{"reset"}, common...)
	if rc := cli.Run("test", dryRun); rc != 0 {
		t.Fatalf("reset dry-run: rc = %d, want 0", rc)
	}

	requeue := append([]string{"reset", "--requeue"}, common...)
	if rc := cli.Run("test", requeue); rc != 0 {
		t.Fatalf("reset --requeue: rc = %d, want 0", rc)
	}

	// F2.T2 must be back to todo now.
	rows := append([]string{"tasks", "--status", "todo", "--json"}, common...)
	if rc := cli.Run("test", rows); rc != 0 {
		t.Fatalf("tasks --status todo: rc = %d, want 0", rc)
	}
}

// TestResetInvalidOnlyGlobFails covers inProgressCandidates' error path: a
// malformed --only glob (unterminated character class) must fail loudly,
// not silently match nothing — same rule status.go's --only follows.
func TestResetInvalidOnlyGlobFails(t *testing.T) {
	_, common := newTestProject(t)
	statusArgs := append([]string{"status", "--json"}, common...)
	if rc := cli.Run("test", statusArgs); rc != 0 {
		t.Fatalf("bootstrap via status: rc = %d, want 0", rc)
	}
	args := append([]string{"reset", "--only", "F0["}, common...)
	if rc := cli.Run("test", args); rc == 0 {
		t.Fatalf("reset --only 'F0[': rc = 0, want non-zero (malformed glob)")
	}
}
