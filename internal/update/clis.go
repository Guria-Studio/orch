package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// The agent CLIs orch dispatches to (#448) are checked the way orch itself is:
// one read of the npm registry per package at most once a day, cached in
// ~/.orch, and a registry that cannot be reached says nothing.

const defaultRegistry = "https://registry.npmjs.org"

// Registry reads the latest version of an npm package.
type Registry struct {
	HTTP *http.Client
	// URL is the registry's root; empty means registry.npmjs.org.
	URL string
}

// Latest is the version the package's `latest` dist-tag points at.
func (r Registry) Latest(ctx context.Context, pkg string) (string, error) {
	root := r.URL
	if root == "" {
		root = defaultRegistry
	}
	httpc := r.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 5 * time.Second}
	}
	// A scoped name keeps its "@" and escapes its "/", as npm itself asks.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root+"/"+url.PathEscape(pkg)+"/latest", nil)
	if err != nil {
		return "", fmt.Errorf("building the registry request: %w", err)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("asking the npm registry for %s: %w", pkg, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asking the npm registry for %s: HTTP %d", pkg, resp.StatusCode)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil || body.Version == "" {
		return "", fmt.Errorf("reading %s's latest version from the npm registry", pkg)
	}
	return body.Version, nil
}

// CLIVersionsPath is where the CLIs' latest versions are remembered:
// ~/.orch/cli-versions.json.
func CLIVersionsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return filepath.Join(home, ".orch", "cli-versions.json"), nil
}

type cliEntry struct {
	CheckedAt time.Time `json:"checked_at"`
	Version   string    `json:"version"`
}

// cliCacheMu serializes the read-modify-write of the cache: doctor and the
// Tools page ask for several packages at once.
var cliCacheMu sync.Mutex

// LatestCLI is pkg's latest version, from the cache at path while it is
// younger than MaxAge and from the registry otherwise. Like Check, a failed
// read returns the last known version with the error and is still recorded,
// so an offline machine waits out the timeout once a day, not every command.
func LatestCLI(ctx context.Context, r Registry, path, pkg string, now time.Time) (string, error) {
	cliCacheMu.Lock()
	defer cliCacheMu.Unlock()
	entries := map[string]cliEntry{}
	if data, err := os.ReadFile(path); err == nil { // #nosec G304 -- orch's own cache file
		_ = json.Unmarshal(data, &entries)
	}
	old, ok := entries[pkg]
	if ok && now.Sub(old.CheckedAt) < MaxAge && !now.Before(old.CheckedAt) {
		return old.Version, nil
	}
	version, fetchErr := r.Latest(ctx, pkg)
	if fetchErr != nil {
		version = old.Version
	}
	entries[pkg] = cliEntry{CheckedAt: now, Version: version}
	data, err := json.Marshal(entries)
	if err == nil {
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
			err = os.WriteFile(path, data, 0o600)
		}
	}
	if fetchErr != nil {
		return version, fetchErr
	}
	if err != nil {
		return version, fmt.Errorf("writing %s: %w", path, err)
	}
	return version, nil
}

var looseVersion = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// LooseVersion is the first x.y.z in s, whatever surrounds it: a CLI's
// `--version` says "codex-cli 0.154.0" or "2.1.310 (Claude Code)".
func LooseVersion(s string) (string, bool) {
	m := looseVersion.FindString(s)
	return m, m != ""
}

// CLIBehind reports whether installed is older than latest; either one
// without a version in it is not behind.
func CLIBehind(installed, latest string) bool {
	a := looseVersion.FindStringSubmatch(installed)
	b := looseVersion.FindStringSubmatch(latest)
	if a == nil || b == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return x < y
		}
	}
	return false
}
