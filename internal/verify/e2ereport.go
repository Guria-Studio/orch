package verify

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ReportFile is the name of the report verify writes into a task attempt's
// artifacts directory: Playwright's results, in orch's own shape, with every
// attachment path relative to that directory. The dashboard reads it.
const ReportFile = "report.json"

// maxAttachmentBytes skips copying an attachment bigger than this: a long
// video of a hung test is not worth a disk full of state.
const maxAttachmentBytes = 100 << 20

// E2EReport is one E2E run of one task attempt.
type E2EReport struct {
	TaskID     string    `json:"task_id"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS int64     `json:"duration_ms"`
	// AppURL is where the app answered, when verify.app ran it.
	AppURL string    `json:"app_url,omitempty"`
	Stats  E2EStats  `json:"stats"`
	Tests  []E2ETest `json:"tests"`
	// Errors are the run's own errors, outside any test: a config that did
	// not load, a webServer that never came up.
	Errors []string `json:"errors,omitempty"`
}

// E2EStats counts the tests by outcome.
type E2EStats struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Flaky   int `json:"flaky"`
	Skipped int `json:"skipped"`
}

// E2ETest is one test, as its last result left it.
type E2ETest struct {
	// Title is the describe blocks and the test, joined with " › " as
	// Playwright prints them.
	Title   string `json:"title"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Project string `json:"project,omitempty"`
	// Status is passed, failed, flaky or skipped.
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Retries    int    `json:"retries"`
	// Error is the failure's message without terminal colours; Location is
	// where it was raised, file:line.
	Error    string `json:"error,omitempty"`
	Location string `json:"location,omitempty"`
	// Steps are the test.step titles the last result ran, in order: the
	// last one is where a failure happened, or after it.
	Steps       []string        `json:"steps,omitempty"`
	Attachments []E2EAttachment `json:"attachments,omitempty"`
}

// E2EAttachment is a file the test left: screenshot, video, trace, …
type E2EAttachment struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	// Path is relative to the artifacts directory, "" when the file was not
	// copied (missing, or over maxAttachmentBytes).
	Path string `json:"path,omitempty"`
	// Source is where the test left it, relative to the worktree.
	Source string `json:"source"`
}

