package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/sandbox"
)

// sandboxPolicy is what a task's agent may touch under dispatch.sandbox
// (#481): its worktree, the database `orch done` writes, the git directory
// its commits land in, dispatch.sandbox_allow, and read-only the project root
// (config.yaml, the .claude settings passed with --settings), its prompt and
// orch itself, which the agent runs for `orch done` and `orch mcp`. The
// system and toolchain paths are sandbox.For's.
func (s *Scheduler) sandboxPolicy(ctx context.Context, backend model.Backend, workdir, promptPath string) *sandbox.Policy {
	home, _ := os.UserHomeDir()
	writable := []string{s.dbDir()}
	if dir := gitCommonDir(ctx, workdir); dir != "" {
		writable = append(writable, dir)
	}
	for _, p := range s.Opts.Cfg.Dispatch.SandboxAllow {
		if rest, ok := strings.CutPrefix(p, "~/"); ok && home != "" {
			p = filepath.Join(home, rest)
		}
		writable = append(writable, p)
	}
	readable := []string{s.Opts.Cwd, filepath.Dir(promptPath)}
	if self, err := os.Executable(); err == nil {
		readable = append(readable, filepath.Dir(self))
	}
	p := sandbox.For(sandbox.Options{
		Backend:  backend,
		Workdir:  workdir,
		Writable: writable,
		Readable: readable,
		Home:     home,
		PathEnv:  os.Getenv("PATH"),
		TempDir:  os.TempDir(),
	})
	return &p
}

// gitCommonDir is the repository directory a worktree's commits write
// objects and refs into, outside the worktree itself; "" when dir is not in a
// git repository.
func gitCommonDir(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--git-common-dir").Output() // #nosec G204 -- fixed git argv; dir is the task's worktree path
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return common
}
