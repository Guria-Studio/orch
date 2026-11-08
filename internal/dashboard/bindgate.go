package dashboard

import (
	"net/http"

	"github.com/Guria-Studio/orch/internal/tunnel"
)

// loopbackBind reports whether host only listens on this machine. An empty
// host is the default, 127.0.0.1.
func loopbackBind(host string) bool {
	return host == "" || tunnel.IsLoopbackHost(host)
}

// bindVerdict is the gate the listening address adds to every data route
// (#537), before the profile's own rules.
//
// Bound to loopback, only a loopback Host is served: a page on a domain that
// rebinds to 127.0.0.1 sends its own name as Host, and without this check it
// could read logs, diffs and config through the operator's browser.
//
// Bound beyond loopback (`--host 0.0.0.0`), anyone not on this machine shows
// the dashboard token — the operator surface included, which the operator
// profile otherwise leaves open. The stakeholder surface keeps its own rule
// (decide), which already wants that token.
func (s *Server) bindVerdict(r *http.Request) Verdict {
	if loopbackBind(s.cfg.Host) {
		if !tunnel.IsLoopbackHost(tunnel.ExtractHost(r.Host)) {
			return Forbidden
		}
		return Allow
	}
	if isLocalRequest(r) || s.cfg.stakeholderContext(r.URL.Path) {
		return Allow
	}
	hash := s.expectedTokenHash(r.Context())
	if hash == "" || !constantTimeEqual(HashToken(tokenFrom(r)), hash) {
		return Unauthorized
	}
	return Allow
}
