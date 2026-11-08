package findings

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/state"
)

// TestRedactRemovesSecretsAndPaths (#536): the old redaction replaced the
// project root and $HOME only. A report is redacted of the token shapes an
// agent's evidence most often carries before the operator previews it, so
// the preview is exactly what leaves.
func TestRedactRemovesSecretsAndPaths(t *testing.T) {
	in := strings.Join([]string{
		"root /work/client-x/app/main.go",
		"gh ghp_" + strings.Repeat("a", 36),
		"pat github_pat_" + strings.Repeat("B", 40),
		"openai sk-proj-" + strings.Repeat("c", 30),
		"anthropic sk-ant-api03-" + strings.Repeat("d", 30),
		"aws AKIAIOSFODNN7EXAMPLE",
		"slack xoxb-123456789012-abcdefghij",
		"auth: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl",
		"API_KEY=hunter2hunter2",
		`"password": "s3cr3t-value"`,
		"-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----",
	}, "\n")
	got := Redact(in, "/work/client-x")
	for _, leak := range []string{"/work/client-x", "ghp_", "github_pat_", "sk-proj-", "sk-ant-", "AKIAIOSFODNN7EXAMPLE",
		"xoxb-", "eyJhbGci", "hunter2", "s3cr3t", "AAAA"} {
		if strings.Contains(got, leak) {
			t.Errorf("redacted text still holds %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "<project>/app/main.go") || !strings.Contains(got, Redacted) {
		t.Errorf("redacted text = %q", got)
	}
	if Redact("orch run --max-tasks 2 failed with exit 1", "") != "orch run --max-tasks 2 failed with exit 1" {
		t.Error("ordinary text was changed")
	}
}

type fakeStore struct {
	queue  []state.QueuedFinding
	filed  []state.FiledFinding
	status map[int64]string
}

func (f *fakeStore) QueuedFindings(context.Context) ([]state.QueuedFinding, error) { return f.queue, nil }
func (f *fakeStore) SetFindingStatus(_ context.Context, id int64, status, url string) error {
	f.status[id] = status + " " + url
	return nil
}
func (f *fakeStore) RecordFiledFinding(_ context.Context, ff state.FiledFinding) error {
	f.filed = append(f.filed, ff)
	return nil
}

type fakeGitHub struct{ created []string }

func (g *fakeGitHub) CreateIssue(repo, title, body string, labels []string) (string, error) {
	g.created = append(g.created, fmt.Sprintf("%s|%s|%s|%v", repo, title, body, labels))
	return "https://github.com/Guria-Studio/orch/issues/1234", nil
}

// TestSendFilesExactlyThePreview (#536): sending files the queued title and
// body as stored — what the operator previewed — remembers the issue for
// de-duplication, and marks the row sent. A row already sent is not filed
// twice.
func TestSendFilesExactlyThePreview(t *testing.T) {
	store := &fakeStore{status: map[int64]string{}, queue: []state.QueuedFinding{
		{ID: 1, Title: "t", Body: "b", Summary: "s", Status: state.FindingQueued, QueuedAt: time.Now()},
		{ID: 2, Title: "old", Body: "x", Status: state.FindingSent},
	}}
	gh := &fakeGitHub{}
	url, err := Send(context.Background(), store, gh, 1)
	if err != nil || url != "https://github.com/Guria-Studio/orch/issues/1234" {
		t.Fatalf("Send = %q, %v", url, err)
	}
	if len(gh.created) != 1 || gh.created[0] != Repo+"|t|b|["+Label+"]" {
		t.Errorf("created = %v", gh.created)
	}
	if len(store.filed) != 1 || store.filed[0].Number != 1234 || store.filed[0].Summary != "s" {
		t.Errorf("filed = %+v", store.filed)
	}
	if store.status[1] != state.FindingSent+" "+url {
		t.Errorf("status = %q", store.status[1])
	}
	if _, err := Send(context.Background(), store, gh, 2); err == nil || len(gh.created) != 1 {
		t.Errorf("a sent row was filed again (err %v)", err)
	}
}
