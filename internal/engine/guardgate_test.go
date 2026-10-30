package engine

import (
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/model"
)

// Assembled at run time so this file does not trip the forge's secret scanning.
var guardFakeToken = "ghp_" + strings.Repeat("A1b2", 9)

func guardDiff(added string) string {
	return "diff --git a/src/webhook.ts b/src/webhook.ts\n--- a/src/webhook.ts\n+++ b/src/webhook.ts\n@@ -1,0 +1,1 @@\n+" + added + "\n"
}

// TestGuardBlocksASecretBeforeThePush pins #484: a committed token keeps the
// task's work off the remote, blocks the task, and is never printed.
func TestGuardBlocksASecretBeforeThePush(t *testing.T) {
	f, wt, v, _ := prFixture(t)
	f.s.Opts.Cfg.Guard = config.Guard{Secrets: "block", Debug: "warn"}
	wt.diff = guardDiff(`const token = "` + guardFakeToken + `"`)

	f.runOnce(t)

	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusBlocked {
		t.Fatalf("status = %q, want blocked", got)
	}
	if calls := wt.callList(); contains(calls, "push") || v.prHead != "" {
		t.Fatalf("worktree calls = %v, PR head = %q: the work was published", calls, v.prHead)
	}
	notes := strings.Join(f.backend.notes, "\n")
	if !strings.Contains(notes, "src/webhook.ts:1: a GitHub token") {
		t.Errorf("block reason does not name the finding:\n%s", notes)
	}
	if strings.Contains(notes, guardFakeToken) {
		t.Errorf("block reason prints the token:\n%s", notes)
	}
}

// TestGuardWarningsReachThePRBody: a warn rule lets the work through and the
// reviewer sees the finding.
func TestGuardWarningsReachThePRBody(t *testing.T) {
	f, wt, v, _ := prFixture(t)
	f.s.Opts.Cfg.Guard = config.Guard{Secrets: "block", Debug: "warn"}
	wt.diff = guardDiff("console.log(payload)")

	f.runOnce(t)

	if v.prHead != "orch/C-1" {
		t.Fatalf("no PR was opened (head %q)", v.prHead)
	}
	if !strings.Contains(v.prBody, "src/webhook.ts:1: console.log") {
		t.Errorf("PR body does not name the warning:\n%s", v.prBody)
	}
}

// TestGuardOffLetsASecretThrough: guard.secrets off is the operator's call.
func TestGuardOffLetsASecretThrough(t *testing.T) {
	f, wt, v, _ := prFixture(t)
	f.s.Opts.Cfg.Guard = config.Guard{Secrets: "off", Debug: "off"}
	wt.diff = guardDiff(`const token = "` + guardFakeToken + `"`)

	f.runOnce(t)

	if v.prHead != "orch/C-1" {
		t.Fatalf("no PR was opened (head %q)", v.prHead)
	}
}

// TestGuardBlocksAProtectedFile pins #504: an edit to a file
// guard.protected_paths names keeps the work off the remote, the reason names
// the glob and never the file's content; with protected: warn it publishes
// and the PR body says so.
func TestGuardBlocksAProtectedFile(t *testing.T) {
	launch := "diff --git a/.vscode/launch.json b/.vscode/launch.json\n--- a/.vscode/launch.json\n+++ b/.vscode/launch.json\n@@ -1,0 +1,1 @@\n+\"DB_PASSWORD\": \"hunter2\"\n"
	guardCfg := config.Guard{Secrets: "block", Debug: "warn", Protected: "block", ProtectedPaths: []string{".vscode/launch.json"}}

	f, wt, v, _ := prFixture(t)
	f.s.Opts.Cfg.Guard = guardCfg
	wt.diff = launch
	f.runOnce(t)
	if got, _ := f.s.Queue.Status("C-1"); got != model.StatusBlocked || v.prHead != "" {
		t.Fatalf("status = %q, PR head = %q: want blocked and unpublished", got, v.prHead)
	}
	notes := strings.Join(f.backend.notes, "\n")
	if !strings.Contains(notes, ".vscode/launch.json: a protected file (guard.protected_paths: .vscode/launch.json)") || strings.Contains(notes, "hunter2") {
		t.Errorf("block reason:\n%s", notes)
	}

	f, wt, v, _ = prFixture(t)
	guardCfg.Protected = "warn"
	f.s.Opts.Cfg.Guard = guardCfg
	wt.diff = launch
	f.runOnce(t)
	if v.prHead != "orch/C-1" || !strings.Contains(v.prBody, ".vscode/launch.json: a protected file") {
		t.Errorf("PR head %q, body:\n%s", v.prHead, v.prBody)
	}
}
