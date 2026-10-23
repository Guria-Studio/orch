package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const budgetFiles = "# keep me\npresets:\n  conservative:\n    claude:\n      window_hours: 5\n      token_budget: 800000\n      threshold_pct: 60\n"

func budgetServer(t *testing.T) *Server {
	t.Helper()
	s, root := newMoneyServer(t, &fakeState{}, map[string]string{
		"config.yaml":  "spec_root: specs\nbudgets_config: budgets.yaml\nbudgets_preset: conservative\nconcurrency:\n  global_max: 6\n  per_provider:\n    claude: 3\n",
		"budgets.yaml": budgetFiles,
	})
	if err := os.MkdirAll(filepath.Join(root, ".orchestrator"), 0o750); err != nil {
		t.Fatal(err)
	}
	router := "claude/sonnet:\n  backend: claude\n  cli_model: claude-sonnet-4-6\n  tier: premium\ncodex/gpt-5:\n  backend: codex\n  cli_model: gpt-5\n  tier: standard\n"
	if err := os.WriteFile(filepath.Join(root, ".orchestrator", "model_router.yaml"), []byte(router), 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

// The Quota page's "Edit limits" goes through budget.SetLimits, as
// `orch budget set` does.
func TestBudgetEditWritesTheActivePreset(t *testing.T) {
	s := budgetServer(t)
	rec := operatorSend(t, s, http.MethodPost, "/api/budget/claude", `{"token_budget":1200000,"threshold_pct":75}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"token_budget":1200000`) {
		t.Fatalf("edit = %d %s", rec.Code, rec.Body)
	}
	raw, _ := os.ReadFile(filepath.Join(s.paths.Root, "budgets.yaml"))
	if !strings.Contains(string(raw), "threshold_pct: 75") || !strings.Contains(string(raw), "# keep me") {
		t.Errorf("budgets.yaml = %s", raw)
	}
	if rec := operatorSend(t, s, http.MethodPost, "/api/budget/claude", `{"threshold_pct":150}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid edit = %d %s, want 422", rec.Code, rec.Body)
	}
}

func TestBudgetEditIsTheLocalOperatorsOnly(t *testing.T) {
	s := budgetServer(t)
	for name, mutate := range map[string]func(*http.Request){
		"not loopback": func(r *http.Request) { r.RemoteAddr = "192.168.1.20:50000" },
		"cross-origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/budget/claude", strings.NewReader(`{"token_budget":1}`))
		req.Host, req.RemoteAddr = "127.0.0.1:7420", "127.0.0.1:50000"
		req.Header.Set("Origin", "http://127.0.0.1:7420")
		mutate(req)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, rec.Code)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(s.paths.Root, "budgets.yaml"))
	if string(raw) != budgetFiles {
		t.Error("a refused request changed budgets.yaml")
	}
}

// The routing table: every model_router.yaml route with the parallelism its
// backend gets.
func TestRouterListsRoutesWithTheirParallelism(t *testing.T) {
	s := budgetServer(t)
	var got routerPayload
	decode(t, get(t, s, "/api/router"), &got)
	if got.GlobalMax != 6 || len(got.Routes) != 2 {
		t.Fatalf("router = %+v", got)
	}
	if r := got.Routes[0]; r.Key != "claude/sonnet" || r.Backend != "claude" || r.CLIModel != "claude-sonnet-4-6" || r.MaxParallel != 3 {
		t.Errorf("first route = %+v", r)
	}
	// codex has no line in this config.yaml: the default per_provider (2) applies.
	if r := got.Routes[1]; r.Backend != "codex" || r.MaxParallel != 2 {
		t.Errorf("second route = %+v", r)
	}
	if resp := get(t, newGatedServer(t, &fakeState{}), "/api/router?token=test-token-stakeholder"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("stakeholder: %d, want 403", resp.StatusCode)
	}
}
