package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/hectorcanaimero/orch/internal/tunnel"
	"github.com/hectorcanaimero/orch/internal/vcs"
)

// The dashboard tunnel: a Cloudflare quick tunnel or a tailscale serve/funnel
// the operator raises from the Share panel (or a quick tunnel with `orch
// dashboard --tunnel`) to share this dashboard.
//
// # The gates, in a fixed order
//
// config enabled → operator profile → local caller, and PATH is consulted
// only after all three pass. Each gate's failure reveals less than the next
// one's: answering "cloudflared is missing" to someone on a shared URL would
// tell them what the box runs before establishing they may ask.
//
// # Who may reach the dashboard while the tunnel is up
//
// cloudflared and tailscale both connect to this listener from 127.0.0.1, so
// the internet arrives looking local by address. What gives it away is the
// Host header (the trycloudflare or ts.net hostname) and the headers the
// relay adds (Cloudflare's own; X-Forwarded-For from tailscale's proxy). While the
// tunnel is up, every gated route asks a request that is not local (see
// isLocalRequest) for a token, and the token decides how far it gets:
//
//   - the dashboard link token (minted on each start) opens everything, and
//     is for the operator's own use;
//   - the portal link token (minted alongside it) and the project's
//     stakeholder token reach only the stakeholder allow-list. The portal
//     link opens this same SPA, and /api/whoami answers such a request as a
//     stakeholder, so the client sees the backoffice with only their pages.
//     A client handed the portal link cannot turn it into the operator's
//     view by editing the path.
//
// The operator profile, which gates nothing for the person at the keyboard,
// is never what a stranger holding the URL gets.
//
// # Why start/stop do not break the read-only rule
//
// The dashboard takes a read-only StateReader (CHECKLIST rule 13) because a
// second writer to orch.db is the risk. Start and stop write no row: they
// drive a child process whose files live under state/tunnel/. They are
// local-only and refuse a cross-site Origin, so a web page elsewhere cannot
// press them through the operator's browser.

// tunnelDeps is what the routes need from the process.
//
// A nil Manager with Enabled true is a wiring bug, not a config state, and
// `/status` says so with a 500 rather than pretending the tunnel is idle.
type tunnelDeps struct {
	Enabled bool
	// Command overrides the binary, for tests; empty means cloudflared.
	Command          string
	URLParseTimeoutS int
	Manager          *tunnel.Manager

	mu sync.Mutex
	// The link tokens are minted on each start and dropped on stop, so a
	// shared link dies with the tunnel it was made for. Kept in plaintext only
	// to render the links for a local caller; requests compare the hashes.
	dashboardToken, dashboardHash string
	portalToken, portalHash       string
}

var errTunnelNotConfigured = errors.New("the tunnel is not enabled (tunnel.enabled in config.yaml)")

func (s *Server) tunnelRoutes() []route {
	return []route{
		{pattern: "GET /api/tunnel/capabilities", name: "api_tunnel_capabilities",
			handler: s.handleTunnelCapabilities},
		{pattern: "GET /api/tunnel/status", name: "api_tunnel_status",
			handler: s.handleTunnelStatus},
		{pattern: "POST /api/tunnel/start", name: "api_tunnel_start",
			handler: s.handleTunnelStart},
		{pattern: "POST /api/tunnel/stop", name: "api_tunnel_stop",
			handler: s.handleTunnelStop},
	}
}

// relayHeaders are set by Cloudflare or any reverse proxy in front of the
// dashboard, and by no browser talking to it directly.
var relayHeaders = []string{"Cf-Connecting-Ip", "Cf-Ray", "X-Forwarded-For", "X-Forwarded-Host", "Forwarded"}

// isLocalRequest reports whether the caller is on this machine and talking
// to the dashboard directly: a loopback peer, a loopback Host, and no header
// a relay adds. All three, because each alone is forgeable or ambiguous —
// cloudflared's peer address is loopback, a LAN client on a 0.0.0.0 bind
// can send `Host: 127.0.0.1`, and a proxy may rewrite Host.
func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return false
	}
	if !tunnel.IsLoopbackHost(tunnel.ExtractHost(r.Host)) {
		return false
	}
	for _, h := range relayHeaders {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	return true
}

