// Package evidence is a task's evidence bundle (#520): what orch's local
// verify proved about the work it pushed — the checks that ran, the app's
// probes, the review, the change in numbers and the E2E run with its media —
// kept at a path that does not change from run to run:
//
//	<state>/evidence/<task>/evidence.json                 the attempt that was pushed
//	<state>/evidence/<task>/history/<run>-attempt-<n>.json every verified attempt
//
// The engine writes it (it knows the run and the attempt; internal/verify
// does not); `orch status --json` and the dashboard read it. The E2E files
// themselves stay where the verify kept them, under <state>/e2e/; every path
// in a manifest is relative to the state directory, or to the E2E directory
// for media, so the manifest never names the operator's machine.
package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Guria-Studio/orch/internal/contract"
)

// Version is the manifest's format; a reader refuses one it does not know.
const Version = 1

// ManifestFile is the pushed attempt's manifest, inside a task's directory.
const ManifestFile = "evidence.json"

// historyDir holds one manifest per verified attempt, passed or failed.
const historyDir = "history"

// Dir is where the evidence directory for every task lives under the state
// directory.
const Dir = "evidence"

// The verdicts a manifest can have.
const (
	VerdictPassed = "passed"
	VerdictFailed = "failed"
	// VerdictReviewUnavailable is work that passed its checks and was
	// published without the review that was asked for (#539).
	VerdictReviewUnavailable = "review_unavailable"
)

// EvidenceTag marks the E2E tests whose walk-through is the task's evidence:
// `test('…', { tag: '@evidence' }, …)`. Playwright's JSON report lists tags
// without the @; both spellings are read.
const EvidenceTag = "@evidence"

// Manifest is one verified attempt's evidence.
type Manifest struct {
	Version int    `json:"version"`
	TaskID  string `json:"task_id"`
	Title   string `json:"title"`
	RunID   string `json:"run_id"`
	Attempt int    `json:"attempt"`
	// HeadSHA is the commit verified; BaseSHA where its branch left the
	// base.
	HeadSHA    string    `json:"head_sha,omitempty"`
	BaseSHA    string    `json:"base_sha,omitempty"`
	VerifiedAt time.Time `json:"verified_at"`
	Verdict    string    `json:"verdict"`
	Checks     []Check   `json:"checks"`
	Review     *Review   `json:"review,omitempty"`
	Diff       *Diff     `json:"diff,omitempty"`
	E2E        *E2E      `json:"e2e,omitempty"`
}

// Check is one command the verify ran, with its kind (tests, lint, e2e, …).
type Check struct {
	Command   string  `json:"command"`
	DurationS float64 `json:"duration_s"`
	Kind      string  `json:"kind,omitempty"`
}

// Review is who read the diff and what they found. Unavailable says why a
// review that was asked for did not happen.
type Review struct {
	Reviewer    string    `json:"reviewer,omitempty"`
	Model       string    `json:"model,omitempty"`
	Blocking    int       `json:"blocking"`
	Findings    []Finding `json:"findings"`
	Unavailable string    `json:"unavailable,omitempty"`
}

