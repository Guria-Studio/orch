package atomize

import (
	"fmt"
	"regexp"
	"strings"
)

// SourceIssue is the slice of a GitHub issue a spec is written from. Its own
// type, not vcs.Issue: atomize turns markdown into tasks and has no business
// knowing how gh spells an issue.
type SourceIssue struct {
	Number int
	Title  string
	Body   string
	URL    string
}

// IssueTaskID is the id of the one task IssueSpec writes for issue number:
// phase 0, the issue number as the package, task 1. The package number is
// what keeps two issues' tasks apart, in the atomizer's own id format so the
// spec can be split by hand into F0.<n>.T2, T3, ... and atomized as usual.
func IssueTaskID(number int) string { return fmt.Sprintf("F0.%d.T1", number) }

// issueSpecPrefix is how a spec file names the issue it came from. A re-run
// finds an imported issue by this prefix, not by the whole name, because the
// title in the slug can change on GitHub after the import.
func issueSpecPrefix(number int) string { return fmt.Sprintf("issue-%d", number) }

// IsIssueSpecFile reports whether base (a file name, no directory) is the
// spec `--from-issues` writes for issue number, whatever its slug says now.
func IsIssueSpecFile(base string, number int) bool {
	prefix := issueSpecPrefix(number)
	return base == prefix+".md" || strings.HasPrefix(base, prefix+"-") && strings.HasSuffix(base, ".md")
}

var reSlugJunk = regexp.MustCompile(`[^a-z0-9]+`)

// maxSlug bounds the title part of a spec's file name.
const maxSlug = 50

// IssueSpecFileName is `issue-<n>-<slug>.md`, the slug the title in
// lowercase ASCII words joined by dashes and cut at a word boundary;
// `issue-<n>.md` when the title has no ASCII letter or digit to slug.
func IssueSpecFileName(number int, title string) string {
	slug := strings.Trim(reSlugJunk.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > maxSlug {
		slug = slug[:maxSlug]
		if i := strings.LastIndex(slug, "-"); i > 0 {
			slug = slug[:i]
		}
		slug = strings.Trim(slug, "-")
	}
	if slug == "" {
		return issueSpecPrefix(number) + ".md"
	}
	return issueSpecPrefix(number) + "-" + slug + ".md"
}

// IssueSpec is the spec `orch atomize --from-issues` writes for one issue:
// one task, `IssueTaskID`, whose description is the issue's URL followed by
// its body.
//
// The body is quoted (every line behind "> ") on purpose. It is free
// markdown somebody typed on GitHub, and the spec parser acts on lines: a
// `- **Type**: feature` bullet would be read as an unknown field and end the
// description there, a `# F1 — ...` heading would start a phase, and a fence
// would hide the rest of the file. No parser rule matches a line that starts
// with ">", so the body reaches the task whole and the agent reads it as the
// quote it is.
//
// The issue's URL goes in the frontmatter too (`source_issue`), so every task
// the file defines carries it — including the ones an operator adds when
// splitting the issue into several tasks.
func IssueSpec(issue SourceIssue, projectID, model string) string {
	var b strings.Builder
	b.WriteString("---\ntype: spec\n")
	if projectID != "" {
		fmt.Fprintf(&b, "project_id: %s\n", projectID)
	}
	fmt.Fprintf(&b, "source_issue: %s\n---\n\n", issue.URL)
	b.WriteString("# F0 — GitHub issues\n\n")
	fmt.Fprintf(&b, "## F0.%d — %s\n\n", issue.Number, oneLine(issue.Title))
	fmt.Fprintf(&b, "Imported from GitHub issue #%d (%s) by `orch atomize --from-issues`.\n", issue.Number, issue.URL)
	b.WriteString("A later run never rewrites this file: split the work into more tasks\n")
	fmt.Fprintf(&b, "here (### F0.%d.T2, ...) and run `orch atomize --apply`.\n\n", issue.Number)
	fmt.Fprintf(&b, "### %s — %s\n\n", IssueTaskID(issue.Number), oneLine(issue.Title))
	b.WriteString(issue.URL + "\n")
	if body := strings.TrimSpace(strings.ReplaceAll(issue.Body, "\r\n", "\n")); body != "" {
		b.WriteString("\n")
		for _, line := range strings.Split(body, "\n") {
			if strings.TrimSpace(line) == "" {
				b.WriteString(">\n")
				continue
			}
			b.WriteString("> " + line + "\n")
		}
	}
	fmt.Fprintf(&b, "\n- **Model**: %s\n", model)
	return b.String()
}

// oneLine keeps a header on one line: a title is one line on GitHub, but a
// header that broke would lose the rest of the title to the description.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
