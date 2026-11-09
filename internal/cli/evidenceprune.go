package cli

import (
	"fmt"
	"io"
	"log/slog"

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
