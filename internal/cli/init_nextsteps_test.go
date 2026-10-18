package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `orch init` ends by printing the commands to run next, and a new user
// copies them literally. Its "Run it" step printed `orch --project-root X
// --mode semi` — `--mode` belongs to `orch run`, so the first command anyone
// ran after init failed with "unknown flag". Every `orch …` line init prints,
// with and without a template, has to resolve against the real command tree.
func TestInitNextStepsOnlyNameCommandsThatExist(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"blank project", nil},
		{"from a template", []string{"--template", "nextjs-saas"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// claude installed, the planning skills not yet: every line init
			// can print about the next steps is printed.
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o700); err != nil { // #nosec G306 -- an executable fake
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			t.Setenv("HOME", t.TempDir())
			root, _ := newRootCmd("test")
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			dir := filepath.Join(t.TempDir(), "proj")
			root.SetArgs(append([]string{"init", dir}, tc.args...))
			if err := root.Execute(); err != nil {
				t.Fatalf("orch init: %v\n%s", err, out.String())
			}

			checked := 0
			for _, line := range strings.Split(out.String(), "\n") {
				inv := strings.TrimSpace(line)
				if !strings.HasPrefix(inv, "orch ") {
					continue
				}
				checked++
				fresh, _ := newRootCmd("test")
				if problem := resolveInvocation(fresh, inv); problem != "" {
					t.Errorf("init tells the user to run `%s`: %s", inv, problem)
				}
			}
			// orch run, install-skills, claude-permissions: an extractor that
			// finds none would make this a test of nothing.
			if checked < 3 {
				t.Fatalf("found %d orch commands in init's output, want at least 3:\n%s", checked, out.String())
			}
		})
	}
}
