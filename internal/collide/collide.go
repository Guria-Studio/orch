// Package collide finds semantic collisions between branches that git merges
// cleanly (#522): two tasks that each add migration 005 under different file
// names, say. git sees two new files; the project sees two migrations claiming
// one place in the sequence, and breaks after both are merged.
//
// A Rule (semantic_checks in .orchestrator/config.yaml) names files by glob
// and says how to read an identity from them. Only what a branch adds is
// looked at — the files it adds relative to where it left the base, minus any
// the base already has at the same path — so a rule never fires on what was
// already there. Everything is read with git from the repository, no checkout
// is touched, and no dependency beyond git is needed.
package collide

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Rule kinds. KindSequence is the only one so far; unique_key and command
// are the spec's next ones (docs/design/issue-522-merge-queue.md §3.3).
const KindSequence = "sequence"

// Modes: block stops a merge, warn records the collision, off skips the rule.
const (
	ModeBlock = "block"
	ModeWarn  = "warn"
	ModeOff   = "off"
)

// gitTimeout bounds one git command.
const gitTimeout = time.Minute

// Rule is one semantic_checks entry.
type Rule struct {
	Name string `yaml:"name"`
	// Kind is how the rule reads an identity (KindSequence).
	Kind string `yaml:"kind"`
	// Paths are the globs the rule watches, repository-relative; `**`
	// crosses directories.
	Paths []string `yaml:"paths"`
	// Key is a regular expression over a watched file's NAME; its first
	// group is the identity, \d+ for a sequence number.
	Key string `yaml:"key"`
	// Contiguous also requires a sequence's new numbers to follow the base's
	// highest one with no gap.
	Contiguous bool `yaml:"contiguous"`
	// Mode is block, warn or off; empty is warn.
	Mode string `yaml:"mode"`
}

// Validate reports what makes the rule unusable.
func (r Rule) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("a rule needs a name")
	}
	if r.Kind != KindSequence {
		return fmt.Errorf("%s: kind %q is not one of: %s", r.Name, r.Kind, KindSequence)
	}
	if len(r.Paths) == 0 {
		return fmt.Errorf("%s: paths is empty, so the rule watches nothing", r.Name)
	}
	re, err := regexp.Compile(r.Key)
	if err != nil {
		return fmt.Errorf("%s: key: %w", r.Name, err)
	}
	if re.NumSubexp() < 1 {
		return fmt.Errorf("%s: key %q has no group; its first group is the identity", r.Name, r.Key)
	}
	switch r.Mode {
	case "", ModeBlock, ModeWarn, ModeOff:
	default:
		return fmt.Errorf("%s: mode %q is not one of block, warn, off", r.Name, r.Mode)
	}
	return nil
}

func (r Rule) mode() string {
	if r.Mode == "" {
		return ModeWarn
	}
	return r.Mode
}

// Branch is another open branch to compare with, and how to name it.
type Branch struct {
	Label string
	Ref   string
}

// Collision is one identity a branch adds that the base, or another branch,
// already claims.
type Collision struct {
	Rule string `json:"rule"`
	Key  string `json:"key"`
	// File is the branch's file.
	File string `json:"file"`
	// With is "base" or the other branch's label; WithFile its file, empty
	// for a contiguity gap.
	With     string `json:"with"`
	WithFile string `json:"with_file,omitempty"`
	Mode     string `json:"mode"`
	Reason   string `json:"reason"`
}

