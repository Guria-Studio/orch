package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/dashboard"
)

// fakePlanner is a claude that, asked to plan (stream-json), writes one spec
// and says so in a stream-json line; any other call does nothing.
const fakePlanner = `#!/bin/sh
case "$*" in *stream-json*) ;; *) exit 0 ;; esac
cat >/dev/null
mkdir -p specs
cat > specs/f1-demo.md <<'SPEC'
# F1 — Demo

## F1.1 — Package: demo

### F1.1.T1 — Say hello

Print hello.

- **Modelo**: claude/claude-sonnet-4-6
- **Estimación**: 1h
SPEC
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"specs written"}]}}'
`

func TestWelcomeFlowFromEmptyFolderToEstimate(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fakePlanner), 0o755); err != nil { // #nosec G306 -- a test executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORCH_TELEMETRY", "off")
	ctx := context.Background()
	f := &welcomeFlow{ctx: ctx}
	root := filepath.Join(t.TempDir(), "app")

	if err := f.Project(ctx, root, false); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Fatalf("missing folder without create: err = %v", err)
	}
	if err := f.Project(ctx, root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "tasks.json")); err != nil {
		t.Fatalf("no project scaffolded: %v", err)
	}
	if err := f.Plan("a hello app"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for f.busy() {
		if time.Now().After(deadline) {
			t.Fatal("planning never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
	s := f.State().(welcomeState)
	if s.PlanError != "" || len(s.Steps) == 0 || s.Steps[0].Detail != "specs written" {
		t.Fatalf("plan: err %q, steps %+v", s.PlanError, s.Steps)
	}
	if len(s.Tasks) != 1 || s.Tasks[0].ID != "F1.1.T1" || s.Diff == "" || s.Applied {
		t.Fatalf("before apply: tasks %+v, diff %q, applied %v", s.Tasks, s.Diff, s.Applied)
	}
	if err := f.Run(); err == nil {
		t.Fatal("Run before Apply should be refused")
	}
	if err := f.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	s = f.State().(welcomeState)
	if !s.Applied || s.Ready != 1 || s.Diff != "" || s.Estimate == "" {
		t.Fatalf("after apply: applied %v, ready %d, diff %q, estimate %q", s.Applied, s.Ready, s.Diff, s.Estimate)
	}
}

func TestWelcomeRefusesOtherHostsAndOrigins(t *testing.T) {
	w, err := dashboard.NewWelcome(dashboard.Config{Host: dashboard.DefaultHost, Port: 0},
		http.NotFoundHandler(), &welcomeFlow{ctx: context.Background()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, host, origin string
		want               int
	}{
		{"local", "127.0.0.1:7420", "http://127.0.0.1:7420", http.StatusBadRequest}, // reaches the step: no folder chosen
		{"rebound host", "evil.example:7420", "", http.StatusForbidden},
		{"cross-site page", "127.0.0.1:7420", "https://evil.example", http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/welcome/plan", strings.NewReader(`{"idea":"x"}`))
		req.Host = tc.host
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		w.Handler().ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body)
		}
	}
	if _, err := dashboard.NewWelcome(dashboard.Config{Host: "0.0.0.0", Port: 0}, http.NotFoundHandler(), &welcomeFlow{}); err == nil {
		t.Error("a welcome page on 0.0.0.0 should be refused")
	}
}
