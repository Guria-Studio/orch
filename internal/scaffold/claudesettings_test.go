package scaffold

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hectorcanaimero/orch/internal/doctor"
)

func TestAllowOrchTools(t *testing.T) {
	tests := []struct {
		name      string
		before    string // "" = no file
		wantAdded []string
		want      string
	}{
		{
			name:      "no settings yet",
			wantAdded: doctor.OrchToolRules,
			want: `{
  "permissions": {
    "allow": [
      "mcp__orch",
      "Bash(scripts/task-finish.sh:*)",
      "Bash(scripts/task-block.sh:*)"
    ]
  }
}
`,
		},
		{
			name:      "the operator's keys, rules and order stay",
			before:    `{"$schema":"x","permissions":{"deny":["Bash(rm:*)"],"allow":["Bash(go test:*)","mcp__orch"]},"env":{"A":"1"}}`,
			wantAdded: []string{"Bash(scripts/task-finish.sh:*)", "Bash(scripts/task-block.sh:*)"},
			want: `{
  "$schema": "x",
  "permissions": {
    "deny": [
      "Bash(rm:*)"
    ],
    "allow": [
      "Bash(go test:*)",
      "mcp__orch",
      "Bash(scripts/task-finish.sh:*)",
      "Bash(scripts/task-block.sh:*)"
    ]
  },
  "env": {
    "A": "1"
  }
}
`,
		},
		{
			name:   "nothing missing leaves the file alone",
			before: `{"permissions":{"allow":["mcp__orch__*","Bash"]}}`,
			want:   `{"permissions":{"allow":["mcp__orch__*","Bash"]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".claude", "settings.json")
			if tt.before != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tt.before), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			added, err := AllowOrchTools(root)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(added, tt.wantAdded) {
				t.Errorf("added = %v, want %v", added, tt.wantAdded)
			}
			got, err := os.ReadFile(path) // #nosec G304 -- under t.TempDir()
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("file:\n%s\nwant:\n%s", got, tt.want)
			}
			if m := doctor.MissingOrchRules(root); len(m) != 0 {
				t.Errorf("still missing %v", m)
			}
		})
	}
}

// A file orch cannot read as an object is reported, never overwritten.
func TestAllowOrchToolsRefusesABrokenFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"permissions": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AllowOrchTools(root); err == nil {
		t.Fatal("want an error")
	}
	if got, _ := os.ReadFile(path); string(got) != `{"permissions": [` { // #nosec G304 -- under t.TempDir()
		t.Errorf("file changed to %s", got)
	}
}
