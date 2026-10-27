// Package guard checks a task's diff before orch publishes it (#484): secrets
// that must never reach a PR, and debug leftovers a reviewer should see. It
// reads the diff only; what to do with a finding (block, warn, nothing) is
// config's guard section, applied by the engine.
package guard

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// The rules, as config's guard keys name them.
const (
	RuleSecrets = "secrets"
	RuleDebug   = "debug"
)

// Finding is one thing the guard saw. It never holds the matched text, so a
// secret cannot leak through a log line, an event or a block reason.
type Finding struct {
	Rule string
	File string
	Line int // 0 for a finding about the whole file
	What string
}

func (f Finding) String() string {
	if f.Line == 0 {
		return fmt.Sprintf("%s: %s", f.File, f.What)
	}
	return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.What)
}

// pattern is one rule's regexp, tried only on lines holding one of its hints:
// Go's regexp is linear but slow, and a diff is mostly lines matching nothing.
type pattern struct {
	rule, what string
	hints      []string
	re         *regexp.Regexp
}

// ponytail: known token formats only, no entropy check — entropy flags every
// hash and fixture. Add one when a real leak slips past these.
var patterns = []pattern{
	{RuleSecrets, "a private key", []string{"PRIVATE KEY"}, regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{RuleSecrets, "a GitHub token", []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"}, regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{50,})\b`)},
	{RuleSecrets, "an AWS access key", []string{"AKIA", "ASIA"}, regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{RuleSecrets, "a Slack token", []string{"xox"}, regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`)},
	{RuleSecrets, "an Anthropic API key", []string{"sk-ant-"}, regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	{RuleSecrets, "an OpenAI API key", []string{"sk-"}, regexp.MustCompile(`\bsk-(proj-)?[A-Za-z0-9_-]{32,}`)},
	{RuleSecrets, "a Google API key", []string{"AIza"}, regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{RuleSecrets, "a Stripe live key", []string{"k_live_"}, regexp.MustCompile(`\b[sr]k_live_[0-9A-Za-z]{20,}`)},
	{RuleDebug, "console.log", []string{"console.log("}, regexp.MustCompile(`\bconsole\.log\(`)},
	{RuleDebug, "a debugger statement", []string{"debugger"}, regexp.MustCompile(`^\s*debugger;?\s*$`)},
	{RuleDebug, "a Python breakpoint", []string{"pdb.set_trace()", "breakpoint()"}, regexp.MustCompile(`\b(pdb\.set_trace|breakpoint)\(\)`)},
	{RuleDebug, "binding.pry", []string{"binding.pry"}, regexp.MustCompile(`\bbinding\.pry\b`)},
	{RuleDebug, "dbg!", []string{"dbg!("}, regexp.MustCompile(`\bdbg!\(`)},
}

func (p pattern) matches(line string) bool {
	for _, h := range p.hints {
		if strings.Contains(line, h) {
			return p.re.MatchString(line)
		}
	}
	return false
}

// Scan reads a unified diff (`git diff`) and returns what its added lines and
// changed files hold, in diff order: at most one finding per line.
func Scan(diff string) []Finding {
	var out []Finding
	file, line, prev := "", 0, ""
	for _, l := range strings.Split(diff, "\n") {
		header := strings.HasPrefix(prev, "--- ")
		prev = l
		switch {
		case header && strings.HasPrefix(l, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
			if isEnvFile(file) {
				out = append(out, Finding{Rule: RuleSecrets, File: file, What: "an environment file"})
			}
		case strings.HasPrefix(l, "@@"):
			line = hunkStart(l)
		case strings.HasPrefix(l, "+"):
			for _, p := range patterns {
				if p.matches(l[1:]) {
					out = append(out, Finding{Rule: p.rule, File: file, Line: line, What: p.what})
					break
				}
			}
			line++
		case strings.HasPrefix(l, " "):
			line++
		}
	}
	return out
}

// hunkStart is the new-file line a hunk header (`@@ -a,b +c,d @@`) starts at.
func hunkStart(h string) int {
	_, after, _ := strings.Cut(h, "+")
	f := strings.FieldsFunc(after, func(r rune) bool { return r == ',' || r == ' ' })
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(f[0])
	return n
}

// isEnvFile is a dotenv file, leaving out the templates repositories commit
// on purpose.
func isEnvFile(file string) bool {
	base := path.Base(file)
	if base != ".env" && !strings.HasPrefix(base, ".env.") {
		return false
	}
	switch strings.TrimPrefix(base, ".env.") {
	case "example", "sample", "template", "dist":
		return false
	}
	return true
}
