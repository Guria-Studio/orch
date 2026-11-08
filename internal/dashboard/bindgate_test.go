package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func hostRequest(t *testing.T, s *Server, path, host, remote, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	req.RemoteAddr = remote
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Code
}

// TestOperatorReadsAreGatedOffLoopback (#537): the operator profile gated
// writes only. Bound to 0.0.0.0, logs, diffs and config were open to the LAN
// without a token; bound to localhost, a page on a rebinding domain could
// read them through the victim's browser, since GETs never looked at Host.
func TestOperatorReadsAreGatedOffLoopback(t *testing.T) {
	t.Run("loopback bind refuses a foreign Host", func(t *testing.T) {
		s := newReadServer(t, &fakeState{})
		if got := hostRequest(t, s, "/api/tasks", "127.0.0.1:7420", "127.0.0.1:50000", ""); got != http.StatusOK {
			t.Errorf("local read = %d, want 200", got)
		}
		if got := hostRequest(t, s, "/api/tasks", "localhost:7420", "127.0.0.1:50000", ""); got != http.StatusOK {
			t.Errorf("localhost read = %d, want 200", got)
		}
		if got := hostRequest(t, s, "/api/tasks", "attacker.example:7420", "127.0.0.1:50000", ""); got != http.StatusForbidden {
			t.Errorf("rebinding read = %d, want 403", got)
		}
	})
	t.Run("non-loopback bind wants the token from anyone not local", func(t *testing.T) {
		c := cfg(ProfileOperator, "lan-secret")
		c.Host = "0.0.0.0"
		s := newReadServerWith(t, c, &fakeState{})
		if got := hostRequest(t, s, "/api/tasks", "192.168.1.5:7420", "192.168.1.9:50000", ""); got != http.StatusUnauthorized {
			t.Errorf("LAN read without a token = %d, want 401", got)
		}
		if got := hostRequest(t, s, "/api/tasks", "192.168.1.5:7420", "192.168.1.9:50000", "wrong"); got != http.StatusUnauthorized {
			t.Errorf("LAN read with a wrong token = %d, want 401", got)
		}
		if got := hostRequest(t, s, "/api/tasks", "192.168.1.5:7420", "192.168.1.9:50000", "lan-secret"); got != http.StatusOK {
			t.Errorf("LAN read with the token = %d, want 200", got)
		}
		if got := hostRequest(t, s, "/api/tasks", "127.0.0.1:7420", "127.0.0.1:50000", ""); got != http.StatusOK {
			t.Errorf("local read = %d, want 200", got)
		}
	})
}

// A non-loopback bind with no token would serve the operator surface to the
// network: the server refuses to start (#537).
func TestANonLoopbackBindNeedsAToken(t *testing.T) {
	c := cfg(ProfileOperator, "")
	c.Host = "0.0.0.0"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("Validate = %v, want a refusal naming the token", err)
	}
	c.Host = "127.0.0.1"
	if err := c.Validate(); err != nil {
		t.Errorf("loopback without a token: %v", err)
	}
}
