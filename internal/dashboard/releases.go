package dashboard

import (
	"context"
	"net/http"
	"time"

	"github.com/Guria-Studio/orch/internal/update"
)

// The What's new page (#364): orch's releases with their notes, the running
// version among them, and which ones `orch upgrade` would bring.

const releasesTimeout = 5 * time.Second

func (s *Server) releasesRoutes() []route {
	return []route{{pattern: "GET /api/releases", name: "api_releases", handler: s.handleReleases}}
}

type releaseItem struct {
	update.Release
	// Newer is "orch upgrade would install this"; Current is the running one.
	Newer   bool `json:"newer"`
	Current bool `json:"current"`
}

// releasesPayload's Error says why the list is short or empty — offline,
// GitHub refusing, update checks turned off — next to whatever is cached:
// the page says it, it never fails.
type releasesPayload struct {
	Current  string        `json:"current"`
	Releases []releaseItem `json:"releases"`
	Error    string        `json:"error,omitempty"`
}

func (s *Server) handleReleases(w http.ResponseWriter, r *http.Request) {
	out := releasesPayload{Current: s.version, Releases: []releaseItem{}}
	if s.releases == nil {
		out.Error = "update checks are off (" + update.DisableEnv + ")"
		writeJSON(w, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), releasesTimeout)
	defer cancel()
	list, err := s.releases(ctx)
	if err != nil {
		out.Error = err.Error()
	}
	for _, rel := range list {
		out.Releases = append(out.Releases, releaseItem{
			Release: rel,
			Newer:   update.IsRelease(s.version) && update.Less(s.version, rel.Tag),
			Current: rel.Tag == s.version,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
