package dashboard

import (
	"io/fs"
	"net/http"
	"strings"
)

// The client portal, served live: the page `orch publish` writes as a static
// site, mounted at /stakeholder/, reading the same snapshot from
// /stakeholder/data.json instead of a file beside it.
//
// Every dashboard serves its SPA at /, a stakeholder's too: the client's view
// is the SPA's Delivery › Client view page, which reads this same snapshot,
// so links shared as /?token=… open the backoffice with the client's pages.
// /stakeholder/ stays the page `orch publish` exports, viewable here as is.

const (
	portalPrefix = "/stakeholder/"
	// portalIndex is the file vite emits for the portal's entry point.
	portalIndex = "stakeholder.html"
)

func (s *Server) portalRoutes() []route {
	if s.snapshot == nil {
		return nil
	}
	return []route{
		{pattern: "GET /stakeholder/data.json", name: "stakeholder_snapshot_json", handler: s.handlePortalSnapshot},
	}
}

func (s *Server) handlePortalSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := s.snapshot(r.Context())
	if err != nil {
		s.failRead(w, "portal snapshot", err)
		return
	}
	// Live data behind a token: no proxy or browser cache may hand it to the
	// next reader.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, snap)
}

// portalHandler serves the portal's files, ungated like the SPA's: they are
// code, and the data they fetch is gated. Anything it does not have is the
// same JSON 404 as every unclaimed path under a data prefix.
func (s *Server) portalHandler() http.Handler {
	files := http.FileServerFS(s.portal)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, portalPrefix)
		if name == "" {
			body, err := fs.ReadFile(s.portal, portalIndex)
			if err != nil {
				http.Error(w, "no portal in this build — run `make web`", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(body)
			return
		}
		if _, err := fs.Stat(s.portal, name); err != nil {
			handleAPINotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + name
		files.ServeHTTP(w, r2)
	})
}
