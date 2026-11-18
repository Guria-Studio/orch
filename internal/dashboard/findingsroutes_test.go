package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

	// get() leaves RemoteAddr at httptest's default (192.0.2.1, not
	// loopback); a real request from this machine is loopback on both Host
	// and peer, and that is the case Local must report true for.
	localReq := httptest.NewRequest(http.MethodGet, "/api/findings", nil)
	localReq.Host, localReq.RemoteAddr = "127.0.0.1:7420", "127.0.0.1:50000"
	localRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(localRec, localReq)
	var localGot findingsPayload
	if err := json.NewDecoder(localRec.Result().Body).Decode(&localGot); err != nil {
		t.Fatal(err)
	}
	if !localGot.Local {
		t.Errorf("Local = false for a loopback peer, want true")
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

// TestFindingsPayloadReportsLocal (#617): the GET payload says whether THIS
// request is the loopback write gate would allow, so the SPA can disable
// Send/Dismiss before the operator ever clicks a button that is known to be
// refused. A simulated LAN/Tailscale peer gets Local: false even though
// CanSend (a Filer is configured) stays true.
func TestFindingsPayloadReportsLocal(t *testing.T) {
	ctx := context.Background()
	db, _, err := state.Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	root := writeProject(t, "spec_root: specs\n", testTasksJSON)
	backend := state.NewSQLite(db, "demo", root)
	s, err := New(cfg(ProfileOperator, ""), Options{
		Static: spaHandler(t), State: backend,
		Paths:    config.Paths{Root: root, ID: "demo", ConfigYAML: filepath.Join(root, "config.yaml")},
		Findings: &FindingsDesk{Store: backend, Filer: &countingFiler{}},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/findings", nil)
	req.Host, req.RemoteAddr = "127.0.0.1:7420", "192.168.1.20:50000" // a LAN/Tailscale peer
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var got findingsPayload
	if err := json.NewDecoder(rec.Result().Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Local {
		t.Error("Local = true for a non-loopback peer, want false")
	}
	if !got.CanSend {
		t.Error("CanSend = false, want true — a Filer is still configured, Local is a separate thing")
	}
}

// TestFindingsWriteStillRefusedOffLoopback (#617 acceptance criterion 3): the
// write gate itself is not relaxed by the new Local field — a LAN/Tailscale
// POST to send or dismiss must still 403, same-origin notwithstanding.
func TestFindingsWriteStillRefusedOffLoopback(t *testing.T) {
	ctx := context.Background()
	db, _, err := state.Open(ctx, filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	root := writeProject(t, "spec_root: specs\n", testTasksJSON)
	backend := state.NewSQLite(db, "demo", root)
	if _, err := backend.QueueFinding(ctx, state.QueuedFinding{Title: "t", Body: "b", QueuedAt: time.Now()}); err != nil {
		t.Fatal(err)
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

	for _, path := range []string{"/api/findings/1/send", "/api/findings/1/dismiss"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(``))
		req.Host, req.RemoteAddr = "127.0.0.1:7420", "192.168.1.20:50000" // a LAN/Tailscale peer, same-origin
		req.Header.Set("Origin", "http://127.0.0.1:7420")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s from a LAN peer = %d, want 403", path, rec.Code)
		}
		var body errorPayload
		if err := json.NewDecoder(rec.Result().Body).Decode(&body); err == nil && body.Detail != "loopback only" {
			t.Errorf("POST %s detail = %q, want %q", path, body.Detail, "loopback only")
		}
	}
	if filer.n != 0 {
		t.Errorf("filed %d issues from a non-loopback peer, want 0", filer.n)
	}
}
