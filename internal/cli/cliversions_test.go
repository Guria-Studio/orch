package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/update"
)

// withFakeClaude puts a `claude` that reports version on a PATH of its own,
// a fake npm registry whose latest is latest (an unreachable one when ""),
// and a fresh HOME for the cache (#448).
func withFakeClaude(t *testing.T, version, latest string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\necho '" + version + " (Claude Code)'\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil { // #nosec G306 -- a test's fake executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(update.DisableEnv, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"` + latest + `"}`))
	}))
	if latest == "" {
		srv.Close()
	} else {
		t.Cleanup(srv.Close)
	}
	t.Setenv(NPMRegistryEnv, srv.URL)
}

func doctorLine(t *testing.T, root, check string) string {
	t.Helper()
	out, _ := runOrch(t, "", "doctor", "--project-root", root)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, check) {
			return line
		}
	}
	t.Fatalf("no %s in:\n%s", check, out)
	return ""
}

// #448's acceptance, through `orch doctor`.
func TestDoctorSaysWhenAnAgentCLIIsBehind(t *testing.T) {
	for _, c := range []struct{ name, latest, want string }{
		{"newer", "2.1.301", "warn  backend.claude.version  claude 2.1.290 → 2.1.301 available: `npm install -g @anthropic-ai/claude-code@latest`"},
		{"equal", "2.1.290", "ok    backend.claude.version  claude 2.1.290 is the latest"},
		{"unreachable", "", "skip  backend.claude.version  claude 2.1.290: latest version unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			withFakeClaude(t, "2.1.290", c.latest)
			root := filepath.Join(t.TempDir(), "proj")
			if out, err := runOrch(t, "", "init", root, "--no-permissions"); err != nil {
				t.Fatalf("init: %v\n%s", err, out)
			}
			if got := strings.TrimSpace(doctorLine(t, root, "backend.claude.version")); !strings.HasPrefix(got, c.want) {
				t.Errorf("got  %q\nwant %q…", got, c.want)
			}
		})
	}
}

func TestDoctorCLICheckOptOut(t *testing.T) {
	withFakeClaude(t, "2.1.290", "2.1.301")
	root := filepath.Join(t.TempDir(), "proj")
	if out, err := runOrch(t, "", "init", root, "--no-permissions"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	cfgPath := filepath.Join(root, ".orchestrator", "config.yaml")
	raw, err := os.ReadFile(cfgPath) // #nosec G304 -- the test's own project
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, append(raw, []byte("\nupdate:\n  check_clis: false\n")...), 0o600); err != nil { // #nosec G703 -- the test's own project
		t.Fatal(err)
	}
	if got := doctorLine(t, root, "backend.claude.version"); !strings.Contains(got, "skip") || !strings.Contains(got, "check_clis: false") {
		t.Errorf("with check_clis: false: %q", got)
	}
}

func TestRunNoticeNamesOutdatedCLIs(t *testing.T) {
	withFakeClaude(t, "2.1.290", "2.1.301")
	var buf bytes.Buffer
	noteOutdatedCLIs(context.Background(), &buf, []string{"claude"}, cliLatest(config.Defaults()))
	if got := buf.String(); !strings.HasPrefix(got, "note: claude 2.1.290 → 2.1.301 available") {
		t.Errorf("notice = %q", got)
	}
}

// `orch router refresh` shows the routes a newly installed CLI brings, and
// writes them only with --apply.
func TestRouterRefreshDiffThenApply(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	t.Setenv("PATH", t.TempDir())
	if out, err := runOrch(t, "", "init", root, "--no-permissions"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	withFakeClaude(t, "2.1.290", "2.1.290")
	routerPath := filepath.Join(root, ".orchestrator", "model_router.yaml")
	before, _ := os.ReadFile(routerPath) // #nosec G304 -- the test's own project

	out, err := runOrch(t, "", "router", "refresh", "--project-root", root)
	if err != nil || !strings.Contains(out, "+ claude/") || !strings.Contains(out, "orch router refresh --apply") {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if after, _ := os.ReadFile(routerPath); !bytes.Equal(before, after) { // #nosec G304 -- the test's own project
		t.Fatal("the dry run wrote model_router.yaml")
	}
	if out, err = runOrch(t, "", "router", "refresh", "--apply", "--project-root", root); err != nil || !strings.Contains(out, "Added ") {
		t.Fatalf("--apply: %v\n%s", err, out)
	}
	if out, err = runOrch(t, "", "router", "refresh", "--project-root", root); err != nil || !strings.Contains(out, "No routes to add.") {
		t.Fatalf("after --apply: %v\n%s", err, out)
	}
	if out, err := runOrch(t, "", "router", "validate", "--project-root", root); err != nil {
		t.Fatalf("the refreshed router does not load: %v\n%s", err, out)
	}
}
