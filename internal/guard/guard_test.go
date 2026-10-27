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

func TestScanIsFastOnALargeDiff(t *testing.T) {
	lines := make([]string, 50000)
	for i := range lines {
		lines[i] = "x := computeSomething(a, b, c) // a perfectly ordinary line"
	}
	d := diff("big.go", lines...)
	start := time.Now()
	Scan(d)
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("a 50,000-line diff took %v, over the 500 ms budget", took)
	}
}
