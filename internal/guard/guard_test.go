package guard

import (
	"strings"
	"testing"
	"time"
)

// The fake secrets are assembled at run time so this file does not trip the
// forge's own secret scanning.
var fakeGitHubToken = "ghp_" + strings.Repeat("A1b2", 9)

func diff(file string, added ...string) string {
	var b strings.Builder
	b.WriteString("diff --git a/" + file + " b/" + file + "\n--- a/" + file + "\n+++ b/" + file + "\n@@ -1,1 +10,3 @@\n context\n")
	for _, l := range added {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

func TestScanFindsAGitHubTokenWithoutPrintingIt(t *testing.T) {
	got := Scan(diff("app/config.go", `client := github.New("`+fakeGitHubToken+`")`))
	if len(got) != 1 {
		t.Fatalf("got %v, want one finding", got)
	}
	f := got[0]
	if f.Rule != RuleSecrets || f.File != "app/config.go" || f.Line != 11 {
		t.Fatalf("got %+v, want a secrets finding at app/config.go:11", f)
	}
	if strings.Contains(f.String(), fakeGitHubToken) || strings.Contains(f.String(), fakeGitHubToken[4:12]) {
		t.Fatalf("the finding prints the token: %s", f)
	}
}

func TestScanFindsAPrivateKeyAndAnEnvFile(t *testing.T) {
	d := diff("deploy/id_rsa", "-----BEGIN OPENSSH PRIVATE KEY-----") +
		diff(".env", "DB_PASSWORD=hunter2") +
		diff(".env.example", "DB_PASSWORD=")
	got := Scan(d)
	if len(got) != 2 || got[0].File != "deploy/id_rsa" || got[1].File != ".env" {
		t.Fatalf("got %v, want the key and .env, not .env.example", got)
	}
}

func TestScanWarnsOnDebugLeftovers(t *testing.T) {
	got := Scan(diff("web/app.ts", "console.log(user)", "  debugger;") + diff("api/x.py", "breakpoint()"))
	if len(got) != 3 {
		t.Fatalf("got %v, want three debug findings", got)
	}
	for _, f := range got {
		if f.Rule != RuleDebug {
			t.Fatalf("got %+v, want a debug finding", f)
		}
	}
}

func TestScanIgnoresRemovedAndContextLines(t *testing.T) {
	d := "diff --git a/a.ts b/a.ts\n--- a/a.ts\n+++ b/a.ts\n@@ -1,2 +1,1 @@\n-console.log(x)\n console.log(y)\n"
	if got := Scan(d); len(got) != 0 {
		t.Fatalf("got %v, want nothing: only added lines count", got)
	}
}

// TestScanIsFastOnALargeDiff holds #484's 500 ms budget on a diff ten times a
// large task's, timed as the best of three runs: under `go test ./... -race`
// on a loaded machine one run can be descheduled past the budget (#491 saw
// 575 ms), which says nothing about the scan.
func TestScanIsFastOnALargeDiff(t *testing.T) {
	lines := make([]string, 20000)
	for i := range lines {
		lines[i] = "x := computeSomething(a, b, c) // a perfectly ordinary line"
	}
	d := diff("big.go", lines...)
	best := time.Hour
	for range 3 {
		start := time.Now()
		Scan(d)
		best = min(best, time.Since(start))
	}
	if best > 500*time.Millisecond {
		t.Fatalf("a 20,000-line diff took %v at best, over the 500 ms budget", best)
	}
}

// TestScanBlocksAProtectedFileWholly pins #504: a file guard.protected_paths
// names is one finding for the whole file, added, changed or deleted, and
// none of its lines reach the finding.
func TestScanBlocksAProtectedFileWholly(t *testing.T) {
	protected := []string{".vscode/launch.json", "infrastructure/customers/**"}
	deleted := "diff --git a/infrastructure/customers/acme/db.yml b/infrastructure/customers/acme/db.yml\n" +
		"--- a/infrastructure/customers/acme/db.yml\n+++ /dev/null\n@@ -1,1 +0,0 @@\n-password: s3cret\n"
	got := Scan(diff(".vscode/launch.json", `"DB_PASSWORD": "hunter2"`)+deleted+diff("src/app.ts", "x()"), protected...)
	if len(got) != 2 {
		t.Fatalf("got %v, want one finding per protected file", got)
	}
	for i, want := range []string{
		".vscode/launch.json: a protected file (guard.protected_paths: .vscode/launch.json)",
		"infrastructure/customers/acme/db.yml: a protected file (guard.protected_paths: infrastructure/customers/**)",
	} {
		if got[i].Rule != RuleProtected || got[i].String() != want {
			t.Errorf("finding %d = %+v, want %q", i, got[i], want)
		}
		if strings.Contains(got[i].String(), "hunter2") || strings.Contains(got[i].String(), "s3cret") {
			t.Errorf("finding prints the file's content: %s", got[i])
		}
	}
	if got := Scan(diff(".vscode/launch.json", `"DB_PASSWORD": "hunter2"`)); len(got) != 0 {
		t.Errorf("without protected_paths got %v, want nothing", got)
	}
}

func TestMatchPath(t *testing.T) {
	tests := []struct {
		pattern, file string
		want          bool
	}{
		{"a/b.go", "a/b.go", true},
		{"./a/b.go", "a/b.go", true},
		{"a/", "a/b/c.go", true},
		{"a/", "ab/c.go", false},
		{"a/*.go", "a/b.go", true},
		{"a/*.go", "a/b/c.go", false},
		{"a/**", "a/b/c.go", true},
		{"a/**/c.go", "a/c.go", true},
		{"a/**/c.go", "a/b/d/c.go", true},
		{"**/launch.json", ".vscode/launch.json", true},
		{"a/**", "b/a/c.go", false},
	}
	for _, tt := range tests {
		if got := MatchPath(tt.pattern, tt.file); got != tt.want {
			t.Errorf("MatchPath(%q, %q) = %v, want %v", tt.pattern, tt.file, got, tt.want)
		}
	}
}
