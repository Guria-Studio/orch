package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
)

// TestRetryPromptCarriesThePreviousFailure (#540): every retry's prompt says
// why the attempt before it failed — its class, its message and the end of
// its log — for each class the retry policy retries, so the agent does not
// start from scratch.
func TestRetryPromptCarriesThePreviousFailure(t *testing.T) {
	tests := []struct {
		class   providers.Failure
		message string
		script  string // the fake agent's work before it answers, if any
	}{
		{providers.FailureTransient, "503 service unavailable", ""},
		{providers.FailureRateLimit, "429 rate limit exceeded", ""},
		{providers.FailureVersionDrift, "model not found: claude-opus-9", ""},
		{providers.FailureParser, "could not parse the stream", ""},
		{providers.FailureOther, "the agent tripped over something odd", ""},
		{providers.FailureTimeout, "orchestrator timeout", "while true; do echo still-working-tick; sleep 0.05; done"},
	}
	for _, tt := range tests {
		t.Run(string(tt.class), func(t *testing.T) {
			tk := task("C-1", 1, "claude/opus")
			resp := failResponse(tt.message)
			if tt.class == providers.FailureTimeout {
				tk.EstimateHours = 0.0004 // ≈ 2.2s at 1.5×
				resp = okResponse()
			}
			f := newReapFixture(t, []model.Task{tk}, map[string]fakeResponse{"C-1": resp}, SchedulerOptions{})
			if tt.script != "" {
				write(t, filepath.Join(f.fakeDir, "C-1.sh"), tt.script)
			}

			f.runOnce(t)
			if len(f.s.RetryQueue()) != 1 {
				t.Fatalf("no retry queued for a %s failure", tt.class)
			}
			// Past any backoff, and the second attempt answers at once.
			later := time.Now().Add(2 * time.Hour)
			f.s.now = func() time.Time { return later }
			write(t, filepath.Join(f.fakeDir, "C-1.sh"), "")
			if _, err := f.s.Refill(context.Background()); err != nil {
				t.Fatalf("Refill: %v", err)
			}
			body, err := os.ReadFile(filepath.Join(f.s.Opts.StateDir, "prompts", f.s.Opts.RunID, "C-1.txt")) // #nosec G304 -- a temp dir this test made
			if err != nil {
				t.Fatal(err)
			}
			prompt := string(body)
			wantLog := tt.message
			if tt.class == providers.FailureTimeout {
				wantLog = "still-working-tick"
			}
			for _, want := range []string{"previous attempt", "class: " + string(tt.class), wantLog} {
				if !strings.Contains(prompt, want) {
					t.Errorf("attempt 2's prompt lacks %q:\n%s", want, prompt)
				}
			}
			if _, err := f.s.DrainWait(context.Background(), 30*time.Second); err != nil {
				t.Fatalf("DrainWait: %v", err)
			}
		})
	}
}

// TestFailureNoteCarriesTheStandingFindings (#540): a retry after a verify
// send-back still sees what the review asked for.
func TestFailureNoteCarriesTheStandingFindings(t *testing.T) {
	s := &Scheduler{verifyFindings: map[string][]VerifyFinding{
		"C-1": {{Severity: "blocking", File: "a.go", Line: 9, Title: "leaks the file handle"}},
	}}
	entry := &InFlight{Task: task("C-1", 1, "claude/opus"), Attempt: 2}
	note := s.failureNote(entry, Outcome{Failure: providers.FailureTransient, Result: providers.Result{ErrorMessage: "503"}})
	if !strings.Contains(note, "[blocking] a.go:9 leaks the file handle") {
		t.Errorf("note lacks the standing finding:\n%s", note)
	}
}
