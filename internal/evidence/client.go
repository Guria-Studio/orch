package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ClientMedia is what a client page may show of a task's evidence (#520
// §3.4): the walk-through of its first @evidence test that passed and, only
// when the operator opted in (evidence.publish.video), that test's video and
// screenshot — as paths inside a published site, and where they are under
// the state directory. The site paths are a hash: no task id, run or
// attempt reaches the client.
type ClientMedia struct {
	Walkthrough *Walkthrough
	// Video and Poster are site paths, "" when not shown; VideoSrc and
	// PosterSrc are relative to the state directory.
	Video, Poster       string
	VideoSrc, PosterSrc string
}

// ForClient is m's client media. video is evidence.publish.video; a video
// over maxVideoBytes is left out and its screenshot shown alone.
func ForClient(m Manifest, video bool, maxVideoBytes int64) ClientMedia {
	var out ClientMedia
	if m.E2E == nil {
		return out
	}
	for i := range m.E2E.Walkthroughs {
		if w := m.E2E.Walkthroughs[i]; w.Status == "passed" {
			out.Walkthrough = &w
			break
		}
	}
	if out.Walkthrough == nil || !video || m.E2E.Dir == "" {
		return out
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", m.TaskID, m.RunID, m.Attempt)))
	dir := "evidence/" + hex.EncodeToString(sum[:8])
	w := out.Walkthrough
	if w.Video != "" && sizeOf(m.E2E.Media, w.Video) <= maxVideoBytes {
		out.Video, out.VideoSrc = dir+"/video"+path.Ext(w.Video), path.Join(m.E2E.Dir, w.Video)
	}
	if w.Screenshot != "" {
		out.Poster, out.PosterSrc = dir+"/poster"+path.Ext(w.Screenshot), path.Join(m.E2E.Dir, w.Screenshot)
	}
	return out
}

// sizeOf is the recorded size of the medium at p; unknown counts as too big.
func sizeOf(media []Media, p string) int64 {
	for _, m := range media {
		if m.Path == p {
			return m.Bytes
		}
	}
	return 1 << 62
}

// ReadAll is every task's evidence (the pushed attempts), by task id. A
// manifest that cannot be read is skipped and named in the error.
func ReadAll(stateDir string) (map[string]Manifest, error) {
	out := map[string]Manifest{}
	entries, err := os.ReadDir(filepath.Join(stateDir, Dir))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("evidence: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m, err := Read(stateDir, e.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if m != nil {
			out[e.Name()] = *m
		}
	}
	return out, errors.Join(errs...)
}
