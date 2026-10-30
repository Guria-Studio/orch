package dashboard

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/update"
)

func releasesServer(t *testing.T, version string, list func(context.Context) ([]update.Release, error)) *Server {
	t.Helper()
	root := writeProject(t, "spec_root: specs\n", testTasksJSON)
	s, err := New(cfg(ProfileOperator, ""), Options{
		Static:   spaHandler(t),
		State:    &fakeState{},
		Paths:    config.Paths{Root: root, ID: "demo", ConfigYAML: filepath.Join(root, "config.yaml")},
		Version:  version,
		Releases: list,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

var someReleases = []update.Release{
	{Tag: "v0.18.0", Notes: "- new", PublishedAt: "2026-10-20T00:00:00Z", Critical: true},
	{Tag: "v0.17.0", Notes: "- this one", PublishedAt: "2026-10-16T00:00:00Z"},
	{Tag: "v0.16.0", Notes: "- older", PublishedAt: "2026-09-26T00:00:00Z"},
}

func TestReleasesMarkTheRunningVersionAndWhatIsNewer(t *testing.T) {
	s := releasesServer(t, "v0.17.0", func(context.Context) ([]update.Release, error) { return someReleases, nil })
	var got releasesPayload
	decode(t, get(t, s, "/api/releases"), &got)
	if got.Current != "v0.17.0" || got.Error != "" || len(got.Releases) != 3 {
		t.Fatalf("payload = %+v", got)
	}
	want := []struct{ newer, current bool }{{true, false}, {false, true}, {false, false}}
	for i, w := range want {
		r := got.Releases[i]
		if r.Newer != w.newer || r.Current != w.current {
			t.Errorf("%s: newer=%v current=%v, want %v %v", r.Tag, r.Newer, r.Current, w.newer, w.current)
		}
	}
	if got.Releases[0].Notes != "- new" || !got.Releases[0].Critical || got.Releases[0].PublishedAt == "" {
		t.Errorf("first release = %+v", got.Releases[0])
	}

	// A dev build is not older than anything.
	dev := releasesServer(t, "dev", func(context.Context) ([]update.Release, error) { return someReleases, nil })
	decode(t, get(t, dev, "/api/releases"), &got)
	for _, r := range got.Releases {
		if r.Newer || r.Current {
			t.Errorf("dev build: %s newer=%v current=%v", r.Tag, r.Newer, r.Current)
		}
	}
}

// Offline the cached list still answers, with the reason; with checks off
// there is nothing to list, and the page is told why. Never a 500.
func TestReleasesNeverFail(t *testing.T) {
	offline := releasesServer(t, "v0.17.0", func(context.Context) ([]update.Release, error) {
		return someReleases[1:], errors.New("asking GitHub for orch's releases: HTTP 403")
	})
	resp := get(t, offline, "/api/releases")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("offline: status %d", resp.StatusCode)
	}
	var got releasesPayload
	decode(t, resp, &got)
	if len(got.Releases) != 2 || got.Error == "" {
		t.Errorf("offline = %+v, want the cached two and the error", got)
	}

	var off releasesPayload
	decode(t, get(t, releasesServer(t, "v0.17.0", nil), "/api/releases"), &off)
	if off.Releases == nil || len(off.Releases) != 0 || off.Error == "" {
		t.Errorf("checks off = %+v, want an empty list and the reason", off)
	}
}

func TestReleasesAreOperatorOnly(t *testing.T) {
	s := newGatedServer(t, &fakeState{})
	if resp := get(t, s, "/api/releases?token=test-token-stakeholder"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("stakeholder token: status = %d, want 403", resp.StatusCode)
	}
}
