package worktree

import (
	"fmt"
	"os/exec"
	"strings"
)

// BaseProblem says why task worktrees cannot branch from base in root — not a
// repository, or no commit on the base yet, the state `git init` leaves —
// with the command that fixes it; "" when they can. #376: a fresh repository
// blocked its first task on git's "invalid reference: main".
func BaseProblem(root, base string) string {
	if base == "" {
		base = "main"
	}
	if exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Run() != nil { // #nosec G204 -- fixed git arguments
		return `each task works on its own git branch, and this is not a git repository yet: git init && git add -A && git commit -m "orch init"`
	}
	for _, ref := range []string{base, "origin/" + base} {
		if exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run() == nil { // #nosec G204 -- fixed git arguments; base comes from the project's config
			return ""
		}
	}
	return fmt.Sprintf(`each task branches from %s, which has no commit yet: git add -A && git commit -m "orch init"`, base)
}

// HasRemote says whether root's repository has any remote to push to.
func HasRemote(root string) bool {
	out, err := exec.Command("git", "-C", root, "remote").Output() // #nosec G204 -- fixed git arguments; root is the project root
	return err == nil && strings.TrimSpace(string(out)) != ""
}
