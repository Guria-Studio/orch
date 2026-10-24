package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hectorcanaimero/orch/internal/doctor"
	"github.com/hectorcanaimero/orch/internal/engine"
)

// #437: the smoke dispatches through `orch run`'s own path (here with the
// fake runner standing in for claude), reads each check back from the
// project it made, exits non-zero on a failed check, and leaves nothing
// behind.
func TestDoctorSmokeThroughTheFakeRunner(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	const nonce = "smoke-feedf00d"
	prev := smokeNonce
	smokeNonce = func() string { return nonce }
	t.Cleanup(func() { smokeNonce = prev })

	fake := t.TempDir()
	if err := os.Mkdir(filepath.Join(fake, "claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	// claude's answer echoes the token, with usage and a cost; what the fake
	// cannot do is call orch's tools, so the agent never reports done itself.
	out := `{"type":"result","subtype":"success","is_error":false,"result":"done ` + nonce + `",` +
		`"total_cost_usd":0.0012,"usage":{"input_tokens":4,"cache_read_input_tokens":900,"cache_creation_input_tokens":20,"output_tokens":30}}` + "\n"
	if err := os.WriteFile(filepath.Join(fake, "claude", "_default.out"), []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(engine.FakeProviderEnv, fake)

	root, _ := newRootCmd("test")
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"doctor", "--smoke", "--backend", "claude", "--json"})
	err := root.Execute()

	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 1 {
		t.Fatalf("err = %v, want exit 1\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	var p smokePayload
	if err := json.Unmarshal(stdout.Bytes(), &p); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout.String())
	}
	if len(p.Backends) != 1 || p.Backends[0].Backend != "claude" || p.Backends[0].Model != "claude-haiku-4-5" {
		t.Fatalf("rows = %+v", p.Backends)
	}
	got := map[string]doctor.Status{}
	for _, c := range p.Backends[0].Checks {
		got[c.Name] = c.Status
	}
	want := map[string]doctor.Status{
		"smoke.prompt": doctor.StatusOK, "smoke.mcp": doctor.StatusError, "smoke.done": doctor.StatusError,
		"smoke.usage": doctor.StatusOK, "smoke.route": doctor.StatusOK,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q, want %q (%+v)", name, got[name], w, p.Backends[0].Checks)
		}
	}
	if p.SpendUSD != 0.0012 {
		t.Errorf("spend = %v, want 0.0012", p.SpendUSD)
	}

	left, err := os.ReadDir(filepath.Join(cache, "orch"))
	if err != nil || len(left) != 0 {
		t.Errorf("the smoke left %v behind (err %v)", left, err)
	}
}

// The human report: one block per backend with its model, effort and spend,
// each check on its own line, and the verdict with the total.
func TestRenderSmoke(t *testing.T) {
	var b bytes.Buffer
	err := renderSmoke(&b, smokePayload{
		Backends: []smokeRow{{Backend: "codex", Model: "gpt-5.4", Effort: "low", SpendUSD: 0.0123, Checks: []doctor.Check{
			{Name: "smoke.prompt", Status: doctor.StatusOK, Detail: "the agent wrote back the token"},
			{Name: "smoke.done", Status: doctor.StatusError, Detail: "the task ended blocked"},
		}}},
		SpendUSD: 0.0123, ExitCode: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"codex · gpt-5.4 · effort low · $0.0123", "ok     smoke.prompt  the agent wrote back the token",
		"error  smoke.done    the task ended blocked", "spent $0.0123 · a check failed"} {
		if !bytes.Contains(b.Bytes(), []byte(want)) {
			t.Errorf("missing %q in:\n%s", want, b.String())
		}
	}
}

// --backend and --keep mean nothing without --smoke, and say so rather than
// running the ordinary doctor.
func TestDoctorSmokeFlagsNeedSmoke(t *testing.T) {
	// Should the guard ever break, the ordinary doctor runs here and not in
	// the package directory, where it would leave a database behind.
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"doctor", "--backend", "claude"}, {"doctor", "--keep"}} {
		out, err := runOrch(t, "", args...)
		var ee *exitError
		if !errors.As(err, &ee) || ee.code != 2 || !bytes.Contains([]byte(err.Error()), []byte("need --smoke")) {
			t.Errorf("%v: err = %v\n%s", args, err, out)
		}
	}
}
