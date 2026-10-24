package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// fakeRegistry answers /<pkg>/latest with version and counts the requests.
func fakeRegistry(t *testing.T, version string, hits *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if r.URL.EscapedPath() != "/@anthropic-ai%2Fclaude-code/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"name":"@anthropic-ai/claude-code","version":"` + version + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestCLIReadsTheRegistryOnceADay(t *testing.T) {
	hits := 0
	srv := fakeRegistry(t, "2.1.301", &hits)
	path := filepath.Join(t.TempDir(), "cli-versions.json")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{now, now.Add(time.Hour)} {
		got, err := LatestCLI(context.Background(), Registry{URL: srv.URL}, path, "@anthropic-ai/claude-code", at)
		if err != nil || got != "2.1.301" {
			t.Fatalf("LatestCLI = %q, %v; want 2.1.301", got, err)
		}
	}
	if hits != 1 {
		t.Errorf("the registry was asked %d times within a day, want 1", hits)
	}
	if _, err := LatestCLI(context.Background(), Registry{URL: srv.URL}, path, "@anthropic-ai/claude-code", now.Add(25*time.Hour)); err != nil || hits != 2 {
		t.Errorf("after a day: err=%v hits=%d, want a second read", err, hits)
	}
}

// Offline: the error comes back with no version, and the attempt is recorded
// so the next command does not wait on the network again.
func TestLatestCLIUnreachableIsUnknownAndRemembered(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	path := filepath.Join(t.TempDir(), "cli-versions.json")
	now := time.Now()
	got, err := LatestCLI(context.Background(), Registry{URL: url}, path, "opencode-ai", now)
	if err == nil || got != "" {
		t.Fatalf("LatestCLI = %q, %v; want an error and no version", got, err)
	}
	if got, err := LatestCLI(context.Background(), Registry{URL: url}, path, "opencode-ai", now.Add(time.Minute)); err != nil || got != "" {
		t.Errorf("second call = %q, %v; want the recorded attempt, no error", got, err)
	}
}

func TestCLIBehind(t *testing.T) {
	for _, c := range []struct {
		installed, latest string
		want              bool
	}{
		{"2.1.290 (Claude Code)", "2.1.301", true},
		{"codex-cli 0.154.0", "0.160.1", true},
		{"codex-cli 0.154.0", "0.154.0", false},
		{"1.18.32", "1.9.0", false},
		{"0.59.0", "1.0.0", true},
		{"(no output)", "1.0.0", false},
		{"1.0.0", "", false},
	} {
		if got := CLIBehind(c.installed, c.latest); got != c.want {
			t.Errorf("CLIBehind(%q, %q) = %v, want %v", c.installed, c.latest, got, c.want)
		}
	}
}
