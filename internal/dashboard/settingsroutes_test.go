package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
)

// The effective config by section, every leaf under the dotted key
// docs/CONFIG.md uses, defaults included.
func TestEffectiveConfigListsEveryKeyBySection(t *testing.T) {
	s, _ := newMoneyServer(t, &fakeState{}, map[string]string{"config.yaml": "spec_root: specs\ntunnel:\n  enabled: true\nnotifications:\n  slack_webhook: https://hooks.slack.test/T0/B0/secret\n"})
	var got effectiveConfigPayload
	decode(t, get(t, s, "/api/config/effective"), &got)
	if got.Error != "" {
		t.Fatalf("error = %q", got.Error)
	}
	values := map[string]string{}
	for _, sec := range got.Sections {
		for _, e := range sec.Entries {
			values[e.Key] = e.Value
		}
	}
	if values["tunnel.enabled"] != "true" {
		t.Errorf("tunnel.enabled = %q, want the file's true", values["tunnel.enabled"])
	}
	if v := values["notifications.slack_webhook"]; v != "(set)" {
		t.Errorf("notifications.slack_webhook = %q, want it masked", v)
	}
	if _, ok := values["verify.enabled"]; !ok {
		t.Errorf("verify.enabled missing: defaults belong on the page too (%d keys)", len(values))
	}
	if len(got.Sources) == 0 {
		t.Error("no sources: the page must say which file the values came from")
	}
	if resp := get(t, newGatedServer(t, &fakeState{}), "/api/config/effective?token=test-token-stakeholder"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("stakeholder: %d, want 403", resp.StatusCode)
	}
}

// Saving Settings goes through config.Set, all or nothing, and answers with
// the config as it now stands; a credential is stored but never echoed.
func TestConfigEditSavesThroughConfigSet(t *testing.T) {
	s, root := newMoneyServer(t, &fakeState{}, map[string]string{"config.yaml": "spec_root: specs # where specs live\n"})
	rec := operatorSend(t, s, http.MethodPatch, "/api/config",
		`{"set":{"tunnel.enabled":true,"concurrency.global_max":4,"verify.commands":["make lint","go test ./..."],"notifications.slack_webhook":"https://hooks.slack.test/T0/secret"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "hooks.slack.test/T0/secret") {
		t.Error("the answer echoed the webhook")
	}
	res, err := config.Load(filepath.Join(root, "config.yaml"), root)
	if err != nil {
		t.Fatal(err)
	}
	c := res.Config
	if !c.Tunnel.Enabled || c.Concurrency.GlobalMax != 4 || len(c.Verify.Commands) != 2 || c.Notifications.SlackWebhook == "" {
		t.Errorf("saved config = %+v", c)
	}
	body, _ := os.ReadFile(filepath.Join(root, "config.yaml")) // #nosec G304 -- the test project's own config.yaml
	if !strings.Contains(string(body), "# where specs live") {
		t.Errorf("the comment was lost:\n%s", body)
	}

	// A bad value is a 400 with the reason, and nothing is written.
	before, _ := os.ReadFile(filepath.Join(root, "config.yaml")) // #nosec G304 -- the test project's own config.yaml
	rec = operatorSend(t, s, http.MethodPatch, "/api/config", `{"set":{"tunnel.enabled":true,"concurrency.global_max":"lots"}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "whole number") {
		t.Errorf("bad value = %d %s, want 400 naming the type", rec.Code, rec.Body)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "config.yaml")); string(after) != string(before) { // #nosec G304 -- the test project's own config.yaml
		t.Error("a refused save changed config.yaml")
	}
}

func TestConfigEditRefusesAnyoneButTheLocalOperator(t *testing.T) {
	for _, tc := range []struct {
		name, remote, origin string
		profile              Profile
	}{
		{"stakeholder", "127.0.0.1:50000", "", ProfileStakeholder},
		{"not loopback", "192.168.1.20:50000", "", ProfileOperator},
		{"cross-origin", "127.0.0.1:50000", "https://evil.example", ProfileOperator},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := ""
			if tc.profile == ProfileStakeholder {
				token = "test-token-stakeholder"
			}
			s, _ := newSteerServer(t, tc.profile, token)
			req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(`{"set":{"tunnel.enabled":true}}`))
			req.Host = "127.0.0.1:7420"
			req.RemoteAddr = tc.remote
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			req.Header.Set("Authorization", "Bearer test-token-stakeholder")
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("PATCH = %d, want 403: %s", rec.Code, rec.Body)
			}
		})
	}
}
