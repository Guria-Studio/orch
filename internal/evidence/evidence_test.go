package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/contract"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// report is a verify report.json (verify.E2EReport) with a tagged test that
// left a video and a screenshot, an untagged one with a trace, and an
// attachment that was not copied.
const report = `{
  "task_id": "F2.T3",
  "stats": {"passed": 2, "failed": 0, "flaky": 0, "skipped": 0},
  "tests": [
    {"title": "login › entra", "status": "passed", "tags": ["evidence"],
     "steps": ["Abre la página", "Entra con su cuenta"],
     "attachments": [
       {"name": "video", "content_type": "video/webm", "path": "t001/video.webm", "source": "test-results/a/video.webm"},
       {"name": "screenshot", "content_type": "image/png", "path": "t001/shot.png", "source": "test-results/a/shot.png"}
     ]},
    {"title": "cart › empty", "status": "passed", "tags": [],
     "attachments": [
       {"name": "trace", "content_type": "application/zip", "path": "t002/trace.zip", "source": "test-results/b/trace.zip"},
       {"name": "video", "content_type": "video/webm", "source": "test-results/b/huge.webm"}
     ]},
    {"title": "orders › list", "status": "passed", "tags": ["@evidence", "@slow"]}
  ]
}`

func fixture(t *testing.T) (stateDir, artifacts string) {
	t.Helper()
	stateDir = t.TempDir()
	artifacts = filepath.Join(stateDir, "e2e", "run-1", "F2.T3", "attempt-2")
	writeFile(t, filepath.Join(artifacts, "report.json"), report)
	writeFile(t, filepath.Join(artifacts, "t001", "video.webm"), "0123456789")
	writeFile(t, filepath.Join(artifacts, "t001", "shot.png"), "png")
	writeFile(t, filepath.Join(artifacts, "t002", "trace.zip"), "zip!")
	return stateDir, artifacts
}

func verified() contract.VerifyResult {
	return contract.VerifyResult{
		Commands: []contract.VerifyCommand{
			{Command: "go test ./...", DurationS: 41.2, Kind: "tests"},
			{Command: "npx playwright test", DurationS: 12, Kind: "e2e"},
		},
		Reviewer: "gemini", ReviewModel: "gemini-3-pro",
		Findings: []contract.VerifyFinding{
			{Severity: "minor", File: "app.go", Line: 3, Title: "naming", Detail: "for the agent only"},
			{Severity: "minor", Title: "docs"},
		},
		E2E:     &contract.E2ESummary{Passed: 2, Report: "ignored"},
		HeadSHA: "abc123", BaseSHA: "def456",
		Diff: &contract.DiffStat{Files: 7, Insertions: 120, Deletions: 14, Top: []string{"app.go"}},
	}
}