// playwrightReport is the part of Playwright's JSON reporter output orch
// reads (testdata/playwright/<version>/report.json is a real one).
type playwrightReport struct {
	Suites []pwSuite `json:"suites"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Stats struct {
		StartTime time.Time `json:"startTime"`
		Duration  float64   `json:"duration"`
	} `json:"stats"`
}

type pwSuite struct {
	Title  string    `json:"title"`
	File   string    `json:"file"`
	Specs  []pwSpec  `json:"specs"`
	Suites []pwSuite `json:"suites"`
}

type pwSpec struct {
	Title string `json:"title"`
	File  string `json:"file"`
	Line  int    `json:"line"`
	Tests []struct {
		ProjectName string `json:"projectName"`
		// Status is expected, unexpected, flaky or skipped.
		Status  string `json:"status"`
		Results []struct {
			Status   string  `json:"status"`
			Duration float64 `json:"duration"`
			Retry    int     `json:"retry"`
			Error    *struct {
				Message string `json:"message"`
			} `json:"error"`
			ErrorLocation *struct {
				File string `json:"file"`
				Line int    `json:"line"`
			} `json:"errorLocation"`
			Steps []struct {
				Title string `json:"title"`
			} `json:"steps"`
			Attachments []struct {
				Name        string `json:"name"`
				ContentType string `json:"contentType"`
				Path        string `json:"path"`
			} `json:"attachments"`
		} `json:"results"`
	} `json:"tests"`
}

// ansi matches terminal colour codes, which Playwright puts in its messages.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// collectE2E reads the Playwright JSON report at reportPath, which the E2E
// run in worktree wrote, copies its attachments into artifactsDir and writes
// the report there as ReportFile. The source report is removed: it is orch's
// to read, not the task's to commit.
func collectE2E(reportPath, worktree, artifactsDir, taskID, appURL string) (*E2EReport, error) {
	b, err := os.ReadFile(reportPath) // #nosec G304 -- verify.e2e_report, inside the task's worktree
	if err != nil {
		return nil, fmt.Errorf("reading the E2E report: %w", err)
	}
	var pw playwrightReport
	if err := json.Unmarshal(b, &pw); err != nil {
		return nil, fmt.Errorf("reading the E2E report %s: %w", reportPath, err)
	}
	if err := os.MkdirAll(artifactsDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating %s: %w", artifactsDir, err)
	}
	rep := &E2EReport{
		TaskID:     taskID,
		StartedAt:  pw.Stats.StartTime,
		DurationMS: int64(pw.Stats.Duration),
		AppURL:     appURL,
		Tests:      []E2ETest{},
	}
	for _, e := range pw.Errors {
		rep.Errors = append(rep.Errors, strings.TrimSpace(ansi.ReplaceAllString(e.Message, "")))
	}
	for _, s := range pw.Suites {
		rep.walk(s, nil, worktree, artifactsDir)
	}
	out, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(artifactsDir, ReportFile), out, 0o600); err != nil {
		return nil, fmt.Errorf("writing the E2E report: %w", err)
	}
	_ = os.Remove(reportPath)
	return rep, nil
}

// walk adds every test under s. titles are the enclosing describe blocks; a
// file-level suite's title is its file name, which the test carries anyway.
func (r *E2EReport) walk(s pwSuite, titles []string, worktree, artifactsDir string) {
	if s.Title != "" && s.Title != s.File {
		titles = append(titles, s.Title)
	}
	for _, spec := range s.Specs {
		for _, t := range spec.Tests {
			test := E2ETest{
				Title:   strings.Join(append(append([]string(nil), titles...), spec.Title), " › "),
				File:    spec.File,
				Line:    spec.Line,
				Project: t.ProjectName,
				Status:  testStatus(t.Status),
			}
			if n := len(t.Results); n > 0 {
				res := t.Results[n-1]
				test.DurationMS = int64(res.Duration)
				test.Retries = res.Retry
				if res.Error != nil {
					test.Error = strings.TrimSpace(ansi.ReplaceAllString(res.Error.Message, ""))
				}
				if res.ErrorLocation != nil {
					test.Location = fmt.Sprintf("%s:%d", relTo(worktree, res.ErrorLocation.File), res.ErrorLocation.Line)
				}
				for _, st := range res.Steps {
					test.Steps = append(test.Steps, st.Title)
				}
				dir := attachmentDir(len(r.Tests))
				for _, a := range res.Attachments {
					test.Attachments = append(test.Attachments, copyAttachment(a.Name, a.ContentType, a.Path, worktree, artifactsDir, dir))
				}
			}
			switch test.Status {
			case "passed":
				r.Stats.Passed++
			case "failed":
				r.Stats.Failed++
			case "flaky":
				r.Stats.Flaky++
			default:
				r.Stats.Skipped++
			}
			r.Tests = append(r.Tests, test)
		}
	}
	for _, child := range s.Suites {
		r.walk(child, titles, worktree, artifactsDir)
	}
}

// testStatus is Playwright's verdict on a test in plain words.
func testStatus(pw string) string {
	switch pw {
	case "expected":
		return "passed"
	case "unexpected":
		return "failed"
	case "flaky":
		return "flaky"
	}
	return "skipped"
}

// attachmentDir is where test i's attachments go, inside the artifacts
// directory: by index, as titles make poor file names.
func attachmentDir(i int) string { return fmt.Sprintf("t%03d", i+1) }

func copyAttachment(name, contentType, src, worktree, artifactsDir, dir string) E2EAttachment {
	a := E2EAttachment{Name: name, ContentType: contentType, Source: relTo(worktree, src)}
	if src == "" {
		return a
	}
	info, err := os.Stat(src)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxAttachmentBytes {
		return a
	}
	rel := filepath.Join(dir, filepath.Base(src))
	if err := copyFile(src, filepath.Join(artifactsDir, rel)); err != nil {
		return a
	}
	a.Path = filepath.ToSlash(rel)
	return a
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	in, err := os.Open(src) // #nosec G304 -- an attachment Playwright wrote in the task's worktree
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- under orch's own state dir
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// relTo is path relative to dir when it is inside it, else path unchanged.
func relTo(dir, path string) string {
	if rel, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

// failureSummary is what the agent is told about a failed E2E run: each
// failing test, where, the step it was in, the first lines of the error and
// the files it left in the worktree.
func (r *E2EReport) failureSummary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d E2E tests failed", r.Stats.Failed, len(r.Tests))
	if r.AppURL != "" {
		fmt.Fprintf(&b, " against the app at %s", r.AppURL)
	}
	b.WriteString(":\n")
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "\n- the run itself: %s\n", firstLines(e, 6))
	}
	for _, t := range r.Tests {
		if t.Status != "failed" {
			continue
		}
		where := t.Location
		if where == "" {
			where = fmt.Sprintf("%s:%d", t.File, t.Line)
		}
		fmt.Fprintf(&b, "\n- %s (%s)\n", t.Title, where)
		if n := len(t.Steps); n > 0 {
			fmt.Fprintf(&b, "  last step: %s\n", t.Steps[n-1])
		}
		if t.Error != "" {
			fmt.Fprintf(&b, "  %s\n", strings.ReplaceAll(firstLines(t.Error, 8), "\n", "\n  "))
		}
		for _, a := range t.Attachments {
			if a.Source != "" {
				fmt.Fprintf(&b, "  %s: %s\n", a.Name, a.Source)
			}
		}
	}
	return b.String()
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}
