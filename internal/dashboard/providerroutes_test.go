package dashboard

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// The Providers and quota switch: the local operator turns a provider off
// and on through the same function as `orch provider`, and /api/quota says
// so; nobody else may flip it.
func TestProviderSwitchForTheLocalOperator(t *testing.T) {
	s, backend := newSteerServer(t, ProfileOperator, "")
	rec := steerPost(t, s, "/api/provider/codex/disable", `{"reason":"quota spent"}`, "127.0.0.1:50000", "http://127.0.0.1:7420")
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d: %s", rec.Code, rec.Body)
	}
	states, _ := backend.ProviderStates(context.Background())
	if len(states) != 1 || !states[0].Disabled || states[0].Reason != "quota spent" {
		t.Fatalf("states = %+v, want codex off", states)
	}

	var got quotaPayload
	decode(t, get(t, s, "/api/quota"), &got)
	if len(got.Providers) != 1 || got.Providers[0].Backend != "codex" || !got.Providers[0].Disabled || got.Providers[0].Reason != "quota spent" {
		t.Errorf("quota providers = %+v, want codex listed as off with its reason", got.Providers)
	}

	if rec := steerPost(t, s, "/api/provider/codex/enable", ``, "127.0.0.1:50000", "http://127.0.0.1:7420"); rec.Code != http.StatusOK {
		t.Fatalf("enable = %d: %s", rec.Code, rec.Body)
	}
	if states, _ := backend.ProviderStates(context.Background()); states[0].Disabled {
		t.Errorf("codex still off: %+v", states)
	}
	if rec := steerPost(t, s, "/api/provider/ngrok/disable", `{}`, "127.0.0.1:50000", "http://127.0.0.1:7420"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown provider") {
		t.Errorf("unknown provider = %d %s, want 400", rec.Code, rec.Body)
	}
}

func TestProviderSwitchRefusesAnyoneButTheLocalOperator(t *testing.T) {
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
			s, backend := newSteerServer(t, tc.profile, token)
			if rec := steerPost(t, s, "/api/provider/codex/disable", `{}`, tc.remote, tc.origin); rec.Code != http.StatusForbidden {
				t.Errorf("disable = %d, want 403: %s", rec.Code, rec.Body)
			}
			if states, _ := backend.ProviderStates(context.Background()); len(states) != 0 {
				t.Errorf("a refused request wrote %+v", states)
			}
		})
	}
}
