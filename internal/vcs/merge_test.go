package vcs

import (
	"errors"
	"strings"
	"testing"
)

// The stderr gh printed for the failures of #347, as the merge saw them.
const (
	ghBaseModified = "GraphQL: Base branch was modified. Review and try the merge again. (mergePullRequest)"
	ghNotMergeable = "X Pull request org/repo#1 is not mergeable: the merge commit cannot be cleanly created."
	ghAutoNotAllow = "GraphQL: Auto merge is not allowed for this repository (enablePullRequestAutoMerge)"
)

// TestGitHubMergePRDiagnosesTheFailure: a failed `gh pr merge` is followed by
// a read of the PR, and what that read says decides whether trying again can
// help (#347). Before, every one of these was the bare "merge did not
// succeed", and the orch run that hit them gave up on PRs a person then
// merged by hand a minute later.
func TestGitHubMergePRDiagnosesTheFailure(t *testing.T) {
	tests := []struct {
		name          string
		stderr        string
		view          string // gh pr view --json state,mergeStateStatus
		updateExit    string
		wantNil       bool
		wantState     string
		wantRetryable bool
		wantUpdated   bool
		wantUpdate    bool // gh pr update-branch was called
	}{
		{
			name:   "mergeability still being computed after the base moved",
			stderr: ghNotMergeable, view: `{"mergeStateStatus":"UNKNOWN","state":"OPEN"}`,
			wantState: "UNKNOWN", wantRetryable: true,
		},
		{
			name:   "the base changed under the merge",
			stderr: ghBaseModified, view: `{"mergeStateStatus":"CLEAN","state":"OPEN"}`,
			wantState: "CLEAN", wantRetryable: true,
		},
		{
			name:   "--auto refused on a repo without auto-merge while not yet mergeable",
			stderr: ghAutoNotAllow, view: `{"mergeStateStatus":"BLOCKED","state":"OPEN"}`,
			wantState: "BLOCKED", wantRetryable: true,
		},
		{
			name:   "behind a base that requires up-to-date branches: updated",
			stderr: ghNotMergeable, view: `{"mergeStateStatus":"BEHIND","state":"OPEN"}`,
			wantState: "BEHIND", wantRetryable: true, wantUpdated: true, wantUpdate: true,
		},
		{
			name:   "behind, and the update itself failed",
			stderr: ghNotMergeable, view: `{"mergeStateStatus":"BEHIND","state":"OPEN"}`,
			updateExit: "1",
			wantState:  "BEHIND", wantRetryable: true, wantUpdate: true,
		},
		{
			name:   "conflicts need a person",
			stderr: ghNotMergeable, view: `{"mergeStateStatus":"DIRTY","state":"OPEN"}`,
			wantState: "DIRTY",
		},
		{
			name:   "a draft needs a person",
			stderr: ghNotMergeable, view: `{"mergeStateStatus":"DRAFT","state":"OPEN"}`,
			wantState: "DRAFT",
		},
		{
			name:   "closed without merging",
			stderr: ghNotMergeable, view: `{"mergeStateStatus":"UNKNOWN","state":"CLOSED"}`,
			wantState: "UNKNOWN",
		},
		{
			name:   "merged by someone else between the two calls",
			stderr: ghNotMergeable, view: `{"mergeStateStatus":"UNKNOWN","state":"MERGED"}`,
			wantNil: true,
		},
		{
			name:   "the PR could not be read",
			stderr: ghBaseModified, view: ``,
			wantRetryable: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFakeBin(t)
			log := ghLog(t)
			t.Setenv("FAKE_GH_MERGE_EXIT", "1")
			t.Setenv("FAKE_GH_MERGE_STDERR", tt.stderr)
			t.Setenv("FAKE_GH_VIEW_STDOUT", tt.view)
			t.Setenv("FAKE_GH_UPDATE_EXIT", tt.updateExit)
			t.Setenv("FAKE_GH_UPDATE_STDERR", "update refused")

			err := NewGitHubProvider().MergePR("https://github.com/org/repo/pull/1", true, true)

			calls := readLog(t, log)
			if got := strings.Contains(calls, "gh pr update-branch https://github.com/org/repo/pull/1"); got != tt.wantUpdate {
				t.Errorf("update-branch called = %v, want %v; calls:\n%s", got, tt.wantUpdate, calls)
			}
			if tt.wantNil {
				if err != nil {
					t.Fatalf("err = %v, want nil: the PR is merged", err)
				}
				return
			}
			if !errors.Is(err, ErrMergeFailed) {
				t.Fatalf("err = %v, want ErrMergeFailed", err)
			}
			// The real stderr reaches the caller, not only the generic line.
			if !strings.Contains(err.Error(), tt.stderr) {
				t.Errorf("err = %q, does not carry gh's stderr %q", err, tt.stderr)
			}
			var me *MergeError
			if !errors.As(err, &me) {
				t.Fatalf("err = %v (%T), want *MergeError", err, err)
			}
			got := MergeError{MergeState: me.MergeState, Retryable: me.Retryable, BranchUpdated: me.BranchUpdated}
			want := MergeError{MergeState: tt.wantState, Retryable: tt.wantRetryable, BranchUpdated: tt.wantUpdated}
			if got != want {
				t.Errorf("diagnosis = %+v, want %+v", got, want)
			}
			if tt.updateExit == "1" && !strings.Contains(err.Error(), "update refused") {
				t.Errorf("err = %q, does not say why the update failed", err)
			}
		})
	}
}