// Check runs rules over what head adds, against base and against what each
// of others adds. Refs are anything git rev-parse takes; base's Label is how
// the reasons name it.
func Check(ctx context.Context, repo string, rules []Rule, base Branch, head string, others []Branch) ([]Collision, error) {
	var active []Rule
	for _, r := range rules {
		if r.mode() == ModeOff {
			continue
		}
		if err := r.Validate(); err != nil {
			return nil, err
		}
		active = append(active, r)
	}
	if len(active) == 0 {
		return nil, nil
	}
	baseFiles, err := lines(ctx, repo, "ls-tree", "-r", "--name-only", "-z", base.Ref)
	if err != nil {
		return nil, err
	}
	inBase := map[string]bool{}
	for _, f := range baseFiles {
		inBase[f] = true
	}
	added := func(ref string) ([]string, error) {
		mb, err := lines(ctx, repo, "merge-base", base.Ref, ref)
		if err != nil || len(mb) == 0 {
			return nil, fmt.Errorf("collide: no common history between %s and %s: %w", base.Label, ref, err)
		}
		files, err := lines(ctx, repo, "diff", "--name-only", "--no-renames", "--diff-filter=A", "-z", mb[0], ref)
		if err != nil {
			return nil, err
		}
		return slices.DeleteFunc(files, func(f string) bool { return inBase[f] }), nil
	}
	headAdded, err := added(head)
	if err != nil {
		return nil, err
	}
	othersAdded := make([][]string, len(others))
	for i, o := range others {
		if othersAdded[i], err = added(o.Ref); err != nil {
			return nil, err
		}
	}

	var out []Collision
	for _, r := range active {
		out = append(out, r.sequence(base.Label, baseFiles, headAdded, others, othersAdded)...)
	}
	return out, nil
}

// sequence is a KindSequence rule's collisions.
func (r Rule) sequence(base string, baseFiles, headAdded []string, others []Branch, othersAdded [][]string) []Collision {
	re := regexp.MustCompile(r.Key) // Validate compiled it
	keys := func(files []string) map[string]string {
		m := map[string]string{}
		for _, f := range files {
			if k, ok := r.key(re, f); ok {
				if _, seen := m[k]; !seen {
					m[k] = f
				}
			}
		}
		return m
	}
	baseKeys := keys(baseFiles)
	type entry struct{ key, file string }
	var mine []entry
	for _, f := range headAdded {
		if k, ok := r.key(re, f); ok {
			mine = append(mine, entry{k, f})
		}
	}
	slices.SortFunc(mine, func(a, b entry) int { return compareKeys(a.key, b.key) })

	var out []Collision
	hit := func(e entry, with, withFile, reason string) {
		out = append(out, Collision{Rule: r.Name, Key: e.key, File: e.file, With: with, WithFile: withFile,
			Mode: r.mode(), Reason: r.Name + ": " + reason})
	}
	last, haveLast := -1, false
	for k := range baseKeys {
		if n, err := strconv.Atoi(k); err == nil && n > last {
			last, haveLast = n, true
		}
	}
	for _, e := range mine {
		if bf, ok := baseKeys[e.key]; ok {
			hit(e, "base", bf, fmt.Sprintf("%s adds %s, which %s already has as %s", e.file, e.key, base, bf))
			continue
		}
		n, err := strconv.Atoi(e.key)
		if !r.Contiguous || err != nil || !haveLast {
			continue
		}
		if n != last+1 {
			next := fmt.Sprintf("%0*d", len(e.key), last+1)
			hit(e, "base", "", fmt.Sprintf("%s adds %s, but %s ends at %0*d: the next is %s", e.file, e.key, base, len(e.key), last, next))
		}
		last = n
	}
	for i, o := range others {
		theirs := keys(othersAdded[i])
		for _, e := range mine {
			if of, ok := theirs[e.key]; ok && of != e.file {
				hit(e, o.Label, of, fmt.Sprintf("%s adds %s, which %s's %s also adds", e.file, e.key, o.Label, of))
			}
		}
	}
	return out
}

// key is a watched file's identity, if the rule watches it and its name
// carries one.
func (r Rule) key(re *regexp.Regexp, file string) (string, bool) {
	if !slices.ContainsFunc(r.Paths, func(p string) bool { return match(p, file) }) {
		return "", false
	}
	m := re.FindStringSubmatch(path.Base(file))
	if len(m) < 2 || m[1] == "" {
		return "", false
	}
	return m[1], true
}

// compareKeys orders numbers as numbers, anything else as text.
func compareKeys(a, b string) int {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)
	if errA == nil && errB == nil {
		return na - nb
	}
	return strings.Compare(a, b)
}

// match is path.Match over whole paths, where a `**` segment matches any
// number of directories, none included.
func match(pattern, name string) bool {
	return matchParts(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchParts(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(name); i++ {
				if matchParts(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// lines runs git in repo and splits its output on NULs and newlines.
func lines(ctx context.Context, repo string, args ...string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	// #nosec G204 -- fixed git subcommands; the refs are branch names and hashes orch made.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("collide: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 || r == '\n' }), nil
}
