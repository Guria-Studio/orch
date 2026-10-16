package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// #363: a private repository's workflow runs on pull requests only, and says
// why; every workflow cancels a pull request's superseded runs.
func TestRenderForAPrivateRepository(t *testing.T) {
	root := repo(t, map[string]string{"Makefile": ".PHONY: test\ntest:\n\t./run-tests.sh\n"})
	st, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Render(Jobs(st, AllChecks), Options{BaseBranch: "dev", Checks: AllChecks, Private: true})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "makefile-private", out, "make")
	var doc struct {
		On          map[string]any `yaml:"on"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel string `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if _, push := doc.On["push"]; push || !strings.Contains(string(out), "private") {
		t.Errorf("a private repository's workflow still runs on push, or does not say why:\n%s", out)
	}
	if !strings.Contains(doc.Concurrency.Group, "pull_request.number") || !strings.Contains(doc.Concurrency.Cancel, "pull_request") {
		t.Errorf("concurrency = %+v", doc.Concurrency)
	}
}

func TestPlaceholderOutgrown(t *testing.T) {
	placeholder := "jobs:\n  test:\n    steps:\n      - name: " + PlaceholderStep + "\n"
	write := func(root, name, body string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	if PlaceholderOutgrown(root) {
		t.Error("no workflow at all is not an outgrown placeholder")
	}
	write(root, WorkflowPath, placeholder)
	if PlaceholderOutgrown(root) {
		t.Error("the placeholder of a repository with nothing to check yet is still right")
	}
	write(root, "Makefile", ".PHONY: test\ntest:\n\t./t.sh\n")
	if !PlaceholderOutgrown(root) {
		t.Error("placeholder + a Makefile with a test target: want outgrown")
	}
	write(root, WorkflowPath, "jobs: {}\n")
	if PlaceholderOutgrown(root) {
		t.Error("a workflow the operator wrote is never outgrown")
	}
}
