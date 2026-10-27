package sandbox

import (
	"slices"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/model"
)

// covers reports whether path falls under one of paths, the way a Landlock
// rule on a directory covers everything beneath it.
func covers(paths []string, path string) bool {
	return slices.ContainsFunc(paths, func(p string) bool { return isWithin(path, p) })
}

func testOptions(b model.Backend) Options {
	return Options{
		Backend:  b,
		Workdir:  "/work/app.worktrees/F1.T1",
		Writable: []string{"/work/app/.orchestrator/state/app", "/work/app/.git"},
		Readable: []string{"/work/app", "/home/u/.local/bin"},
		Home:     "/home/u",
		// ~/bin is readable, but its parent is $HOME; "." is relative.
		// Neither may widen the policy.
		PathEnv: "/home/u/bin:/home/u/.nvm/versions/node/v24/bin:/usr/bin:.",
		TempDir: "/tmp",
	}
}

// The agent writes its worktree and the database, and reaches none of the
// home directory's secrets, other projects or other tasks' worktrees.
func TestForConfinesAgentToItsTask(t *testing.T) {
	p := For(testOptions(model.BackendClaude))
	for _, path := range []string{
		"/work/app.worktrees/F1.T1/main.go",
		"/work/app/.orchestrator/state/app/orch.db",
		"/work/app/.git/objects/ab",
		"/tmp/x",
		"/home/u/.claude/settings.json",
		"/home/u/.claude.json",
		"/home/u/.cache/go-build",
	} {
		if !covers(p.RW, path) {
			t.Errorf("%s is not writable; RW = %v", path, p.RW)
		}
	}
	for _, path := range []string{
		"/work/app/.orchestrator/config.yaml",
		"/usr/lib/x",
		"/home/u/.nvm/versions/node/v24/lib/node_modules/x",
		"/home/u/.gitconfig",
	} {
		if !covers(p.RO, path) && !covers(p.RW, path) {
			t.Errorf("%s is not readable; RO = %v", path, p.RO)
		}
	}
	for _, path := range []string{
		"/home/u/.ssh/id_ed25519",
		"/home/u/.aws/credentials",
		"/home/u/.config/gh/hosts.yml",
		"/home/u/other-project/.env",
		"/work/app.worktrees/F1.T2/main.go",
		"/home/u/.codex/auth.json",
	} {
		if covers(p.RW, path) || covers(p.RO, path) {
			t.Errorf("%s is reachable; RW = %v, RO = %v", path, p.RW, p.RO)
		}
	}
	if covers(p.RW, "/work/app/tasks.json") {
		t.Errorf("the project root is writable; RW = %v", p.RW)
	}
}

// Each CLI gets its own config directory and no other's.
func TestForOpensOnlyTheDispatchedCLIsConfig(t *testing.T) {
	codex := For(testOptions(model.BackendCodex))
	if !covers(codex.RW, "/home/u/.codex/auth.json") {
		t.Errorf("codex cannot write ~/.codex; RW = %v", codex.RW)
	}
	if covers(codex.RW, "/home/u/.claude/.credentials.json") || covers(codex.RO, "/home/u/.claude/.credentials.json") {
		t.Errorf("codex reaches ~/.claude; RW = %v", codex.RW)
	}
}

// A path at or above $HOME, or "/", never makes it into the policy, whoever
// asked for it.
func TestForDropsPathsThatOpenTheHomeDirectory(t *testing.T) {
	o := testOptions(model.BackendClaude)
	o.Writable = append(o.Writable, "/home/u", "/home", "/", "/home/u/")
	p := For(o)
	for _, path := range append(p.RW, p.RO...) {
		if isWithin("/home/u", path) {
			t.Errorf("policy holds %s, which covers all of $HOME", path)
		}
	}
}

func TestWrapPutsTheCommandAfterTheDoubleDash(t *testing.T) {
	got := Wrap("/usr/bin/orch", Policy{RW: []string{"/a"}, RO: []string{"/b"}}, []string{"claude", "-p", "--rw"})
	want := "/usr/bin/orch sandbox-exec --rw /a --ro /b -- claude -p --rw"
	if strings.Join(got, " ") != want {
		t.Errorf("Wrap = %q, want %q", strings.Join(got, " "), want)
	}
}
