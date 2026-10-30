package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/sandbox"
)

// runAsSandboxLauncher makes this test binary stand in for `orch
// sandbox-exec` when Spawn wraps a dispatch in it: the --rw and --ro pairs
// become the policy and sandbox.Exec applies it for real. It returns only
// when the binary was not started as the launcher.
func runAsSandboxLauncher() {
	if len(os.Args) < 2 || os.Args[1] != sandbox.Command {
		return
	}
	var p sandbox.Policy
	args := os.Args[2:]
	for len(args) >= 2 && args[0] != "--" {
		if args[0] == "--rw" {
			p.RW = append(p.RW, args[1])
		} else {
			p.RO = append(p.RO, args[1])
		}
		args = args[2:]
	}
	err := sandbox.Exec(p, args[1:])
	fmt.Fprintln(os.Stderr, err)
	os.Exit(126)
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- fixed git subcommands on a test repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// covered reports whether path falls under one of paths.
func covered(paths []string, path string) bool {
	return slices.ContainsFunc(paths, func(p string) bool {
		return path == p || strings.HasPrefix(path, p+string(filepath.Separator))
	})
}

// A worktree's policy writes the worktree and the repository's common git
// dir its commits land in, reads the project root, and opens the dispatched
// CLI's config only; sandbox_allow's ~/ is $HOME.
func TestSandboxPolicyCoversTheWorktreeAndItsGitDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(t.TempDir(), "F1.T1")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "orch/F1.T1", wt)

	s := &Scheduler{Opts: SchedulerOptions{
		Cwd:      repo,
		StateDir: filepath.Join(repo, ".orchestrator", "state", "p"),
		Cfg:      config.Config{Dispatch: config.Dispatch{SandboxAllow: []string{"~/.orca"}}},
	}}
	prompt := filepath.Join(s.Opts.StateDir, "prompts", "run", "F1.T1.txt")
	p := s.sandboxPolicy(context.Background(), model.BackendClaude, wt, prompt)

	for _, path := range []string{wt, filepath.Join(repo, ".git"), s.Opts.StateDir, filepath.Join(home, ".orca"), filepath.Join(home, ".claude")} {
		if !covered(p.RW, path) {
			t.Errorf("%s is not writable; RW = %v", path, p.RW)
		}
	}
	for _, path := range []string{repo, filepath.Dir(prompt)} {
		if !covered(p.RO, path) {
			t.Errorf("%s is not readable; RO = %v", path, p.RO)
		}
	}
	if covered(p.RW, filepath.Join(home, ".codex")) || covered(p.RO, filepath.Join(home, ".codex")) {
		t.Errorf("a claude agent reaches ~/.codex; RW = %v", p.RW)
	}
	codex := s.sandboxPolicy(context.Background(), model.BackendCodex, wt, prompt)
	if !covered(codex.RW, filepath.Join(home, ".codex")) || covered(codex.RW, filepath.Join(home, ".claude")) {
		t.Errorf("codex's policy has the wrong CLI config; RW = %v", codex.RW)
	}
}

// gitCommonDir is absolute whether git answers relatively (the main
// checkout says ".git") or not (a worktree), and empty outside a repository.
func TestGitCommonDir(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "w", wt)
	want, err := filepath.EvalSymlinks(filepath.Join(repo, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{repo, wt} {
		got, err := filepath.EvalSymlinks(gitCommonDir(context.Background(), dir))
		if err != nil || got != want {
			t.Errorf("gitCommonDir(%s) = %q (%v), want %q", dir, got, err, want)
		}
	}
	if got := gitCommonDir(context.Background(), t.TempDir()); got != "" {
		t.Errorf("gitCommonDir outside a repository = %q, want empty", got)
	}
}

// With a policy, Spawn runs orch's launcher with the provider's argv after
// "--", in its own process group, writing the same log: the launcher execs
// the CLI in place, so the pid and descriptors are the CLI's.
func TestSpawnWrapsTheCLIInTheSandboxLauncher(t *testing.T) {
	if err := sandbox.Available(); err != nil {
		t.Skipf("Landlock is not available here, so the launcher cannot run: %v", err)
	}
	fake := fakeDir(t, model.BackendClaude, "S-1", "confined output\n", 0, "")
	t.Setenv(FakeProviderEnv, fake)
	d := claudeDispatch(t, "S-1")
	policy := &sandbox.Policy{RW: []string{d.Cwd}, RO: []string{"/usr", "/bin", "/lib", "/lib64", "/etc", fake}}

	sp, err := Spawn(context.Background(), SpawnRequest{
		Provider: d.Provider, Req: d.Req, PromptPath: d.PromptPath, LogPath: d.LogPath, Cwd: d.Cwd,
		Sandbox: policy,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	exit, _, _ := sp.Wait(10 * time.Second)

	self, _ := os.Executable()
	args := sp.cmd.Args
	dash := slices.Index(args, "--")
	if len(args) < 3 || args[0] != self || args[1] != sandbox.Command || dash < 0 || dash == len(args)-1 {
		t.Fatalf("argv = %q, want %s %s … -- <cli argv>", args, self, sandbox.Command)
	}
	if want := sandbox.Wrap(self, *policy, args[dash+1:]); !slices.Equal(args, want) {
		t.Errorf("argv = %q, want %q", args, want)
	}
	if sp.cmd.SysProcAttr == nil || !sp.cmd.SysProcAttr.Setpgid {
		t.Error("the launcher is not in its own process group")
	}
	if exit != 0 || !strings.Contains(ReadLog(d.LogPath), "confined output") {
		t.Errorf("exit = %d, log = %q; want 0 and the CLI's output", exit, ReadLog(d.LogPath))
	}
}

// Without a policy nothing is wrapped.
func TestSpawnWithoutSandboxRunsTheCLIDirectly(t *testing.T) {
	t.Setenv(FakeProviderEnv, fakeDir(t, model.BackendClaude, "S-2", "ok\n", 0, ""))
	d := claudeDispatch(t, "S-2")
	sp, err := Spawn(context.Background(), SpawnRequest{
		Provider: d.Provider, Req: d.Req, PromptPath: d.PromptPath, LogPath: d.LogPath, Cwd: d.Cwd,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	sp.Wait(10 * time.Second)
	if slices.Contains(sp.cmd.Args, sandbox.Command) {
		t.Errorf("argv = %q, wrapped with no policy", sp.cmd.Args)
	}
}
