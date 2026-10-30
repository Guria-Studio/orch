package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
)

// TestOldConfigKeepsConflictRetriesOff (#438): a config written before
// retry.conflict existed blocks a conflicting PR as before — nothing pushed,
// no agent paid for. Only a config that writes the key turns them on.
func TestOldConfigKeepsConflictRetriesOff(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"without the key": {"retry:\n  backoff_seconds: 5\n  rate_limit_backoff_seconds: 60\n", 0},
		"with the key":    {"retry:\n  conflict:\n    max_attempts: 1\n", 1},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := config.Load(path, dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := conflictRetries(loaded.Config); got != tc.want {
				t.Errorf("conflict retries = %d, want %d", got, tc.want)
			}
		})
	}
}
