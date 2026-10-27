// Package sandbox confines a dispatched agent's filesystem access to what its
// task needs (dispatch.sandbox, #481): read-write its worktree, the project's
// database directory, the git directory its commits land in, /tmp and its own
// CLI's config; read-only the system and the toolchains on PATH; nothing else
// in $HOME — not ~/.ssh, not another project, not another task's worktree.
//
// The mechanism is Landlock, Linux only. A Landlock ruleset binds the process
// that applies it and every child it forks, and cannot be lifted, so it is
// applied in a launcher rather than in orch: the engine spawns
// `orch sandbox-exec --rw … --ro … -- <cli> <args>`, which restricts itself
// and then execs the CLI in its place (Wrap, Exec). The process group, the
// log file descriptors and the timeout the engine set up all carry over the
// exec unchanged.
//
// Policy building (For) is pure, so the path list is pinned by unit tests;
// applying it is in sandbox_linux.go.
package sandbox

import (
	"path/filepath"
	"strings"

	"github.com/hectorcanaimero/orch/internal/model"
)

// Command is the hidden subcommand the launcher runs as.
const Command = "sandbox-exec"

// MinABI is the oldest Landlock ABI the sandbox runs on. ABI 1 (Linux 5.13 to
// 5.18) cannot grant renaming a file across directories, which git does on
// every commit (an object moves from a temporary name into objects/xx/);
// go-landlock's best-effort mode then turns the whole ruleset into a no-op
// rather than break that. A sandbox that silently confines nothing is worse
// than none, so ABI 1 is refused like a kernel without Landlock.
const MinABI = 2

// Policy is what a sandboxed agent may touch. A path covers everything
// beneath it; a file path covers that file. Paths that do not exist when the
// launcher starts are skipped.
type Policy struct {
	RW []string
	RO []string
}

// Options are the facts For needs about one dispatch.
type Options struct {
	// Backend picks which CLI's config directories are writable.
	Backend model.Backend
	// Workdir is the task's worktree (the project root without worktree mode).
	Workdir string
	// Writable are other directories the agent must write: the database
	// directory `orch done` writes, the git common dir a worktree commits into.
	Writable []string
	// Readable are other paths it must read: the project root (config.yaml,
	// .claude/settings.json), the prompt's directory, orch's own binary.
	Readable []string
	// Home is $HOME; PathEnv is $PATH; TempDir is os.TempDir().
	Home, PathEnv, TempDir string
}

// systemRO is the base system every CLI and toolchain reads. /run holds the
// resolver's resolv.conf on systemd hosts; /sys is read by node and go.
var systemRO = []string{
	"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc", "/opt",
	"/sys", "/run", "/dev", "/nix", "/snap", "/home/linuxbrew",
}

// systemRW is scratch space and the device files programs open for writing.
// /proc is writable because codex's own bwrap sandbox writes its uid_map
// there; Landlock still refuses /proc/<pid> of any process outside the
// sandbox (it goes through the ptrace check), so another process's environ
// stays unreadable.
var systemRW = []string{
	"/tmp", "/var/tmp", "/proc", "/dev/null", "/dev/zero", "/dev/full", "/dev/tty", "/dev/pts", "/dev/shm",
}

// homeRO are $HOME paths every agent reads: git's identity for its commits,
// the shared skills directory (~/.agents/skills), and the usual per-user
// toolchain roots (~/.local/bin and the installs it links to, ~/go's module
// cache).
var homeRO = []string{".gitconfig", ".config/git", ".agents", ".local", "go"}

// homeRW are $HOME caches every toolchain writes (go build, npm, pnpm, pip).
var homeRW = []string{".cache", ".npm"}

// cliRW is each CLI's own config, credentials and session state, relative to
// $HOME. Only the dispatched CLI's are opened, so a claude agent cannot read
// codex's auth.json.
var cliRW = map[model.Backend][]string{
	model.BackendClaude:   {".claude", ".claude.json"},
	model.BackendCodex:    {".codex"},
	model.BackendOpencode: {".config/opencode", ".local/share/opencode", ".local/state/opencode"},
	model.BackendGemini:   {".gemini"},
	// agy keeps its settings under ~/.gemini/antigravity-cli.
	model.BackendAgy:  {".gemini"},
	model.BackendMuse: {".local/share/muse", ".config/muse"},
}

// For builds the policy of one dispatch.
//
// Every directory on PATH is readable, and so is the parent of one named bin
// (~/.nvm/versions/node/vX/bin's node_modules live beside it). A path that is
// $HOME itself or above it is dropped whatever asked for it — a PATH entry
// like ~/bin must not open the whole home directory — and so is anything
// relative.
func For(o Options) Policy {
	home := filepath.Clean(o.Home)
	var rw, ro []string
	rw = append(rw, o.Workdir)
	rw = append(rw, o.Writable...)
	rw = append(rw, systemRW...)
	if o.TempDir != "" {
		rw = append(rw, o.TempDir)
	}
	ro = append(ro, systemRO...)
	ro = append(ro, o.Readable...)
	if o.Home != "" {
		for _, p := range homeRW {
			rw = append(rw, filepath.Join(home, p))
		}
		for _, p := range cliRW[o.Backend] {
			rw = append(rw, filepath.Join(home, p))
		}
		for _, p := range homeRO {
			ro = append(ro, filepath.Join(home, p))
		}
	}
	for _, dir := range filepath.SplitList(o.PathEnv) {
		ro = append(ro, dir)
		if filepath.Base(dir) == "bin" {
			ro = append(ro, filepath.Dir(dir))
		}
	}
	return Policy{RW: clean(rw, home), RO: clean(ro, home)}
}

// clean drops empty, relative and too-wide paths and duplicates, keeping the
// first occurrence's order so the argv is stable.
func clean(paths []string, home string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		if p == "" || !filepath.IsAbs(p) {
			continue
		}
		p = filepath.Clean(p)
		if p == "/" || (home != "" && home != "/" && isWithin(home, p)) || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// isWithin reports whether path is dir or below it.
func isWithin(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// Wrap is argv run through the launcher at self (orch's own binary).
func Wrap(self string, p Policy, argv []string) []string {
	out := []string{self, Command}
	for _, d := range p.RW {
		out = append(out, "--rw", d)
	}
	for _, d := range p.RO {
		out = append(out, "--ro", d)
	}
	out = append(out, "--")
	return append(out, argv...)
}