// TestBuildIsTheAttemptsEvidence: the manifest carries the checks with their
// kinds, the review without the findings' detail, the change, and the E2E run
// with every copied file and the walk-throughs of the tests tagged @evidence
// only; its paths never name the operator's machine.
func TestBuildIsTheAttemptsEvidence(t *testing.T) {
	stateDir, artifacts := fixture(t)
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	m := Build(stateDir, Attempt{TaskID: "F2.T3", Title: "Login", RunID: "run-1", Attempt: 2, At: at, ArtifactsDir: artifacts}, verified())

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "version": 1,
  "task_id": "F2.T3",
  "title": "Login",
  "run_id": "run-1",
  "attempt": 2,
  "head_sha": "abc123",
  "base_sha": "def456",
  "verified_at": "2026-11-20T12:00:00Z",
  "verdict": "passed",
  "checks": [
    {
      "command": "go test ./...",
      "duration_s": 41.2,
      "kind": "tests"
    },
    {
      "command": "npx playwright test",
      "duration_s": 12,
      "kind": "e2e"
    }
  ],
  "review": {
    "reviewer": "gemini",
    "model": "gemini-3-pro",
    "blocking": 0,
    "findings": [
      {
        "severity": "minor",
        "file": "app.go",
        "line": 3,
        "title": "naming"
      },
      {
        "severity": "minor",
        "title": "docs"
      }
    ]
  },
  "diff": {
    "files": 7,
    "insertions": 120,
    "deletions": 14,
    "top": [
      "app.go"
    ]
  },
  "e2e": {
    "passed": 2,
    "failed": 0,
    "flaky": 0,
    "skipped": 0,
    "dir": "e2e/run-1/F2.T3/attempt-2",
    "media": [
      {
        "test": "login › entra",
        "kind": "video",
        "path": "t001/video.webm",
        "bytes": 10
      },
      {
        "test": "login › entra",
        "kind": "screenshot",
        "path": "t001/shot.png",
        "bytes": 3
      },
      {
        "test": "cart › empty",
        "kind": "trace",
        "path": "t002/trace.zip",
        "bytes": 4
      }
    ],
    "walkthroughs": [
      {
        "title": "login › entra",
        "status": "passed",
        "steps": [
          "Abre la página",
          "Entra con su cuenta"
        ],
        "video": "t001/video.webm",
        "screenshot": "t001/shot.png"
      },
      {
        "title": "orders › list",
        "status": "passed"
      }
    ]
  }
}`
	if string(b) != want {
		t.Errorf("manifest =\n%s\nwant\n%s", b, want)
	}
	if strings.Contains(string(b), stateDir) {
		t.Error("the manifest names the state directory's absolute path")
	}
}

func TestBuildVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  contract.VerifyResult
		want string
	}{
		{"pass", contract.VerifyResult{Reviewer: "codex"}, VerdictPassed},
		{"failure", contract.VerifyResult{Failure: "make test failed"}, VerdictFailed},
		{"no review", contract.VerifyResult{ReviewUnavailable: "no other provider"}, VerdictReviewUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Build(t.TempDir(), Attempt{TaskID: "T"}, tc.res)
			if m.Verdict != tc.want {
				t.Errorf("verdict = %q, want %q", m.Verdict, tc.want)
			}
			if m.Checks == nil {
				t.Error("checks is null, want [] (a reader iterates it)")
			}
		})
	}
	m := Build(t.TempDir(), Attempt{TaskID: "T"}, contract.VerifyResult{ReviewUnavailable: "no other provider"})
	if m.Review == nil || m.Review.Unavailable != "no other provider" {
		t.Errorf("review = %+v, want why it was unavailable", m.Review)
	}
}

// TestPromoteReplacesAndHistoryKeeps is phase 1's acceptance (#520): every
// verified attempt is in history/; evidence.json is the last one pushed, and
// a later run replaces it.
func TestPromoteReplacesAndHistoryKeeps(t *testing.T) {
	stateDir := t.TempDir()
	if m, err := Read(stateDir, "F2.T3"); m != nil || err != nil {
		t.Fatalf("Read before any verify = %v, %v; want nothing", m, err)
	}
	first := Manifest{Version: Version, TaskID: "F2.T3", RunID: "run-1", Attempt: 1, Verdict: VerdictPassed}
	second := Manifest{Version: Version, TaskID: "F2.T3", RunID: "run-2", Attempt: 1, Verdict: VerdictPassed}
	for _, m := range []Manifest{first, second} {
		rel, err := Record(stateDir, m)
		if err != nil {
			t.Fatal(err)
		}
		if want := "evidence/F2.T3/history/" + m.RunID + "-attempt-1.json"; rel != want {
			t.Errorf("Record = %q, want %q", rel, want)
		}
		if err := Promote(stateDir, m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Read(stateDir, "F2.T3")
	if err != nil || got == nil || got.RunID != "run-2" {
		t.Fatalf("Read = %+v, %v; want run-2's", got, err)
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "evidence", "F2.T3", "history"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"run-1-attempt-1.json", "run-2-attempt-1.json"}; !slices.Equal(names, want) {
		t.Errorf("history = %v, want %v", names, want)
	}
}

func TestTaskIDsThatLeaveTheDirectoryAreRefused(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../x", `a\b`, "a/b"} {
		if _, err := Record(t.TempDir(), Manifest{TaskID: id}); err == nil {
			t.Errorf("Record(%q) = nil error, want a refusal", id)
		}
	}
}

func TestReadRefusesAnotherVersion(t *testing.T) {
	stateDir := t.TempDir()
	writeFile(t, filepath.Join(stateDir, "evidence", "T", ManifestFile), `{"version": 9, "task_id": "T"}`)
	if _, err := Read(stateDir, "T"); err == nil || !strings.Contains(err.Error(), "version 9") {
		t.Errorf("Read = %v, want a version error", err)
	}
}