// sameOriginLocal refuses a browser request sent from another site. A
// browser always sends Origin on a POST; a missing one is curl or a test,
// which isLocalRequest has already confined to this machine.
func sameOriginLocal(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && tunnel.IsLoopbackHost(tunnel.ExtractHost(u.Host))
}

// tunnelUp reports whether a tunnel may be forwarding to this listener.
// Error means the child exited; idle means none was started.
func (s *Server) tunnelUp() bool {
	m := s.tunnel.Manager
	if m == nil {
		return false
	}
	st := m.Status().State
	return st != tunnel.StateIdle && st != tunnel.StateError
}

// tunnelledVerdict is the extra gate for a non-local request while the tunnel
// is up: 401 without a token this tunnel knows, 403 for a portal-level token
// on a route outside the stakeholder allow-list.
//
// portal reports an allowed request that came in on a portal-level token:
// the stakeholder's view of an operator dashboard.
func (s *Server) tunnelledVerdict(r *http.Request, routeName string) (v Verdict, portal bool) {
	supplied := tokenFrom(r)
	if supplied == "" {
		return Unauthorized, false
	}
	hash := HashToken(supplied)
	s.tunnel.mu.Lock()
	dashboardHash, portalHash := s.tunnel.dashboardHash, s.tunnel.portalHash
	s.tunnel.mu.Unlock()
	if dashboardHash != "" && constantTimeEqual(hash, dashboardHash) {
		return Allow, false
	}
	portalLevel := portalHash != "" && constantTimeEqual(hash, portalHash)
	if !portalLevel {
		expected := s.expectedTokenHash(r.Context())
		portalLevel = expected != "" && constantTimeEqual(hash, expected)
	}
	switch {
	case !portalLevel:
		return Unauthorized, false
	case !s.cfg.routeAllowed(r.URL.Path, routeName):
		return Forbidden, false
	}
	return Allow, true
}

func newLinkToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// StartTunnel raises a share of this server's bound port through provider
// ("" for the default, cloudflared) and mints fresh link tokens. The URL
// arrives asynchronously; TunnelLinks has it once the provider prints it.
func (s *Server) StartTunnel(provider string) (tunnel.State, error) {
	if !s.tunnel.Enabled {
		return tunnel.State{}, errTunnelNotConfigured
	}
	if s.tunnel.Manager == nil {
		return tunnel.State{}, errNoTunnelManager
	}
	port := s.cfg.Port
	if _, p, err := net.SplitHostPort(s.BoundAddr()); err == nil {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	dashboardToken, err := newLinkToken()
	if err != nil {
		return tunnel.State{}, err
	}
	portalToken, err := newLinkToken()
	if err != nil {
		return tunnel.State{}, err
	}

	st, err := s.tunnel.Manager.Start(tunnel.ManagerConfig{
		Provider: provider, Command: s.tunnel.Command, Port: port, URLParseTimeoutS: s.tunnel.URLParseTimeoutS,
	})
	if err != nil {
		return st, err
	}
	s.tunnel.mu.Lock()
	s.tunnel.dashboardToken, s.tunnel.dashboardHash = dashboardToken, HashToken(dashboardToken)
	s.tunnel.portalToken, s.tunnel.portalHash = portalToken, HashToken(portalToken)
	s.tunnel.mu.Unlock()
	return st, nil
}

// StopTunnel stops the tunnel and invalidates its link tokens.
func (s *Server) StopTunnel() (tunnel.State, error) {
	if s.tunnel.Manager == nil {
		return tunnel.State{}, tunnel.ErrNotRunning
	}
	st, err := s.tunnel.Manager.Stop()
	if err == nil || errors.Is(err, tunnel.ErrNotRunning) {
		s.tunnel.mu.Lock()
		s.tunnel.dashboardToken, s.tunnel.dashboardHash = "", ""
		s.tunnel.portalToken, s.tunnel.portalHash = "", ""
		s.tunnel.mu.Unlock()
	}
	return st, err
}

// TunnelLinks returns the full-dashboard and client-portal links, each with
// its own token, once the tunnel has a URL. Empty when there is no URL yet,
// or no tokens (a tunnel adopted from a previous process: restart it).
func (s *Server) TunnelLinks() (dashboard, portal string) {
	if s.tunnel.Manager == nil {
		return "", ""
	}
	st := s.tunnel.Manager.Status()
	s.tunnel.mu.Lock()
	dashboardToken, portalToken := s.tunnel.dashboardToken, s.tunnel.portalToken
	s.tunnel.mu.Unlock()
	if st.URL == nil || dashboardToken == "" {
		return "", ""
	}
	return *st.URL + "/?token=" + url.QueryEscape(dashboardToken),
		*st.URL + "/?token=" + url.QueryEscape(portalToken)
}

// capabilitiesPayload is `/api/tunnel/capabilities`'s body.
//
// `reason` is the FIRST failing gate, never a list. `reasons` is the
// short-form list the SPA renders as chips: at most one entry. Binary, Host,
// Guides, DocsURL and ConfigBlocker are only filled for a caller who passed
// the first three gates — the local operator.
type capabilitiesPayload struct {
	Enabled       bool                  `json:"enabled"`
	Provider      *string               `json:"provider"`
	CanControl    bool                  `json:"can_control"`
	Reason        string                `json:"reason"`
	Reasons       []string              `json:"reasons"`
	Binary        *tunnel.Binary        `json:"binary,omitempty"`
	Host          *tunnel.Host          `json:"host,omitempty"`
	Guides        []tunnel.InstallGuide `json:"guides,omitempty"`
	DocsURL       string                `json:"docs_url,omitempty"`
	ConfigBlocker string                `json:"config_blocker,omitempty"`
	// Providers, Repo and LocalURL are what the Share panel draws, for the
	// local operator only: every provider with its binary and install
	// guides, the repository this dashboard serves, and its own address.
	Providers []tunnel.ProviderCapability `json:"providers,omitempty"`
	Repo      *vcs.Repo                   `json:"repo,omitempty"`
	LocalURL  string                      `json:"local_url,omitempty"`
}

// shortReason maps a gate's reason to the chip the SPA draws.
var shortReason = map[string]string{
	tunnel.ReasonConfigDisabled: "disabled",
	tunnel.ReasonProfileGate:    "not_operator",
	tunnel.ReasonHostGate:       "not_loopback",
}

// handleTunnelCapabilities always answers 200, and for a stakeholder needs no
// token (see noTokenRoutes): the SPA decides whether to draw the tunnel page
// before it has asked anybody for one.
func (s *Server) handleTunnelCapabilities(w http.ResponseWriter, r *http.Request) {
	gates := tunnel.Gates{
		Enabled:         s.tunnel.Enabled,
		OperatorProfile: s.cfg.Profile == ProfileOperator,
		LoopbackHost:    isLocalRequest(r),
	}
	payload := capabilitiesPayload{Enabled: s.tunnel.Enabled, Reasons: []string{}}
	if s.tunnel.Enabled {
		p := tunnel.Provider
		payload.Provider = &p
	}

	// PATH is only consulted once every other gate has passed — checking it
	// first would leak what the box has installed to a caller who may not ask.
	anyBinary := false
	if gates.Enabled && gates.OperatorProfile && gates.LoopbackHost {
		host := tunnel.DetectHost()
		payload.Host = &host
		payload.Providers = tunnel.DescribeProviders(host, s.tunnel.Command)
		for _, p := range payload.Providers {
			anyBinary = anyBinary || p.Binary.Found
			if p.ID == tunnel.ProviderCloudflared {
				bin := p.Binary
				payload.Binary = &bin
				payload.Guides, payload.DocsURL = p.Guides, p.DocsURL
				payload.ConfigBlocker = tunnel.ConfigBlocker()
			}
		}
		repo := vcs.DescribeRepo(s.paths.Root)
		payload.Repo = &repo
		payload.LocalURL = "http://" + s.BoundAddr()
	}
	payload.CanControl, payload.Reason = tunnel.EvaluateCapabilities(gates, anyBinary)
	if !payload.CanControl {
		if short, ok := shortReason[payload.Reason]; ok {
			payload.Reasons = append(payload.Reasons, short)
		} else {
			payload.Reasons = append(payload.Reasons, payload.Reason)
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

// tunnelStatusPayload is the manager's State plus the share links, and the
// provider's last output lines: what tailscale prints when Funnel is not
// allowed yet, or cloudflared when it cannot connect, is the explanation.
type tunnelStatusPayload struct {
	tunnel.State
	ShareURL  *string  `json:"share_url"`
	PortalURL *string  `json:"portal_url"`
	LogTail   []string `json:"log_tail"`
}

// statusLogTail is how many of the provider's last lines status carries.
const statusLogTail = 8

// controlGate answers the three gates for status/start/stop and reports
// whether the handler may continue. The status codes each say something
// different: 404 when the tunnel is not configured (the route may as well not
// exist), 403 for a profile or caller who may not ask.
func (s *Server) controlGate(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case !s.tunnel.Enabled:
		writeJSON(w, http.StatusNotFound, errorPayload{Detail: "not found"})
	case s.cfg.Profile != ProfileOperator:
		writeJSON(w, http.StatusForbidden, errorPayload{Detail: "operator only"})
	case !isLocalRequest(r):
		writeJSON(w, http.StatusForbidden, errorPayload{Detail: "loopback only"})
	case s.tunnel.Manager == nil:
		s.failRead(w, "tunnel status", errNoTunnelManager)
	default:
		return true
	}
	return false
}

func (s *Server) writeTunnelStatus(w http.ResponseWriter, status int) {
	payload := tunnelStatusPayload{State: s.tunnel.Manager.Status(), LogTail: []string{}}
	if logs := s.tunnel.Manager.LogsSnapshot(); len(logs) > 0 {
		payload.LogTail = logs[max(0, len(logs)-statusLogTail):]
	}
	if operator, portal := s.TunnelLinks(); operator != "" {
		payload.ShareURL, payload.PortalURL = &operator, &portal
	}
	writeJSON(w, status, payload)
}

func (s *Server) handleTunnelStatus(w http.ResponseWriter, r *http.Request) {
	if !s.controlGate(w, r) {
		return
	}
	s.writeTunnelStatus(w, http.StatusOK)
}

// tunnelStartBody is POST /api/tunnel/start's optional body.
type tunnelStartBody struct {
	Provider string `json:"provider"`
}

// handleTunnelStart refuses, with the reason as `detail`, what would only fail
// a moment later inside the provider: an unknown provider, a missing binary,
// or a config file that blocks quick tunnels. No body means cloudflared.
func (s *Server) handleTunnelStart(w http.ResponseWriter, r *http.Request) {
	if !s.controlGate(w, r) {
		return
	}
	if !sameOriginLocal(r) {
		writeJSON(w, http.StatusForbidden, errorPayload{Detail: "cross-origin request refused"})
		return
	}
	var body tunnelStartBody
	if r.ContentLength != 0 {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorPayload{Detail: "body: " + err.Error()})
			return
		}
	}
	provider := body.Provider
	if provider == "" {
		provider = tunnel.Provider
	}
	if !tunnel.KnownProvider(provider) {
		writeJSON(w, http.StatusBadRequest, errorPayload{Detail: "unknown provider " + strconv.Quote(provider)})
		return
	}
	command := s.tunnel.Command
	if command == "" {
		command = tunnel.CommandFor(provider)
	}
	if !tunnel.LookupBinary(command).Found {
		writeJSON(w, http.StatusConflict, errorPayload{Detail: tunnel.ReasonBinaryMissing})
		return
	}
	if provider == tunnel.ProviderCloudflared {
		if path := tunnel.ConfigBlocker(); path != "" {
			writeJSON(w, http.StatusConflict, errorPayload{Detail: tunnel.BlockerMessage(path)})
			return
		}
	}
	if _, err := s.StartTunnel(provider); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, tunnel.ErrAlreadyRunning) || errors.Is(err, tunnel.ErrLocked) {
			code = http.StatusConflict
		}
		writeJSON(w, code, errorPayload{Detail: err.Error()})
		return
	}
	s.writeTunnelStatus(w, http.StatusOK)
}

func (s *Server) handleTunnelStop(w http.ResponseWriter, r *http.Request) {
	if !s.controlGate(w, r) {
		return
	}
	if !sameOriginLocal(r) {
		writeJSON(w, http.StatusForbidden, errorPayload{Detail: "cross-origin request refused"})
		return
	}
	if _, err := s.StopTunnel(); err != nil && !errors.Is(err, tunnel.ErrNotRunning) {
		writeJSON(w, http.StatusInternalServerError, errorPayload{Detail: err.Error()})
		return
	}
	s.writeTunnelStatus(w, http.StatusOK)
}
