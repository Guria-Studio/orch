package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// SpecRefCheck is what a task's specRef resolves to inside one directory: a
// project root, or the worktree a task is about to run in (#341, #342).
//
// The two directories can disagree, and that disagreement is the bug: `orch
// atomize` reads specs from the operator's working tree, while a task's
// worktree starts from the base branch, so a spec written but never committed
// and pushed is there for the first and missing for the second.
type SpecRefCheck struct {
	// Ref is the specRef as written in tasks.json.
	Ref string
	// File is the spec's path relative to the directory checked, slash
	// separated: the candidate that exists, or spec_root/<file> when none
	// does. Empty when Ref names no file.
	File string
	// Anchor is what follows `#` in Ref, or "".
	Anchor string
	// FileFound says File exists in the directory checked.
	FileFound bool
	// AnchorFound says File has a heading for Anchor. True when there is no
	// anchor to look for, false when the file itself is missing.
	AnchorFound bool
}

// OK is a spec ref an agent can follow: no ref at all, or a file that exists
// and holds its anchor.
func (c SpecRefCheck) OK() bool {
	return c.Ref == "" || (c.FileFound && c.AnchorFound)
}

// CheckSpecRef resolves specRef under dir the way SpecOutline does: under
// spec_root first, then as written, because specs from before bug 12 carry the
// `specs/` prefix themselves. A spec that exists and cannot be read is an
// error; one that is not there is a result.
func CheckSpecRef(dir, specRoot, specRef string) (SpecRefCheck, error) {
	file, anchor, _ := strings.Cut(specRef, "#")
	c := SpecRefCheck{Ref: specRef, Anchor: anchor}
	if file == "" {
		c.AnchorFound = specRef == ""
		return c, nil
	}
	data, rel, found, err := readSpec(dir, specRoot, file)
	if err != nil {
		return c, err
	}
	c.File = rel
	if !found {
		return c, nil
	}
	c.FileFound = true
	c.AnchorFound = anchor == "" || HasAnchor(string(data), anchor)
	return c, nil
}

// specCandidates are the paths a specRef's file is looked for at, relative to
// a project root, in order.
func specCandidates(specRoot, file string) []string {
	if specRoot == "" {
		return []string{filepath.ToSlash(filepath.Clean(file))}
	}
	return []string{
		filepath.ToSlash(filepath.Join(specRoot, file)),
		filepath.ToSlash(filepath.Clean(file)),
	}
}

// readSpec reads the first candidate for file that exists under dir, and
// returns its path relative to dir. Not found is (nil, spec_root/file, false,
// nil).
func readSpec(dir, specRoot, file string) ([]byte, string, bool, error) {
	candidates := specCandidates(specRoot, file)
	for _, rel := range candidates {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		data, err := os.ReadFile(path) // #nosec G304 -- a spec path inside the operator's own project or a task worktree.
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, rel, false, fmt.Errorf("reading spec %s: %w", path, err)
		}
		return data, rel, true, nil
	}
	return nil, candidates[0], false, nil
}

var (
	headingLine = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$`)
	explicitID  = regexp.MustCompile(`(?:\{#|\bid="|\bname=")([^"}]+)`)
)

// HasAnchor reports whether markdown has a heading for anchor.
//
// Three spellings count, because three kinds of spec reach tasks.json: a
// heading that starts with the anchor as its own word (`### F11.1.T1 — Load
// the dataset`, which is what `orch atomize` writes specRefs from; the next
// character must not continue the id, so F1.1.T1 does not match F1.1.T10), a
// GitHub-style slug of the whole heading (`#load-the-dataset`), and an
// explicit `{#id}` or `<a id="...">`. Fenced code blocks are skipped: a spec
// showing the format in an example is not a spec that has the section.
func HasAnchor(markdown, anchor string) bool {
	anchor = strings.TrimSpace(anchor)
	if anchor == "" {
		return true
	}
	inFence := false
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range explicitID.FindAllStringSubmatch(line, -1) {
			if m[1] == anchor {
				return true
			}
		}
		m := headingLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := m[1]
		if rest, ok := strings.CutPrefix(text, anchor); ok && !continuesID(rest) {
			return true
		}
		if slug(text) == strings.ToLower(anchor) {
			return true
		}
	}
	return false
}

// continuesID says the text right after a matched prefix is still part of the
// same identifier: `F1.1.T1` followed by `0`, or by `.2`.
func continuesID(rest string) bool {
	if rest == "" {
		return false
	}
	r := []rune(rest)
	if unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]) || r[0] == '_' {
		return true
	}
	return (r[0] == '.' || r[0] == '-') && len(r) > 1 && (unicode.IsLetter(r[1]) || unicode.IsDigit(r[1]))
}

// slug is GitHub's heading anchor: lower-cased, punctuation dropped, spaces
// to hyphens.
func slug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// SpecGitState is where a spec file stands in git, relative to the branch a
// task's worktree starts from.
type SpecGitState struct {
	// InRepo is false when the directory is not inside a git work tree; the
	// other fields are then meaningless.
	InRepo bool
	// Untracked and Modified describe the working tree against HEAD.
	Untracked bool
	Modified  bool
	// BaseRef is the ref the file was looked for in: origin/<base> when the
	// remote-tracking branch exists, else the local <base>, else "" (no base
	// to compare against, and OnBase is meaningless).
	BaseRef string
	// OnBase says the file exists in BaseRef.
	OnBase bool
}

// gitTimeout bounds each git command here. They are local reads; a git that
// takes this long is hung, and a warning is not worth hanging a command for.
const gitTimeout = 10 * time.Second

// BaseRef is the ref a task's worktree starts from, as far as the local
// repository knows: origin/<base> when that remote-tracking branch exists,
// else the local <base>, else "". dir must be inside the repository.
func BaseRef(dir, base string) string {
	if base == "" {
		base = "main"
	}
	for _, ref := range []string{"origin/" + base, base} {
		if _, err := git(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			return ref
		}
	}
	return ""
}

// InGitRepo says dir is inside a git work tree.
func InGitRepo(dir string) bool {
	out, err := git(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// ExistsAtRef says relPath (relative to dir, slash separated) exists in ref.
func ExistsAtRef(dir, ref, relPath string) bool {
	_, err := git(dir, "cat-file", "-e", ref+":./"+relPath)
	return err == nil
}

// ReadAtRef reads relPath (relative to dir) as it is in ref.
func ReadAtRef(dir, ref, relPath string) (string, error) {
	return git(dir, "show", ref+":./"+relPath)
}

// GitStateOf reports where relPath (relative to dir) stands against base.
func GitStateOf(dir, relPath, base string) SpecGitState {
	if !InGitRepo(dir) {
		return SpecGitState{}
	}
	st := SpecGitState{InRepo: true}
	if _, err := git(dir, "ls-files", "--error-unmatch", "--", relPath); err != nil {
		st.Untracked = true
	} else if _, err := git(dir, "diff", "--quiet", "HEAD", "--", relPath); err != nil {
		st.Modified = true
	}
	st.BaseRef = BaseRef(dir, base)
	if st.BaseRef != "" {
		st.OnBase = ExistsAtRef(dir, st.BaseRef, relPath)
	}
	return st
}

// git runs one read-only git command in dir.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	// #nosec G204 -- fixed git subcommands; the only variable parts are a ref and a path from the operator's own project.
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