// TestGitHubMergePRDoesNotDiagnoseASuccess: a merge that worked costs one
// call, not a follow-up read (rule 25: asserted by what it did not do).
func TestGitHubMergePRDoesNotDiagnoseASuccess(t *testing.T) {
	withFakeBin(t)
	log := ghLog(t)

	if err := NewGitHubProvider().MergePR("https://github.com/org/repo/pull/1", true, false); err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSpace(readLog(t, log)), "\n")
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "gh pr merge ") {
		t.Errorf("calls = %q, want only the merge", calls)
	}
}

func TestGitLabMergePRCarriesGlabsStderr(t *testing.T) {
	withFakeBin(t)
	t.Setenv("FAKE_GLAB_MERGE_EXIT", "1")
	t.Setenv("FAKE_GLAB_MERGE_STDERR", "ERROR: 405 Method Not Allowed\n")

	err := NewGitLabProvider("gitlab.com").MergePR("https://gitlab.com/org/repo/-/merge_requests/7", true, false)
	if !errors.Is(err, ErrMergeFailed) {
		t.Fatalf("err = %v, want ErrMergeFailed", err)
	}
	if !strings.Contains(err.Error(), "ERROR: 405 Method Not Allowed") {
		t.Errorf("err = %q, does not carry glab's stderr", err)
	}
	var me *MergeError
	if !errors.As(err, &me) || !me.Retryable {
		t.Errorf("err = %#v, want a retryable *MergeError", err)
	}
}

func TestMergeErrorMessage(t *testing.T) {
	tests := []struct {
		err  MergeError
		want string
	}{
		{MergeError{}, "vcs: merge did not succeed"},
		{MergeError{Detail: "boom"}, "vcs: merge did not succeed: boom"},
		{MergeError{Detail: "boom", MergeState: "UNKNOWN"}, "vcs: merge did not succeed (merge state UNKNOWN): boom"},
		{
			MergeError{Detail: "boom", MergeState: "BEHIND", BranchUpdated: true},
			"vcs: merge did not succeed (merge state BEHIND); branch updated from its base, CI runs again: boom",
		},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("Error() = %q, want %q", got, tt.want)
		}
	}
}

func TestMergeDetailTrimsAndBounds(t *testing.T) {
	long := strings.Repeat("x", maxMergeDetail+50)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"stderr, whitespace folded", &runError{stderr: "  line one\n  line two\n"}, "line one line two"},
		{"no stderr: the run error", &runError{binary: "gh", args: []string{"pr"}, err: errors.New("exec failed")}, "vcs: run gh [pr]: exec failed"},
		{"bounded", &runError{stderr: long}, long[:maxMergeDetail]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeDetail(tt.err); got != tt.want {
				t.Errorf("mergeDetail = %q, want %q", got, tt.want)
			}
		})
	}
}
