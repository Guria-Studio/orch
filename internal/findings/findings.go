// Package findings is the path from an agent's report about orch to a public
// issue (#536): redaction before the operator sees it, and sending once they
// approve. orch_report_finding queues; `orch findings` and the dashboard send.
package findings

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Guria-Studio/orch/internal/state"
)

// Repo is where a finding is filed. Fixed: the findings are about orch,
// whichever project the agent happened to be working in.
const Repo = "Guria-Studio/orch"

// Label marks an issue an agent reported. `orch sync issues` refuses to
// ingest it, so a report never loops back into a project as a task.
const Label = "auto-reported"

// Redacted replaces whatever Redact removes.
const Redacted = "[redacted]"

// secretPatterns are the credential shapes an agent's evidence most often
// carries: provider tokens, cloud keys, bearer and JWT values, private key
// blocks, and `key = value` pairs whose key names a secret.
//
// ponytail: a pattern list, not a scanner; the operator's preview is the
// real gate, and a new token shape is one line here.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{30,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`),
}

// reSecretAssignment is `password: x`, `API_KEY=x`, `"token": "x"`: the value
// goes, the key stays so the report still reads.
var reSecretAssignment = regexp.MustCompile(
	`(?i)(["']?[A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential)["']?\s*[:=]\s*)("[^"\n]*"|'[^'\n]*'|[^\s,;]+)`)

// Redact keeps the operator's machine and credentials out of a report: the
// project root becomes `<project>`, the home directory `~`, and secrets
// Redacted. It cannot know what else is private — client names, code — which
// is why the operator previews every report before it is sent.
func Redact(text, projectRoot string) string {
	if projectRoot != "" && projectRoot != "/" {
		text = strings.ReplaceAll(text, projectRoot, "<project>")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		text = strings.ReplaceAll(text, home, "~")
	}
	for _, re := range secretPatterns {
		text = re.ReplaceAllString(text, Redacted)
	}
	return reSecretAssignment.ReplaceAllString(text, "${1}"+Redacted)
}

// Store is the slice of state.Backend sending needs.
type Store interface {
	QueuedFindings(ctx context.Context) ([]state.QueuedFinding, error)
	SetFindingStatus(ctx context.Context, id int64, status, url string) error
	RecordFiledFinding(ctx context.Context, f state.FiledFinding) error
}

// Filer creates the issue; *vcs.GitHubProvider is one.
type Filer interface {
	CreateIssue(repo, title, body string, labels []string) (string, error)
}

// Find returns the queued finding id, in any status.
func Find(ctx context.Context, store Store, id int64) (state.QueuedFinding, error) {
	all, err := store.QueuedFindings(ctx)
	if err != nil {
		return state.QueuedFinding{}, err
	}
	for _, f := range all {
		if f.ID == id {
			return f, nil
		}
	}
	return state.QueuedFinding{}, fmt.Errorf("no finding %d in this project's queue", id)
}

// Send files the queued finding id as stored — the text the operator
// previewed — remembers the issue for de-duplication, and marks it sent.
func Send(ctx context.Context, store Store, filer Filer, id int64) (string, error) {
	f, err := Find(ctx, store, id)
	if err != nil {
		return "", err
	}
	if f.Status != state.FindingQueued {
		return "", fmt.Errorf("finding %d is already %s", id, f.Status)
	}
	url, err := filer.CreateIssue(Repo, f.Title, f.Body, []string{Label})
	if err != nil {
		return "", fmt.Errorf("filing finding %d in %s: %w", id, Repo, err)
	}
	if err := store.SetFindingStatus(ctx, id, state.FindingSent, url); err != nil {
		return url, fmt.Errorf("filed %s, but could not mark finding %d sent: %w", url, id, err)
	}
	// An issue whose number cannot be read from its URL is not remembered;
	// de-duplication then relies on GitHub's search alone.
	if n, err := strconv.Atoi(url[strings.LastIndex(url, "/")+1:]); err == nil {
		_ = store.RecordFiledFinding(ctx, state.FiledFinding{Number: n, URL: url, Title: f.Title,
			Summary: f.Summary, FiledAt: time.Now()})
	}
	return url, nil
}

// Dismiss drops the queued finding id without sending it.
func Dismiss(ctx context.Context, store Store, id int64) error {
	f, err := Find(ctx, store, id)
	if err != nil {
		return err
	}
	if f.Status != state.FindingQueued {
		return fmt.Errorf("finding %d is already %s", id, f.Status)
	}
	return store.SetFindingStatus(ctx, id, state.FindingDismissed, "")
}

// Pending counts the findings waiting for the operator.
func Pending(all []state.QueuedFinding) int {
	n := 0
	for _, f := range all {
		if f.Status == state.FindingQueued {
			n++
		}
	}
	return n
}
