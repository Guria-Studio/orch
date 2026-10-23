package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/demo"
	"github.com/hectorcanaimero/orch/internal/usage"
)

// `orch report usage` (#417) against the demo's spend: the text names the
// split and the quota caveat, --json is the whole breakdown.
func TestReportUsageAgainstTheDemo(t *testing.T) {
	paths, err := demo.Build(context.Background(), filepath.Join(t.TempDir(), "demo"), time.Now())
	if err != nil {
		t.Fatalf("demo.Build: %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		root, _ := newRootCmd("test")
		var out, errOut bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(append([]string{"report", "usage", "--project-root", paths.Root, "--project-id", paths.ID}, args...))
		if err := root.Execute(); err != nil {
			t.Fatalf("orch report usage %v: %v\n%s", args, err, errOut.String())
		}
		return out.String()
	}

	text := run("--top", "3")
	for _, want := range []string{"Total: ", "cached input", "By model", "By task (heaviest 3 of", "not your provider plan's quota"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}

	var u usage.Usage
	if err := json.Unmarshal([]byte(run("--json")), &u); err != nil {
		t.Fatal(err)
	}
	if u.Total.Dispatches == 0 || len(u.ByModel) == 0 || len(u.ByTask) <= 3 || u.Total.CachedInput == 0 {
		t.Errorf("--json = %+v; want the demo's dispatches, models, tasks and cache reads", u.Total)
	}
}
