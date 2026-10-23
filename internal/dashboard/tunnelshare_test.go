package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/tunnel"
)

// startWith posts a provider choice to /api/tunnel/start as the local operator.
func startWith(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/start", strings.NewReader(body))
	req.Host = "127.0.0.1:7420"
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// The client link opens the same SPA, and the SPA learns from /api/whoami
// that this session is a stakeholder's, with the allow-list it may see.
func TestTunnelPortalLinkOpensTheSPAAsAStakeholder(t *testing.T) {
	s := startedTunnel(t)
	operator, portal := waitForLinks(t, s)
	relayed := http.Header{"Cf-Connecting-Ip": {"203.0.113.9"}}
	whoami := func(link string) whoamiPayload {
		t.Helper()
		u, err := url.Parse(link)
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != "/" {
			t.Errorf("%s opens %q, want the SPA at /", link, u.Path)
		}
		rec := request(t, s, http.MethodGet, "/api/whoami?token="+u.Query().Get("token"), tunnelHost, relayed)
		var got whoamiPayload
		decodeBody(t, rec, &got)
		return got
	}
	if got := whoami(portal); got.Profile != string(ProfileStakeholder) || !slices.Equal(got.Routes, s.cfg.StakeholderRoutes) || len(got.Routes) == 0 {
		t.Errorf("whoami on the client link = %+v, want stakeholder with the allow-list", got)
	}
	if got := whoami(operator); got.Profile != string(ProfileOperator) || got.Routes != nil {
		t.Errorf("whoami on the operator link = %+v, want operator", got)
	}
}

// tailscale funnel is started by choice, and its ts.net name becomes the links.
func TestTunnelStartsTheChosenProvider(t *testing.T) {
	script := fakeCloudflared(t)
	t.Setenv("FAKE_TUNNEL_LINES", "Available on the internet:\n\nhttps://box.tail1234.ts.net/\n|-- proxy http://127.0.0.1:7420\n")
	s := newTunnelServer(t, ProfileOperator, "", TunnelOptions{
		Enabled: true, Command: script, Manager: tunnel.NewManager(t.TempDir(), nil, 10),
	})
	if rec := startWith(t, s, `{"provider":"tailscale-funnel"}`); rec.Code != http.StatusOK {
		t.Fatalf("start = %d: %s", rec.Code, rec.Body)
	}
	t.Cleanup(func() { _, _ = s.StopTunnel() })
	operator, portal := waitForLinks(t, s)
	if !strings.HasPrefix(operator, "https://box.tail1234.ts.net/?token=") || !strings.HasPrefix(portal, "https://box.tail1234.ts.net/?token=") {
		t.Fatalf("links = %q, %q", operator, portal)
	}
	var status tunnelStatusPayload
	decodeBody(t, getWithHost(t, s, "/api/tunnel/status", "127.0.0.1:7420"), &status)
	if status.Provider == nil || *status.Provider != tunnel.ProviderTailscaleFunnel {
		t.Errorf("provider = %v, want %s", status.Provider, tunnel.ProviderTailscaleFunnel)
	}
	if len(status.LogTail) == 0 || !strings.Contains(strings.Join(status.LogTail, "\n"), "Available on the internet") {
		t.Errorf("log_tail = %q, want the provider's output", status.LogTail)
	}
}

func TestTunnelStartRefusesAnUnknownProvider(t *testing.T) {
	s := newTunnelServer(t, ProfileOperator, "", TunnelOptions{
		Enabled: true, Command: fakeCloudflared(t), Manager: tunnel.NewManager(t.TempDir(), nil, 10),
	})
	if rec := startWith(t, s, `{"provider":"ngrok"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("start = %d: %s, want 400", rec.Code, rec.Body)
	}
	if st := s.tunnel.Manager.Status().State; st != tunnel.StateIdle {
		t.Errorf("state = %q, want idle", st)
	}
}

// The Share panel's facts — providers, repository, local address — are for
// the local operator, never for a caller through the tunnel.
func TestTunnelCapabilitiesDescribeTheShareToTheLocalOperatorOnly(t *testing.T) {
	s := newTunnelServer(t, ProfileOperator, "", TunnelOptions{Enabled: true, Command: fakeCloudflared(t)})
	var local map[string]json.RawMessage
	decodeBody(t, getWithHost(t, s, "/api/tunnel/capabilities", "127.0.0.1:7420"), &local)
	var providers []tunnel.ProviderCapability
	if err := json.Unmarshal(local["providers"], &providers); err != nil {
		t.Fatalf("providers: %v", err)
	}
	var ids []string
	for _, p := range providers {
		ids = append(ids, p.ID)
		if !p.Binary.Found || len(p.Guides) == 0 {
			t.Errorf("%s = %+v, want the binary found and install guides", p.ID, p)
		}
	}
	if !slices.Equal(ids, tunnel.Providers()) {
		t.Errorf("providers = %v, want %v", ids, tunnel.Providers())
	}
	for _, key := range []string{"repo", "local_url"} {
		if _, ok := local[key]; !ok {
			t.Errorf("the local operator's capabilities lack %q", key)
		}
	}

	var remote map[string]json.RawMessage
	decodeBody(t, getWithHost(t, s, "/api/tunnel/capabilities", tunnelHost), &remote)
	for _, key := range []string{"providers", "repo", "local_url"} {
		if _, ok := remote[key]; ok {
			t.Errorf("a caller at %s was told %q", tunnelHost, key)
		}
	}
}
