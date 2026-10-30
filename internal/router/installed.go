package router

import (
	"os/exec"

	"github.com/Guria-Studio/orch/internal/model"
)

// BackendPreference is every backend, in the order a route is picked in when several backends
// are installed: the CLIs most people have first.
var BackendPreference = []model.Backend{
	model.BackendClaude, model.BackendCodex, model.BackendGemini, model.BackendOpencode, model.BackendAgy,
	model.BackendMuse,
}

// OnPath reports whether a backend's CLI is on PATH; each backend's binary
// has the backend's name.
func OnPath(b model.Backend) bool {
	_, err := exec.LookPath(string(b))
	return err == nil
}

// ForTier is the route key of tier whose backend installed accepts — by
// BackendPreference, then by key — or "" when no installed backend has a
// route of that tier (#377).
func (r Router) ForTier(tier model.Tier, installed func(model.Backend) bool) string {
	keys := r.Keys()
	for _, b := range BackendPreference {
		if !installed(b) {
			continue
		}
		for _, k := range keys {
			if e := r[k]; e.Tier == tier && e.Backend == b {
				return k
			}
		}
	}
	return ""
}
