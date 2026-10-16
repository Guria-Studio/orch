package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hectorcanaimero/orch/internal/ci"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckCIStack(t *testing.T) {
	root := t.TempDir()
	if c := CheckCIStack(root); c.Status != StatusSkip {
		t.Errorf("no workflow: %+v", c)
	}
	writeFile(t, root, ci.WorkflowPath, "jobs:\n  test:\n    steps:\n      - name: "+ci.PlaceholderStep+"\n")
	if c := CheckCIStack(root); c.Status != StatusOK {
		t.Errorf("placeholder, no code yet: %+v", c)
	}
	writeFile(t, root, "go.mod", "module x\n\ngo 1.25\n")
	if c := CheckCIStack(root); c.Status != StatusWarn || c.Remediation == "" {
		t.Errorf("placeholder + go.mod: %+v, want a warning naming orch ci setup", c)
	}
}

func TestCheckCIMinutes(t *testing.T) {
	root := t.TempDir()
	vis := func(v string, err error) func(string) (string, error) {
		return func(string) (string, error) { return v, err }
	}
	if c := CheckCIMinutes(root, vis("private", nil)); c.Status != StatusSkip {
		t.Errorf("no workflow: %+v", c)
	}
	writeFile(t, root, ci.WorkflowPath, "jobs: {}\n")
	cases := []struct {
		v    string
		err  error
		want Status
	}{
		{"public", nil, StatusOK},
		{"private", nil, StatusWarn},
		{"internal", nil, StatusWarn},
		{"", errors.New("no remote"), StatusSkip},
	}
	for _, c := range cases {
		if got := CheckCIMinutes(root, vis(c.v, c.err)); got.Status != c.want {
			t.Errorf("%q/%v: %+v, want %s", c.v, c.err, got, c.want)
		}
	}
}