// Finding is a review finding without its detail: the detail is for the
// agent, and stays in the verify event.
type Finding struct {
	Severity string `json:"severity"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Title    string `json:"title"`
}

// Diff is the change in numbers.
type Diff struct {
	Files      int      `json:"files"`
	Insertions int      `json:"insertions"`
	Deletions  int      `json:"deletions"`
	Top        []string `json:"top,omitempty"`
}

// E2E is the attempt's E2E run: counts, the directory its report and files
// are in (relative to the state directory), every file a test left, and the
// walk-throughs of the tests tagged @evidence.
type E2E struct {
	Passed  int     `json:"passed"`
	Failed  int     `json:"failed"`
	Flaky   int     `json:"flaky"`
	Skipped int     `json:"skipped"`
	Dir     string  `json:"dir,omitempty"`
	Media   []Media `json:"media,omitempty"`
	// Walkthroughs are the tests tagged @evidence, in the report's order:
	// what a client is shown as "this is how it was tested".
	Walkthroughs []Walkthrough `json:"walkthroughs,omitempty"`
}

// Media is one file a test left, relative to E2E.Dir.
type Media struct {
	Test  string `json:"test"`
	Kind  string `json:"kind"` // video, screenshot, trace, other
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// Walkthrough is one @evidence test: its steps (test.step titles, written
// in the client's language) and its video and screenshot, relative to
// E2E.Dir, when it left them.
type Walkthrough struct {
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	Steps      []string `json:"steps,omitempty"`
	Video      string   `json:"video,omitempty"`
	Screenshot string   `json:"screenshot,omitempty"`
}

// Attempt is what the engine knows about a verified attempt that the
// verify's result does not say.
type Attempt struct {
	TaskID, Title, RunID string
	Attempt              int
	At                   time.Time
	// ArtifactsDir is the attempt's E2E directory, absolute.
	ArtifactsDir string
}

// Build is the manifest of a verified attempt, from the verify's result and,
// when the verify collected an E2E run, the report it left in
// a.ArtifactsDir. A report that cannot be read leaves the counts without
// media.
func Build(stateDir string, a Attempt, res contract.VerifyResult) Manifest {
	m := Manifest{
		Version: Version, TaskID: a.TaskID, Title: a.Title, RunID: a.RunID, Attempt: a.Attempt,
		HeadSHA: res.HeadSHA, BaseSHA: res.BaseSHA, VerifiedAt: a.At.UTC(),
		Verdict: verdict(res), Checks: []Check{},
	}
	for _, c := range res.Commands {
		m.Checks = append(m.Checks, Check(c))
	}
	if res.Reviewer != "" || res.ReviewUnavailable != "" {
		r := &Review{Reviewer: res.Reviewer, Model: res.ReviewModel, Findings: []Finding{}, Unavailable: res.ReviewUnavailable}
		for _, f := range res.Findings {
			if f.Severity == "blocking" {
				r.Blocking++
			}
			r.Findings = append(r.Findings, Finding{Severity: f.Severity, File: f.File, Line: f.Line, Title: f.Title})
		}
		m.Review = r
	}
	if d := res.Diff; d != nil {
		m.Diff = &Diff{Files: d.Files, Insertions: d.Insertions, Deletions: d.Deletions, Top: d.Top}
	}
	if e := res.E2E; e != nil {
		m.E2E = &E2E{Passed: e.Passed, Failed: e.Failed, Flaky: e.Flaky, Skipped: e.Skipped}
		if a.ArtifactsDir != "" {
			if rel, err := filepath.Rel(stateDir, a.ArtifactsDir); err == nil && !strings.HasPrefix(rel, "..") {
				m.E2E.Dir = filepath.ToSlash(rel)
			}
			m.E2E.Media, m.E2E.Walkthroughs = readReport(a.ArtifactsDir)
		}
	}
	return m
}

func verdict(res contract.VerifyResult) string {
	switch {
	case res.Failure != "":
		return VerdictFailed
	case res.ReviewUnavailable != "":
		return VerdictReviewUnavailable
	}
	return VerdictPassed
}

// e2eReport is the part of verify's report.json (verify.E2EReport) the
// manifest reads. Its own copy: evidence is read by the CLI and the
// dashboard, which have no business importing the verify.
type e2eReport struct {
	Tests []struct {
		Title       string   `json:"title"`
		Status      string   `json:"status"`
		Tags        []string `json:"tags"`
		Steps       []string `json:"steps"`
		Attachments []struct {
			Name        string `json:"name"`
			ContentType string `json:"content_type"`
			Path        string `json:"path"`
		} `json:"attachments"`
	} `json:"tests"`
}

// reportFile is verify.ReportFile.
const reportFile = "report.json"

func readReport(dir string) ([]Media, []Walkthrough) {
	b, err := os.ReadFile(filepath.Join(dir, reportFile)) // #nosec G304 -- the attempt's report, under orch's state dir
	if err != nil {
		return nil, nil
	}
	var rep e2eReport
	if json.Unmarshal(b, &rep) != nil {
		return nil, nil
	}
	var media []Media
	var walks []Walkthrough
	for _, t := range rep.Tests {
		w := Walkthrough{Title: t.Title, Status: t.Status, Steps: t.Steps}
		for _, a := range t.Attachments {
			if a.Path == "" {
				continue // not copied: missing, or over the size limit
			}
			info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(a.Path)))
			if err != nil {
				continue
			}
			kind := mediaKind(a.Name, a.ContentType)
			media = append(media, Media{Test: t.Title, Kind: kind, Path: a.Path, Bytes: info.Size()})
			switch {
			case kind == "video" && w.Video == "":
				w.Video = a.Path
			case kind == "screenshot" && w.Screenshot == "":
				w.Screenshot = a.Path
			}
		}
		if Tagged(t.Tags) {
			walks = append(walks, w)
		}
	}
	return media, walks
}

// Tagged reports whether tags, as Playwright's report lists them, include
// the evidence tag.
func Tagged(tags []string) bool {
	return slices.ContainsFunc(tags, func(t string) bool { return "@"+strings.TrimPrefix(t, "@") == EvidenceTag })
}

func mediaKind(name, contentType string) string {
	switch {
	case strings.HasPrefix(contentType, "video/"):
		return "video"
	case strings.HasPrefix(contentType, "image/"):
		return "screenshot"
	case name == "trace":
		return "trace"
	}
	return "other"
}

// TaskDir is a task's evidence directory, refusing an id that would leave
// the evidence directory.
func TaskDir(stateDir, taskID string) (string, error) {
	if taskID == "" || taskID == "." || taskID == ".." || strings.ContainsAny(taskID, `/\`) {
		return "", fmt.Errorf("evidence: task id %q cannot name a directory", taskID)
	}
	return filepath.Join(stateDir, Dir, taskID), nil
}

// HistoryName is an attempt's file name in history/.
func HistoryName(runID string, attempt int) string {
	return fmt.Sprintf("%s-attempt-%d.json", runID, attempt)
}

// Record writes m into the task's history and returns its path relative to
// the state directory, for the verify event to point at.
func Record(stateDir string, m Manifest) (string, error) {
	dir, err := TaskDir(stateDir, m.TaskID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, historyDir, HistoryName(m.RunID, m.Attempt))
	if err := writeJSON(path, m); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(Dir, m.TaskID, historyDir, HistoryName(m.RunID, m.Attempt))), nil
}

// Promote makes m the task's evidence: the attempt that was pushed. It
// replaces the one before, which stays in history/.
func Promote(stateDir string, m Manifest) error {
	dir, err := TaskDir(stateDir, m.TaskID)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, ManifestFile), m)
}

// Read is the task's evidence, nil without an error when it has none.
func Read(stateDir, taskID string) (*Manifest, error) {
	dir, err := TaskDir(stateDir, taskID)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, ManifestFile)) // #nosec G304 -- under orch's state dir, the id checked above
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("evidence: reading %s's manifest: %w", taskID, err)
	}
	if m.Version != Version {
		return nil, fmt.Errorf("evidence: %s's manifest is version %d; this orch reads %d", taskID, m.Version, Version)
	}
	return &m, nil
}

// writeJSON writes v to path through a temporary file and a rename, so a
// reader never sees half a manifest.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("evidence: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".evidence-*")
	if err != nil {
		return fmt.Errorf("evidence: %w", err)
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("evidence: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("evidence: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("evidence: %w", err)
	}
	return nil
}
