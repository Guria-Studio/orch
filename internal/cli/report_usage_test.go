package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/demo"
	"github.com/Guria-Studio/orch/internal/state"
	"github.com/Guria-Studio/orch/internal/usage"
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
	// The demo's F2.T5 failed twice before it passed, and its codex rows carry reasoning.
	for _, want := range []string{"Total: ", "Retries: ", "cached input", "By model", "By task (heaviest 3 of",
		"      attempt 2: ", "not your provider plan's quota"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}

	var u usage.Usage
	if err := json.Unmarshal([]byte(run("--json")), &u); err != nil {
		t.Fatal(err)
	}
	if u.Total.Dispatches == 0 || len(u.ByModel) == 0 || len(u.ByTask) <= 3 || u.Total.CachedInput == 0 ||
		u.Total.Reasoning == 0 || u.RetryTokens == 0 {
		t.Errorf("--json = %+v; want the demo's dispatches, models, tasks and cache reads", u.Total)
	}
}

// Before any dispatch the text says so, and still says what the numbers are not.
func TestWriteUsageWithNoDispatch(t *testing.T) {
	var b bytes.Buffer
	if err := writeUsage(&b, usage.Build(nil), 10); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); !strings.HasPrefix(got, "No dispatch has been recorded yet.\n") || !strings.Contains(got, "not your provider plan's quota") {
		t.Errorf("text:\n%s", got)
	}
}

// The plan's windows each CLI last reported sit beside the project's own
// tokens, named as the account's (#577).
func TestWriteUsageShowsThePlanQuota(t *testing.T) {
	u := usage.Build(nil)
	u.Quota = []usage.PlanQuota{{Backend: "codex", SeenAt: "2026-10-23T00:03:02Z", Windows: []state.QuotaWindow{
		{Window: "five_hour", Utilization: 0.2, ResetsAt: 1792722104},
		{Window: "seven_day", Utilization: 0.03, ResetsAt: 1793308904},
	}}}
	var b bytes.Buffer
	if err := writeUsage(&b, u, 10); err != nil {
		t.Fatal(err)
	}
	want := "Plan quota (the whole account, as each CLI last reported it)\n" +
		"  codex — five_hour 20% used, resets 2026-10-23T02:21:44Z · seven_day 3% used, resets 2026-10-29T21:21:44Z (seen 2026-10-23T00:03:02Z)\n"
	if got := b.String(); !strings.Contains(got, want) {
		t.Errorf("text lacks\n%s\ngot:\n%s", want, got)
	}
}
