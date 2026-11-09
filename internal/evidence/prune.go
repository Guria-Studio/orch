package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultKeepAttempts is evidence.keep_attempts when it is unset.
const DefaultKeepAttempts = 3

// DefaultMaxBytesPerTask is evidence.max_bytes_per_task when it is unset:
// 200 MiB of E2E files per task.
const DefaultMaxBytesPerTask = 200 << 20

// Pruned is what Prune removed.
type Pruned struct {
	Attempts int
	Bytes    int64
}

// attemptRef is one verified attempt of a task, as its history has it.
type attemptRef struct {
	run     string
	attempt int
	at      int64 // VerifiedAt, unix nanoseconds
	history string
	e2e     string
	bytes   int64
}

func (a attemptRef) key() string { return fmt.Sprintf("%s#%d", a.run, a.attempt) }

// Prune keeps each task's evidence to its keep newest verified attempts and
// their E2E files to maxBytes (0: no limit), removing the rest: history
// manifests and E2E directories, including a task's E2E directories no
// manifest names (from before #520). The attempt that was pushed, the one
// evidence.json is, is never removed and counts toward keep. Only tasks with
// an evidence directory are touched.
func Prune(stateDir string, keep int, maxBytes int64) (Pruned, error) {
	if keep < 1 {
		keep = 1
	}
	var out Pruned
	tasks, err := os.ReadDir(filepath.Join(stateDir, Dir))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("evidence: %w", err)
	}
	var errs []error
	for _, t := range tasks {
		if !t.IsDir() {
			continue
		}
		if err := pruneTask(stateDir, t.Name(), keep, maxBytes, &out); err != nil {
			errs = append(errs, err)
		}
	}
	return out, errors.Join(errs...)
}

func pruneTask(stateDir, task string, keep int, maxBytes int64, out *Pruned) error {
	pushed, err := Read(stateDir, task)
	if err != nil {
		return err // a manifest that cannot be read: leave the task as it is
	}
	attempts, err := history(stateDir, task)
	if err != nil {
		return err
	}
	// Newest first; the pushed attempt goes in first whatever its age.
	sort.SliceStable(attempts, func(i, j int) bool { return attempts[i].at > attempts[j].at })
	isPushed := func(a attemptRef) bool { return pushed != nil && a.run == pushed.RunID && a.attempt == pushed.Attempt }
	var kept, dropped []attemptRef
	for _, a := range attempts {
		if isPushed(a) {
			kept = append([]attemptRef{a}, kept...)
		}
	}
	for _, a := range attempts {
		switch {
		case isPushed(a):
		case len(kept) < keep:
			kept = append(kept, a)
		default:
			dropped = append(dropped, a)
		}
	}
	if maxBytes > 0 {
		var total int64
		for i := range kept {
			kept[i].bytes = dirSize(filepath.Join(stateDir, kept[i].e2e))
			total += kept[i].bytes
		}
		// The oldest go first; kept[0] may be the pushed one, which stays.
		for i := len(kept) - 1; i >= 0 && total > maxBytes; i-- {
			if isPushed(kept[i]) {
				continue
			}
			total -= kept[i].bytes
			dropped = append(dropped, kept[i])
			kept = append(kept[:i], kept[i+1:]...)
		}
	}

	keepDirs := map[string]bool{}
	for _, a := range kept {
		keepDirs[a.e2e] = true
	}
	var errs []error
	for _, a := range dropped {
		out.Attempts++
		out.Bytes += removeAll(filepath.Join(stateDir, a.e2e), &errs)
		if err := os.Remove(a.history); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
		keepDirs[a.e2e] = true // counted already, whatever the orphan walk finds
	}
	// The task's E2E directories no manifest names.
	orphans, _ := filepath.Glob(filepath.Join(globQuote(stateDir), "e2e", "*", globQuote(task), "attempt-*"))
	for _, dir := range orphans {
		rel, err := filepath.Rel(stateDir, dir)
		if err != nil || keepDirs[filepath.ToSlash(rel)] {
			continue
		}
		out.Attempts++
		out.Bytes += removeAll(dir, &errs)
	}
	return errors.Join(errs...)
}

// history is a task's verified attempts, read from history/.
func history(stateDir, task string) ([]attemptRef, error) {
	dir := filepath.Join(stateDir, Dir, task, historyDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("evidence: %w", err)
	}
	var out []attemptRef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path) // #nosec G304 -- a manifest under orch's state dir
		if err != nil {
			continue
		}
		var m Manifest
		if json.Unmarshal(b, &m) != nil || m.RunID == "" || m.Attempt < 1 || strings.ContainsAny(m.RunID, `/\`) || m.RunID == ".." {
			continue // not one of ours: leave it
		}
		out = append(out, attemptRef{
			run: m.RunID, attempt: m.Attempt, at: m.VerifiedAt.UnixNano(), history: path,
			// engine.E2EArtifactsDir's layout, from the manifest's own ids
			// rather than its e2e.dir, so no manifest can point the
			// removal elsewhere.
			e2e: filepath.ToSlash(filepath.Join("e2e", m.RunID, task, fmt.Sprintf("attempt-%d", m.Attempt))),
		})
	}
	return out, nil
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// removeAll removes dir and returns how many bytes it held, then the
// directories above it up to e2e/ that it left empty.
func removeAll(dir string, errs *[]error) int64 {
	n := dirSize(dir)
	if err := os.RemoveAll(dir); err != nil {
		*errs = append(*errs, err)
		return 0
	}
	for parent := filepath.Dir(dir); filepath.Base(parent) != "e2e" && filepath.Base(parent) != "."; parent = filepath.Dir(parent) {
		if os.Remove(parent) != nil { // not empty, or gone: stop
			break
		}
	}
	return n
}

// globQuote makes s match itself in a filepath.Glob pattern.
func globQuote(s string) string {
	return strings.NewReplacer("*", "[*]", "?", "[?]", "[", "[[]", `\`, `\\`).Replace(s)
}
