package cli

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/Guria-Studio/orch/internal/publish/snapshot"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/evidence"
)

// pruneEvidence keeps each task's evidence to evidence.keep_attempts and
// evidence.max_bytes_per_task once a run ends (#520 §3.5), and says what it
// freed. A failure is logged: the run's outcome is not the evidence's.
func pruneEvidence(w io.Writer, paths config.Paths, cfg config.Config) {
	got, err := evidence.Prune(paths.StateDir(), cfg.Evidence.Keep(), cfg.Evidence.MaxBytes())
	if err != nil {
		slog.Warn("pruning the evidence failed", "err", err)
	}
	if got.Attempts == 0 {
		return
	}
	noun := "attempts"
	if got.Attempts == 1 {
		noun = "attempt"
	}
	if _, err := fmt.Fprintf(w, "evidence: pruned %d old %s (%.1f MiB; evidence.keep_attempts: %d)\n",
		got.Attempts, noun, float64(got.Bytes)/(1<<20), cfg.Evidence.Keep()); err != nil {
		slog.Warn("saying what the evidence pruning freed failed", "err", err)
	}
}

// publishMediaFor is publishMedia for the snapshot about to be exported.
func publishMediaFor(paths config.Paths, cfg config.Config, snap snapshot.Snapshot) map[string]string {
	shown := snapshot.MediaPaths(snap)
	if len(shown) == 0 {
		return nil
	}
	all, err := evidence.ReadAll(paths.StateDir())
	if err != nil {
		slog.Warn("reading the evidence for the published media", "err", err)
	}
	return publishMedia(paths, cfg, all, shown)
}

// publishMedia maps each evidence video and poster in shown (site paths the
// snapshot uses) to its file under the state dir (#520 §3.4). Only with
// evidence.publish.video on.
func publishMedia(paths config.Paths, cfg config.Config, all map[string]evidence.Manifest, shown []string) map[string]string {
	if !cfg.Evidence.Publish.Video || len(shown) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, s := range shown {
		want[s] = true
	}
	out := map[string]string{}
	for _, m := range all {
		cm := evidence.ForClient(m, true, cfg.Evidence.Publish.MaxVideo())
		for site, src := range map[string]string{cm.Video: cm.VideoSrc, cm.Poster: cm.PosterSrc} {
			if site != "" && want[site] {
				out[site] = filepath.Join(paths.StateDir(), filepath.FromSlash(src))
			}
		}
	}
	return out
}

// stakeholderEvidence is the evidence the client snapshot reads: every
// task's manifest, and whether its media show.
func stakeholderEvidence(paths config.Paths) map[string]evidence.Manifest {
	all, err := evidence.ReadAll(paths.StateDir())
	if err != nil {
		slog.Warn("reading the evidence for the client page", "err", err)
	}
	return all
}
