package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergeQueueConfig (#522): off by default with when_base_moved; reverify
// is read case-insensitively and anything but the two values is refused.
func TestMergeQueueConfig(t *testing.T) {
	for _, tc := range []struct {
		name, yaml   string
		enabled      bool
		reverify     string
		wantErrMatch string
	}{
		{"absent", "", false, "when_base_moved", ""},
		{"on", "merge_queue:\n  enabled: true\n", true, "when_base_moved", ""},
		{"never", "merge_queue:\n  enabled: true\n  reverify: NEVER\n", true, "never", ""},
		{"unknown", "merge_queue:\n  reverify: always\n", false, "", "merge_queue.reverify"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			res, err := Load(path, dir)
			if tc.wantErrMatch != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrMatch) {
					t.Fatalf("Load = %v, want an error naming %s", err, tc.wantErrMatch)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if q := res.Config.MergeQueue; q.Enabled != tc.enabled || q.Reverify != tc.reverify {
				t.Errorf("merge_queue = %+v, want enabled=%v reverify=%s", q, tc.enabled, tc.reverify)
			}
		})
	}
}
