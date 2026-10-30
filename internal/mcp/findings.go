package mcp

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Guria-Studio/orch/internal/state"
	"github.com/Guria-Studio/orch/internal/vcs"
)

// FindingsRepo is where orch_report_finding files. Fixed: the findings are
// about orch, whichever project the agent happened to be working in.
const FindingsRepo = "Guria-Studio/orch"

// FindingsLabel marks an issue an agent filed. `orch sync issues` refuses to
// ingest it, so a report never loops back into a project as a task.
const FindingsLabel = "auto-reported"

// FindingsReporter is the slice of *vcs.GitHubProvider the tool needs.
type FindingsReporter interface {
	SearchIssues(repo, label, query string, limit int) ([]vcs.Issue, error)
	CreateIssue(repo, title, body string, labels []string) (string, error)
	CommentIssue(repo string, number int, body string) (string, error)
}

var findingTypes = map[string]bool{"bug": true, "improvement": true, "feature": true}

type reportFindingIn struct {
	Type         string `json:"type" jsonschema:"bug, improvement or feature"`
	Title        string `json:"title" jsonschema:"one line naming the problem or the idea, e.g. 'orch explain lists backlog tasks as ready'"`
	Summary      string `json:"summary" jsonschema:"what happens and what should happen instead"`
	Evidence     string `json:"evidence,omitempty" jsonschema:"what you saw: log lines, command output, file:line in orch. No secrets, no project data."`
	Repro        string `json:"repro,omitempty" jsonschema:"minimal steps to reproduce, for a bug"`
	SuggestedFix string `json:"suggested_fix,omitempty" jsonschema:"where in orch and how, if you know"`
	Confidence   string `json:"confidence,omitempty" jsonschema:"high, medium or low"`
	ConfirmNew   bool   `json:"confirm_new,omitempty" jsonschema:"file even though similar issues exist; set it only after reading the ones a previous call returned"`
}

type findingIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

type reportFindingOut struct {
	// Filed is true when a new issue was created; URL is then its address.
	Filed bool   `json:"filed"`
	URL   string `json:"url,omitempty"`
	// Duplicate is an existing issue with the same title, or an open one
	// this finding likely repeats: nothing was filed. Commented is true when
	// the call added a "seen again" comment to it; URL is then its address.
	Duplicate *findingIssue `json:"duplicate,omitempty"`
	Commented bool          `json:"commented,omitempty"`
	// Similar are existing issues whose titles match loosely. Without
	// confirm_new they stop the filing, so the caller reads them first.
	Similar []findingIssue `json:"similar,omitempty"`
	// Message says what happened and what to do next, for the model.
	Message string `json:"message"`
}

func (s *server) reportFinding(ctx context.Context, _ *mcpsdk.CallToolRequest, in reportFindingIn) (*mcpsdk.CallToolResult, reportFindingOut, error) {
	refuse := func(msg string) (*mcpsdk.CallToolResult, reportFindingOut, error) {
		return &mcpsdk.CallToolResult{IsError: true}, reportFindingOut{Message: msg}, nil
	}
	if s.opts.Findings == nil {
		return refuse("reporting findings is off for this project. Tell the operator instead; " +
			"they can enable it with `report_findings.enabled: true` in .orchestrator/config.yaml.")
	}
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.Title = strings.TrimSpace(in.Title)
	if !findingTypes[in.Type] {
		return refuse(fmt.Sprintf("type %q is not one of bug, improvement, feature", in.Type))
	}
	if in.Title == "" || strings.TrimSpace(in.Summary) == "" {
		return refuse("title and summary are required")
	}

	// What this project filed comes first: GitHub's search indexes a new
	// issue only after a while, and #433-#435 were filed within a minute.
	filed, err := s.opts.Backend.FiledFindings(ctx)
	if err != nil {
		return nil, reportFindingOut{}, fmt.Errorf("reading the findings this project filed: %w", err)
	}
	existing, err := s.opts.Findings.SearchIssues(FindingsRepo, FindingsLabel, strings.Join(keyTokens(in.Title), " "), 10)
	if err != nil {
		return nil, reportFindingOut{}, fmt.Errorf("searching %s for similar issues: %w", FindingsRepo, err)
	}
	closed := map[int]bool{}
	var similar []findingIssue
	var candidates []findingIssue
	candidateText := map[int]string{}
	for _, is := range existing {
		fi := findingIssue{Number: is.Number, Title: is.Title, State: is.State, URL: is.URL}
		if normalizeTitle(is.Title) == normalizeTitle(in.Title) && !strings.EqualFold(is.State, "open") {
			return nil, reportFindingOut{Duplicate: &fi,
				Message: fmt.Sprintf("already reported as #%d (%s); nothing filed", is.Number, is.State)}, nil
		}
		if !strings.EqualFold(is.State, "open") {
			closed[is.Number] = true
			similar = append(similar, fi)
			continue
		}
		candidates = append(candidates, fi)
		candidateText[is.Number] = is.Title + "\n" + is.Body
	}
	// ponytail: a row is taken as still open unless the search saw it closed;
	// a closed one the search missed gets a comment instead of a new issue.
	var mine []findingIssue
	for _, f := range filed {
		if closed[f.Number] {
			continue
		}
		mine = append(mine, findingIssue{Number: f.Number, Title: f.Title, State: "OPEN", URL: f.URL})
		candidateText[f.Number] = f.Title + "\n" + f.Summary
	}
	for _, c := range append(mine, candidates...) {
		if !likelyDuplicate(in.Title, in.Title+"\n"+in.Summary, c.Title, candidateText[c.Number]) {
			continue
		}
		task := os.Getenv("ORCH_TASK_ID")
		if task == "" {
			task = "an unnamed task"
		}
		note := fmt.Sprintf("Seen again in task %s at %s (orch `%s`).\n\n_Reported by an orch agent through `orch_report_finding`._",
			task, time.Now().UTC().Format(time.RFC3339), versionOr(s.opts.Version))
		if _, err := s.opts.Findings.CommentIssue(FindingsRepo, c.Number, s.redact(note)); err != nil {
			return nil, reportFindingOut{}, fmt.Errorf("commenting on %s#%d: %w", FindingsRepo, c.Number, err)
		}
		return nil, reportFindingOut{Duplicate: &c, Commented: true, URL: c.URL,
			Message: fmt.Sprintf("already reported as #%d (%s); added a \"seen again\" comment, nothing filed", c.Number, c.URL)}, nil
	}
	similar = append(similar, candidates...)
	if len(similar) > 0 && !in.ConfirmNew {
		return nil, reportFindingOut{Similar: similar,
			Message: "similar issues exist; nothing filed. If none of them is this finding, " +
				"call again with confirm_new: true."}, nil
	}

	title := s.redact(in.Title)
	body := s.redact(findingBody(in, s.opts.Version, time.Now()))
	url, err := s.opts.Findings.CreateIssue(FindingsRepo, title, body, []string{FindingsLabel})
	if err != nil {
		return nil, reportFindingOut{}, fmt.Errorf("filing the finding in %s: %w", FindingsRepo, err)
	}
	// Remembered so the next call from this project finds it at once; an
	// issue whose number cannot be read from its URL cannot be commented on.
	if n, err := strconv.Atoi(url[strings.LastIndex(url, "/")+1:]); err == nil {
		if err := s.opts.Backend.RecordFiledFinding(ctx, state.FiledFinding{Number: n, URL: url, Title: title,
			Summary: s.redact(in.Summary), FiledAt: time.Now()}); err != nil {
			return nil, reportFindingOut{Filed: true, URL: url, Similar: similar,
				Message: "filed " + url + " (not remembered for de-duplication: " + err.Error() + ")"}, nil
		}
	}
	return nil, reportFindingOut{Filed: true, URL: url, Similar: similar, Message: "filed " + url}, nil
}

