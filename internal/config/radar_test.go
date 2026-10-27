package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// radar (#482) warns by default; a mode orch does not know is refused at load.
func TestRadarModes(t *testing.T) {
	cases := []struct{ name, yaml, want, wantErr string }{
		{"default", "", "warn", ""},
		{"serialize", "radar: Serialize\n", "serialize", ""},
		{"off", "radar: off\n", "off", ""},
		{"unknown mode", "radar: block\n", "", "radar"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			write(t, path, c.yaml)
			res, err := Load(path, filepath.Dir(path))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("Load error = %v, want one naming %s", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if res.Config.Radar != c.want {
				t.Errorf("radar = %q, want %q", res.Config.Radar, c.want)
			}
			for _, w := range res.Warnings {
				if strings.Contains(w, "radar") {
					t.Errorf("radar warned as unknown: %s", w)
				}
			}
		})
	}
}
