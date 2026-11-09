package evidence

import (
	"fmt"
	"math"
	"strings"

	"github.com/Guria-Studio/orch/internal/findings"
	"github.com/Guria-Studio/orch/internal/guard"
)

// CommentMarker opens the evidence comment on a task's PR: the comment orch
// edits in place on every push instead of adding another (§3.2).
const CommentMarker = "<!-- orch:evidence -->"

// Comment is the PR comment summarising m, in English like the rest of what
// orch writes on PRs. Every free text in it (commands, finding and step
// titles) passes Safe first; root is the project's root, which never shows.
// The media are kept on the operator's machine (#520 §9): the comment says
// where to see them instead of linking them.
func Comment(m Manifest, root string) string {
	var b strings.Builder
	b.WriteString(CommentMarker + "\n")
	fmt.Fprintf(&b, "### Evidence — %s (attempt %d)\n\n", safeOr(m.TaskID, root, "task"), m.Attempt)
	b.WriteString("| Check | Result |\n|---|---|\n")

	var kinds []string
	var checks, e2e int
	var checkS, e2eS float64
	for _, c := range m.Checks {
		if c.Kind == "e2e" {
			e2e++
			e2eS += c.DurationS
			continue
		}
		checks++
		checkS += c.DurationS
		if c.Kind != "" && !contains(kinds, c.Kind) {
			kinds = append(kinds, c.Kind)
		}
	}
	mark := "✅"
	if m.Verdict == VerdictFailed {
		mark = "❌"
	}
	if checks > 0 {
		label := "checks"
		if len(kinds) > 0 {
			label = strings.Join(kinds, " · ")
		}
		fmt.Fprintf(&b, "| %s (%s) | %s |\n", label, runs(checks, checkS), mark)
	}
	if len(m.Probes) > 0 {
		var parts []string
		ok := true
		for _, p := range m.Probes {
			ok = ok && p.OK
			parts = append(parts, fmt.Sprintf("`%s` %d", safeOr(p.Path, root, "…"), p.Status))
		}
		fmt.Fprintf(&b, "| app probes (%d) | %s %s |\n", len(m.Probes), okMark(ok), strings.Join(parts, ", "))
	}
	if e := m.E2E; e != nil {
		result := fmt.Sprintf("%d passed · %d failed", e.Passed, e.Failed)
		if e.Flaky > 0 {
			result += fmt.Sprintf(" · %d flaky", e.Flaky)
		}
		label := "E2E"
		if e2e > 0 {
			label += " (" + runs(e2e, e2eS) + ")"
		}
		fmt.Fprintf(&b, "| %s | %s %s |\n", label, okMark(e.Failed == 0), result)
	}
	var notes []string
	if r := m.Review; r != nil {
		switch {
		case r.Unavailable != "":
			fmt.Fprintf(&b, "| review | ⚠️ not reviewed: %s |\n", safeOr(r.Unavailable, root, "the review did not run"))
		default:
			minor := len(r.Findings) - r.Blocking
			result := "✅ no blocking findings"
			if r.Blocking > 0 {
				result = fmt.Sprintf("❌ %d blocking", r.Blocking)
			}
			if minor > 0 {
				result += fmt.Sprintf(" · %d minor", minor)
			}
			fmt.Fprintf(&b, "| review (%s) | %s |\n", safeOr(r.Reviewer, root, "another provider"), result)
			for _, f := range r.Findings {
				if title, ok := Safe(f.Title, root); ok {
					notes = append(notes, fmt.Sprintf("- %s: %s", f.Severity, cell(title)))
				}
			}
		}
	}

	b.WriteString("\n")
	var facts []string
	if d := m.Diff; d != nil {
		facts = append(facts, fmt.Sprintf("Diff: %s, +%d −%d", plural(d.Files, "file"), d.Insertions, d.Deletions))
	}
	if len(m.HeadSHA) >= 7 {
		facts = append(facts, fmt.Sprintf("commit `%s`", m.HeadSHA[:7]))
	}
	if len(facts) > 0 {
		b.WriteString(strings.Join(facts, " · ") + ".\n")
	}
	if e := m.E2E; e != nil {
		for _, w := range e.Walkthroughs {
			title, ok := Safe(w.Title, root)
			if !ok {
				continue
			}
			what := []string{plural(len(w.Steps), "step")}
			if w.Video != "" {
				what = append(what, "video")
			}
			fmt.Fprintf(&b, "\nWalk-through: %s (%s)\n", cell(title), strings.Join(what, ", "))
			for i, s := range w.Steps {
				if s, ok := Safe(s, root); ok {
					fmt.Fprintf(&b, "%d. %s\n", i+1, cell(s))
				}
			}
		}
		if n := len(e.Media); n > 0 {
			fmt.Fprintf(&b, "\n%s kept locally: open the task in orch's dashboard (operator), or `orch status --json`.\n",
				capitalise(mediaCount(e.Media)))
		}
	}
	if len(notes) > 0 {
		b.WriteString("\n<details><summary>Review notes</summary>\n\n" + strings.Join(notes, "\n") + "\n</details>\n")
	}
	return b.String()
}

// Safe is text fit to leave the machine: nothing when the guard sees a
// secret in it (the field is dropped whole, never masked), else redacted of
// the project's path, the home directory and anything shaped like a key.
func Safe(text, root string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	diff := "+++ b/evidence\n@@ -0,0 +1 @@\n+" + strings.ReplaceAll(text, "\n", "\n+")
	for _, f := range guard.Scan(diff) {
		if f.Rule == guard.RuleSecrets {
			return "", false
		}
	}
	return findings.Redact(text, root), true
}

func safeOr(text, root, fallback string) string {
	if s, ok := Safe(text, root); ok {
		return cell(s)
	}
	return fallback
}

// cell keeps text on one line and out of the table's syntax.
func cell(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

func runs(n int, seconds float64) string {
	return fmt.Sprintf("%s, %d s", plural(n, "command"), int(math.Round(seconds)))
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func okMark(ok bool) string {
	if ok {
		return "✅"
	}
	return "❌"
}

func mediaCount(media []Media) string {
	count := map[string]int{}
	var order []string
	for _, m := range media {
		if count[m.Kind] == 0 {
			order = append(order, m.Kind)
		}
		count[m.Kind]++
	}
	var parts []string
	for _, k := range order {
		word := map[string]string{"video": "video", "screenshot": "screenshot", "trace": "trace"}[k]
		if word == "" {
			word = "file"
		}
		parts = append(parts, plural(count[k], word))
	}
	return strings.Join(parts, ", ")
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
