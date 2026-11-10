package collide

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// uniqueKey is a KindUniqueKey rule's collisions: an identity on a line head
// adds to a watched file that the base already has on a line of a watched
// file, or that another open branch also adds.
func (r Rule) uniqueKey(ctx context.Context, repo string, base Branch, head string, others []Branch) ([]Collision, error) {
	re := regexp.MustCompile(r.Pattern) // Validate compiled it
	mine, err := r.addedKeys(ctx, repo, re, base.Ref, head)
	if err != nil || len(mine) == 0 {
		return nil, err
	}
	keys := make([]string, 0, len(mine))
	for k := range mine {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []Collision
	hit := func(key, with, withFile, reason string) {
		out = append(out, Collision{Rule: r.Name, Key: key, File: mine[key], With: with, WithFile: withFile,
			Mode: r.mode(), Reason: r.Name + ": " + reason})
	}
	inBase, err := r.baseKeys(ctx, repo, re, base.Ref, keys)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if bf, ok := inBase[k]; ok {
			hit(k, "base", bf, fmt.Sprintf("%s adds %s, which %s already has in %s", mine[k], k, base.Label, bf))
		}
	}
	for _, o := range others {
		theirs, err := r.addedKeys(ctx, repo, re, base.Ref, o.Ref)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if of, ok := theirs[k]; ok {
				hit(k, o.Label, of, fmt.Sprintf("%s adds %s, which %s's %s also adds", mine[k], k, o.Label, of))
			}
		}
	}
	return out, nil
}

// addedKeys are the identities on the lines ref adds to watched files since
// it left baseRef, each with the first file adding it. A key ref also
// removes (a line moved or edited in place) is not new, so it is left out.
func (r Rule) addedKeys(ctx context.Context, repo string, re *regexp.Regexp, baseRef, ref string) (map[string]string, error) {
	mb, err := lines(ctx, repo, "merge-base", baseRef, ref)
	if err != nil || len(mb) == 0 {
		return nil, fmt.Errorf("collide: no common history between %s and %s: %w", baseRef, ref, err)
	}
	out, _, err := git(ctx, repo, "-c", "core.quotePath=false", "diff", "-U0", "--no-color", "--no-ext-diff",
		"--no-renames", mb[0], ref)
	if err != nil {
		return nil, err
	}
	added := map[string]string{}
	removed := map[string]bool{}
	var oldFile, newFile string
	header := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			header, oldFile, newFile = true, "", ""
			continue
		case header && strings.HasPrefix(line, "--- "):
			oldFile = strings.TrimPrefix(strings.TrimPrefix(line, "--- "), "a/")
			continue
		case header && strings.HasPrefix(line, "+++ "):
			newFile = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			continue
		case strings.HasPrefix(line, "@@"):
			header = false
			continue
		case header:
			continue
		}
		switch {
		case strings.HasPrefix(line, "+") && r.watches(newFile):
			if k := firstGroup(re, line[1:]); k != "" {
				if _, seen := added[k]; !seen {
					added[k] = newFile
				}
			}
		case strings.HasPrefix(line, "-") && r.watches(oldFile):
			if k := firstGroup(re, line[1:]); k != "" {
				removed[k] = true
			}
		}
	}
	for k := range removed {
		delete(added, k)
	}
	return added, nil
}

// baseKeys finds which of keys the base already has on a line of a watched
// file, and in which file: `git grep` for the literal keys narrows the
// lines, the pattern decides.
func (r Rule) baseKeys(ctx context.Context, repo string, re *regexp.Regexp, baseRef string, keys []string) (map[string]string, error) {
	args := []string{"-c", "core.quotePath=false", "grep", "-I", "--no-color", "-z", "-F"}
	for _, k := range keys {
		args = append(args, "-e", k)
	}
	args = append(args, baseRef, "--")
	for _, p := range r.Paths {
		args = append(args, ":(glob)"+p)
	}
	out, code, err := git(ctx, repo, args...)
	if err != nil && code != 1 { // 1: no line matched
		return nil, err
	}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	found := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		name, content, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		file := strings.TrimPrefix(name, baseRef+":")
		if !r.watches(file) {
			continue
		}
		if k := firstGroup(re, content); want[k] {
			if _, seen := found[k]; !seen {
				found[k] = file
			}
		}
	}
	return found, nil
}

// command runs a KindCommand rule's script: a non-zero exit is one
// collision whose reason is what it printed.
func (r Rule) command(ctx context.Context, repo string, base Branch, head string, others []Branch) ([]Collision, error) {
	sha := func(ref string) (string, error) {
		out, err := lines(ctx, repo, "rev-parse", "--verify", ref+"^{commit}")
		if err != nil || len(out) == 0 {
			return "", fmt.Errorf("collide: resolving %s: %w", ref, err)
		}
		return out[0], nil
	}
	baseSHA, err := sha(base.Ref)
	if err != nil {
		return nil, err
	}
	headSHA, err := sha(head)
	if err != nil {
		return nil, err
	}
	var otherSHAs []string
	for _, o := range others {
		s, err := sha(o.Ref)
		if err != nil {
			return nil, err
		}
		otherSHAs = append(otherSHAs, s)
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	// #nosec G204 -- the operator's own semantic_checks command from config.yaml.
	cmd := exec.CommandContext(ctx, "sh", "-c", r.Run)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "ORCH_BASE_SHA="+baseSHA, "ORCH_HEAD_SHA="+headSHA,
		"ORCH_OTHER_HEADS="+strings.Join(otherSHAs, " "))
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil, nil
	case ctx.Err() != nil:
		return nil, fmt.Errorf("collide: %s: the command ran past %s", r.Name, commandTimeout)
	case !errors.As(err, &exit):
		return nil, fmt.Errorf("collide: %s: running %q: %w", r.Name, r.Run, err)
	}
	detail := strings.TrimSpace(buf.String())
	if detail == "" {
		detail = fmt.Sprintf("%q exited %d", r.Run, exit.ExitCode())
	}
	if len(detail) > 500 {
		detail = detail[:500] + "…"
	}
	return []Collision{{Rule: r.Name, With: "base", Mode: r.mode(), Reason: r.Name + ": " + detail}}, nil
}

// watches reports whether file is under one of the rule's paths.
func (r Rule) watches(file string) bool {
	return file != "" && file != "/dev/null" && slices.ContainsFunc(r.Paths, func(p string) bool { return match(p, file) })
}

// firstGroup is re's first group in s, "" when it does not match.
func firstGroup(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

// git runs git in repo and returns its output and exit code.
func git(ctx context.Context, repo string, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	// #nosec G204 -- fixed git subcommands; the refs are branch names and hashes orch made.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		return string(out), code, fmt.Errorf("collide: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), 0, nil
}
