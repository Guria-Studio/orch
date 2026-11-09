package engine

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
)

// codexFixtureHome is a CODEX_HOME whose sessions/ holds trimmed real
// rollouts, keyed by the thread ids of the exec logs beside them.
func codexFixtureHome(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "providers", "testdata", "codex", "0.154.0"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func codexDispatch(env ...string) Dispatch {
	return Dispatch{
		Req: providers.Request{TaskID: "B-020", Route: model.RouteEntry{Backend: model.BackendCodex, CLIModel: "gpt-6-astra"}},
		Env: env,
	}
}

// After a codex run the engine reads the session rollout its thread id
// names and keeps the plan's windows on the result, where outcomeEvent
// turns them into extra.quota (#577).
func TestReadSessionFactsTakesCodexQuotaFromTheRollout(t *testing.T) {
	d := codexDispatch("HOME=/nonexistent", "CODEX_HOME="+codexFixtureHome(t))
	res := providers.Result{SessionID: "01a16611-5f35-7b43-a5a8-0c137c0da92a"}
	if err := readSessionFacts(d, &res); err != nil {
		t.Fatalf("readSessionFacts: %v", err)
	}
	want := []providers.QuotaWindow{
		{Window: "five_hour", Utilization: 0.20, ResetsAt: 1792722104},
		{Window: "seven_day", Utilization: 0.03, ResetsAt: 1793308904},
	}
	if !reflect.DeepEqual(res.Quota, want) {
		t.Errorf("Quota = %+v, want %+v", res.Quota, want)
	}
	ev := outcomeEvent("run-1", d, Outcome{Result: res}, "2026-10-23T00:03:02Z", 1)
	if got, ok := ev.Extra["quota"].([]providers.QuotaWindow); !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("outcome extra.quota = %#v", ev.Extra["quota"])
	}
}

// Nothing to read is not a failure: no thread id, no rollout for it, no
// sessions directory, or a backend that keeps none. The result is left as
// the parser made it.
func TestReadSessionFactsMissingRolloutIsNotAFailure(t *testing.T) {
	home := codexFixtureHome(t)
	cases := []struct {
		name string
		d    Dispatch
		id   string
	}{
		{"no thread id", codexDispatch("CODEX_HOME=" + home), ""},
		{"no rollout for the thread", codexDispatch("CODEX_HOME=" + home), "01a00000-0000-7000-8000-000000000000"},
		{"no sessions dir", codexDispatch("CODEX_HOME=" + t.TempDir()), "01a16611-5f35-7b43-a5a8-0c137c0da92a"},
		{"not codex", Dispatch{Req: providers.Request{Route: model.RouteEntry{Backend: model.BackendClaude}}, Env: []string{"CODEX_HOME=" + home}}, "01a16611-5f35-7b43-a5a8-0c137c0da92a"},
	}
	for _, c := range cases {
		res := providers.Result{SessionID: c.id}
		if err := readSessionFacts(c.d, &res); err != nil {
			t.Errorf("%s: err = %v, want nil", c.name, err)
		}
		if res.Quota != nil {
			t.Errorf("%s: Quota = %+v, want nil", c.name, res.Quota)
		}
	}
}

// The dispatch's own environment decides where codex kept the session; the
// last assignment of a variable wins, as it does for the child.
func TestDispatchEnvLookup(t *testing.T) {
	get := dispatchGetenv([]string{"A=1", "B=2", "A=3", "C"})
	if get("A") != "3" || get("B") != "2" || get("C") != "" {
		t.Errorf("A=%q B=%q C=%q", get("A"), get("B"), get("C"))
	}
}
