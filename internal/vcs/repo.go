package vcs

import (
	"net/url"
	"strings"
)

// Repo is what the share panel says about the checkout a dashboard serves:
// which repository and which branch.
type Repo struct {
	// Remote is origin's URL with any credentials removed; "" without an
	// origin.
	Remote string `json:"remote"`
	// Host is the remote's host ("github.com"), Slug its path without .git
	// ("owner/name"); both "" when the URL has neither.
	Host string `json:"host"`
	Slug string `json:"slug"`
	// Branch is the checked-out branch; "" when HEAD is detached.
	Branch string `json:"branch"`
}

// DescribeRepo reads dir's origin and branch. A directory that is not a
// repository, or has no origin, answers what it has rather than an error:
// the panel shows the fields it can.
func DescribeRepo(dir string) Repo {
	var r Repo
	if out, err := runIn(dir, "git", nil, "remote", "get-url", "origin"); err == nil {
		r.Remote, r.Host, r.Slug = parseRemote(strings.TrimSpace(out))
	}
	// symbolic-ref rather than rev-parse: it names a branch with no commit
	// yet, and fails (leaving Branch "") on a detached HEAD.
	if out, err := runIn(dir, "git", nil, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		r.Branch = strings.TrimSpace(out)
	}
	return r
}

// parseRemote splits a git remote into a printable URL, host and slug. The
// printable URL never carries credentials: an https remote may embed a
// token (https://x-access-token:…@github.com/…), and this one is shown on a
// page.
func parseRemote(remote string) (clean, host, slug string) {
	if remote == "" {
		return "", "", ""
	}
	var path string
	if u, err := url.Parse(remote); err == nil && u.Scheme != "" && u.Host != "" {
		u.User = nil
		clean, host, path = u.String(), u.Hostname(), u.Path
	} else if at, rest, ok := strings.Cut(remote, ":"); ok && !strings.Contains(at, "/") {
		// scp-like: git@github.com:owner/name.git
		if _, h, ok := strings.Cut(at, "@"); ok {
			at = h
		}
		clean, host, path = at+":"+rest, at, rest
	} else {
		return remote, "", ""
	}
	slug = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	return clean, host, slug
}