var reNonWord = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeTitle is what two titles must share to be the same report.
func normalizeTitle(t string) string {
	return strings.Trim(reNonWord.ReplaceAllString(strings.ToLower(t), " "), " ")
}

// stopwords carry no meaning in a finding's title; "orch" is in every one.
var stopwords = map[string]bool{"a": true, "an": true, "the": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "on": true, "for": true, "is": true, "it": true, "its": true, "with": true,
	"as": true, "at": true, "by": true, "from": true, "be": true, "orch": true}

// keyTokens are a title's words without the stopwords, in order, once each.
func keyTokens(title string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(normalizeTitle(title)) {
		if !stopwords[w] && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// reCodeSpan is a `backticked` or "double-quoted" string: a command or an
// error message, as findings quote them.
var reCodeSpan = regexp.MustCompile("`([^`]+)`|\"([^\"]+)\"")

// codeSpans are the quoted strings of text, normalized, three characters or
// more.
func codeSpans(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range reCodeSpan.FindAllStringSubmatch(text, -1) {
		if span := normalizeTitle(m[1] + m[2]); len(span) >= 3 {
			out[span] = true
		}
	}
	return out
}

// likelyDuplicate is the plain test two findings must pass to be one (#441),
// no model involved: titles sharing half their key words, or two shared key
// words and the same quoted command or error string.
func likelyDuplicate(title, text, otherTitle, otherText string) bool {
	a, b := keyTokens(title), keyTokens(otherTitle)
	inA := map[string]bool{}
	for _, w := range a {
		inA[w] = true
	}
	shared := 0
	for _, w := range b {
		if inA[w] {
			shared++
		}
	}
	if union := len(a) + len(b) - shared; union > 0 && 2*shared >= union {
		return true
	}
	if shared < 2 {
		return false
	}
	spans := codeSpans(otherText)
	for span := range codeSpans(text) {
		if spans[span] {
			return true
		}
	}
	return false
}

// findingBody lays a finding out the way the reports filed by hand were.
func findingBody(in reportFindingIn, version string, now time.Time) string {
	version = versionOr(version)
	confidence := in.Confidence
	if confidence == "" {
		confidence = "unstated"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Type**: %s\n**About**: orch\n**Confidence**: %s\n**Captured at**: %s\n**Env**: orch `%s`, %s/%s\n",
		in.Type, confidence, now.UTC().Format(time.RFC3339), version, runtime.GOOS, runtime.GOARCH)
	for _, sec := range []struct{ head, text string }{
		{"Summary", in.Summary}, {"Evidence", in.Evidence},
		{"Repro", in.Repro}, {"Suggested fix", in.SuggestedFix},
	} {
		if strings.TrimSpace(sec.text) != "" {
			fmt.Fprintf(&b, "\n## %s\n%s\n", sec.head, strings.TrimSpace(sec.text))
		}
	}
	b.WriteString("\n---\n_Reported by an orch agent through `orch_report_finding`._\n")
	return b.String()
}

func versionOr(version string) string {
	if version == "" {
		return "unknown"
	}
	return version
}

// redact keeps the operator's machine out of a public issue: the project
// root becomes `<project>` and the home directory `~`. It cannot know what
// else is private, which is why the tool's description asks for no project
// data at all.
func (s *server) redact(text string) string {
	if root := s.opts.ProjectRoot; root != "" {
		text = strings.ReplaceAll(text, root, "<project>")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		text = strings.ReplaceAll(text, home, "~")
	}
	return text
}
