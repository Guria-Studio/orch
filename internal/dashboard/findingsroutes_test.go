package dashboard

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/state"
)

type countingFiler struct{ n int }

func (c *countingFiler) CreateIssue(_, _, _ string, _ []string) (string, error) {
	c.n++
	return "https://github.com/Guria-Studio/orch/issues/501", nil
}

// TestFindingsPageSendsOnlyWhatTheOperatorApproves (#536): the page lists
// the queue with the exact body, Send files one, Dismiss drops one, and a
// cross-origin request sends nothing.
func TestFindingsPageSendsOnlyWhatTheOperatorApproves(t *testing.T) {
	ctx := context.Background()
	db, _, err := state.Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	root := writeProject(t, "spec_root: specs\n", testTasksJSON)
	backend := state.NewSQLite(db, "demo", root)
	for _, title := range []string{"first", "second"} {
		if _, err := backend.QueueFinding(ctx, state.QueuedFinding{Title: title, Body: "body of " + title, QueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	filer := &countingFiler{}
	s, err := New(cfg(ProfileOperator, ""), Options{
		Static: spaHandler(t), State: backend,
		Paths:    config.Paths{Root: root, ID: "demo", ConfigYAML: filepath.Join(root, "config.yaml")},
		Findings: &FindingsDesk{Store: backend, Filer: filer},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got findingsPayload
	decode(t, get(t, s, "/api/findings"), &got)
	if got.Pending != 2 || len(got.Findings) != 2 || got.Findings[1].Body != "body of first" || !got.CanSend {
		t.Fatalf("findings = %+v", got)
	}

	if rec := steerPost(t, s, "/api/findings/1/send", ``, "127.0.0.1:50000", "http://evil.example"); rec.Code != http.StatusForbidden || filer.n != 0 {
		t.Errorf("cross-origin send = %d, filed %d", rec.Code, filer.n)
	}
	if rec := steerPost(t, s, "/api/findings/1/send", ``, "127.0.0.1:50000", "http://127.0.0.1:7420"); rec.Code != http.StatusOK || filer.n != 1 {
		t.Fatalf("send = %d %s, filed %d", rec.Code, rec.Body, filer.n)
	}
	if rec := steerPost(t, s, "/api/findings/2/dismiss", ``, "127.0.0.1:50000", "http://127.0.0.1:7420"); rec.Code != http.StatusOK {
		t.Fatalf("dismiss = %d %s", rec.Code, rec.Body)
	}
	if rec := steerPost(t, s, "/api/findings/1/send", ``, "127.0.0.1:50000", "http://127.0.0.1:7420"); rec.Code != http.StatusBadRequest || filer.n != 1 {
		t.Errorf("re-send = %d, filed %d; want refused", rec.Code, filer.n)
	}
	decode(t, get(t, s, "/api/findings"), &got)
	if got.Pending != 0 || got.Findings[1].Status != state.FindingSent || got.Findings[0].Status != state.FindingDismissed {
		t.Errorf("after the decisions = %+v", got)
	}
}

// /api/findings is the operator's: a stakeholder token does not reach it.
func TestFindingsAreGated(t *testing.T) {
	s := newGatedServer(t, &fakeState{})
	if resp := get(t, s, "/api/findings?token=test-token-stakeholder"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("stakeholder token: status = %d, want 403", resp.StatusCode)
	}
}
