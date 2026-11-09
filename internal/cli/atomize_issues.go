package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/atomize"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/router"
	"github.com/Guria-Studio/orch/internal/vcs"
)

// atomizeIssuesOpts is what `orch atomize --from-issues` was asked.
type atomizeIssuesOpts struct {
	query      string
	model      string
	allAuthors bool
	specsRoot  string
	tasksPath  string
	apply      bool
	noBackup   bool
	list       bool
}

// issueImport is one issue that gets a spec written for it.
type issueImport struct {
	issue vcs.Issue
	path  string // absolute
	text  string
}

// issuePlan is what an import would write, worked out before anything is.
type issuePlan struct {
	Write []issueImport
	// Imported names the issues that already have a spec or a task, with
	// where it is: a re-run never writes a second spec for one, and never
	// overwrites the first (it may have been split by hand since).
	Imported []string
	// Taken names the issues whose task id another task already uses.
	Taken []string
}

// planIssueImport decides, per open issue, whether it gets a spec. Pure:
// specFiles is the spec walk, existing the tasks in tasks.json, parsedIDs
// the task ids the walked specs define.
//
// An issue counts as imported when a spec file is named for its number
// (whatever the slug says now: titles change on GitHub) or a task carries its
// URL in sourceIssue (the spec may have been deleted after the import).
func planIssueImport(issues []vcs.Issue, specFiles []string, existing []model.Task, parsedIDs map[string]bool,
	root, specsRoot, projectID, modelName string) issuePlan {
	byIssue := map[string]bool{}
	taken := map[string]string{} // task id -> its sourceIssue
	for _, t := range existing {
		if t.SourceIssue != "" {
			byIssue[t.SourceIssue] = true
		}
		taken[t.ID] = t.SourceIssue
	}

	sorted := append([]vcs.Issue(nil), issues...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Number < sorted[j].Number })

	var plan issuePlan
	for _, issue := range sorted {
		if spec := issueSpecFile(specFiles, issue.Number); spec != "" {
			plan.Imported = append(plan.Imported, fmt.Sprintf("#%d (%s)", issue.Number, relTo(root, spec)))
			continue
		}
		if byIssue[issue.URL] {
			plan.Imported = append(plan.Imported, fmt.Sprintf("#%d (tasks.json)", issue.Number))
			continue
		}
		id := atomize.IssueTaskID(issue.Number)
		if src, ok := taken[id]; (ok && src != issue.URL) || parsedIDs[id] {
			plan.Taken = append(plan.Taken, fmt.Sprintf("#%d (%s)", issue.Number, id))
			continue
		}
		src := atomize.SourceIssue{Number: issue.Number, Title: issue.Title, Body: issue.Body, URL: issue.URL}
		plan.Write = append(plan.Write, issueImport{
			issue: issue,
			path:  filepath.Join(specsRoot, atomize.IssueSpecFileName(issue.Number, issue.Title)),
			text:  atomize.IssueSpec(src, projectID, modelName),
		})
	}
	return plan
}

func issueSpecFile(specFiles []string, number int) string {
	for _, f := range specFiles {
		if atomize.IsIssueSpecFile(filepath.Base(f), number) {
			return f
		}
	}
	return ""
}

func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

func hasLabel(issue vcs.Issue, label string) bool {
	for _, l := range issue.LabelNames() {
		if l == label {
			return true
		}
	}
	return false
}

