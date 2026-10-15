package cli

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/hectorcanaimero/orch/internal/atomize"
	"github.com/hectorcanaimero/orch/internal/graph"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/project"
	"github.com/hectorcanaimero/orch/internal/prompt"
)

// specGitWarnings are `orch atomize`'s warnings about spec files a task's
// worktree will not see (#341).
//
// atomize reads specs from the working tree; a worktree starts from the base
// branch. A spec that is untracked, carries uncommitted edits, or is committed
// but not pushed produces tasks whose spec ref is missing — or stale — where
// the agent runs. Only files that produced tasks are checked, so the
// scaffold's specs/README.md does not earn a warning. Outside a git
// repository there is no worktree to miss anything, and nothing is said.
func specGitWarnings(root, base string, parse atomize.ParseResult) []string {
	if !project.InGitRepo(root) {
		return nil
	}
	ref := project.BaseRef(root, base)
	where := "the base branch"
	if ref != "" {
		where = ref
	}
	seen := map[string]bool{}
	var files []string
	for _, t := range parse.Tasks {
		if !seen[t.SourceFile] {
			seen[t.SourceFile] = true
			files = append(files, t.SourceFile)
		}
	}
	sort.Strings(files)

	var out []string
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil || outsideRoot(abs, root) {
			continue
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		st := project.GitStateOf(root, rel, base)
		switch {
		case st.Untracked:
			out = append(out, fmt.Sprintf(
				"warning: %s is not committed — a task's worktree starts from %s and will not have it; commit and push it before `orch run`",
				rel, where))
		case st.BaseRef != "" && !st.OnBase:
			out = append(out, fmt.Sprintf(
				"warning: %s is not on %s — a task's worktree starts there and will not have it; push it before `orch run`",
				rel, st.BaseRef))
		case st.Modified:
			out = append(out, fmt.Sprintf(
				"warning: %s has uncommitted changes — a task's worktree starts from %s and will read the committed version; commit and push them before `orch run`",
				rel, where))
		}
	}
	return out
}

// specProblems is `orch validate`'s spec check (#341, #342): every task whose
// specRef names a file, or a heading, that is not there.
//
// Looked for twice when the project is a git repository with a base branch:
// in the working tree, which is what the operator sees and `orch atomize`
// reads, and in the base ref, which is what a task's worktree starts from. A
// spec only in the first is the one an agent was told to READ FIRST and could
// not find.
//
// Warnings, not errors: a scaffolded project's tasks name specs nobody has
// written yet, and dispatch copes with that by telling the agent so. A spec
// that is in the project but not on the base blocks the task at dispatch,
// which is what the remediation is for. Reads the file's specRef, which is
// authored data, so no Hydrate is needed (rule 30).
func specProblems(root, specRoot, base string, tasks []model.Task) []graph.Problem {
	if specRoot == "" {
		specRoot = prompt.DefaultSpecRoot
	}
	ref := ""
	if project.InGitRepo(root) {
		ref = project.BaseRef(root, base)
	}
	var problems []graph.Problem
	add := func(id, msg, fix string) {
		taskID := id
		problems = append(problems, graph.Problem{
			TaskID:      &taskID,
			Field:       "specRef",
			Kind:        "spec.missing",
			Message:     msg,
			Remediation: strPtr(fix),
			Severity:    graph.SeverityWarn,
		})
	}
	for _, t := range tasks {
		if t.SpecRef == "" {
			continue
		}
		local, err := project.CheckSpecRef(root, specRoot, t.SpecRef)
		if err != nil {
			add(t.ID, fmt.Sprintf("spec ref %s cannot be read: %v", t.SpecRef, err),
				"Fix the file's permissions, or the task's specRef.")
			continue
		}
		if !local.FileFound {
			add(t.ID, fmt.Sprintf("spec file %s not found", local.File),
				"Write the spec, or fix the task's specRef; until then its agent works from the description alone.")
			continue
		}
		if !local.AnchorFound {
			add(t.ID, fmt.Sprintf("spec file %s has no heading for %s", local.File, local.Anchor),
				"Add the heading to the spec, or fix the anchor in the task's specRef.")
			continue
		}
		if ref == "" {
			continue
		}
		if !project.ExistsAtRef(root, ref, local.File) {
			add(t.ID, fmt.Sprintf("spec file %s is not on %s", local.File, ref),
				"Commit and push it: the task's worktree starts from "+ref+", and the task blocks at dispatch without it.")
			continue
		}
		if local.Anchor == "" {
			continue
		}
		body, err := project.ReadAtRef(root, ref, local.File)
		if err == nil && !project.HasAnchor(body, local.Anchor) {
			add(t.ID, fmt.Sprintf("spec file %s on %s has no heading for %s", local.File, ref, local.Anchor),
				"Commit and push the spec: the working tree has the heading, the task's worktree will not.")
		}
	}
	return problems
}
