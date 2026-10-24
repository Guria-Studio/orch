package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/demo"
)

// `orch report summary` words the demo's finished run in the language asked
// for, as text or JSON.
func TestReportSummaryAgainstTheDemo(t *testing.T) {
	paths, err := demo.Build(context.Background(), filepath.Join(t.TempDir(), "demo"), time.Now())
	if err != nil {
		t.Fatalf("demo.Build: %v", err)
	}
	run := func(args ...string) (string, error) {
		root, _ := newRootCmd("test")
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(append([]string{"report", "summary", "--project-root", paths.Root, "--project-id", paths.ID}, args...))
		err := root.Execute()
		return out.String(), err
	}
	if out, err := run("--language", "en"); err != nil || !strings.HasPrefix(out, "Run "+demo.HistoryRunID+" finished: 16 done, 0 blocked.") || !strings.Contains(out, "Next:") {
		t.Errorf("en: %v\n%s", err, out)
	}
	if out, err := run("--language", "es"); err != nil || !strings.Contains(out, "16 hechas") {
		t.Errorf("es: %v\n%s", err, out)
	}
	if out, err := run("--json", "--language", "pt"); err != nil || !strings.Contains(out, `"lang":"pt"`) || !strings.Contains(out, `"actions":[`) {
		t.Errorf("--json: %v\n%s", err, out)
	}
	if _, err := run("--language", "fr"); err == nil {
		t.Error("--language fr should be refused")
	}
}