// runAtomizeFromIssues is `orch atomize --from-issues <query>` (#525): each
// open GitHub issue the query matches gets a spec, specs/issue-<n>-<slug>.md,
// and the specs become tasks through the same parse, merge and diff as any
// other `orch atomize`. Read-only unless --apply, which writes the specs and
// then tasks.json.
//
// The same guards as `orch sync issues`, for the same reasons: --model must be
// one the router resolves (bug 14), only collaborators' issues are imported
// unless --all-authors (an issue body reaches an agent's prompt, #542), and
// issues labelled auto-reported are skipped (orch files those itself).
func runAtomizeFromIssues(cmd *cobra.Command, flags *projectFlags, o atomizeIssuesOpts) error {
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		return err
	}
	rtr, err := router.Load(paths.RouterYAML())
	if err != nil {
		return withExitCode(2, fmt.Errorf("checking --model against %s: %w", paths.RouterYAML(), err))
	}
	if err := checkSyncModel(o.model, rtr.Keys()); err != nil {
		return withExitCode(2, err)
	}

	gh := vcs.NewGitHubProvider()
	issues, err := gh.ListIssues(vcs.IssueQuery{Search: o.query, State: "open", Dir: paths.Root})
	if err != nil {
		return fmt.Errorf("listing issues for project %s: %w", paths.ID, err)
	}
	var autoReported []string
	kept := issues[:0:0]
	for _, issue := range issues {
		if hasLabel(issue, autoReportedLabel) {
			autoReported = append(autoReported, fmt.Sprintf("#%d", issue.Number))
			continue
		}
		kept = append(kept, issue)
	}
	issues = kept
	var strangers []vcs.Issue
	if !o.allAuthors {
		issues, strangers, err = splitByCollaborator(issues, func(login string) (bool, error) {
			return gh.IsCollaborator(paths.Root, login)
		})
		if err != nil {
			return fmt.Errorf("%w (--all-authors imports every author's issues without asking)", err)
		}
	}

	specFiles, err := atomize.WalkSpecFiles(o.specsRoot)
	if err != nil {
		return fmt.Errorf("walk specs under %s: %w", o.specsRoot, err)
	}
	parse, err := atomize.ParseFiles(specFiles, o.specsRoot, paths.ID)
	if err != nil {
		return fmt.Errorf("parse specs under %s: %w", o.specsRoot, err)
	}
	existing, err := atomize.LoadExisting(o.tasksPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", o.tasksPath, err)
	}
	parsedIDs := map[string]bool{}
	for _, t := range parse.Tasks {
		parsedIDs[t.ID] = true
	}
	plan := planIssueImport(issues, specFiles, existing.Tasks, parsedIDs, paths.Root, o.specsRoot, paths.ID, o.model)
	if err := printIssuePlan(cmd, plan, o, paths.Root, strangers, autoReported); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if o.apply && len(plan.Write) > 0 {
		if err := os.MkdirAll(o.specsRoot, 0o750); err != nil {
			return fmt.Errorf("creating %s: %w", o.specsRoot, err)
		}
		for _, w := range plan.Write {
			// O_EXCL: a spec that appeared since the plan is someone's, and
			// is never overwritten.
			f, err := os.OpenFile(w.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- a path under the project's own spec root
			if err != nil {
				return fmt.Errorf("writing the spec for issue #%d: %w", w.issue.Number, err)
			}
			_, werr := f.WriteString(w.text)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				return fmt.Errorf("writing %s: %w", w.path, werr)
			}
		}
		if _, err := fmt.Fprintf(out, "Wrote %d spec(s)\n", len(plan.Write)); err != nil {
			return err
		}
		specFiles, err = atomize.WalkSpecFiles(o.specsRoot)
		if err != nil {
			return fmt.Errorf("walk specs under %s: %w", o.specsRoot, err)
		}
		if parse, err = atomize.ParseFiles(specFiles, o.specsRoot, paths.ID); err != nil {
			return fmt.Errorf("parse specs under %s: %w", o.specsRoot, err)
		}
	} else {
		// Dry run: the specs the import would write are parsed from memory,
		// so the diff below is the one --apply would produce.
		for _, w := range plan.Write {
			r := atomize.ParseText(w.text, w.path, o.specsRoot, paths.ID)
			parse.FilesScanned = append(parse.FilesScanned, r.FilesScanned...)
			parse.Warnings = append(parse.Warnings, r.Warnings...)
			parse.Tasks = append(parse.Tasks, r.Tasks...)
		}
	}

	if len(parse.Tasks) == 0 && len(parse.FilesScanned) == 0 {
		return nil
	}
	if o.apply {
		for _, w := range specGitWarnings(paths.Root, cfg.Dispatch.BaseBranch, parse) {
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), w); err != nil {
				return err
			}
		}
	}
	if o.list {
		return renderAtomizeList(cmd, parse)
	}

	_, diff := atomize.MergeTasks(existing, parse.Tasks)
	if _, err := fmt.Fprint(out, "\n"+atomize.RenderDiff(diff, parse)); err != nil {
		return err
	}
	if !o.apply {
		return nil
	}
	if len(diff.NewTasks) == 0 && len(diff.Updated) == 0 {
		_, err := fmt.Fprintln(out, "--apply pasado pero no hay cambios que aplicar.")
		return err
	}
	backupPath, err := project.MutateTasks(paths.StateDir(), o.tasksPath, !o.noBackup, func(tf *model.TasksFile) error {
		*tf, _ = atomize.MergeTasks(*tf, parse.Tasks)
		return nil
	})
	if err != nil {
		return fmt.Errorf("write %s: %w", o.tasksPath, err)
	}
	if _, err := fmt.Fprintf(out, "✓ Escrito: %s\n", o.tasksPath); err != nil {
		return err
	}
	if backupPath != "" {
		_, err = fmt.Fprintf(out, "  backup: %s\n", backupPath)
	}
	return err
}

// printIssuePlan is the import's half of the report; the atomize diff
// follows it. Same text with and without --apply but for the verb.
func printIssuePlan(cmd *cobra.Command, plan issuePlan, o atomizeIssuesOpts, root string, strangers []vcs.Issue, autoReported []string) error {
	var b strings.Builder
	if len(plan.Write) == 0 {
		fmt.Fprintf(&b, "No new issues matching %q — nothing to write.\n", o.query)
	} else {
		verb := "Would write"
		if o.apply {
			verb = "Writing"
		}
		fmt.Fprintf(&b, "%s %d spec(s) from issues matching %q:\n", verb, len(plan.Write), o.query)
		for _, w := range plan.Write {
			fmt.Fprintf(&b, "  %s  #%d %s\n", relTo(root, w.path), w.issue.Number, firstLineOf(w.issue.Title))
		}
	}
	if len(plan.Imported) > 0 {
		fmt.Fprintf(&b, "Already imported: %s\n", strings.Join(plan.Imported, ", "))
	}
	if len(plan.Taken) > 0 {
		fmt.Fprintf(&b, "Skipping %d issue(s) whose task id another task already uses: %s\n",
			len(plan.Taken), strings.Join(plan.Taken, ", "))
	}
	if len(strangers) > 0 {
		names := make([]string, 0, len(strangers))
		for _, s := range strangers {
			names = append(names, fmt.Sprintf("#%d (@%s)", s.Number, s.Author.Login))
		}
		fmt.Fprintf(&b, "Skipping %d issue(s) opened by people who are not collaborators of the repo: %s (--all-authors imports them)\n",
			len(strangers), strings.Join(names, ", "))
	}
	if len(autoReported) > 0 {
		fmt.Fprintf(&b, "Skipping %d issue(s) labelled %s (orch files those itself): %s\n",
			len(autoReported), autoReportedLabel, strings.Join(autoReported, ", "))
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), b.String())
	return err
}

// errFromIssuesWithFile: --file names one spec on disk, --from-issues writes
// new ones; together there is no single input to atomize.
var errFromIssuesWithFile = errors.New("--from-issues and --file name two different inputs: pass one")
