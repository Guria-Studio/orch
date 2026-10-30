package vcs

import "testing"

func TestParseRemote(t *testing.T) {
	cases := []struct{ in, clean, host, slug string }{
		{"git@github.com:Guria-Studio/orch.git", "github.com:Guria-Studio/orch.git", "github.com", "Guria-Studio/orch"},
		{"https://github.com/Guria-Studio/orch.git", "https://github.com/Guria-Studio/orch.git", "github.com", "Guria-Studio/orch"},
		// A token in the remote never reaches the page.
		{"https://x-access-token:ghs_secret@github.com/o/r.git", "https://github.com/o/r.git", "github.com", "o/r"},
		{"ssh://git@gitlab.example.com:2222/group/sub/proj.git", "ssh://gitlab.example.com:2222/group/sub/proj.git", "gitlab.example.com", "group/sub/proj"},
		{"/srv/git/local.git", "/srv/git/local.git", "", ""},
		{"", "", "", ""},
	}
	for _, c := range cases {
		clean, host, slug := parseRemote(c.in)
		if clean != c.clean || host != c.host || slug != c.slug {
			t.Errorf("parseRemote(%q) = %q, %q, %q; want %q, %q, %q", c.in, clean, host, slug, c.clean, c.host, c.slug)
		}
	}
}

func TestDescribeRepoReadsOriginAndBranch(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "feat/x"},
		{"remote", "add", "origin", "https://tok@github.com/o/r.git"},
	} {
		if _, err := runIn(dir, "git", nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	got := DescribeRepo(dir)
	want := Repo{Remote: "https://github.com/o/r.git", Host: "github.com", Slug: "o/r", Branch: "feat/x"}
	if got != want {
		t.Errorf("DescribeRepo = %+v, want %+v", got, want)
	}
	if got := DescribeRepo(t.TempDir()); got != (Repo{}) {
		t.Errorf("DescribeRepo(not a repo) = %+v, want zero", got)
	}
}
