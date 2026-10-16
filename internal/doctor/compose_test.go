package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles lays out files under root, creating directories as needed.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// #343: a compose file with a fixed top-level name or a fixed published host
// port collides when two tasks bring it up from sibling worktrees at once.
// The check names each file and what in it collides, and leaves alone what
// cannot: a variable port, a container-only port, a file orch never sees.
func TestCheckComposeIsolation(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		isolate  bool
		status   Status
		contains []string
		absent   []string
	}{
		{
			name:    "no compose files",
			isolate: true, status: StatusOK,
			contains: []string{"no compose file"},
		},
		{
			name: "variable and container-only ports, no name",
			files: map[string]string{"docker-compose.yml": `
services:
  api:
    ports:
      - "${API_PORT:-3001}:3001"
      - "3002"
      - 3003
      - "127.0.0.1::3004"
  web:
    ports:
      - target: 80
        published: "${WEB_PORT:-8080}"
      - target: 81
`},
			isolate: true, status: StatusOK,
			contains: []string{"1 compose file"},
		},
		{
			name: "a fixed top-level name",
			files: map[string]string{"compose.yaml": `
name: predicts
services:
  api:
    image: x
`},
			isolate: true, status: StatusWarn,
			contains: []string{"compose.yaml", `name: "predicts"`, "COMPOSE_PROJECT_NAME"},
		},
		{
			name: "fixed host ports, short and long syntax",
			files: map[string]string{"docker-compose.yml": `
services:
  api:
    ports:
      - "3001:3001"
      - "127.0.0.1:5432:5432/tcp"
  web:
    ports:
      - target: 80
        published: 8080
`},
			isolate: true, status: StatusWarn,
			contains: []string{"docker-compose.yml", "api publishes host port 3001", "api publishes host port 5432", "web publishes host port 8080", "ORCH_TASK_SLOT", "${API_PORT:-3001}:3001"},
			absent:   []string{"name:"},
		},
		{
			name: "one level down is scanned, two levels and node_modules are not",
			files: map[string]string{
				"deploy/docker-compose.prod.yml":      "name: shop\nservices: {}\n",
				"a/b/compose.yml":                     "name: deep\nservices: {}\n",
				"node_modules/compose.yml":            "name: vendored\nservices: {}\n",
				".git/compose.yml":                    "name: hidden\nservices: {}\n",
				"deploy/not-a-compose-file.yml":       "name: unrelated\n",
				"deploy/docker-compose.override.yaml": "services:\n  db:\n    ports: [\"6543:5432\"]\n",
			},
			isolate: true, status: StatusWarn,
			contains: []string{filepath.Join("deploy", "docker-compose.prod.yml"), `name: "shop"`, "db publishes host port 6543"},
			absent:   []string{"deep", "vendored", "hidden", "unrelated"},
		},
		{
			name:    "isolation off says how to turn it on",
			files:   map[string]string{"compose.yml": "name: predicts\nservices: {}\n"},
			isolate: false, status: StatusWarn,
			contains: []string{"dispatch.isolate_compose is off", "isolate_compose: true"},
		},
		{
			name:    "a file that does not parse is not vouched for",
			files:   map[string]string{"compose.yml": "services: [unclosed\n"},
			isolate: true, status: StatusWarn,
			contains: []string{"compose.yml", "could not parse"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, c.files)
			got := CheckComposeIsolation(root, c.isolate)
			if got.Name != "compose.isolation" {
				t.Errorf("name = %q", got.Name)
			}
			if got.Status != c.status {
				t.Errorf("status = %q, want %q: %+v", got.Status, c.status, got)
			}
			text := got.Detail + "\n" + got.Remediation
			for _, want := range c.contains {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in:\n%s", want, text)
				}
			}
			for _, not := range c.absent {
				if strings.Contains(text, not) {
					t.Errorf("unexpected %q in:\n%s", not, text)
				}
			}
		})
	}
}
