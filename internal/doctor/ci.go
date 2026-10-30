package doctor

import (
	"os"
	"path/filepath"

	"github.com/Guria-Studio/orch/internal/ci"
	"github.com/Guria-Studio/orch/internal/vcs"
)

// CheckCIStack warns when orch-ci.yml is still the placeholder `orch init`
// wrote before the code existed, and the repository now has checks to run:
// every PR passes a CI that checks nothing (#363).
func CheckCIStack(root string) Check {
	const name = "ci.stack"
	if _, err := os.Stat(filepath.Join(root, ci.WorkflowPath)); err != nil {
		return Check{Name: name, Status: StatusSkip, Detail: ci.WorkflowPath + " not found"}
	}
	if !ci.PlaceholderOutgrown(root) {
		return Check{Name: name, Status: StatusOK, Detail: ci.WorkflowPath + " is not init's placeholder, or there is still nothing to check"}
	}
	return Check{
		Name: name, Status: StatusWarn,
		Detail:      ci.WorkflowPath + " is still the placeholder that checks nothing, and the repository now has a stack",
		Remediation: "Run `orch ci setup --force` to have the real pipeline proposed (`--dry-run` shows it first).",
	}
}

// CheckCIMinutes says whether the repository's Actions minutes are billed:
// a private repository pays for every run, and orch opens a pull request per
// task. visibility is vcs.RepoVisibility; a variable at the call site keeps
// doctor's tests off the network.
func CheckCIMinutes(root string, visibility func(string) (string, error)) Check {
	const name = "ci.minutes"
	if _, err := os.Stat(filepath.Join(root, ci.WorkflowPath)); err != nil {
		return Check{Name: name, Status: StatusSkip, Detail: ci.WorkflowPath + " not found"}
	}
	v, err := visibility(root)
	if err != nil || v == "" {
		return Check{Name: name, Status: StatusSkip, Detail: "could not tell whether the repository is private (gh, a GitHub remote)"}
	}
	if v == "public" {
		return Check{Name: name, Status: StatusOK, Detail: "public repository: GitHub does not bill its Actions minutes"}
	}
	return Check{
		Name: name, Status: StatusWarn,
		Detail:      v + " repository: every task's PR runs orch-ci on billed minutes — " + vcs.FreeMinutesNote,
		Remediation: "`orch ci usage` shows this month's minutes and what the remaining tasks would add.",
	}
}
