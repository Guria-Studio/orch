package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
)

// fakeGemini puts a `gemini` on PATH that prints help as its --help, and
// otherwise exits 0.
func fakeGemini(t *testing.T, help string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = --help ]; then printf '%s\\n' '" + help + "'; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "gemini"), []byte(script), 0o700); err != nil { // #nosec G306 -- an executable test stub
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The probe reads gemini's own help: 0.59's lists stream-json among the
// --output-format choices, an older one has no such flag (#543).
func TestProbeGeminiStreamJSON(t *testing.T) {
	fakeGemini(t, `  -o, --output-format  The format of the CLI output.  [string] [choices: "text", "json", "stream-json"]`)
	if !probeGeminiStreamJSON() {
		t.Error("a gemini whose help lists stream-json was not run with it")
	}
	fakeGemini(t, "  -p, --prompt  Prompt.")
	if probeGeminiStreamJSON() {
		t.Error("a gemini without the flag would be handed one it rejects")
	}
	t.Setenv("PATH", t.TempDir())
	if probeGeminiStreamJSON() {
		t.Error("no gemini at all probed as stream-json")
	}
}

// Spawn hands gemini --output-format stream-json when the probe says the
// installed CLI has it, and not otherwise.
func TestSpawnAsksGeminiForStreamJSON(t *testing.T) {
	fakeGemini(t, "")
	for _, has := range []bool{true, false} {
		saved := GeminiStreamJSON
		GeminiStreamJSON = func() bool { return has }
		t.Cleanup(func() { GeminiStreamJSON = saved })

		sp, err := Spawn(context.Background(), SpawnRequest{
			Provider:   providers.GeminiProvider{},
			Req:        providers.Request{TaskID: "G-1", Route: model.RouteEntry{Backend: model.BackendGemini, CLIModel: "gemini-2.5-flash"}},
			PromptPath: promptFile(t, "hi"),
			LogPath:    filepath.Join(t.TempDir(), "G-1.log"),
			Cwd:        t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		sp.Wait(10 * time.Second)
		if got := strings.Contains(strings.Join(sp.cmd.Args, " "), "--output-format stream-json"); got != has {
			t.Errorf("probe %v: argv %q", has, sp.cmd.Args)
		}
	}
}

// A run whose CLI reported no usage at all (gemini without stream-json, a
// silent opencode) is estimated from the size of its prompt and output, so the
// spend views and the budget see a real dispatch rather than a free one
// (#543). A run that reported its tokens is left as it reported them.
func TestSpendRowEstimatesUnreportedUsage(t *testing.T) {
	d := Dispatch{
		Req:        providers.Request{TaskID: "G-1", Route: model.RouteEntry{Backend: model.BackendGemini, CLIModel: "gemini-2.5-flash"}},
		PromptPath: promptFile(t, strings.Repeat("p", 4000)),
	}
	o := Outcome{Result: providers.Result{Success: true, Stdout: strings.Repeat("o", 800), Estimated: true}}

	row := spendRow(d, o, 1)
	if row.TokensIn != 1000 || row.TokensOut != 200 {
		t.Errorf("tokens = (%d,%d), want (1000,200): four characters a token", row.TokensIn, row.TokensOut)
	}
	if !row.Estimated {
		t.Error("an estimate was recorded as reported")
	}

	o.Result = providers.Result{Success: true, Stdout: strings.Repeat("o", 800), TokensIn: 7, TokensOut: 3}
	if row := spendRow(d, o, 1); row.TokensIn != 7 || row.TokensOut != 3 || row.Estimated {
		t.Errorf("reported usage was overwritten: %+v", row)
	}
}
